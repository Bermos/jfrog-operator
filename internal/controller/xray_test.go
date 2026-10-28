package controller_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	xrayv1alpha1 "github.com/Bermos/jfrog-operator/api/xray/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
)

func rawJSON(s string) apiextensionsv1.JSON { return apiextensionsv1.JSON{Raw: []byte(s)} }

func rawJSONPtr(s string) *apiextensionsv1.JSON { j := rawJSON(s); return &j }

func TestXrayPolicyLifecycle(t *testing.T) {
	ns := newNamespace(t)
	p := &xrayv1alpha1.Policy{
		ObjectMeta: metav1.ObjectMeta{Name: "sec", Namespace: ns},
		Spec: xrayv1alpha1.PolicySpec{
			PolicyName:  ns + "-sec",
			Type:        "security",
			Description: "Block critical vulnerabilities",
			Rules: []xrayv1alpha1.PolicyRule{
				{Name: "high", Priority: 2, Criteria: rawJSON(`{"min_severity":"high"}`), Actions: rawJSONPtr(`{"fail_build":false}`)},
				{Name: "critical", Priority: 1, Criteria: rawJSON(`{"cvss_range":{"from":9,"to":10}}`),
					Actions: rawJSONPtr(`{"fail_build":true,"block_download":{"active":true,"unscanned":false}}`)},
			},
		},
	}
	mustCreate(t, p)
	name := p.Spec.PolicyName
	waitManagedReady(t, p)

	got := jfrogs.Object(fake.XrayPolicies, name)
	rules := got["rules"].([]any)
	if got["type"] != "security" || got["description"] != "Block critical vulnerabilities" || len(rules) != 2 {
		t.Fatalf("unexpected policy %v", got)
	}
	first := rules[0].(map[string]any)
	if first["name"] != "critical" || !reflect.DeepEqual(first["criteria"], map[string]any{"cvss_range": map[string]any{"from": 9.0, "to": 10.0}}) {
		t.Fatalf("rules not sent in priority order with criteria: %v", rules)
	}

	// Fields Xray adds (author) and rule order do not cause updates.
	path := "/xray/api/v2/policies/" + name
	puts := jfrogs.CountRequests(http.MethodPut, path)
	reordered := jfrogs.Object(fake.XrayPolicies, name)
	r := reordered["rules"].([]any)
	r[0], r[1] = r[1], r[0]
	jfrogs.SetObject(fake.XrayPolicies, name, reordered)
	time.Sleep(2 * syncInterval)
	if n := jfrogs.CountRequests(http.MethodPut, path); n != puts {
		t.Fatalf("reordered rules caused %d updates", n-puts)
	}

	// Drift in a nested action is corrected.
	drifted := jfrogs.Object(fake.XrayPolicies, name)
	for _, r := range drifted["rules"].([]any) {
		rule := r.(map[string]any)
		if rule["name"] == "critical" {
			rule["actions"].(map[string]any)["fail_build"] = false
		}
	}
	jfrogs.SetObject(fake.XrayPolicies, name, drifted)
	eventually(t, func() error {
		for _, r := range jfrogs.Object(fake.XrayPolicies, name)["rules"].([]any) {
			rule := r.(map[string]any)
			if rule["name"] == "critical" && rule["actions"].(map[string]any)["fail_build"] != true {
				return fmt.Errorf("drift not corrected: %v", rule)
			}
		}
		return nil
	})

	// Removing a rule from the spec removes it in Xray.
	update(t, p, func() { p.Spec.Rules = p.Spec.Rules[1:] })
	eventually(t, func() error {
		if n := len(jfrogs.Object(fake.XrayPolicies, name)["rules"].([]any)); n != 1 {
			return fmt.Errorf("%d rules", n)
		}
		return nil
	})

	deleteObj(t, p)
	waitGone(t, p)
	if jfrogs.Object(fake.XrayPolicies, name) != nil {
		t.Fatal("policy not deleted")
	}
}

func TestXrayPolicyTypeMismatch(t *testing.T) {
	ns := newNamespace(t)
	name := ns + "-lic"
	jfrogs.SetObject(fake.XrayPolicies, name, map[string]any{"type": "license", "rules": []any{}})
	p := &xrayv1alpha1.Policy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: xrayv1alpha1.PolicySpec{
			ResourceSpec: commonv1alpha1.ResourceSpec{AdoptExisting: true},
			Type:         "security",
			Rules:        []xrayv1alpha1.PolicyRule{{Name: "r", Priority: 1, Criteria: rawJSON(`{"min_severity":"low"}`)}},
		},
	}
	mustCreate(t, p)
	eventually(t, func() error {
		if err := get(t, p); err != nil {
			return err
		}
		if err := conditionIs(p.Status.Conditions, commonv1alpha1.ConditionSynced, metav1.ConditionFalse, commonv1alpha1.ReasonReconcileError); err != nil {
			return err
		}
		for _, c := range p.Status.Conditions {
			if c.Type == commonv1alpha1.ConditionSynced && !strings.Contains(c.Message, "is a license policy") {
				return fmt.Errorf("unexpected message %q", c.Message)
			}
		}
		return nil
	})
	if jfrogs.Object(fake.XrayPolicies, name)["type"] != "license" {
		t.Fatal("policy of another type was overwritten")
	}
}

func TestXrayWatchLifecycle(t *testing.T) {
	ns := newNamespace(t)
	w := &xrayv1alpha1.Watch{
		ObjectMeta: metav1.ObjectMeta{Name: "libs", Namespace: ns},
		Spec: xrayv1alpha1.WatchSpec{
			WatchName:   ns + "-libs",
			Description: "Watch team repositories",
			Resources: []xrayv1alpha1.WatchResource{
				{Type: "repository", Name: "libs-release", Filters: []xrayv1alpha1.WatchFilter{{Type: "regex", Value: rawJSON(`".*\\.jar"`)}}},
				{Type: "all-builds", Filters: []xrayv1alpha1.WatchFilter{
					{Type: "ant-patterns", Value: rawJSON(`{"IncludePatterns":["team-a/**"]}`)},
				}},
			},
			AssignedPolicies: []xrayv1alpha1.AssignedPolicy{{Name: "sec", Type: "security"}, {Name: "lic", Type: "license"}},
			WatchRecipients:  []string{"security@example.com"},
		},
	}
	mustCreate(t, w)
	name := w.Spec.WatchName
	waitManagedReady(t, w)
	if w.Spec.Active == nil || !*w.Spec.Active {
		t.Fatal("active should default to true")
	}

	got := jfrogs.Object(fake.XrayWatches, name)
	general := got["general_data"].(map[string]any)
	if general["name"] != name || general["active"] != true || general["description"] != "Watch team repositories" {
		t.Fatalf("unexpected general_data %v", general)
	}
	resources := got["project_resources"].(map[string]any)["resources"].([]any)
	repo := resources[0].(map[string]any)
	if repo["bin_mgr_id"] != "default" || repo["name"] != "libs-release" ||
		!reflect.DeepEqual(repo["filters"], []any{map[string]any{"type": "regex", "value": `.*\.jar`}}) {
		t.Fatalf("unexpected repository resource %v", repo)
	}
	builds := resources[1].(map[string]any)
	if _, ok := builds["name"]; ok {
		t.Fatalf("all-builds must not carry a name: %v", builds)
	}
	if !reflect.DeepEqual(got["assigned_policies"], []any{
		map[string]any{"name": "lic", "type": "license"},
		map[string]any{"name": "sec", "type": "security"},
	}) {
		t.Fatalf("unexpected assigned policies %v", got["assigned_policies"])
	}

	// Fields Xray adds to resources (repo_type) and the order of assigned
	// policies do not cause updates.
	path := "/xray/api/v2/watches/" + name
	puts := jfrogs.CountRequests(http.MethodPut, path)
	reordered := jfrogs.Object(fake.XrayWatches, name)
	a := reordered["assigned_policies"].([]any)
	a[0], a[1] = a[1], a[0]
	jfrogs.SetObject(fake.XrayWatches, name, reordered)
	time.Sleep(2 * syncInterval)
	if n := jfrogs.CountRequests(http.MethodPut, path); n != puts {
		t.Fatalf("watch in sync was updated %d times", n-puts)
	}

	// Deactivating in the spec is applied.
	update(t, w, func() { w.Spec.Active = boolPtr(false) })
	eventually(t, func() error {
		if a := jfrogs.Object(fake.XrayWatches, name)["general_data"].(map[string]any)["active"]; a != false {
			return fmt.Errorf("active = %v", a)
		}
		return nil
	})

	deleteObj(t, w)
	waitGone(t, w)
	if jfrogs.Object(fake.XrayWatches, name) != nil {
		t.Fatal("watch not deleted")
	}
}

func TestXrayValidation(t *testing.T) {
	ctx := context.Background()
	ns := newNamespace(t)
	noName := &xrayv1alpha1.Watch{
		ObjectMeta: metav1.ObjectMeta{Name: "noname", Namespace: ns},
		Spec: xrayv1alpha1.WatchSpec{
			Resources:        []xrayv1alpha1.WatchResource{{Type: "repository"}},
			AssignedPolicies: []xrayv1alpha1.AssignedPolicy{{Name: "p", Type: "security"}},
		},
	}
	if err := k8s.Create(ctx, noName); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("repository resource without name: got %v", err)
	}
	noPolicies := &xrayv1alpha1.Watch{
		ObjectMeta: metav1.ObjectMeta{Name: "nopolicies", Namespace: ns},
		Spec:       xrayv1alpha1.WatchSpec{Resources: []xrayv1alpha1.WatchResource{{Type: "all-repos"}}},
	}
	if err := k8s.Create(ctx, noPolicies); err == nil {
		t.Fatal("watch without policies must be rejected")
	}
	badType := &xrayv1alpha1.Policy{
		ObjectMeta: metav1.ObjectMeta{Name: "badtype", Namespace: ns},
		Spec: xrayv1alpha1.PolicySpec{Type: "malware",
			Rules: []xrayv1alpha1.PolicyRule{{Name: "r", Priority: 1, Criteria: rawJSON(`{}`)}}},
	}
	if err := k8s.Create(ctx, badType); err == nil {
		t.Fatal("unknown policy type must be rejected")
	}
	noRules := &xrayv1alpha1.Policy{
		ObjectMeta: metav1.ObjectMeta{Name: "norules", Namespace: ns},
		Spec:       xrayv1alpha1.PolicySpec{Type: "security", Rules: []xrayv1alpha1.PolicyRule{}},
	}
	if err := k8s.Create(ctx, noRules); err == nil {
		t.Fatal("policy without rules must be rejected")
	}
}
