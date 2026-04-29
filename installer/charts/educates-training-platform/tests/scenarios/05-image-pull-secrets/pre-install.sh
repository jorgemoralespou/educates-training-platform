#!/usr/bin/env bash
# Stands up an htpasswd-protected docker registry on the educates docker
# network, mirrors educates-session-manager:3.7.1 into it, configures
# the kind control-plane node's containerd to accept it, and stages the
# pull secret in both `educates-secrets` and `educates` namespaces.
#
# Anything that fails here will fail the scenario early. The registry
# container and tmp dir are cleaned up by teardown.sh, which the runner
# calls in its cleanup trap regardless of outcome.

set -Eeuo pipefail

REGISTRY_NAME="educates-test-pull-registry"
REGISTRY_USER="educates-test"
REGISTRY_PASS="educates-test-pass"
REGISTRY_HOST_PORT="5003"
REGISTRY_INNER_PORT="5000"
REGISTRY_INTERNAL_HOST="${REGISTRY_NAME}:${REGISTRY_INNER_PORT}"
REGISTRY_LOGIN_HOST="localhost:${REGISTRY_HOST_PORT}"
DOCKER_NET="educates"
KIND_CONTROL_PLANE="educates-control-plane"

UPSTREAM_IMAGE="ghcr.io/educates/educates-session-manager:3.7.1"
LOCAL_PATH="educates/educates-session-manager:3.7.1"
LOGIN_REF="${REGISTRY_LOGIN_HOST}/${LOCAL_PATH}"
INTERNAL_REF="${REGISTRY_INTERNAL_HOST}/${LOCAL_PATH}"

PULL_SECRET="test-pull-secret"
SECRETS_NS="educates-secrets"
OP_NS="educates"

WORKDIR="$(mktemp -d -t educates-scenario-05-XXXX)"
echo "$WORKDIR" > /tmp/educates-scenario-05.workdir
echo "[pre-install] using workdir: $WORKDIR"

# 1. htpasswd file. registry:2's htpasswd auth backend only accepts
# bcrypt-hashed entries (Apache MD5 / SHA-1 / plain are rejected with
# 401). macOS doesn't ship the `htpasswd` binary, so we run it from a
# throwaway httpd:2 container.
docker run --rm httpd:2 htpasswd -Bbn "$REGISTRY_USER" "$REGISTRY_PASS" \
  > "$WORKDIR/htpasswd"

# 2. Run the auth'd registry on the educates docker network.
docker rm -f "$REGISTRY_NAME" >/dev/null 2>&1 || true
docker network inspect "$DOCKER_NET" >/dev/null 2>&1 || docker network create "$DOCKER_NET" >/dev/null
docker run -d \
  --name "$REGISTRY_NAME" \
  --network "$DOCKER_NET" \
  -p "127.0.0.1:${REGISTRY_HOST_PORT}:${REGISTRY_INNER_PORT}" \
  -v "$WORKDIR/htpasswd:/auth/htpasswd:ro" \
  -e "REGISTRY_AUTH=htpasswd" \
  -e "REGISTRY_AUTH_HTPASSWD_REALM=Educates Test Realm" \
  -e "REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd" \
  registry:2 >/dev/null

# Also attach the registry to every network the kind control-plane
# container is on, so containerd inside the node can resolve us by
# container name through that network's DNS. Mirrors what the v3
# educates CLI does for its own registry (dual-attaches to `educates`
# and `kind`).
KIND_NETWORKS="$(docker inspect "$KIND_CONTROL_PLANE" --format '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{"\n"}}{{end}}' 2>/dev/null | sed '/^$/d')"
if [[ -z "$KIND_NETWORKS" ]]; then
  echo "[pre-install] could not enumerate networks for ${KIND_CONTROL_PLANE}" >&2
  exit 1
fi
while IFS= read -r net; do
  [[ "$net" == "$DOCKER_NET" ]] && continue
  docker network connect "$net" "$REGISTRY_NAME" >/dev/null 2>&1 || true
  echo "[pre-install] attached registry to docker network: $net"
done <<<"$KIND_NETWORKS"

for i in $(seq 1 20); do
  if curl -sSf "http://${REGISTRY_LOGIN_HOST}/v2/" -u "${REGISTRY_USER}:${REGISTRY_PASS}" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
echo "[pre-install] registry up at ${REGISTRY_LOGIN_HOST}"

# 3. Mirror the upstream image. Use a tmp Docker config so we don't
# pollute the user's ~/.docker/config.json.
DOCKER_CONFIG="$WORKDIR/dockercfg"
mkdir -p "$DOCKER_CONFIG"
export DOCKER_CONFIG
echo "$REGISTRY_PASS" | docker --config "$DOCKER_CONFIG" login "$REGISTRY_LOGIN_HOST" -u "$REGISTRY_USER" --password-stdin >/dev/null
docker pull "$UPSTREAM_IMAGE" >/dev/null
docker tag "$UPSTREAM_IMAGE" "$LOGIN_REF"
docker --config "$DOCKER_CONFIG" push "$LOGIN_REF" >/dev/null
echo "[pre-install] mirrored ${UPSTREAM_IMAGE} → ${LOGIN_REF}"

# 4. Configure containerd in the kind control-plane to accept the
# registry over plain HTTP under the *internal* hostname. Modern
# containerd reloads certs.d dynamically.
HOSTS_TOML="[host.\"http://${REGISTRY_INTERNAL_HOST}\"]
  capabilities = [\"pull\", \"resolve\"]
"
docker exec "$KIND_CONTROL_PLANE" mkdir -p "/etc/containerd/certs.d/${REGISTRY_INTERNAL_HOST}"
printf '%s' "$HOSTS_TOML" \
  | docker exec -i "$KIND_CONTROL_PLANE" tee "/etc/containerd/certs.d/${REGISTRY_INTERNAL_HOST}/hosts.toml" >/dev/null
echo "[pre-install] containerd certs.d configured for ${REGISTRY_INTERNAL_HOST}"

# 5. Stage the pull secret in `educates-secrets` only. The operator
# namespace is created by helm install (not pre-creating it avoids
# ordering quirks); post-install.sh creates the in-namespace copy
# *after* helm has applied the chart manifests.
kubectl create namespace "$SECRETS_NS" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$SECRETS_NS" create secret docker-registry "$PULL_SECRET" \
  --docker-server="$REGISTRY_INTERNAL_HOST" \
  --docker-username="$REGISTRY_USER" \
  --docker-password="$REGISTRY_PASS" \
  --docker-email="test@invalid" \
  --dry-run=client -o yaml | kubectl apply -f -
echo "[pre-install] ${PULL_SECRET} staged in ${SECRETS_NS}"
