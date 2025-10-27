#!/usr/bin/env bash

# Outputs a semver-like identifier for the current git state.
# Prefer annotated tags (vX.Y.Z). Fall back to dev-<sha>[-dirty].

set -euo pipefail

describe="$(git describe --tags --dirty --always 2>/dev/null || true)"

if [[ -z "${describe}" ]]; then
  describe="unknown"
fi

if [[ "${describe}" =~ ^v[0-9] ]]; then
  echo "${describe}"
else
  echo "dev-${describe}"
fi
