# Crossplane example

A Crossplane v2 composition that onboards a team: local, remote and virtual
repositories for one package type, a group with a permission target on the
repositories, and an Xray watch applying existing policies.

```sh
kubectl apply -f functions.yaml -f definition.yaml -f composition.yaml
kubectl create namespace team-payments
kubectl apply -f team.yaml
kubectl get team,jfrog -n team-payments
```

Crossplane needs permission to manage the operator's resources. The
`jfrog-editor` and `jfrog-security-editor` ClusterRoles installed with the
operator aggregate into Crossplane's role, so nothing extra is needed when
the operator is installed with `make deploy`.

The composition template is rendered and validated against the CRDs in the
operator's test suite (`internal/controller/examples_test.go`).
