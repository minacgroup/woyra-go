package woyra

import (
	"context"
	"net/http"
	"net/url"
)

// EmailsService sends and inspects email.
type EmailsService struct{ c *Client }

// Send sends one email. An Idempotency-Key is generated unless
// WithIdempotencyKey is passed.
func (s *EmailsService) Send(ctx context.Context, params *SendEmailParams, opts ...RequestOption) (*Email, error) {
	out := new(Email)
	if err := s.c.do(ctx, http.MethodPost, "/v1/emails", nil, params, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Batch sends up to 100 emails, all or nothing, under one idempotency key.
func (s *EmailsService) Batch(ctx context.Context, emails []SendEmailParams, opts ...RequestOption) (*List[Email], error) {
	if emails == nil {
		emails = []SendEmailParams{}
	}
	out := new(List[Email])
	if err := s.c.do(ctx, http.MethodPost, "/v1/emails/batch", nil, emails, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Get retrieves an email, including every status it has had.
func (s *EmailsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Email, error) {
	out := new(Email)
	if err := s.c.do(ctx, http.MethodGet, "/v1/emails/"+url.PathEscape(id), nil, nil, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// List lists emails, newest first.
func (s *EmailsService) List(ctx context.Context, params *ListParams, opts ...RequestOption) (*List[Email], error) {
	out := new(List[Email])
	if err := s.c.do(ctx, http.MethodGet, "/v1/emails", listQuery(params), nil, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Cancel withdraws a scheduled email before it leaves.
func (s *EmailsService) Cancel(ctx context.Context, id string, opts ...RequestOption) (*Email, error) {
	out := new(Email)
	if err := s.c.do(ctx, http.MethodPost, "/v1/emails/"+url.PathEscape(id)+"/cancel", nil, nil, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}
