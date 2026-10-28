// Package platform contains the controllers for platform-wide resources.
package platform

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	platformv1alpha1 "github.com/Bermos/jfrog-operator/api/platform/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/connection"
	"github.com/Bermos/jfrog-operator/internal/jfrog"
)

// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=connections;clusterconnections,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.jfrog.bermos.io,resources=connections/status;clusterconnections/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// DefaultCheckInterval is how often connections are re-checked.
const DefaultCheckInterval = 5 * time.Minute

// ConnectionReconciler checks that a Connection can reach its platform.
type ConnectionReconciler struct {
	Client        client.Client
	Resolver      *connection.Resolver
	CheckInterval time.Duration
}

// SetupWithManager registers the controller with the manager.
func (r *ConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Connection{}).
		Named("connection").
		Complete(r)
}

// Reconcile implements reconcile.Reconciler.
func (r *ConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var conn platformv1alpha1.Connection
	if err := r.Client.Get(ctx, req.NamespacedName, &conn); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	jc, err := r.Resolver.ForConnection(ctx, &conn)
	check(ctx, jc, err, &conn.Status, conn.Generation)
	if err := r.Client.Status().Update(ctx, &conn); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: interval(r.CheckInterval)}, nil
}

// ClusterConnectionReconciler checks that a ClusterConnection can reach its platform.
type ClusterConnectionReconciler struct {
	Client        client.Client
	Resolver      *connection.Resolver
	CheckInterval time.Duration
}

// SetupWithManager registers the controller with the manager.
func (r *ClusterConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ClusterConnection{}).
		Named("clusterconnection").
		Complete(r)
}

// Reconcile implements reconcile.Reconciler.
func (r *ClusterConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var conn platformv1alpha1.ClusterConnection
	if err := r.Client.Get(ctx, req.NamespacedName, &conn); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	jc, err := r.Resolver.ForClusterConnection(ctx, &conn)
	check(ctx, jc, err, &conn.Status, conn.Generation)
	if err := r.Client.Status().Update(ctx, &conn); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: interval(r.CheckInterval)}, nil
}

// check fills status from talking to the platform. Failures are reported in
// the Ready condition rather than returned, so that a broken connection is
// re-checked on the regular interval instead of with tight backoff.
func check(ctx context.Context, jc *jfrog.Client, buildErr error, status *platformv1alpha1.ConnectionStatus, generation int64) {
	now := metav1.Now()
	status.LastCheckTime = &now
	status.ObservedGeneration = generation
	cond := metav1.Condition{Type: commonv1alpha1.ConditionReady, ObservedGeneration: generation}

	if buildErr != nil {
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, commonv1alpha1.ReasonConnectionError, buildErr.Error()
		meta.SetStatusCondition(&status.Conditions, cond)
		return
	}
	version, err := jc.ArtifactoryVersion(ctx)
	if err != nil {
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, commonv1alpha1.ReasonUnavailable, err.Error()
		meta.SetStatusCondition(&status.Conditions, cond)
		status.ArtifactoryVersion, status.XrayVersion = "", ""
		return
	}
	status.ArtifactoryVersion = version
	cond.Status, cond.Reason, cond.Message = metav1.ConditionTrue, commonv1alpha1.ReasonAvailable, ""

	// Xray is optional; its absence does not make the connection unready.
	xray, err := jc.XrayVersion(ctx)
	if err != nil {
		log.FromContext(ctx).V(1).Info("Xray is not reachable", "error", err.Error())
		cond.Message = "Xray is not reachable: " + err.Error()
	}
	status.XrayVersion = xray
	meta.SetStatusCondition(&status.Conditions, cond)
}

func interval(d time.Duration) time.Duration {
	if d == 0 {
		return DefaultCheckInterval
	}
	return d
}
