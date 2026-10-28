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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	artifactoryv1alpha1 "github.com/Bermos/jfrog-operator/api/artifactory/v1alpha1"
	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
	"github.com/Bermos/jfrog-operator/internal/reconciler"
)

func mustCreate(t *testing.T, obj client.Object) {
	t.Helper()
	if err := k8s.Create(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
}

func keyOf(ns, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: ns, Name: name}
}

func localRepo(ns, name string) *artifactoryv1alpha1.Repository {
	return &artifactoryv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: artifactoryv1alpha1.RepositorySpec{
			Key:         ns + "-" + name,
			RClass:      artifactoryv1alpha1.RClassLocal,
			PackageType: "maven",
			Description: "managed by the operator",
		},
	}
}

func waitReady(t *testing.T, repo *artifactoryv1alpha1.Repository) {
	t.Helper()
	eventually(t, func() error {
		if err := get(t, repo); err != nil {
			return err
		}
		if err := conditionIs(repo.Status.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionTrue, ""); err != nil {
			return err
		}
		if err := conditionIs(repo.Status.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionTrue, ""); err != nil {
			return err
		}
		if repo.Status.ObservedGeneration != repo.Generation {
			return fmt.Errorf("generation %d not observed yet (%d)", repo.Generation, repo.Status.ObservedGeneration)
		}
		return nil
	})
}

func waitGone(t *testing.T, obj client.Object) {
	t.Helper()
	eventually(t, func() error {
		err := get(t, obj)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("%s still exists", obj.GetName())
	})
}

func deleteObj(t *testing.T, obj client.Object) {
	t.Helper()
	if err := k8s.Delete(context.Background(), obj); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryLifecycle(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	repo := localRepo(ns, "libs")
	mustCreate(t, repo)
	key := repo.Spec.Key

	// Create.
	waitReady(t, repo)
	got := jfrogs.Repository(key)
	if got == nil {
		t.Fatal("repository was not created in JFrog")
	}
	if got["rclass"] != "local" || got["packageType"] != "maven" || got["description"] != "managed by the operator" {
		t.Fatalf("unexpected repository config %v", got)
	}
	if repo.Status.ExternalName != key {
		t.Fatalf("externalName = %q", repo.Status.ExternalName)
	}
	if repo.Status.URL != jfrogs.URL+"/artifactory/"+key {
		t.Fatalf("status.url = %q", repo.Status.URL)
	}
	if !controllerutil.ContainsFinalizer(repo, reconciler.Finalizer) {
		t.Fatal("finalizer missing")
	}
	if _, ok := repo.Annotations[reconciler.AnnotationCreatePending]; ok {
		t.Fatal("create-pending annotation not removed after create")
	}

	// Update.
	repo.Spec.Description = "changed"
	repo.Spec.Config = &apiextensionsv1.JSON{Raw: []byte(`{"handleSnapshots": false, "maxUniqueSnapshots": 5}`)}
	if err := k8s.Update(ctx, repo); err != nil {
		t.Fatal(err)
	}
	waitReady(t, repo)
	eventually(t, func() error {
		got := jfrogs.Repository(key)
		if got["description"] != "changed" || got["handleSnapshots"] != false || got["maxUniqueSnapshots"] != 5.0 {
			return fmt.Errorf("update not applied: %v", got)
		}
		return nil
	})

	// Drift made outside the operator is corrected.
	drifted := jfrogs.Repository(key)
	drifted["description"] = "changed by hand"
	jfrogs.SetRepository(key, drifted)
	eventually(t, func() error {
		if d := jfrogs.Repository(key)["description"]; d != "changed" {
			return fmt.Errorf("drift not corrected: %v", d)
		}
		return nil
	})

	// A resource deleted outside the operator is recreated.
	jfrogs.DeleteRepositoryDirect(key)
	eventually(t, func() error {
		if jfrogs.Repository(key) == nil {
			return fmt.Errorf("repository not recreated")
		}
		return nil
	})

	// Delete.
	deleteObj(t, repo)
	waitGone(t, repo)
	if jfrogs.Repository(key) != nil {
		t.Fatal("repository not deleted in JFrog")
	}
}

func TestRepositoryInSyncIsNotRewritten(t *testing.T) {
	ns := newNamespace(t)
	repo := localRepo(ns, "steady")
	mustCreate(t, repo)
	waitReady(t, repo)
	path := "/artifactory/api/repositories/" + repo.Spec.Key
	before := jfrogs.CountRequests(http.MethodPost, path)
	// Several sync intervals pass; the operator must only read.
	time.Sleep(3 * syncInterval)
	if after := jfrogs.CountRequests(http.MethodPost, path); after != before {
		t.Fatalf("in-sync repository was updated %d times", after-before)
	}
	if n := jfrogs.CountRequests(http.MethodGet, path); n < 2 {
		t.Fatalf("expected periodic drift checks, saw %d GETs", n)
	}
}

func TestRepositoryKeyDefaultsToName(t *testing.T) {
	ns := newNamespace(t)
	repo := localRepo(ns, ns+"-defaulted")
	repo.Spec.Key = ""
	mustCreate(t, repo)
	waitReady(t, repo)
	if jfrogs.Repository(ns+"-defaulted") == nil {
		t.Fatal("repository with defaulted key not created")
	}
	deleteObj(t, repo)
	waitGone(t, repo)
}

func TestRepositoryOrphan(t *testing.T) {
	ns := newNamespace(t)
	repo := localRepo(ns, "orphan")
	repo.Spec.DeletionPolicy = commonv1alpha1.DeletionPolicyOrphan
	mustCreate(t, repo)
	waitReady(t, repo)
	deleteObj(t, repo)
	waitGone(t, repo)
	if jfrogs.Repository(repo.Spec.Key) == nil {
		t.Fatal("orphaned repository was deleted in JFrog")
	}
}

func TestRepositoryAdoption(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	repo := localRepo(ns, "existing")
	jfrogs.SetRepository(repo.Spec.Key, map[string]any{"rclass": "local", "packageType": "maven", "description": "pre-existing"})

	mustCreate(t, repo)
	eventually(t, func() error {
		if err := get(t, repo); err != nil {
			return err
		}
		return conditionIs(repo.Status.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionFalse, commonv1alpha1.ReasonAlreadyExists)
	})
	if d := jfrogs.Repository(repo.Spec.Key)["description"]; d != "pre-existing" {
		t.Fatalf("pre-existing repository was modified without adoptExisting: %v", d)
	}

	// Deleting the object must not delete a repository it never owned.
	deleteObj(t, repo)
	waitGone(t, repo)
	if jfrogs.Repository(repo.Spec.Key) == nil {
		t.Fatal("repository that was never adopted got deleted")
	}

	adopted := localRepo(ns, "existing")
	adopted.Spec.AdoptExisting = true
	mustCreate(t, adopted)
	waitReady(t, adopted)
	if d := jfrogs.Repository(adopted.Spec.Key)["description"]; d != "managed by the operator" {
		t.Fatalf("adopted repository not updated: %v", d)
	}
	if err := k8s.Delete(ctx, adopted); err != nil {
		t.Fatal(err)
	}
	waitGone(t, adopted)
	if jfrogs.Repository(adopted.Spec.Key) != nil {
		t.Fatal("adopted repository not deleted")
	}
}

func TestRepositoryCreatePendingIsAdopted(t *testing.T) {
	// Simulates a crash after creating the repository but before recording
	// it in the status: the annotation lets the operator recognise its own.
	ns := newNamespace(t)
	repo := localRepo(ns, "pending")
	repo.Annotations = map[string]string{reconciler.AnnotationCreatePending: repo.Spec.Key}
	jfrogs.SetRepository(repo.Spec.Key, map[string]any{"rclass": "local", "packageType": "maven"})
	mustCreate(t, repo)
	waitReady(t, repo)
	if d := jfrogs.Repository(repo.Spec.Key)["description"]; d != "managed by the operator" {
		t.Fatalf("repository not updated: %v", d)
	}
}

func TestRepositoryObserveOnly(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	repo := localRepo(ns, "observed")
	repo.Spec.ManagementPolicy = commonv1alpha1.ManagementPolicyObserve
	mustCreate(t, repo)

	eventually(t, func() error {
		if err := get(t, repo); err != nil {
			return err
		}
		return conditionIs(repo.Status.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionFalse, commonv1alpha1.ReasonNotFound)
	})
	if jfrogs.Repository(repo.Spec.Key) != nil {
		t.Fatal("observe-only repository was created")
	}
	if controllerutil.ContainsFinalizer(repo, reconciler.Finalizer) {
		t.Fatal("observe-only repository must not get a finalizer")
	}

	jfrogs.SetRepository(repo.Spec.Key, map[string]any{"rclass": "local", "packageType": "maven", "description": "theirs"})
	eventually(t, func() error {
		if err := get(t, repo); err != nil {
			return err
		}
		if err := conditionIs(repo.Status.Conditions, commonv1alpha1.ConditionReady, metav1.ConditionTrue, commonv1alpha1.ReasonAvailable); err != nil {
			return err
		}
		ready := repo.Status.Conditions[0]
		for _, c := range repo.Status.Conditions {
			if c.Type == commonv1alpha1.ConditionReady {
				ready = c
			}
		}
		if !strings.Contains(ready.Message, "description") {
			return fmt.Errorf("drift should be reported in the Ready message, got %q", ready.Message)
		}
		return nil
	})
	if d := jfrogs.Repository(repo.Spec.Key)["description"]; d != "theirs" {
		t.Fatalf("observe-only repository was modified: %v", d)
	}

	if err := k8s.Delete(ctx, repo); err != nil {
		t.Fatal(err)
	}
	waitGone(t, repo)
	if jfrogs.Repository(repo.Spec.Key) == nil {
		t.Fatal("observe-only repository was deleted")
	}
}

func TestRemoteRepositoryWithPassword(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	mustCreate(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "upstream", Namespace: ns},
		StringData: map[string]string{"password": "s3cret"},
	})
	repo := &artifactoryv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "central", Namespace: ns},
		Spec: artifactoryv1alpha1.RepositorySpec{
			Key:               ns + "-central",
			RClass:            artifactoryv1alpha1.RClassRemote,
			PackageType:       "maven",
			URL:               "https://repo1.maven.org/maven2",
			Username:          "reader",
			PasswordSecretRef: &commonv1alpha1.SecretKeySelector{Name: "upstream", Key: "password"},
		},
	}
	mustCreate(t, repo)
	waitReady(t, repo)
	got := jfrogs.Repository(repo.Spec.Key)
	if got["url"] != "https://repo1.maven.org/maven2" || got["username"] != "reader" || got["password"] != "s3cret" {
		t.Fatalf("unexpected remote config %v", got)
	}
	if repo.Status.SecretHash == "" {
		t.Fatal("secret hash not recorded")
	}

	// Rotating the password is picked up on the next sync.
	var secret corev1.Secret
	if err := k8s.Get(ctx, keyOf(ns, "upstream"), &secret); err != nil {
		t.Fatal(err)
	}
	secret.Data["password"] = []byte("rotated")
	if err := k8s.Update(ctx, &secret); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() error {
		if p := jfrogs.Repository(repo.Spec.Key)["password"]; p != "rotated" {
			return fmt.Errorf("password not rotated: %v", p)
		}
		return nil
	})
}

func TestRemoteRepositoryMissingSecret(t *testing.T) {
	ns := newNamespace(t)
	repo := &artifactoryv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "nosecret", Namespace: ns},
		Spec: artifactoryv1alpha1.RepositorySpec{
			Key: ns + "-nosecret", RClass: artifactoryv1alpha1.RClassRemote, PackageType: "npm",
			URL:               "https://registry.npmjs.org",
			PasswordSecretRef: &commonv1alpha1.SecretKeySelector{Name: "missing", Key: "password"},
		},
	}
	mustCreate(t, repo)
	eventually(t, func() error {
		if err := get(t, repo); err != nil {
			return err
		}
		return conditionIs(repo.Status.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionFalse, commonv1alpha1.ReasonReconcileError)
	})
	if jfrogs.Repository(repo.Spec.Key) != nil {
		t.Fatal("repository created without its password")
	}
}

func TestVirtualAndFederatedRepositories(t *testing.T) {
	ns := newNamespace(t)
	local := localRepo(ns, "local")
	mustCreate(t, local)
	virtual := &artifactoryv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "virtual", Namespace: ns},
		Spec: artifactoryv1alpha1.RepositorySpec{
			Key: ns + "-virtual", RClass: artifactoryv1alpha1.RClassVirtual, PackageType: "maven",
			Repositories:          []string{local.Spec.Key},
			DefaultDeploymentRepo: local.Spec.Key,
		},
	}
	mustCreate(t, virtual)
	disabled := false
	federated := &artifactoryv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "federated", Namespace: ns},
		Spec: artifactoryv1alpha1.RepositorySpec{
			Key: ns + "-federated", RClass: artifactoryv1alpha1.RClassFederated, PackageType: "generic",
			Members: []artifactoryv1alpha1.FederatedMember{
				{URL: "https://a.example.com/artifactory/x"},
				{URL: "https://b.example.com/artifactory/x", Enabled: &disabled},
			},
		},
	}
	mustCreate(t, federated)
	waitReady(t, virtual)
	waitReady(t, federated)

	v := jfrogs.Repository(virtual.Spec.Key)
	if !reflect.DeepEqual(v["repositories"], []any{local.Spec.Key}) || v["defaultDeploymentRepo"] != local.Spec.Key {
		t.Fatalf("unexpected virtual config %v", v)
	}
	f := jfrogs.Repository(federated.Spec.Key)
	want := []any{
		map[string]any{"url": "https://a.example.com/artifactory/x", "enabled": true},
		map[string]any{"url": "https://b.example.com/artifactory/x", "enabled": false},
	}
	if !reflect.DeepEqual(f["members"], want) {
		t.Fatalf("unexpected federated members %v", f["members"])
	}
}

func TestRepositoryWithNamespacedConnection(t *testing.T) {
	ns := newNamespace(t)
	mustCreate(t, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: ns},
		StringData: map[string]string{"accessToken": fake.Token},
	})
	mustCreate(t, &platformv1alpha1.Connection{
		ObjectMeta: metav1.ObjectMeta{Name: "mine", Namespace: ns},
		Spec: platformv1alpha1.ConnectionSpec{
			URL:         jfrogs.URL,
			Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "creds"}},
		},
	})
	repo := localRepo(ns, "via-namespaced")
	repo.Spec.ConnectionRef = &commonv1alpha1.ConnectionReference{Kind: commonv1alpha1.ConnectionKind, Name: "mine"}
	mustCreate(t, repo)
	waitReady(t, repo)
}

func TestRepositoryWithMissingConnection(t *testing.T) {
	ns := newNamespace(t)
	repo := localRepo(ns, "no-connection")
	repo.Spec.ConnectionRef = &commonv1alpha1.ConnectionReference{Kind: commonv1alpha1.ClusterConnectionKind, Name: "does-not-exist"}
	mustCreate(t, repo)
	eventually(t, func() error {
		if err := get(t, repo); err != nil {
			return err
		}
		return conditionIs(repo.Status.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionFalse, commonv1alpha1.ReasonConnectionError)
	})
	// Deletion must not hang on the broken connection.
	deleteObj(t, repo)
	waitGone(t, repo)
}

func TestRepositoryJFrogErrorsAreReported(t *testing.T) {
	ns := newNamespace(t)
	repo := localRepo(ns, "failing")
	path := "/artifactory/api/repositories/" + repo.Spec.Key
	jfrogs.FailWith(http.MethodPut, path, http.StatusInternalServerError)
	defer jfrogs.FailWith(http.MethodPut, path, 0)
	mustCreate(t, repo)
	eventually(t, func() error {
		if err := get(t, repo); err != nil {
			return err
		}
		return conditionIs(repo.Status.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionFalse, commonv1alpha1.ReasonReconcileError)
	})
	jfrogs.FailWith(http.MethodPut, path, 0)
	waitReady(t, repo)
}

func TestRepositoryDeleteFailureKeepsFinalizer(t *testing.T) {
	ns := newNamespace(t)
	repo := localRepo(ns, "undeletable")
	mustCreate(t, repo)
	waitReady(t, repo)
	path := "/artifactory/api/repositories/" + repo.Spec.Key
	jfrogs.FailWith(http.MethodDelete, path, http.StatusInternalServerError)
	deleteObj(t, repo)
	consistently(t, time.Second, func() error { return get(t, repo) })
	jfrogs.FailWith(http.MethodDelete, path, 0)
	waitGone(t, repo)
	if jfrogs.Repository(repo.Spec.Key) != nil {
		t.Fatal("repository not deleted after the failure cleared")
	}
}

func TestRepositoryValidation(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)

	remote := &artifactoryv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{Name: "remote-without-url", Namespace: ns},
		Spec:       artifactoryv1alpha1.RepositorySpec{RClass: artifactoryv1alpha1.RClassRemote, PackageType: "npm"},
	}
	if err := k8s.Create(ctx, remote); err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("remote without url: want validation error, got %v", err)
	}

	pw := localRepo(ns, "local-with-password")
	pw.Spec.PasswordSecretRef = &commonv1alpha1.SecretKeySelector{Name: "x", Key: "y"}
	if err := k8s.Create(ctx, pw); err == nil || !strings.Contains(err.Error(), "only valid for remote") {
		t.Fatalf("local with password: want validation error, got %v", err)
	}

	badClass := localRepo(ns, "bad-class")
	badClass.Spec.RClass = "distribution"
	if err := k8s.Create(ctx, badClass); err == nil {
		t.Fatal("unknown rclass must be rejected")
	}

	badKey := localRepo(ns, "bad-key")
	badKey.Spec.Key = "has spaces"
	if err := k8s.Create(ctx, badKey); err == nil {
		t.Fatal("invalid key must be rejected")
	}

	repo := localRepo(ns, "immutable")
	repo.Spec.ManagementPolicy = commonv1alpha1.ManagementPolicyObserve
	mustCreate(t, repo)
	for name, mutate := range map[string]func(*artifactoryv1alpha1.Repository){
		"key is immutable":         func(r *artifactoryv1alpha1.Repository) { r.Spec.Key = "other" },
		"key is immutable (unset)": func(r *artifactoryv1alpha1.Repository) { r.Spec.Key = "" },
		"rclass is immutable":      func(r *artifactoryv1alpha1.Repository) { r.Spec.RClass = artifactoryv1alpha1.RClassVirtual },
		"packageType is immutable": func(r *artifactoryv1alpha1.Repository) { r.Spec.PackageType = "npm" },
	} {
		var err error
		// The operator writes the status concurrently; retry on conflicts.
		for attempt := 0; attempt < 10; attempt++ {
			var fresh artifactoryv1alpha1.Repository
			if err := k8s.Get(ctx, client.ObjectKeyFromObject(repo), &fresh); err != nil {
				t.Fatal(err)
			}
			mutate(&fresh)
			if err = k8s.Update(ctx, &fresh); !apierrors.IsConflict(err) {
				break
			}
		}
		want := strings.TrimSuffix(name, " (unset)")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
}

func TestRepositoryDefaults(t *testing.T) {
	ns := newNamespace(t)
	repo := localRepo(ns, "defaults")
	mustCreate(t, repo)
	if err := get(t, repo); err != nil {
		t.Fatal(err)
	}
	if repo.Spec.DeletionPolicy != commonv1alpha1.DeletionPolicyDelete || repo.Spec.ManagementPolicy != commonv1alpha1.ManagementPolicyManage {
		t.Fatalf("defaults not applied: %+v", repo.Spec.ResourceSpec)
	}
}
