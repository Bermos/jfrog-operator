package jfrog_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Bermos/jfrog-operator/internal/jfrog"
	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
)

func TestXrayPolicyLifecycle(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()

	if _, err := c.GetXrayPolicy(ctx, "sec"); !jfrog.IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
	p := jfrog.XrayPolicy{"name": "sec", "type": "security", "rules": []any{map[string]any{"name": "r", "priority": 1}}}
	if err := c.CreateXrayPolicy(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateXrayPolicy(ctx, p); !jfrog.IsConflict(err) {
		t.Fatalf("want conflict, got %v", err)
	}
	if srv.CountRequests(http.MethodPost, "/xray/api/v2/policies") != 2 {
		t.Fatal("policies must be created with POST on the collection")
	}
	got, err := c.GetXrayPolicy(ctx, "sec")
	if err != nil || got["type"] != "security" {
		t.Fatalf("GetXrayPolicy = %v, %v", got, err)
	}
	if err := c.UpdateXrayPolicy(ctx, "sec", jfrog.XrayPolicy{"name": "sec", "type": "security", "description": "d"}); err != nil {
		t.Fatal(err)
	}
	if got := srv.Object(fake.XrayPolicies, "sec"); got["description"] != "d" || got["rules"] != nil {
		t.Fatalf("update must replace the policy: %v", got)
	}
	if err := c.DeleteXrayPolicy(ctx, "sec"); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteXrayPolicy(ctx, "sec"); !jfrog.IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestXrayWatchLifecycle(t *testing.T) {
	srv := fake.NewServer()
	defer srv.Close()
	c := newClient(t, srv)
	ctx := context.Background()

	w := jfrog.XrayWatch{
		"general_data":      map[string]any{"name": "w", "active": true},
		"project_resources": map[string]any{"resources": []any{map[string]any{"type": "repository", "name": "libs", "bin_mgr_id": "default"}}},
		"assigned_policies": []any{map[string]any{"name": "sec", "type": "security"}},
	}
	if err := c.CreateXrayWatch(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetXrayWatch(ctx, "w")
	if err != nil {
		t.Fatal(err)
	}
	res := got["project_resources"].(map[string]any)["resources"].([]any)[0].(map[string]any)
	if res["name"] != "libs" || res["repo_type"] != "local" {
		t.Fatalf("unexpected watch %v", got)
	}
	w["general_data"] = map[string]any{"name": "w", "active": false}
	if err := c.UpdateXrayWatch(ctx, "w", w); err != nil {
		t.Fatal(err)
	}
	if a := srv.Object(fake.XrayWatches, "w")["general_data"].(map[string]any)["active"]; a != false {
		t.Fatalf("update not applied: %v", a)
	}
	if err := c.DeleteXrayWatch(ctx, "w"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetXrayWatch(ctx, "w"); !jfrog.IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
	}
}
