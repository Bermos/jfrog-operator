package jfrog

import (
	"context"
	"net/http"
	"strings"
)

// RepositoryConfig is the repository configuration JSON of the Artifactory
// repository configuration API. It is kept untyped because the accepted
// fields depend on the repository class and package type.
type RepositoryConfig map[string]any

// RepositorySummary is an entry of the repository list.
type RepositorySummary struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	PackageType string `json:"packageType"`
	URL         string `json:"url"`
}

// ListRepositories returns all repositories.
func (c *Client) ListRepositories(ctx context.Context) ([]RepositorySummary, error) {
	var out []RepositorySummary
	if err := c.do(ctx, http.MethodGet, c.artifactoryURL+"/api/repositories", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRepository returns the configuration of the repository with the given
// key. A missing repository yields an error for which IsNotFound is true.
func (c *Client) GetRepository(ctx context.Context, key string) (RepositoryConfig, error) {
	var out RepositoryConfig
	u := join(c.artifactoryURL+"/api/repositories", key)
	err := c.do(ctx, http.MethodGet, u, nil, &out)
	if err == nil {
		return out, nil
	}
	// Artifactory answers 400 rather than 404 for an unknown key. Tell that
	// apart from a genuinely bad request by looking at the repository list.
	if hasStatus(err, http.StatusBadRequest) {
		repos, listErr := c.ListRepositories(ctx)
		if listErr != nil {
			return nil, err
		}
		for _, r := range repos {
			if strings.EqualFold(r.Key, key) {
				return nil, err
			}
		}
		return nil, &APIError{Method: http.MethodGet, URL: u, StatusCode: http.StatusNotFound, Body: "repository does not exist"}
	}
	return nil, err
}

// CreateRepository creates a repository.
func (c *Client) CreateRepository(ctx context.Context, key string, cfg RepositoryConfig) error {
	return c.do(ctx, http.MethodPut, join(c.artifactoryURL+"/api/repositories", key), cfg, new(string))
}

// UpdateRepository updates a repository. Fields missing from cfg are left as they are.
func (c *Client) UpdateRepository(ctx context.Context, key string, cfg RepositoryConfig) error {
	return c.do(ctx, http.MethodPost, join(c.artifactoryURL+"/api/repositories", key), cfg, new(string))
}

// DeleteRepository deletes a repository and all artifacts in it.
func (c *Client) DeleteRepository(ctx context.Context, key string) error {
	return c.do(ctx, http.MethodDelete, join(c.artifactoryURL+"/api/repositories", key), nil, new(string))
}
