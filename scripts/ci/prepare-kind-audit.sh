#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${REPO_ROOT}"

mkdir -p /tmp/kind-audit
cp test/audit/policy.yaml /tmp/kind-audit/policy.yaml
echo "Prepared audit policy at /tmp/kind-audit/policy.yaml"
