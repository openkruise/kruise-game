package telemetryfields

import "strings"

// This file contains enumeration values for low-cardinality telemetry fields.
// These are canonical, snake_case values intended for spanmetrics dimensions.

const (
	// Error types
	ErrorTypeAPICall          = "api_call_error"
	ErrorTypeInternal         = "internal_error"
	ErrorTypeParameter        = "parameter_error"
	ErrorTypeNotImplemented   = "not_implemented_error"
	ErrorTypeResourceNotReady = "resource_not_ready"
	ErrorTypePortExhausted    = "port_exhausted"

	// Network statuses
	NetworkStatusReady    = "ready"
	NetworkStatusNotReady = "not_ready"
	NetworkStatusError    = "error"
	NetworkStatusWaiting  = "waiting"

	// Plugin slugs (canonical snake_case)
	NetworkPluginKubernetesHostPort = "kubernetes_hostport"
	NetworkPluginKubernetesNodePort = "kubernetes_nodeport"
	NetworkPluginAlibabaCloudNLB    = "alibabacloud_nlb"
)

// NormalizeErrorType maps many possible error-type string formats into a canonical
// lower_snake_case enumeration. It accepts the raw plugin error type string (e.g., "apiCallError" or "ApiCallError")
// and returns the canonical form (e.g., "api_call_error").
func NormalizeErrorType(raw string) string {
	switch raw {
	case "ApiCallError", "apiCallError", "api_call_error", "api_callerror", "APICallError":
		return ErrorTypeAPICall
	case "InternalError", "internalError", "internal_error":
		return ErrorTypeInternal
	case "ParameterError", "parameterError", "parameter_error":
		return ErrorTypeParameter
	case "NotImplementedError", "notImplementedError", "not_implemented_error":
		return ErrorTypeNotImplemented
	case "ResourceNotReady", "resourceNotReady", "resource_not_ready":
		return ErrorTypeResourceNotReady
	case "PortExhausted", "portExhausted", "port_exhausted":
		return ErrorTypePortExhausted
	default:
		// Fallback: normalize by heuristics – convert camelCase to snake_case-like lower case
		// replace spaces and hyphens with underscore and to lower.
		res := normalizeDimensionValue(raw)
		// also convert hyphens to underscores
		res = strings.ReplaceAll(res, "-", "_")
		return res
	}
}

// NormalizeNetworkPlugin returns a canonical plugin slug in snake_case (e.g. "kubernetes_hostport").
func NormalizeNetworkPlugin(raw string) string {
	if raw == "" {
		return ""
	}
	lower := normalizeDimensionValue(raw)
	lower = strings.ReplaceAll(lower, "-", "_")
	// map other common variants
	switch lower {
	case "kubernetes_hostport", "kubernetes-hostport", "hostport":
		return NetworkPluginKubernetesHostPort
	case "kubernetes_nodeport", "kubernetes-nodeport", "nodeport":
		return NetworkPluginKubernetesNodePort
	case "alibabacloud_nlb", "alibabacloud-nlb", "nlb":
		return NetworkPluginAlibabaCloudNLB
	default:
		return lower
	}
}

// normalizeDimensionValue converts human-friendly names into a lower-case string
// with spaces and tabs converted to underscore. It does not convert hyphens to underscores
// — callers can do that if they prefer underscores.
func normalizeDimensionValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	if strings.ContainsAny(lower, " \t") {
		lower = strings.Join(strings.Fields(lower), "_")
	}
	return lower
}
