#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${REPO_ROOT}"

VERSION=$(./hack/compute-version.sh)
echo "Computed version: ${VERSION}"

if [[ -z "${GITHUB_ENV:-}" ]]; then
  echo "GITHUB_ENV is not set; are you running inside GitHub Actions?" >&2
  exit 1
fi

{
  echo "VERSION=${VERSION}"
  echo "LDFLAGS=-X github.com/openkruise/kruise-game/pkg/version.Version=${VERSION}"
} >> "${GITHUB_ENV}"
