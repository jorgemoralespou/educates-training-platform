#!/usr/bin/env bash
# Stages everything scenario 09 references from foreign namespaces:
#
#   1. Wildcard TLS Secret + CA Secret in `educates-secrets` (same
#      resolution logic and inputs as scenario 02 — TLS_CERT_PATH /
#      TLS_KEY_PATH / CA_CERT_PATH / CA_KEY_PATH, falling back to a
#      generated self-signed CA).
#   2. Two website-theme Secrets: `scenario-theme` (themeDataRefs +
#      defaultTheme) and `extra-theme` (upstream.websiteThemes copy).
#   3. A dummy docker-registry Secret `scenario-pull-secret`
#      (upstream.imagePullSecrets copy + by-name propagation). The
#      credentials are fake — only the plumbing is asserted.
#
# Invoked by run-scenario.sh between cluster create and helm install.

set -Eeuo pipefail

: "${DOMAIN:?DOMAIN env var must be set by the runner}"
SECRETS_NS="educates-secrets"
TLS_SECRET="wildcard-tls"
CA_SECRET="wildcard-ca"

WORKDIR="$(mktemp -d -t educates-scenario-09-XXXX)"
echo "[pre-install] using workdir: $WORKDIR"

# --- TLS material (same resolution order as scenario 02) -------------

if [[ -n "${TLS_CERT_PATH:-}" && -n "${TLS_KEY_PATH:-}" ]]; then
  echo "[pre-install] using supplied leaf cert"
  echo "  cert: ${TLS_CERT_PATH}"
  echo "  key:  ${TLS_KEY_PATH}"
  cp "$TLS_CERT_PATH" "$WORKDIR/tls.crt"
  cp "$TLS_KEY_PATH"  "$WORKDIR/tls.key"
  if [[ -n "${CA_CERT_PATH:-}" ]]; then
    cp "$CA_CERT_PATH" "$WORKDIR/ca.crt"
    echo "[pre-install] using supplied CA cert: ${CA_CERT_PATH}"
  else
    # No CA supplied; publish a copy of the leaf as the CA Secret so
    # the runtime has *something* to read.
    cp "$WORKDIR/tls.crt" "$WORKDIR/ca.crt"
  fi
elif [[ -n "${CA_CERT_PATH:-}" && -n "${CA_KEY_PATH:-}" ]]; then
  echo "[pre-install] signing fresh wildcard leaf for *.${DOMAIN} with supplied CA"
  echo "  ca-cert: ${CA_CERT_PATH}"
  echo "  ca-key:  ${CA_KEY_PATH}"
  cp "$CA_CERT_PATH" "$WORKDIR/ca.crt"
  cp "$CA_KEY_PATH"  "$WORKDIR/ca.key"
  openssl req -nodes -newkey rsa:2048 \
    -subj "/CN=*.${DOMAIN}" \
    -keyout "$WORKDIR/tls.key" -out "$WORKDIR/tls.csr" \
    >/dev/null 2>&1
  cat >"$WORKDIR/tls.ext" <<EOF
subjectAltName = DNS:*.${DOMAIN}, DNS:${DOMAIN}
extendedKeyUsage = serverAuth
EOF
  openssl x509 -req -in "$WORKDIR/tls.csr" \
    -CA "$WORKDIR/ca.crt" -CAkey "$WORKDIR/ca.key" -CAcreateserial \
    -out "$WORKDIR/tls.crt" -days 365 -sha256 \
    -extfile "$WORKDIR/tls.ext" \
    >/dev/null 2>&1
elif [[ -n "${CA_CERT_PATH:-}" && -z "${CA_KEY_PATH:-}" ]]; then
  echo "[pre-install] ERROR: --ca-cert was supplied but --ca-key is missing." >&2
  echo "  Pass --ca-key <path>, or omit --ca-cert to fall back to a self-signed CA." >&2
  echo "  For mkcert, the key is typically at \$(mkcert -CAROOT)/rootCA-key.pem" >&2
  exit 2
else
  echo "[pre-install] generating self-signed CA + wildcard leaf for *.${DOMAIN}"
  openssl req -x509 -nodes -newkey rsa:4096 -days 3650 \
    -subj "/CN=Educates Test Root CA" \
    -keyout "$WORKDIR/ca.key" -out "$WORKDIR/ca.crt" \
    >/dev/null 2>&1
  openssl req -nodes -newkey rsa:2048 \
    -subj "/CN=*.${DOMAIN}" \
    -keyout "$WORKDIR/tls.key" -out "$WORKDIR/tls.csr" \
    >/dev/null 2>&1
  cat >"$WORKDIR/tls.ext" <<EOF
subjectAltName = DNS:*.${DOMAIN}, DNS:${DOMAIN}
extendedKeyUsage = serverAuth
EOF
  openssl x509 -req -in "$WORKDIR/tls.csr" \
    -CA "$WORKDIR/ca.crt" -CAkey "$WORKDIR/ca.key" -CAcreateserial \
    -out "$WORKDIR/tls.crt" -days 365 -sha256 \
    -extfile "$WORKDIR/tls.ext" \
    >/dev/null 2>&1
fi

kubectl create namespace "$SECRETS_NS" --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$SECRETS_NS" create secret tls "$TLS_SECRET" \
  --cert="$WORKDIR/tls.crt" --key="$WORKDIR/tls.key" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$SECRETS_NS" create secret generic "$CA_SECRET" \
  --from-file=ca.crt="$WORKDIR/ca.crt" \
  --dry-run=client -o yaml | kubectl apply -f -

# --- Website themes ---------------------------------------------------
# Theme Secrets carry the same flat key shape the chart's inline-theme
# helper produces (training-portal.{html,js,css}, etc.).

cat >"$WORKDIR/theme-training-portal.html" <<'EOF'
<div id="scenario-09-theme-marker"></div>
EOF
kubectl -n "$SECRETS_NS" create secret generic scenario-theme \
  --from-file=training-portal.html="$WORKDIR/theme-training-portal.html" \
  --dry-run=client -o yaml | kubectl apply -f -

cat >"$WORKDIR/extra-theme-training-portal.html" <<'EOF'
<div id="scenario-09-extra-theme-marker"></div>
EOF
kubectl -n "$SECRETS_NS" create secret generic extra-theme \
  --from-file=training-portal.html="$WORKDIR/extra-theme-training-portal.html" \
  --dry-run=client -o yaml | kubectl apply -f -

# --- Dummy pull secret ------------------------------------------------
# Fake credentials: only copy + propagation is asserted, no real pulls.

kubectl -n "$SECRETS_NS" create secret docker-registry scenario-pull-secret \
  --docker-server=registry.scenario-09.invalid \
  --docker-username=scenario --docker-password=scenario-09 \
  --dry-run=client -o yaml | kubectl apply -f -

echo "[pre-install] secrets in ${SECRETS_NS}: ${TLS_SECRET}, ${CA_SECRET}, scenario-theme, extra-theme, scenario-pull-secret"
