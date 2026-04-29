#!/usr/bin/env bash
# End-to-end check that bundled + user-supplied Kyverno policies reach
# the cluster on both paths:
#
# 1. `clusterPolicies` path — bundled (baseline + restricted) AND
#    user-supplied via additionalKyvernoPolicies.clusterPolicies are
#    installed as cluster-wide ClusterPolicy resources directly by the
#    chart.
# 2. `workshopPolicies` path — operational + the internal
#    require-ingress-session-name + user-supplied via
#    additionalKyvernoPolicies.workshopPolicies are bundled into the
#    `kyverno-policies.yaml` Secret feed and re-emitted by
#    session-manager as `educates-environment-<env>` ClusterPolicy
#    rules.

set -Eeuo pipefail

EXPECTED_BASELINE_CP="disallow-privileged-containers"
EXPECTED_RESTRICTED_CP="require-run-as-nonroot"
EXPECTED_USER_CLUSTER_CP="scenario-06-cluster-marker"
EXPECTED_OPS_RULE="no-loadbalancer-service"
EXPECTED_INTERNAL_RULE="require-ingress-session-name"
EXPECTED_USER_WORKSHOP_RULE="scenario-06-workshop-marker"

fail=0

# 1. Cluster-wide ClusterPolicies — bundled + user-supplied.
for cp in "$EXPECTED_BASELINE_CP" "$EXPECTED_RESTRICTED_CP" "$EXPECTED_USER_CLUSTER_CP"; do
  if kubectl get clusterpolicy "$cp" >/dev/null 2>&1; then
    echo "[post-deploy] ✓ cluster-wide ClusterPolicy present: $cp"
  else
    echo "[post-deploy] ✗ cluster-wide ClusterPolicy missing:  $cp" >&2
    fail=1
  fi
done

# 2. Per-environment ClusterPolicy (workshopPolicies bundle + user).
POLICY=""
for i in $(seq 1 30); do
  POLICY="$(kubectl get clusterpolicy -o name 2>/dev/null | grep '^clusterpolicy.kyverno.io/educates-environment-' | head -1 || true)"
  [[ -n "$POLICY" ]] && break
  sleep 2
done
if [[ -z "$POLICY" ]]; then
  echo "[post-deploy] ✗ no educates-environment-* ClusterPolicy appeared within 60s" >&2
  echo "ClusterPolicies in cluster:" >&2
  kubectl get clusterpolicy >&2 || true
  exit 1
fi
echo "[post-deploy] ✓ runtime spawned ${POLICY}"

RULES="$(kubectl get "$POLICY" -o jsonpath='{.spec.rules[*].name}')"
echo "[post-deploy] rules: $RULES"

for rule in "$EXPECTED_OPS_RULE" "$EXPECTED_INTERNAL_RULE" "$EXPECTED_USER_WORKSHOP_RULE"; do
  if grep -qE "(^| )${rule}($| )" <<<"$RULES"; then
    echo "[post-deploy] ✓ per-env rule present: ${rule}"
  else
    echo "[post-deploy] ✗ per-env rule missing:  ${rule}" >&2
    fail=1
  fi
done

exit "$fail"
