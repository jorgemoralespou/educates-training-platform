#!/usr/bin/env bash
# Stages the pull secret in the operator namespace immediately after
# helm has applied the chart manifests. The session-manager Deployment
# already exists at this point and its first Pod creation may have
# already failed to pull (ImagePullBackOff) — that's fine, the kubelet
# retries and the next attempt will succeed once this Secret is in
# place.
#
# secrets-manager will eventually take over keeping this Secret in
# sync (driven by the chart's secretPropagation.upstream.imagePullSecrets
# rule), but secrets-manager isn't necessarily Ready yet either, so
# we materialise the in-namespace copy directly here.

set -Eeuo pipefail

OP_NS="educates"
PULL_SECRET="test-pull-secret"
REGISTRY_INTERNAL_HOST="educates-test-pull-registry:5000"
REGISTRY_USER="educates-test"
REGISTRY_PASS="educates-test-pass"

kubectl -n "$OP_NS" create secret docker-registry "$PULL_SECRET" \
  --docker-server="$REGISTRY_INTERNAL_HOST" \
  --docker-username="$REGISTRY_USER" \
  --docker-password="$REGISTRY_PASS" \
  --docker-email="test@invalid" \
  --dry-run=client -o yaml | kubectl apply -f -

echo "[post-install] ${PULL_SECRET} created in ${OP_NS}"
