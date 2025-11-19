package tracing

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
)

func TestEnsureNetworkStatusAttrAddsDefault(t *testing.T) {
	attrs := []attribute.KeyValue{AttrComponent("okg-controller-manager")}
	result := EnsureNetworkStatusAttr(attrs, "waiting")
	if len(result) != len(attrs)+1 {
		t.Fatalf("expected default network status to be appended")
	}
	found := false
	for _, attr := range result {
		if attr.Key == networkStatusKey {
			found = true
			if attr.Value.AsString() != "waiting" {
				t.Fatalf("expected waiting status, got %s", attr.Value.AsString())
			}
		}
	}
	if !found {
		t.Fatalf("network status attribute not found")
	}
}

func TestEnsureNetworkStatusAttrRespectsExisting(t *testing.T) {
	attrs := []attribute.KeyValue{AttrNetworkStatus("ready")}
	result := EnsureNetworkStatusAttr(attrs, "waiting")
	if len(result) != len(attrs) {
		t.Fatalf("expected slice length to remain unchanged")
	}
	if result[0].Value.AsString() != "ready" {
		t.Fatalf("expected ready to be preserved, got %s", result[0].Value.AsString())
	}
}

func TestCloudProviderFromNetworkType(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		expected    CloudProvider
		shouldMatch bool
	}{
		{"kubernetes", "Kubernetes-HostPort", CloudProviderKubernetes, true},
		{"kubernetes_lower", "kubernetes-hostport", CloudProviderKubernetes, true},
		{"alibaba", "AlibabaCloud-NLB", CloudProviderAlibabaCloud, true},
		{"alibaba_mixed", "  alibabacloud-custom  ", CloudProviderAlibabaCloud, true},
		{"aws", "AmazonWebServices-NLB", CloudProviderAWS, true},
		{"tencent", "TencentCloud-CLB", CloudProviderTencentCloud, true},
		{"volcengine", "Volcengine-CLB", CloudProviderVolcengine, true},
		{"jdcloud", "JDCloud-NLB", CloudProviderJDCloud, true},
		{"hwcloud", "HwCloud-ELB", CloudProviderHwCloud, true},
		{"hwcloud_lower", "hwcloud-elb", CloudProviderHwCloud, true},
		{"unknown", "Custom-Network", CloudProviderUnknown, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			provider, ok := CloudProviderFromNetworkType(tc.input)
			if ok != tc.shouldMatch {
				t.Fatalf("expected match=%v, got %v", tc.shouldMatch, ok)
			}
			if ok && provider != tc.expected {
				t.Fatalf("expected provider %s, got %s", tc.expected, provider)
			}
		})
	}
}

func TestAttrNetworkPluginNormalizesValue(t *testing.T) {
	attr := AttrNetworkPlugin("  Kubernetes-HostPort  ")
	if attr.Value.AsString() != "kubernetes-hostport" {
		t.Fatalf("expected kubernetes-hostport, got %s", attr.Value.AsString())
	}

	attr = AttrNetworkPlugin("Custom Plugin Name")
	if attr.Value.AsString() != "custom_plugin_name" {
		t.Fatalf("expected custom_plugin_name, got %s", attr.Value.AsString())
	}
}
