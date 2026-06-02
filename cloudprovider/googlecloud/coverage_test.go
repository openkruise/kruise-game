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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
)

// ---------------------------------------------------------------------------
// provider registry (googlecloud.go)
// ---------------------------------------------------------------------------

func TestProvider_NameAndListPlugins(t *testing.T) {
	cp, err := NewGoogleCloudProvider()
	if err != nil {
		t.Fatalf("NewGoogleCloudProvider: %v", err)
	}
	if cp.Name() != GoogleCloud {
		t.Errorf("Name: want %q, got %q", GoogleCloud, cp.Name())
	}
	plugins, err := cp.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	// Both plugins register themselves via init().
	if _, ok := plugins[PassthroughNlbNetwork]; !ok {
		t.Errorf("PassthroughNLB plugin not registered: %v", keys(plugins))
	}
	if _, ok := plugins[GlobalProxyNlbNetwork]; !ok {
		t.Errorf("GlobalProxyNLB plugin not registered: %v", keys(plugins))
	}
}

func TestProvider_ListPluginsNilMap(t *testing.T) {
	p := &Provider{} // plugins == nil
	got, err := p.ListPlugins()
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil map, got (%v, %v)", got, err)
	}
}

func keys(m map[string]cloudprovider.Plugin) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// Alias + Init early branches (both plugins)
// ---------------------------------------------------------------------------

func TestAlias_BothPlugins(t *testing.T) {
	if (&PassthroughNlbPlugin{}).Alias() != AliasPassthroughNlb {
		t.Errorf("passthrough alias wrong")
	}
	if (&GlobalProxyNlbPlugin{}).Alias() != AliasGlobalProxyNlb {
		t.Errorf("proxy alias wrong")
	}
}

// notGCPOptions is a stand-in CloudProviderOptions of the wrong concrete type,
// used to exercise the type-assertion guard in Init.
type notGCPOptions struct{}

func (notGCPOptions) Enabled() bool { return true }
func (notGCPOptions) Valid() bool   { return true }

func TestInit_WrongOptionType(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	if err := (&PassthroughNlbPlugin{}).Init(cli, notGCPOptions{}, context.Background()); err == nil {
		t.Errorf("passthrough Init: expected type-mismatch error")
	}
	if err := (&GlobalProxyNlbPlugin{}).Init(cli, notGCPOptions{}, context.Background()); err == nil {
		t.Errorf("proxy Init: expected type-mismatch error")
	}
}

func TestInit_DisabledIsNoop(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	// PassthroughNLB.Enable defaults to false -> Init returns nil without touching
	// the cluster (never reaches GetConfigOrDie/VerifyKCCInstalled).
	if err := (&PassthroughNlbPlugin{}).Init(cli, provideroptions.GoogleCloudOptions{}, context.Background()); err != nil {
		t.Errorf("passthrough disabled Init: want nil, got %v", err)
	}
	if err := (&GlobalProxyNlbPlugin{}).Init(cli, provideroptions.GoogleCloudOptions{}, context.Background()); err != nil {
		t.Errorf("proxy disabled Init: want nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// prereq.go
// ---------------------------------------------------------------------------

func TestVerifyKCCInstalled_NilConfig(t *testing.T) {
	if err := VerifyKCCInstalled(nil); err == nil {
		t.Fatalf("expected error for nil rest.Config")
	}
}

func TestNotInstalledError_MentionsGcloud(t *testing.T) {
	err := notInstalledError()
	if err == nil || !strings.Contains(err.Error(), "ConfigConnector=ENABLED") {
		t.Fatalf("notInstalledError missing remediation text: %v", err)
	}
}

// ---------------------------------------------------------------------------
// readiness.go
// ---------------------------------------------------------------------------

func TestIsKCCReady_Branches(t *testing.T) {
	ready := []gcpv1beta1.Condition{{Type: "Ready", Status: "True"}}
	if IsKCCReady(ready, 1, 2) {
		t.Errorf("stale observedGeneration should not be Ready")
	}
	if !IsKCCReady(ready, 2, 2) {
		t.Errorf("caught-up Ready=True should be Ready")
	}
	if IsKCCReady([]gcpv1beta1.Condition{{Type: "Ready", Status: "False"}}, 1, 1) {
		t.Errorf("Ready=False should not be Ready")
	}
	if IsKCCReady(nil, 1, 1) {
		t.Errorf("no conditions should not be Ready")
	}
}

func TestCondReasonAndMessage(t *testing.T) {
	conds := []gcpv1beta1.Condition{{Type: "Ready", Status: "False", Reason: "Pending", Message: "creating"}}
	if CondReason(conds) != "Pending" {
		t.Errorf("CondReason: got %q", CondReason(conds))
	}
	if CondMessage(conds) != "creating" {
		t.Errorf("CondMessage: got %q", CondMessage(conds))
	}
	// Absent Ready condition -> empty strings.
	if CondReason(nil) != "" || CondMessage(nil) != "" {
		t.Errorf("expected empty reason/message for nil conditions")
	}
}

func TestDerefInt64(t *testing.T) {
	if derefInt64(nil) != 0 {
		t.Errorf("nil -> 0")
	}
	v := int64(7)
	if derefInt64(&v) != 7 {
		t.Errorf("deref wrong")
	}
}

// ---------------------------------------------------------------------------
// naming.go (truncation + sanitize edge cases)
// ---------------------------------------------------------------------------

func TestNaming_TruncationAndSanitize(t *testing.T) {
	longSuffix := strings.Repeat("abcdefgh", 12) // 96 chars, forces >63 truncation
	if got := DeriveResourceID(types.UID("u1"), longSuffix); len(got) > 63 {
		t.Errorf("DeriveResourceID not capped: %d", len(got))
	}
	if got := DeriveStableResourceID(types.UID("u1"), 0, longSuffix); len(got) > 63 {
		t.Errorf("DeriveStableResourceID not capped: %d", len(got))
	}
	// sanitizeSuffix collapses an all-invalid suffix to the "x" placeholder.
	if got := DeriveResourceID(types.UID("u1"), "***"); !strings.Contains(got, "-x-") {
		t.Errorf("expected placeholder suffix, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// ownership.go
// ---------------------------------------------------------------------------

func TestGssOwnerRef(t *testing.T) {
	gss := gssForTest("gs", "ns1", "u-123", 3)
	gss.APIVersion = "game.kruise.io/v1alpha1"
	gss.Kind = "GameServerSet"
	ref := gssOwnerRef(gss)
	if ref.UID != "u-123" || ref.Name != "gs" || ref.Controller == nil || !*ref.Controller {
		t.Fatalf("owner ref wrong: %+v", ref)
	}
}

func TestShouldReleaseSlot_NilReplicas(t *testing.T) {
	gss := &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "gs", Namespace: "ns1", UID: "u1"},
	} // Spec.Replicas == nil
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(gss).Build()
	if shouldReleaseSlot(context.Background(), cli, "ns1", "gs", "gs-0") {
		t.Fatalf("nil replicas should preserve (return false)")
	}
}

// ---------------------------------------------------------------------------
// address.go (uncovered branches)
// ---------------------------------------------------------------------------

func TestEnsureComputeAddress_LocationRequired(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	if _, err := EnsureComputeAddress(context.Background(), cli, AddressSpec{Name: "a", Namespace: "ns1"}); err == nil {
		t.Fatalf("expected error when Location empty")
	}
}

func TestEnsureComputeAddress_InternalSubnetwork(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	_, err := EnsureComputeAddress(context.Background(), cli, AddressSpec{
		Name:          "ia",
		Namespace:     "ns1",
		Location:      "us-central1",
		AddressType:   "INTERNAL",
		ProjectID:     "proj-1",
		SubnetworkRef: "sub-1",
	})
	if err != nil {
		t.Fatalf("ensure internal: %v", err)
	}
	got := &gcpv1beta1.ComputeAddress{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "ia"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.NetworkTier != nil {
		t.Errorf("INTERNAL address must not set NetworkTier")
	}
	if got.Spec.SubnetworkRef == nil || got.Spec.SubnetworkRef.External == "" {
		t.Errorf("expected subnetwork selfLink, got %+v", got.Spec.SubnetworkRef)
	}
	if got.Spec.NetworkRef != nil {
		t.Errorf("subnetwork-anchored INTERNAL address must not also set NetworkRef")
	}
}

func TestEnsureComputeAddress_InternalNetworkOnly(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	_, err := EnsureComputeAddress(context.Background(), cli, AddressSpec{
		Name:        "ia2",
		Namespace:   "ns1",
		Location:    "us-central1",
		AddressType: "INTERNAL",
		ProjectID:   "proj-1",
		NetworkRef:  "vpc-1",
	})
	if err != nil {
		t.Fatalf("ensure internal net-only: %v", err)
	}
	got := &gcpv1beta1.ComputeAddress{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "ia2"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.NetworkRef == nil || got.Spec.NetworkRef.External == "" {
		t.Errorf("expected network selfLink, got %+v", got.Spec.NetworkRef)
	}
}

func TestNetworkRefOrSelfLink_Fallbacks(t *testing.T) {
	// No projectID -> bare Name form.
	if r := networkRefOrSelfLink("", "vpc", "global/networks"); r.Name != "vpc" || r.External != "" {
		t.Errorf("empty project should fall back to Name: %+v", r)
	}
	// Name already a path -> bare Name form.
	if r := networkRefOrSelfLink("proj", "projects/x/global/networks/vpc", "global/networks"); r.External != "" {
		t.Errorf("path-like name should fall back to Name: %+v", r)
	}
	// Normal -> External selfLink.
	if r := networkRefOrSelfLink("proj", "vpc", "global/networks"); r.External != "projects/proj/global/networks/vpc" {
		t.Errorf("selfLink wrong: %+v", r)
	}
}

func TestSubnetRefOrSelfLink_Fallbacks(t *testing.T) {
	if r := subnetRefOrSelfLink("", "us-central1", "sub"); r.Name != "sub" || r.External != "" {
		t.Errorf("empty project fallback: %+v", r)
	}
	if r := subnetRefOrSelfLink("proj", "", "sub"); r.Name != "sub" {
		t.Errorf("empty location fallback: %+v", r)
	}
	if r := subnetRefOrSelfLink("proj", "us-central1", "sub"); r.External != "projects/proj/regions/us-central1/subnetworks/sub" {
		t.Errorf("subnet selfLink wrong: %+v", r)
	}
}

func TestWaitForAddressReady_NotFoundAndNoState(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	// Absent object -> ("", false, nil).
	ip, ready, err := WaitForAddressReady(context.Background(), cli, types.NamespacedName{Namespace: "ns1", Name: "missing"})
	if ip != "" || ready || err != nil {
		t.Fatalf("missing addr: want ('',false,nil), got (%q,%v,%v)", ip, ready, err)
	}

	// Ready=True but ObservedState nil -> ("", false, nil).
	addr := &gcpv1beta1.ComputeAddress{
		ObjectMeta: metav1.ObjectMeta{Name: "noip", Namespace: "ns1", Generation: 1},
		Status: gcpv1beta1.ComputeAddressStatus{
			ObservedGeneration: int64Ptr2(1),
			Conditions:         []gcpv1beta1.Condition{{Type: "Ready", Status: "True"}},
		},
	}
	cli2 := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(addr).Build()
	ip, ready, err = WaitForAddressReady(context.Background(), cli2, types.NamespacedName{Namespace: "ns1", Name: "noip"})
	if ip != "" || ready || err != nil {
		t.Fatalf("ready-but-no-IP: want ('',false,nil), got (%q,%v,%v)", ip, ready, err)
	}
}

// ---------------------------------------------------------------------------
// finalizer.go (already-present branch)
// ---------------------------------------------------------------------------

func TestEnsurePodFinalizer_AlreadyPresent(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Finalizers: []string{PodFinalizer}}}
	if EnsurePodFinalizer(pod, PodFinalizer) {
		t.Fatalf("expected no mutation when finalizer already present")
	}
}

func TestRemovePodFinalizer_Absent(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns1"}}
	if err := RemovePodFinalizer(context.Background(), cli, pod, PodFinalizer); err != nil {
		t.Fatalf("removing an absent finalizer should be a no-op, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// neg_watcher.go (error branches)
// ---------------------------------------------------------------------------

func TestParseNEGStatusAnnotation_Errors(t *testing.T) {
	bad := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "s", Namespace: "ns1",
		Annotations: map[string]string{NEGStatusAnnotationKey: "{not-json"},
	}}
	if _, err := ParseNEGStatusAnnotation(bad); err == nil {
		t.Errorf("expected JSON parse error")
	}

	badPort := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "s", Namespace: "ns1",
		Annotations: map[string]string{
			NEGStatusAnnotationKey: `{"network_endpoint_groups":{"notaport":"neg"},"zones":["z1"]}`,
		},
	}}
	if _, err := ParseNEGStatusAnnotation(badPort); err == nil {
		t.Errorf("expected invalid-port error")
	}

	// Present groups but no zones -> (nil, nil).
	noZones := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: "s", Namespace: "ns1",
		Annotations: map[string]string{
			NEGStatusAnnotationKey: `{"network_endpoint_groups":{"7777":"neg"},"zones":[]}`,
		},
	}}
	got, err := ParseNEGStatusAnnotation(noZones)
	if err != nil || got != nil {
		t.Errorf("empty zones: want (nil,nil), got (%v,%v)", got, err)
	}
}

// ---------------------------------------------------------------------------
// proxy parseConfig: all override + error branches
// ---------------------------------------------------------------------------

func TestProxyParseConfig_AllFields(t *testing.T) {
	p := newProxyPlugin(t)
	cfg, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("ProjectId", "proj-x"),
		confEntry("Network", "vpc-x"),
		confEntry("Port", "9000"),
		confEntry("ProxyHeader", "PROXY_V1"),
		confEntry("HealthCheckIntervalSec", "10"),
		confEntry("HealthCheckTimeoutSec", "8"),
		confEntry("HealthyThreshold", "3"),
		confEntry("UnhealthyThreshold", "4"),
		confEntry("BalancingMode", "RATE"),
		confEntry("MaxConnectionsPerEndpoint", "500"),
		confEntry("Annotations", "a=b"),
		confEntry("RetainOnDelete", "true"),
	})
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.ProjectID != "proj-x" || cfg.Network != "vpc-x" || cfg.Port != 9000 ||
		cfg.ProxyHeader != "PROXY_V1" || cfg.HealthCheckIntervalSec != 10 ||
		cfg.HealthCheckTimeoutSec != 8 || cfg.HealthyThreshold != 3 ||
		cfg.UnhealthyThreshold != 4 || cfg.BalancingMode != "RATE" ||
		cfg.MaxConnectionsPerEndpoint != 500 || !cfg.RetainOnDelete ||
		cfg.Annotations["a"] != "b" {
		t.Fatalf("overrides not all applied: %+v", cfg)
	}
}

func TestProxyParseConfig_MoreErrors(t *testing.T) {
	cases := []struct {
		name    string
		entries []gamekruiseiov1alpha1.NetworkConfParams
		want    string
	}{
		{"bad port", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "abc")}, "invalid Port"},
		{"bad interval", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "1"), confEntry("HealthCheckIntervalSec", "0")}, "invalid HealthCheckIntervalSec"},
		{"bad timeout", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "1"), confEntry("HealthCheckTimeoutSec", "x")}, "invalid HealthCheckTimeoutSec"},
		{"bad healthy", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "1"), confEntry("HealthyThreshold", "0")}, "invalid HealthyThreshold"},
		{"bad unhealthy", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "1"), confEntry("UnhealthyThreshold", "-1")}, "invalid UnhealthyThreshold"},
		{"bad maxconn", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "1"), confEntry("MaxConnectionsPerEndpoint", "0")}, "invalid MaxConnectionsPerEndpoint"},
		{"bad annotation", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "1"), confEntry("Annotations", "novalue")}, "invalid Annotations entry"},
		{"bad retain", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "1"), confEntry("RetainOnDelete", "maybe")}, "invalid RetainOnDelete"},
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

func TestProxyParseConfig_NetworkRequired(t *testing.T) {
	p := newProxyPlugin(t)
	p.firewallNetworkRef = "" // clear default
	if _, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "1")}); err == nil ||
		!strings.Contains(err.Error(), "network is required") {
		t.Fatalf("expected network-required error, got %v", err)
	}
}

func TestPassthroughParseConfig_MoreErrors(t *testing.T) {
	cases := []struct {
		name    string
		entries []gamekruiseiov1alpha1.NetworkConfParams
		want    string
	}{
		{"bad allowglobal", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "1"), confEntry("AllowGlobalAccess", "x")}, "invalid AllowGlobalAccess"},
		{"bad networktier", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "1"), confEntry("NetworkTier", "GOLD")}, "invalid NetworkTier"},
		{"bad retain", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "1"), confEntry("RetainOnDelete", "x")}, "invalid RetainOnDelete"},
		{"bad port text", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "abc")}, "invalid PortProtocols port"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := newPlugin(t).parseConfig(tc.entries)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPassthroughParseConfig_InternalNeedsSubnetwork(t *testing.T) {
	p := newPlugin(t)
	p.defaultSubnetwork = "" // network present, subnetwork cleared
	_, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Scheme", "Internal"),
		confEntry("PortProtocols", "7777"),
	})
	if err == nil || !strings.Contains(err.Error(), "subnetwork is required") {
		t.Fatalf("expected subnetwork-required error, got %v", err)
	}
}

func TestPassthroughParseConfig_RegionRequired(t *testing.T) {
	p := newPlugin(t)
	p.defaultRegion = ""
	_, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777")})
	if err == nil || !strings.Contains(err.Error(), "region is required") {
		t.Fatalf("expected region-required error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// checkReady gate-by-gate (proxy)
// ---------------------------------------------------------------------------

func TestCheckReady_EachGate(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", nil)
	uid := pod.UID
	hc := DeriveResourceID(uid, "hc")
	bes := DeriveResourceID(uid, "bes")
	tp := DeriveResourceID(uid, "tp")
	addr := DeriveResourceID(uid, "addr")
	fr := DeriveResourceID(uid, "fr")
	fw := DeriveResourceID(uid, "fw")
	p := newProxyPlugin(t)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})

	// All objects present and ready EXCEPT one, walked across the chain. The
	// readiness gates short-circuit in order HC->BES->TP->FW->FR->Addr.
	type step struct {
		omitReady string // which kind is present-but-not-ready
		wantMsg   string
	}
	build := func(notReady string) []client.Object {
		mk := func(kind string, o client.Object) client.Object {
			if kind == notReady {
				return o // present, no ready status
			}
			return withReady(o)
		}
		return []client.Object{
			mk("hc", &gcpv1beta1.ComputeHealthCheck{ObjectMeta: metav1.ObjectMeta{Name: hc, Namespace: "ns1"}}),
			mk("bes", &gcpv1beta1.ComputeBackendService{ObjectMeta: metav1.ObjectMeta{Name: bes, Namespace: "ns1"}}),
			mk("tp", &gcpv1beta1.ComputeTargetTCPProxy{ObjectMeta: metav1.ObjectMeta{Name: tp, Namespace: "ns1"}}),
			mk("fw", &gcpv1beta1.ComputeFirewall{ObjectMeta: metav1.ObjectMeta{Name: fw, Namespace: "ns1"}}),
			mk("fr", &gcpv1beta1.ComputeForwardingRule{ObjectMeta: metav1.ObjectMeta{Name: fr, Namespace: "ns1"}}),
		}
	}
	for _, s := range []step{
		{"hc", "HealthCheck not Ready"},
		{"bes", "BackendService not Ready"},
		{"tp", "TargetTCPProxy not Ready"},
		{"fw", "Firewall not Ready"},
		{"fr", "ForwardingRule not Ready"},
	} {
		objs := build(s.omitReady)
		// Address ready so the gate that fails is the intended one.
		objs = append(objs, withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addr, Namespace: "ns1"}}, "1.2.3.4"))
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		ready, _, msg := p.checkReady(context.Background(), cli, pod, cfg, hc, bes, tp, addr, fr, fw)
		if ready || !strings.Contains(msg, s.wantMsg) {
			t.Errorf("omit %q: want not-ready msg %q, got ready=%v msg=%q", s.omitReady, s.wantMsg, ready, msg)
		}
	}

	// Missing HC object entirely -> "get HC" error path.
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	if ready, _, msg := p.checkReady(context.Background(), cli, pod, cfg, hc, bes, tp, addr, fr, fw); ready || !strings.Contains(msg, "get HC") {
		t.Errorf("missing HC: want get-HC error, got ready=%v msg=%q", ready, msg)
	}

	// Address present+ready but IP empty -> "Address Ready but IP empty".
	// withReadyAddress(..., "") yields a Ready address whose ObservedState.Address
	// is a non-nil pointer to the empty string, the only way to reach that gate.
	objs := build("")
	emptyIP := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addr, Namespace: "ns1"}}, "")
	objs = append(objs, emptyIP)
	cli2 := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	if ready, _, msg := p.checkReady(context.Background(), cli2, pod, cfg, hc, bes, tp, addr, fr, fw); ready || !strings.Contains(msg, "IP empty") {
		t.Errorf("empty IP: want IP-empty msg, got ready=%v msg=%q", ready, msg)
	}
}

// ---------------------------------------------------------------------------
// OnPodUpdated: not-ready + fully-ready reconcile paths (both plugins)
// ---------------------------------------------------------------------------

// podWithStatus returns a game Pod carrying a NetworkStatus annotation (so
// GetNetworkStatus returns non-nil) and a PodIP, owned by GSS "gs".
func podWithStatus(name, ns string, conf []gamekruiseiov1alpha1.NetworkConfParams) *corev1.Pod {
	pod := newGamePod(name, ns, conf)
	pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] = `{"currentNetworkState":"NotReady"}`
	pod.Status.PodIP = "10.0.0.5"
	return pod
}

func TestPassthroughOnPodUpdated_NilNetworkStatus(t *testing.T) {
	// No NetworkStatus annotation -> GetNetworkStatus returns nil -> NotReady set.
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	out, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("expected NotReady status, got %q", out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus])
	}
}

func TestPassthroughOnPodUpdated_ParseError(t *testing.T) {
	scheme := testScheme(t)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "bad")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	if _, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected parse error")
	}
}

func TestPassthroughOnPodUpdated_AddressNotReady(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).Build()
	// EnsureComputeAddress creates the addr (no status) -> WaitForAddressReady
	// reports not-ready -> overall NotReady, no panic.
	out, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("expected NotReady while address provisioning")
	}
}

func TestPassthroughOnPodUpdated_FullyReady(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP,7778/TCP")})

	// Pre-seed the ComputeAddress (Ready + IP) and Service (LB ingress assigned)
	// at the names the reconcile derives, so CreateOrUpdate adopts them and the
	// reconcile walks all the way to NetworkReady.
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "203.0.113.9")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.9"}}},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr, svc).Build()

	out, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	status := out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]
	if !strings.Contains(status, "Ready") || strings.Contains(status, "NotReady") {
		t.Fatalf("expected NetworkReady, got %q", status)
	}
	if !strings.Contains(status, "203.0.113.9") {
		t.Errorf("expected external IP in status, got %q", status)
	}
}

func TestPassthroughOnPodUpdated_Disabled(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	pod.Labels[gamekruiseiov1alpha1.GameServerNetworkDisabled] = "true"
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "203.0.113.9")
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr).Build()
	out, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated disabled: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("disabled network should report NotReady")
	}
	// Service must be ClusterIP (LB torn down) when disabled.
	got := &corev1.Service{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: addrName}, got); err != nil {
		t.Fatalf("get svc: %v", err)
	}
	if got.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Errorf("disabled -> ClusterIP, got %q", got.Spec.Type)
	}
}

func TestPassthroughOnPodUpdated_GssDeletingFlagsPod(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	now := metav1.Now()
	gss.DeletionTimestamp = &now
	gss.Finalizers = []string{"x"}
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).Build()
	out, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if out.Labels[GssDeletingLabelKey] != "true" {
		t.Errorf("expected gss-deleting label set on pod")
	}
}

func TestProxyOnPodUpdated_NilNetworkStatus(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	out, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("expected NotReady")
	}
}

func TestProxyOnPodUpdated_WaitingForNEG(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).Build()
	// Service gets created but neg-status annotation is absent -> NotReady gate (1).
	out, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("expected NotReady while waiting for NEG")
	}
}

func TestProxyOnPodUpdated_FullyReady(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	ordinal := 0

	svcName := DeriveServiceName(pod.Name, proxySuffix)
	// Pre-seed Service with the neg-status annotation GKE would publish.
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: svcName, Namespace: "ns1",
			Annotations: map[string]string{
				NEGStatusAnnotationKey: `{"network_endpoint_groups":{"7777":"neg-7777"},"zones":["us-central1-a"]}`,
			},
		},
	}
	// Pre-seed all six KCC objects Ready at their stable names.
	hc := DeriveStableResourceID(gss.UID, ordinal, "gpnlb-hc")
	bes := DeriveStableResourceID(gss.UID, ordinal, "gpnlb-bes")
	tp := DeriveStableResourceID(gss.UID, ordinal, "gpnlb-tp")
	addr := DeriveStableResourceID(gss.UID, ordinal, "gpnlb-addr")
	fr := DeriveStableResourceID(gss.UID, ordinal, "gpnlb-fr")
	fw := DeriveStableResourceID(gss.UID, ordinal, "gpnlb-fw")
	objs := []client.Object{
		gss, pod, svc,
		withReady(&gcpv1beta1.ComputeHealthCheck{ObjectMeta: metav1.ObjectMeta{Name: hc, Namespace: "ns1"}}),
		withReady(&gcpv1beta1.ComputeBackendService{ObjectMeta: metav1.ObjectMeta{Name: bes, Namespace: "ns1"}}),
		withReady(&gcpv1beta1.ComputeTargetTCPProxy{ObjectMeta: metav1.ObjectMeta{Name: tp, Namespace: "ns1"}}),
		withReady(&gcpv1beta1.ComputeFirewall{ObjectMeta: metav1.ObjectMeta{Name: fw, Namespace: "ns1"}}),
		withReady(&gcpv1beta1.ComputeForwardingRule{ObjectMeta: metav1.ObjectMeta{Name: fr, Namespace: "ns1"}}),
		withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addr, Namespace: "ns1"}}, "34.120.0.7"),
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

	out, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	status := out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus]
	if !strings.Contains(status, "Ready") || strings.Contains(status, "NotReady") {
		t.Fatalf("expected NetworkReady, got %q", status)
	}
	if !strings.Contains(status, "34.120.0.7") {
		t.Errorf("expected anycast IP in status, got %q", status)
	}
}

func TestProxyOnPodDeleted_PreservesOnRecreate(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	pod.Finalizers = []string{PodFinalizer}
	addrName := DeriveStableResourceID(gss.UID, 0, "gpnlb-addr")
	addr := &gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr).Build()
	if perr := newProxyPlugin(t).OnPodDeleted(cli, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted: %v", perr)
	}
	// In-range ordinal -> address preserved.
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: addrName}, &gcpv1beta1.ComputeAddress{}); err != nil {
		t.Errorf("address should be preserved on recreate, got %v", err)
	}
}

func TestProxyOnPodDeleted_ParseErrorStillRemovesFinalizer(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "bad")})
	pod.Finalizers = []string{PodFinalizer}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	if perr := newProxyPlugin(t).OnPodDeleted(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected parse error surfaced")
	}
	got := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "gs-0"}, got); err == nil {
		if containsFinalizer(got.Finalizers, PodFinalizer) {
			t.Errorf("finalizer should be removed even on parse error")
		}
	}
}

func TestPassthroughOnPodDeleted_ParseErrorStillRemovesFinalizer(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "bad")})
	pod.Finalizers = []string{PodFinalizer}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	if perr := newPlugin(t).OnPodDeleted(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected parse error surfaced")
	}
	got := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "gs-0"}, got); err == nil {
		if containsFinalizer(got.Finalizers, PodFinalizer) {
			t.Errorf("finalizer should be removed even on parse error")
		}
	}
}

// OnPodAdded for proxy (passthrough OnPodAdded is already covered elsewhere).
func TestProxyOnPodAdded_AddsFinalizer(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	out, perr := newProxyPlugin(t).OnPodAdded(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodAdded: %v", perr)
	}
	if !containsFinalizer(out.Finalizers, PodFinalizer) {
		t.Errorf("expected finalizer added")
	}
}
