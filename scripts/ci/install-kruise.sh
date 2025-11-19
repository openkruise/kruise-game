#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${REPO_ROOT}"

helm repo add openkruise https://openkruise.github.io/charts/ || true
helm repo update

kubectl cluster-info
make helm
helm upgrade --install kruise openkruise/kruise --version 1.8.0 --wait --timeout 5m

echo "Waiting for kruise-controller-manager deployment rollout..."
if ! kubectl rollout status deployment/kruise-controller-manager -n kruise-system --timeout=180s; then
  echo "kruise-controller-manager did not become ready in time"
  kubectl get deployment -n kruise-system kruise-controller-manager -o wide || true
  kubectl get pods -n kruise-system -l app=kruise-controller-manager || true
  exit 1
fi

pods=$(kubectl get pods -n kruise-system -l control-plane=controller-manager --no-headers 2>/dev/null || true)
ready_pods=$(awk '$2=="1/1"{count++} END {print count+0}' <<<"${pods}")
ready_pods=${ready_pods:-0}
echo "Kruise controller pods ready: ${ready_pods}"
if [[ -z "${pods}" || $ready_pods -lt 2 ]]; then
  echo "Expected 2 controller-manager pods but found ${ready_pods}"
  kubectl get pods -n kruise-system -l control-plane=controller-manager -o wide || true
  exit 1
fi
