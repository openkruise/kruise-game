#!/bin/bash
# Progressive OTel Collector configuration test
# Tests configurations from minimal to full-featured

set -e

NAMESPACE="observability"
MANIFEST_DIR="test/e2e/manifests"

echo "=== Progressive OTel Collector Test ==="

# Function to wait for pod
wait_for_otel() {
    local timeout=$1
    echo "Waiting for OTel Collector to be ready (timeout: ${timeout}s)..."
    if kubectl wait --for=condition=ready pod -l app=otel-collector -n $NAMESPACE --timeout=${timeout}s; then
        echo "✅ OTel Collector is ready"
        return 0
    else
        echo "❌ OTel Collector failed to become ready"
        return 1
    fi
}

# Function to show logs
show_logs() {
    echo ""
    echo "=== Pod Status ==="
    kubectl get pods -n $NAMESPACE -l app=otel-collector -o wide
    
    echo ""
    echo "=== Last 50 lines of logs ==="
    kubectl logs -n $NAMESPACE -l app=otel-collector --tail=50 || true
    
    echo ""
    echo "=== Previous logs (if crashed) ==="
    kubectl logs -n $NAMESPACE -l app=otel-collector --previous --tail=50 2>/dev/null || echo "No previous logs"
}

# Test 1: Deploy with current configuration
echo ""
echo "Test 1: Deploying with current configuration..."
kubectl delete -f $MANIFEST_DIR/01-otel-collector.yaml --ignore-not-found=true
sleep 5
kubectl apply -f $MANIFEST_DIR/01-otel-collector.yaml

if wait_for_otel 120; then
    echo "✅ SUCCESS: OTel Collector started with full configuration"
    kubectl logs -n $NAMESPACE -l app=otel-collector --tail=20
    exit 0
else
    echo "❌ FAILED: OTel Collector failed with full configuration"
    show_logs
    exit 1
fi
