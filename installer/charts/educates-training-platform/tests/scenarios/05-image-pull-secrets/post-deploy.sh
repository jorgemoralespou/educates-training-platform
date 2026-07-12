#!/usr/bin/env bash
# Asserts the session-manager Pod actually pulled its image from the
# auth'd local registry. By the time this hook runs, rollout-status
# has already succeeded — so the Pod is up. Greps the Pod's events to
# confirm the pull came from our registry, not from a stale node cache
# or an unexpected fallback.

set -Eeuo pipefail

OP_NS="educates"
EXPECTED_REGISTRY="educates-test-pull-registry:5000"

POD="$(kubectl -n "$OP_NS" get pod -l deployment=session-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "$POD" ]]; then
  echo "[post-deploy] ✗ no session-manager Pod found in ${OP_NS}" >&2
  exit 1
fi

EVENTS="$(kubectl -n "$OP_NS" describe pod "$POD" 2>/dev/null || true)"

if grep -qE "Successfully pulled image .*${EXPECTED_REGISTRY}" <<<"$EVENTS"; then
  echo "[post-deploy] ✓ session-manager pulled from ${EXPECTED_REGISTRY}"
elif grep -qE "Container image .*${EXPECTED_REGISTRY}.* already present" <<<"$EVENTS"; then
  echo "[post-deploy] ✓ session-manager image (${EXPECTED_REGISTRY}) already cached on node"
else
  echo "[post-deploy] ✗ no pull from ${EXPECTED_REGISTRY} observed for ${OP_NS}/${POD}" >&2
  echo "--- events tail ---" >&2
  echo "$EVENTS" | grep -A0 -E "Pulling|Pulled|Failed|ImagePull|ErrImagePull|BackOff" >&2 || true
  exit 1
fi
