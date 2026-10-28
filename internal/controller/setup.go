// Package controller wires all controllers into a manager.
package controller

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	artifactoryv1alpha1 "github.com/Bermos/jfrog-operator/api/artifactory/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/connection"
	"github.com/Bermos/jfrog-operator/internal/controller/artifactory"
	"github.com/Bermos/jfrog-operator/internal/controller/platform"
	"github.com/Bermos/jfrog-operator/internal/reconciler"
)

// Options configures the controllers.
type Options struct {
	// SyncInterval is how often in-sync resources are compared against JFrog.
	SyncInterval time.Duration
	// CheckInterval is how often connections are checked.
	CheckInterval time.Duration
	// HTTPTimeout is the timeout of each request to JFrog.
	HTTPTimeout time.Duration
}

// ClientOptions returns the manager client options the controllers expect.
// Secrets are read directly from the API server instead of being cached, so
// the operator neither needs to list and watch every Secret in the cluster
// nor keep them all in memory.
func ClientOptions() client.Options {
	return client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}}}}
}

// Setup registers all controllers with the manager.
func Setup(mgr ctrl.Manager, opts Options) error {
	resolver := &connection.Resolver{Client: mgr.GetClient(), HTTPTimeout: opts.HTTPTimeout}
	recorder := mgr.GetEventRecorder("jfrog-operator")

	if err := (&platform.ConnectionReconciler{
		Client: mgr.GetClient(), Resolver: resolver, CheckInterval: opts.CheckInterval,
	}).SetupWithManager(mgr); err != nil {
		return err
	}
	if err := (&platform.ClusterConnectionReconciler{
		Client: mgr.GetClient(), Resolver: resolver, CheckInterval: opts.CheckInterval,
	}).SetupWithManager(mgr); err != nil {
		return err
	}
	return artifactory.SetupRepository(mgr, &reconciler.Reconciler[*artifactoryv1alpha1.Repository]{
		Client:       mgr.GetClient(),
		Resolver:     resolver,
		Recorder:     recorder,
		SyncInterval: opts.SyncInterval,
	})
}
