// Package xray contains the controllers for Xray resources.
package xray

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xrayv1alpha1 "github.com/Bermos/jfrog-operator/api/xray/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog"
	"github.com/Bermos/jfrog-operator/internal/reconciler"
)

// +kubebuilder:rbac:groups=xray.jfrog.bermos.io,resources=policies;watches,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=xray.jfrog.bermos.io,resources=policies/status;watches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=xray.jfrog.bermos.io,resources=policies/finalizers;watches/finalizers,verbs=update

// SetupPolicy registers the Policy controller with the manager.
func SetupPolicy(mgr ctrl.Manager, r *reconciler.Reconciler[*xrayv1alpha1.Policy]) error {
	r.NewObject = func() *xrayv1alpha1.Policy { return &xrayv1alpha1.Policy{} }
	r.Connect = ConnectPolicy
	return ctrl.NewControllerManagedBy(mgr).For(&xrayv1alpha1.Policy{}).Named("xray-policy").Complete(r)
}

// SetupWatch registers the Watch controller with the manager.
func SetupWatch(mgr ctrl.Manager, r *reconciler.Reconciler[*xrayv1alpha1.Watch]) error {
	r.NewObject = func() *xrayv1alpha1.Watch { return &xrayv1alpha1.Watch{} }
	r.Connect = ConnectWatch
	return ctrl.NewControllerManagedBy(mgr).For(&xrayv1alpha1.Watch{}).Named("xray-watch").Complete(r)
}

// ConnectPolicy builds the External for a Policy.
func ConnectPolicy(_ context.Context, _ client.Client, jc *jfrog.Client, obj *xrayv1alpha1.Policy) (reconciler.External, error) {
	rules := slices.Clone(obj.Spec.Rules)
	slices.SortStableFunc(rules, func(a, b xrayv1alpha1.PolicyRule) int { return cmp.Compare(a.Priority, b.Priority) })
	desiredRules := make([]any, 0, len(rules))
	for _, r := range rules {
		criteria, err := object(&r.Criteria, "rules["+r.Name+"].criteria")
		if err != nil {
			return nil, err
		}
		rule := map[string]any{"name": r.Name, "priority": r.Priority, "criteria": criteria}
		if r.Actions != nil {
			actions, err := object(r.Actions, "rules["+r.Name+"].actions")
			if err != nil {
				return nil, err
			}
			rule["actions"] = actions
		}
		desiredRules = append(desiredRules, rule)
	}
	desired := jfrog.XrayPolicy{
		"name":  obj.GetPolicyName(),
		"type":  obj.Spec.Type,
		"rules": desiredRules,
	}
	if obj.Spec.Description != "" {
		desired["description"] = obj.Spec.Description
	}
	return &policy{jc: jc, obj: obj, desired: desired}, nil
}

type policy struct {
	jc      *jfrog.Client
	obj     *xrayv1alpha1.Policy
	desired jfrog.XrayPolicy
}

func (p *policy) Name() string { return p.obj.GetPolicyName() }

func (p *policy) Observe(ctx context.Context) (reconciler.Observation, error) {
	observed, err := p.jc.GetXrayPolicy(ctx, p.Name())
	if jfrog.IsNotFound(err) {
		return reconciler.Observation{}, nil
	}
	if err != nil {
		return reconciler.Observation{}, err
	}
	if t, _ := observed["type"].(string); t != "" && t != p.obj.Spec.Type {
		return reconciler.Observation{}, fmt.Errorf("policy %s is a %s policy, spec wants %s", p.Name(), t, p.obj.Spec.Type)
	}
	if rules, ok := observed["rules"].([]any); ok {
		slices.SortStableFunc(rules, func(a, b any) int { return cmp.Compare(priority(a), priority(b)) })
	}
	return observe(p.desired, observed)
}

func (p *policy) Create(ctx context.Context) error { return p.jc.CreateXrayPolicy(ctx, p.desired) }
func (p *policy) Update(ctx context.Context) error {
	return p.jc.UpdateXrayPolicy(ctx, p.Name(), p.desired)
}
func (p *policy) Delete(ctx context.Context) error { return p.jc.DeleteXrayPolicy(ctx, p.Name()) }

// ConnectWatch builds the External for a Watch.
func ConnectWatch(_ context.Context, _ client.Client, jc *jfrog.Client, obj *xrayv1alpha1.Watch) (reconciler.External, error) {
	spec := obj.Spec
	general := map[string]any{"name": obj.GetWatchName(), "active": spec.Active == nil || *spec.Active}
	if spec.Description != "" {
		general["description"] = spec.Description
	}
	resources := make([]any, 0, len(spec.Resources))
	for _, r := range spec.Resources {
		res := map[string]any{"type": r.Type, "bin_mgr_id": cmp.Or(r.BinMgrID, "default")}
		if r.Name != "" {
			res["name"] = r.Name
		}
		if len(r.Filters) > 0 {
			filters := make([]any, 0, len(r.Filters))
			for _, f := range r.Filters {
				var value any
				if err := json.Unmarshal(f.Value.Raw, &value); err != nil {
					return nil, fmt.Errorf("invalid filter value: %w", err)
				}
				filters = append(filters, map[string]any{"type": f.Type, "value": value})
			}
			res["filters"] = filters
		}
		resources = append(resources, res)
	}
	policies := slices.Clone(spec.AssignedPolicies)
	slices.SortFunc(policies, func(a, b xrayv1alpha1.AssignedPolicy) int { return cmp.Compare(a.Name, b.Name) })
	assigned := make([]any, 0, len(policies))
	for _, p := range policies {
		assigned = append(assigned, map[string]any{"name": p.Name, "type": p.Type})
	}
	desired := jfrog.XrayWatch{
		"general_data":      general,
		"project_resources": map[string]any{"resources": resources},
		"assigned_policies": assigned,
	}
	if len(spec.WatchRecipients) > 0 {
		desired["watch_recipients"] = spec.WatchRecipients
	}
	return &watch{jc: jc, obj: obj, desired: desired}, nil
}

type watch struct {
	jc      *jfrog.Client
	obj     *xrayv1alpha1.Watch
	desired jfrog.XrayWatch
}

func (w *watch) Name() string { return w.obj.GetWatchName() }

func (w *watch) Observe(ctx context.Context) (reconciler.Observation, error) {
	observed, err := w.jc.GetXrayWatch(ctx, w.Name())
	if jfrog.IsNotFound(err) {
		return reconciler.Observation{}, nil
	}
	if err != nil {
		return reconciler.Observation{}, err
	}
	if assigned, ok := observed["assigned_policies"].([]any); ok {
		slices.SortStableFunc(assigned, func(a, b any) int { return cmp.Compare(name(a), name(b)) })
	}
	return observe(w.desired, observed)
}

func (w *watch) Create(ctx context.Context) error { return w.jc.CreateXrayWatch(ctx, w.desired) }
func (w *watch) Update(ctx context.Context) error {
	return w.jc.UpdateXrayWatch(ctx, w.Name(), w.desired)
}
func (w *watch) Delete(ctx context.Context) error { return w.jc.DeleteXrayWatch(ctx, w.Name()) }

func observe(desired, observed any) (reconciler.Observation, error) {
	paths, err := jfrog.Diff(desired, observed)
	if err != nil {
		return reconciler.Observation{}, err
	}
	return reconciler.Observation{Exists: true, UpToDate: len(paths) == 0, Diff: strings.Join(paths, ", ")}, nil
}

func object(j *apiextensionsv1.JSON, field string) (map[string]any, error) {
	out := map[string]any{}
	if j == nil || len(j.Raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(j.Raw, &out); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", field, err)
	}
	return out, nil
}

func priority(v any) float64 {
	m, _ := v.(map[string]any)
	p, _ := m["priority"].(float64)
	return p
}

func name(v any) string {
	m, _ := v.(map[string]any)
	n, _ := m["name"].(string)
	return n
}
