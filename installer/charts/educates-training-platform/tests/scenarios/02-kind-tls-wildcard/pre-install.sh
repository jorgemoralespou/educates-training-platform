#!/usr/bin/env bash
# Materialises a wildcard TLS Secret and a CA Secret in the
# `educates-secrets` namespace, where the chart's
# `secretPropagation.upstream.{ingressTLS,ingressCA}` rules pull them
# into the operator namespace. Invoked by run-scenario.sh between
# cluster create and helm install.
#
# Inputs (provided by run-scenario.sh as env vars):
#   DOMAIN         required. Wildcard domain (e.g., "192.168.1.5.nip.io").
#   TLS_CERT_PATH  optional. Pre-existing leaf cert.
#   TLS_KEY_PATH   optional. Pre-existing private key. Required when
#                  TLS_CERT_PATH is set.
#   CA_CERT_PATH   optional. CA cert PEM.
#   CA_KEY_PATH    optional. CA private key PEM.
#
# Resolution order (first match wins):
#   1. TLS_CERT_PATH + TLS_KEY_PATH given → use them as the leaf.
#   2. CA_CERT_PATH + CA_KEY_PATH given → generate a fresh wildcard
#      leaf signed by that CA.
#   3. Otherwise → generate a self-signed CA + sign a fresh wildcard
#      leaf with it.
#
# The CA Secret published to the cluster is whichever CA is actually
# in play (supplied or generated), so the runtime trusts the chain.

set -Eeuo pipefail

: "${DOMAIN:?DOMAIN env var must be set by the runner}"
SECRETS_NS="educates-secrets"
TLS_SECRET="wildcard-tls"
CA_SECRET="wildcard-ca"

WORKDIR="$(mktemp -d -t educates-scenario-02-XXXX)"
echo "[pre-install] using workdir: $WORKDIR"

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
    # the runtime has *something* to read. Tools that strictly verify
    # chain-of-trust may reject it, but the leaf is self-signed.
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

echo "[pre-install] secrets in ${SECRETS_NS}: ${TLS_SECRET}, ${CA_SECRET}"
