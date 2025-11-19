#!/bin/bash
# Setup Observability Stack in Kubernetes for E2E Tests
# Usage: ./setup-k8s-observability.sh [deploy|cleanup|status]

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFESTS_DIR="${SCRIPT_DIR}/manifests"
OBS_NS="observability"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

apply_quiet() {
    local description="$1"
    shift
    log_info "${description}..."
    if ! output=$("$@" 2>&1); then
        log_error "${description} failed:"
        echo "${output}"
        exit 1
    fi
}

wait_component_ready() {
    local component="$1"
    local selector="$2"
    if kubectl wait --for=condition=Ready pod -l "${selector}" -n "${OBS_NS}" --timeout=120s >/dev/null 2>&1; then
        log_info "✓ ${component} Ready"
        return 0
    fi

    log_error "${component} did not become Ready in time"
    kubectl get pods -n "${OBS_NS}" -l "${selector}"
    kubectl logs -n "${OBS_NS}" -l "${selector}" --tail=50 || true
    exit 1
}

# Deploy observability stack
deploy_stack() {
    log_info "Deploying observability stack to Kubernetes..."
    
    local otel_manifest="01-otel-collector.yaml"
    if [[ "${GRAFANA_CLOUD_ENABLED:-false}" == "true" ]]; then
        otel_manifest="01-otel-collector-grafana.yaml"
        log_info "Grafana Cloud dual-write is enabled (using ${otel_manifest})"
    fi
    
    # Apply manifests in order
    apply_quiet "Creating observability namespace" kubectl apply -f "${MANIFESTS_DIR}/00-namespace.yaml"
    
    if [[ "${GRAFANA_CLOUD_ENABLED:-false}" == "true" ]]; then
        if ! kubectl get secret grafana-cloud-credentials -n observability >/dev/null 2>&1; then
            log_warn "Grafana Cloud secret 'grafana-cloud-credentials' not found in observability namespace"
            log_warn "Collector deployment will likely fail until the secret is created"
        fi
    fi
    
    apply_quiet "Deploying OTel Collector" kubectl apply -f "${MANIFESTS_DIR}/${otel_manifest}"
    apply_quiet "Deploying Tempo" kubectl apply -f "${MANIFESTS_DIR}/02-tempo.yaml"
    apply_quiet "Deploying Loki" kubectl apply -f "${MANIFESTS_DIR}/03-loki.yaml"
    apply_quiet "Deploying Prometheus" kubectl apply -f "${MANIFESTS_DIR}/04-prometheus.yaml"
    
    log_info "Waiting for observability pods to be Ready..."
    wait_for_pods
    
    log_info ""
    log_info "═══════════════════════════════════════════════════════════"
    log_info "Observability Stack Deployed Successfully!"
    log_info "═══════════════════════════════════════════════════════════"
    log_info "OTel Collector:    otel-collector.observability.svc.cluster.local:4317"
    log_info "Tempo API:         tempo.observability.svc.cluster.local:3200"
    log_info "Loki API:          loki.observability.svc.cluster.local:3100"
    log_info "Prometheus:        prometheus-server.observability.svc.cluster.local:80"
    log_info "═══════════════════════════════════════════════════════════"
}

# Wait for all pods to be ready
wait_for_pods() {
    wait_component_ready "OTel Collector" "app=otel-collector"
    wait_component_ready "Tempo" "app=tempo"
    wait_component_ready "Loki" "app=loki"
    wait_component_ready "Prometheus" "app=prometheus"
}

# Check status of all components
check_status() {
    log_info "Checking observability stack status..."
    echo ""
    
    log_info "Pods in observability namespace:"
    kubectl get pods -n observability
    echo ""
    
    log_info "Services in observability namespace:"
    kubectl get svc -n observability
    echo ""
    
    # Check if pods are ready
    log_info "Health Checks:"
    
    # OTel Collector
    if kubectl get pods -n observability -l app=otel-collector | grep -q "Running"; then
        echo -e "  OTel Collector: ${GREEN}✓${NC} Running"
    else
        echo -e "  OTel Collector: ${RED}✗${NC} Not Running"
    fi
    
    # Tempo
    if kubectl get pods -n observability -l app=tempo | grep -q "Running"; then
        echo -e "  Tempo:          ${GREEN}✓${NC} Running"
    else
        echo -e "  Tempo:          ${RED}✗${NC} Not Running"
    fi
    
    # Loki
    if kubectl get pods -n observability -l app=loki | grep -q "Running"; then
        echo -e "  Loki:           ${GREEN}✓${NC} Running"
    else
        echo -e "  Loki:           ${RED}✗${NC} Not Running"
    fi
    
    # Prometheus
    if kubectl get pods -n observability -l app=prometheus | grep -q "Running"; then
        echo -e "  Prometheus:     ${GREEN}✓${NC} Running"
    else
        echo -e "  Prometheus:     ${RED}✗${NC} Not Running"
    fi
}

# Cleanup observability stack
cleanup_stack() {
    log_info "Cleaning up observability stack..."
    
    kubectl delete -f "${MANIFESTS_DIR}/04-prometheus.yaml" --ignore-not-found=true
    kubectl delete -f "${MANIFESTS_DIR}/03-loki.yaml" --ignore-not-found=true
    kubectl delete -f "${MANIFESTS_DIR}/02-tempo.yaml" --ignore-not-found=true
    kubectl delete -f "${MANIFESTS_DIR}/01-otel-collector.yaml" --ignore-not-found=true
    kubectl delete -f "${MANIFESTS_DIR}/00-namespace.yaml" --ignore-not-found=true
    
    log_info "✓ Cleanup complete"
}

# Port forward for local access (optional, for debugging)
port_forward() {
    log_info "Setting up port forwards..."
    log_info "Tempo:      kubectl port-forward -n observability svc/tempo 3200:3200"
    log_info "Prometheus: kubectl port-forward -n observability svc/prometheus-server 9090:80"
    log_info "Loki:       kubectl port-forward -n observability svc/loki 3100:3100"
}

# Show logs
show_logs() {
    local component="${1:-}"
    if [ -z "$component" ]; then
        log_error "Usage: $0 logs <component>"
        log_info "Available components: otel-collector, tempo, loki, prometheus"
        exit 1
    fi
    
    case "$component" in
        otel-collector)
            kubectl logs -n observability -l app=otel-collector --tail=100 -f
            ;;
        tempo)
            kubectl logs -n observability -l app=tempo --tail=100 -f
            ;;
        loki)
            kubectl logs -n observability -l app=loki --tail=100 -f
            ;;
        prometheus)
            kubectl logs -n observability -l app=prometheus --tail=100 -f
            ;;
        *)
            log_error "Unknown component: $component"
            exit 1
            ;;
    esac
}

# Main
case "${1:-}" in
    deploy)
        deploy_stack
        ;;
    cleanup)
        cleanup_stack
        ;;
    status)
        check_status
        ;;
    port-forward)
        port_forward
        ;;
    logs)
        shift
        show_logs "$@"
        ;;
    *)
        echo "Usage: $0 {deploy|cleanup|status|port-forward|logs}"
        echo ""
        echo "Commands:"
        echo "  deploy        Deploy observability stack to Kubernetes"
        echo "  cleanup       Remove observability stack from Kubernetes"
        echo "  status        Check status of all components"
        echo "  port-forward  Show port-forward commands for local access"
        echo "  logs <comp>   Show logs for a component (otel-collector, tempo, loki, prometheus)"
        echo ""
        echo "Examples:"
        echo "  $0 deploy"
        echo "  $0 status"
        echo "  $0 logs otel-collector"
        echo "  $0 cleanup"
        exit 1
        ;;
esac
