// Package jfrog is a small REST client for the parts of the JFrog Platform
// the operator manages.
package jfrog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config configures a Client.
type Config struct {
	// URL is the platform base URL, e.g. https://jfrog.example.com.
	URL string
	// ArtifactoryURL, AccessURL and XrayURL override the product URLs that
	// otherwise default to URL + "/artifactory", "/access" and "/xray".
	ArtifactoryURL string
	AccessURL      string
	XrayURL        string

	// AccessToken is sent as a bearer token. It takes precedence over basic auth.
	AccessToken string
	Username    string
	Password    string

	// HTTPClient is used for all requests. Defaults to a client with a 30s timeout.
	HTTPClient *http.Client
	// UserAgent is sent with every request.
	UserAgent string
}

// Client talks to Artifactory, Access and Xray of one JFrog Platform.
type Client struct {
	artifactoryURL string
	accessURL      string
	xrayURL        string
	cfg            Config
	http           *http.Client
}

// NewClient returns a client for the given configuration.
func NewClient(cfg Config) (*Client, error) {
	if cfg.URL == "" && (cfg.ArtifactoryURL == "" || cfg.AccessURL == "" || cfg.XrayURL == "") {
		return nil, errors.New("jfrog: URL is required")
	}
	if cfg.AccessToken == "" && (cfg.Username == "" || cfg.Password == "") {
		return nil, errors.New("jfrog: either an access token or a username and password are required")
	}
	base := strings.TrimRight(cfg.URL, "/")
	pick := func(override, suffix string) string {
		if override != "" {
			return strings.TrimRight(override, "/")
		}
		return base + suffix
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "jfrog-operator"
	}
	return &Client{
		artifactoryURL: pick(cfg.ArtifactoryURL, "/artifactory"),
		accessURL:      pick(cfg.AccessURL, "/access"),
		xrayURL:        pick(cfg.XrayURL, "/xray"),
		cfg:            cfg,
		http:           hc,
	}, nil
}

// ArtifactoryURL returns the Artifactory base URL.
func (c *Client) ArtifactoryURL() string { return c.artifactoryURL }

// CloseIdleConnections closes idle connections of the underlying HTTP client.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

// APIError is returned for any non-2xx response.
type APIError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	body := e.Body
	if len(body) > 512 {
		body = body[:512] + "..."
	}
	return fmt.Sprintf("jfrog: %s %s: %d %s: %s", e.Method, e.URL, e.StatusCode, http.StatusText(e.StatusCode), body)
}

// IsNotFound reports whether err is an API error with status 404.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// IsConflict reports whether err is an API error with status 409.
func IsConflict(err error) bool { return hasStatus(err, http.StatusConflict) }

func hasStatus(err error, code int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == code
}

// do sends a request. in is JSON encoded unless it is a string or nil, out is
// JSON decoded unless it is a *string or nil.
func (c *Client) do(ctx context.Context, method, rawURL string, in, out any) error {
	var body io.Reader
	contentType := ""
	switch v := in.(type) {
	case nil:
	case string:
		body = strings.NewReader(v)
		contentType = "text/plain"
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("jfrog: encoding request: %w", err)
		}
		body = bytes.NewReader(b)
		contentType = "application/json"
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	if c.cfg.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.AccessToken)
	} else {
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("jfrog: %s %s: %w", method, rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("jfrog: reading response of %s %s: %w", method, rawURL, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{Method: method, URL: rawURL, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	switch o := out.(type) {
	case nil:
		return nil
	case *string:
		*o = string(respBody)
		return nil
	default:
		if len(bytes.TrimSpace(respBody)) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("jfrog: decoding response of %s %s: %w", method, rawURL, err)
		}
		return nil
	}
}

func join(base string, segments ...string) string {
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
	}
	return base + "/" + strings.Join(escaped, "/")
}
