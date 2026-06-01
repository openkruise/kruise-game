/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package v1beta1 contains a minimal Go representation of the subset of the
// Config Connector (KCC) compute.cnrm.cloud.google.com/v1beta1 CRD API used by
// the kruise-game GoogleCloud network plugins.
//
// We hand-define these types instead of vendoring github.com/GoogleCloudPlatform/k8s-config-connector
// to avoid pulling its large transitive dependency surface. Only the JSON
// schema needs to match what KCC's installed CRDs expect; field selection here
// covers exactly what the Passthrough and GlobalProxy NLB plugins read/write.
//
// +kubebuilder:object:generate=true
// +groupName=compute.cnrm.cloud.google.com
package v1beta1
