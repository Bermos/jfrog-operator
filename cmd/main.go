// Command manager runs the JFrog operator.
package main

import (
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	artifactoryv1alpha1 "github.com/Bermos/jfrog-operator/api/artifactory/v1alpha1"
	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	xrayv1alpha1 "github.com/Bermos/jfrog-operator/api/xray/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/controller"
	"github.com/Bermos/jfrog-operator/internal/controller/platform"
	"github.com/Bermos/jfrog-operator/internal/reconciler"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(platformv1alpha1.AddToScheme(scheme))
	utilruntime.Must(artifactoryv1alpha1.AddToScheme(scheme))
	utilruntime.Must(xrayv1alpha1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		enableLeaderElection bool
		syncInterval         time.Duration
		checkInterval        time.Duration
		httpTimeout          time.Duration
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metrics endpoint binds to. Use 0 to disable.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election to run more than one replica.")
	flag.DurationVar(&syncInterval, "sync-interval", reconciler.DefaultSyncInterval, "How often in-sync resources are compared against JFrog to detect drift.")
	flag.DurationVar(&checkInterval, "connection-check-interval", platform.DefaultCheckInterval, "How often connections are checked.")
	flag.DurationVar(&httpTimeout, "http-timeout", 30*time.Second, "Timeout of each request to JFrog.")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Client:                 controller.ClientOptions(),
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "jfrog-operator.jfrog.bermos.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if err := controller.Setup(mgr, controller.Options{
		SyncInterval:  syncInterval,
		CheckInterval: checkInterval,
		HTTPTimeout:   httpTimeout,
	}); err != nil {
		setupLog.Error(err, "unable to set up controllers")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
