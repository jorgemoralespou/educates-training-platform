#!/usr/bin/env bash
# End-to-end check: the custom websiteTheme must reach the live portal.
# Curls the resolved PORTAL_URL and looks for the marker that
# chart-values.yaml put into `training-portal.html`. This validates the
# whole chain — chart → educates-config Secret → session-manager
# pickup → training-portal pod → HTTP response.

set -Eeuo pipefail

: "${PORTAL_URL:?PORTAL_URL must be set by the runner}"
EXPECTED="scenario-04-marker"

echo "[post-deploy] curling ${PORTAL_URL}"
HTML=""
# Retry — the portal Pod may still be coming up after deploy-workshop.
for i in $(seq 1 30); do
  if HTML="$(curl -fsSL -k "$PORTAL_URL" 2>/dev/null)"; then
    [[ -n "$HTML" ]] && break
  fi
  sleep 2
done

if [[ -z "$HTML" ]]; then
  echo "[post-deploy] ✗ no response body from ${PORTAL_URL} after ~60s" >&2
  exit 1
fi

if grep -q "$EXPECTED" <<<"$HTML"; then
  echo "[post-deploy] ✓ marker '${EXPECTED}' found in portal HTML"
else
  echo "[post-deploy] ✗ marker '${EXPECTED}' NOT found in portal HTML" >&2
  echo "--- response (first 40 lines) ---" >&2
  head -40 <<<"$HTML" >&2
  exit 1
fi
