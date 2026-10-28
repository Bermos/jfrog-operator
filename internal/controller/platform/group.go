package platform

import (
	"context"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog"
	"github.com/Bermos/jfrog-operator/internal/reconciler"
)

// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=groups,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=groups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=groups/finalizers,verbs=update

// SetupGroup registers the Group controller with the manager.
func SetupGroup(mgr ctrl.Manager, r *reconciler.Reconciler[*platformv1alpha1.Group]) error {
	r.NewObject = func() *platformv1alpha1.Group { return &platformv1alpha1.Group{} }
	r.Connect = ConnectGroup
	return ctrl.NewControllerManagedBy(mgr).For(&platformv1alpha1.Group{}).Named("group").Complete(r)
}

// ConnectGroup builds the External for a Group.
func ConnectGroup(_ context.Context, _ client.Client, jc *jfrog.Client, obj *platformv1alpha1.Group) (reconciler.External, error) {
	spec := obj.Spec
	return &group{jc: jc, obj: obj, desired: jfrog.Group{
		Name:            obj.GetGroupName(),
		Description:     spec.Description,
		AutoJoin:        spec.AutoJoin,
		AdminPrivileges: spec.AdminPrivileges,
		Realm:           spec.Realm,
		RealmAttributes: spec.RealmAttributes,
	}}, nil
}

type group struct {
	jc      *jfrog.Client
	obj     *platformv1alpha1.Group
	desired jfrog.Group
}

func (g *group) Name() string { return g.obj.GetGroupName() }

func (g *group) Observe(ctx context.Context) (reconciler.Observation, error) {
	observed, err := g.jc.GetGroup(ctx, g.Name())
	if jfrog.IsNotFound(err) {
		return reconciler.Observation{}, nil
	}
	if err != nil {
		return reconciler.Observation{}, err
	}
	paths, err := jfrog.Diff(g.desired, observed)
	if err != nil {
		return reconciler.Observation{}, err
	}
	return reconciler.Observation{Exists: true, UpToDate: len(paths) == 0, Diff: strings.Join(paths, ", ")}, nil
}

func (g *group) Create(ctx context.Context) error { return g.jc.CreateGroup(ctx, &g.desired) }
func (g *group) Update(ctx context.Context) error { return g.jc.UpdateGroup(ctx, &g.desired) }
func (g *group) Delete(ctx context.Context) error { return g.jc.DeleteGroup(ctx, g.Name()) }
