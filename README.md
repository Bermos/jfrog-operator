# jfrog-operator

A Kubernetes operator that manages resources in a self-hosted JFrog Platform:
Artifactory repositories, users, groups, permission targets, and Xray
policies and watches. It is designed to be composed by
[Crossplane](https://crossplane.io) but works on its own as well.

| Kind | API group | Manages |
|---|---|---|
| `ClusterConnection`, `Connection` | `platform.jfrog.bermos.io` | URL and credentials of a platform |
| `Repository` | `artifactory.jfrog.bermos.io` | local, remote, virtual and federated repositories |
| `User`, `Group`, `PermissionTarget` | `platform.jfrog.bermos.io` | access |
| `Policy`, `Watch` | `xray.jfrog.bermos.io` | Xray security, license and operational risk policies |

See [docs/design.md](docs/design.md) for the design and roadmap.

## Quick start

Install with Helm (from a checkout, or from `oci://ghcr.io/bermos/charts/jfrog-operator` once a release is tagged):

```sh
helm install jfrog-operator charts/jfrog-operator -n jfrog-system --create-namespace
```

Or with kustomize: `make install deploy IMG=<your image>`, or `make run` to run
the operator locally against your kubeconfig.

Point the operator at your platform with an access token:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: jfrog-credentials
  namespace: jfrog-system
stringData:
  accessToken: <token>
---
apiVersion: platform.jfrog.bermos.io/v1alpha1
kind: ClusterConnection
metadata:
  name: default
spec:
  url: https://jfrog.example.com
  credentials:
    secretRef:
      name: jfrog-credentials
      namespace: jfrog-system
```

Then create repositories:

```yaml
apiVersion: artifactory.jfrog.bermos.io/v1alpha1
kind: Repository
metadata:
  name: team-a-maven-local
  namespace: team-a
spec:
  rclass: local
  packageType: maven
  config:            # any other field of the Artifactory repository configuration API
    handleSnapshots: false
```

```sh
$ kubectl get jfrog -A
NAMESPACE   NAME                 KEY                  CLASS   TYPE    READY   SYNCED   AGE
team-a      team-a-maven-local   team-a-maven-local   local   maven   True    True     5s
```

More examples are in [config/samples](config/samples), and a Crossplane
composition that onboards a team (repositories, group, permissions, Xray
watch) is in [examples/crossplane](examples/crossplane).

Users, groups, permission targets and Xray resources act platform-wide, so
their RBAC is not aggregated into namespace `edit`/`admin`; bind the
`jfrog-security-editor` ClusterRole to whoever administers JFrog.

## Behaviour in short

- Changes made by hand in JFrog are reverted on the next sync
  (`--sync-interval`, 10 minutes by default).
- Resources that already exist in JFrog are left alone unless
  `spec.adoptExisting: true`.
- `spec.deletionPolicy: Orphan` keeps the resource in JFrog when the object is deleted.
- `spec.managementPolicy: Observe` only reports on a resource and never changes it.
- `Ready` and `Synced` conditions report the state; errors from JFrog show up
  in the `Synced` condition and as events.

## Development

```sh
make generate   # regenerate deepcopy code, CRDs and RBAC after changing api/
make lint
make test       # unit tests and envtest integration tests against a fake JFrog
make e2e        # installs the Helm chart into kind next to a fake JFrog (needs docker)
```

Pushing a tag such as `v0.1.0` publishes the image and the Helm chart to GHCR.

`make test` downloads a Kubernetes API server and etcd for envtest into `bin/`.
