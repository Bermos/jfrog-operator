package jfrog

import (
	"context"
	"net/http"
)

// ArtifactoryVersion returns the Artifactory version. It doubles as an
// authenticated health check.
func (c *Client) ArtifactoryVersion(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, http.MethodGet, c.artifactoryURL+"/api/system/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// XrayVersion returns the Xray version.
func (c *Client) XrayVersion(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"xray_version"`
	}
	if err := c.do(ctx, http.MethodGet, c.xrayURL+"/api/v1/system/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}
