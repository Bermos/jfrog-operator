package jfrog

import (
	"context"
	"net/http"
)

// XrayPolicy is an Xray policy as accepted by the Xray v2 policies API. It is
// kept untyped because rule criteria and actions depend on the policy type.
type XrayPolicy map[string]any

// XrayWatch is an Xray watch as accepted by the Xray v2 watches API.
type XrayWatch map[string]any

// GetXrayPolicy returns the policy with the given name.
func (c *Client) GetXrayPolicy(ctx context.Context, name string) (XrayPolicy, error) {
	var out XrayPolicy
	if err := c.do(ctx, http.MethodGet, join(c.xrayURL+"/api/v2/policies", name), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateXrayPolicy creates a policy.
func (c *Client) CreateXrayPolicy(ctx context.Context, p XrayPolicy) error {
	return c.do(ctx, http.MethodPost, c.xrayURL+"/api/v2/policies", p, new(string))
}

// UpdateXrayPolicy replaces the policy with the given name.
func (c *Client) UpdateXrayPolicy(ctx context.Context, name string, p XrayPolicy) error {
	return c.do(ctx, http.MethodPut, join(c.xrayURL+"/api/v2/policies", name), p, new(string))
}

// DeleteXrayPolicy deletes a policy.
func (c *Client) DeleteXrayPolicy(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, join(c.xrayURL+"/api/v2/policies", name), nil, new(string))
}

// GetXrayWatch returns the watch with the given name.
func (c *Client) GetXrayWatch(ctx context.Context, name string) (XrayWatch, error) {
	var out XrayWatch
	if err := c.do(ctx, http.MethodGet, join(c.xrayURL+"/api/v2/watches", name), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateXrayWatch creates a watch.
func (c *Client) CreateXrayWatch(ctx context.Context, w XrayWatch) error {
	return c.do(ctx, http.MethodPost, c.xrayURL+"/api/v2/watches", w, new(string))
}

// UpdateXrayWatch replaces the watch with the given name.
func (c *Client) UpdateXrayWatch(ctx context.Context, name string, w XrayWatch) error {
	return c.do(ctx, http.MethodPut, join(c.xrayURL+"/api/v2/watches", name), w, new(string))
}

// DeleteXrayWatch deletes a watch.
func (c *Client) DeleteXrayWatch(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, join(c.xrayURL+"/api/v2/watches", name), nil, new(string))
}
