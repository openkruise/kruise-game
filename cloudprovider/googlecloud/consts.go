/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

// Constants shared by the GoogleCloud plugins.
const (
	// ProjectIDAnnotation is set on every KCC CR to override the default project.
	ProjectIDAnnotation = "cnrm.cloud.google.com/project-id"

	// ResourceTagKey labels resources managed by this operator so they can be
	// listed for cleanup and informer filtering.
	//
	// GCP label values MUST NOT contain dots (KCC propagates K8s labels into
	// GCP labels), so we use the dash form here while keeping the
	// game.kruise.io semantics in the key namespace.
	ResourceTagKey   = "managed-by"
	ResourceTagValue = "game-kruise-io"

	// SvcSelectorKey is the Pod label written by Kruise advanced StatefulSet
	// for the per-pod identity. Services use it as their selector.
	SvcSelectorKey = "statefulset.kubernetes.io/pod-name"

	// ConfigHashKey is annotated on every K8s resource we author so we can
	// detect drift between the live object and the desired spec hash.
	ConfigHashKey = "game.kruise.io/network-config-hash"

	// RetainFinalizer guards a KCC CR from cascade deletion when the user has
	// asked us to retain the underlying cloud resources past Pod deletion.
	RetainFinalizer = "game.kruise.io/google-retain"

	// PodFinalizer is added to Pods when RetainOnDelete=false so we can run
	// cleanup of KCC CRs before the Pod object is removed.
	PodFinalizer = "game.kruise.io/google-delete-on-pod-deleted"

	// GssDeletingLabelKey is set on a Pod by OnPodUpdated when its owning GSS
	// has started deletion; OnPodDeleted reads it to decide whether to cascade
	// delete shared resources (mirrors the alibabacloud/auto_nlbs_v2 pattern).
	GssDeletingLabelKey = "game.kruise.io/gss-deleting"

	// NEGAnnotationKey is the GKE Service annotation that triggers standalone
	// NEG creation. Value is JSON like {"exposed_ports":{"7777":{"name":"..."}}}.
	NEGAnnotationKey = "cloud.google.com/neg"

	// NEGStatusAnnotationKey is written back by the GKE NEG controller, mapping
	// service port to per-zone NEG names.
	NEGStatusAnnotationKey = "cloud.google.com/neg-status"

	// L4RBSAnnotationKey enables the Regional Backend Service backed Passthrough
	// NLB for a Service. Requires GKE >= 1.32.2-gke.1652000.
	L4RBSAnnotationKey = "cloud.google.com/l4-rbs"

	// GKELoadBalancerIPAnnotationKey tells the GKE LB controller to adopt a
	// pre-reserved IP address by name (the ComputeAddress.spec.resourceID).
	GKELoadBalancerIPAnnotationKey = "networking.gke.io/load-balancer-ip-addresses"

	// GKELoadBalancerTypeAnnotationKey switches the Service into an internal
	// passthrough LB when set to "Internal".
	GKELoadBalancerTypeAnnotationKey = "networking.gke.io/load-balancer-type"

	// NetworkTierAnnotationKey sets PREMIUM or STANDARD on a Service.
	NetworkTierAnnotationKey = "cloud.google.com/network-tier"
)

// Configuration parameter names used in NetworkConfParams entries. Plugins
// share these so users can mix-and-match on the same GameServerSet template.
const (
	// Shared.
	ConfRetainOnDelete = "RetainOnDelete"
	ConfProjectID      = "ProjectId"
	ConfNetwork        = "Network"
	ConfSubnetwork     = "Subnetwork"
	ConfAnnotations    = "Annotations"
	ConfAllowNotReady  = "AllowNotReadyContainers"

	// Passthrough-only.
	ConfRegion            = "Region"
	ConfScheme            = "Scheme" // External | Internal
	ConfAllowGlobalAccess = "AllowGlobalAccess"
	ConfNetworkTier       = "NetworkTier" // PREMIUM | STANDARD
	ConfPortProtocols     = "PortProtocols"

	// Proxy-only.
	ConfPort                      = "Port"        // single 1-65535
	ConfProxyHeader               = "ProxyHeader" // NONE | PROXY_V1
	ConfHealthCheckIntervalSec    = "HealthCheckIntervalSec"
	ConfHealthCheckTimeoutSec     = "HealthCheckTimeoutSec"
	ConfHealthyThreshold          = "HealthyThreshold"
	ConfUnhealthyThreshold        = "UnhealthyThreshold"
	ConfBalancingMode             = "BalancingMode"
	ConfMaxConnectionsPerEndpoint = "MaxConnectionsPerEndpoint"
)

// Scheme values for ConfScheme.
const (
	SchemeExternal = "External"
	SchemeInternal = "Internal"
)

// Proxy header values for TargetTcpProxy.
const (
	ProxyHeaderNone = "NONE"
	ProxyHeaderV1   = "PROXY_V1"
)

// Default health check probe ranges that must be allowed to reach pod ports.
var DefaultHealthCheckSourceRanges = []string{
	"35.191.0.0/16",
	"130.211.0.0/22",
}
