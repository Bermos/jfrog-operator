package controller_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
)

// managed is any object with the shared managed resource status.
type managed interface {
	client.Object
	GetResourceStatus() *commonv1alpha1.ResourceStatus
}

func waitManagedReady(t *testing.T, obj managed) {
	t.Helper()
	eventually(t, func() error {
		if err := get(t, obj); err != nil {
			return err
		}
		st := obj.GetResourceStatus()
		if err := conditionIs(st.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionTrue, ""); err != nil {
			return err
		}
		if err := conditionIs(st.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionTrue, ""); err != nil {
			return err
		}
		if st.ObservedGeneration != obj.GetGeneration() {
			return fmt.Errorf("generation %d not observed yet", obj.GetGeneration())
		}
		return nil
	})
}

func update(t *testing.T, obj client.Object, mutate func()) {
	t.Helper()
	for attempt := 0; ; attempt++ {
		if err := get(t, obj); err != nil {
			t.Fatal(err)
		}
		mutate()
		err := k8s.Update(context.Background(), obj)
		if err == nil {
			return
		}
		if !apierrors.IsConflict(err) || attempt > 10 {
			t.Fatal(err)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func TestUserLifecycle(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	mustCreate(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "alice", Namespace: ns},
		StringData: map[string]string{"password": "Passw0rd!"},
	})
	u := &platformv1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: "alice", Namespace: ns},
		Spec: platformv1alpha1.UserSpec{
			Username:          ns + "-alice",
			Email:             "alice@example.com",
			PasswordSecretRef: &commonv1alpha1.SecretKeySelector{Name: "alice", Key: "password"},
			Groups:            []string{"readers", "developers"},
			DisableUIAccess:   boolPtr(true),
		},
	}
	mustCreate(t, u)
	name := u.Spec.Username
	waitManagedReady(t, u)

	got := jfrogs.Object(fake.Users, name)
	if got["email"] != "alice@example.com" || got["password"] != "Passw0rd!" || got["disableUIAccess"] != true {
		t.Fatalf("unexpected user %v", got)
	}
	if !reflect.DeepEqual(got["groups"], []any{"developers", "readers"}) {
		t.Fatalf("groups = %v", got["groups"])
	}
	if u.Status.Realm != "internal" {
		t.Fatalf("realm = %q", u.Status.Realm)
	}

	// Group order returned by JFrog does not matter.
	path := "/artifactory/api/security/users/" + name
	posts := jfrogs.CountRequests(http.MethodPost, path)
	reordered := jfrogs.Object(fake.Users, name)
	reordered["groups"] = []any{"readers", "developers"}
	jfrogs.SetObject(fake.Users, name, reordered)
	time.Sleep(2 * syncInterval)
	if n := jfrogs.CountRequests(http.MethodPost, path); n != posts {
		t.Fatalf("reordered groups caused %d updates", n-posts)
	}

	// Spec changes and drift are applied.
	update(t, u, func() { u.Spec.Email = "alice@corp.example.com" })
	drifted := jfrogs.Object(fake.Users, name)
	drifted["admin"] = true
	jfrogs.SetObject(fake.Users, name, drifted)
	update(t, u, func() { u.Spec.Admin = boolPtr(false) })
	eventually(t, func() error {
		got := jfrogs.Object(fake.Users, name)
		if got["email"] != "alice@corp.example.com" || got["admin"] != false {
			return fmt.Errorf("not updated: %v", got)
		}
		return nil
	})
	if jfrogs.Object(fake.Users, name)["password"] != "Passw0rd!" {
		t.Fatal("password must not be resent when unchanged")
	}

	// Password rotation.
	var secret corev1.Secret
	if err := k8s.Get(ctx, keyOf(ns, "alice"), &secret); err != nil {
		t.Fatal(err)
	}
	secret.Data["password"] = []byte("N3wPassw0rd!")
	if err := k8s.Update(ctx, &secret); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() error {
		if p := jfrogs.Object(fake.Users, name)["password"]; p != "N3wPassw0rd!" {
			return fmt.Errorf("password not rotated: %v", p)
		}
		return nil
	})

	// Deleting works after the Secret is gone, as when the namespace is deleted.
	deleteObj(t, &secret)
	deleteObj(t, u)
	waitGone(t, u)
	if jfrogs.Object(fake.Users, name) != nil {
		t.Fatal("user not deleted")
	}
}

func TestUserWithoutPassword(t *testing.T) {
	ns := newNamespace(t)
	u := &platformv1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: ns + "-sso", Namespace: ns},
		Spec: platformv1alpha1.UserSpec{
			Email:                    "sso@example.com",
			InternalPasswordDisabled: boolPtr(true),
		},
	}
	mustCreate(t, u)
	waitManagedReady(t, u)
	got := jfrogs.Object(fake.Users, ns+"-sso")
	if got == nil || got["internalPasswordDisabled"] != true {
		t.Fatalf("unexpected user %v", got)
	}
	// Groups are not managed when unset, so auto-joined groups stay.
	if !reflect.DeepEqual(got["groups"], []any{"readers"}) {
		t.Fatalf("groups = %v", got["groups"])
	}
}

func TestUserValidation(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	u := &platformv1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: "nopass", Namespace: ns},
		Spec:       platformv1alpha1.UserSpec{Email: "a@example.com"},
	}
	if err := k8s.Create(ctx, u); err == nil || !strings.Contains(err.Error(), "passwordSecretRef is required") {
		t.Fatalf("want validation error, got %v", err)
	}
	u.Spec.InternalPasswordDisabled = boolPtr(true)
	u.Spec.Username = "first"
	u.Spec.ManagementPolicy = commonv1alpha1.ManagementPolicyObserve
	mustCreate(t, u)
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		if err = get(t, u); err != nil {
			t.Fatal(err)
		}
		u.Spec.Username = "second"
		if err = k8s.Update(ctx, u); !apierrors.IsConflict(err) {
			break
		}
	}
	if err == nil || !strings.Contains(err.Error(), "username is immutable") {
		t.Fatalf("want immutability error, got %v", err)
	}
}

func TestUserMissingPasswordSecret(t *testing.T) {
	ns := newNamespace(t)
	u := &platformv1alpha1.User{
		ObjectMeta: metav1.ObjectMeta{Name: ns + "-bob", Namespace: ns},
		Spec: platformv1alpha1.UserSpec{
			Email:             "bob@example.com",
			PasswordSecretRef: &commonv1alpha1.SecretKeySelector{Name: "missing", Key: "password"},
		},
	}
	mustCreate(t, u)
	eventually(t, func() error {
		if err := get(t, u); err != nil {
			return err
		}
		return conditionIs(u.Status.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionFalse, commonv1alpha1.ReasonReconcileError)
	})
	if jfrogs.Object(fake.Users, ns+"-bob") != nil {
		t.Fatal("user created without password")
	}
}

func TestGroupLifecycle(t *testing.T) {
	ns := newNamespace(t)
	g := &platformv1alpha1.Group{
		ObjectMeta: metav1.ObjectMeta{Name: "devs", Namespace: ns},
		Spec: platformv1alpha1.GroupSpec{
			GroupName:   ns + "-devs",
			Description: "Developers",
			AutoJoin:    boolPtr(false),
		},
	}
	mustCreate(t, g)
	name := g.Spec.GroupName
	waitManagedReady(t, g)
	got := jfrogs.Object(fake.Groups, name)
	if got["description"] != "Developers" || got["autoJoin"] != false {
		t.Fatalf("unexpected group %v", got)
	}

	update(t, g, func() {
		g.Spec.Description = "All developers"
		g.Spec.AdminPrivileges = boolPtr(true)
	})
	eventually(t, func() error {
		got := jfrogs.Object(fake.Groups, name)
		if got["description"] != "All developers" || got["adminPrivileges"] != true {
			return fmt.Errorf("not updated: %v", got)
		}
		return nil
	})

	jfrogs.DeleteObjectDirect(fake.Groups, name)
	eventually(t, func() error {
		if jfrogs.Object(fake.Groups, name) == nil {
			return fmt.Errorf("group not recreated")
		}
		return nil
	})

	deleteObj(t, g)
	waitGone(t, g)
	if jfrogs.Object(fake.Groups, name) != nil {
		t.Fatal("group not deleted")
	}
}

func TestGroupAdoption(t *testing.T) {
	ns := newNamespace(t)
	name := ns + "-existing"
	jfrogs.SetObject(fake.Groups, name, map[string]any{"description": "theirs"})
	g := &platformv1alpha1.Group{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       platformv1alpha1.GroupSpec{Description: "ours"},
	}
	mustCreate(t, g)
	eventually(t, func() error {
		if err := get(t, g); err != nil {
			return err
		}
		return conditionIs(g.Status.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionFalse, commonv1alpha1.ReasonAlreadyExists)
	})
	update(t, g, func() { g.Spec.AdoptExisting = true })
	waitManagedReady(t, g)
	if d := jfrogs.Object(fake.Groups, name)["description"]; d != "ours" {
		t.Fatalf("adopted group not updated: %v", d)
	}
}

func TestPermissionTargetLifecycle(t *testing.T) {
	ns := newNamespace(t)
	pt := &platformv1alpha1.PermissionTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "team", Namespace: ns},
		Spec: platformv1alpha1.PermissionTargetSpec{
			TargetName: ns + "-team",
			Repo: &platformv1alpha1.PermissionSection{
				Repositories:    []string{"libs-release", "libs-snapshot"},
				IncludePatterns: []string{"com/example/**"},
				Actions: platformv1alpha1.PermissionActions{
					Users:  map[string][]platformv1alpha1.PermissionAction{"alice": {"write", "read"}},
					Groups: map[string][]platformv1alpha1.PermissionAction{"devs": {"read"}},
				},
			},
		},
	}
	mustCreate(t, pt)
	name := pt.Spec.TargetName
	waitManagedReady(t, pt)

	got := jfrogs.Object(fake.Permissions, name)
	repo := got["repo"].(map[string]any)
	if !reflect.DeepEqual(repo["repositories"], []any{"libs-release", "libs-snapshot"}) ||
		!reflect.DeepEqual(repo["include-patterns"], []any{"com/example/**"}) ||
		!reflect.DeepEqual(repo["actions"], map[string]any{
			"users":  map[string]any{"alice": []any{"read", "write"}},
			"groups": map[string]any{"devs": []any{"read"}},
		}) {
		t.Fatalf("unexpected permission target %v", got)
	}
	if _, ok := got["build"]; ok {
		t.Fatal("unset sections must not be sent")
	}

	// Order returned by JFrog does not matter.
	path := "/artifactory/api/v2/security/permissions/" + name
	puts := jfrogs.CountRequests(http.MethodPut, path)
	repo["repositories"] = []any{"libs-snapshot", "libs-release"}
	repo["actions"].(map[string]any)["users"] = map[string]any{"alice": []any{"write", "read"}}
	jfrogs.SetObject(fake.Permissions, name, got)
	time.Sleep(2 * syncInterval)
	if n := jfrogs.CountRequests(http.MethodPut, path); n != puts {
		t.Fatalf("reordered lists caused %d updates", n-puts)
	}

	// Grants added by hand are removed.
	extra := jfrogs.Object(fake.Permissions, name)
	extra["repo"].(map[string]any)["actions"].(map[string]any)["users"].(map[string]any)["mallory"] = []any{"manage"}
	jfrogs.SetObject(fake.Permissions, name, extra)
	eventually(t, func() error {
		users := jfrogs.Object(fake.Permissions, name)["repo"].(map[string]any)["actions"].(map[string]any)["users"].(map[string]any)
		if _, ok := users["mallory"]; ok {
			return fmt.Errorf("extra grant not removed: %v", users)
		}
		return nil
	})

	// A section added by hand is removed.
	extra = jfrogs.Object(fake.Permissions, name)
	extra["build"] = map[string]any{"repositories": []any{"artifactory-build-info"}}
	jfrogs.SetObject(fake.Permissions, name, extra)
	eventually(t, func() error {
		if _, ok := jfrogs.Object(fake.Permissions, name)["build"]; ok {
			return fmt.Errorf("extra section not removed")
		}
		return nil
	})

	// Spec changes replace the target.
	update(t, pt, func() {
		pt.Spec.Repo.Actions.Users = nil
		pt.Spec.Build = &platformv1alpha1.PermissionSection{
			Repositories: []string{"artifactory-build-info"},
			Actions:      platformv1alpha1.PermissionActions{Groups: map[string][]platformv1alpha1.PermissionAction{"devs": {"read"}}},
		}
	})
	eventually(t, func() error {
		got := jfrogs.Object(fake.Permissions, name)
		actions := got["repo"].(map[string]any)["actions"].(map[string]any)
		if _, ok := actions["users"]; ok {
			return fmt.Errorf("user grants not removed: %v", actions)
		}
		if _, ok := got["build"]; !ok {
			return fmt.Errorf("build section not added")
		}
		return nil
	})

	deleteObj(t, pt)
	waitGone(t, pt)
	if jfrogs.Object(fake.Permissions, name) != nil {
		t.Fatal("permission target not deleted")
	}
}

func TestPermissionTargetValidation(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	empty := &platformv1alpha1.PermissionTarget{ObjectMeta: metav1.ObjectMeta{Name: "empty", Namespace: ns}}
	if err := k8s.Create(ctx, empty); err == nil || !strings.Contains(err.Error(), "at least one of repo") {
		t.Fatalf("want validation error, got %v", err)
	}
	badAction := &platformv1alpha1.PermissionTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: ns},
		Spec: platformv1alpha1.PermissionTargetSpec{Repo: &platformv1alpha1.PermissionSection{
			Repositories: []string{"ANY"},
			Actions:      platformv1alpha1.PermissionActions{Users: map[string][]platformv1alpha1.PermissionAction{"a": {"admin"}}},
		}},
	}
	if err := k8s.Create(ctx, badAction); err == nil {
		t.Fatal("unknown action must be rejected")
	}
	noRepos := &platformv1alpha1.PermissionTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "norepos", Namespace: ns},
		Spec:       platformv1alpha1.PermissionTargetSpec{Repo: &platformv1alpha1.PermissionSection{Repositories: []string{}}},
	}
	if err := k8s.Create(ctx, noRepos); err == nil {
		t.Fatal("a section without repositories must be rejected")
	}
}
