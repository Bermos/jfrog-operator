package jfrog_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Bermos/jfrog-operator/internal/jfrog"
	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
)

func TestUserLifecycle(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()

	if _, err := c.GetUser(ctx, "alice"); !jfrog.IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
	if err := c.CreateUser(ctx, &jfrog.User{Name: "alice", Email: "a@example.com"}); err == nil {
		t.Fatal("creating a user without password must fail")
	}
	if err := c.CreateUser(ctx, &jfrog.User{Name: "alice", Email: "a@example.com", Password: "pw", Groups: []string{"devs"}}); err != nil {
		t.Fatal(err)
	}
	if srv.CountRequests(http.MethodPut, "/artifactory/api/security/users/alice") != 2 {
		t.Fatal("users must be created with PUT")
	}
	u, err := c.GetUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "a@example.com" || u.Password != "" || u.Realm != "internal" || len(u.Groups) != 1 {
		t.Fatalf("unexpected user %+v", u)
	}
	admin := true
	if err := c.UpdateUser(ctx, &jfrog.User{Name: "alice", Admin: &admin}); err != nil {
		t.Fatal(err)
	}
	if srv.CountRequests(http.MethodPost, "/artifactory/api/security/users/alice") != 1 {
		t.Fatal("users must be updated with POST")
	}
	if got := srv.Object(fake.Users, "alice"); got["admin"] != true || got["email"] != "a@example.com" {
		t.Fatalf("update not merged: %v", got)
	}
	if err := c.DeleteUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteUser(ctx, "alice"); !jfrog.IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestGroupLifecycle(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()

	if err := c.CreateGroup(ctx, &jfrog.Group{Name: "devs", Description: "Developers"}); err != nil {
		t.Fatal(err)
	}
	g, err := c.GetGroup(ctx, "devs")
	if err != nil || g.Description != "Developers" || g.AutoJoin == nil || *g.AutoJoin {
		t.Fatalf("GetGroup = %+v, %v", g, err)
	}
	if err := c.UpdateGroup(ctx, &jfrog.Group{Name: "devs", Description: "All developers"}); err != nil {
		t.Fatal(err)
	}
	if got := srv.Object(fake.Groups, "devs"); got["description"] != "All developers" {
		t.Fatalf("update not applied: %v", got)
	}
	if err := c.DeleteGroup(ctx, "devs"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetGroup(ctx, "devs"); !jfrog.IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestPermissionTargetLifecycle(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()

	pt := &jfrog.PermissionTarget{Name: "team", Repo: &jfrog.PermissionSection{
		Repositories:    []string{"libs"},
		IncludePatterns: []string{"**"},
		Actions:         &jfrog.PermissionActions{Users: map[string][]string{"alice": {"read"}}},
	}}
	if err := c.CreatePermissionTarget(ctx, pt); err != nil {
		t.Fatal(err)
	}
	if err := c.CreatePermissionTarget(ctx, pt); !jfrog.IsConflict(err) {
		t.Fatalf("want conflict on duplicate create, got %v", err)
	}
	if srv.CountRequests(http.MethodPost, "/artifactory/api/v2/security/permissions/team") != 2 {
		t.Fatal("permission targets must be created with POST")
	}
	got, err := c.GetPermissionTarget(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo == nil || got.Repo.IncludePatterns[0] != "**" || got.Repo.Actions.Users["alice"][0] != "read" {
		t.Fatalf("unexpected permission target %+v", got)
	}

	// Updates replace the whole target.
	if err := c.UpdatePermissionTarget(ctx, &jfrog.PermissionTarget{Name: "team", Build: &jfrog.PermissionSection{Repositories: []string{"artifactory-build-info"}}}); err != nil {
		t.Fatal(err)
	}
	got, err = c.GetPermissionTarget(ctx, "team")
	if err != nil || got.Repo != nil || got.Build == nil {
		t.Fatalf("update did not replace: %+v, %v", got, err)
	}
	if err := c.DeletePermissionTarget(ctx, "team"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetPermissionTarget(ctx, "team"); !jfrog.IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}
