#!/usr/bin/env bash
# Stages the CA Secret that the v3 installer's certs package will use as
# its ClusterIssuer source. Invoked by run-scenario.sh between the
# cluster-only kind create and `educates admin platform deploy`.
#
# Inputs (provided by run-scenario.sh as env vars):
#   DOMAIN         required.
#   CA_CERT_PATH   optional. PEM CA cert.
#   CA_KEY_PATH    optional. PEM CA private key. The runner auto-detects
#                  the mkcert sibling (rootCA.pem ↔ rootCA-key.pem) when
#                  CA_KEY_PATH is left unset and CA_CERT_PATH is set.
#
# Resolution:
#   - CA_CERT_PATH + CA_KEY_PATH given → publish them as the CA Secret.
#   - Neither given → generate a self-signed CA + key on the fly.
#   - Only one given → fail (the runner enforces this; double-checking
#     here for defence in depth).

set -Eeuo pipefail

: "${DOMAIN:?DOMAIN env var must be set by the runner}"
SECRETS_NS="educates-secrets"
CA_SECRET="local-root-ca"

WORKDIR="$(mktemp -d -t educates-scenario-03-XXXX)"
echo "[pre-install] using workdir: $WORKDIR"

if [[ -n "${CA_CERT_PATH:-}" && -n "${CA_KEY_PATH:-}" ]]; then
  echo "[pre-install] using supplied CA:"
  echo "  ca-cert: ${CA_CERT_PATH}"
  echo "  ca-key:  ${CA_KEY_PATH}"
  cp "$CA_CERT_PATH" "$WORKDIR/ca.crt"
  cp "$CA_KEY_PATH"  "$WORKDIR/ca.key"
elif [[ -n "${CA_CERT_PATH:-}" || -n "${CA_KEY_PATH:-}" ]]; then
  echo "[pre-install] ERROR: both --ca-cert and --ca-key are required (got only one)." >&2
  exit 2
else
  echo "[pre-install] generating self-signed CA"
  openssl req -x509 -nodes -newkey rsa:4096 -days 3650 \
    -subj "/CN=Educates Test Root CA" \
    -keyout "$WORKDIR/ca.key" -out "$WORKDIR/ca.crt" \
    >/dev/null 2>&1
fi

kubectl create namespace "$SECRETS_NS" --dry-run=client -o yaml | kubectl apply -f -

# Single Secret with three keys:
#   tls.crt + tls.key — consumed by cert-manager's `ca:` ClusterIssuer.
#   ca.crt           — consumed by the v4 runtime for chain trust.
#
# Note that the v3 ytt overlay-localca.yaml uses a different Secret
# layout when caCertificate is given inline (it stores the CA *key* in
# a field named `tls.crt`, which looks like a v3 quirk we don't want to
# replicate). When using caCertificateRef, the user owns the layout, so
# we use the canonical cert-manager + chain-trust shape.
kubectl -n "$SECRETS_NS" create secret generic "$CA_SECRET" \
  --type=kubernetes.io/tls \
  --from-file=tls.crt="$WORKDIR/ca.crt" \
  --from-file=tls.key="$WORKDIR/ca.key" \
  --from-file=ca.crt="$WORKDIR/ca.crt" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "[pre-install] CA Secret created: ${SECRETS_NS}/${CA_SECRET}"
