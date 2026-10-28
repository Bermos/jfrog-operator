package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog"
	"github.com/Bermos/jfrog-operator/internal/reconciler"
)

// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=users,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=users/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=users/finalizers,verbs=update

// SetupUser registers the User controller with the manager.
func SetupUser(mgr ctrl.Manager, r *reconciler.Reconciler[*platformv1alpha1.User]) error {
	r.NewObject = func() *platformv1alpha1.User { return &platformv1alpha1.User{} }
	r.Connect = ConnectUser
	return ctrl.NewControllerManagedBy(mgr).For(&platformv1alpha1.User{}).Named("user").Complete(r)
}

// ConnectUser builds the External for a User.
func ConnectUser(ctx context.Context, kube client.Client, jc *jfrog.Client, obj *platformv1alpha1.User) (reconciler.External, error) {
	u := &user{jc: jc, obj: obj}
	spec := obj.Spec
	u.desired = jfrog.User{
		Name:                     obj.GetUsername(),
		Email:                    spec.Email,
		Admin:                    spec.Admin,
		ProfileUpdatable:         spec.ProfileUpdatable,
		DisableUIAccess:          spec.DisableUIAccess,
		InternalPasswordDisabled: spec.InternalPasswordDisabled,
		Groups:                   sorted(spec.Groups),
	}
	// Deleting needs only the name, and the Secret may already be gone, e.g.
	// when the whole namespace is deleted.
	if ref := spec.PasswordSecretRef; ref != nil && obj.GetDeletionTimestamp() == nil {
		var secret corev1.Secret
		if err := kube.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: ref.Name}, &secret); err != nil {
			return nil, fmt.Errorf("getting password Secret %s: %w", ref.Name, err)
		}
		password, ok := secret.Data[ref.Key]
		if !ok || len(password) == 0 {
			return nil, fmt.Errorf("secret %s has no key %q", ref.Name, ref.Key)
		}
		u.password = string(password)
		sum := sha256.Sum256(password)
		u.secretHash = hex.EncodeToString(sum[:])
	}
	return u, nil
}

type user struct {
	jc         *jfrog.Client
	obj        *platformv1alpha1.User
	desired    jfrog.User
	password   string
	secretHash string
}

func (u *user) Name() string { return u.obj.GetUsername() }

func (u *user) Observe(ctx context.Context) (reconciler.Observation, error) {
	observed, err := u.jc.GetUser(ctx, u.Name())
	if jfrog.IsNotFound(err) {
		return reconciler.Observation{}, nil
	}
	if err != nil {
		return reconciler.Observation{}, err
	}
	u.obj.Status.Realm = observed.Realm
	observed.Groups = sorted(observed.Groups)
	paths, err := jfrog.Diff(u.desired, observed)
	if err != nil {
		return reconciler.Observation{}, err
	}
	if u.secretHash != u.obj.Status.SecretHash {
		paths = append(paths, "password")
	}
	return reconciler.Observation{Exists: true, UpToDate: len(paths) == 0, Diff: strings.Join(paths, ", ")}, nil
}

func (u *user) Create(ctx context.Context) error {
	body := u.desired
	body.Password = u.password
	if err := u.jc.CreateUser(ctx, &body); err != nil {
		return err
	}
	u.obj.Status.SecretHash = u.secretHash
	return nil
}

func (u *user) Update(ctx context.Context) error {
	body := u.desired
	if u.secretHash != u.obj.Status.SecretHash {
		body.Password = u.password
	}
	if err := u.jc.UpdateUser(ctx, &body); err != nil {
		return err
	}
	u.obj.Status.SecretHash = u.secretHash
	return nil
}

func (u *user) Delete(ctx context.Context) error { return u.jc.DeleteUser(ctx, u.Name()) }

func sorted(s []string) []string {
	if s == nil {
		return nil
	}
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
