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

// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=permissiontargets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=permissiontargets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=permissiontargets/finalizers,verbs=update

// SetupPermissionTarget registers the PermissionTarget controller with the manager.
func SetupPermissionTarget(mgr ctrl.Manager, r *reconciler.Reconciler[*platformv1alpha1.PermissionTarget]) error {
	r.NewObject = func() *platformv1alpha1.PermissionTarget { return &platformv1alpha1.PermissionTarget{} }
	r.Connect = ConnectPermissionTarget
	return ctrl.NewControllerManagedBy(mgr).For(&platformv1alpha1.PermissionTarget{}).Named("permissiontarget").Complete(r)
}

// ConnectPermissionTarget builds the External for a PermissionTarget.
func ConnectPermissionTarget(_ context.Context, _ client.Client, jc *jfrog.Client, obj *platformv1alpha1.PermissionTarget) (reconciler.External, error) {
	return &permissionTarget{jc: jc, obj: obj, desired: jfrog.PermissionTarget{
		Name:          obj.GetTargetName(),
		Repo:          toSection(obj.Spec.Repo),
		Build:         toSection(obj.Spec.Build),
		ReleaseBundle: toSection(obj.Spec.ReleaseBundle),
	}}, nil
}

type permissionTarget struct {
	jc      *jfrog.Client
	obj     *platformv1alpha1.PermissionTarget
	desired jfrog.PermissionTarget
}

func (p *permissionTarget) Name() string { return p.obj.GetTargetName() }

func (p *permissionTarget) Observe(ctx context.Context) (reconciler.Observation, error) {
	observed, err := p.jc.GetPermissionTarget(ctx, p.Name())
	if jfrog.IsNotFound(err) {
		return reconciler.Observation{}, nil
	}
	if err != nil {
		return reconciler.Observation{}, err
	}
	normaliseSection(observed.Repo)
	normaliseSection(observed.Build)
	normaliseSection(observed.ReleaseBundle)

	// The whole target is replaced on update, so sections and grants that
	// exist in Artifactory but not in the spec are drift too.
	paths, err := jfrog.Diff(p.desired, observed)
	if err != nil {
		return reconciler.Observation{}, err
	}
	extra := []struct {
		name             string
		desired, current *jfrog.PermissionSection
	}{
		{"repo", p.desired.Repo, observed.Repo},
		{"build", p.desired.Build, observed.Build},
		{"releaseBundle", p.desired.ReleaseBundle, observed.ReleaseBundle},
	}
	for _, s := range extra {
		if s.desired == nil && s.current != nil {
			paths = append(paths, s.name)
		}
		if s.desired != nil && s.current != nil && hasExtraGrants(s.desired.Actions, s.current.Actions) {
			paths = append(paths, s.name+".actions")
		}
	}
	return reconciler.Observation{Exists: true, UpToDate: len(paths) == 0, Diff: strings.Join(paths, ", ")}, nil
}

func (p *permissionTarget) Create(ctx context.Context) error {
	return p.jc.CreatePermissionTarget(ctx, &p.desired)
}

func (p *permissionTarget) Update(ctx context.Context) error {
	return p.jc.UpdatePermissionTarget(ctx, &p.desired)
}

func (p *permissionTarget) Delete(ctx context.Context) error {
	return p.jc.DeletePermissionTarget(ctx, p.Name())
}

func toSection(s *platformv1alpha1.PermissionSection) *jfrog.PermissionSection {
	if s == nil {
		return nil
	}
	out := &jfrog.PermissionSection{
		Repositories:    sorted(s.Repositories),
		IncludePatterns: s.IncludePatterns,
		ExcludePatterns: s.ExcludePatterns,
	}
	if len(s.Actions.Users) > 0 || len(s.Actions.Groups) > 0 {
		out.Actions = &jfrog.PermissionActions{Users: toGrants(s.Actions.Users), Groups: toGrants(s.Actions.Groups)}
	}
	return out
}

func toGrants(in map[string][]platformv1alpha1.PermissionAction) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for name, actions := range in {
		a := make([]string, len(actions))
		for i, action := range actions {
			a[i] = string(action)
		}
		out[name] = sorted(a)
	}
	return out
}

// normaliseSection sorts lists Artifactory returns in no particular order.
func normaliseSection(s *jfrog.PermissionSection) {
	if s == nil {
		return
	}
	s.Repositories = sorted(s.Repositories)
	if s.Actions == nil {
		return
	}
	for _, m := range []map[string][]string{s.Actions.Users, s.Actions.Groups} {
		for k, v := range m {
			m[k] = sorted(v)
		}
	}
}

// hasExtraGrants reports whether current grants actions to a user or group
// that desired does not mention.
func hasExtraGrants(desired, current *jfrog.PermissionActions) bool {
	if current == nil {
		return false
	}
	var du, dg map[string][]string
	if desired != nil {
		du, dg = desired.Users, desired.Groups
	}
	for name := range current.Users {
		if _, ok := du[name]; !ok {
			return true
		}
	}
	for name := range current.Groups {
		if _, ok := dg[name]; !ok {
			return true
		}
	}
	return false
}
