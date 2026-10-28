package controller_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	artifactoryv1alpha1 "github.com/Bermos/jfrog-operator/api/artifactory/v1alpha1"
	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/controller"
	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
)

const (
	// operatorNS holds the credentials of the default ClusterConnection.
	operatorNS = "jfrog-system"
	// syncInterval is short so that drift correction can be tested.
	syncInterval = 2 * time.Second
	timeout      = 30 * time.Second
)

var (
	k8s    client.Client
	jfrogs *fake.Server
)

// TestMain starts a real API server (envtest) with the operator's CRDs, a
// fake JFrog Platform, and the operator itself.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		fmt.Fprintln(os.Stderr, "KUBEBUILDER_ASSETS is not set; run the tests with `make test`")
		return 1
	}
	ctrl.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr), zap.Level(zapcore.Level(zapLevel()))))

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "starting envtest:", err)
		return 1
	}
	defer func() { _ = env.Stop() }()

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme, platformv1alpha1.AddToScheme, artifactoryv1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	jfrogs = fake.NewServer()
	defer jfrogs.Close()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme,
		Client:  controller.ClientOptions(),
		Metrics: metricsserver.Options{BindAddress: "0"},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := controller.Setup(mgr, controller.Options{
		SyncInterval: syncInterval, CheckInterval: syncInterval, HTTPTimeout: 5 * time.Second,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		if err := mgr.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "manager stopped:", err)
		}
	}()

	k8s, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := createDefaultConnection(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

func zapLevel() int8 {
	if os.Getenv("TEST_VERBOSE_LOGS") != "" {
		return -1
	}
	return 5 // effectively silences the operator's logs
}

func createDefaultConnection(ctx context.Context) error {
	if err := k8s.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorNS}}); err != nil {
		return err
	}
	if err := k8s.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "jfrog-credentials", Namespace: operatorNS},
		StringData: map[string]string{"accessToken": fake.Token},
	}); err != nil {
		return err
	}
	return k8s.Create(ctx, &platformv1alpha1.ClusterConnection{
		ObjectMeta: metav1.ObjectMeta{Name: commonv1alpha1.DefaultConnectionName},
		Spec: platformv1alpha1.ConnectionSpec{
			URL: jfrogs.URL,
			Credentials: platformv1alpha1.Credentials{
				SecretRef: platformv1alpha1.SecretReference{Name: "jfrog-credentials", Namespace: operatorNS},
			},
		},
	})
}

// newNamespace creates a namespace for one test, so tests do not interfere.
func newNamespace(t *testing.T) string {
	t.Helper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-"}}
	if err := k8s.Create(context.Background(), ns); err != nil {
		t.Fatal(err)
	}
	return ns.Name
}

// eventually retries f until it returns nil or the timeout expires.
func eventually(t *testing.T, f func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := f()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s: %v", timeout, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// consistently checks that f keeps returning nil for d.
func consistently(t *testing.T, d time.Duration, f func() error) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if err := f(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// conditionIs returns nil when obj has the given condition status (and reason, if not empty).
func conditionIs(conds []metav1.Condition, condType string, status metav1.ConditionStatus, reason string) error {
	c := meta.FindStatusCondition(conds, condType)
	if c == nil {
		return fmt.Errorf("condition %s not set", condType)
	}
	if c.Status != status || (reason != "" && c.Reason != reason) {
		return fmt.Errorf("condition %s is %s/%s (%s), want %s/%s", condType, c.Status, c.Reason, c.Message, status, reason)
	}
	return nil
}

func get(t *testing.T, obj client.Object) error {
	t.Helper()
	return k8s.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
}
