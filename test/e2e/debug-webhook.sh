#!/bin/bash

# Debug script for webhook issues
# Usage: ./debug-webhook.sh

set -e

NAMESPACE="${1:-kruise-game-system}"

echo "========================================="
echo "=== Kruise Game Webhook Diagnostics ==="
echo "========================================="
echo ""

echo "1. Checking MutatingWebhookConfiguration..."
echo "-------------------------------------------"
if kubectl get mutatingwebhookconfiguration kruise-game-mutating-webhook &>/dev/null; then
  echo "✅ MutatingWebhookConfiguration exists"
  echo ""
  echo "Webhook details:"
  kubectl get mutatingwebhookconfiguration kruise-game-mutating-webhook -o yaml | grep -A10 "webhooks:"
  echo ""
  echo "Webhook client config:"
  kubectl get mutatingwebhookconfiguration kruise-game-mutating-webhook -o jsonpath='{.webhooks[0].clientConfig}' | jq .
  echo ""
else
  echo "❌ MutatingWebhookConfiguration NOT found"
  echo "Available MutatingWebhookConfigurations:"
  kubectl get mutatingwebhookconfiguration
fi
echo ""

echo "2. Checking Webhook Service..."
echo "-------------------------------------------"
if kubectl get svc -n "$NAMESPACE" kruise-game-webhook-service &>/dev/null; then
  echo "✅ Webhook service exists"
  kubectl get svc -n "$NAMESPACE" kruise-game-webhook-service
  echo ""
  echo "Service endpoints:"
  kubectl get endpoints -n "$NAMESPACE" kruise-game-webhook-service
else
  echo "❌ Webhook service NOT found"
  echo "Available services in $NAMESPACE:"
  kubectl get svc -n "$NAMESPACE"
fi
echo ""

echo "3. Checking Controller Manager Pods..."
echo "-------------------------------------------"
kubectl get pods -n "$NAMESPACE" -l control-plane=controller-manager
echo ""

echo "Controller pod details:"
for pod in $(kubectl get pods -n "$NAMESPACE" -l control-plane=controller-manager -o jsonpath='{.items[*].metadata.name}'); do
  echo "Pod: $pod"
  echo "  Ready: $(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')"
  echo "  Containers: $(kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.spec.containers[*].name}')"
  echo ""
done
echo ""

echo "4. Checking Webhook Arguments in Controller..."
echo "-------------------------------------------"
echo "Checking if webhook is enabled in controller args..."
for pod in $(kubectl get pods -n "$NAMESPACE" -l control-plane=controller-manager -o jsonpath='{.items[*].metadata.name}'); do
  echo "Pod: $pod"
  echo "Args:"
  kubectl get pod -n "$NAMESPACE" "$pod" -o jsonpath='{.spec.containers[?(@.name=="manager")].args}' | jq -r '.[]'
  echo ""
done
echo ""

echo "5. Checking Certificate Secret..."
echo "-------------------------------------------"
if kubectl get secret -n "$NAMESPACE" kruise-game-webhook-server-cert &>/dev/null; then
  echo "✅ Webhook certificate secret exists"
  kubectl get secret -n "$NAMESPACE" kruise-game-webhook-server-cert -o jsonpath='{.data}' | jq 'keys'
else
  echo "❌ Webhook certificate secret NOT found"
  echo "Available secrets in $NAMESPACE:"
  kubectl get secrets -n "$NAMESPACE"
fi
echo ""

echo "6. Checking Recent Controller Logs for Webhook Errors..."
echo "-------------------------------------------"
echo "Searching for webhook-related errors in last 100 lines..."
for pod in $(kubectl get pods -n "$NAMESPACE" -l control-plane=controller-manager -o jsonpath='{.items[*].metadata.name}' | head -n1); do
  echo "Pod: $pod"
  kubectl logs -n "$NAMESPACE" "$pod" --tail=100 2>&1 | grep -iE "webhook|certificate|tls|mutating" | head -20 || echo "No webhook-related logs found"
done
echo ""

echo "7. Checking API Server Connection to Webhook..."
echo "-------------------------------------------"
echo "Testing if webhook service is accessible from within cluster..."
if kubectl run -n "$NAMESPACE" --rm -it --restart=Never webhook-test --image=curlimages/curl:latest --command -- \
  curl -k -v https://kruise-game-webhook-service.kruise-game-system.svc:443/mutate-v1-pod 2>&1 | grep -E "Connected|SSL|TLS" | head -10; then
  echo "✅ Webhook service is accessible"
else
  echo "⚠️  Could not test webhook service connectivity"
fi
echo ""

echo "8. Test Pod Creation (Dry Run)..."
echo "-------------------------------------------"
echo "Creating a test pod to see if webhook is triggered..."
cat <<EOF | kubectl apply --dry-run=server -f - 2>&1 | head -20
apiVersion: v1
kind: Pod
metadata:
  name: webhook-test-pod
  namespace: default
  labels:
    game.kruise.io/owner-gss: test-gss
spec:
  containers:
  - name: test
    image: nginx:latest
EOF
echo ""

echo "========================================="
echo "=== Webhook Diagnostics Complete ==="
echo "========================================="
