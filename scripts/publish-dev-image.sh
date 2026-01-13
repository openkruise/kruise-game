#!/usr/bin/env bash

# Build and push a dev image for KruiseGame to a container registry.
# Defaults to GHCR under ballista01, tagging both an immutable version and a moving dev tag.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

command -v docker >/dev/null || { echo "docker not found" >&2; exit 1; }
command -v make >/dev/null || { echo "make not found" >&2; exit 1; }

REGISTRY="${REGISTRY:-ghcr.io}"
IMAGE_REPO="${IMAGE_REPO:-ballista01/kruise-game-manager}"
IMAGE="${REGISTRY}/${IMAGE_REPO}"

# Immutable tag derived from git metadata; can be overridden via VERSION env.
VERSION="${VERSION:-$(./hack/compute-version.sh)}"

# Moving tag that always points to the latest dev build; set empty to skip.
DEV_TAG="${DEV_TAG:-dev}"

echo "Building image..."
make docker-build IMG="${IMAGE}:${VERSION}"

echo "Pushing image tag: ${IMAGE}:${VERSION}"
docker push "${IMAGE}:${VERSION}"

if [[ -n "${DEV_TAG}" ]]; then
  echo "Tagging moving tag: ${IMAGE}:${DEV_TAG}"
  docker tag "${IMAGE}:${VERSION}" "${IMAGE}:${DEV_TAG}"
  echo "Pushing image tag: ${IMAGE}:${DEV_TAG}"
  docker push "${IMAGE}:${DEV_TAG}"
fi

# Surface digest for pinning in Helm.
DIGEST="$(docker image inspect --format '{{index .RepoDigests 0}}' "${IMAGE}:${VERSION}" | sed -n 's/^[^@]*@//p')"
echo "Done. Immutable tag: ${IMAGE}:${VERSION}"
[[ -n "${DEV_TAG}" ]] && echo "Moving tag   : ${IMAGE}:${DEV_TAG}"
[[ -n "${DIGEST}" ]] && echo "Digest       : ${DIGEST}"
