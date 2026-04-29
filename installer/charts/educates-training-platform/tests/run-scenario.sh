#!/usr/bin/env bash
# End-to-end runner for an educates-training-platform chart scenario.
#
# Steps:
#   1a. educates local cluster create --cluster-only --config <educates-config.yaml>
#       (kind cluster only, no platform install)
#   1b. (optional) <scenario>/pre-install.sh  — stage cluster-side fixtures
#       (e.g., create TLS / CA Secrets in foreign namespaces) before the
#       v3 installer runs.
#   1c. educates admin platform deploy --config <educates-config.yaml>
#       (installs cluster prerequisites; v3 Educates package is disabled)
#   2. helm install ... -f <chart-values.yaml>  (installs v4 runtime)
#   3. educates cluster portal create
#   4. educates deploy-workshop -f <workshop URL>
#   5. Pause for manual / Playwright verification (URL printed)
#   6. educates local cluster delete
#
# Usage:
#   ./run-scenario.sh <scenario-id> [--keep] [--no-wait] \
#                                   [--domain <domain>] \
#                                   [--tls-cert <path> --tls-key <path>] \
#                                   [--ca-cert <path>]
#
#   --keep      Skip the cluster-delete step (keep cluster around for inspection).
#   --no-wait   Skip the interactive pause before teardown.
#   --domain    Wildcard ingress domain. Defaults to "<host-ip>.nip.io"
#               using the first non-loopback IPv4 interface, matching the
#               educates CLI's GetHostIpAsDns().
#   --tls-cert  Path to PEM-encoded TLS leaf cert. Used by scenario
#               pre-install hooks that need a wildcard cert; falls back
#               to a self-signed cert generated at runtime when omitted.
#   --tls-key   Path to PEM-encoded TLS private key. Required when
#               --tls-cert is set.
#   --ca-cert   Path to PEM-encoded CA cert that signed --tls-cert. When
#               provided, scenarios use it instead of a self-signed CA;
#               browsers that trust this CA will not warn.
#
# Templating:
#   Scenario files (educates-config.yaml, chart-values.yaml, pre-install.sh)
#   are passed through `envsubst` before use. Any `${DOMAIN}` token is
#   substituted with the resolved domain. Other tokens
#   (TLS_CERT_PATH, TLS_KEY_PATH, CA_CERT_PATH) are exported for use by
#   pre-install.sh.

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHART_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Load tests/.env first so its values become defaults that flags and any
# pre-set shell env vars can override. Each line is `KEY=VALUE` (with
# optional surrounding quotes); blank lines and `#` comments are
# ignored. Env vars already set in the calling shell are not overwritten
# — flags > shell env > .env > built-in defaults.
load_env_file() {
  local f="$1"
  [[ -f "$f" ]] || return 0
  echo "[runner] loading env file: $f"

  # Snapshot any vars listed in the file that are already set in the
  # environment, so shell-set values win over .env-set values. Bash
  # itself sources the file (handles $HOME, ~, quoted paths with
  # spaces, etc.).
  declare -A _preset=()
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[[:space:]]*# || -z "${line// }" ]] && continue
    [[ "$line" =~ ^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)= ]] || continue
    local k="${BASH_REMATCH[1]}"
    if [[ -n "${!k:-}" ]]; then
      _preset[$k]="${!k}"
    fi
  done < "$f"

  set -a
  # shellcheck disable=SC1090
  source "$f"
  set +a

  local k
  for k in "${!_preset[@]}"; do
    export "$k"="${_preset[$k]}"
  done
}

load_env_file "${SCRIPT_DIR}/.env"

SCENARIO=""
KEEP=0
NO_WAIT=0
DOMAIN="${DOMAIN:-}"
TLS_CERT_PATH="${TLS_CERT_PATH:-}"
TLS_KEY_PATH="${TLS_KEY_PATH:-}"
CA_CERT_PATH="${CA_CERT_PATH:-}"
CA_KEY_PATH="${CA_KEY_PATH:-}"
WORKSHOP_URL="https://github.com/educates/lab-k8s-fundamentals/releases/download/8.4/workshop.yaml"

while (( $# )); do
  case "$1" in
    --keep)      KEEP=1 ;;
    --no-wait)   NO_WAIT=1 ;;
    --domain)    DOMAIN="$2"; shift ;;
    --tls-cert)  TLS_CERT_PATH="$2"; shift ;;
    --tls-key)   TLS_KEY_PATH="$2"; shift ;;
    --ca-cert)   CA_CERT_PATH="$2"; shift ;;
    --ca-key)    CA_KEY_PATH="$2"; shift ;;
    -h|--help)
      sed -n '2,/^set -Eeuo/p' "$0" | sed 's/^# \{0,1\}//' | head -n -1
      exit 0
      ;;
    -*)
      echo "unknown flag: $1" >&2; exit 2 ;;
    *)
      [[ -n "$SCENARIO" ]] && { echo "only one scenario id allowed" >&2; exit 2; }
      SCENARIO="$1"
      ;;
  esac
  shift
done

if [[ -z "$SCENARIO" ]]; then
  echo "usage: $0 <scenario-id> [flags…]" >&2
  echo "available scenarios:" >&2
  ls "${SCRIPT_DIR}/scenarios" >&2
  exit 2
fi

SCEN_DIR="${SCRIPT_DIR}/scenarios/${SCENARIO}"
SRC_EDUCATES_CONFIG="${SCEN_DIR}/educates-config.yaml"
SRC_CHART_VALUES="${SCEN_DIR}/chart-values.yaml"

if [[ ! -d "$SCEN_DIR" ]]; then
  echo "scenario '$SCENARIO' not found at $SCEN_DIR" >&2
  exit 2
fi
for f in "$SRC_EDUCATES_CONFIG" "$SRC_CHART_VALUES"; do
  [[ -f "$f" ]] || { echo "missing: $f" >&2; exit 2; }
done

# Cert-flag consistency.
if [[ -n "$TLS_CERT_PATH" || -n "$TLS_KEY_PATH" ]]; then
  if [[ -z "$TLS_CERT_PATH" || -z "$TLS_KEY_PATH" ]]; then
    echo "--tls-cert and --tls-key must be provided together" >&2; exit 2
  fi
  [[ -f "$TLS_CERT_PATH" ]] || { echo "--tls-cert: file not found: $TLS_CERT_PATH" >&2; exit 2; }
  [[ -f "$TLS_KEY_PATH"  ]] || { echo "--tls-key: file not found: $TLS_KEY_PATH"  >&2; exit 2; }
fi
if [[ -n "$CA_CERT_PATH" ]]; then
  [[ -f "$CA_CERT_PATH" ]] || { echo "--ca-cert: file not found: $CA_CERT_PATH" >&2; exit 2; }
  # Auto-derive the CA private key from the mkcert sibling convention
  # (rootCA.pem ↔ rootCA-key.pem) when --ca-key wasn't passed explicitly.
  if [[ -z "$CA_KEY_PATH" ]]; then
    CANDIDATE="${CA_CERT_PATH%.pem}-key.pem"
    if [[ -f "$CANDIDATE" ]]; then
      CA_KEY_PATH="$CANDIDATE"
      echo "[runner] auto-detected CA key (mkcert convention): $CA_KEY_PATH"
    fi
  fi
fi
if [[ -n "$CA_KEY_PATH" ]]; then
  [[ -f "$CA_KEY_PATH" ]] || { echo "--ca-key: file not found: $CA_KEY_PATH" >&2; exit 2; }
  [[ -n "$CA_CERT_PATH" ]] || { echo "--ca-key requires --ca-cert" >&2; exit 2; }
fi

step() { printf '\n\033[1;36m== %s ==\033[0m\n' "$*"; }
ok()   { printf '\033[1;32m[ok]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[warn]\033[0m %s\n' "$*" >&2; }
fail() { printf '\033[1;31m[fail]\033[0m %s\n' "$*" >&2; }

# Resolve domain default if not provided.
if [[ -z "$DOMAIN" ]]; then
  HOST_IP=""
  for iface in en0 en1 en2 en3 en4; do
    HOST_IP="$(ipconfig getifaddr "$iface" 2>/dev/null || true)"
    [[ -n "$HOST_IP" ]] && break
  done
  if [[ -z "$HOST_IP" ]] && command -v hostname >/dev/null 2>&1; then
    HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
  fi
  if [[ -z "$HOST_IP" || "$HOST_IP" == "127.0.0.1" ]]; then
    fail "could not detect a non-loopback host IP. Pass --domain explicitly."
    exit 2
  fi
  DOMAIN="${HOST_IP}.nip.io"
  echo "[runner] auto-detected domain: ${DOMAIN}"
fi

export DOMAIN TLS_CERT_PATH TLS_KEY_PATH CA_CERT_PATH CA_KEY_PATH

# Render templated scenario files into a tmp dir; envsubst replaces
# ${DOMAIN} (and any other exported $VARs the templates use).
RENDER_DIR="$(mktemp -d -t educates-scenario-render-XXXX)"
envsubst < "$SRC_EDUCATES_CONFIG" > "${RENDER_DIR}/educates-config.yaml"
envsubst < "$SRC_CHART_VALUES"    > "${RENDER_DIR}/chart-values.yaml"
EDUCATES_CONFIG="${RENDER_DIR}/educates-config.yaml"
CHART_VALUES="${RENDER_DIR}/chart-values.yaml"

# Always attempt to clean up the cluster on early exit unless --keep.
# Scenario `teardown.sh` (if present) runs first so it can drop docker
# containers / tmp dirs created by `pre-install.sh`, regardless of
# whether the scenario passed or failed.
cleanup() {
  local rc=$?
  if [[ -x "${SCEN_DIR:-}/teardown.sh" ]]; then
    echo "[runner] scenario teardown" >&2
    "${SCEN_DIR}/teardown.sh" || true
  fi
  if [[ $rc -ne 0 && $KEEP -eq 0 ]]; then
    warn "scenario failed (rc=$rc); deleting cluster"
    educates local cluster delete >/dev/null 2>&1 || true
  fi
  return $rc
}
trap cleanup EXIT

step "Scenario: ${SCENARIO}"
echo "  domain:    ${DOMAIN}"
echo "  tls-cert:  ${TLS_CERT_PATH:-(self-signed at runtime)}"
echo "  tls-key:   ${TLS_KEY_PATH:-}"
echo "  ca-cert:   ${CA_CERT_PATH:-(self-signed at runtime)}"
echo "  ca-key:    ${CA_KEY_PATH:-}"
echo "  rendered:  ${RENDER_DIR}"
echo
cat "${SCEN_DIR}/description.md" 2>/dev/null | sed 's/^/   /' || true

step "1a/6 Create kind cluster (no platform install yet)"
educates local cluster create --cluster-only --config "$EDUCATES_CONFIG"
ok "cluster up"

if [[ -x "${SCEN_DIR}/pre-install.sh" ]]; then
  step "1b/6 Run scenario pre-install hook"
  "${SCEN_DIR}/pre-install.sh"
  ok "pre-install done"
fi

step "1c/6 Install v3 prereqs (educates admin platform deploy)"
educates admin platform deploy --config "$EDUCATES_CONFIG"
ok "platform prereqs deployed"

step "2/6  Install v4 runtime chart"
# No --wait here. The post-install hook may need to stage things in
# the operator namespace (e.g., a pull secret the session-manager Pod
# needs to start) before the Deployments become Ready. The
# rollout-status step below is the actual readiness gate.
helm upgrade --install educates-runtime "$CHART_DIR" \
  --namespace educates --create-namespace \
  -f "$CHART_VALUES" \
  --timeout 5m
ok "chart manifests applied"

if [[ -x "${SCEN_DIR}/post-install.sh" ]]; then
  step "2a/6 Run scenario post-install hook"
  "${SCEN_DIR}/post-install.sh"
  ok "post-install done"
fi

step "2b/6 Wait for operators to be Ready"
kubectl -n educates rollout status deployment/session-manager --timeout=5m
kubectl -n educates rollout status deployment/secrets-manager --timeout=5m
ok "operators ready"

step "3/6  Create training portal"
educates cluster portal create
ok "portal created"

step "4/6  Deploy workshop"
educates deploy-workshop -f "$WORKSHOP_URL"
ok "workshop deployed: $WORKSHOP_URL"

step "Resolving portal URL"
PORTAL_URL=""
for i in $(seq 1 30); do
  PORTAL_URL="$(kubectl get trainingportal educates-cli -o jsonpath='{.status.educates.url}' 2>/dev/null || true)"
  [[ -n "$PORTAL_URL" ]] && break
  sleep 2
done
if [[ -z "$PORTAL_URL" ]]; then
  warn "could not resolve portal URL from trainingportal/educates-cli within 60s"
fi
export PORTAL_URL

if [[ -x "${SCEN_DIR}/post-deploy.sh" ]]; then
  step "4b/6 Run scenario post-deploy hook"
  "${SCEN_DIR}/post-deploy.sh"
  ok "post-deploy done"
fi

step "5/6  Ready for manual verification"

# Pure-bash percent-encoder (RFC 3986 unreserved set). Passwords from
# `educates cluster portal password` can contain `%`, `@`, `#`, `^`,
# etc., all of which break a raw query-string.
url_encode() {
  local s="$1" out="" i c
  for (( i=0; i<${#s}; i++ )); do
    c="${s:i:1}"
    case "$c" in
      [-_.~A-Za-z0-9]) out+="$c" ;;
      *)               printf -v c '%%%02X' "'$c"; out+="$c" ;;
    esac
  done
  printf '%s' "$out"
}

if [[ -z "$PORTAL_URL" ]]; then
  echo "Portal:   (use 'educates cluster portal open' to launch the browser)"
else
  PORTAL_PASS="$(educates cluster portal password 2>/dev/null | tr -d '[:space:]' || true)"
  if [[ -n "$PORTAL_PASS" ]]; then
    PORTAL_PASS_ENC="$(url_encode "$PORTAL_PASS")"
    # The /workshops/access/ endpoint accepts ?password=<pw> for a
    # one-click skip of the access-code prompt.
    ACCESS_URL="${PORTAL_URL%/}/workshops/access/?password=${PORTAL_PASS_ENC}&redirect_url=%2F"
    echo "Portal:   $PORTAL_URL"
    echo "Password: $PORTAL_PASS"
    echo "Direct:   $ACCESS_URL"
  else
    echo "Portal:   $PORTAL_URL  (could not resolve password — run 'educates cluster portal password')"
  fi
fi
echo
if [[ $NO_WAIT -eq 0 ]]; then
  read -r -p "Press Enter once verification is complete to proceed to teardown… "
fi

if [[ $KEEP -eq 1 ]]; then
  step "6/6  --keep set; leaving cluster running"
else
  step "6/6  Delete cluster"
  educates local cluster delete
  ok "cluster deleted"
fi

step "Result: PASS for scenario ${SCENARIO}"
