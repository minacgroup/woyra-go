package woyra

import (
	"context"
	"net/http"
	"net/url"
)

// TemplatesService manages stored email templates.
type TemplatesService struct{ c *Client }

// Create creates a template. Its {{name}} placeholders become Variables.
func (s *TemplatesService) Create(ctx context.Context, params *CreateTemplateParams, opts ...RequestOption) (*Template, error) {
	out := new(Template)
	if err := s.c.do(ctx, http.MethodPost, "/v1/templates", nil, params, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Get retrieves a template.
func (s *TemplatesService) Get(ctx context.Context, id string, opts ...RequestOption) (*Template, error) {
	out := new(Template)
	if err := s.c.do(ctx, http.MethodGet, "/v1/templates/"+url.PathEscape(id), nil, nil, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// List lists templates.
func (s *TemplatesService) List(ctx context.Context, params *ListParams, opts ...RequestOption) (*List[Template], error) {
	out := new(List[Template])
	if err := s.c.do(ctx, http.MethodGet, "/v1/templates", listQuery(params), nil, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Update changes the fields of params that are set.
func (s *TemplatesService) Update(ctx context.Context, id string, params *UpdateTemplateParams, opts ...RequestOption) (*Template, error) {
	if params == nil {
		params = &UpdateTemplateParams{}
	}
	out := new(Template)
	if err := s.c.do(ctx, http.MethodPatch, "/v1/templates/"+url.PathEscape(id), nil, params, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// Remove deletes a template.
func (s *TemplatesService) Remove(ctx context.Context, id string, opts ...RequestOption) error {
	return s.c.do(ctx, http.MethodDelete, "/v1/templates/"+url.PathEscape(id), nil, nil, nil, opts)
}
