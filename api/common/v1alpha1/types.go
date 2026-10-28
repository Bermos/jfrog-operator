// Package v1alpha1 contains API types shared by every managed JFrog resource.
// +kubebuilder:object:generate=true
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition types. They follow the Crossplane conventions so that Crossplane
// compositions (e.g. function-auto-ready) can derive readiness from them.
const (
	// ConditionReady is True when the resource exists in JFrog and is usable.
	ConditionReady = "Ready"
	// ConditionSynced is True when the last reconcile succeeded.
	ConditionSynced = "Synced"
)

// Condition reasons.
const (
	ReasonAvailable        = "Available"
	ReasonCreating         = "Creating"
	ReasonDeleting         = "Deleting"
	ReasonUnavailable      = "Unavailable"
	ReasonNotFound         = "NotFound"
	ReasonReconcileSuccess = "ReconcileSuccess"
	ReasonReconcileError   = "ReconcileError"
	ReasonConnectionError  = "ConnectionError"
	ReasonAlreadyExists    = "AlreadyExists"
)

// Connection kinds that can be referenced by a managed resource.
const (
	ConnectionKind        = "Connection"
	ClusterConnectionKind = "ClusterConnection"
	DefaultConnectionName = "default"
)

// ConnectionReference points to the Connection or ClusterConnection that holds
// the URL and credentials of the JFrog Platform to manage the resource in.
type ConnectionReference struct {
	// Kind of the referenced connection. A namespaced Connection must live in
	// the same namespace as the referencing resource.
	// +kubebuilder:validation:Enum=Connection;ClusterConnection
	// +kubebuilder:default=ClusterConnection
	// +optional
	Kind string `json:"kind,omitempty"`

	// Name of the referenced connection.
	// +kubebuilder:default=default
	// +optional
	Name string `json:"name,omitempty"`
}

// DeletionPolicy decides what happens in JFrog when the Kubernetes object is deleted.
// +kubebuilder:validation:Enum=Delete;Orphan
type DeletionPolicy string

const (
	// DeletionPolicyDelete deletes the resource in JFrog.
	DeletionPolicyDelete DeletionPolicy = "Delete"
	// DeletionPolicyOrphan leaves the resource in JFrog untouched.
	DeletionPolicyOrphan DeletionPolicy = "Orphan"
)

// ManagementPolicy decides whether the operator writes to JFrog at all.
// +kubebuilder:validation:Enum=Manage;Observe
type ManagementPolicy string

const (
	// ManagementPolicyManage creates, updates and (subject to the deletion
	// policy) deletes the resource in JFrog.
	ManagementPolicyManage ManagementPolicy = "Manage"
	// ManagementPolicyObserve only reports whether the resource exists and
	// matches the spec. Nothing is ever written to JFrog.
	ManagementPolicyObserve ManagementPolicy = "Observe"
)

// ResourceSpec holds the fields every managed JFrog resource shares.
type ResourceSpec struct {
	// ConnectionRef selects the JFrog Platform to manage this resource in.
	// Defaults to the ClusterConnection named "default".
	// +optional
	ConnectionRef *ConnectionReference `json:"connectionRef,omitempty"`

	// DeletionPolicy decides whether the resource is deleted in JFrog when
	// this object is deleted.
	// +kubebuilder:default=Delete
	// +optional
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// ManagementPolicy decides whether the operator may write to JFrog.
	// +kubebuilder:default=Manage
	// +optional
	ManagementPolicy ManagementPolicy `json:"managementPolicy,omitempty"`

	// AdoptExisting allows the operator to take over a resource that already
	// exists in JFrog but was not created by this object. Without it the
	// operator refuses to touch pre-existing resources.
	// +optional
	AdoptExisting bool `json:"adoptExisting,omitempty"`
}

// ResourceStatus holds the status fields every managed JFrog resource shares.
type ResourceStatus struct {
	// Conditions of the resource. Ready and Synced are always reported.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ExternalName is the identifier of the resource in JFrog once this object
	// has created or adopted it.
	// +optional
	ExternalName string `json:"externalName,omitempty"`

	// LastSyncTime is when the resource was last compared against JFrog.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
}

// SecretKeySelector selects a key of a Secret in the same namespace as the
// referencing object.
type SecretKeySelector struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Key within the Secret.
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// GetConnectionRef returns the connection reference with defaults applied.
func (s *ResourceSpec) GetConnectionRef() ConnectionReference {
	ref := ConnectionReference{Kind: ClusterConnectionKind, Name: DefaultConnectionName}
	if s.ConnectionRef != nil {
		if s.ConnectionRef.Kind != "" {
			ref.Kind = s.ConnectionRef.Kind
		}
		if s.ConnectionRef.Name != "" {
			ref.Name = s.ConnectionRef.Name
		}
	}
	return ref
}

// ShouldManage reports whether the operator may write to JFrog.
func (s *ResourceSpec) ShouldManage() bool {
	return s.ManagementPolicy != ManagementPolicyObserve
}

// ShouldDelete reports whether the resource is deleted in JFrog together
// with the Kubernetes object.
func (s *ResourceSpec) ShouldDelete() bool {
	return s.ShouldManage() && s.DeletionPolicy != DeletionPolicyOrphan
}
