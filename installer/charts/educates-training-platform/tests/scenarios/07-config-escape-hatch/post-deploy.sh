#!/usr/bin/env bash
# Asserts the chart's `session-manager.config` escape hatch deep-merges
# correctly on top of the typed-derived runtime config:
#
#   1. `dockerDaemon.networkMTU: 1450` overrides the typed default of 1400.
#   2. `experimental.markerKey: scenario-07-marker` is passed through
#      untouched (a key not in the typed surface).
#
# Reads the live `educates-config` Secret in the operator namespace.

set -Eeuo pipefail

NS="${OPERATOR_NAMESPACE:-educates}"

echo "[post-deploy] reading educates-operator-config.yaml from secret/educates-config in ${NS}"
CFG="$(kubectl -n "$NS" get secret educates-config -o jsonpath='{.data.educates-operator-config\.yaml}' | base64 -d)"

if [[ -z "$CFG" ]]; then
  echo "[post-deploy] ✗ secret content empty" >&2
  exit 1
fi

fail=0

if grep -qE '^[[:space:]]*networkMTU:[[:space:]]*1450[[:space:]]*$' <<<"$CFG"; then
  echo "[post-deploy] ✓ dockerDaemon.networkMTU override (1400 → 1450) honoured"
else
  echo "[post-deploy] ✗ dockerDaemon.networkMTU not 1450" >&2
  fail=1
fi

if grep -qE '^[[:space:]]*markerKey:[[:space:]]*scenario-07-marker[[:space:]]*$' <<<"$CFG"; then
  echo "[post-deploy] ✓ experimental.markerKey passed through"
else
  echo "[post-deploy] ✗ experimental.markerKey not found" >&2
  fail=1
fi

if (( fail )); then
  echo "--- educates-operator-config.yaml ---" >&2
  cat <<<"$CFG" >&2
  exit 1
fi
