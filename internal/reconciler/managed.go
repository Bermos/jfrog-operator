// Package reconciler contains a generic reconciler for Kubernetes objects
// that represent a resource in JFrog. Each kind only implements how to
// observe, create, update and delete its resource; the lifecycle (finalizers,
// conditions, adoption, deletion and management policies, drift detection)
// lives here so that every kind behaves the same.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
	"github.com/Bermos/jfrog-operator/internal/jfrog"
)

const (
	// Finalizer is added to every managed object so that the resource in
	// JFrog can be cleaned up before the object goes away.
	Finalizer = "jfrog.bermos.io/finalizer"

	// AnnotationCreatePending is set just before the operator creates a
	// resource in JFrog. If the operator dies after creating the resource but
	// before recording it in the status, the annotation tells the next
	// reconcile that the existing resource is its own.
	AnnotationCreatePending = "jfrog.bermos.io/create-pending"

	// DefaultSyncInterval is how often in-sync resources are compared against JFrog.
	DefaultSyncInterval = 10 * time.Minute
)

// Object is a Kubernetes object managed by this reconciler.
type Object interface {
	client.Object
	GetResourceSpec() *commonv1alpha1.ResourceSpec
	GetResourceStatus() *commonv1alpha1.ResourceStatus
}

// Observation is what an External found in JFrog.
type Observation struct {
	// Exists is true when the resource exists in JFrog.
	Exists bool
	// UpToDate is true when the resource matches the spec.
	UpToDate bool
	// Diff optionally describes what differs, for events and logs.
	Diff string
}

// External manages one resource in JFrog on behalf of one object. It may
// write to the object's status (e.g. observed URLs); the reconciler persists
// the status afterwards.
type External interface {
	// Name returns the identifier of the resource in JFrog.
	Name() string
	Observe(ctx context.Context) (Observation, error)
	Create(ctx context.Context) error
	Update(ctx context.Context) error
	Delete(ctx context.Context) error
}

// ConnectFunc builds the External for obj using a client for its connection.
type ConnectFunc[T Object] func(ctx context.Context, kube client.Client, jc *jfrog.Client, obj T) (External, error)

// ClientResolver resolves connection references to JFrog clients.
type ClientResolver interface {
	Resolve(ctx context.Context, namespace string, ref commonv1alpha1.ConnectionReference) (*jfrog.Client, error)
}

// Reconciler reconciles objects of type T.
type Reconciler[T Object] struct {
	Client   client.Client
	Resolver ClientResolver
	Recorder events.EventRecorder
	// NewObject returns an empty T.
	NewObject func() T
	Connect   ConnectFunc[T]
	// SyncInterval is how often in-sync resources are re-checked for drift.
	SyncInterval time.Duration
}

// Reconcile implements reconcile.Reconciler.
func (r *Reconciler[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	obj := r.NewObject()
	if err := r.Client.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	spec := obj.GetResourceSpec()

	deleting := !obj.GetDeletionTimestamp().IsZero()
	if deleting && !controllerutil.ContainsFinalizer(obj, Finalizer) {
		return ctrl.Result{}, nil
	}

	// Nothing to clean up in JFrog: drop the finalizer without connecting,
	// so that a broken connection never blocks deletion of such objects.
	neverCreated := obj.GetResourceStatus().ExternalName == "" && obj.GetAnnotations()[AnnotationCreatePending] == ""
	if deleting && (!spec.ShouldDelete() || neverCreated) {
		return ctrl.Result{}, r.removeFinalizer(ctx, obj)
	}

	jc, err := r.Resolver.Resolve(ctx, obj.GetNamespace(), spec.GetConnectionRef())
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, obj, commonv1alpha1.ReasonConnectionError, err)
	}
	ext, err := r.Connect(ctx, r.Client, jc, obj)
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, obj, commonv1alpha1.ReasonReconcileError, err)
	}

	if deleting {
		return ctrl.Result{}, r.delete(ctx, obj, ext)
	}

	if spec.ShouldManage() && !controllerutil.ContainsFinalizer(obj, Finalizer) {
		controllerutil.AddFinalizer(obj, Finalizer)
		if err := r.Client.Update(ctx, obj); err != nil {
			return ctrl.Result{}, err
		}
	}

	obs, err := ext.Observe(ctx)
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, obj, commonv1alpha1.ReasonReconcileError, fmt.Errorf("observing %s: %w", ext.Name(), err))
	}
	status := obj.GetResourceStatus()

	if !spec.ShouldManage() {
		if !obs.Exists {
			r.setConditions(obj, metav1.ConditionFalse, commonv1alpha1.ReasonNotFound, fmt.Sprintf("%s does not exist", ext.Name()),
				metav1.ConditionTrue, commonv1alpha1.ReasonReconcileSuccess, "")
			return r.done(ctx, obj)
		}
		msg := ""
		if !obs.UpToDate {
			msg = "resource differs from spec: " + obs.Diff
		}
		status.ExternalName = ext.Name()
		r.setConditions(obj, metav1.ConditionTrue, commonv1alpha1.ReasonAvailable, msg,
			metav1.ConditionTrue, commonv1alpha1.ReasonReconcileSuccess, "")
		return r.done(ctx, obj)
	}

	// A resource we never created or adopted already exists in JFrog.
	ours := status.ExternalName == ext.Name() || obj.GetAnnotations()[AnnotationCreatePending] == ext.Name()
	if obs.Exists && !ours && !spec.AdoptExisting {
		err := fmt.Errorf("%s already exists in JFrog and is not managed by this object; set spec.adoptExisting to take it over", ext.Name())
		return ctrl.Result{}, r.fail(ctx, obj, commonv1alpha1.ReasonAlreadyExists, err)
	}
	if status.ExternalName != "" && status.ExternalName != ext.Name() {
		err := fmt.Errorf("object manages %s but spec now names %s; renaming is not supported", status.ExternalName, ext.Name())
		return ctrl.Result{}, r.fail(ctx, obj, commonv1alpha1.ReasonReconcileError, err)
	}

	switch {
	case !obs.Exists:
		if err := r.setAnnotation(ctx, obj, AnnotationCreatePending, ext.Name()); err != nil {
			return ctrl.Result{}, err
		}
		if err := ext.Create(ctx); err != nil {
			return ctrl.Result{}, r.fail(ctx, obj, commonv1alpha1.ReasonReconcileError, fmt.Errorf("creating %s: %w", ext.Name(), err))
		}
		logger.Info("created resource in JFrog", "name", ext.Name())
		r.event(obj, "Normal", "Created", "Created %s", ext.Name())
	case !obs.UpToDate:
		if err := ext.Update(ctx); err != nil {
			return ctrl.Result{}, r.fail(ctx, obj, commonv1alpha1.ReasonReconcileError, fmt.Errorf("updating %s: %w", ext.Name(), err))
		}
		logger.Info("updated resource in JFrog", "name", ext.Name(), "diff", obs.Diff)
		r.event(obj, "Normal", "Updated", "Updated %s: %s", ext.Name(), obs.Diff)
	}

	status.ExternalName = ext.Name()
	r.setConditions(obj, metav1.ConditionTrue, commonv1alpha1.ReasonAvailable, "",
		metav1.ConditionTrue, commonv1alpha1.ReasonReconcileSuccess, "")
	res, err := r.done(ctx, obj)
	if err != nil {
		return res, err
	}
	return res, r.setAnnotation(ctx, obj, AnnotationCreatePending, "")
}

func (r *Reconciler[T]) delete(ctx context.Context, obj T, ext External) error {
	r.setConditions(obj, metav1.ConditionFalse, commonv1alpha1.ReasonDeleting, "",
		metav1.ConditionTrue, commonv1alpha1.ReasonReconcileSuccess, "")
	if err := ext.Delete(ctx); err != nil && !jfrog.IsNotFound(err) {
		return r.fail(ctx, obj, commonv1alpha1.ReasonReconcileError, fmt.Errorf("deleting %s: %w", ext.Name(), err))
	}
	log.FromContext(ctx).Info("deleted resource in JFrog", "name", ext.Name())
	r.event(obj, "Normal", "Deleted", "Deleted %s", ext.Name())
	return r.removeFinalizer(ctx, obj)
}

func (r *Reconciler[T]) removeFinalizer(ctx context.Context, obj T) error {
	if !controllerutil.RemoveFinalizer(obj, Finalizer) {
		return nil
	}
	return client.IgnoreNotFound(r.Client.Update(ctx, obj))
}

func (r *Reconciler[T]) setAnnotation(ctx context.Context, obj T, key, value string) error {
	ann := obj.GetAnnotations()
	if ann[key] == value {
		return nil
	}
	patch := client.MergeFrom(obj.DeepCopyObject().(T))
	if value == "" {
		delete(ann, key)
	} else {
		if ann == nil {
			ann = map[string]string{}
		}
		ann[key] = value
	}
	obj.SetAnnotations(ann)
	return r.Client.Patch(ctx, obj, patch)
}

// done persists a successful status and schedules the next drift check.
func (r *Reconciler[T]) done(ctx context.Context, obj T) (ctrl.Result, error) {
	status := obj.GetResourceStatus()
	now := metav1.Now()
	status.LastSyncTime = &now
	status.ObservedGeneration = obj.GetGeneration()
	if err := r.Client.Status().Update(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}
	interval := r.SyncInterval
	if interval == 0 {
		interval = DefaultSyncInterval
	}
	return ctrl.Result{RequeueAfter: interval}, nil
}

// fail records err in the Synced condition and returns it so that the
// request is retried with backoff.
func (r *Reconciler[T]) fail(ctx context.Context, obj T, reason string, cause error) error {
	status := obj.GetResourceStatus()
	ready := meta.FindStatusCondition(status.Conditions, commonv1alpha1.ConditionReady)
	if ready == nil {
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: commonv1alpha1.ConditionReady, Status: metav1.ConditionFalse,
			Reason: commonv1alpha1.ReasonUnavailable, ObservedGeneration: obj.GetGeneration(),
		})
	}
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type: commonv1alpha1.ConditionSynced, Status: metav1.ConditionFalse,
		Reason: reason, Message: truncate(cause.Error()), ObservedGeneration: obj.GetGeneration(),
	})
	status.ObservedGeneration = obj.GetGeneration()
	r.event(obj, "Warning", reason, "%s", truncate(cause.Error()))
	if err := r.Client.Status().Update(ctx, obj); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (r *Reconciler[T]) setConditions(obj T, ready metav1.ConditionStatus, readyReason, readyMsg string,
	synced metav1.ConditionStatus, syncedReason, syncedMsg string) {
	status := obj.GetResourceStatus()
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type: commonv1alpha1.ConditionReady, Status: ready, Reason: readyReason,
		Message: readyMsg, ObservedGeneration: obj.GetGeneration(),
	})
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type: commonv1alpha1.ConditionSynced, Status: synced, Reason: syncedReason,
		Message: syncedMsg, ObservedGeneration: obj.GetGeneration(),
	})
}

func (r *Reconciler[T]) event(obj T, eventType, reason, format string, args ...any) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(obj, nil, eventType, reason, reason, format, args...)
}

func truncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1024 {
		return s[:1024] + "..."
	}
	return s
}
