package jfrog

import (
	"context"
	"net/http"
)

// User is a user of the Artifactory security API.
type User struct {
	Name                     string   `json:"name,omitempty"`
	Email                    string   `json:"email,omitempty"`
	Password                 string   `json:"password,omitempty"`
	Admin                    *bool    `json:"admin,omitempty"`
	ProfileUpdatable         *bool    `json:"profileUpdatable,omitempty"`
	DisableUIAccess          *bool    `json:"disableUIAccess,omitempty"`
	InternalPasswordDisabled *bool    `json:"internalPasswordDisabled,omitempty"`
	Groups                   []string `json:"groups,omitempty"`
	Realm                    string   `json:"realm,omitempty"`
	LastLoggedIn             string   `json:"lastLoggedIn,omitempty"`
}

// GetUser returns the user with the given name.
func (c *Client) GetUser(ctx context.Context, name string) (*User, error) {
	var out User
	if err := c.do(ctx, http.MethodGet, join(c.artifactoryURL+"/api/security/users", name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateUser creates a user. Artifactory requires a password unless
// internalPasswordDisabled is set.
func (c *Client) CreateUser(ctx context.Context, u *User) error {
	return c.do(ctx, http.MethodPut, join(c.artifactoryURL+"/api/security/users", u.Name), u, new(string))
}

// UpdateUser updates a user. Fields left empty are not changed.
func (c *Client) UpdateUser(ctx context.Context, u *User) error {
	return c.do(ctx, http.MethodPost, join(c.artifactoryURL+"/api/security/users", u.Name), u, new(string))
}

// DeleteUser deletes a user.
func (c *Client) DeleteUser(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, join(c.artifactoryURL+"/api/security/users", name), nil, new(string))
}

// Group is a group of the Artifactory security API.
type Group struct {
	Name            string `json:"name,omitempty"`
	Description     string `json:"description,omitempty"`
	AutoJoin        *bool  `json:"autoJoin,omitempty"`
	AdminPrivileges *bool  `json:"adminPrivileges,omitempty"`
	Realm           string `json:"realm,omitempty"`
	RealmAttributes string `json:"realmAttributes,omitempty"`
}

// GetGroup returns the group with the given name.
func (c *Client) GetGroup(ctx context.Context, name string) (*Group, error) {
	var out Group
	if err := c.do(ctx, http.MethodGet, join(c.artifactoryURL+"/api/security/groups", name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateGroup creates a group.
func (c *Client) CreateGroup(ctx context.Context, g *Group) error {
	return c.do(ctx, http.MethodPut, join(c.artifactoryURL+"/api/security/groups", g.Name), g, new(string))
}

// UpdateGroup updates a group. Fields left empty are not changed.
func (c *Client) UpdateGroup(ctx context.Context, g *Group) error {
	return c.do(ctx, http.MethodPost, join(c.artifactoryURL+"/api/security/groups", g.Name), g, new(string))
}

// DeleteGroup deletes a group.
func (c *Client) DeleteGroup(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, join(c.artifactoryURL+"/api/security/groups", name), nil, new(string))
}

// PermissionTarget is a permission target of the Artifactory v2 security API.
type PermissionTarget struct {
	Name          string             `json:"name"`
	Repo          *PermissionSection `json:"repo,omitempty"`
	Build         *PermissionSection `json:"build,omitempty"`
	ReleaseBundle *PermissionSection `json:"releaseBundle,omitempty"`
}

// PermissionSection grants actions on a set of repositories (or builds or
// release bundles, which Artifactory also keeps in repositories).
type PermissionSection struct {
	IncludePatterns []string           `json:"include-patterns,omitempty"`
	ExcludePatterns []string           `json:"exclude-patterns,omitempty"`
	Repositories    []string           `json:"repositories"`
	Actions         *PermissionActions `json:"actions,omitempty"`
}

// PermissionActions maps user and group names to the actions granted to them.
type PermissionActions struct {
	Users  map[string][]string `json:"users,omitempty"`
	Groups map[string][]string `json:"groups,omitempty"`
}

// GetPermissionTarget returns the permission target with the given name.
func (c *Client) GetPermissionTarget(ctx context.Context, name string) (*PermissionTarget, error) {
	var out PermissionTarget
	if err := c.do(ctx, http.MethodGet, join(c.artifactoryURL+"/api/v2/security/permissions", name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreatePermissionTarget creates a permission target.
func (c *Client) CreatePermissionTarget(ctx context.Context, p *PermissionTarget) error {
	return c.do(ctx, http.MethodPost, join(c.artifactoryURL+"/api/v2/security/permissions", p.Name), p, new(string))
}

// UpdatePermissionTarget replaces a permission target.
func (c *Client) UpdatePermissionTarget(ctx context.Context, p *PermissionTarget) error {
	return c.do(ctx, http.MethodPut, join(c.artifactoryURL+"/api/v2/security/permissions", p.Name), p, new(string))
}

// DeletePermissionTarget deletes a permission target.
func (c *Client) DeletePermissionTarget(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, join(c.artifactoryURL+"/api/v2/security/permissions", name), nil, new(string))
}
