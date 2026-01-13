package telemetryfields

import "testing"

func TestNormalizeErrorType(t *testing.T) {
	cases := map[string]string{
		"ApiCallError":        ErrorTypeAPICall,
		"apiCallError":        ErrorTypeAPICall,
		"api_call_error":      ErrorTypeAPICall,
		"InternalError":       ErrorTypeInternal,
		"internalError":       ErrorTypeInternal,
		"ParameterError":      ErrorTypeParameter,
		"NotImplementedError": ErrorTypeNotImplemented,
		"somethingElse":       "somethingelse",
	}
	for k, want := range cases {
		if got := NormalizeErrorType(k); got != want {
			t.Fatalf("NormalizeErrorType(%q) = %q, want %q", k, got, want)
		}
	}
}

func TestNormalizeNetworkPlugin(t *testing.T) {
	cases := map[string]string{
		"Kubernetes-HostPort": NetworkPluginKubernetesHostPort,
		"kubernetes-hostport": NetworkPluginKubernetesHostPort,
		"hostport":            NetworkPluginKubernetesHostPort,
		"Kubernetes-NodePort": NetworkPluginKubernetesNodePort,
		"nodeport":            NetworkPluginKubernetesNodePort,
		"AlibabaCloud-NLB":    NetworkPluginAlibabaCloudNLB,
	}
	for k, want := range cases {
		if got := NormalizeNetworkPlugin(k); got != want {
			t.Fatalf("NormalizeNetworkPlugin(%q) = %q, want %q", k, got, want)
		}
	}
}
