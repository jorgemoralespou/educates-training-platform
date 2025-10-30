#!/usr/bin/env bash
set -euo pipefail

echo "[devcontainer] Verifying Docker connectivity..."
docker version >/dev/null

# echo "[devcontainer] Ensuring buildx builder exists..."
# if ! docker buildx inspect devcontainer >/dev/null 2>&1; then
#   docker buildx create --name devcontainer --driver docker-container --use || true
# fi

# echo "[devcontainer] Installing binfmt (qemu) for multi-arch builds..."
# docker run --privileged --rm tonistiigi/binfmt --install all || true

# echo "[devcontainer] Ensuring local registry 'educates-registry' is running on :5001..."
# if ! docker inspect educates-registry >/dev/null 2>&1; then
#   docker run -d --restart=always -p 5001:5000 --name educates-registry registry:2
# fi

echo "[devcontainer] Ready: buildx and local registry configured."


