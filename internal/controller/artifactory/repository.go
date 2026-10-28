// Package artifactory contains the controllers for Artifactory resources.
package artifactory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	artifactoryv1alpha1 "github.com/Bermos/jfrog-operator/api/artifactory/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog"
	"github.com/Bermos/jfrog-operator/internal/reconciler"
)

// +kubebuilder:rbac:groups=artifactory.jfrog.bermos.io,resources=repositories,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=artifactory.jfrog.bermos.io,resources=repositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=artifactory.jfrog.bermos.io,resources=repositories/finalizers,verbs=update

// SetupRepository registers the Repository controller with the manager.
func SetupRepository(mgr ctrl.Manager, r *reconciler.Reconciler[*artifactoryv1alpha1.Repository]) error {
	r.NewObject = func() *artifactoryv1alpha1.Repository { return &artifactoryv1alpha1.Repository{} }
	r.Connect = ConnectRepository
	return ctrl.NewControllerManagedBy(mgr).
		For(&artifactoryv1alpha1.Repository{}).
		Named("repository").
		Complete(r)
}

// ConnectRepository builds the External for a Repository.
func ConnectRepository(ctx context.Context, kube client.Client, jc *jfrog.Client, obj *artifactoryv1alpha1.Repository) (reconciler.External, error) {
	desired, secretHash, err := desiredRepository(ctx, kube, obj)
	if err != nil {
		return nil, err
	}
	return &repository{jc: jc, obj: obj, desired: desired, secretHash: secretHash}, nil
}

type repository struct {
	jc         *jfrog.Client
	obj        *artifactoryv1alpha1.Repository
	desired    jfrog.RepositoryConfig
	secretHash string
}

func (r *repository) Name() string { return r.obj.GetKey() }

func (r *repository) Observe(ctx context.Context) (reconciler.Observation, error) {
	observed, err := r.jc.GetRepository(ctx, r.Name())
	if jfrog.IsNotFound(err) {
		return reconciler.Observation{}, nil
	}
	if err != nil {
		return reconciler.Observation{}, err
	}
	if rclass, _ := observed["rclass"].(string); rclass != "" && rclass != r.obj.Spec.RClass {
		return reconciler.Observation{}, fmt.Errorf("repository %s is a %s repository, spec wants %s", r.Name(), rclass, r.obj.Spec.RClass)
	}
	// Artifactory never returns secrets, so compare those by hash.
	compare := make(jfrog.RepositoryConfig, len(r.desired))
	for k, v := range r.desired {
		if k != "password" {
			compare[k] = v
		}
	}
	paths, err := jfrog.Diff(compare, observed)
	if err != nil {
		return reconciler.Observation{}, err
	}
	if r.secretHash != r.obj.Status.SecretHash {
		paths = append(paths, "password")
	}
	r.obj.Status.URL = strings.TrimRight(r.jc.ArtifactoryURL(), "/") + "/" + r.Name()
	return reconciler.Observation{Exists: true, UpToDate: len(paths) == 0, Diff: strings.Join(paths, ", ")}, nil
}

func (r *repository) Create(ctx context.Context) error {
	if err := r.jc.CreateRepository(ctx, r.Name(), r.desired); err != nil {
		return err
	}
	r.obj.Status.SecretHash = r.secretHash
	r.obj.Status.URL = strings.TrimRight(r.jc.ArtifactoryURL(), "/") + "/" + r.Name()
	return nil
}

func (r *repository) Update(ctx context.Context) error {
	if err := r.jc.UpdateRepository(ctx, r.Name(), r.desired); err != nil {
		return err
	}
	r.obj.Status.SecretHash = r.secretHash
	return nil
}

func (r *repository) Delete(ctx context.Context) error {
	return r.jc.DeleteRepository(ctx, r.Name())
}

// desiredRepository builds the repository configuration from the spec, and
// returns a hash of the secret values in it.
func desiredRepository(ctx context.Context, kube client.Client, obj *artifactoryv1alpha1.Repository) (jfrog.RepositoryConfig, string, error) {
	spec := obj.Spec
	if len(obj.GetKey()) > 64 {
		return nil, "", fmt.Errorf("repository key %q is longer than 64 characters; set spec.key", obj.GetKey())
	}
	cfg := jfrog.RepositoryConfig{}
	if spec.Config != nil && len(spec.Config.Raw) > 0 {
		if err := json.Unmarshal(spec.Config.Raw, &cfg); err != nil {
			return nil, "", fmt.Errorf("spec.config must be a JSON object: %w", err)
		}
	}
	set := func(k string, v any) { cfg[k] = v }
	setString := func(k, v string) {
		if v != "" {
			cfg[k] = v
		}
	}

	set("key", obj.GetKey())
	set("rclass", spec.RClass)
	set("packageType", spec.PackageType)
	setString("description", spec.Description)
	setString("notes", spec.Notes)
	setString("includesPattern", spec.IncludesPattern)
	setString("excludesPattern", spec.ExcludesPattern)
	setString("repoLayoutRef", spec.RepoLayoutRef)
	setString("projectKey", spec.ProjectKey)
	if spec.Environments != nil {
		set("environments", spec.Environments)
	}
	if spec.XrayIndex != nil {
		set("xrayIndex", *spec.XrayIndex)
	}

	secretHash := ""
	switch spec.RClass {
	case artifactoryv1alpha1.RClassRemote:
		setString("url", spec.URL)
		setString("username", spec.Username)
		if ref := spec.PasswordSecretRef; ref != nil {
			var secret corev1.Secret
			if err := kube.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: ref.Name}, &secret); err != nil {
				return nil, "", fmt.Errorf("getting password Secret %s: %w", ref.Name, err)
			}
			password, ok := secret.Data[ref.Key]
			if !ok {
				return nil, "", fmt.Errorf("secret %s has no key %q", ref.Name, ref.Key)
			}
			set("password", string(password))
			sum := sha256.Sum256(password)
			secretHash = hex.EncodeToString(sum[:])
		}
	case artifactoryv1alpha1.RClassVirtual:
		if spec.Repositories != nil {
			set("repositories", spec.Repositories)
		}
		setString("defaultDeploymentRepo", spec.DefaultDeploymentRepo)
	case artifactoryv1alpha1.RClassFederated:
		if spec.Members != nil {
			members := make([]map[string]any, 0, len(spec.Members))
			for _, m := range spec.Members {
				enabled := m.Enabled == nil || *m.Enabled
				members = append(members, map[string]any{"url": m.URL, "enabled": enabled})
			}
			set("members", members)
		}
	}
	return cfg, secretHash, nil
}
