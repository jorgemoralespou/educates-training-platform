#!/usr/bin/env bash
#
# Phase 0 smoke test for the educates-installer operator.
#
# Builds the operator image, loads it into a kind cluster, helm-installs the
# educates-installer chart, applies a minimal EducatesClusterConfig CR, and
# asserts that the operator's reconciler emitted its "Reconciling
# EducatesClusterConfig" log line.
#
# Run from installer/operator/ via `make smoke-test`. The kind cluster is
# created if absent and torn down on exit (set KEEP_CLUSTER=true to keep it).
#
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-educates-installer-smoke}"
NAMESPACE="${NAMESPACE:-educates-installer}"
IMG="${IMG:-ghcr.io/educates/educates-operator:dev}"
RELEASE="${RELEASE:-educates-installer}"
CHART_DIR="${CHART_DIR:-../charts/educates-installer}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-60}"

CREATED_CLUSTER=false
cleanup() {
    if [[ "$CREATED_CLUSTER" == "true" && "${KEEP_CLUSTER:-false}" != "true" ]]; then
        echo "Tearing down kind cluster $CLUSTER_NAME..."
        kind delete cluster --name "$CLUSTER_NAME" >/dev/null 2>&1 || true
    fi
}
trap cleanup EXIT

for tool in kind kubectl helm docker; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "FAIL: $tool not found on PATH"
        exit 1
    fi
done

if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
    echo "Creating kind cluster $CLUSTER_NAME..."
    kind create cluster --name "$CLUSTER_NAME"
    CREATED_CLUSTER=true
fi
kubectl cluster-info --context "kind-$CLUSTER_NAME" >/dev/null

echo "Building operator image $IMG..."
make docker-build IMG="$IMG" >/dev/null

echo "Loading image into kind..."
kind load docker-image "$IMG" --name "$CLUSTER_NAME"

repo="${IMG%:*}"
tag="${IMG##*:}"

echo "Installing chart $RELEASE..."
helm upgrade --install "$RELEASE" "$CHART_DIR" \
    --namespace "$NAMESPACE" --create-namespace \
    --set "image.repository=$repo" \
    --set "image.tag=$tag" \
    --wait --timeout 2m

echo "Applying sample EducatesClusterConfig..."
kubectl apply -f - <<'EOF'
apiVersion: config.educates.dev/v1alpha1
kind: EducatesClusterConfig
metadata:
  name: cluster
spec:
  mode: Managed
EOF

echo "Waiting up to ${TIMEOUT_SECONDS}s for the operator to reconcile..."
deadline=$(( $(date +%s) + TIMEOUT_SECONDS ))
while (( $(date +%s) < deadline )); do
    if kubectl logs -n "$NAMESPACE" \
            -l app.kubernetes.io/name=educates-installer --tail=500 2>/dev/null \
            | grep -q "Reconciling EducatesClusterConfig"; then
        echo "PASS: operator reconciled the EducatesClusterConfig CR"
        kubectl delete educatesclusterconfig cluster --ignore-not-found
        exit 0
    fi
    sleep 2
done

echo "FAIL: did not see 'Reconciling EducatesClusterConfig' in operator logs within ${TIMEOUT_SECONDS}s"
echo "--- last 100 lines of operator logs ---"
kubectl logs -n "$NAMESPACE" -l app.kubernetes.io/name=educates-installer --tail=100 || true
exit 1
