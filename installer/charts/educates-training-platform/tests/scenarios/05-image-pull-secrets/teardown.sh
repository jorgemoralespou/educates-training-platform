#!/usr/bin/env bash
# Always-runs cleanup for scenario 05. Removes the docker registry
# container and the tmp dir pre-install.sh stashed its path into.
# Idempotent — invoked by run-scenario.sh from its EXIT trap regardless
# of whether the scenario passed or failed.

set -u
REGISTRY_NAME="educates-test-pull-registry"

docker rm -f "$REGISTRY_NAME" >/dev/null 2>&1 || true

if [[ -f /tmp/educates-scenario-05.workdir ]]; then
  WORKDIR="$(cat /tmp/educates-scenario-05.workdir 2>/dev/null || true)"
  if [[ -n "$WORKDIR" && -d "$WORKDIR" && "$WORKDIR" == /var/folders/* || "$WORKDIR" == /tmp/* ]]; then
    rm -rf "$WORKDIR"
  fi
  rm -f /tmp/educates-scenario-05.workdir
fi

echo "[teardown] scenario 05 cleanup complete"
