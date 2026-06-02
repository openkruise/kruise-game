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
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
)

// ---------------------------------------------------------------------------
// kccClient: apiClient-populated branch (both plugins)
// ---------------------------------------------------------------------------

func TestKccClient_PrefersAPIClient(t *testing.T) {
	apiCli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	fallback := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()

	pp := newPlugin(t)
	pp.apiClient = apiCli
	if pp.kccClient(fallback) != apiCli {
		t.Errorf("passthrough kccClient should return apiClient when set")
	}

	gp := newProxyPlugin(t)
	gp.apiClient = apiCli
	if gp.kccClient(fallback) != apiCli {
		t.Errorf("proxy kccClient should return apiClient when set")
	}
}

// ---------------------------------------------------------------------------
// finalizer multi-element removal
// ---------------------------------------------------------------------------

func TestRemoveFinalizer_KeepsOthers(t *testing.T) {
	got := removeFinalizer([]string{"a", PodFinalizer, "b"}, PodFinalizer)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected [a b], got %v", got)
	}
}

// ---------------------------------------------------------------------------
// OnPodAdded parse-error branches
// ---------------------------------------------------------------------------

func TestOnPodAdded_ParseError(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()

	pPod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "bad")})
	if _, perr := newPlugin(t).OnPodAdded(cli, pPod, context.Background()); perr == nil {
		t.Errorf("passthrough OnPodAdded: expected parse error")
	}

	gPod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "bad")})
	if _, perr := newProxyPlugin(t).OnPodAdded(cli, gPod, context.Background()); perr == nil {
		t.Errorf("proxy OnPodAdded: expected parse error")
	}
}

// ---------------------------------------------------------------------------
// EnsureComputeAddress: default EXTERNAL/PREMIUM + create error
// ---------------------------------------------------------------------------

func TestEnsureComputeAddress_DefaultsExternalPremium(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	if _, err := EnsureComputeAddress(context.Background(), cli, AddressSpec{
		Name: "a", Namespace: "ns1", Location: "us-central1", // AddressType + NetworkTier empty
	}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	got := &gcpv1beta1.ComputeAddress{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "a"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.AddressType == nil || *got.Spec.AddressType != "EXTERNAL" {
		t.Errorf("default AddressType should be EXTERNAL, got %+v", got.Spec.AddressType)
	}
	if got.Spec.NetworkTier == nil || *got.Spec.NetworkTier != "PREMIUM" {
		t.Errorf("default NetworkTier should be PREMIUM, got %+v", got.Spec.NetworkTier)
	}
}

func TestEnsureComputeAddress_CreateError(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
			return errors.New("boom-create")
		},
	}).Build()
	if _, err := EnsureComputeAddress(context.Background(), cli, AddressSpec{
		Name: "a", Namespace: "ns1", Location: "us-central1", AddressType: "EXTERNAL",
	}); err == nil {
		t.Fatalf("expected create error to surface")
	}
}

// ---------------------------------------------------------------------------
// WaitForAddressReady: non-NotFound Get error
// ---------------------------------------------------------------------------

func TestWaitForAddressReady_GetError(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return errors.New("boom-get")
		},
	}).Build()
	if _, _, err := WaitForAddressReady(context.Background(), cli, types.NamespacedName{Namespace: "ns1", Name: "x"}); err == nil {
		t.Fatalf("expected get error to surface")
	}
}

// ---------------------------------------------------------------------------
// RemovePodFinalizerPlugin: Update error path
// ---------------------------------------------------------------------------

func TestRemovePodFinalizerPlugin_UpdateError(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns1", Finalizers: []string{PodFinalizer}}}
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(pod).WithInterceptorFuncs(interceptor.Funcs{
		Update: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.UpdateOption) error {
			return errors.New("boom-update")
		},
	}).Build()
	if perr := RemovePodFinalizerPlugin(context.Background(), cli, pod); perr == nil {
		t.Fatalf("expected update error to surface as PluginError")
	}
}

// ---------------------------------------------------------------------------
// passthrough OnPodUpdated: remaining branches
// ---------------------------------------------------------------------------

func TestPassthroughOnPodUpdated_NetworkStatusUnmarshalError(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] = "{not-json"
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	if _, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected unmarshal error to surface")
	}
}

func TestPassthroughOnPodUpdated_GssNotFound_StillReady(t *testing.T) {
	scheme := testScheme(t)
	// No GameServerSet object -> gssErr != nil; owner refs dropped but reconcile
	// proceeds and still reaches Ready when addr + svc are healthy.
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "203.0.113.5")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.5"}}},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, addr, svc).Build()
	out, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "203.0.113.5") {
		t.Errorf("expected Ready with IP even without GSS, got %q", out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus])
	}
}

func TestPassthroughOnPodUpdated_AllowNotReadyContainers(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	// A realistic GSS carries a Network spec; AllowNotReadyContainers reads it.
	gss.Spec.Network = &gamekruiseiov1alpha1.Network{
		NetworkType: PassthroughNlbNetwork,
		NetworkConf: []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry("AllowNotReadyContainers", "true"),
		},
	}
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
		confEntry("AllowNotReadyContainers", "true"),
	})
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "203.0.113.6")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.6"}}},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr, svc).Build()
	if _, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodUpdated with AllowNotReadyContainers: %v", perr)
	}
}

// ---------------------------------------------------------------------------
// proxy OnPodUpdated: remaining branches
// ---------------------------------------------------------------------------

func TestProxyOnPodUpdated_NEGParseError(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	svcName := DeriveServiceName(pod.Name, proxySuffix)
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: svcName, Namespace: "ns1",
		Annotations: map[string]string{NEGStatusAnnotationKey: "{bad-json"},
	}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, svc).Build()
	if _, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected NEG parse error")
	}
}

func TestProxyOnPodUpdated_EnsureError(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	svcName := DeriveServiceName(pod.Name, proxySuffix)
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
		Name: svcName, Namespace: "ns1",
		Annotations: map[string]string{
			NEGStatusAnnotationKey: `{"network_endpoint_groups":{"7777":"neg-7777"},"zones":["us-central1-a"]}`,
		},
	}}
	// Fail creation of the HealthCheck (first KCC object) -> ensureHealthCheck errors.
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, svc).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*gcpv1beta1.ComputeHealthCheck); ok {
				return errors.New("boom-hc")
			}
			return c.Create(ctx, obj, opts...)
		},
	}).Build()
	if _, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected ensureHealthCheck error to surface")
	}
}

func TestProxyOnPodUpdated_GssNotFound_NotReady(t *testing.T) {
	scheme := testScheme(t)
	// No GSS -> gssErr != nil branch; reconcile proceeds, Service created, no
	// neg-status yet -> NotReady (covers the gssErr!=nil owner-skip path).
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	out, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("expected NotReady")
	}
}

// ---------------------------------------------------------------------------
// checkReady: get-error gates for BES / TP / FW / FR
// ---------------------------------------------------------------------------

func TestCheckReady_GetErrorGates(t *testing.T) {
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

	readyHC := func() client.Object {
		return withReady(&gcpv1beta1.ComputeHealthCheck{ObjectMeta: metav1.ObjectMeta{Name: hc, Namespace: "ns1"}})
	}
	readyBES := func() client.Object {
		return withReady(&gcpv1beta1.ComputeBackendService{ObjectMeta: metav1.ObjectMeta{Name: bes, Namespace: "ns1"}})
	}
	readyTP := func() client.Object {
		return withReady(&gcpv1beta1.ComputeTargetTCPProxy{ObjectMeta: metav1.ObjectMeta{Name: tp, Namespace: "ns1"}})
	}
	readyFW := func() client.Object {
		return withReady(&gcpv1beta1.ComputeFirewall{ObjectMeta: metav1.ObjectMeta{Name: fw, Namespace: "ns1"}})
	}

	cases := []struct {
		name string
		objs []client.Object
		want string
	}{
		{"get BES", []client.Object{readyHC()}, "get BES"},
		{"get TP", []client.Object{readyHC(), readyBES()}, "get TargetTcpProxy"},
		{"get FW", []client.Object{readyHC(), readyBES(), readyTP()}, "get FW"},
		{"get FR", []client.Object{readyHC(), readyBES(), readyTP(), readyFW()}, "get FR"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.objs...).Build()
			ready, _, msg := p.checkReady(context.Background(), cli, pod, cfg, hc, bes, tp, addr, fr, fw)
			if ready || !strings.Contains(msg, tc.want) {
				t.Fatalf("want msg %q, got ready=%v msg=%q", tc.want, ready, msg)
			}
		})
	}
}
