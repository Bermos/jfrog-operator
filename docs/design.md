# Design

This operator manages resources inside a self-hosted JFrog Platform
(Artifactory, Access and Xray) from Kubernetes. It does not install the
platform itself; the official Helm charts do that.

The main consumer is Crossplane: platform teams build compositions out of
these resources and hand teams a simple API such as "give me Maven
repositories". The resources are just as usable on their own with `kubectl`,
Helm or GitOps tools.

## Goals

- One Kubernetes object per JFrog resource, declarative and continuously
  reconciled, including correction of changes made by hand in JFrog.
- Safe by default: never take over or delete something in JFrog the operator
  did not create, unless told to.
- Crossplane compatible without depending on Crossplane.
- Self-hosted platforms first, including private CAs and per-product URLs.

## API groups and kinds

| Group | Kind | Scope | JFrog API | Status |
|---|---|---|---|---|
| `platform.jfrog.bermos.io` | `ClusterConnection` | Cluster | n/a | done |
| `platform.jfrog.bermos.io` | `Connection` | Namespaced | n/a | done |
| `artifactory.jfrog.bermos.io` | `Repository` | Namespaced | `/artifactory/api/repositories` | done |
| `platform.jfrog.bermos.io` | `User` | Namespaced | `/artifactory/api/security/users` | done |
| `platform.jfrog.bermos.io` | `Group` | Namespaced | `/artifactory/api/security/groups` | done |
| `platform.jfrog.bermos.io` | `PermissionTarget` | Namespaced | `/artifactory/api/v2/security/permissions` | done |
| `xray.jfrog.bermos.io` | `Policy` | Namespaced | `/xray/api/v2/policies` | done |
| `xray.jfrog.bermos.io` | `Watch` | Namespaced | `/xray/api/v2/watches` | done |

All managed kinds are namespaced. Crossplane v2 composes namespaced
resources into namespaced composites, and namespaces are the natural tenancy
boundary for "team A may manage its own repositories". JFrog resources are
global to the platform, so two namespaces naming the same repository key would
fight over it; the adoption rules below make the second one fail instead.

### One `Repository` kind instead of one kind per class

Artifactory has local, remote, virtual and federated repositories for ~30
package types, each with its own set of fields. A kind per class and type would
mean well over a hundred CRDs. Instead `Repository` has:

- typed fields for what nearly every repository needs (`rclass`,
  `packageType`, description, patterns, project, environments, Xray indexing),
- typed fields per class (`url`/credentials for remote, `repositories` for
  virtual, `members` for federated), validated with CEL rules,
- a free-form `config` object passed through to Artifactory for everything
  else, so no Artifactory feature is out of reach.

`key`, `rclass` and `packageType` are immutable because Artifactory cannot
change them either.

## Connections

`ClusterConnection` (cluster scoped) and `Connection` (namespaced) hold the
platform URL and point to a Secret with either an access token or a username
and password. Product URLs default to `<url>/artifactory`, `<url>/access` and
`<url>/xray` and can be overridden for split deployments. A CA bundle can be
supplied from a Secret for platforms behind a private CA.

Every managed resource has an optional `spec.connectionRef`; it defaults to
the `ClusterConnection` named `default`. A namespaced `Connection` can only be
referenced from its own namespace and only reads Secrets from its own
namespace, so tenants cannot borrow the operator's admin credentials through
it.

The connection controllers check reachability (Artifactory and, optionally,
Xray) every five minutes and report versions and a `Ready` condition.

Clients are cached per connection and rebuilt only when the connection or one
of its Secrets changes. Secrets are read straight from the API server rather
than through the informer cache, so the operator only needs `get` on Secrets.

## Reconciliation

Every managed kind is reconciled by one generic reconciler
(`internal/reconciler`). A kind only implements `Observe`, `Create`, `Update`
and `Delete` against JFrog; the shared reconciler takes care of:

- **Finalizers**, so the JFrog resource is removed before the object goes away.
- **Drift detection.** Each object is compared against JFrog every
  `--sync-interval` (10 minutes by default) and on every change. Only the fields
  set in the spec are compared, because JFrog fills in defaults for the rest.
  Secrets JFrog never returns (e.g. remote repository passwords) are compared
  by a hash stored in the status, so rotating a Secret is picked up too.
- **Adoption.** If the resource already exists in JFrog and the object did not
  create it, the operator refuses to touch it (`Synced=False`, reason
  `AlreadyExists`) unless `spec.adoptExisting: true`. Before creating, the
  operator records a `create-pending` annotation so a crash between create and
  status update is not mistaken for someone else's resource.
- **Deletion policy.** `spec.deletionPolicy: Orphan` keeps the resource in
  JFrog when the object is deleted. An object that never created or adopted
  anything never deletes anything.
- **Management policy.** `spec.managementPolicy: Observe` never writes to
  JFrog. It reports whether the resource exists and whether it matches the
  spec, which is useful for referencing resources managed elsewhere.

### Users, groups and permission targets

- Group membership is managed on the `User` (`spec.groups`), which is how the
  Artifactory security API models it. When `spec.groups` is set the
  membership is enforced exactly, including auto-join groups; leave it unset
  for users whose groups come from LDAP, SAML or auto-join.
- User passwords come from Secrets and are only sent when they change
  (detected by hash), so the operator does not reset passwords on every sync.
- A `PermissionTarget` is replaced as a whole on update. Sections and grants
  added by hand in Artifactory are drift and get removed. Lists that JFrog
  returns in no particular order (repositories, actions) are compared sorted.
- Users, groups and permission targets grant access to the whole platform (a
  `User` can be an admin), so their RBAC is not aggregated into the namespace
  `admin` and `edit` roles. The `jfrog-security-editor` ClusterRole has to be
  bound explicitly.

### Xray policies and watches

- A `Policy` has typed rules (name, priority) whose `criteria` and `actions`
  are passed through with the field names of the Xray API, because they
  differ per policy type and grow with every Xray release. Rules are compared
  in priority order.
- A `Watch` has typed resources, filters, assigned policies and recipients.
  Assigned policies are compared sorted by name. Xray adds fields to watched
  resources (e.g. `repo_type`); list elements are compared field by field,
  so such additions are not drift.
- Both are replaced as a whole on update, so rules or resources added by hand
  are removed.
- Like users and permissions, they act platform-wide and their RBAC is part
  of `jfrog-security-editor`, not namespace `edit`.

### Status

Every managed resource reports:

- `Ready`: the resource exists in JFrog and is usable.
- `Synced`: the last reconcile succeeded. On failure the reason
  (`ConnectionError`, `AlreadyExists`, `ReconcileError`) and the JFrog error
  message are in the condition.
- `externalName`: the identifier in JFrog the object manages.
- Kind-specific outputs, e.g. `status.url` for repositories.

## Crossplane compatibility

Crossplane v2 compositions can compose any namespaced Kubernetes resource, so
no Crossplane provider wrapper is needed. What makes the kinds work well inside
compositions:

- `Ready` and `Synced` conditions follow Crossplane's conventions, so
  `function-auto-ready` marks composed resources ready correctly.
- Outputs live in `status` (e.g. `status.url`) and can be copied to the
  composite's status.
- Spec fields reference other resources by JFrog name (e.g. a virtual
  repository's `repositories`), which a composition can compute from its input.
- The `jfrog-editor` ClusterRole aggregates into Crossplane's own role
  (`rbac.crossplane.io/aggregate-to-crossplane`), so Crossplane may compose
  the resources without extra RBAC.

See `examples/crossplane` for a composition that onboards a team: local,
remote and virtual repositories, a group with a permission target, and an
Xray watch. The test suite renders its template and validates the result
against the CRDs.

A Crossplane provider generated with Upjet from the JFrog Terraform providers
is the alternative. We prefer a native operator because it avoids running
Terraform per resource, reacts to drift in seconds rather than through a
Terraform plan, and keeps the operator usable without Crossplane.

## JFrog API client

`internal/jfrog` is a small client written for the operator instead of
`jfrog-client-go`, which is built for the JFrog CLI: it logs through a global
logger, has no context support on most calls and pulls in a large dependency
tree. The client returns typed `APIError`s so callers can tell "not found"
apart from failures. Artifactory answers `400` for an unknown repository
key; the client confirms with the repository list and turns it into a not
found error.

## Testing

- Unit tests for the client against `internal/jfrog/fake`, an in-memory JFrog
  Platform with the same methods, status codes and create/update semantics as
  the real one.
- Integration tests run the real controllers against a real API server
  (envtest) and the fake platform. They cover creation, updates, drift
  correction, recreation after out-of-band deletion, deletion and orphaning,
  adoption, observe-only mode, secret rotation, error reporting, CEL validation
  and that the samples match the CRD schemas.
- An end-to-end test builds the images, installs the Helm chart into a kind
  cluster next to a containerised fake platform, and checks that one resource
  of every kind becomes Ready and is cleaned up again.
- CI runs lint, the full test suite with the race detector, a check that
  generated code, manifests and the chart's CRDs are up to date, Helm lint,
  the end-to-end test, and an image build.

## Roadmap

1. Connections, generic reconciler and `Repository`.
2. `User`, `Group` and `PermissionTarget`, with user passwords from Secrets.
3. Xray `Policy` and `Watch`, and a Crossplane example covering a full team
   onboarding (repositories, group, permission target, Xray watch).
4. Helm chart, end-to-end test in kind, and a release workflow publishing the
   image and chart on tags.
5. End-to-end tests against a real Artifactory in CI, if a licence allows it.
