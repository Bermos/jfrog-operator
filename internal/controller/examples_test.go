package controller_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// TestCrossplaneExampleRendersValidResources renders the Go template of the
// example composition the way function-go-templating does, for the example
// Team, and creates every composed resource with a server-side dry run. This
// keeps the example in step with the CRDs.
func TestCrossplaneExampleRendersValidResources(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "crossplane")
	tmplText := compositionTemplate(t, filepath.Join(dir, "composition.yaml"))
	xr := readYAML(t, filepath.Join(dir, "team.yaml"))

	tmpl, err := template.New("composition").Option("missingkey=zero").Parse(tmplText)
	if err != nil {
		t.Fatal(err)
	}

	ns := "team-payments"
	_ = k8s.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})

	for name, observed := range map[string]map[string]any{
		"first render": {},
		"after the virtual repository exists": {
			"virtual": map[string]any{"resource": map[string]any{"status": map[string]any{"url": "https://jfrog.example.com/artifactory/payments-maven"}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			data := map[string]any{"observed": map[string]any{
				"composite": map[string]any{"resource": xr},
				"resources": observed,
			}}
			if err := tmpl.Execute(&out, data); err != nil {
				t.Fatal(err)
			}
			kinds := map[string]int{}
			dec := utilyaml.NewYAMLOrJSONDecoder(&out, 4096)
			for {
				var obj unstructured.Unstructured
				if err := dec.Decode(&obj.Object); err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					t.Fatalf("rendered template is not valid YAML: %v\n%s", err, out.String())
				}
				if len(obj.Object) == 0 {
					continue
				}
				kinds[obj.GetKind()]++
				if obj.GetKind() == "Team" {
					if url, _, _ := unstructured.NestedString(obj.Object, "status", "url"); url == "" {
						t.Errorf("composite status update without url: %v", obj.Object)
					}
					continue
				}
				// Crossplane v2 creates composed resources in the composite's namespace.
				obj.SetNamespace(ns)
				if err := k8s.Create(context.Background(), &obj, client.DryRunAll); err != nil {
					t.Errorf("%s %s is invalid: %v", obj.GetKind(), obj.GetName(), err)
				}
			}
			want := map[string]int{"Repository": 3, "Group": 1, "PermissionTarget": 1, "Watch": 1}
			if len(observed) > 0 {
				want["Team"] = 1
			}
			for k, n := range want {
				if kinds[k] != n {
					t.Errorf("rendered %d %s, want %d", kinds[k], k, n)
				}
			}
		})
	}
}

func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func compositionTemplate(t *testing.T, path string) string {
	t.Helper()
	comp := readYAML(t, path)
	pipeline, _, _ := unstructured.NestedSlice(comp, "spec", "pipeline")
	for _, step := range pipeline {
		s, _ := step.(map[string]any)
		if tmpl, ok, _ := unstructured.NestedString(s, "input", "inline", "template"); ok {
			return tmpl
		}
	}
	t.Fatal("no inline template in composition")
	return ""
}
