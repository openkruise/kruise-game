#!/usr/bin/env bash

go mod vendor

rm -rf ./pkg/client/{clientset,informers,listers}

/bin/bash ./vendor/k8s.io/code-generator/generate-groups.sh all \
github.com/openkruise/kruise-game/pkg/client  github.com/openkruise/kruise-game "apis:v1alpha1" -h ./hack/boilerplate.go.txt