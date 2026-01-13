#!/bin/bash
set -e

echo "=== OTel Collector Debug Script ==="

NAMESPACE=${1:-observability}

echo ""
echo "1. Pod Status:"
kubectl get pods -n $NAMESPACE -l app=otel-collector -o wide

echo ""
echo "2. Pod Events:"
kubectl get events -n $NAMESPACE --field-selector involvedObject.kind=Pod --sort-by='.lastTimestamp' | grep otel-collector | tail -20

echo ""
echo "3. Pod Description (last 50 lines):"
kubectl describe pods -n $NAMESPACE -l app=otel-collector | tail -50

echo ""
echo "4. Container Logs (last 100 lines):"
kubectl logs -n $NAMESPACE -l app=otel-collector --tail=100 || echo "Failed to get logs"

echo ""
echo "5. Previous Container Logs (if crashed):"
kubectl logs -n $NAMESPACE -l app=otel-collector --previous --tail=100 2>/dev/null || echo "No previous logs available"

echo ""
echo "6. ConfigMap Validation:"
kubectl get configmap otel-collector-config -n $NAMESPACE -o jsonpath='{.data.config\.yaml}' > /tmp/otel-config-debug.yaml
echo "ConfigMap retrieved, checking YAML syntax..."
python3 -c "import yaml; yaml.safe_load(open('/tmp/otel-config-debug.yaml'))" && echo "✅ YAML syntax OK" || echo "❌ YAML syntax ERROR"

echo ""
echo "7. RBAC Check:"
kubectl get serviceaccount otel-collector -n $NAMESPACE -o yaml
kubectl get clusterrolebinding otel-collector -o yaml | grep -A5 subjects

echo ""
echo "8. Resource Usage:"
kubectl top pods -n $NAMESPACE -l app=otel-collector 2>/dev/null || echo "Metrics not available yet"

echo ""
echo "=== Debug script completed ==="
