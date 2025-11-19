#!/usr/bin/env bash
set -euo pipefail

KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:?KIND_CLUSTER_NAME must be set}

ARTIFACT_ROOT=${E2E_ARTIFACT_ROOT:-/tmp/e2e-artifacts}
mkdir -p "${ARTIFACT_ROOT}"

echo "=== Switching to Kind context ==="
kubectl config use-context "kind-${KIND_CLUSTER_NAME}"

echo "=== Verifying current context ==="
kubectl config current-context

echo "=== Checking cluster status ==="
kubectl cluster-info
kubectl get nodes

echo "=== Building ginkgo ==="
make ginkgo

timeout=${E2E_GINKGO_TIMEOUT:-10m}
suite=${E2E_GINKGO_SUITE:-test/e2e}
read -r -a flag_array <<< "${E2E_GINKGO_FLAGS:--v --trace --progress}"

echo "=== Running tests with verbose output ==="
EXIT_CODE=0
./bin/ginkgo --timeout "${timeout}" "${flag_array[@]}" "${suite}" || EXIT_CODE=$?

is_truthy() {
  case "$(echo "${1:-false}" | tr '[:upper:]' '[:lower:]')" in
    1|t|true|y|yes|on) return 0 ;;
    *) return 1 ;;
  esac
}

if is_truthy "${E2E_LOG_DIAGNOSTICS:-false}"; then
  echo "=== Collecting diagnostics ==="
  kubectl get gss -A || true
  kubectl get pods -n e2e-test || true
  kubectl get events -n e2e-test || true
fi

check_restarts() {
  local max_restarts=$1
  local restart_count
  restart_count=$(kubectl get pod -n kruise-game-system --no-headers | awk '{s+=$4} END {print s+0}')
  if [[ -z "${restart_count}" ]]; then
    restart_count=0
  fi
  if (( restart_count > max_restarts )); then
    echo "Kruise-game restarted unexpectedly (allowed=${max_restarts}, actual=${restart_count})"
    kubectl get pod -n kruise-game-system || true
    kubectl get pod -n kruise-game-system --no-headers | awk '{print $1}' | xargs -r -I {} kubectl logs {} -p -n kruise-game-system --tail=100 || true
    exit 1
  fi
  echo "Kruise-game restart count (${restart_count}) within allowed threshold (${max_restarts})."
}

if [[ -n "${E2E_MAX_RESTARTS:-}" ]]; then
  check_restarts "${E2E_MAX_RESTARTS}"
fi

run_observability_debug() {
  local ns="observability"
  if ! kubectl get ns "${ns}" >/dev/null 2>&1; then
    echo "Observability namespace not found; skipping tracing diagnostics."
    return
  fi

  local trace_dir="/tmp/tracing-test-logs"
  rm -rf "${trace_dir}"
  mkdir -p "${trace_dir}"
  CONTROLLER_PODS=$(kubectl get pods -n kruise-game-system -l control-plane=controller-manager -o jsonpath='{.items[*].metadata.name}')
  for pod in ${CONTROLLER_PODS}; do
    kubectl logs -n kruise-game-system "${pod}" --all-containers=true --timestamps > "${trace_dir}/controller-${pod}.log" 2>&1 || true
  done

  kubectl logs -n kruise-game-system -l control-plane=controller-manager --tail=1000 --timestamps > "${trace_dir}/controller-full.log" 2>&1 || true
  kubectl logs -n kruise-game-system -l app.kubernetes.io/component=webhook --tail=200 > "${trace_dir}/webhook.log" 2>&1 || true
  kubectl get pods -n e2e-test -o wide > "${trace_dir}/e2e-pods.txt" 2>&1 || true
  kubectl get pods -n e2e-test -o yaml > "${trace_dir}/e2e-pods.yaml" 2>&1 || true
  kubectl get mutatingwebhookconfiguration kruise-game-mutating-webhook -o yaml > "${trace_dir}/webhook-config.yaml" 2>&1 || true
  kubectl logs -n "${ns}" -l app=otel-collector --tail=300 > "${trace_dir}/otel-collector.log" 2>&1 || true
  kubectl logs -n "${ns}" -l app=tempo --tail=100 > "${trace_dir}/tempo.log" 2>&1 || true

  echo "Tracing diagnostics saved to ${trace_dir} (see artifacts for details)"
}

if [[ "${EXIT_CODE}" -ne 0 && "$(printf %s "${E2E_OBSERVABILITY_DEBUG:-false}" | tr '[:upper:]' '[:lower:]')" == "true" ]]; then
  run_observability_debug || true
fi

if [[ -f /tmp/tempo-pf.pid ]]; then
  kill "$(cat /tmp/tempo-pf.pid)" 2>/dev/null || true
fi
if [[ -f /tmp/loki-pf.pid ]]; then
  kill "$(cat /tmp/loki-pf.pid)" 2>/dev/null || true
fi

exit "${EXIT_CODE}"
