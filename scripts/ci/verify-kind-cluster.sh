#!/usr/bin/env bash
set -euo pipefail

KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:?KIND_CLUSTER_NAME must be set}

echo "=== Verifying Kind cluster (${KIND_CLUSTER_NAME}) ==="
kind get clusters
kubectl config use-context "kind-${KIND_CLUSTER_NAME}"
kubectl cluster-info --request-timeout=15s
kubectl get nodes -o wide
