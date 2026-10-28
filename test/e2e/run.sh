#!/usr/bin/env bash
# End-to-end test: installs the Helm chart into a kind cluster together with
# a fake JFrog Platform, then creates one resource of every kind and checks
# that the operator makes them Ready and cleans them up again.
#
# Needs docker, kind, kubectl and helm on PATH (make e2e provides them).
set -euo pipefail

CLUSTER=${CLUSTER:-jfrog-operator-e2e}
NS=jfrog-system
TIMEOUT=${TIMEOUT:-120s}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

log() { printf '\n=== %s\n' "$*"; }

dump() {
  log "Debug output"
  kubectl get jfrog -A -o wide || true
  kubectl get pods -A || true
  kubectl -n "$NS" logs deploy/jfrog-operator --tail=200 || true
  kubectl get events -A --sort-by=.lastTimestamp | tail -50 || true
}
trap 'status=$?; if [ $status -ne 0 ]; then dump; fi; [ -n "${KEEP_CLUSTER:-}" ] || kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true; exit $status' EXIT

if [ -z "${SKIP_BUILD:-}" ]; then
  log "Building images"
  docker build -q -t jfrog-operator:e2e .
  docker build -q -t fake-jfrog:e2e --build-arg CMD=./test/e2e/fake-jfrog .
fi

log "Creating kind cluster"
kind get clusters | grep -qx "$CLUSTER" || kind create cluster --name "$CLUSTER" --wait 120s
kind load docker-image --name "$CLUSTER" jfrog-operator:e2e fake-jfrog:e2e

log "Deploying fake JFrog Platform"
kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" apply -f test/e2e/fake-jfrog.yaml
kubectl -n "$NS" create secret generic jfrog-credentials --from-literal=accessToken=fake-token \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$NS" rollout status deploy/fake-jfrog --timeout="$TIMEOUT"

log "Installing the operator with Helm"
helm upgrade --install jfrog-operator charts/jfrog-operator -n "$NS" --wait --timeout "$TIMEOUT" \
  --set image.repository=jfrog-operator --set image.tag=e2e --set image.pullPolicy=Never \
  --set syncInterval=10s \
  --set defaultConnection.enabled=true \
  --set defaultConnection.url=http://fake-jfrog.$NS.svc:8080

kubectl wait clusterconnection/default --for=condition=Ready --timeout="$TIMEOUT"
kubectl get clusterconnection default -o jsonpath='{.status.artifactoryVersion}' | grep -q .

log "Creating resources"
kubectl create namespace e2e --dry-run=client -o yaml | kubectl apply -f -
kubectl -n e2e apply -f test/e2e/resources.yaml
for kind in repository user group permissiontarget policy watch; do
  kubectl -n e2e wait "$kind" --all --for=condition=Ready --timeout="$TIMEOUT"
  kubectl -n e2e wait "$kind" --all --for=condition=Synced --timeout="$TIMEOUT"
done
kubectl -n e2e get jfrog

log "Checking outputs"
kubectl -n e2e get repository e2e-virtual -o jsonpath='{.status.url}' | grep -q '/artifactory/e2e-virtual'

log "Deleting resources"
kubectl -n e2e delete -f test/e2e/resources.yaml --wait --timeout="$TIMEOUT"

log "Operator restarted cleanly"
test "$(kubectl -n "$NS" get pods -l app.kubernetes.io/name=jfrog-operator -o jsonpath='{.items[0].status.containerStatuses[0].restartCount}')" = 0

log "End-to-end test passed"
