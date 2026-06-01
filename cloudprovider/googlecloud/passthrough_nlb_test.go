/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(s); err != nil {
		t.Fatalf("client-go scheme: %v", err)
	}
	if err := gcpv1beta1.AddToScheme(s); err != nil {
		t.Fatalf("gcp scheme: %v", err)
	}
	if err := gamekruiseiov1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("kruise-game scheme: %v", err)
	}
	return s
}

func newPlugin(t *testing.T) *PassthroughNlbPlugin {
	t.Helper()
	return &PassthroughNlbPlugin{
		projectID:             "demo-project",
		defaultRegion:         "us-central1",
		defaultNetwork:        "default",
		defaultSubnetwork:     "default-us-central1",
		defaultNetworkTier:    "PREMIUM",
		retainOnDeleteDefault: false,
	}
}

func confEntry(name, value string) gamekruiseiov1alpha1.NetworkConfParams {
	return gamekruiseiov1alpha1.NetworkConfParams{Name: name, Value: value}
}

func TestParseConfig_Defaults(t *testing.T) {
	p := newPlugin(t)
	cfg, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Scheme != SchemeExternal {
		t.Errorf("Scheme: want External, got %q", cfg.Scheme)
	}
	if cfg.Region != "us-central1" {
		t.Errorf("Region: want us-central1, got %q", cfg.Region)
	}
	if cfg.NetworkTier != "PREMIUM" {
		t.Errorf("NetworkTier: want PREMIUM, got %q", cfg.NetworkTier)
	}
	if len(cfg.TargetPorts) != 1 || cfg.TargetPorts[0] != 7777 {
		t.Errorf("TargetPorts: want [7777], got %v", cfg.TargetPorts)
	}
	if cfg.Protocols[0] != corev1.ProtocolUDP {
		t.Errorf("Protocols[0]: want UDP, got %q", cfg.Protocols[0])
	}
	if cfg.RetainOnDelete != false {
		t.Errorf("RetainOnDelete: want false (default), got true")
	}
}

func TestParseConfig_AllFields(t *testing.T) {
	p := newPlugin(t)
	cfg, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("ProjectId", "override-proj"),
		confEntry("Region", "europe-west1"),
		confEntry("Scheme", "Internal"),
		confEntry("Network", "vpc-1"),
		confEntry("Subnetwork", "sub-1"),
		confEntry("AllowGlobalAccess", "true"),
		confEntry("NetworkTier", "STANDARD"),
		confEntry("PortProtocols", "7777/UDP,8000/TCP"),
		confEntry("Annotations", "foo=bar,baz=qux"),
		confEntry("RetainOnDelete", "true"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ProjectID != "override-proj" || cfg.Region != "europe-west1" || cfg.Scheme != SchemeInternal {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if !cfg.AllowGlobalAccess || cfg.NetworkTier != "STANDARD" || !cfg.RetainOnDelete {
		t.Errorf("bool/enum overrides not applied: %+v", cfg)
	}
	if len(cfg.TargetPorts) != 2 || cfg.Protocols[0] != corev1.ProtocolUDP || cfg.Protocols[1] != corev1.ProtocolTCP {
		t.Errorf("PortProtocols parse wrong: %+v", cfg)
	}
	if cfg.Annotations["foo"] != "bar" || cfg.Annotations["baz"] != "qux" {
		t.Errorf("Annotations parse wrong: %+v", cfg.Annotations)
	}
}

func TestParseConfig_Errors(t *testing.T) {
	cases := []struct {
		name    string
		entries []gamekruiseiov1alpha1.NetworkConfParams
		want    string
	}{
		{"missing PortProtocols", nil, "PortProtocols is required"},
		{"invalid Scheme", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("Scheme", "Whatever"), confEntry("PortProtocols", "7777"),
		}, "invalid Scheme"},
		{"too many ports", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("PortProtocols", "1,2,3,4,5,6"),
		}, "at most 5"},
		{"invalid port number", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("PortProtocols", "70000/UDP"),
		}, "invalid PortProtocols port 70000"},
		{"invalid protocol", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("PortProtocols", "7777/SCTP"),
		}, "invalid PortProtocols protocol"},
		{"internal needs network", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("Scheme", "Internal"),
			confEntry("Network", ""),
			confEntry("PortProtocols", "7777"),
		}, "network is required"},
		{"bad annotation entry", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("PortProtocols", "7777"),
			confEntry("Annotations", "novalue"),
		}, "invalid Annotations entry"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Special case: clear defaults that the test relies on.
			plugin := newPlugin(t)
			if tc.name == "internal needs network" {
				plugin.defaultNetwork = ""
			}
			_, err := plugin.parseConfig(tc.entries)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestDeriveResourceID_StableAndCapped(t *testing.T) {
	uid := types.UID("11111111-2222-3333-4444-555555555555")
	a := DeriveResourceID(uid, "pnlb-addr")
	b := DeriveResourceID(uid, "pnlb-addr")
	if a != b {
		t.Fatalf("DeriveResourceID not deterministic: %q vs %q", a, b)
	}
	if len(a) > 63 {
		t.Fatalf("DeriveResourceID over 63 chars: %d / %q", len(a), a)
	}
	other := DeriveResourceID(types.UID("99999999-2222-3333-4444-555555555555"), "pnlb-addr")
	if other == a {
		t.Fatalf("different UIDs collided: %q", a)
	}
}

func TestDeriveServiceName_StableAndCapped(t *testing.T) {
	a := DeriveServiceName("very-long-game-server-pod-name-that-may-exceed-the-limit-zzzzz", "gcp-pnlb")
	if len(a) > 63 {
		t.Fatalf("DeriveServiceName over 63 chars: %d / %q", len(a), a)
	}
	b := DeriveServiceName("very-long-game-server-pod-name-that-may-exceed-the-limit-zzzzz", "gcp-pnlb")
	if a != b {
		t.Fatalf("not deterministic: %q vs %q", a, b)
	}
}

func TestEnsureComputeAddress_Idempotent(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	spec := AddressSpec{
		Name:        "gs-addr",
		Namespace:   "ns1",
		Location:    "us-central1",
		AddressType: "EXTERNAL",
		NetworkTier: "PREMIUM",
		ProjectID:   "proj-1",
		ResourceID:  "gs-addr-abc1234567",
	}
	if _, err := EnsureComputeAddress(context.Background(), cli, spec); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if _, err := EnsureComputeAddress(context.Background(), cli, spec); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	got := &gcpv1beta1.ComputeAddress{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "gs-addr"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Annotations[ProjectIDAnnotation] != "proj-1" {
		t.Errorf("project-id annotation missing: %v", got.Annotations)
	}
	if got.Labels[ResourceTagKey] != ResourceTagValue {
		t.Errorf("managed-by label missing: %v", got.Labels)
	}
	if got.Spec.ResourceID == nil || *got.Spec.ResourceID != spec.ResourceID {
		t.Errorf("ResourceID not set: %+v", got.Spec.ResourceID)
	}
	if got.Spec.NetworkTier == nil || *got.Spec.NetworkTier != "PREMIUM" {
		t.Errorf("NetworkTier not set: %+v", got.Spec.NetworkTier)
	}
}

func TestWaitForAddressReady_StatusGated(t *testing.T) {
	scheme := testScheme(t)
	addr := &gcpv1beta1.ComputeAddress{
		ObjectMeta: metav1.ObjectMeta{Name: "gs-addr", Namespace: "ns1", Generation: 1},
		Status: gcpv1beta1.ComputeAddressStatus{
			ObservedGeneration: int64Ptr2(1),
			Conditions: []gcpv1beta1.Condition{{
				Type: "Ready", Status: "True",
			}},
			ObservedState: &gcpv1beta1.ComputeAddressObservedState{
				Address: strPtr("203.0.113.42"),
			},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(addr).Build()
	ip, ready, err := WaitForAddressReady(context.Background(), cli, types.NamespacedName{Namespace: "ns1", Name: "gs-addr"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !ready || ip != "203.0.113.42" {
		t.Fatalf("want (203.0.113.42, true), got (%q, %v)", ip, ready)
	}

	// Status not Ready -> ip="", ready=false.
	addr2 := addr.DeepCopy()
	addr2.Status.Conditions[0].Status = "False"
	if err := cli.Update(context.Background(), addr2); err != nil {
		t.Fatalf("update: %v", err)
	}
	ip, ready, err = WaitForAddressReady(context.Background(), cli, types.NamespacedName{Namespace: "ns1", Name: "gs-addr"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ready || ip != "" {
		t.Fatalf("want ('', false), got (%q, %v)", ip, ready)
	}
}

func TestOnPodAdded_AddsFinalizerWhenNotRetained(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	plugin := newPlugin(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
	})
	out, perr := plugin.OnPodAdded(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodAdded: %v", perr)
	}
	if !containsFinalizer(out.Finalizers, PodFinalizer) {
		t.Fatalf("Finalizer not added; finalizers=%v", out.Finalizers)
	}
}

func TestOnPodAdded_SkipsFinalizerWhenRetained(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	plugin := newPlugin(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
		confEntry("RetainOnDelete", "true"),
	})
	out, perr := plugin.OnPodAdded(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodAdded: %v", perr)
	}
	if containsFinalizer(out.Finalizers, PodFinalizer) {
		t.Fatalf("Finalizer should not have been added; finalizers=%v", out.Finalizers)
	}
}

func TestEnsureService_ExternalAnnotations(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	plugin := newPlugin(t)
	cfg, err := plugin.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP,7778/TCP"),
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	pod := newGamePod("gs-0", "ns1", nil)
	svc, err := plugin.ensureService(context.Background(), cli, pod, cfg, "gs-0-svc", "gs-0-addr", "", false)
	if err != nil {
		t.Fatalf("ensureService: %v", err)
	}
	if svc.Annotations[L4RBSAnnotationKey] != "enabled" {
		t.Errorf("missing l4-rbs annotation: %v", svc.Annotations)
	}
	if svc.Annotations[GKELoadBalancerIPAnnotationKey] != "gs-0-addr" {
		t.Errorf("missing IP-adoption annotation: %v", svc.Annotations)
	}
	if _, has := svc.Annotations[GKELoadBalancerTypeAnnotationKey]; has {
		t.Errorf("Internal LB annotation should not be set for External Scheme: %v", svc.Annotations)
	}
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		t.Errorf("Service type want LoadBalancer, got %q", svc.Spec.Type)
	}
	if svc.Spec.ExternalTrafficPolicy != corev1.ServiceExternalTrafficPolicyTypeLocal {
		t.Errorf("ExternalTrafficPolicy must be Local for client-IP preservation")
	}
	if len(svc.Spec.Ports) != 2 {
		t.Errorf("want 2 ports, got %d", len(svc.Spec.Ports))
	}
}

func TestEnsureService_InternalAnnotations(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	plugin := newPlugin(t)
	cfg, err := plugin.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Scheme", "Internal"),
		confEntry("AllowGlobalAccess", "true"),
		confEntry("PortProtocols", "7777/UDP"),
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	pod := newGamePod("gs-0", "ns1", nil)
	svc, err := plugin.ensureService(context.Background(), cli, pod, cfg, "gs-0-svc", "gs-0-addr", "", false)
	if err != nil {
		t.Fatalf("ensureService: %v", err)
	}
	if svc.Annotations[GKELoadBalancerTypeAnnotationKey] != "Internal" {
		t.Errorf("missing Internal LB annotation: %v", svc.Annotations)
	}
	if svc.Annotations["networking.gke.io/internal-load-balancer-allow-global-access"] != "true" {
		t.Errorf("missing AllowGlobalAccess annotation: %v", svc.Annotations)
	}
	if _, has := svc.Annotations[L4RBSAnnotationKey]; has {
		t.Errorf("l4-rbs annotation should not be set on Internal Scheme: %v", svc.Annotations)
	}
}

func TestOnPodDeleted_RetainedSkipsCleanup(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
		confEntry("RetainOnDelete", "true"),
	})
	// Pre-create an Address so we can verify it survives.
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := &gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: pod.Namespace}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(addr, pod).Build()
	plugin := newPlugin(t)
	if perr := plugin.OnPodDeleted(cli, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted: %v", perr)
	}
	got := &gcpv1beta1.ComputeAddress{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: addrName}, got); err != nil {
		t.Fatalf("Address should still exist with RetainOnDelete=true, got error: %v", err)
	}
}

func TestOnPodDeleted_DeletesAddressWhenNotRetained(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
	})
	pod.Finalizers = []string{PodFinalizer}
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := &gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: pod.Namespace}}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: pod.Namespace}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(addr, svc, pod).Build()
	plugin := newPlugin(t)
	if perr := plugin.OnPodDeleted(cli, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted: %v", perr)
	}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: addrName}, &gcpv1beta1.ComputeAddress{}); err == nil {
		t.Fatalf("Address should be deleted")
	}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: addrName}, &corev1.Service{}); err == nil {
		t.Fatalf("Service should be deleted")
	}
	// Pod Finalizer should have been removed.
	got := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, got); err == nil {
		if containsFinalizer(got.Finalizers, PodFinalizer) {
			t.Fatalf("Finalizer should have been removed; got %v", got.Finalizers)
		}
	}
}

// --- test helpers ------------------------------------------------------------

func newGamePod(name, ns string, conf []gamekruiseiov1alpha1.NetworkConfParams) *corev1.Pod {
	confJSON := "[]"
	if conf != nil {
		b, _ := encodeConfJSON(conf)
		confJSON = string(b)
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       types.UID("uid-" + name),
			Labels: map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: "gs",
				SvcSelectorKey: name,
			},
			Annotations: map[string]string{
				gamekruiseiov1alpha1.GameServerNetworkType: PassthroughNlbNetwork,
				gamekruiseiov1alpha1.GameServerNetworkConf: confJSON,
			},
		},
	}
}

func encodeConfJSON(conf []gamekruiseiov1alpha1.NetworkConfParams) ([]byte, error) {
	// Inlined to keep the test self-contained; matches NetworkManager's parser.
	type kv = gamekruiseiov1alpha1.NetworkConfParams
	_ = kv{}
	return []byte(toJSON(conf)), nil
}

func toJSON(conf []gamekruiseiov1alpha1.NetworkConfParams) string {
	// Manual minimal JSON to avoid importing encoding/json into the test helper twice.
	var b strings.Builder
	b.WriteString("[")
	for i, c := range conf {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"name":"`)
		b.WriteString(c.Name)
		b.WriteString(`","value":"`)
		b.WriteString(strings.ReplaceAll(c.Value, `"`, `\"`))
		b.WriteString(`"}`)
	}
	b.WriteString("]")
	return b.String()
}

func containsFinalizer(list []string, name string) bool {
	for _, f := range list {
		if f == name {
			return true
		}
	}
	return false
}

func strPtr(s string) *string { return &s }
func int64Ptr2(v int64) *int64 { return &v }

// silence "unused import" if test file is the only consumer of ctrlclient.
var _ = ctrlclient.Object(&corev1.Pod{})
