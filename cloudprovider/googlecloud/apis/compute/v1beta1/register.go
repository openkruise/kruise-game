/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// GroupName is the Config Connector compute API group.
	GroupName = "compute.cnrm.cloud.google.com"
	// GroupVersion is the API version.
	GroupVersion = "v1beta1"
)

// SchemeGroupVersion is the GroupVersion used to register these objects.
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: GroupVersion}

// Resource maps a kind name to a schema.GroupResource (used by client-go errors).
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

var (
	// SchemeBuilder collects functions that register API types into a scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&ComputeAddress{}, &ComputeAddressList{},
		&ComputeForwardingRule{}, &ComputeForwardingRuleList{},
		&ComputeBackendService{}, &ComputeBackendServiceList{},
		&ComputeHealthCheck{}, &ComputeHealthCheckList{},
		&ComputeTargetTCPProxy{}, &ComputeTargetTCPProxyList{},
		&ComputeFirewall{}, &ComputeFirewallList{},
	)
	// REQUIRED: registers metav1.GetOptions/ListOptions/etc. against this GV so
	// client-go can build typed REST requests for these CRDs. Without this,
	// client.Get returns "v1.GetOptions is not suitable for converting to ...".
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
