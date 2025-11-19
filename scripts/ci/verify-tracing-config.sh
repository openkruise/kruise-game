#!/usr/bin/env bash
set -euo pipefail

NS_SYSTEM=${NS_SYSTEM:-kruise-game-system}
OBS_NS=${OBS_NS:-observability}
TEMPO_URL=${TEMPO_URL:-http://localhost:3200}
OTEL_HOST=${OTEL_HOST:-otel-collector.observability.svc.cluster.local}

failures=0

run_check() {
    local desc="$1"
    shift
    set +e
    "$@"
    local rc=$?
    set -e
    if [[ $rc -eq 0 ]]; then
        echo "✅ ${desc}"
    else
        echo "❌ ${desc}"
        failures=$((failures + 1))
    fi
}

check_controller_args() {
    local tmp
    tmp=$(mktemp)
    if ! kubectl get deployment -n "${NS_SYSTEM}" kruise-game-controller-manager -o json >"${tmp}" 2>&1; then
        echo "Failed to fetch controller deployment"
        cat "${tmp}"
        rm -f "${tmp}"
        return 1
    fi
    local args
    args=$(jq -r '.spec.template.spec.containers[0].args[]?' "${tmp}")
    rm -f "${tmp}"

    local missing=0
    if ! grep -q -- "--enable-tracing" <<<"${args}"; then
        echo "Controller args missing --enable-tracing flag:"
        echo "${args}"
        missing=1
    fi
    if ! grep -q -- "--otel-collector-endpoint" <<<"${args}"; then
        echo "Controller args missing --otel-collector-endpoint flag:"
        echo "${args}"
        missing=1
    fi
    return ${missing}
}

check_controller_logs() {
    local tmp
    tmp=$(mktemp)
    if ! kubectl logs -n "${NS_SYSTEM}" -l control-plane=controller-manager --tail=200 >"${tmp}" 2>&1; then
        echo "Failed to read controller logs"
        cat "${tmp}"
        rm -f "${tmp}"
        return 1
    fi
    if ! grep -iqE 'trace|otel|telemetry' "${tmp}"; then
        echo "Tracing keywords not found in recent controller logs. Excerpt:"
        tail -n 40 "${tmp}"
        rm -f "${tmp}"
        return 1
    fi
    rm -f "${tmp}"
    return 0
}

check_controller_connectivity() {
    local controller_pod
    controller_pod=$(kubectl get pods -n "${NS_SYSTEM}" -l control-plane=controller-manager -o jsonpath='{.items[0].metadata.name}')
    if [[ -z "${controller_pod}" ]]; then
        echo "Unable to determine controller pod name"
        return 1
    fi

    local output
    set +e
    output=$(kubectl exec -n "${NS_SYSTEM}" "${controller_pod}" -- nc -zvw5 "${OTEL_HOST}" 4317 2>&1)
    local rc=$?
    set -e
    if [[ $rc -ne 0 ]]; then
        echo "Controller pod ${controller_pod} cannot reach ${OTEL_HOST}:4317"
        echo "${output}"
        return 1
    fi
    return 0
}

check_otel_collector() {
    if kubectl wait --for=condition=Ready pod -l app=otel-collector -n "${OBS_NS}" --timeout=30s >/dev/null 2>&1; then
        return 0
    fi
    echo "OTel Collector pods are not Ready. Current status:"
    kubectl get pods -n "${OBS_NS}" -l app=otel-collector
    kubectl logs -n "${OBS_NS}" -l app=otel-collector --tail=50 || true
    return 1
}

check_tempo_has_traces() {
    local response
    if ! response=$(curl -sf "${TEMPO_URL}/api/search?tags=service.name%3Dokg-controller-manager&limit=1" 2>/tmp/tempo_err); then
        echo "Failed to query Tempo search API:"
        cat /tmp/tempo_err
        rm -f /tmp/tempo_err
        return 1
    fi
    rm -f /tmp/tempo_err

    local count
    count=$(echo "${response}" | jq '.traces | length' 2>/dev/null || echo "0")
    if [[ "${count}" == "0" ]]; then
        echo "Tempo search returned no controller traces. Raw response:"
        echo "${response}"
        return 1
    fi
    return 0
}

run_check "Controller deployment includes tracing flags" check_controller_args
run_check "Controller logs show tracing activity" check_controller_logs
run_check "Controller pods can reach the OTel collector endpoint" check_controller_connectivity
run_check "OTel Collector pods are Ready" check_otel_collector
run_check "Tempo contains controller traces" check_tempo_has_traces

if [[ "${failures}" -ne 0 ]]; then
    exit 1
fi
