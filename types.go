package woyra

import (
	"encoding/json"
	"time"
)

// Recipients is one address or a list of them. `Name <address>` is accepted.
//
// It is sent as a JSON string when it holds exactly one address and as an
// array otherwise, and it decodes from either form.
type Recipients []string

// MarshalJSON implements json.Marshaler.
func (r Recipients) MarshalJSON() ([]byte, error) {
	if len(r) == 1 {
		return json.Marshal(r[0])
	}
	return json.Marshal([]string(r))
}

// UnmarshalJSON implements json.Unmarshaler.
func (r *Recipients) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*r = Recipients{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*r = many
	return nil
}

// Bool returns a pointer to b, for optional boolean fields.
func Bool(b bool) *bool { return &b }

// String returns a pointer to s, for optional string fields.
func String(s string) *string { return &s }

// Time returns a pointer to t, for optional time fields.
func Time(t time.Time) *time.Time { return &t }

// Priority selects the dispatch queue.
const (
	PriorityOTP           = "otp"
	PriorityTransactional = "transactional"
	PriorityBulk          = "bulk"
)

// Attachment is a file sent with an email.
type Attachment struct {
	Filename string `json:"filename"`
	// Content is the file, base64 encoded.
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
	// ContentID makes the file referenceable from the HTML body as cid:<id>.
	ContentID string `json:"content_id,omitempty"`
}

// RoutingOptions says how Woyra should choose a route.
type RoutingOptions struct {
	Strategy   string  `json:"strategy,omitempty"`
	MaxCostUSD float64 `json:"max_cost_usd,omitempty"`
}

// SendEmailParams is the body of POST /v1/emails and one element of a batch.
//
// To, From and Subject are required for a fresh email. A reply (InReplyTo)
// defaults them, and a template send (TemplateID) takes Subject, HTML and Text
// from the template.
type SendEmailParams struct {
	From        string            `json:"from,omitempty"`
	To          Recipients        `json:"to,omitempty"`
	Cc          Recipients        `json:"cc,omitempty"`
	Bcc         Recipients        `json:"bcc,omitempty"`
	ReplyTo     Recipients        `json:"reply_to,omitempty"`
	Subject     string            `json:"subject,omitempty"`
	Text        string            `json:"text,omitempty"`
	HTML        string            `json:"html,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	ScheduledAt *time.Time        `json:"scheduled_at,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Priority    string            `json:"priority,omitempty"`
	Routing     *RoutingOptions   `json:"routing,omitempty"`
	WebhookURL  string            `json:"webhook_url,omitempty"`
	InReplyTo   string            `json:"in_reply_to,omitempty"`
	TemplateID  string            `json:"template_id,omitempty"`
	Variables   map[string]string `json:"variables,omitempty"`
	TrackOpens  *bool             `json:"track_opens,omitempty"`
	TrackClicks *bool             `json:"track_clicks,omitempty"`
}

// EmailEvent is one status an email has had, and when.
type EmailEvent struct {
	Status      string    `json:"status"`
	At          time.Time `json:"at"`
	Carrier     string    `json:"carrier,omitempty"`
	ErrorCode   string    `json:"error_code,omitempty"`
	ErrorDetail string    `json:"error_detail,omitempty"`
}

// EmailRecipient is one per-recipient copy of a multi-recipient send.
type EmailRecipient struct {
	To     string `json:"to"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

// RouteDecision says which carrier carried the email and why.
type RouteDecision struct {
	Carrier    string  `json:"carrier,omitempty"`
	Reason     string  `json:"reason,omitempty"`
	Score      float64 `json:"score,omitempty"`
	CostMicros int64   `json:"cost_micros,omitempty"`
}

// ErrorSummary is the canonical failure reason of an email.
type ErrorSummary struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Email is a sent (or scheduled) email.
type Email struct {
	Object      string            `json:"object"`
	ID          string            `json:"id"`
	Status      string            `json:"status"`
	To          string            `json:"to"`
	From        string            `json:"from"`
	Subject     string            `json:"subject"`
	CreatedAt   time.Time         `json:"created_at"`
	ScheduledAt *time.Time        `json:"scheduled_at,omitempty"`
	Recipients  []EmailRecipient  `json:"recipients,omitempty"`
	Events      []EmailEvent      `json:"events,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	ThreadID    string            `json:"thread_id,omitempty"`
	InReplyTo   string            `json:"in_reply_to,omitempty"`
	Route       *RouteDecision    `json:"route,omitempty"`
	Error       *ErrorSummary     `json:"error,omitempty"`
}

// List is one page of a list endpoint.
type List[T any] struct {
	Object     string `json:"object"`
	Data       []T    `json:"data"`
	HasMore    bool   `json:"has_more"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// ListParams pages through a list. Pass NextCursor back as Cursor.
type ListParams struct {
	Limit  int
	Cursor string
}

// Template is a stored email template.
type Template struct {
	Object  string `json:"object"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subject string `json:"subject"`
	HTML    string `json:"html,omitempty"`
	Text    string `json:"text,omitempty"`
	// Variables lists the {{name}} placeholders the template uses.
	Variables []string  `json:"variables"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateTemplateParams is the body of POST /v1/templates.
type CreateTemplateParams struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	HTML    string `json:"html,omitempty"`
	Text    string `json:"text,omitempty"`
}

// UpdateTemplateParams changes the fields that are set.
type UpdateTemplateParams struct {
	Name    *string `json:"name,omitempty"`
	Subject *string `json:"subject,omitempty"`
	HTML    *string `json:"html,omitempty"`
	Text    *string `json:"text,omitempty"`
}

// DNSRecord is one record to publish for a domain.
type DNSRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Purpose  string `json:"purpose,omitempty"`
	Required bool   `json:"required,omitempty"`
	Verified bool   `json:"verified,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Domain is a sending domain.
type Domain struct {
	Object              string      `json:"object"`
	ID                  string      `json:"id"`
	Domain              string      `json:"domain"`
	Status              string      `json:"status"`
	VerifiedAt          *time.Time  `json:"verified_at,omitempty"`
	Receiving           bool        `json:"receiving,omitempty"`
	ReceivingVerifiedAt *time.Time  `json:"receiving_verified_at,omitempty"`
	CreatedAt           time.Time   `json:"created_at"`
	Records             []DNSRecord `json:"records,omitempty"`
}

// Webhook event types.
const (
	EventEmailSent        = "email.sent"
	EventEmailDelivered   = "email.delivered"
	EventEmailUndelivered = "email.undelivered"
	EventEmailFailed      = "email.failed"
	EventEmailComplained  = "email.complained"
	EventEmailCanceled    = "email.canceled"
	EventEmailReceived    = "email.received"
	EventEmailOpened      = "email.opened"
	EventEmailClicked     = "email.clicked"
)

// WebhookEndpoint is a registered webhook destination.
type WebhookEndpoint struct {
	Object              string    `json:"object"`
	ID                  string    `json:"id"`
	URL                 string    `json:"url"`
	Events              []string  `json:"events"`
	Status              string    `json:"status"`
	ConsecutiveFailures int       `json:"consecutive_failures,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
	// Secret is present only in the response to creation. Store it.
	Secret string `json:"secret,omitempty"`
}

// CreateWebhookEndpointParams is the body of POST /v1/webhook_endpoints.
type CreateWebhookEndpointParams struct {
	URL string `json:"url"`
	// Events to send; empty means every one.
	Events []string `json:"events,omitempty"`
}

// WebhookEvent is a verified webhook delivery.
type WebhookEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	CreatedAt time.Time      `json:"created_at"`
	Data      map[string]any `json:"data"`
}
