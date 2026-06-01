/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Hand-written DeepCopy implementations to satisfy controller-runtime's
// runtime.Object contract. The "zz_generated" prefix matches the convention used
// for generated code but these are maintained by hand (the package is small).

package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime"
)

// stringPtr / int64Ptr / boolPtr / float64Ptr are pointer-deep-copy helpers.
func stringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func int64Ptr(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func boolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func float64Ptr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneConditions(in []Condition) []Condition {
	if in == nil {
		return nil
	}
	out := make([]Condition, len(in))
	copy(out, in)
	return out
}

// ResourceRef -----------------------------------------------------------------

func (in *ResourceRef) DeepCopy() *ResourceRef {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// ComputeAddress --------------------------------------------------------------

func (in *ComputeAddress) DeepCopyInto(out *ComputeAddress) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ComputeAddress) DeepCopy() *ComputeAddress {
	if in == nil {
		return nil
	}
	out := new(ComputeAddress)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeAddress) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeAddressList) DeepCopyInto(out *ComputeAddressList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ComputeAddress, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ComputeAddressList) DeepCopy() *ComputeAddressList {
	if in == nil {
		return nil
	}
	out := new(ComputeAddressList)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeAddressList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeAddressSpec) DeepCopyInto(out *ComputeAddressSpec) {
	*out = *in
	out.ResourceID = stringPtr(in.ResourceID)
	out.Address = stringPtr(in.Address)
	out.AddressType = stringPtr(in.AddressType)
	out.IPVersion = stringPtr(in.IPVersion)
	out.NetworkTier = stringPtr(in.NetworkTier)
	out.Purpose = stringPtr(in.Purpose)
	out.Description = stringPtr(in.Description)
	out.PrefixLength = int64Ptr(in.PrefixLength)
	if in.NetworkRef != nil {
		out.NetworkRef = in.NetworkRef.DeepCopy()
	}
	if in.SubnetworkRef != nil {
		out.SubnetworkRef = in.SubnetworkRef.DeepCopy()
	}
}

func (in *ComputeAddressStatus) DeepCopyInto(out *ComputeAddressStatus) {
	*out = *in
	out.Conditions = cloneConditions(in.Conditions)
	out.CreationTimestamp = stringPtr(in.CreationTimestamp)
	out.LabelFingerprint = stringPtr(in.LabelFingerprint)
	out.ObservedGeneration = int64Ptr(in.ObservedGeneration)
	out.SelfLink = stringPtr(in.SelfLink)
	out.Users = cloneStringSlice(in.Users)
	if in.ObservedState != nil {
		obs := *in.ObservedState
		obs.Address = stringPtr(in.ObservedState.Address)
		out.ObservedState = &obs
	}
}

// ComputeForwardingRule -------------------------------------------------------

func (in *ComputeForwardingRule) DeepCopyInto(out *ComputeForwardingRule) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ComputeForwardingRule) DeepCopy() *ComputeForwardingRule {
	if in == nil {
		return nil
	}
	out := new(ComputeForwardingRule)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeForwardingRule) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeForwardingRuleList) DeepCopyInto(out *ComputeForwardingRuleList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ComputeForwardingRule, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ComputeForwardingRuleList) DeepCopy() *ComputeForwardingRuleList {
	if in == nil {
		return nil
	}
	out := new(ComputeForwardingRuleList)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeForwardingRuleList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeForwardingRuleSpec) DeepCopyInto(out *ComputeForwardingRuleSpec) {
	*out = *in
	out.ResourceID = stringPtr(in.ResourceID)
	out.Description = stringPtr(in.Description)
	out.IPProtocol = stringPtr(in.IPProtocol)
	out.IPVersion = stringPtr(in.IPVersion)
	out.LoadBalancingScheme = stringPtr(in.LoadBalancingScheme)
	out.NetworkTier = stringPtr(in.NetworkTier)
	out.PortRange = stringPtr(in.PortRange)
	out.Ports = cloneStringSlice(in.Ports)
	out.AllPorts = boolPtr(in.AllPorts)
	out.AllowGlobalAccess = boolPtr(in.AllowGlobalAccess)
	out.ServiceLabel = stringPtr(in.ServiceLabel)
	if in.Target != nil {
		t := ForwardingRuleTarget{}
		if in.Target.TargetTCPProxyRef != nil {
			t.TargetTCPProxyRef = in.Target.TargetTCPProxyRef.DeepCopy()
		}
		if in.Target.TargetSSLProxyRef != nil {
			t.TargetSSLProxyRef = in.Target.TargetSSLProxyRef.DeepCopy()
		}
		if in.Target.TargetHTTPProxyRef != nil {
			t.TargetHTTPProxyRef = in.Target.TargetHTTPProxyRef.DeepCopy()
		}
		if in.Target.TargetHTTPSProxyRef != nil {
			t.TargetHTTPSProxyRef = in.Target.TargetHTTPSProxyRef.DeepCopy()
		}
		if in.Target.ServiceAttachmentRef != nil {
			t.ServiceAttachmentRef = in.Target.ServiceAttachmentRef.DeepCopy()
		}
		t.GoogleAPIsBundle = stringPtr(in.Target.GoogleAPIsBundle)
		out.Target = &t
	}
	if in.BackendServiceRef != nil {
		out.BackendServiceRef = in.BackendServiceRef.DeepCopy()
	}
	if in.IPAddress != nil {
		ip := ForwardingRuleIPAddress{}
		if in.IPAddress.AddressRef != nil {
			ip.AddressRef = in.IPAddress.AddressRef.DeepCopy()
		}
		ip.IP = stringPtr(in.IPAddress.IP)
		out.IPAddress = &ip
	}
	if in.NetworkRef != nil {
		out.NetworkRef = in.NetworkRef.DeepCopy()
	}
	if in.SubnetworkRef != nil {
		out.SubnetworkRef = in.SubnetworkRef.DeepCopy()
	}
}

func (in *ComputeForwardingRuleStatus) DeepCopyInto(out *ComputeForwardingRuleStatus) {
	*out = *in
	out.Conditions = cloneConditions(in.Conditions)
	out.BaseForwardingRule = stringPtr(in.BaseForwardingRule)
	out.CreationTimestamp = stringPtr(in.CreationTimestamp)
	out.LabelFingerprint = stringPtr(in.LabelFingerprint)
	out.ObservedGeneration = int64Ptr(in.ObservedGeneration)
	out.SelfLink = stringPtr(in.SelfLink)
	out.ServiceName = stringPtr(in.ServiceName)
	out.Target = stringPtr(in.Target)
}

// ComputeBackendService -------------------------------------------------------

func (in *ComputeBackendService) DeepCopyInto(out *ComputeBackendService) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ComputeBackendService) DeepCopy() *ComputeBackendService {
	if in == nil {
		return nil
	}
	out := new(ComputeBackendService)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeBackendService) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeBackendServiceList) DeepCopyInto(out *ComputeBackendServiceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ComputeBackendService, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ComputeBackendServiceList) DeepCopy() *ComputeBackendServiceList {
	if in == nil {
		return nil
	}
	out := new(ComputeBackendServiceList)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeBackendServiceList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeBackendServiceSpec) DeepCopyInto(out *ComputeBackendServiceSpec) {
	*out = *in
	out.ResourceID = stringPtr(in.ResourceID)
	out.Description = stringPtr(in.Description)
	out.Protocol = stringPtr(in.Protocol)
	out.LoadBalancingScheme = stringPtr(in.LoadBalancingScheme)
	out.PortName = stringPtr(in.PortName)
	out.TimeoutSec = int64Ptr(in.TimeoutSec)
	out.ConnectionDrainingTimeoutSec = int64Ptr(in.ConnectionDrainingTimeoutSec)
	out.SessionAffinity = stringPtr(in.SessionAffinity)
	out.EnableCDN = boolPtr(in.EnableCDN)
	if in.HealthChecks != nil {
		out.HealthChecks = make([]BackendServiceHealthCheckRef, len(in.HealthChecks))
		for i, hc := range in.HealthChecks {
			h := BackendServiceHealthCheckRef{}
			if hc.HealthCheckRef != nil {
				h.HealthCheckRef = hc.HealthCheckRef.DeepCopy()
			}
			if hc.HTTPHealthCheckRef != nil {
				h.HTTPHealthCheckRef = hc.HTTPHealthCheckRef.DeepCopy()
			}
			out.HealthChecks[i] = h
		}
	}
	if in.Backend != nil {
		out.Backend = make([]BackendServiceBackend, len(in.Backend))
		for i, b := range in.Backend {
			nb := BackendServiceBackend{
				Description:               stringPtr(b.Description),
				BalancingMode:             stringPtr(b.BalancingMode),
				CapacityScaler:            float64Ptr(b.CapacityScaler),
				MaxConnections:            int64Ptr(b.MaxConnections),
				MaxConnectionsPerEndpoint: int64Ptr(b.MaxConnectionsPerEndpoint),
				MaxConnectionsPerInstance: int64Ptr(b.MaxConnectionsPerInstance),
				MaxRate:                   int64Ptr(b.MaxRate),
				MaxRatePerEndpoint:        float64Ptr(b.MaxRatePerEndpoint),
				MaxRatePerInstance:        float64Ptr(b.MaxRatePerInstance),
				MaxUtilization:            float64Ptr(b.MaxUtilization),
			}
			if b.Group.NetworkEndpointGroupRef != nil {
				nb.Group.NetworkEndpointGroupRef = b.Group.NetworkEndpointGroupRef.DeepCopy()
			}
			if b.Group.InstanceGroupRef != nil {
				nb.Group.InstanceGroupRef = b.Group.InstanceGroupRef.DeepCopy()
			}
			out.Backend[i] = nb
		}
	}
}

func (in *ComputeBackendServiceStatus) DeepCopyInto(out *ComputeBackendServiceStatus) {
	*out = *in
	out.Conditions = cloneConditions(in.Conditions)
	out.CreationTimestamp = stringPtr(in.CreationTimestamp)
	out.Fingerprint = stringPtr(in.Fingerprint)
	out.GeneratedId = int64Ptr(in.GeneratedId)
	out.ObservedGeneration = int64Ptr(in.ObservedGeneration)
	out.SelfLink = stringPtr(in.SelfLink)
}

// ComputeHealthCheck ----------------------------------------------------------

func (in *ComputeHealthCheck) DeepCopyInto(out *ComputeHealthCheck) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ComputeHealthCheck) DeepCopy() *ComputeHealthCheck {
	if in == nil {
		return nil
	}
	out := new(ComputeHealthCheck)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeHealthCheck) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeHealthCheckList) DeepCopyInto(out *ComputeHealthCheckList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ComputeHealthCheck, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ComputeHealthCheckList) DeepCopy() *ComputeHealthCheckList {
	if in == nil {
		return nil
	}
	out := new(ComputeHealthCheckList)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeHealthCheckList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeHealthCheckSpec) DeepCopyInto(out *ComputeHealthCheckSpec) {
	*out = *in
	out.ResourceID = stringPtr(in.ResourceID)
	out.Description = stringPtr(in.Description)
	out.CheckIntervalSec = int64Ptr(in.CheckIntervalSec)
	out.TimeoutSec = int64Ptr(in.TimeoutSec)
	out.HealthyThreshold = int64Ptr(in.HealthyThreshold)
	out.UnhealthyThreshold = int64Ptr(in.UnhealthyThreshold)
	if in.TCPHealthCheck != nil {
		cp := *in.TCPHealthCheck
		cp.Port = int64Ptr(in.TCPHealthCheck.Port)
		cp.PortName = stringPtr(in.TCPHealthCheck.PortName)
		cp.PortSpecification = stringPtr(in.TCPHealthCheck.PortSpecification)
		cp.ProxyHeader = stringPtr(in.TCPHealthCheck.ProxyHeader)
		cp.Request = stringPtr(in.TCPHealthCheck.Request)
		cp.Response = stringPtr(in.TCPHealthCheck.Response)
		out.TCPHealthCheck = &cp
	}
	if in.HTTPHealthCheck != nil {
		cp := *in.HTTPHealthCheck
		cp.Port = int64Ptr(in.HTTPHealthCheck.Port)
		cp.PortName = stringPtr(in.HTTPHealthCheck.PortName)
		cp.PortSpecification = stringPtr(in.HTTPHealthCheck.PortSpecification)
		cp.Host = stringPtr(in.HTTPHealthCheck.Host)
		cp.RequestPath = stringPtr(in.HTTPHealthCheck.RequestPath)
		cp.Response = stringPtr(in.HTTPHealthCheck.Response)
		cp.ProxyHeader = stringPtr(in.HTTPHealthCheck.ProxyHeader)
		out.HTTPHealthCheck = &cp
	}
	if in.HTTPSHealthCheck != nil {
		cp := *in.HTTPSHealthCheck
		cp.Port = int64Ptr(in.HTTPSHealthCheck.Port)
		cp.PortName = stringPtr(in.HTTPSHealthCheck.PortName)
		cp.PortSpecification = stringPtr(in.HTTPSHealthCheck.PortSpecification)
		cp.Host = stringPtr(in.HTTPSHealthCheck.Host)
		cp.RequestPath = stringPtr(in.HTTPSHealthCheck.RequestPath)
		cp.Response = stringPtr(in.HTTPSHealthCheck.Response)
		cp.ProxyHeader = stringPtr(in.HTTPSHealthCheck.ProxyHeader)
		out.HTTPSHealthCheck = &cp
	}
	if in.SSLHealthCheck != nil {
		cp := *in.SSLHealthCheck
		cp.Port = int64Ptr(in.SSLHealthCheck.Port)
		cp.PortName = stringPtr(in.SSLHealthCheck.PortName)
		cp.PortSpecification = stringPtr(in.SSLHealthCheck.PortSpecification)
		cp.ProxyHeader = stringPtr(in.SSLHealthCheck.ProxyHeader)
		cp.Request = stringPtr(in.SSLHealthCheck.Request)
		cp.Response = stringPtr(in.SSLHealthCheck.Response)
		out.SSLHealthCheck = &cp
	}
}

func (in *ComputeHealthCheckStatus) DeepCopyInto(out *ComputeHealthCheckStatus) {
	*out = *in
	out.Conditions = cloneConditions(in.Conditions)
	out.CreationTimestamp = stringPtr(in.CreationTimestamp)
	out.ObservedGeneration = int64Ptr(in.ObservedGeneration)
	out.SelfLink = stringPtr(in.SelfLink)
	out.Type = stringPtr(in.Type)
}

// ComputeTargetTCPProxy -------------------------------------------------------

func (in *ComputeTargetTCPProxy) DeepCopyInto(out *ComputeTargetTCPProxy) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ComputeTargetTCPProxy) DeepCopy() *ComputeTargetTCPProxy {
	if in == nil {
		return nil
	}
	out := new(ComputeTargetTCPProxy)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeTargetTCPProxy) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeTargetTCPProxyList) DeepCopyInto(out *ComputeTargetTCPProxyList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ComputeTargetTCPProxy, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ComputeTargetTCPProxyList) DeepCopy() *ComputeTargetTCPProxyList {
	if in == nil {
		return nil
	}
	out := new(ComputeTargetTCPProxyList)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeTargetTCPProxyList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeTargetTCPProxySpec) DeepCopyInto(out *ComputeTargetTCPProxySpec) {
	*out = *in
	out.BackendServiceRef = *in.BackendServiceRef.DeepCopy()
	out.Description = stringPtr(in.Description)
	out.Location = stringPtr(in.Location)
	out.ProxyBind = boolPtr(in.ProxyBind)
	out.ProxyHeader = stringPtr(in.ProxyHeader)
	out.ResourceID = stringPtr(in.ResourceID)
}

func (in *ComputeTargetTCPProxyStatus) DeepCopyInto(out *ComputeTargetTCPProxyStatus) {
	*out = *in
	out.Conditions = cloneConditions(in.Conditions)
	out.CreationTimestamp = stringPtr(in.CreationTimestamp)
	out.ExternalRef = stringPtr(in.ExternalRef)
	out.ObservedGeneration = int64Ptr(in.ObservedGeneration)
	out.ProxyId = int64Ptr(in.ProxyId)
	out.SelfLink = stringPtr(in.SelfLink)
}

// ComputeFirewall -------------------------------------------------------------

func (in *ComputeFirewall) DeepCopyInto(out *ComputeFirewall) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *ComputeFirewall) DeepCopy() *ComputeFirewall {
	if in == nil {
		return nil
	}
	out := new(ComputeFirewall)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeFirewall) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeFirewallList) DeepCopyInto(out *ComputeFirewallList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ComputeFirewall, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *ComputeFirewallList) DeepCopy() *ComputeFirewallList {
	if in == nil {
		return nil
	}
	out := new(ComputeFirewallList)
	in.DeepCopyInto(out)
	return out
}

func (in *ComputeFirewallList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ComputeFirewallSpec) DeepCopyInto(out *ComputeFirewallSpec) {
	*out = *in
	out.ResourceID = stringPtr(in.ResourceID)
	out.Description = stringPtr(in.Description)
	out.Disabled = boolPtr(in.Disabled)
	out.Direction = stringPtr(in.Direction)
	out.Priority = int64Ptr(in.Priority)
	out.NetworkRef = *in.NetworkRef.DeepCopy()
	out.SourceRanges = cloneStringSlice(in.SourceRanges)
	out.DestinationRanges = cloneStringSlice(in.DestinationRanges)
	out.SourceTags = cloneStringSlice(in.SourceTags)
	out.TargetTags = cloneStringSlice(in.TargetTags)
	out.SourceServiceAccounts = cloneStringSlice(in.SourceServiceAccounts)
	out.TargetServiceAccounts = cloneStringSlice(in.TargetServiceAccounts)
	if in.Allowed != nil {
		out.Allowed = make([]FirewallRule, len(in.Allowed))
		for i, r := range in.Allowed {
			out.Allowed[i] = FirewallRule{Protocol: r.Protocol, Ports: cloneStringSlice(r.Ports)}
		}
	}
	if in.Denied != nil {
		out.Denied = make([]FirewallRule, len(in.Denied))
		for i, r := range in.Denied {
			out.Denied[i] = FirewallRule{Protocol: r.Protocol, Ports: cloneStringSlice(r.Ports)}
		}
	}
	if in.LogConfig != nil {
		cp := *in.LogConfig
		cp.Metadata = stringPtr(in.LogConfig.Metadata)
		out.LogConfig = &cp
	}
}

func (in *ComputeFirewallStatus) DeepCopyInto(out *ComputeFirewallStatus) {
	*out = *in
	out.Conditions = cloneConditions(in.Conditions)
	out.CreationTimestamp = stringPtr(in.CreationTimestamp)
	out.ObservedGeneration = int64Ptr(in.ObservedGeneration)
	out.SelfLink = stringPtr(in.SelfLink)
}
