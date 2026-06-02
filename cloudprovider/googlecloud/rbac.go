/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package-level RBAC markers for the Google Cloud network plugins. Picked up
// by controller-gen when `make manifests` regenerates config/rbac/role.yaml so
// the contents of role.yaml stay in sync with what the plugins actually call.

// +kubebuilder:rbac:groups=compute.cnrm.cloud.google.com,resources=computeaddresses;computeforwardingrules;computebackendservices;computehealthchecks;computetargettcpproxies;computefirewalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=compute.cnrm.cloud.google.com,resources=computeaddresses/status;computeforwardingrules/status;computebackendservices/status;computehealthchecks/status;computetargettcpproxies/status;computefirewalls/status,verbs=get

package googlecloud
