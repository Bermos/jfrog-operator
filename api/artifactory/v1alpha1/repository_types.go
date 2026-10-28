package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
)

// Repository classes.
const (
	RClassLocal     = "local"
	RClassRemote    = "remote"
	RClassVirtual   = "virtual"
	RClassFederated = "federated"
)

// RepositorySpec is the desired configuration of an Artifactory repository.
// The typed fields cover what most repositories need. Anything else
// Artifactory accepts in its repository configuration JSON can be set in
// config, and typed fields win where both are set.
// +kubebuilder:validation:XValidation:rule="has(self.key) == has(oldSelf.key) && (!has(self.key) || self.key == oldSelf.key)",message="key is immutable"
// +kubebuilder:validation:XValidation:rule="self.rclass == oldSelf.rclass",message="rclass is immutable"
// +kubebuilder:validation:XValidation:rule="self.packageType == oldSelf.packageType",message="packageType is immutable"
// +kubebuilder:validation:XValidation:rule="self.rclass != 'remote' || has(self.url)",message="url is required for remote repositories"
// +kubebuilder:validation:XValidation:rule="!has(self.passwordSecretRef) || self.rclass == 'remote'",message="passwordSecretRef is only valid for remote repositories"
type RepositorySpec struct {
	commonv1alpha1.ResourceSpec `json:",inline"`

	// Key of the repository in Artifactory. Defaults to metadata.name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._-]*$`
	// +optional
	Key string `json:"key,omitempty"`

	// RClass is the repository class.
	// +kubebuilder:validation:Enum=local;remote;virtual;federated
	RClass string `json:"rclass"`

	// PackageType of the repository, e.g. maven, npm, docker, helm, generic.
	// +kubebuilder:validation:MinLength=1
	PackageType string `json:"packageType"`

	// Description of the repository.
	// +optional
	Description string `json:"description,omitempty"`

	// Notes are internal notes on the repository.
	// +optional
	Notes string `json:"notes,omitempty"`

	// IncludesPattern lists the artifact patterns to include, comma separated.
	// +optional
	IncludesPattern string `json:"includesPattern,omitempty"`

	// ExcludesPattern lists the artifact patterns to exclude, comma separated.
	// +optional
	ExcludesPattern string `json:"excludesPattern,omitempty"`

	// RepoLayoutRef is the name of the repository layout.
	// +optional
	RepoLayoutRef string `json:"repoLayoutRef,omitempty"`

	// ProjectKey assigns the repository to a JFrog project.
	// +optional
	ProjectKey string `json:"projectKey,omitempty"`

	// Environments the repository belongs to, e.g. DEV or PROD.
	// +optional
	Environments []string `json:"environments,omitempty"`

	// XrayIndex enables indexing by Xray.
	// +optional
	XrayIndex *bool `json:"xrayIndex,omitempty"`

	// URL of the upstream for remote repositories.
	// +optional
	URL string `json:"url,omitempty"`

	// Username for the upstream of remote repositories.
	// +optional
	Username string `json:"username,omitempty"`

	// PasswordSecretRef selects the password for the upstream of remote repositories.
	// +optional
	PasswordSecretRef *commonv1alpha1.SecretKeySelector `json:"passwordSecretRef,omitempty"`

	// Repositories aggregated by a virtual repository, in resolution order.
	// +optional
	Repositories []string `json:"repositories,omitempty"`

	// DefaultDeploymentRepo of a virtual repository.
	// +optional
	DefaultDeploymentRepo string `json:"defaultDeploymentRepo,omitempty"`

	// Members of a federated repository.
	// +optional
	Members []FederatedMember `json:"members,omitempty"`

	// Config holds additional repository configuration as accepted by the
	// Artifactory repository configuration API, e.g. {"handleSnapshots": false}.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
}

// FederatedMember is a member of a federated repository.
type FederatedMember struct {
	// URL of the member repository, e.g. https://other.example.com/artifactory/my-repo.
	URL string `json:"url"`
	// Enabled toggles the member.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// RepositoryStatus is the observed state of a Repository.
type RepositoryStatus struct {
	commonv1alpha1.ResourceStatus `json:",inline"`

	// URL under which the repository is served.
	// +optional
	URL string `json:"url,omitempty"`

	// SecretHash is a hash of the secret values last written to Artifactory,
	// used to detect rotated credentials.
	// +optional
	SecretHash string `json:"secretHash,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=repo;repos,categories=jfrog
// +kubebuilder:printcolumn:name="Key",type=string,JSONPath=`.status.externalName`
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.rclass`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.packageType`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Repository is an Artifactory repository of any class.
type Repository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RepositorySpec   `json:"spec"`
	Status RepositoryStatus `json:"status,omitempty"`
}

// GetResourceSpec returns the shared managed resource spec.
func (r *Repository) GetResourceSpec() *commonv1alpha1.ResourceSpec { return &r.Spec.ResourceSpec }

// GetResourceStatus returns the shared managed resource status.
func (r *Repository) GetResourceStatus() *commonv1alpha1.ResourceStatus {
	return &r.Status.ResourceStatus
}

// GetKey returns the repository key in Artifactory.
func (r *Repository) GetKey() string {
	if r.Spec.Key != "" {
		return r.Spec.Key
	}
	return r.Name
}

// +kubebuilder:object:root=true

// RepositoryList contains a list of Repository.
type RepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Repository `json:"items"`
}

func init() {
	knownTypes = append(knownTypes, &Repository{}, &RepositoryList{})
}
