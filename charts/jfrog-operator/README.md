# jfrog-operator Helm chart

```sh
helm install jfrog-operator charts/jfrog-operator -n jfrog-system --create-namespace \
  --set defaultConnection.enabled=true \
  --set defaultConnection.url=https://jfrog.example.com
```

With `defaultConnection.enabled`, create the credentials Secret (key
`accessToken`, or `username` and `password`) in the release namespace under
the name in `defaultConnection.secretName`.

The CRDs in `crds/` and the operator's ClusterRole are generated from the
repository by `make generate`; do not edit them by hand. See `values.yaml` for
all settings.
