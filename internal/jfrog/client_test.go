package jfrog_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Bermos/jfrog-operator/internal/jfrog"
	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
)

func newClient(t *testing.T, srv *fake.Server) *jfrog.Client {
	t.Helper()
	c, err := jfrog.NewClient(jfrog.Config{URL: srv.URL, AccessToken: fake.Token})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewClientValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     jfrog.Config
		wantErr string
	}{
		{"missing url", jfrog.Config{AccessToken: "t"}, "URL is required"},
		{"missing credentials", jfrog.Config{URL: "https://x"}, "access token or a username and password"},
		{"username without password", jfrog.Config{URL: "https://x", Username: "u"}, "access token or a username and password"},
		{"token", jfrog.Config{URL: "https://x", AccessToken: "t"}, ""},
		{"basic", jfrog.Config{URL: "https://x", Username: "u", Password: "p"}, ""},
		{"product URLs only", jfrog.Config{ArtifactoryURL: "https://a", AccessURL: "https://b", XrayURL: "https://c", AccessToken: "t"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := jfrog.NewClient(tt.cfg)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestProductURLs(t *testing.T) {
	c, err := jfrog.NewClient(jfrog.Config{URL: "https://jfrog.example.com/", AccessToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ArtifactoryURL(); got != "https://jfrog.example.com/artifactory" {
		t.Fatalf("ArtifactoryURL = %q", got)
	}
	c, err = jfrog.NewClient(jfrog.Config{URL: "https://jfrog.example.com", ArtifactoryURL: "https://art.example.com/", AccessToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ArtifactoryURL(); got != "https://art.example.com" {
		t.Fatalf("ArtifactoryURL override = %q", got)
	}
}

func TestAuthentication(t *testing.T) {
	var gotAuth, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotUA = r.Header.Get("Authorization"), r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"version":"7.0.0"}`))
	}))
	defer srv.Close()
	ctx := context.Background()

	c, _ := jfrog.NewClient(jfrog.Config{URL: srv.URL, AccessToken: "tok", Username: "u", Password: "p"})
	if _, err := c.ArtifactoryVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("token auth header = %q", gotAuth)
	}
	if gotUA != "jfrog-operator" {
		t.Fatalf("user agent = %q", gotUA)
	}

	c, _ = jfrog.NewClient(jfrog.Config{URL: srv.URL, Username: "u", Password: "p"})
	if _, err := c.ArtifactoryVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("basic auth header = %q", gotAuth)
	}
}

func TestVersions(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()

	v, err := c.ArtifactoryVersion(ctx)
	if err != nil || v != "7.104.5" {
		t.Fatalf("ArtifactoryVersion = %q, %v", v, err)
	}
	x, err := c.XrayVersion(ctx)
	if err != nil || x != "3.111.0" {
		t.Fatalf("XrayVersion = %q, %v", x, err)
	}
}

func TestAPIErrors(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	ctx := context.Background()

	bad, _ := jfrog.NewClient(jfrog.Config{URL: srv.URL, AccessToken: "wrong"})
	_, err := bad.ArtifactoryVersion(ctx)
	var apiErr *jfrog.APIError
	if err == nil || !asAPIError(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 APIError, got %v", err)
	}
	if !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("error should include the response body: %v", err)
	}

	c := newClient(t, srv)
	srv.FailWith(http.MethodDelete, "/artifactory/api/repositories/x", http.StatusConflict)
	if err := c.DeleteRepository(ctx, "x"); !jfrog.IsConflict(err) {
		t.Fatalf("want conflict, got %v", err)
	}
	srv.FailWith(http.MethodDelete, "/artifactory/api/repositories/x", 0)
	if err := c.DeleteRepository(ctx, "x"); !jfrog.IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestRepositoryLifecycle(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()

	if _, err := c.GetRepository(ctx, "libs"); !jfrog.IsNotFound(err) {
		t.Fatalf("missing repository: want not found, got %v", err)
	}

	cfg := jfrog.RepositoryConfig{"key": "libs", "rclass": "local", "packageType": "maven", "description": "first"}
	if err := c.CreateRepository(ctx, "libs", cfg); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateRepository(ctx, "libs", cfg); err == nil {
		t.Fatal("creating an existing repository must fail")
	}

	got, err := c.GetRepository(ctx, "libs")
	if err != nil {
		t.Fatal(err)
	}
	if got["description"] != "first" || got["includesPattern"] != "**/*" {
		t.Fatalf("unexpected repository %v", got)
	}

	if err := c.UpdateRepository(ctx, "libs", jfrog.RepositoryConfig{"description": "second"}); err != nil {
		t.Fatal(err)
	}
	if got := srv.Repository("libs"); got["description"] != "second" || got["packageType"] != "maven" {
		t.Fatalf("update not applied as a partial update: %v", got)
	}

	list, err := c.ListRepositories(ctx)
	if err != nil || len(list) != 1 || list[0].Key != "libs" || list[0].Type != "LOCAL" {
		t.Fatalf("ListRepositories = %v, %v", list, err)
	}

	if err := c.DeleteRepository(ctx, "libs"); err != nil {
		t.Fatal(err)
	}
	if srv.Repository("libs") != nil {
		t.Fatal("repository still exists after delete")
	}
}

func TestGetRepositoryBadRequestForExistingKey(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()
	srv.SetRepository("libs", map[string]any{"rclass": "local", "packageType": "generic"})
	srv.FailWith(http.MethodGet, "/artifactory/api/repositories/libs", http.StatusBadRequest)

	_, err := c.GetRepository(ctx, "libs")
	if err == nil || jfrog.IsNotFound(err) {
		t.Fatalf("a 400 for an existing key must not be reported as not found, got %v", err)
	}
}

func TestRepositoryKeyIsEscaped(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c, _ := jfrog.NewClient(jfrog.Config{URL: srv.URL, AccessToken: "t"})
	if _, err := c.GetRepository(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/artifactory/api/repositories/a%2Fb" {
		t.Fatalf("path = %q", gotPath)
	}
}

func asAPIError(err error, target **jfrog.APIError) bool {
	return errors.As(err, target)
}
