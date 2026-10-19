// Package woyra is the official Go SDK for the Woyra email API.
//
//	client := woyra.NewClient(os.Getenv("WOYRA_API_KEY"))
//	email, err := client.Emails.Send(ctx, &woyra.SendEmailParams{
//		From:    "Woyra <noreply@yourdomain.com>",
//		To:      woyra.Recipients{"abebe@example.com"},
//		Subject: "Your code",
//		Text:    "Your code is 483920",
//	})
//
// Network errors, 429 and 5xx responses are retried with exponential backoff
// and jitter, honouring Retry-After. Sends carry an automatically generated
// Idempotency-Key that stays the same across retries, so a retried send never
// becomes a second email.
package woyra

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version is the SDK version, sent in the User-Agent.
const Version = "0.1.0"

// DefaultBaseURL is the canonical, country-unpinned API endpoint.
const DefaultBaseURL = "https://api.woyra.io"

// Client talks to the Woyra API. It is safe for concurrent use.
type Client struct {
	Emails    *EmailsService
	Templates *TemplatesService
	Domains   *DomainsService
	Webhooks  *WebhooksService

	apiKey         string
	baseURL        string
	httpClient     *http.Client
	maxRetries     int
	apiVersion     string
	retryBaseDelay time.Duration
	retryMaxDelay  time.Duration
	userAgent      string
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another endpoint, such as a country
// endpoint or a local mock. Defaults to WOYRA_BASE_URL, then DefaultBaseURL.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient sets the HTTP client. Its Timeout applies per attempt.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithTimeout sets the per-attempt timeout of the default HTTP client. Default 30s.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		h := *c.httpClient
		h.Timeout = d
		c.httpClient = &h
	}
}

// WithMaxRetries sets how many times a failed request is retried. Default 3.
func WithMaxRetries(n int) Option { return func(c *Client) { c.maxRetries = max(0, n) } }

// WithAPIVersion sends a Woyra-Version header on every request.
func WithAPIVersion(v string) Option { return func(c *Client) { c.apiVersion = v } }

// WithRetryDelay sets the first backoff delay and the ceiling on any one
// delay, including a Retry-After. Defaults 500ms and 30s.
func WithRetryDelay(base, ceiling time.Duration) Option {
	return func(c *Client) { c.retryBaseDelay, c.retryMaxDelay = base, ceiling }
}

// NewClient returns a client. An empty apiKey falls back to WOYRA_API_KEY.
func NewClient(apiKey string, opts ...Option) *Client {
	if apiKey == "" {
		apiKey = os.Getenv("WOYRA_API_KEY")
	}
	base := os.Getenv("WOYRA_BASE_URL")
	if base == "" {
		base = DefaultBaseURL
	}
	c := &Client{
		apiKey:         apiKey,
		baseURL:        strings.TrimRight(base, "/"),
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		maxRetries:     3,
		retryBaseDelay: 500 * time.Millisecond,
		retryMaxDelay:  30 * time.Second,
		userAgent:      "woyra-go/" + Version,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.Emails = &EmailsService{c}
	c.Templates = &TemplatesService{c}
	c.Domains = &DomainsService{c}
	c.Webhooks = &WebhooksService{c}
	return c
}

// RequestOption configures a single request.
type RequestOption func(*requestConfig)

type requestConfig struct {
	idempotencyKey string
	headers        http.Header
}

// WithIdempotencyKey overrides the automatically generated Idempotency-Key.
// Derive it from what the send is about, such as "order-4821-receipt".
func WithIdempotencyKey(key string) RequestOption {
	return func(r *requestConfig) { r.idempotencyKey = key }
}

// WithHeader adds a header to a single request.
func WithHeader(name, value string) RequestOption {
	return func(r *requestConfig) {
		if r.headers == nil {
			r.headers = http.Header{}
		}
		r.headers.Add(name, value)
	}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// parseRetryAfter reads Retry-After as seconds or an HTTP date.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil {
		return max(0, time.Duration(secs*float64(time.Second))), true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(0, t.Sub(now)), true
	}
	return 0, false
}

func (c *Client) backoff(attempt int) time.Duration {
	exp := min(c.retryMaxDelay, c.retryBaseDelay<<min(attempt, 30))
	if exp <= 0 {
		return 0
	}
	// Equal jitter: half fixed, half random, so concurrent clients spread out.
	return exp/2 + time.Duration(mrand.Int64N(int64(exp/2)+1))
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func listQuery(p *ListParams) url.Values {
	q := url.Values{}
	if p != nil {
		if p.Limit > 0 {
			q.Set("limit", strconv.Itoa(p.Limit))
		}
		if p.Cursor != "" {
			q.Set("cursor", p.Cursor)
		}
	}
	return q
}

// do sends one API request with retries, idempotency and typed errors, and
// decodes a successful response into out (when out is non-nil).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any, opts []RequestOption) error {
	if c.apiKey == "" {
		return &Error{Code: "authentication_required", Message: "missing API key: pass it to NewClient or set WOYRA_API_KEY"}
	}
	cfg := requestConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("woyra: encoding request: %w", err)
		}
	}
	// Generated once, before the first attempt, so every retry carries the
	// same key and a retried send can never become a second send.
	idempotencyKey := ""
	if method == http.MethodPost {
		idempotencyKey = cfg.idempotencyKey
		if idempotencyKey == "" {
			idempotencyKey = newUUID()
		}
	}

	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, reader)
		if err != nil {
			return fmt.Errorf("woyra: building request: %w", err)
		}
		for name, values := range cfg.headers {
			for _, v := range values {
				req.Header.Add(name, v)
			}
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("User-Agent", c.userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		if c.apiVersion != "" {
			req.Header.Set("Woyra-Version", c.apiVersion)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < c.maxRetries {
				if err := sleep(ctx, c.backoff(attempt)); err != nil {
					return err
				}
				continue
			}
			return &Error{Code: "connection_error", Message: fmt.Sprintf("could not reach Woyra at %s: %v", c.baseURL, err), Err: err}
		}
		raw, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		if readErr != nil && (resp.StatusCode < 300 || retryable) {
			// A response cut off mid-body is a network error.
			if attempt < c.maxRetries {
				if err := sleep(ctx, c.backoff(attempt)); err != nil {
					return err
				}
				continue
			}
			return &Error{StatusCode: resp.StatusCode, Code: "connection_error", Message: "reading response: " + readErr.Error(), Err: readErr}
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if out != nil && len(bytes.TrimSpace(raw)) > 0 {
				if err := json.Unmarshal(raw, out); err != nil {
					return fmt.Errorf("woyra: decoding response: %w", err)
				}
			}
			return nil
		}
		if retryable && attempt < c.maxRetries {
			delay, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
			if !ok {
				delay = c.backoff(attempt)
			}
			if err := sleep(ctx, min(delay, c.retryMaxDelay)); err != nil {
				return err
			}
			continue
		}
		return errorFromResponse(resp, raw)
	}
}

func errorFromResponse(resp *http.Response, raw []byte) *Error {
	e := &Error{StatusCode: resp.StatusCode, Code: "unknown_error", Message: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	var envelope struct {
		Error *struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			DocURL    string `json:"doc_url"`
			Param     string `json:"param"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Error != nil {
		if envelope.Error.Code != "" {
			e.Code = envelope.Error.Code
		}
		if envelope.Error.Message != "" {
			e.Message = envelope.Error.Message
		}
		e.Param = envelope.Error.Param
		e.RequestID = envelope.Error.RequestID
		e.DocURL = envelope.Error.DocURL
	}
	if e.RequestID == "" {
		e.RequestID = resp.Header.Get("Woyra-Request-Id")
	}
	return e
}
