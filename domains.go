package woyra

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// DomainsService manages sending domains.
type DomainsService struct{ c *Client }

// Create registers a sending domain and returns the DNS records to publish.
func (s *DomainsService) Create(ctx context.Context, domain string, opts ...RequestOption) (*Domain, error) {
	out := new(Domain)
	body := map[string]string{"domain": domain}
	if err := s.c.do(ctx, http.MethodPost, "/v1/domains", nil, body, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// DomainListParams pages the domain list. A Limit of 0 uses the API default.
type DomainListParams struct {
	Limit int
}

// List lists the account's domains. params may be nil.
func (s *DomainsService) List(ctx context.Context, params *DomainListParams, opts ...RequestOption) (*List[Domain], error) {
	q := url.Values{}
	if params != nil && params.Limit > 0 {
		q.Set("limit", strconv.Itoa(params.Limit))
	}
	out := new(List[Domain])
	if err := s.c.do(ctx, http.MethodGet, "/v1/domains", q, nil, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Verify re-checks the domain's DNS. Safe to call repeatedly.
func (s *DomainsService) Verify(ctx context.Context, id string, opts ...RequestOption) (*Domain, error) {
	out := new(Domain)
	if err := s.c.do(ctx, http.MethodPost, "/v1/domains/"+url.PathEscape(id)+"/verify", nil, nil, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Remove deletes a sending domain.
func (s *DomainsService) Remove(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.do(ctx, http.MethodDelete, "/v1/domains/"+url.PathEscape(id), nil, nil, nil, opts)
}
