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
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
)

func newProxyPlugin(t *testing.T) *GlobalProxyNlbPlugin {
	t.Helper()
	return &GlobalProxyNlbPlugin{
		projectID:             "demo-project",
		defaultNetwork:        "default",
		firewallNetworkRef:    "default",
		retainOnDeleteDefault: false,
	}
}

func TestProxyParseConfig_Defaults(t *testing.T) {
	p := newProxyPlugin(t)
	cfg, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 7777 || cfg.ProxyHeader != ProxyHeaderNone {
		t.Errorf("defaults wrong: %+v", cfg)
	}
	if cfg.BalancingMode != "CONNECTION" || cfg.MaxConnectionsPerEndpoint != 1000 {
		t.Errorf("balancing defaults wrong: %+v", cfg)
	}
	if cfg.RetainOnDelete != false {
		t.Errorf("RetainOnDelete default: want false, got true")
	}
}

func TestProxyParseConfig_Errors(t *testing.T) {
	cases := []struct {
		name    string
		entries []gamekruiseiov1alpha1.NetworkConfParams
		want    string
	}{
		{"missing Port", nil, "port is required"},
		{"invalid Port out of range", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("Port", "99999"),
		}, "invalid Port 99999"},
		{"timeout greater than interval", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("Port", "7777"),
			confEntry("HealthCheckIntervalSec", "2"),
			confEntry("HealthCheckTimeoutSec", "5"),
		}, "HealthCheckTimeoutSec"},
		{"invalid ProxyHeader", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("Port", "7777"),
			confEntry("ProxyHeader", "PROXY_V2"),
		}, "invalid ProxyHeader"},
		{"invalid BalancingMode", []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("Port", "7777"),
			confEntry("BalancingMode", "NOPE"),
		}, "invalid BalancingMode"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			p := newProxyPlugin(t)
			_, err := p.parseConfig(tc.entries)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestEnsureService_NEGAnnotationShape(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := newProxyPlugin(t)
	cfg, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"),
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	pod := newGamePod("gs-0", "ns1", nil)
	svc, err := p.ensureService(context.Background(), cli, pod, cfg, "gs-0-svc", pod.UID, 0, nil)
	if err != nil {
		t.Fatalf("ensureService: %v", err)
	}
	raw, ok := svc.Annotations[NEGAnnotationKey]
	if !ok {
		t.Fatalf("missing NEG annotation; have %v", svc.Annotations)
	}
	var ann map[string]map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &ann); err != nil {
		t.Fatalf("NEG annotation parse: %v / %q", err, raw)
	}
	ports, ok := ann["exposed_ports"]
	if !ok || ports["7777"] == nil {
		t.Fatalf("expected exposed_ports[7777], got %v", ann)
	}
	if ports["7777"]["name"] == "" {
		t.Fatalf("expected NEG name to be set, got %v", ports["7777"])
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("expected ClusterIP, got %q", svc.Spec.Type)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 7777 {
		t.Errorf("unexpected ports: %+v", svc.Spec.Ports)
	}
}

func TestEnsureHealthCheck_GlobalUSEServingPort(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := newProxyPlugin(t)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	pod := newGamePod("gs-0", "ns1", nil)
	if err := p.ensureHealthCheck(context.Background(), cli, pod, cfg, "hc-0", nil); err != nil {
		t.Fatalf("ensureHealthCheck: %v", err)
	}
	hc := &gcpv1beta1.ComputeHealthCheck{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "hc-0"}, hc); err != nil {
		t.Fatalf("get: %v", err)
	}
	if hc.Spec.Location != "global" {
		t.Errorf("expected global location, got %q", hc.Spec.Location)
	}
	if hc.Spec.TCPHealthCheck == nil || hc.Spec.TCPHealthCheck.PortSpecification == nil ||
		*hc.Spec.TCPHealthCheck.PortSpecification != "USE_SERVING_PORT" {
		t.Errorf("expected TCP USE_SERVING_PORT, got %+v", hc.Spec.TCPHealthCheck)
	}
}

func TestEnsureBackendService_NEGSorted(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := newProxyPlugin(t)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	pod := newGamePod("gs-0", "ns1", nil)
	// Pass NEGs in non-alphabetical zone order; expect sorted output.
	negs := []NEGRef{
		{Name: "neg-x", Zone: "us-central1-c"},
		{Name: "neg-x", Zone: "us-central1-a"},
		{Name: "neg-x", Zone: "us-central1-b"},
	}
	if err := p.ensureBackendService(context.Background(), cli, pod, cfg, "bes-0", "hc-0", negs, nil); err != nil {
		t.Fatalf("ensureBackendService: %v", err)
	}
	bes := &gcpv1beta1.ComputeBackendService{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "bes-0"}, bes); err != nil {
		t.Fatalf("get: %v", err)
	}
	if bes.Spec.Location != "global" || *bes.Spec.LoadBalancingScheme != "EXTERNAL_MANAGED" {
		t.Errorf("scheme/location wrong: %+v", bes.Spec)
	}
	if len(bes.Spec.Backend) != 3 {
		t.Fatalf("expected 3 backends, got %d", len(bes.Spec.Backend))
	}
	prev := ""
	for i, b := range bes.Spec.Backend {
		if b.Group.NetworkEndpointGroupRef == nil || b.Group.NetworkEndpointGroupRef.External == "" {
			t.Fatalf("backend %d missing NEG external ref", i)
		}
		if prev != "" && b.Group.NetworkEndpointGroupRef.External < prev {
			t.Errorf("backends not sorted by zone-encoded selflink: %q < %q", b.Group.NetworkEndpointGroupRef.External, prev)
		}
		prev = b.Group.NetworkEndpointGroupRef.External
	}
}

func TestEnsureTargetTCPProxy_ProxyHeader(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := newProxyPlugin(t)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"),
		confEntry("ProxyHeader", "PROXY_V1"),
	})
	pod := newGamePod("gs-0", "ns1", nil)
	if err := p.ensureTargetTCPProxy(context.Background(), cli, pod, cfg, "tp-0", "bes-0", nil); err != nil {
		t.Fatalf("ensureTargetTCPProxy: %v", err)
	}
	tp := &gcpv1beta1.ComputeTargetTCPProxy{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "tp-0"}, tp); err != nil {
		t.Fatalf("get: %v", err)
	}
	if tp.Spec.ProxyHeader == nil || *tp.Spec.ProxyHeader != "PROXY_V1" {
		t.Errorf("expected proxyHeader=PROXY_V1, got %+v", tp.Spec.ProxyHeader)
	}
	if tp.Spec.BackendServiceRef.Name != "bes-0" {
		t.Errorf("backendServiceRef wrong: %+v", tp.Spec.BackendServiceRef)
	}
}

func TestEnsureForwardingRule_SinglePortPremium(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := newProxyPlugin(t)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	pod := newGamePod("gs-0", "ns1", nil)
	if err := p.ensureForwardingRule(context.Background(), cli, pod, cfg, "fr-0", "tp-0", "addr-0", nil); err != nil {
		t.Fatalf("ensureForwardingRule: %v", err)
	}
	fr := &gcpv1beta1.ComputeForwardingRule{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "fr-0"}, fr); err != nil {
		t.Fatalf("get: %v", err)
	}
	if fr.Spec.Location != "global" || *fr.Spec.LoadBalancingScheme != "EXTERNAL_MANAGED" ||
		*fr.Spec.NetworkTier != "PREMIUM" || *fr.Spec.IPProtocol != "TCP" {
		t.Errorf("FR fields wrong: %+v", fr.Spec)
	}
	if fr.Spec.PortRange == nil || *fr.Spec.PortRange != "7777" {
		t.Errorf("expected single-port portRange '7777', got %+v", fr.Spec.PortRange)
	}
	if fr.Spec.Target == nil || fr.Spec.Target.TargetTCPProxyRef == nil || fr.Spec.Target.TargetTCPProxyRef.Name != "tp-0" {
		t.Errorf("target ref wrong: %+v", fr.Spec.Target)
	}
	if fr.Spec.IPAddress == nil || fr.Spec.IPAddress.AddressRef == nil || fr.Spec.IPAddress.AddressRef.Name != "addr-0" {
		t.Errorf("address ref wrong: %+v", fr.Spec.IPAddress)
	}
}

func TestEnsureFirewall_HCRangesAndPort(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := newProxyPlugin(t)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	pod := newGamePod("gs-0", "ns1", nil)
	if err := p.ensureFirewall(context.Background(), cli, pod, cfg, "fw-0", nil); err != nil {
		t.Fatalf("ensureFirewall: %v", err)
	}
	fw := &gcpv1beta1.ComputeFirewall{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "fw-0"}, fw); err != nil {
		t.Fatalf("get: %v", err)
	}
	if *fw.Spec.Direction != "INGRESS" {
		t.Errorf("expected INGRESS, got %v", fw.Spec.Direction)
	}
	srcOK := map[string]bool{"35.191.0.0/16": false, "130.211.0.0/22": false}
	for _, s := range fw.Spec.SourceRanges {
		if _, want := srcOK[s]; want {
			srcOK[s] = true
		}
	}
	for s, present := range srcOK {
		if !present {
			t.Errorf("missing source range %q (have %v)", s, fw.Spec.SourceRanges)
		}
	}
	if len(fw.Spec.Allowed) != 1 || fw.Spec.Allowed[0].Protocol != "tcp" ||
		len(fw.Spec.Allowed[0].Ports) != 1 || fw.Spec.Allowed[0].Ports[0] != "7777" {
		t.Errorf("allowed wrong: %+v", fw.Spec.Allowed)
	}
	if fw.Spec.NetworkRef.External != "projects/demo-project/global/networks/default" {
		t.Errorf("networkRef wrong (want External selfLink): %+v", fw.Spec.NetworkRef)
	}
}

func TestProxyOnPodDeleted_DeletesEverythingInOrder(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"),
	})
	pod.Finalizers = []string{PodFinalizer}
	uid := pod.UID
	hcName := DeriveResourceID(uid, "gpnlb-hc")
	besName := DeriveResourceID(uid, "gpnlb-bes")
	tpName := DeriveResourceID(uid, "gpnlb-tp")
	addrName := DeriveResourceID(uid, "gpnlb-addr")
	frName := DeriveResourceID(uid, "gpnlb-fr")
	fwName := DeriveResourceID(uid, "gpnlb-fw")
	svcName := DeriveServiceName(pod.Name, proxySuffix)
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		pod,
		&gcpv1beta1.ComputeForwardingRule{ObjectMeta: metav1.ObjectMeta{Name: frName, Namespace: "ns1"}},
		&gcpv1beta1.ComputeTargetTCPProxy{ObjectMeta: metav1.ObjectMeta{Name: tpName, Namespace: "ns1"}},
		&gcpv1beta1.ComputeBackendService{ObjectMeta: metav1.ObjectMeta{Name: besName, Namespace: "ns1"}},
		&gcpv1beta1.ComputeHealthCheck{ObjectMeta: metav1.ObjectMeta{Name: hcName, Namespace: "ns1"}},
		&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}},
		&gcpv1beta1.ComputeFirewall{ObjectMeta: metav1.ObjectMeta{Name: fwName, Namespace: "ns1"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: "ns1"}},
	).Build()
	p := newProxyPlugin(t)
	if perr := p.OnPodDeleted(cli, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted: %v", perr)
	}
	// Each KCC kind should be gone.
	for _, key := range []types.NamespacedName{
		{Namespace: "ns1", Name: frName},
		{Namespace: "ns1", Name: tpName},
		{Namespace: "ns1", Name: besName},
		{Namespace: "ns1", Name: hcName},
		{Namespace: "ns1", Name: addrName},
		{Namespace: "ns1", Name: fwName},
		{Namespace: "ns1", Name: svcName},
	} {
		obj := &corev1.Service{}
		err := cli.Get(context.Background(), key, obj)
		if err == nil {
			// Service exists — for others, get with respective type would also fail.
			t.Errorf("%s still exists after delete", key)
		}
	}
	// Finalizer removed.
	got := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "gs-0"}, got); err == nil {
		if containsFinalizer(got.Finalizers, PodFinalizer) {
			t.Errorf("Finalizer should have been removed, got %v", got.Finalizers)
		}
	}
}

func TestProxyOnPodDeleted_RetainedSkipsCleanup(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"),
		confEntry("RetainOnDelete", "true"),
	})
	addrName := DeriveResourceID(pod.UID, "gpnlb-addr")
	addr := &gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, addr).Build()
	p := newProxyPlugin(t)
	if perr := p.OnPodDeleted(cli, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted: %v", perr)
	}
	got := &gcpv1beta1.ComputeAddress{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: addrName}, got); err != nil {
		t.Errorf("Address should survive RetainOnDelete=true, got %v", err)
	}
}

func TestParseNEGStatusAnnotation_ZoneFanout(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gs-0",
			Namespace: "ns1",
			Annotations: map[string]string{
				NEGStatusAnnotationKey: `{"network_endpoint_groups":{"7777":"neg-7777"},"zones":["us-central1-c","us-central1-a","us-central1-b"]}`,
			},
		},
	}
	got, err := ParseNEGStatusAnnotation(svc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	refs := got[7777]
	if len(refs) != 3 {
		t.Fatalf("want 3 zone refs, got %d", len(refs))
	}
	for i, z := range []string{"us-central1-a", "us-central1-b", "us-central1-c"} {
		if refs[i].Zone != z {
			t.Errorf("zone[%d]: want %q, got %q", i, z, refs[i].Zone)
		}
		if refs[i].Name != "neg-7777" {
			t.Errorf("name[%d]: want %q, got %q", i, "neg-7777", refs[i].Name)
		}
		if refs[i].SelfLink("my-proj") != "projects/my-proj/zones/"+z+"/networkEndpointGroups/neg-7777" {
			t.Errorf("selflink[%d]: %q", i, refs[i].SelfLink("my-proj"))
		}
	}
}

func TestParseNEGStatusAnnotation_Empty(t *testing.T) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "x", Namespace: "y"}}
	got, err := ParseNEGStatusAnnotation(svc)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", got, err)
	}
}

func TestCheckReady_AllStagesGate(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", nil)
	uid := pod.UID
	hcName := DeriveResourceID(uid, "gpnlb-hc")
	besName := DeriveResourceID(uid, "gpnlb-bes")
	tpName := DeriveResourceID(uid, "gpnlb-tp")
	addrName := DeriveResourceID(uid, "gpnlb-addr")
	frName := DeriveResourceID(uid, "gpnlb-fr")
	fwName := DeriveResourceID(uid, "gpnlb-fw")

	objs := []client.Object{
		withReady(&gcpv1beta1.ComputeHealthCheck{ObjectMeta: metav1.ObjectMeta{Name: hcName, Namespace: "ns1"}}),
		withReady(&gcpv1beta1.ComputeBackendService{ObjectMeta: metav1.ObjectMeta{Name: besName, Namespace: "ns1"}}),
		withReady(&gcpv1beta1.ComputeTargetTCPProxy{ObjectMeta: metav1.ObjectMeta{Name: tpName, Namespace: "ns1"}}),
		withReady(&gcpv1beta1.ComputeFirewall{ObjectMeta: metav1.ObjectMeta{Name: fwName, Namespace: "ns1"}}),
		withReady(&gcpv1beta1.ComputeForwardingRule{ObjectMeta: metav1.ObjectMeta{Name: frName, Namespace: "ns1"}}),
		withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "34.120.0.1"),
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	p := newProxyPlugin(t)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	ready, ip, msg := p.checkReady(context.Background(), cli, pod, cfg, hcName, besName, tpName, addrName, frName, fwName)
	if !ready {
		t.Fatalf("expected ready, got msg=%q", msg)
	}
	if ip != "34.120.0.1" {
		t.Errorf("expected IP, got %q", ip)
	}

	// Flip address to NotReady -> overall NotReady.
	addr := &gcpv1beta1.ComputeAddress{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: addrName}, addr); err != nil {
		t.Fatalf("get addr: %v", err)
	}
	addr.Status.Conditions[0].Status = "False"
	if err := cli.Update(context.Background(), addr); err != nil {
		t.Fatalf("update addr: %v", err)
	}
	ready, _, msg = p.checkReady(context.Background(), cli, pod, cfg, hcName, besName, tpName, addrName, frName, fwName)
	if ready {
		t.Fatalf("expected NotReady when address not ready; msg=%q", msg)
	}
}

// withReady stamps generation/observedGeneration/Ready=True on any KCC object
// in our scheme so checkReady sees it as healthy.
func withReady(obj client.Object) client.Object {
	switch o := obj.(type) {
	case *gcpv1beta1.ComputeHealthCheck:
		o.Generation = 1
		o.Status.ObservedGeneration = int64Ptr2(1)
		o.Status.Conditions = []gcpv1beta1.Condition{{Type: "Ready", Status: "True"}}
	case *gcpv1beta1.ComputeBackendService:
		o.Generation = 1
		o.Status.ObservedGeneration = int64Ptr2(1)
		o.Status.Conditions = []gcpv1beta1.Condition{{Type: "Ready", Status: "True"}}
	case *gcpv1beta1.ComputeTargetTCPProxy:
		o.Generation = 1
		o.Status.ObservedGeneration = int64Ptr2(1)
		o.Status.Conditions = []gcpv1beta1.Condition{{Type: "Ready", Status: "True"}}
	case *gcpv1beta1.ComputeFirewall:
		o.Generation = 1
		o.Status.ObservedGeneration = int64Ptr2(1)
		o.Status.Conditions = []gcpv1beta1.Condition{{Type: "Ready", Status: "True"}}
	case *gcpv1beta1.ComputeForwardingRule:
		o.Generation = 1
		o.Status.ObservedGeneration = int64Ptr2(1)
		o.Status.Conditions = []gcpv1beta1.Condition{{Type: "Ready", Status: "True"}}
	}
	return obj
}

func withReadyAddress(o *gcpv1beta1.ComputeAddress, ip string) client.Object {
	o.Generation = 1
	o.Status.ObservedGeneration = int64Ptr2(1)
	o.Status.Conditions = []gcpv1beta1.Condition{{Type: "Ready", Status: "True"}}
	o.Status.ObservedState = &gcpv1beta1.ComputeAddressObservedState{Address: strPtr(ip)}
	return o
}
