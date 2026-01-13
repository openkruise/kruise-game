#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${REPO_ROOT}"

IMAGE="openkruise/kruise-game-manager:e2e-${GITHUB_RUN_ID:-dev}"
KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:?KIND_CLUSTER_NAME must be set}

echo "Building ${IMAGE} with LDFLAGS='${LDFLAGS:-}'"
docker build --pull --no-cache --build-arg LDFLAGS="${LDFLAGS:-}" . -t "${IMAGE}"
kind load docker-image --name="${KIND_CLUSTER_NAME}" "${IMAGE}" || {
  echo >&2 "Failed to load ${IMAGE} into kind cluster ${KIND_CLUSTER_NAME}"
  exit 1
}

if [[ -z "${GITHUB_ENV:-}" ]]; then
  echo "GITHUB_ENV is not set; unable to persist E2E image reference" >&2
  exit 1
fi

echo "E2E_IMAGE=${IMAGE}" >> "${GITHUB_ENV}"
echo "E2E image loaded into kind and exported as \$E2E_IMAGE"
