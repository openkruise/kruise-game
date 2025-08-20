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

"${KUSTOMIZE}" build config/default | sed -e 's/imagePullPolicy: Always/imagePullPolicy: IfNotPresent/g' > /tmp/kruise-game-kustomization.yaml

# echo -e "resources:\n- manager.yaml" > config/manager/kustomization.yaml

kubectl apply -f /tmp/kruise-game-kustomization.yaml