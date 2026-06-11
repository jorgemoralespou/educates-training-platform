#!/usr/bin/env bash
# Kitchen-sink assertions, in four groups:
#
#   1. Typed-values serialisation — every populated session-manager
#      value block must appear in the rendered `educates-config`
#      Secret (the runtime config blob).
#   2. Secret plumbing — both theme Secrets and the pull Secret must
#      have been copied into the release namespace by the chart's
#      SecretCopiers; the inline theme assets must be in the
#      `default-website-theme` Secret.
#   3. Optional subcharts — lookup-service, remote-access,
#      node-ca-injector, and the image-puller DaemonSet are all live.
#   4. Theme end-to-end — the portal serves the external
#      `scenario-theme` marker (defaultTheme points at it).

set -Eeuo pipefail

: "${PORTAL_URL:?PORTAL_URL must be set by the runner}"
NS="${OPERATOR_NAMESPACE:-educates}"
fail=0

# --- 1. Typed-values serialisation ------------------------------------

echo "[post-deploy] reading educates-operator-config.yaml from secret/educates-config in ${NS}"
CFG="$(kubectl -n "$NS" get secret educates-config -o jsonpath='{.data.educates-operator-config\.yaml}' | base64 -d)"
if [[ -z "$CFG" ]]; then
  echo "[post-deploy] ✗ secret content empty" >&2
  exit 1
fi

assert_cfg() { # <label> <grep -E pattern>
  if grep -qE "$2" <<<"$CFG"; then
    echo "[post-deploy] ✓ config blob: $1"
  else
    echo "[post-deploy] ✗ config blob missing: $1 (pattern: $2)" >&2
    fail=1
  fi
}

assert_cfg "dockerDaemon.networkMTU"          '^[[:space:]]*networkMTU:[[:space:]]*1450[[:space:]]*$'
assert_cfg "dockerDaemon.proxyCache.remoteURL" 'remoteURL:.*registry-1\.docker\.io'
assert_cfg "workshopAnalytics.google"          'trackingId:[[:space:]]*G-SCENARIO09'
assert_cfg "workshopAnalytics.clarity"         'trackingId:[[:space:]]*clarity-scenario-09'
assert_cfg "workshopAnalytics.amplitude"       'trackingId:[[:space:]]*amp-scenario-09'
assert_cfg "workshopAnalytics.webhook"         'url:.*analytics\.example\.com/scenario-09'
assert_cfg "websiteStyling.defaultTheme"       'defaultTheme:[[:space:]]*scenario-theme'
assert_cfg "websiteStyling.frameAncestors"     'embed\.example\.com'
assert_cfg "clusterNetwork.blockCIDRs"         '169\.254\.169\.254/32'
assert_cfg "imageVersions appended entry"      'scenario-09-extra-image'
assert_cfg "config escape-hatch passthrough"   'markerKey:[[:space:]]*scenario-09-marker'

# --- 2. Secret plumbing ------------------------------------------------

assert_secret() { # <name>
  if kubectl -n "$NS" get secret "$1" >/dev/null 2>&1; then
    echo "[post-deploy] ✓ secret/$1 copied into ${NS}"
  else
    echo "[post-deploy] ✗ secret/$1 not found in ${NS}" >&2
    fail=1
  fi
}

# SecretCopier effects can lag the deploy by a reconcile cycle.
for i in $(seq 1 30); do
  kubectl -n "$NS" get secret scenario-theme extra-theme scenario-pull-secret \
    >/dev/null 2>&1 && break
  sleep 2
done
assert_secret scenario-theme
assert_secret extra-theme
assert_secret scenario-pull-secret

INLINE_HTML="$(kubectl -n "$NS" get secret default-website-theme -o jsonpath='{.data.training-portal\.html}' 2>/dev/null | base64 -d || true)"
if grep -q "scenario-09-inline-marker" <<<"$INLINE_HTML"; then
  echo "[post-deploy] ✓ inline theme assets in default-website-theme Secret"
else
  echo "[post-deploy] ✗ inline marker not in default-website-theme Secret" >&2
  fail=1
fi

# --- 3. Optional subcharts ----------------------------------------------

assert_rollout() { # <kind/name>
  if kubectl -n "$NS" rollout status "$1" --timeout=120s >/dev/null 2>&1; then
    echo "[post-deploy] ✓ $1 rolled out"
  else
    echo "[post-deploy] ✗ $1 not rolled out" >&2
    fail=1
  fi
}

assert_rollout deployment/lookup-service
assert_rollout deployment/node-ca-injector-controller
assert_rollout daemonset/node-ca-injector
assert_rollout daemonset/image-puller
assert_secret remote-access-token

if kubectl -n "$NS" get ingress lookup-service >/dev/null 2>&1; then
  echo "[post-deploy] ✓ lookup-service Ingress rendered"
else
  echo "[post-deploy] ✗ lookup-service Ingress missing" >&2
  fail=1
fi

# Kyverno extras: the cluster marker is applied directly as a
# ClusterPolicy; the workshop marker goes into the per-workshop feed
# (the kyverno-policies.yaml key of the educates-config Secret).
# Per-environment clone depth is scenario 06's job.
if kubectl get clusterpolicy scenario-09-cluster-marker >/dev/null 2>&1; then
  echo "[post-deploy] ✓ user-supplied cluster ClusterPolicy applied"
else
  echo "[post-deploy] ✗ clusterpolicy/scenario-09-cluster-marker missing" >&2
  fail=1
fi
WPOL="$(kubectl -n "$NS" get secret educates-config -o jsonpath='{.data.kyverno-policies\.yaml}' 2>/dev/null | base64 -d || true)"
if grep -q "scenario-09-workshop-marker" <<<"$WPOL"; then
  echo "[post-deploy] ✓ workshop marker policy in kyverno-policies.yaml feed"
else
  echo "[post-deploy] ✗ workshop marker policy not in kyverno-policies.yaml feed" >&2
  fail=1
fi

# --- 4. Theme end-to-end -------------------------------------------------

echo "[post-deploy] curling ${PORTAL_URL}"
HTML=""
for i in $(seq 1 30); do
  if HTML="$(curl -fsSL -k "$PORTAL_URL" 2>/dev/null)"; then
    [[ -n "$HTML" ]] && break
  fi
  sleep 2
done
if grep -q "scenario-09-theme-marker" <<<"$HTML"; then
  echo "[post-deploy] ✓ external defaultTheme marker served by portal"
else
  echo "[post-deploy] ✗ external theme marker NOT in portal HTML" >&2
  echo "--- response (first 40 lines) ---" >&2
  head -40 <<<"$HTML" >&2
  fail=1
fi

exit "$fail"
