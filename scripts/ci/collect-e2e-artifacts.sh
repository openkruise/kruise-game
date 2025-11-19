#!/usr/bin/env bash
set -euo pipefail

ARTIFACT_DIR="${E2E_ARTIFACT_ROOT:-/tmp/e2e-artifacts}"
mkdir -p "${ARTIFACT_DIR}/infrastructure"

copy_dir_if_exists() {
  local src=$1
  local dest=$2
  if [ -d "${src}" ]; then
    echo "Copying $(basename "${src}") into artifacts..."
    rm -rf "${dest}"
    mkdir -p "$(dirname "${dest}")"
    cp -r "${src}" "${dest}"
  fi
}

# Collect audit logs
if [ -d /tmp/kind-audit ]; then
  sudo chmod -R a+r /tmp/kind-audit || true
  copy_dir_if_exists /tmp/kind-audit "${ARTIFACT_DIR}/infrastructure/audit-logs"
fi

# Collect legacy tracing logs (if any)
copy_dir_if_exists /tmp/tracing-test-logs "${ARTIFACT_DIR}/infrastructure/legacy-tracing-logs"

# Collect observability logs when the namespace exists
if kubectl get ns observability >/dev/null 2>&1; then
  echo "Collecting observability infrastructure logs..."
  OBS_DIR="${ARTIFACT_DIR}/infrastructure/observability-logs"
  mkdir -p "${OBS_DIR}"

  kubectl logs -n observability -l app=otel-collector --tail=1000 \
    > "${OBS_DIR}/otel-collector.log" 2>&1 || true
  kubectl logs -n observability -l app=tempo --tail=1000 \
    > "${OBS_DIR}/tempo.log" 2>&1 || true
  kubectl logs -n observability -l app=loki --tail=1000 \
    > "${OBS_DIR}/loki.log" 2>&1 || true
  kubectl logs -n observability -l app=prometheus --tail=1000 \
    > "${OBS_DIR}/prometheus.log" 2>&1 || true
  kubectl get pods -n observability -o yaml \
    > "${OBS_DIR}/pods.yaml" 2>&1 || true
else
  echo "Observability namespace missing; skipping observability log collection."
fi

# Create summary of collected artifacts
SUMMARY="${ARTIFACT_DIR}/ARTIFACT_SUMMARY.txt"
{
  echo "=== E2E Test Artifacts Summary ==="
  echo "Collection Date: $(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  echo "Workflow Run: ${GITHUB_RUN_ID:-unknown}"
  echo "Kubernetes Version: ${KUBECTL_VERSION:-unknown}"
  echo ""
  echo "Directory Structure:"
} > "${SUMMARY}"
find "${ARTIFACT_DIR}" -type f -o -type d | sort >> "${SUMMARY}" || true

echo "Artifact collection complete. Artifacts staged at ${ARTIFACT_DIR}"
