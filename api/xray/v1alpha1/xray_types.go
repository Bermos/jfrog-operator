package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
)

// PolicyRule is a rule of an Xray policy. Rules are evaluated by priority.
type PolicyRule struct {
	// Name of the rule.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Priority of the rule, 1 being the highest.
	// +kubebuilder:validation:Minimum=1
	Priority int32 `json:"priority"`

	// Criteria select the violations the rule applies to, with the field
	// names of the Xray policies API, e.g. {"min_severity": "high"},
	// {"cvss_range": {"from": 7, "to": 10}} or {"banned_licenses": ["GPL-3.0"]}.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	Criteria apiextensionsv1.JSON `json:"criteria"`

	// Actions taken on a violation, with the field names of the Xray policies
	// API, e.g. {"fail_build": true, "block_download": {"active": true, "unscanned": false}}.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	Actions *apiextensionsv1.JSON `json:"actions,omitempty"`
}

// PolicySpec is the desired state of an Xray policy.
// +kubebuilder:validation:XValidation:rule="has(self.policyName) == has(oldSelf.policyName) && (!has(self.policyName) || self.policyName == oldSelf.policyName)",message="policyName is immutable"
// +kubebuilder:validation:XValidation:rule="self.type == oldSelf.type",message="type is immutable"
type PolicySpec struct {
	commonv1alpha1.ResourceSpec `json:",inline"`

	// PolicyName in Xray. Defaults to metadata.name.
	// +kubebuilder:validation:MinLength=1
	// +optional
	PolicyName string `json:"policyName,omitempty"`

	// Type of the policy.
	// +kubebuilder:validation:Enum=security;license;operational_risk
	Type string `json:"type"`

	// Description of the policy.
	// +optional
	Description string `json:"description,omitempty"`

	// Rules of the policy.
	// +kubebuilder:validation:MinItems=1
	Rules []PolicyRule `json:"rules"`
}

// PolicyStatus is the observed state of a Policy.
type PolicyStatus struct {
	commonv1alpha1.ResourceStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=policies,shortName=xpol,categories=jfrog
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=`.status.externalName`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Policy is an Xray security, license or operational risk policy.
type Policy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PolicySpec   `json:"spec"`
	Status PolicyStatus `json:"status,omitempty"`
}

// GetResourceSpec returns the shared managed resource spec.
func (p *Policy) GetResourceSpec() *commonv1alpha1.ResourceSpec { return &p.Spec.ResourceSpec }

// GetResourceStatus returns the shared managed resource status.
func (p *Policy) GetResourceStatus() *commonv1alpha1.ResourceStatus { return &p.Status.ResourceStatus }

// GetPolicyName returns the policy name in Xray.
func (p *Policy) GetPolicyName() string {
	if p.Spec.PolicyName != "" {
		return p.Spec.PolicyName
	}
	return p.Name
}

// +kubebuilder:object:root=true

// PolicyList contains a list of Policy.
type PolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Policy `json:"items"`
}

// WatchFilter narrows the artifacts of a watched resource.
type WatchFilter struct {
	// Type of the filter, e.g. regex, path-regex, package-type, mime-type or ant-patterns.
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type"`

	// Value of the filter. A string for most filter types, an object for
	// ant-patterns (e.g. {"IncludePatterns": ["**"], "ExcludePatterns": []}).
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Value apiextensionsv1.JSON `json:"value"`
}

// WatchResource is a resource an Xray watch covers.
// +kubebuilder:validation:XValidation:rule="!(self.type in ['repository', 'build', 'project', 'releaseBundle']) || has(self.name)",message="name is required for this resource type"
type WatchResource struct {
	// Type of the resource.
	// +kubebuilder:validation:Enum=repository;all-repos;build;all-builds;project;all-projects;releaseBundle;all-releaseBundles
	Type string `json:"type"`

	// Name of the repository, build, project or release bundle.
	// +optional
	Name string `json:"name,omitempty"`

	// BinMgrID is the id of the Artifactory instance.
	// +kubebuilder:default=default
	// +optional
	BinMgrID string `json:"binMgrId,omitempty"`

	// Filters narrow the artifacts the watch covers.
	// +optional
	Filters []WatchFilter `json:"filters,omitempty"`
}

// AssignedPolicy references an Xray policy by name.
type AssignedPolicy struct {
	// Name of the policy in Xray.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Type of the policy.
	// +kubebuilder:validation:Enum=security;license;operational_risk
	Type string `json:"type"`
}

// WatchSpec is the desired state of an Xray watch.
// +kubebuilder:validation:XValidation:rule="has(self.watchName) == has(oldSelf.watchName) && (!has(self.watchName) || self.watchName == oldSelf.watchName)",message="watchName is immutable"
type WatchSpec struct {
	commonv1alpha1.ResourceSpec `json:",inline"`

	// WatchName in Xray. Defaults to metadata.name.
	// +kubebuilder:validation:MinLength=1
	// +optional
	WatchName string `json:"watchName,omitempty"`

	// Description of the watch.
	// +optional
	Description string `json:"description,omitempty"`

	// Active enables the watch.
	// +kubebuilder:default=true
	// +optional
	Active *bool `json:"active,omitempty"`

	// Resources the watch covers.
	// +kubebuilder:validation:MinItems=1
	Resources []WatchResource `json:"resources"`

	// AssignedPolicies applied to the resources.
	// +kubebuilder:validation:MinItems=1
	AssignedPolicies []AssignedPolicy `json:"assignedPolicies"`

	// WatchRecipients are email addresses notified about violations.
	// +optional
	WatchRecipients []string `json:"watchRecipients,omitempty"`
}

// WatchStatus is the observed state of a Watch.
type WatchStatus struct {
	commonv1alpha1.ResourceStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=xwatch,categories=jfrog
// +kubebuilder:printcolumn:name="Watch",type=string,JSONPath=`.status.externalName`
// +kubebuilder:printcolumn:name="Active",type=boolean,JSONPath=`.spec.active`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Watch applies Xray policies to repositories, builds, projects or release bundles.
type Watch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WatchSpec   `json:"spec"`
	Status WatchStatus `json:"status,omitempty"`
}

// GetResourceSpec returns the shared managed resource spec.
func (w *Watch) GetResourceSpec() *commonv1alpha1.ResourceSpec { return &w.Spec.ResourceSpec }

// GetResourceStatus returns the shared managed resource status.
func (w *Watch) GetResourceStatus() *commonv1alpha1.ResourceStatus { return &w.Status.ResourceStatus }

// GetWatchName returns the watch name in Xray.
func (w *Watch) GetWatchName() string {
	if w.Spec.WatchName != "" {
		return w.Spec.WatchName
	}
	return w.Name
}

// +kubebuilder:object:root=true

// WatchList contains a list of Watch.
type WatchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Watch `json:"items"`
}

func init() {
	knownTypes = append(knownTypes, &Policy{}, &PolicyList{}, &Watch{}, &WatchList{})
}
