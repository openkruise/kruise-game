#!/usr/bin/env bash
set -euo pipefail

CERT_MANAGER_VERSION=${CERT_MANAGER_VERSION:?CERT_MANAGER_VERSION must be set}

kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=180s
