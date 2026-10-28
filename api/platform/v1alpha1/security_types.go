package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/Bermos/jfrog-operator/api/common/v1alpha1"
)

// UserSpec is the desired state of a JFrog Platform user.
// +kubebuilder:validation:XValidation:rule="has(self.username) == has(oldSelf.username) && (!has(self.username) || self.username == oldSelf.username)",message="username is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.passwordSecretRef) || (has(self.internalPasswordDisabled) && self.internalPasswordDisabled)",message="passwordSecretRef is required unless internalPasswordDisabled is true"
type UserSpec struct {
	commonv1alpha1.ResourceSpec `json:",inline"`

	// Username in JFrog. Defaults to metadata.name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +optional
	Username string `json:"username,omitempty"`

	// Email address of the user.
	// +kubebuilder:validation:MinLength=3
	Email string `json:"email"`

	// PasswordSecretRef selects the user's password. Changing the Secret
	// changes the password in JFrog on the next sync.
	// +optional
	PasswordSecretRef *commonv1alpha1.SecretKeySelector `json:"passwordSecretRef,omitempty"`

	// Admin grants platform administrator privileges.
	// +optional
	Admin *bool `json:"admin,omitempty"`

	// ProfileUpdatable allows the user to change their own profile.
	// +optional
	ProfileUpdatable *bool `json:"profileUpdatable,omitempty"`

	// DisableUIAccess prevents the user from logging in to the UI.
	// +optional
	DisableUIAccess *bool `json:"disableUIAccess,omitempty"`

	// InternalPasswordDisabled disables password login, e.g. for users that
	// only log in through SSO or only use tokens.
	// +optional
	InternalPasswordDisabled *bool `json:"internalPasswordDisabled,omitempty"`

	// Groups the user is a member of. When set, the membership is enforced
	// exactly, including groups that auto-join new users. Leave it unset to
	// not manage membership.
	// +listType=set
	// +optional
	Groups []string `json:"groups,omitempty"`
}

// UserStatus is the observed state of a User.
type UserStatus struct {
	commonv1alpha1.ResourceStatus `json:",inline"`

	// Realm the user authenticates with, e.g. internal or ldap.
	// +optional
	Realm string `json:"realm,omitempty"`

	// SecretHash is a hash of the password last written to JFrog.
	// +optional
	SecretHash string `json:"secretHash,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=jfrog
// +kubebuilder:printcolumn:name="Username",type=string,JSONPath=`.status.externalName`
// +kubebuilder:printcolumn:name="Email",type=string,JSONPath=`.spec.email`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// User is a JFrog Platform user.
type User struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UserSpec   `json:"spec"`
	Status UserStatus `json:"status,omitempty"`
}

// GetResourceSpec returns the shared managed resource spec.
func (u *User) GetResourceSpec() *commonv1alpha1.ResourceSpec { return &u.Spec.ResourceSpec }

// GetResourceStatus returns the shared managed resource status.
func (u *User) GetResourceStatus() *commonv1alpha1.ResourceStatus { return &u.Status.ResourceStatus }

// GetUsername returns the username in JFrog.
func (u *User) GetUsername() string {
	if u.Spec.Username != "" {
		return u.Spec.Username
	}
	return u.Name
}

// +kubebuilder:object:root=true

// UserList contains a list of User.
type UserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []User `json:"items"`
}

// GroupSpec is the desired state of a JFrog Platform group.
// +kubebuilder:validation:XValidation:rule="has(self.groupName) == has(oldSelf.groupName) && (!has(self.groupName) || self.groupName == oldSelf.groupName)",message="groupName is immutable"
type GroupSpec struct {
	commonv1alpha1.ResourceSpec `json:",inline"`

	// GroupName in JFrog. Defaults to metadata.name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +optional
	GroupName string `json:"groupName,omitempty"`

	// Description of the group.
	// +optional
	Description string `json:"description,omitempty"`

	// AutoJoin adds every new user to the group.
	// +optional
	AutoJoin *bool `json:"autoJoin,omitempty"`

	// AdminPrivileges grants platform administrator privileges to members.
	// +optional
	AdminPrivileges *bool `json:"adminPrivileges,omitempty"`

	// Realm of the group, e.g. internal, ldap or saml.
	// +optional
	Realm string `json:"realm,omitempty"`

	// RealmAttributes of the group, e.g. the LDAP group DN.
	// +optional
	RealmAttributes string `json:"realmAttributes,omitempty"`
}

// GroupStatus is the observed state of a Group.
type GroupStatus struct {
	commonv1alpha1.ResourceStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=jfrog
// +kubebuilder:printcolumn:name="Group",type=string,JSONPath=`.status.externalName`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Group is a JFrog Platform group. Membership is managed on the User.
type Group struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GroupSpec   `json:"spec,omitempty"`
	Status GroupStatus `json:"status,omitempty"`
}

// GetResourceSpec returns the shared managed resource spec.
func (g *Group) GetResourceSpec() *commonv1alpha1.ResourceSpec { return &g.Spec.ResourceSpec }

// GetResourceStatus returns the shared managed resource status.
func (g *Group) GetResourceStatus() *commonv1alpha1.ResourceStatus { return &g.Status.ResourceStatus }

// GetGroupName returns the group name in JFrog.
func (g *Group) GetGroupName() string {
	if g.Spec.GroupName != "" {
		return g.Spec.GroupName
	}
	return g.Name
}

// +kubebuilder:object:root=true

// GroupList contains a list of Group.
type GroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Group `json:"items"`
}

// PermissionAction is an action a permission target grants.
// +kubebuilder:validation:Enum=read;write;annotate;delete;manage;managedXrayMeta;distribute
type PermissionAction string

// PermissionActions grants actions to users and groups.
type PermissionActions struct {
	// Users maps usernames to the actions granted to them.
	// +optional
	Users map[string][]PermissionAction `json:"users,omitempty"`
	// Groups maps group names to the actions granted to their members.
	// +optional
	Groups map[string][]PermissionAction `json:"groups,omitempty"`
}

// PermissionSection grants actions on a set of repositories.
type PermissionSection struct {
	// Repositories the section applies to. ANY, ANY LOCAL, ANY REMOTE and
	// ANY DISTRIBUTION select all repositories of that kind.
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	Repositories []string `json:"repositories"`

	// IncludePatterns selects the paths the section applies to. Defaults to all (**).
	// +optional
	IncludePatterns []string `json:"includePatterns,omitempty"`

	// ExcludePatterns excludes paths from the section.
	// +optional
	ExcludePatterns []string `json:"excludePatterns,omitempty"`

	// Actions granted.
	// +optional
	Actions PermissionActions `json:"actions,omitempty"`
}

// PermissionTargetSpec is the desired state of an Artifactory permission target.
// +kubebuilder:validation:XValidation:rule="has(self.targetName) == has(oldSelf.targetName) && (!has(self.targetName) || self.targetName == oldSelf.targetName)",message="targetName is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.repo) || has(self.build) || has(self.releaseBundle)",message="at least one of repo, build or releaseBundle is required"
type PermissionTargetSpec struct {
	commonv1alpha1.ResourceSpec `json:",inline"`

	// TargetName in Artifactory. Defaults to metadata.name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +optional
	TargetName string `json:"targetName,omitempty"`

	// Repo grants actions on artifacts in repositories.
	// +optional
	Repo *PermissionSection `json:"repo,omitempty"`

	// Build grants actions on build info. Repositories must be artifactory-build-info.
	// +optional
	Build *PermissionSection `json:"build,omitempty"`

	// ReleaseBundle grants actions on release bundles.
	// +optional
	ReleaseBundle *PermissionSection `json:"releaseBundle,omitempty"`
}

// PermissionTargetStatus is the observed state of a PermissionTarget.
type PermissionTargetStatus struct {
	commonv1alpha1.ResourceStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=pt,categories=jfrog
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.status.externalName`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// PermissionTarget grants users and groups actions on repositories, builds
// and release bundles.
type PermissionTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PermissionTargetSpec   `json:"spec"`
	Status PermissionTargetStatus `json:"status,omitempty"`
}

// GetResourceSpec returns the shared managed resource spec.
func (p *PermissionTarget) GetResourceSpec() *commonv1alpha1.ResourceSpec {
	return &p.Spec.ResourceSpec
}

// GetResourceStatus returns the shared managed resource status.
func (p *PermissionTarget) GetResourceStatus() *commonv1alpha1.ResourceStatus {
	return &p.Status.ResourceStatus
}

// GetTargetName returns the permission target name in Artifactory.
func (p *PermissionTarget) GetTargetName() string {
	if p.Spec.TargetName != "" {
		return p.Spec.TargetName
	}
	return p.Name
}

// +kubebuilder:object:root=true

// PermissionTargetList contains a list of PermissionTarget.
type PermissionTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PermissionTarget `json:"items"`
}

func init() {
	knownTypes = append(knownTypes,
		&User{}, &UserList{},
		&Group{}, &GroupList{},
		&PermissionTarget{}, &PermissionTargetList{},
	)
}
