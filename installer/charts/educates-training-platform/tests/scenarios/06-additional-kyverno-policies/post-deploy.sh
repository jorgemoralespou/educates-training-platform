#!/usr/bin/env bash
# End-to-end check that bundled + user-supplied Kyverno policies reach
# the cluster on both paths. Since the ClusterPolicy -> ValidatingPolicy
# migration the bundled policies are `ValidatingPolicy`
# (policies.kyverno.io); the scenario keeps its user-supplied markers as
# legacy `ClusterPolicy` to exercise the deprecated-but-supported path.
#
# 1. Cluster-wide path — the bundled Pod Security Standards profiles
#    (baseline + restricted) are installed by the chart as cluster-scoped
#    `ValidatingPolicy` resources, scoped to Educates-managed namespaces
#    via `spec.matchConstraints.namespaceSelector`. The user-supplied
#    `clusterSecurity.additionalKyvernoPolicies` marker is emitted
#    verbatim as a cluster-wide `ClusterPolicy`.
# 2. Per-workshop path — the bundled workshop policies (operational + the
#    internal require-ingress-session-name) are `ValidatingPolicy` objects
#    that session-manager clones per environment as
#    `educates-environment-<env>-<policy>`. The user-supplied
#    `workshopSecurity.additionalKyvernoPolicies` marker is a legacy
#    `ClusterPolicy`, so its rules are merged into a single
#    `educates-environment-<env>` ClusterPolicy.

set -Eeuo pipefail

# 1a. Bundled cluster-wide ValidatingPolicies (PSS baseline + restricted).
EXPECTED_BASELINE_VP="disallow-privileged-containers"
EXPECTED_RESTRICTED_VP="require-run-as-nonroot"
# 1b. User-supplied cluster-wide policies (verbatim): a legacy ClusterPolicy
#     and a new ValidatingPolicy.
EXPECTED_USER_CLUSTER_CP="scenario-06-cluster-marker"
EXPECTED_USER_CLUSTER_VP="scenario-06-cluster-marker-vp"
# 2a. Bundled + user-supplied per-environment ValidatingPolicies (name
#     suffixes). The user ValidatingPolicy marker is cloned as its own
#     scoped per-environment object, like the bundled ones.
EXPECTED_OPS_VP="no-loadbalancer-service"
EXPECTED_INTERNAL_VP="require-ingress-session-name"
EXPECTED_USER_WORKSHOP_VP="scenario-06-workshop-marker-vp"
# 2b. User-supplied per-environment rule from the legacy ClusterPolicy marker
#     (merged into the educates-environment-<env> ClusterPolicy).
EXPECTED_USER_WORKSHOP_RULE="scenario-06-workshop-marker"

fail=0

# 1a. Bundled cluster-wide ValidatingPolicies, scoped to Educates namespaces.
for vp in "$EXPECTED_BASELINE_VP" "$EXPECTED_RESTRICTED_VP"; do
  if ! kubectl get validatingpolicy "$vp" >/dev/null 2>&1; then
    echo "[post-deploy] ✗ cluster-wide ValidatingPolicy missing:  $vp" >&2
    fail=1
    continue
  fi
  KEYS="$(kubectl get validatingpolicy "$vp" \
    -o jsonpath='{.spec.matchConstraints.namespaceSelector.matchExpressions[*].key}' 2>/dev/null || true)"
  if grep -q 'training.educates.dev/policy.engine' <<<"$KEYS" \
     && grep -q 'training.educates.dev/policy.name' <<<"$KEYS"; then
    echo "[post-deploy] ✓ cluster-wide ValidatingPolicy present and namespace-scoped: $vp"
  else
    echo "[post-deploy] ✗ cluster-wide ValidatingPolicy $vp is not namespace-scoped (keys: ${KEYS:-none})" >&2
    fail=1
  fi
done

# 1b. User-supplied cluster-wide policies (verbatim): ClusterPolicy + VP.
if kubectl get clusterpolicy "$EXPECTED_USER_CLUSTER_CP" >/dev/null 2>&1; then
  echo "[post-deploy] ✓ cluster-wide ClusterPolicy present: $EXPECTED_USER_CLUSTER_CP"
else
  echo "[post-deploy] ✗ cluster-wide ClusterPolicy missing:  $EXPECTED_USER_CLUSTER_CP" >&2
  fail=1
fi
if kubectl get validatingpolicy "$EXPECTED_USER_CLUSTER_VP" >/dev/null 2>&1; then
  echo "[post-deploy] ✓ cluster-wide ValidatingPolicy present: $EXPECTED_USER_CLUSTER_VP"
else
  echo "[post-deploy] ✗ cluster-wide ValidatingPolicy missing:  $EXPECTED_USER_CLUSTER_VP" >&2
  fail=1
fi

# 2a. Per-environment bundled ValidatingPolicies. session-manager spawns
# these once the workshop environment is created; poll for them to appear.
VPS=""
for i in $(seq 1 30); do
  VPS="$(kubectl get validatingpolicy -o name 2>/dev/null \
    | grep '/educates-environment-' || true)"
  [[ -n "$VPS" ]] && break
  sleep 2
done
if [[ -z "$VPS" ]]; then
  echo "[post-deploy] ✗ no educates-environment-* ValidatingPolicy appeared within 60s" >&2
  echo "ValidatingPolicies in cluster:" >&2
  kubectl get validatingpolicy >&2 || true
  exit 1
fi
echo "[post-deploy] per-environment ValidatingPolicies:"
echo "$VPS" | sed 's/^/[post-deploy]   /'

for suffix in "$EXPECTED_OPS_VP" "$EXPECTED_INTERNAL_VP" "$EXPECTED_USER_WORKSHOP_VP"; do
  if grep -qE "/educates-environment-.*-${suffix}\$" <<<"$VPS"; then
    echo "[post-deploy] ✓ per-env ValidatingPolicy present: *-${suffix}"
  else
    echo "[post-deploy] ✗ per-env ValidatingPolicy missing:  *-${suffix}" >&2
    fail=1
  fi
done

# 2b. Per-environment legacy ClusterPolicy carrying the user marker rule.
POLICY=""
for i in $(seq 1 30); do
  POLICY="$(kubectl get clusterpolicy -o name 2>/dev/null \
    | grep '^clusterpolicy.kyverno.io/educates-environment-' | head -1 || true)"
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
if grep -qE "(^| )${EXPECTED_USER_WORKSHOP_RULE}($| )" <<<"$RULES"; then
  echo "[post-deploy] ✓ per-env rule present: ${EXPECTED_USER_WORKSHOP_RULE}"
else
  echo "[post-deploy] ✗ per-env rule missing:  ${EXPECTED_USER_WORKSHOP_RULE}" >&2
  fail=1
fi

exit "$fail"
