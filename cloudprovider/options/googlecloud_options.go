/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package options

import "regexp"

// gcpRegionRegex is a loose validator for GCP region/zone names (lowercase
// letters, digits, dashes; cannot start/end with dash).
var gcpRegionRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`)

// GoogleCloudOptions captures the Google Cloud provider's TOML configuration.
// Two plugins live under this provider and share project/network defaults:
//   - GoogleCloud-PassthroughNLB
//   - GoogleCloud-GlobalProxyNLB
type GoogleCloudOptions struct {
	Enable            bool                  `toml:"enable"`
	ProjectID         string                `toml:"project_id"`
	DefaultRegion     string                `toml:"default_region"`
	DefaultNetwork    string                `toml:"default_network"`
	DefaultSubnetwork string                `toml:"default_subnetwork"`
	PassthroughNLB    PassthroughNLBOptions `toml:"passthrough_nlb"`
	GlobalProxyNLB    GlobalProxyNLBOptions `toml:"global_proxy_nlb"`
}

// PassthroughNLBOptions configures the GoogleCloud-PassthroughNLB plugin.
type PassthroughNLBOptions struct {
	Enable                bool `toml:"enable"`
	RetainOnDeleteDefault bool `toml:"retain_on_delete_default"`
	// NetworkTier is PREMIUM (default) or STANDARD.
	NetworkTier string `toml:"network_tier"`
}

// GlobalProxyNLBOptions configures the GoogleCloud-GlobalProxyNLB plugin.
type GlobalProxyNLBOptions struct {
	Enable                bool `toml:"enable"`
	RetainOnDeleteDefault bool `toml:"retain_on_delete_default"`
	// FirewallNetworkRef is the KCC ComputeNetwork name used as networkRef on
	// the per-pod ComputeFirewall opening up GFE/HC ranges to backend ports.
	// Empty falls back to DefaultNetwork on the parent struct.
	FirewallNetworkRef string `toml:"firewall_network_ref"`
}

// Enabled returns whether the provider is on at all. The per-plugin Enable
// flags further gate plugin registration.
func (o GoogleCloudOptions) Enabled() bool { return o.Enable }

// Valid sanity-checks the TOML. Called once at startup; on false the provider
// is skipped with a log line.
func (o GoogleCloudOptions) Valid() bool {
	if !o.Enable {
		// Disabled providers don't need to be valid.
		return true
	}
	if o.ProjectID == "" {
		return false
	}
	if o.PassthroughNLB.Enable && o.DefaultRegion != "" && !gcpRegionRegex.MatchString(o.DefaultRegion) {
		return false
	}
	switch o.PassthroughNLB.NetworkTier {
	case "", "PREMIUM", "STANDARD":
	default:
		return false
	}
	return true
}
