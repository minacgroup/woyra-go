package woyra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type recorded struct {
	Method string
	Path   string
	Header http.Header
	Raw    string
	Body   any
}

type response struct {
	status  int // 0 drops the connection
	body    any
	headers map[string]string
}

// mock is a local stand-in for the Woyra API: tests queue responses and
// inspect the requests it received.
type mock struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	queue    []response
	requests []recorded
}

func newMock(t *testing.T) *mock {
	t.Helper()
	m := &mock{t: t}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &body)
		}
		m.mu.Lock()
		m.requests = append(m.requests, recorded{r.Method, r.URL.RequestURI(), r.Header.Clone(), string(raw), body})
		next := response{status: 200, body: map[string]any{}}
		if len(m.queue) > 0 {
			next, m.queue = m.queue[0], m.queue[1:]
		}
		m.mu.Unlock()
		if next.status == 0 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		for k, v := range next.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(next.status)
		if next.body != nil {
			_ = json.NewEncoder(w).Encode(next.body)
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mock) reply(status int, body any, headers ...string) {
	h := map[string]string{}
	for i := 0; i+1 < len(headers); i += 2 {
		h[headers[i]] = headers[i+1]
	}
	m.mu.Lock()
	m.queue = append(m.queue, response{status, body, h})
	m.mu.Unlock()
}

func (m *mock) got() []recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recorded(nil), m.requests...)
}

func (m *mock) calls() []string {
	var out []string
	for _, r := range m.got() {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

func (m *mock) client(opts ...Option) *Client {
	base := []Option{WithBaseURL(m.server.URL), WithRetryDelay(time.Millisecond, 5*time.Millisecond)}
	return NewClient("wk_test_123", append(base, opts...)...)
}

func email(over ...string) map[string]any {
	e := map[string]any{
		"object": "email", "id": "email_01K0TEST", "status": "accepted", "to": "abebe@example.com",
		"from": "noreply@yourdomain.com", "subject": "Hi", "created_at": "2026-10-10T08:00:00Z",
	}
	for i := 0; i+1 < len(over); i += 2 {
		e[over[i]] = over[i+1]
	}
	return e
}

func apiError(code, message string, extra ...string) map[string]any {
	e := map[string]any{"code": code, "message": message, "doc_url": "https://docs.woyra.io/errors/" + code, "request_id": "req_123"}
	for i := 0; i+1 < len(extra); i += 2 {
		e[extra[i]] = extra[i+1]
	}
	return map[string]any{"error": e}
}

func eq[T comparable](t *testing.T, want, got T, what string) {
	t.Helper()
	if want != got {
		t.Fatalf("%s: want %v, got %v", what, want, got)
	}
}

func deepEq(t *testing.T, want, got any, what string) {
	t.Helper()
	w, _ := json.Marshal(want)
	g, _ := json.Marshal(got)
	if string(w) != string(g) {
		t.Fatalf("%s:\nwant %s\n got %s", what, w, g)
	}
}

var ctx = context.Background()

func TestAuthUserAgentAndJSON(t *testing.T) {
	m := newMock(t)
	m.reply(202, email())
	sent, err := m.client().Emails.Send(ctx, &SendEmailParams{From: "a@yourdomain.com", To: Recipients{"abebe@example.com"}, Subject: "Hi", Text: "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "email_01K0TEST", sent.ID, "id")
	r := m.got()[0]
	eq(t, "POST /v1/emails", r.Method+" "+r.Path, "call")
	eq(t, "Bearer wk_test_123", r.Header.Get("Authorization"), "authorization")
	eq(t, "woyra-go/"+Version, r.Header.Get("User-Agent"), "user agent")
	eq(t, "application/json", r.Header.Get("Content-Type"), "content type")
}

func TestEnvironmentDefaults(t *testing.T) {
	m := newMock(t)
	t.Setenv("WOYRA_API_KEY", "wk_test_env")
	t.Setenv("WOYRA_BASE_URL", m.server.URL)
	m.reply(200, map[string]any{"object": "list", "data": []any{}, "has_more": false})
	if _, err := NewClient("").Emails.List(ctx, nil); err != nil {
		t.Fatal(err)
	}
	eq(t, "Bearer wk_test_env", m.got()[0].Header.Get("Authorization"), "authorization")
}

func TestMissingAPIKey(t *testing.T) {
	t.Setenv("WOYRA_API_KEY", "")
	_, err := NewClient("").Emails.Get(ctx, "email_1")
	var e *Error
	if !errors.As(err, &e) || e.Code != "authentication_required" {
		t.Fatalf("want authentication_required, got %v", err)
	}
}

func TestIdempotencyKeyGeneratedAndReusedAcrossRetried500(t *testing.T) {
	m := newMock(t)
	m.reply(500, apiError("internal_error", "boom"))
	m.reply(202, email())
	if _, err := m.client().Emails.Send(ctx, &SendEmailParams{To: Recipients{"a@example.com"}}); err != nil {
		t.Fatal(err)
	}
	reqs := m.got()
	eq(t, 2, len(reqs), "attempts")
	key := reqs[0].Header.Get("Idempotency-Key")
	if !uuidPattern.MatchString(key) {
		t.Fatalf("not a UUID v4: %q", key)
	}
	eq(t, key, reqs[1].Header.Get("Idempotency-Key"), "retried key")
}

func TestFreshKeyPerSend(t *testing.T) {
	m := newMock(t)
	m.reply(202, email())
	m.reply(202, email())
	c := m.client()
	_, _ = c.Emails.Send(ctx, &SendEmailParams{To: Recipients{"a@example.com"}})
	_, _ = c.Emails.Send(ctx, &SendEmailParams{To: Recipients{"a@example.com"}})
	reqs := m.got()
	if reqs[0].Header.Get("Idempotency-Key") == reqs[1].Header.Get("Idempotency-Key") {
		t.Fatal("two sends shared one idempotency key")
	}
}

func TestCallerSuppliedIdempotencyKey(t *testing.T) {
	m := newMock(t)
	m.reply(503, apiError("route_unavailable", "later"))
	m.reply(202, map[string]any{"object": "list", "data": []any{email()}})
	if _, err := m.client().Emails.Batch(ctx, []SendEmailParams{{To: Recipients{"a@example.com"}}}, WithIdempotencyKey("digest-2026-10-09")); err != nil {
		t.Fatal(err)
	}
	for _, r := range m.got() {
		eq(t, "digest-2026-10-09", r.Header.Get("Idempotency-Key"), "key")
	}
}

func TestRetries429And5xxThenSucceeds(t *testing.T) {
	m := newMock(t)
	m.reply(429, apiError("rate_limited", "slow down"), "Retry-After", "0")
	m.reply(502, nil)
	m.reply(503, apiError("route_unavailable", "later"))
	m.reply(202, email())
	sent, err := m.client().Emails.Send(ctx, &SendEmailParams{To: Recipients{"a@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "email_01K0TEST", sent.ID, "id")
	eq(t, 4, len(m.got()), "attempts")
}

func TestRetriesNetworkErrorsWithSameKey(t *testing.T) {
	m := newMock(t)
	m.reply(0, nil) // drop the connection
	m.reply(202, email())
	if _, err := m.client().Emails.Send(ctx, &SendEmailParams{To: Recipients{"a@example.com"}}); err != nil {
		t.Fatal(err)
	}
	reqs := m.got()
	eq(t, 2, len(reqs), "attempts")
	eq(t, reqs[0].Header.Get("Idempotency-Key"), reqs[1].Header.Get("Idempotency-Key"), "key")
}

func TestGivesUpAfterThreeRetries(t *testing.T) {
	m := newMock(t)
	for i := range 5 {
		m.reply(500, apiError("internal_error", fmt.Sprintf("boom %d", i)))
	}
	_, err := m.client().Emails.Get(ctx, "email_1")
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %v", err)
	}
	eq(t, 500, e.StatusCode, "status")
	eq(t, "boom 3", e.Message, "message")
	eq(t, 4, len(m.got()), "attempts")
}

func TestConnectionError(t *testing.T) {
	c := NewClient("wk_test_123", WithBaseURL("http://127.0.0.1:1"), WithMaxRetries(1), WithRetryDelay(time.Millisecond, time.Millisecond))
	_, err := c.Emails.Get(ctx, "email_1")
	var e *Error
	if !errors.As(err, &e) || e.Code != "connection_error" || e.StatusCode != 0 || e.Err == nil {
		t.Fatalf("want connection_error, got %#v", err)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	m := newMock(t)
	for range 5 {
		m.reply(503, apiError("route_unavailable", "later"), "Retry-After", "10")
	}
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err := m.client(WithRetryDelay(time.Millisecond, time.Minute)).Emails.Get(cctx, "email_1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline exceeded, got %v", err)
	}
	eq(t, 1, len(m.got()), "attempts")
}

func TestNoRetryOn400AndTypedErrorFields(t *testing.T) {
	m := newMock(t)
	m.reply(400, apiError("invalid_request", `"nope" is not a valid email address.`, "param", "to"))
	m.reply(202, email())
	_, err := m.client().Emails.Send(ctx, &SendEmailParams{To: Recipients{"nope"}})
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *Error, got %v", err)
	}
	eq(t, 400, e.StatusCode, "status")
	eq(t, "invalid_request", e.Code, "code")
	eq(t, `"nope" is not a valid email address.`, e.Message, "message")
	eq(t, "to", e.Param, "param")
	eq(t, "req_123", e.RequestID, "request id")
	eq(t, "https://docs.woyra.io/errors/invalid_request", e.DocURL, "doc url")
	eq(t, 1, len(m.got()), "attempts")
}

func TestNoRetryOnOther4xx(t *testing.T) {
	for _, status := range []int{401, 402, 403, 404, 409} {
		m := newMock(t)
		m.reply(status, apiError("x", "no"))
		_, err := m.client().Emails.Get(ctx, "email_1")
		var e *Error
		if !errors.As(err, &e) || e.StatusCode != status {
			t.Fatalf("status %d: got %v", status, err)
		}
		eq(t, 1, len(m.got()), fmt.Sprintf("attempts for %d", status))
	}
}

func TestNonJSONErrorUsesRequestIDHeader(t *testing.T) {
	m := newMock(t)
	m.reply(404, nil, "Woyra-Request-Id", "req_hdr")
	_, err := m.client().Emails.Get(ctx, "email_1")
	var e *Error
	if !errors.As(err, &e) {
		t.Fatal(err)
	}
	eq(t, "unknown_error", e.Code, "code")
	eq(t, "req_hdr", e.RequestID, "request id")
}

func TestParseRetryAfter(t *testing.T) {
	d, ok := parseRetryAfter("2", time.Now())
	eq(t, true, ok, "ok")
	eq(t, 2*time.Second, d, "seconds")
	_, ok = parseRetryAfter("", time.Now())
	eq(t, false, ok, "empty")
	d, _ = parseRetryAfter("Thu, 01 Jan 1970 00:00:10 GMT", time.Unix(7, 0))
	eq(t, 3*time.Second, d, "date")
}

func TestRecipientsStringOrArray(t *testing.T) {
	m := newMock(t)
	m.reply(202, email())
	_, err := m.client().Emails.Send(ctx, &SendEmailParams{
		From:    "Woyra <noreply@yourdomain.com>",
		To:      Recipients{"abebe@example.com"},
		Cc:      Recipients{"a@example.com", "b@example.com"},
		Bcc:     Recipients{"audit@yourdomain.com"},
		ReplyTo: Recipients{"support@yourdomain.com", "help@yourdomain.com"},
		Subject: "Hi",
		HTML:    "<p>Hi</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := m.got()[0].Body.(map[string]any)
	deepEq(t, "abebe@example.com", b["to"], "to")
	deepEq(t, []string{"a@example.com", "b@example.com"}, b["cc"], "cc")
	deepEq(t, "audit@yourdomain.com", b["bcc"], "bcc")
	deepEq(t, []string{"support@yourdomain.com", "help@yourdomain.com"}, b["reply_to"], "reply_to")

	var r Recipients
	_ = json.Unmarshal([]byte(`"x@example.com"`), &r)
	deepEq(t, Recipients{"x@example.com"}, r, "decode string")
	_ = json.Unmarshal([]byte(`["x@example.com","y@example.com"]`), &r)
	deepEq(t, Recipients{"x@example.com", "y@example.com"}, r, "decode array")
}

func TestTemplateSendWithTracking(t *testing.T) {
	m := newMock(t)
	m.reply(202, email())
	_, err := m.client().Emails.Send(ctx, &SendEmailParams{
		From:        "noreply@yourdomain.com",
		To:          Recipients{"abebe@example.com"},
		TemplateID:  "tmpl_123",
		Variables:   map[string]string{"name": "Abebe"},
		TrackOpens:  Bool(true),
		TrackClicks: Bool(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, `{"from":"noreply@yourdomain.com","to":"abebe@example.com","template_id":"tmpl_123","variables":{"name":"Abebe"},"track_opens":true,"track_clicks":false}`, m.got()[0].Raw, "body")
}

func TestScheduleAndCancel(t *testing.T) {
	m := newMock(t)
	m.reply(202, email("status", "scheduled"))
	m.reply(200, email("status", "canceled"))
	c := m.client()
	sent, err := c.Emails.Send(ctx, &SendEmailParams{To: Recipients{"a@example.com"}, ScheduledAt: Time(time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC))})
	if err != nil {
		t.Fatal(err)
	}
	canceled, err := c.Emails.Cancel(ctx, sent.ID)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "2026-10-11T08:00:00Z", m.got()[0].Body.(map[string]any)["scheduled_at"].(string), "scheduled_at")
	eq(t, "POST /v1/emails/email_01K0TEST/cancel", m.calls()[1], "cancel call")
	eq(t, "canceled", canceled.Status, "status")
}

func TestBatchPostsAnArray(t *testing.T) {
	m := newMock(t)
	m.reply(202, map[string]any{"object": "list", "data": []any{email(), email("id", "email_2")}})
	res, err := m.client().Emails.Batch(ctx, []SendEmailParams{{To: Recipients{"a@example.com"}}, {To: Recipients{"b@example.com", "c@example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	r := m.got()[0]
	eq(t, "/v1/emails/batch", r.Path, "path")
	arr, ok := r.Body.([]any)
	if !ok {
		t.Fatalf("batch body is not an array: %s", r.Raw)
	}
	deepEq(t, []string{"b@example.com", "c@example.com"}, arr[1].(map[string]any)["to"], "second to")
	if !uuidPattern.MatchString(r.Header.Get("Idempotency-Key")) {
		t.Fatal("batch has no generated idempotency key")
	}
	eq(t, 2, len(res.Data), "data")
}

func TestGetAndList(t *testing.T) {
	m := newMock(t)
	m.reply(200, email())
	m.reply(200, map[string]any{"object": "list", "data": []any{}, "has_more": false})
	c := m.client()
	if _, err := c.Emails.Get(ctx, "email/../x"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Emails.List(ctx, &ListParams{Limit: 10, Cursor: "email_9"}); err != nil {
		t.Fatal(err)
	}
	reqs := m.got()
	eq(t, "/v1/emails/email%2F..%2Fx", reqs[0].Path, "escaped id")
	eq(t, "", reqs[0].Header.Get("Idempotency-Key"), "GET key")
	eq(t, "/v1/emails?cursor=email_9&limit=10", reqs[1].Path, "list query")
}

func TestTemplates(t *testing.T) {
	m := newMock(t)
	tmpl := map[string]any{"object": "template", "id": "tmpl_1", "name": "welcome", "subject": "Hi {{name}}", "variables": []string{"name"},
		"created_at": "2026-10-10T08:00:00Z", "updated_at": "2026-10-10T08:00:00Z"}
	m.reply(201, tmpl)
	m.reply(200, tmpl)
	m.reply(200, map[string]any{"object": "list", "data": []any{tmpl}, "has_more": false})
	m.reply(200, tmpl)
	m.reply(204, nil)
	c := m.client()
	created, err := c.Templates.Create(ctx, &CreateTemplateParams{Name: "welcome", Subject: "Hi {{name}}", HTML: "<p>Hi {{name}}</p>"})
	if err != nil {
		t.Fatal(err)
	}
	deepEq(t, []string{"name"}, created.Variables, "variables")
	_, _ = c.Templates.Get(ctx, "tmpl_1")
	_, _ = c.Templates.List(ctx, &ListParams{Limit: 5})
	_, _ = c.Templates.Update(ctx, "tmpl_1", &UpdateTemplateParams{Subject: String("Hello {{name}}")})
	if err := c.Templates.Remove(ctx, "tmpl_1"); err != nil {
		t.Fatal(err)
	}
	deepEq(t, []string{"POST /v1/templates", "GET /v1/templates/tmpl_1", "GET /v1/templates?limit=5", "PATCH /v1/templates/tmpl_1", "DELETE /v1/templates/tmpl_1"}, m.calls(), "calls")
	eq(t, `{"subject":"Hello {{name}}"}`, m.got()[3].Raw, "update body")
}

func TestDomains(t *testing.T) {
	m := newMock(t)
	m.reply(201, map[string]any{"object": "domain", "id": "dom_1", "status": "pending", "created_at": "2026-10-10T08:00:00Z"})
	m.reply(200, map[string]any{"object": "list", "data": []any{}})
	m.reply(200, map[string]any{"object": "domain", "id": "dom_1", "status": "verified", "created_at": "2026-10-10T08:00:00Z"})
	m.reply(204, nil)
	c := m.client()
	if _, err := c.Domains.Create(ctx, "example.com"); err != nil {
		t.Fatal(err)
	}
	_, _ = c.Domains.List(ctx, nil)
	d, err := c.Domains.Verify(ctx, "dom_1")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Domains.Remove(ctx, "dom_1")
	eq(t, "verified", d.Status, "status")
	eq(t, `{"domain":"example.com"}`, m.got()[0].Raw, "create body")
	deepEq(t, []string{"POST /v1/domains", "GET /v1/domains", "POST /v1/domains/dom_1/verify", "DELETE /v1/domains/dom_1"}, m.calls(), "calls")
}

func TestWebhookEndpoints(t *testing.T) {
	m := newMock(t)
	m.reply(201, map[string]any{"object": "webhook_endpoint", "id": "we_1", "secret": "whsec_1", "created_at": "2026-10-10T08:00:00Z"})
	m.reply(200, map[string]any{"object": "list", "data": []any{}, "has_more": false})
	m.reply(204, nil)
	c := m.client()
	ep, err := c.Webhooks.Create(ctx, &CreateWebhookEndpointParams{URL: "https://x.test/hook", Events: []string{EventEmailOpened, EventEmailClicked}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Webhooks.List(ctx, &ListParams{Cursor: "we_0"})
	_ = c.Webhooks.Remove(ctx, "we_1")
	eq(t, "whsec_1", ep.Secret, "secret")
	eq(t, `{"url":"https://x.test/hook","events":["email.opened","email.clicked"]}`, m.got()[0].Raw, "create body")
	deepEq(t, []string{"POST /v1/webhook_endpoints", "GET /v1/webhook_endpoints?cursor=we_0", "DELETE /v1/webhook_endpoints/we_1"}, m.calls(), "calls")
}

func TestErrorString(t *testing.T) {
	e := &Error{StatusCode: 400, Code: "invalid_request", Message: "bad", Param: "to", RequestID: "req_1"}
	if !strings.Contains(e.Error(), "invalid_request") || !strings.Contains(e.Error(), "req_1") {
		t.Fatal(e.Error())
	}
}
