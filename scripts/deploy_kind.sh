#!/usr/bin/env bash

if [ -z "$IMG" ]; then
  echo "no found IMG env"
  exit 1
fi

set -e

make kustomize

KUSTOMIZE=$(pwd)/bin/kustomize

pushd config/manager

"${KUSTOMIZE}" edit set image controller="${IMG}"

# if $ENABLE_HA is set, we will set replicas to 3
if [ -n "$ENABLE_HA" ]; then
  echo "enable HA mode controller-manager"
  "${KUSTOMIZE}" edit set replicas controller-manager=3
  # enable leader election
  "${KUSTOMIZE}" edit add patch --kind Deployment --name controller-manager --path patches/add-leader-elect-patch.yaml
fi

popd

# Choose which kustomization to use based on ENABLE_TRACING
if [ -n "$ENABLE_TRACING" ]; then
  echo "Enabling tracing with OTel Collector"
  OTEL_ENDPOINT="${OTEL_COLLECTOR_ENDPOINT:-otel-collector.observability.svc.cluster.local:4317}"
  OTEL_SAMPLING_RATE="${OTEL_SAMPLING_RATE:-1.0}"
  
  echo "Using tracing overlay for kustomization..."
  echo "  - Endpoint: $OTEL_ENDPOINT"
  echo "  - Sampling rate: $OTEL_SAMPLING_RATE"
  
  # Create temporary kustomization overlay in project directory (kustomize requires relative paths)
  OVERLAY_DIR="config/.tracing-overlay-temp"
  rm -rf "$OVERLAY_DIR"
  mkdir -p "$OVERLAY_DIR"
  
  cat > "$OVERLAY_DIR/kustomization.yaml" <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

bases:
- ../default

patchesJson6902:
- target:
    group: apps
    version: v1
    kind: Deployment
    name: controller-manager
    namespace: kruise-game-system
  patch: |-
    - op: add
      path: /spec/template/spec/containers/0/args/-
      value: --enable-tracing=true
    - op: add
      path: /spec/template/spec/containers/0/args/-
      value: --otel-collector-endpoint=${OTEL_ENDPOINT}
    - op: add
      path: /spec/template/spec/containers/0/args/-
      value: --otel-sampling-rate=${OTEL_SAMPLING_RATE}
    - op: add
      path: /spec/template/spec/containers/0/args/-
      value: --log-json-preset=otel
EOF
  
  "${KUSTOMIZE}" build "$OVERLAY_DIR" | sed -e 's/imagePullPolicy: Always/imagePullPolicy: IfNotPresent/g' > /tmp/kruise-game-kustomization.yaml
  
  # Clean up temporary overlay
  rm -rf "$OVERLAY_DIR"
else
  echo "Using default kustomization (tracing disabled)"
  "${KUSTOMIZE}" build config/default | sed -e 's/imagePullPolicy: Always/imagePullPolicy: IfNotPresent/g' > /tmp/kruise-game-kustomization.yaml
fi

kubectl apply -f /tmp/kruise-game-kustomization.yaml
