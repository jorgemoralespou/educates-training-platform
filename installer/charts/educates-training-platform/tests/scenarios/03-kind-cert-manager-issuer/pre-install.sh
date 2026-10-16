#!/usr/bin/env bash
# Stages the CA key pair that the v3 installer's certs package will use as
# its ClusterIssuer source, and the key-free CA trust Secret the runtime
# chart mounts. Invoked by run-scenario.sh between the cluster-only kind
# create and `educates admin platform deploy`.
#
# Inputs (provided by run-scenario.sh as env vars):
#   DOMAIN         required.
#   CA_CERT_PATH   optional. PEM CA cert.
#   CA_KEY_PATH    optional. PEM CA private key. The runner auto-detects
#                  the mkcert sibling (rootCA.pem ↔ rootCA-key.pem) when
#                  CA_KEY_PATH is left unset and CA_CERT_PATH is set.
#
# Resolution:
#   - CA_CERT_PATH + CA_KEY_PATH given → publish them as the CA Secrets.
#   - Neither given → generate a self-signed CA + key on the fly.
#   - Only one given → fail (the runner enforces this; double-checking
#     here for defence in depth).

set -Eeuo pipefail

: "${DOMAIN:?DOMAIN env var must be set by the runner}"
SECRETS_NS="educates-secrets"
CA_SECRET="local-root-ca"
CA_KEY_PAIR_SECRET="local-root-ca-key-pair"

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

# CA trust and CA signing material are separate Secrets, so the CA's
# private key never reaches the runtime:
#   local-root-ca-key-pair: the CA key pair (tls.crt + tls.key) consumed
#                           by cert-manager's `ca:` ClusterIssuer.
#   local-root-ca:          the CA trust Secret, only ca.crt, which the
#                           chart copies to the runtime for chain trust.
#
# Note that the v3 ytt overlay-localca.yaml uses a different Secret
# layout when caCertificate is given inline (it stores the CA *key* in
# a field named `tls.crt`, which looks like a v3 quirk we don't want to
# replicate). When using caCertificateRef, the user owns the layout, so
# we use the canonical cert-manager shape.
kubectl -n "$SECRETS_NS" create secret tls "$CA_KEY_PAIR_SECRET" \
  --cert="$WORKDIR/ca.crt" \
  --key="$WORKDIR/ca.key" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "$SECRETS_NS" create secret generic "$CA_SECRET" \
  --from-file=ca.crt="$WORKDIR/ca.crt" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "[pre-install] CA key pair created: ${SECRETS_NS}/${CA_KEY_PAIR_SECRET}"
echo "[pre-install] CA trust Secret created: ${SECRETS_NS}/${CA_SECRET}"
