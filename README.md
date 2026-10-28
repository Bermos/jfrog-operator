# jfrog-operator

A Kubernetes operator that manages resources in a self-hosted JFrog Platform:
Artifactory repositories today, with users, groups, permission targets and
Xray policies and watches next. It is designed to be composed by
[Crossplane](https://crossplane.io) but works on its own as well.

See [docs/design.md](docs/design.md) for the design and roadmap.

## Quick start

```sh
make install                     # install the CRDs
make deploy IMG=<your image>     # or: make run, to run locally against your kubeconfig
```

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
composition is in [examples/crossplane](examples/crossplane).

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
```

`make test` downloads a Kubernetes API server and etcd for envtest into `bin/`.
