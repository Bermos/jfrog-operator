package connection

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
)

func newFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := platformv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func secret(ns, name string, data map[string]string) *corev1.Secret {
	s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}, Data: map[string][]byte{}}
	for k, v := range data {
		s.Data[k] = []byte(v)
	}
	return s
}

// versionServer answers the version endpoint and records the auth header.
func versionServer(t *testing.T, tlsServer bool) (*httptest.Server, *string) {
	t.Helper()
	var auth string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"version":"7.1.0"}`))
	})
	var srv *httptest.Server
	if tlsServer {
		srv = httptest.NewTLSServer(h)
	} else {
		srv = httptest.NewServer(h)
	}
	t.Cleanup(srv.Close)
	return srv, &auth
}

func TestResolveClusterConnection(t *testing.T) {
	srv, auth := versionServer(t, false)
	kube := newFakeClient(t,
		secret("ops", "creds", map[string]string{"accessToken": "tok"}),
		&platformv1alpha1.ClusterConnection{
			ObjectMeta: metav1.ObjectMeta{Name: "default"},
			Spec: platformv1alpha1.ConnectionSpec{
				URL:         srv.URL,
				Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "creds", Namespace: "ops"}},
			},
		},
	)
	r := &Resolver{Client: kube}
	ctx := context.Background()

	// An empty kind means the defaulted ClusterConnection.
	c, err := r.Resolve(ctx, "app", commonv1alpha1.ConnectionReference{Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ArtifactoryVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if *auth != "Bearer tok" {
		t.Fatalf("auth = %q", *auth)
	}

	// The client is cached while nothing changes.
	again, err := r.Resolve(ctx, "other", commonv1alpha1.ConnectionReference{Kind: "ClusterConnection", Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if again != c {
		t.Fatal("client was not cached")
	}

	// Changing the Secret builds a new client.
	var s corev1.Secret
	if err := kube.Get(ctx, client.ObjectKey{Namespace: "ops", Name: "creds"}, &s); err != nil {
		t.Fatal(err)
	}
	s.Data["accessToken"] = []byte("new")
	if err := kube.Update(ctx, &s); err != nil {
		t.Fatal(err)
	}
	rotated, err := r.Resolve(ctx, "app", commonv1alpha1.ConnectionReference{Name: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if rotated == c {
		t.Fatal("client not rebuilt after the Secret changed")
	}
	if _, err := rotated.ArtifactoryVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if *auth != "Bearer new" {
		t.Fatalf("auth after rotation = %q", *auth)
	}
}

func TestResolveNamespacedConnectionWithBasicAuth(t *testing.T) {
	srv, auth := versionServer(t, false)
	kube := newFakeClient(t,
		secret("app", "creds", map[string]string{"user": "admin", "pass": "pw"}),
		&platformv1alpha1.Connection{
			ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "app"},
			Spec: platformv1alpha1.ConnectionSpec{
				URL: srv.URL,
				Credentials: platformv1alpha1.Credentials{
					SecretRef:   platformv1alpha1.SecretReference{Name: "creds", Namespace: "elsewhere"},
					UsernameKey: "user", PasswordKey: "pass",
				},
			},
		},
	)
	r := &Resolver{Client: kube}
	ctx := context.Background()
	c, err := r.Resolve(ctx, "app", commonv1alpha1.ConnectionReference{Kind: "Connection", Name: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ArtifactoryVersion(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(*auth, "Basic ") {
		t.Fatalf("auth = %q", *auth)
	}

	// The Connection is looked up in the referencing object's namespace only.
	if _, err := r.Resolve(ctx, "other", commonv1alpha1.ConnectionReference{Kind: "Connection", Name: "c"}); err == nil {
		t.Fatal("a Connection from another namespace must not resolve")
	}
}

func TestResolveErrors(t *testing.T) {
	kube := newFakeClient(t,
		secret("ops", "empty", nil),
		&platformv1alpha1.ClusterConnection{
			ObjectMeta: metav1.ObjectMeta{Name: "no-ns"},
			Spec: platformv1alpha1.ConnectionSpec{
				URL:         "https://x",
				Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "creds"}},
			},
		},
		&platformv1alpha1.ClusterConnection{
			ObjectMeta: metav1.ObjectMeta{Name: "empty-secret"},
			Spec: platformv1alpha1.ConnectionSpec{
				URL:         "https://x",
				Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "empty", Namespace: "ops"}},
			},
		},
	)
	r := &Resolver{Client: kube}
	ctx := context.Background()
	for name, tc := range map[string]struct {
		ref  commonv1alpha1.ConnectionReference
		want string
	}{
		"missing connection":   {commonv1alpha1.ConnectionReference{Kind: "ClusterConnection", Name: "nope"}, "getting ClusterConnection"},
		"unknown kind":         {commonv1alpha1.ConnectionReference{Kind: "Other", Name: "x"}, "unsupported connection kind"},
		"no secret namespace":  {commonv1alpha1.ConnectionReference{Name: "no-ns"}, "namespace is required"},
		"secret without creds": {commonv1alpha1.ConnectionReference{Name: "empty-secret"}, "access token or a username and password"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := r.Resolve(ctx, "app", tc.ref)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestResolveCustomCA(t *testing.T) {
	srv, _ := versionServer(t, true)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	conn := func(name string, tls *platformv1alpha1.TLSConfig) *platformv1alpha1.ClusterConnection {
		return &platformv1alpha1.ClusterConnection{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: platformv1alpha1.ConnectionSpec{
				URL:         srv.URL,
				Credentials: platformv1alpha1.Credentials{SecretRef: platformv1alpha1.SecretReference{Name: "creds", Namespace: "ops"}},
				TLS:         tls,
			},
		}
	}
	kube := newFakeClient(t,
		secret("ops", "creds", map[string]string{"accessToken": "tok"}),
		secret("pki", "ca", map[string]string{"ca.crt": string(caPEM), "bad": "not a cert"}),
		conn("untrusted", nil),
		conn("trusted", &platformv1alpha1.TLSConfig{CASecretRef: &platformv1alpha1.SecretKeyReference{
			SecretReference: platformv1alpha1.SecretReference{Name: "ca", Namespace: "pki"},
		}}),
		conn("insecure", &platformv1alpha1.TLSConfig{InsecureSkipVerify: true}),
		conn("bad-ca", &platformv1alpha1.TLSConfig{CASecretRef: &platformv1alpha1.SecretKeyReference{
			SecretReference: platformv1alpha1.SecretReference{Name: "ca", Namespace: "pki"}, Key: "bad",
		}}),
		conn("missing-key", &platformv1alpha1.TLSConfig{CASecretRef: &platformv1alpha1.SecretKeyReference{
			SecretReference: platformv1alpha1.SecretReference{Name: "ca", Namespace: "pki"}, Key: "nope",
		}}),
	)
	r := &Resolver{Client: kube}
	ctx := context.Background()

	c, err := r.Resolve(ctx, "", commonv1alpha1.ConnectionReference{Name: "untrusted"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.ArtifactoryVersion(ctx)
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("want certificate error without CA, got %v", err)
	}

	for _, name := range []string{"trusted", "insecure"} {
		c, err := r.Resolve(ctx, "", commonv1alpha1.ConnectionReference{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.ArtifactoryVersion(ctx); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	if _, err := r.Resolve(ctx, "", commonv1alpha1.ConnectionReference{Name: "bad-ca"}); err == nil || !strings.Contains(err.Error(), "no valid PEM") {
		t.Fatalf("bad CA: got %v", err)
	}
	if _, err := r.Resolve(ctx, "", commonv1alpha1.ConnectionReference{Name: "missing-key"}); err == nil || !strings.Contains(err.Error(), "has no key") {
		t.Fatalf("missing CA key: got %v", err)
	}
}
