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
)

// ResourceRef references another KCC resource by name (and optional namespace),
// or directly by external selfLink. Matches the shape of
// github.com/GoogleCloudPlatform/k8s-config-connector apis/k8s/v1alpha1.ResourceRef.
type ResourceRef struct {
	// External is a selfLink (e.g. projects/foo/zones/us-central1-a/networkEndpointGroups/bar).
	External string `json:"external,omitempty"`
	// Kind is the kind of the referenced object.
	Kind string `json:"kind,omitempty"`
	// Name is the metadata.name of the referenced object.
	Name string `json:"name,omitempty"`
	// Namespace is the metadata.namespace of the referenced object. Defaults to
	// the same namespace as the referrer.
	Namespace string `json:"namespace,omitempty"`
}

// Condition is the standard KCC condition shape mirrored from apis/k8s/v1alpha1.
type Condition struct {
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	Message            string      `json:"message,omitempty"`
	Reason             string      `json:"reason,omitempty"`
	Status             string      `json:"status,omitempty"`
	Type               string      `json:"type,omitempty"`
}

// -----------------------------------------------------------------------------
// ComputeAddress
// -----------------------------------------------------------------------------

// ComputeAddress represents a Compute Engine reserved IP address.
type ComputeAddress struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ComputeAddressSpec   `json:"spec,omitempty"`
	Status ComputeAddressStatus `json:"status,omitempty"`
}

// ComputeAddressList is a list of ComputeAddress.
type ComputeAddressList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComputeAddress `json:"items"`
}

type ComputeAddressSpec struct {
	// Location is "global" or a region name (e.g. "us-central1").
	Location string `json:"location"`
	// ResourceID is the immutable GCP-side name; defaults to metadata.name when unset.
	ResourceID *string `json:"resourceID,omitempty"`
	// Address optionally pins the IP value; let GCP allocate when unset.
	Address *string `json:"address,omitempty"`
	// AddressType is EXTERNAL or INTERNAL. Defaults to EXTERNAL.
	AddressType *string `json:"addressType,omitempty"`
	// IpVersion is IPV4 or IPV6.
	IPVersion *string `json:"ipVersion,omitempty"`
	// NetworkTier is PREMIUM or STANDARD; PREMIUM is required for global addresses.
	NetworkTier *string `json:"networkTier,omitempty"`
	// Purpose for INTERNAL addresses (e.g. GCE_ENDPOINT, SHARED_LOADBALANCER_VIP).
	Purpose *string `json:"purpose,omitempty"`
	// NetworkRef references the VPC network for internal addresses.
	NetworkRef *ResourceRef `json:"networkRef,omitempty"`
	// SubnetworkRef references the subnetwork for internal addresses.
	SubnetworkRef *ResourceRef `json:"subnetworkRef,omitempty"`
	// Description is an optional human-readable description.
	Description *string `json:"description,omitempty"`
	// PrefixLength only applies to internal IP ranges (not single IPs).
	PrefixLength *int64 `json:"prefixLength,omitempty"`
}

type ComputeAddressStatus struct {
	Conditions         []Condition                  `json:"conditions,omitempty"`
	CreationTimestamp  *string                      `json:"creationTimestamp,omitempty"`
	LabelFingerprint   *string                      `json:"labelFingerprint,omitempty"`
	ObservedGeneration *int64                       `json:"observedGeneration,omitempty"`
	ObservedState      *ComputeAddressObservedState `json:"observedState,omitempty"`
	SelfLink           *string                      `json:"selfLink,omitempty"`
	Users              []string                     `json:"users,omitempty"`
}

type ComputeAddressObservedState struct {
	Address *string `json:"address,omitempty"`
}

// -----------------------------------------------------------------------------
// ComputeForwardingRule
// -----------------------------------------------------------------------------

// ComputeForwardingRule represents a regional or global compute forwarding rule.
type ComputeForwardingRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ComputeForwardingRuleSpec   `json:"spec,omitempty"`
	Status ComputeForwardingRuleStatus `json:"status,omitempty"`
}

// ComputeForwardingRuleList is a list of ComputeForwardingRule.
type ComputeForwardingRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComputeForwardingRule `json:"items"`
}

type ComputeForwardingRuleSpec struct {
	Location            string                   `json:"location"`
	ResourceID          *string                  `json:"resourceID,omitempty"`
	Description         *string                  `json:"description,omitempty"`
	IPProtocol          *string                  `json:"ipProtocol,omitempty"`
	IPVersion           *string                  `json:"ipVersion,omitempty"`
	LoadBalancingScheme *string                  `json:"loadBalancingScheme,omitempty"`
	NetworkTier         *string                  `json:"networkTier,omitempty"`
	PortRange           *string                  `json:"portRange,omitempty"`
	Ports               []string                 `json:"ports,omitempty"`
	AllPorts            *bool                    `json:"allPorts,omitempty"`
	AllowGlobalAccess   *bool                    `json:"allowGlobalAccess,omitempty"`
	Target              *ForwardingRuleTarget    `json:"target,omitempty"`
	BackendServiceRef   *ResourceRef             `json:"backendServiceRef,omitempty"`
	IPAddress           *ForwardingRuleIPAddress `json:"ipAddress,omitempty"`
	NetworkRef          *ResourceRef             `json:"networkRef,omitempty"`
	SubnetworkRef       *ResourceRef             `json:"subnetworkRef,omitempty"`
	ServiceLabel        *string                  `json:"serviceLabel,omitempty"`
}

type ForwardingRuleTarget struct {
	TargetTCPProxyRef    *ResourceRef `json:"targetTCPProxyRef,omitempty"`
	TargetSSLProxyRef    *ResourceRef `json:"targetSSLProxyRef,omitempty"`
	TargetHTTPProxyRef   *ResourceRef `json:"targetHTTPProxyRef,omitempty"`
	TargetHTTPSProxyRef  *ResourceRef `json:"targetHTTPSProxyRef,omitempty"`
	GoogleAPIsBundle     *string      `json:"googleAPIsBundle,omitempty"`
	ServiceAttachmentRef *ResourceRef `json:"serviceAttachmentRef,omitempty"`
}

type ForwardingRuleIPAddress struct {
	AddressRef *ResourceRef `json:"addressRef,omitempty"`
	IP         *string      `json:"ip,omitempty"`
}

type ComputeForwardingRuleStatus struct {
	Conditions         []Condition `json:"conditions,omitempty"`
	BaseForwardingRule *string     `json:"baseForwardingRule,omitempty"`
	CreationTimestamp  *string     `json:"creationTimestamp,omitempty"`
	LabelFingerprint   *string     `json:"labelFingerprint,omitempty"`
	ObservedGeneration *int64      `json:"observedGeneration,omitempty"`
	SelfLink           *string     `json:"selfLink,omitempty"`
	ServiceName        *string     `json:"serviceName,omitempty"`
	Target             *string     `json:"target,omitempty"`
}

// -----------------------------------------------------------------------------
// ComputeBackendService
// -----------------------------------------------------------------------------

// ComputeBackendService represents a regional or global compute backend service.
type ComputeBackendService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ComputeBackendServiceSpec   `json:"spec,omitempty"`
	Status ComputeBackendServiceStatus `json:"status,omitempty"`
}

// ComputeBackendServiceList is a list of ComputeBackendService.
type ComputeBackendServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComputeBackendService `json:"items"`
}

type ComputeBackendServiceSpec struct {
	Location                     string                         `json:"location"`
	ResourceID                   *string                        `json:"resourceID,omitempty"`
	Description                  *string                        `json:"description,omitempty"`
	Protocol                     *string                        `json:"protocol,omitempty"`
	LoadBalancingScheme          *string                        `json:"loadBalancingScheme,omitempty"`
	PortName                     *string                        `json:"portName,omitempty"`
	TimeoutSec                   *int64                         `json:"timeoutSec,omitempty"`
	ConnectionDrainingTimeoutSec *int64                         `json:"connectionDrainingTimeoutSec,omitempty"`
	SessionAffinity              *string                        `json:"sessionAffinity,omitempty"`
	HealthChecks                 []BackendServiceHealthCheckRef `json:"healthChecks,omitempty"`
	Backend                      []BackendServiceBackend        `json:"backend,omitempty"`
	EnableCDN                    *bool                          `json:"enableCDN,omitempty"`
}

type BackendServiceHealthCheckRef struct {
	HealthCheckRef     *ResourceRef `json:"healthCheckRef,omitempty"`
	HTTPHealthCheckRef *ResourceRef `json:"httpHealthCheckRef,omitempty"`
}

type BackendServiceBackend struct {
	Description               *string      `json:"description,omitempty"`
	Group                     BackendGroup `json:"group"`
	BalancingMode             *string      `json:"balancingMode,omitempty"`
	CapacityScaler            *float64     `json:"capacityScaler,omitempty"`
	MaxConnections            *int64       `json:"maxConnections,omitempty"`
	MaxConnectionsPerEndpoint *int64       `json:"maxConnectionsPerEndpoint,omitempty"`
	MaxConnectionsPerInstance *int64       `json:"maxConnectionsPerInstance,omitempty"`
	MaxRate                   *int64       `json:"maxRate,omitempty"`
	MaxRatePerEndpoint        *float64     `json:"maxRatePerEndpoint,omitempty"`
	MaxRatePerInstance        *float64     `json:"maxRatePerInstance,omitempty"`
	MaxUtilization            *float64     `json:"maxUtilization,omitempty"`
}

type BackendGroup struct {
	NetworkEndpointGroupRef *ResourceRef `json:"networkEndpointGroupRef,omitempty"`
	InstanceGroupRef        *ResourceRef `json:"instanceGroupRef,omitempty"`
}

type ComputeBackendServiceStatus struct {
	Conditions         []Condition `json:"conditions,omitempty"`
	CreationTimestamp  *string     `json:"creationTimestamp,omitempty"`
	Fingerprint        *string     `json:"fingerprint,omitempty"`
	GeneratedId        *int64      `json:"generatedId,omitempty"`
	ObservedGeneration *int64      `json:"observedGeneration,omitempty"`
	SelfLink           *string     `json:"selfLink,omitempty"`
}

// -----------------------------------------------------------------------------
// ComputeHealthCheck
// -----------------------------------------------------------------------------

// ComputeHealthCheck represents a regional or global compute health check.
type ComputeHealthCheck struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ComputeHealthCheckSpec   `json:"spec,omitempty"`
	Status ComputeHealthCheckStatus `json:"status,omitempty"`
}

// ComputeHealthCheckList is a list of ComputeHealthCheck.
type ComputeHealthCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComputeHealthCheck `json:"items"`
}

type ComputeHealthCheckSpec struct {
	Location           string           `json:"location"`
	ResourceID         *string          `json:"resourceID,omitempty"`
	Description        *string          `json:"description,omitempty"`
	CheckIntervalSec   *int64           `json:"checkIntervalSec,omitempty"`
	TimeoutSec         *int64           `json:"timeoutSec,omitempty"`
	HealthyThreshold   *int64           `json:"healthyThreshold,omitempty"`
	UnhealthyThreshold *int64           `json:"unhealthyThreshold,omitempty"`
	TCPHealthCheck     *HealthCheckTCP  `json:"tcpHealthCheck,omitempty"`
	HTTPHealthCheck    *HealthCheckHTTP `json:"httpHealthCheck,omitempty"`
	HTTPSHealthCheck   *HealthCheckHTTP `json:"httpsHealthCheck,omitempty"`
	SSLHealthCheck     *HealthCheckTCP  `json:"sslHealthCheck,omitempty"`
}

type HealthCheckTCP struct {
	Port              *int64  `json:"port,omitempty"`
	PortName          *string `json:"portName,omitempty"`
	PortSpecification *string `json:"portSpecification,omitempty"`
	ProxyHeader       *string `json:"proxyHeader,omitempty"`
	Request           *string `json:"request,omitempty"`
	Response          *string `json:"response,omitempty"`
}

type HealthCheckHTTP struct {
	Port              *int64  `json:"port,omitempty"`
	PortName          *string `json:"portName,omitempty"`
	PortSpecification *string `json:"portSpecification,omitempty"`
	Host              *string `json:"host,omitempty"`
	RequestPath       *string `json:"requestPath,omitempty"`
	Response          *string `json:"response,omitempty"`
	ProxyHeader       *string `json:"proxyHeader,omitempty"`
}

type ComputeHealthCheckStatus struct {
	Conditions         []Condition `json:"conditions,omitempty"`
	CreationTimestamp  *string     `json:"creationTimestamp,omitempty"`
	ObservedGeneration *int64      `json:"observedGeneration,omitempty"`
	SelfLink           *string     `json:"selfLink,omitempty"`
	Type               *string     `json:"type,omitempty"`
}

// -----------------------------------------------------------------------------
// ComputeTargetTCPProxy
// -----------------------------------------------------------------------------

// ComputeTargetTCPProxy represents a (global by default) compute target TCP proxy.
type ComputeTargetTCPProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ComputeTargetTCPProxySpec   `json:"spec,omitempty"`
	Status ComputeTargetTCPProxyStatus `json:"status,omitempty"`
}

// ComputeTargetTCPProxyList is a list of ComputeTargetTCPProxy.
type ComputeTargetTCPProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComputeTargetTCPProxy `json:"items"`
}

type ComputeTargetTCPProxySpec struct {
	BackendServiceRef ResourceRef `json:"backendServiceRef"`
	Description       *string     `json:"description,omitempty"`
	Location          *string     `json:"location,omitempty"`
	ProxyBind         *bool       `json:"proxyBind,omitempty"`
	// ProxyHeader is NONE or PROXY_V1.
	ProxyHeader *string `json:"proxyHeader,omitempty"`
	ResourceID  *string `json:"resourceID,omitempty"`
}

type ComputeTargetTCPProxyStatus struct {
	Conditions         []Condition `json:"conditions,omitempty"`
	CreationTimestamp  *string     `json:"creationTimestamp,omitempty"`
	ExternalRef        *string     `json:"externalRef,omitempty"`
	ObservedGeneration *int64      `json:"observedGeneration,omitempty"`
	ProxyId            *int64      `json:"proxyId,omitempty"`
	SelfLink           *string     `json:"selfLink,omitempty"`
}

// -----------------------------------------------------------------------------
// ComputeFirewall (used by GlobalProxyNLB to admit health-check/GFE traffic)
// -----------------------------------------------------------------------------

// ComputeFirewall represents a VPC firewall rule.
type ComputeFirewall struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ComputeFirewallSpec   `json:"spec,omitempty"`
	Status ComputeFirewallStatus `json:"status,omitempty"`
}

// ComputeFirewallList is a list of ComputeFirewall.
type ComputeFirewallList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ComputeFirewall `json:"items"`
}

type ComputeFirewallSpec struct {
	ResourceID            *string         `json:"resourceID,omitempty"`
	Description           *string         `json:"description,omitempty"`
	Disabled              *bool           `json:"disabled,omitempty"`
	Direction             *string         `json:"direction,omitempty"`
	Priority              *int64          `json:"priority,omitempty"`
	NetworkRef            ResourceRef     `json:"networkRef"`
	SourceRanges          []string        `json:"sourceRanges,omitempty"`
	DestinationRanges     []string        `json:"destinationRanges,omitempty"`
	SourceTags            []string        `json:"sourceTags,omitempty"`
	TargetTags            []string        `json:"targetTags,omitempty"`
	SourceServiceAccounts []string        `json:"sourceServiceAccounts,omitempty"`
	TargetServiceAccounts []string        `json:"targetServiceAccounts,omitempty"`
	Allowed               []FirewallRule  `json:"allow,omitempty"`
	Denied                []FirewallRule  `json:"deny,omitempty"`
	LogConfig             *FirewallLogCfg `json:"logConfig,omitempty"`
}

type FirewallRule struct {
	Protocol string   `json:"protocol"`
	Ports    []string `json:"ports,omitempty"`
}

type FirewallLogCfg struct {
	Metadata *string `json:"metadata,omitempty"`
}

type ComputeFirewallStatus struct {
	Conditions         []Condition `json:"conditions,omitempty"`
	CreationTimestamp  *string     `json:"creationTimestamp,omitempty"`
	ObservedGeneration *int64      `json:"observedGeneration,omitempty"`
	SelfLink           *string     `json:"selfLink,omitempty"`
}
