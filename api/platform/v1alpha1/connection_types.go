package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConnectionSpec describes how to reach a self-hosted JFrog Platform.
type ConnectionSpec struct {
	// URL is the base URL of the JFrog Platform, e.g. https://jfrog.example.com.
	// Product URLs default to <url>/artifactory, <url>/access and <url>/xray.
	// +kubebuilder:validation:Pattern=`^https?://`
	URL string `json:"url"`

	// ArtifactoryURL overrides the Artifactory base URL.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +optional
	ArtifactoryURL string `json:"artifactoryURL,omitempty"`

	// AccessURL overrides the Access base URL.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +optional
	AccessURL string `json:"accessURL,omitempty"`

	// XrayURL overrides the Xray base URL.
	// +kubebuilder:validation:Pattern=`^https?://`
	// +optional
	XrayURL string `json:"xrayURL,omitempty"`

	// Credentials used to authenticate against the platform.
	Credentials Credentials `json:"credentials"`

	// TLS settings for talking to the platform.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`
}

// Credentials points to a Secret holding either an access token or a
// username and password. An access token takes precedence.
type Credentials struct {
	// SecretRef is the Secret holding the credentials.
	SecretRef SecretReference `json:"secretRef"`

	// AccessTokenKey is the Secret key holding an access token.
	// +kubebuilder:default=accessToken
	// +optional
	AccessTokenKey string `json:"accessTokenKey,omitempty"`

	// UsernameKey is the Secret key holding the username for basic auth.
	// +kubebuilder:default=username
	// +optional
	UsernameKey string `json:"usernameKey,omitempty"`

	// PasswordKey is the Secret key holding the password for basic auth.
	// +kubebuilder:default=password
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`
}

// SecretReference references a Secret. For a namespaced Connection the
// namespace is always the Connection's own and Namespace is ignored.
type SecretReference struct {
	// Name of the Secret.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Namespace of the Secret. Required for a ClusterConnection.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// TLSConfig configures TLS towards the platform.
type TLSConfig struct {
	// InsecureSkipVerify disables certificate verification. Do not use in production.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// CASecretRef points to a Secret key holding PEM encoded CA certificates
	// to trust in addition to the system roots.
	// +optional
	CASecretRef *SecretKeyReference `json:"caSecretRef,omitempty"`
}

// SecretKeyReference selects a key of a Secret.
type SecretKeyReference struct {
	SecretReference `json:",inline"`
	// Key within the Secret.
	// +kubebuilder:default=ca.crt
	// +optional
	Key string `json:"key,omitempty"`
}

// ConnectionStatus reports whether the platform is reachable.
type ConnectionStatus struct {
	// Conditions of the connection. Ready is True when Artifactory answered.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ArtifactoryVersion as reported by Artifactory.
	// +optional
	ArtifactoryVersion string `json:"artifactoryVersion,omitempty"`

	// XrayVersion as reported by Xray. Empty when Xray is not reachable.
	// +optional
	XrayVersion string `json:"xrayVersion,omitempty"`

	// LastCheckTime is when the connection was last checked.
	// +optional
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=jfrog
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.spec.url`
// +kubebuilder:printcolumn:name="Artifactory",type=string,JSONPath=`.status.artifactoryVersion`
// +kubebuilder:printcolumn:name="Xray",type=string,JSONPath=`.status.xrayVersion`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Connection holds the URL and credentials of a JFrog Platform. Resources in
// the same namespace can reference it.
type Connection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConnectionSpec   `json:"spec"`
	Status ConnectionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConnectionList contains a list of Connection.
type ConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Connection `json:"items"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories=jfrog
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.spec.url`
// +kubebuilder:printcolumn:name="Artifactory",type=string,JSONPath=`.status.artifactoryVersion`
// +kubebuilder:printcolumn:name="Xray",type=string,JSONPath=`.status.xrayVersion`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterConnection holds the URL and credentials of a JFrog Platform.
// Resources in any namespace can reference it.
// +kubebuilder:validation:XValidation:rule="has(self.spec.credentials.secretRef.__namespace__)",message="spec.credentials.secretRef.namespace is required for a ClusterConnection"
type ClusterConnection struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConnectionSpec   `json:"spec"`
	Status ConnectionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterConnectionList contains a list of ClusterConnection.
type ClusterConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterConnection `json:"items"`
}

func init() {
	knownTypes = append(knownTypes, &Connection{}, &ConnectionList{}, &ClusterConnection{}, &ClusterConnectionList{})
}
