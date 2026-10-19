package woyra

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Webhook header names and the default replay window.
const (
	SignatureHeader         = "Woyra-Signature"
	TimestampHeader         = "Woyra-Timestamp"
	DefaultWebhookTolerance = 5 * time.Minute
)

// WebhooksService registers webhook endpoints and verifies deliveries.
type WebhooksService struct{ c *Client }

// Create registers an endpoint. The response carries its signing Secret, once.
func (s *WebhooksService) Create(ctx context.Context, params *CreateWebhookEndpointParams, opts ...RequestOption) (*WebhookEndpoint, error) {
	out := new(WebhookEndpoint)
	if err := s.c.do(ctx, http.MethodPost, "/v1/webhook_endpoints", nil, params, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// List lists the account's endpoints. Secrets are not returned again.
func (s *WebhooksService) List(ctx context.Context, params *ListParams, opts ...RequestOption) (*List[WebhookEndpoint], error) {
	out := new(List[WebhookEndpoint])
	if err := s.c.do(ctx, http.MethodGet, "/v1/webhook_endpoints", listQuery(params), nil, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Remove deletes an endpoint.
func (s *WebhooksService) Remove(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.do(ctx, http.MethodDelete, "/v1/webhook_endpoints/"+url.PathEscape(id), nil, nil, nil, opts)
}

// Verify is VerifyWebhook, for symmetry with the other SDKs.
func (s *WebhooksService) Verify(payload []byte, header http.Header, secret string, opts ...VerifyOption) (*WebhookEvent, error) {
	return VerifyWebhook(payload, header, secret, opts...)
}

// VerifyOption configures VerifyWebhook.
type VerifyOption func(*verifyConfig)

type verifyConfig struct {
	tolerance time.Duration
	now       time.Time
}

// VerifyTolerance sets the replay window. Default 5 minutes.
func VerifyTolerance(d time.Duration) VerifyOption { return func(v *verifyConfig) { v.tolerance = d } }

// VerifyNow sets the current time, for tests.
func VerifyNow(t time.Time) VerifyOption { return func(v *verifyConfig) { v.now = t } }

// SignWebhook computes the Woyra-Signature value for a payload, exactly as
// Woyra does: "v1=" + hex(HMAC-SHA256(secret, "<unix seconds>.<payload>")).
func SignWebhook(secret string, timestamp time.Time, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp.Unix(), 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

var timestampPattern = regexp.MustCompile(`^[0-9]{1,15}$`)

// VerifyWebhook checks a delivery's Woyra-Signature and Woyra-Timestamp
// headers and returns the decoded event.
//
// payload must be the raw request body exactly as received. Errors wrap
// ErrMissingSecret, ErrMissingHeader, ErrInvalidTimestamp,
// ErrSignatureExpired, ErrInvalidSignature or ErrInvalidPayload.
func VerifyWebhook(payload []byte, header http.Header, secret string, opts ...VerifyOption) (*WebhookEvent, error) {
	cfg := verifyConfig{tolerance: DefaultWebhookTolerance}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.now.IsZero() {
		cfg.now = time.Now()
	}
	if secret == "" {
		return nil, ErrMissingSecret
	}
	signature := strings.Join(header.Values(SignatureHeader), ",")
	tsHeader := strings.TrimSpace(header.Get(TimestampHeader))
	if signature == "" {
		return nil, fmt.Errorf("%w: %s", ErrMissingHeader, SignatureHeader)
	}
	if tsHeader == "" {
		return nil, fmt.Errorf("%w: %s", ErrMissingHeader, TimestampHeader)
	}
	if !timestampPattern.MatchString(tsHeader) {
		return nil, ErrInvalidTimestamp
	}
	seconds, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		return nil, ErrInvalidTimestamp
	}
	timestamp := time.Unix(seconds, 0)
	// Checked in both directions: a far-future timestamp is as much a forgery
	// signal as a stale one.
	if delta := cfg.now.Sub(timestamp); delta > cfg.tolerance || delta < -cfg.tolerance {
		return nil, fmt.Errorf("%w: timestamp is %s away from now", ErrSignatureExpired, delta.Round(time.Second))
	}

	expected := []byte(SignWebhook(secret, timestamp, payload))
	// One signature today; a list separated by commas or spaces is accepted so
	// a future secret rotation can send two side by side.
	matched := false
	for _, candidate := range strings.FieldsFunc(signature, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if hmac.Equal([]byte(candidate), expected) {
			matched = true
		}
	}
	if !matched {
		return nil, ErrInvalidSignature
	}

	event := new(WebhookEvent)
	if err := json.Unmarshal(payload, event); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}
	return event, nil
}
