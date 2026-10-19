package woyra

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Reference vector produced by backend/internal/webhooks/signature.go's Sign.
const (
	vecSecret    = "whsec_test_secret"
	vecTimestamp = 1764547200
	vecBody      = `{"id":"evt_01K0TEST","type":"email.delivered","created_at":"2025-12-01T00:00:00Z","data":{"email_id":"email_01K0TEST"}}`
	vecSignature = "v1=f81611964c3b234625bec19c8c51e960aabced3721dc6b6698be5354fda35f8a"
)

// reference is signature.go's Sign, copied verbatim so the SDK is checked
// against the server's algorithm rather than against itself.
func reference(secret string, timestamp time.Time, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp.Unix(), 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func signed(sig string, ts int64) http.Header {
	h := http.Header{}
	h.Set("Woyra-Signature", sig)
	h.Set("Woyra-Timestamp", strconv.FormatInt(ts, 10))
	return h
}

var vecTime = time.Unix(vecTimestamp, 0)

func TestReferenceMatchesVector(t *testing.T) {
	eq(t, vecSignature, reference(vecSecret, vecTime, []byte(vecBody)), "reference")
	eq(t, vecSignature, SignWebhook(vecSecret, vecTime, []byte(vecBody)), "SignWebhook")
}

func TestVerifyAcceptsBackendVector(t *testing.T) {
	event, err := VerifyWebhook([]byte(vecBody), signed(vecSignature, vecTimestamp), vecSecret, VerifyNow(vecTime.Add(10*time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "evt_01K0TEST", event.ID, "id")
	eq(t, EventEmailDelivered, event.Type, "type")
	eq(t, "email_01K0TEST", event.Data["email_id"].(string), "data")
}

func TestVerifyAcceptsFreshSignatureViaClient(t *testing.T) {
	now := time.Now()
	body := []byte(`{"id":"evt_2","type":"email.clicked","created_at":"2026-10-10T08:00:00Z","data":{}}`)
	event, err := NewClient("wk_test").Webhooks.Verify(body, signed(reference(vecSecret, now, body), now.Unix()), vecSecret)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, EventEmailClicked, event.Type, "type")
}

func TestVerifyRejectsTamperedBody(t *testing.T) {
	_, err := VerifyWebhook([]byte(strings.Replace(vecBody, "delivered", "failed", 1)), signed(vecSignature, vecTimestamp), vecSecret, VerifyNow(vecTime))
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("want ErrInvalidSignature, got %v", err)
	}
}

func TestVerifyRejectsWrongSecretAndUnprefixed(t *testing.T) {
	if _, err := VerifyWebhook([]byte(vecBody), signed(vecSignature, vecTimestamp), "whsec_other", VerifyNow(vecTime)); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("wrong secret: %v", err)
	}
	if _, err := VerifyWebhook([]byte(vecBody), signed(vecSignature[3:], vecTimestamp), vecSecret, VerifyNow(vecTime)); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("unprefixed: %v", err)
	}
}

func TestVerifyRejectsStaleAndFutureTimestamps(t *testing.T) {
	h := signed(vecSignature, vecTimestamp)
	if _, err := VerifyWebhook([]byte(vecBody), h, vecSecret, VerifyNow(vecTime.Add(300*time.Second))); err != nil {
		t.Fatalf("at the edge of the window: %v", err)
	}
	for _, offset := range []time.Duration{301 * time.Second, -301 * time.Second} {
		if _, err := VerifyWebhook([]byte(vecBody), h, vecSecret, VerifyNow(vecTime.Add(offset))); !errors.Is(err, ErrSignatureExpired) {
			t.Fatalf("offset %s: want ErrSignatureExpired, got %v", offset, err)
		}
	}
	stale := time.Now().Add(-10 * time.Minute)
	if _, err := VerifyWebhook([]byte(vecBody), signed(reference(vecSecret, stale, []byte(vecBody)), stale.Unix()), vecSecret); !errors.Is(err, ErrSignatureExpired) {
		t.Fatalf("stale: %v", err)
	}
}

func TestVerifyRejectsMissingAndMalformed(t *testing.T) {
	if _, err := VerifyWebhook([]byte(vecBody), http.Header{}, vecSecret); !errors.Is(err, ErrMissingHeader) {
		t.Fatalf("missing: %v", err)
	}
	h := signed(vecSignature, 0)
	h.Set("Woyra-Timestamp", "soon")
	if _, err := VerifyWebhook([]byte(vecBody), h, vecSecret); !errors.Is(err, ErrInvalidTimestamp) {
		t.Fatalf("timestamp: %v", err)
	}
	if _, err := VerifyWebhook([]byte(vecBody), signed(vecSignature, vecTimestamp), ""); !errors.Is(err, ErrMissingSecret) {
		t.Fatalf("secret: %v", err)
	}
}

func TestVerifyAcceptsOneMatchAmongSeveral(t *testing.T) {
	sig := "v1=" + strings.Repeat("0", 64) + ", " + vecSignature
	if _, err := VerifyWebhook([]byte(vecBody), signed(sig, vecTimestamp), vecSecret, VerifyNow(vecTime)); err != nil {
		t.Fatal(err)
	}
}
