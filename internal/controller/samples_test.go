package controller_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestSamplesAreValid creates every sample with a server-side dry run, so the
// samples cannot drift from the CRD schemas.
func TestSamplesAreValid(t *testing.T) {
	ctx := context.Background()
	files, err := filepath.Glob(filepath.Join("..", "..", "config", "samples", "*_v1alpha1_*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no samples found: %v", err)
	}
	_ = k8s.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "team-a"}})
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
		for {
			var obj unstructured.Unstructured
			if err := dec.Decode(&obj.Object); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatalf("%s: %v", file, err)
			}
			if len(obj.Object) == 0 {
				continue
			}
			if obj.GetNamespace() == operatorNS || obj.GetKind() == "Secret" {
				// Secrets are not interesting, and the operator namespace already has them.
				continue
			}
			// Avoid clashing with objects the suite created, e.g. the default ClusterConnection.
			obj.SetName("sample-" + obj.GetName())
			if err := k8s.Create(ctx, &obj, client.DryRunAll); err != nil {
				t.Errorf("%s: %s %s is invalid: %v", filepath.Base(file), obj.GetKind(), obj.GetName(), err)
			}
		}
	}
}
