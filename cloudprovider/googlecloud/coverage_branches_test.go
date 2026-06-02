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

// nonGamePod is a Pod with no game.kruise.io network-type annotation, so
// NewNetworkManager returns nil and the On* hooks short-circuit.
func nonGamePod() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "plain", Namespace: "ns1"}}
}

func TestOnHooks_NilNetworkManager(t *testing.T) {
	cli := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	ctx := context.Background()

	pp := newPlugin(t)
	if _, perr := pp.OnPodAdded(cli, nonGamePod(), ctx); perr != nil {
		t.Errorf("passthrough OnPodAdded nil-nm: %v", perr)
	}
	if _, perr := pp.OnPodUpdated(cli, nonGamePod(), ctx); perr != nil {
		t.Errorf("passthrough OnPodUpdated nil-nm: %v", perr)
	}
	if perr := pp.OnPodDeleted(cli, nonGamePod(), ctx); perr != nil {
		t.Errorf("passthrough OnPodDeleted nil-nm: %v", perr)
	}

	gp := newProxyPlugin(t)
	if _, perr := gp.OnPodAdded(cli, nonGamePod(), ctx); perr != nil {
		t.Errorf("proxy OnPodAdded nil-nm: %v", perr)
	}
	if _, perr := gp.OnPodUpdated(cli, nonGamePod(), ctx); perr != nil {
		t.Errorf("proxy OnPodUpdated nil-nm: %v", perr)
	}
	if perr := gp.OnPodDeleted(cli, nonGamePod(), ctx); perr != nil {
		t.Errorf("proxy OnPodDeleted nil-nm: %v", perr)
	}
}

// ---------------------------------------------------------------------------
// passthrough OnPodUpdated: Internal scheme + ingress-not-assigned + errors
// ---------------------------------------------------------------------------

func TestPassthroughOnPodUpdated_InternalReady(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Scheme", "Internal"),
		confEntry("PortProtocols", "7777/UDP"),
	})
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "10.20.30.40")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "10.20.30.40"}}},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr, svc).Build()
	out, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated internal: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "10.20.30.40") {
		t.Errorf("expected internal LB IP in status")
	}
	// Internal LB adopts the IP via spec.loadBalancerIP.
	got := &corev1.Service{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: addrName}, got); err != nil {
		t.Fatalf("get svc: %v", err)
	}
	if got.Spec.LoadBalancerIP != "10.20.30.40" {
		t.Errorf("internal LB should set spec.loadBalancerIP, got %q", got.Spec.LoadBalancerIP)
	}
}

func TestPassthroughOnPodUpdated_IngressNotAssigned(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "203.0.113.7")
	// Service exists but no LoadBalancer ingress assigned yet -> NotReady gate.
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr, svc).Build()
	out, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("expected NotReady while LB ingress unassigned")
	}
}

func TestPassthroughOnPodDeleted_DeleteErrors(t *testing.T) {
	scheme := testScheme(t)
	mk := func() (*corev1.Pod, *gcpv1beta1.ComputeAddress, *corev1.Service) {
		pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
		pod.Finalizers = []string{PodFinalizer}
		addrName := DeriveServiceName(pod.Name, passthroughSuffix)
		return pod,
			&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}},
			&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}
	}

	// Service delete fails.
	pod, addr, svc := mk()
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, addr, svc).WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
			if _, ok := obj.(*corev1.Service); ok {
				return errors.New("boom-svc")
			}
			return nil
		},
	}).Build()
	if perr := newPlugin(t).OnPodDeleted(cli, pod, context.Background()); perr == nil {
		t.Errorf("expected Service delete error to surface")
	}

	// Address delete fails (Service delete succeeds).
	pod, addr, svc = mk()
	cli = fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, addr, svc).WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			if _, ok := obj.(*gcpv1beta1.ComputeAddress); ok {
				return errors.New("boom-addr")
			}
			return c.Delete(ctx, obj, opts...)
		},
	}).Build()
	if perr := newPlugin(t).OnPodDeleted(cli, pod, context.Background()); perr == nil {
		t.Errorf("expected Address delete error to surface")
	}
}

// ---------------------------------------------------------------------------
// passthrough ensureService variants
// ---------------------------------------------------------------------------

func TestPassthroughEnsureService_DisabledAndInternalIP(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := newPlugin(t)
	pod := newGamePod("gs-0", "ns1", nil)

	// disabled=true -> ClusterIP, no loadBalancerIP.
	extCfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
		confEntry("Annotations", "k=v"),
	})
	svc, err := p.ensureService(context.Background(), cli, pod, extCfg, "svc-dis", "addr", "", true, nil)
	if err != nil {
		t.Fatalf("ensureService disabled: %v", err)
	}
	if svc.Spec.Type != corev1.ServiceTypeClusterIP || svc.Spec.LoadBalancerIP != "" {
		t.Errorf("disabled should be ClusterIP with no LB IP: %+v", svc.Spec)
	}
	if svc.Annotations["k"] != "v" {
		t.Errorf("user annotation not layered: %v", svc.Annotations)
	}

	// Internal + addrIPValue -> spec.loadBalancerIP set.
	intCfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Scheme", "Internal"),
		confEntry("PortProtocols", "7777/UDP"),
	})
	svc2, err := p.ensureService(context.Background(), cli, pod, intCfg, "svc-int", "addr", "10.9.8.7", false, nil)
	if err != nil {
		t.Fatalf("ensureService internal: %v", err)
	}
	if svc2.Spec.LoadBalancerIP != "10.9.8.7" {
		t.Errorf("internal LB should adopt IP via loadBalancerIP, got %q", svc2.Spec.LoadBalancerIP)
	}
}

func TestPassthroughEnsureService_CreateError(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
			return errors.New("boom")
		},
	}).Build()
	p := newPlugin(t)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	if _, err := p.ensureService(context.Background(), cli, newGamePod("gs-0", "ns1", nil), cfg, "svc", "addr", "", false, nil); err == nil {
		t.Errorf("expected create error")
	}
}

// ---------------------------------------------------------------------------
// proxy OnPodUpdated remaining branches
// ---------------------------------------------------------------------------

func TestProxyOnPodUpdated_NetworkStatusUnmarshalError(t *testing.T) {
	scheme := testScheme(t)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	pod.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus] = "{bad"
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	if _, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected unmarshal error")
	}
}

func TestProxyOnPodUpdated_GssDeletingFlagsPod(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	now := metav1.Now()
	gss.DeletionTimestamp = &now
	gss.Finalizers = []string{"x"}
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).Build()
	out, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if out.Labels[GssDeletingLabelKey] != "true" {
		t.Errorf("expected gss-deleting label")
	}
}

func TestProxyOnPodUpdated_KCCNotReady(t *testing.T) {
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
	// neg-status present so the KCC graph is created, but nothing is pre-seeded
	// Ready -> checkReady false -> NotReady (covers the not-ready return path).
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, svc).Build()
	out, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background())
	if perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	if !strings.Contains(out.Annotations[gamekruiseiov1alpha1.GameServerNetworkStatus], "NotReady") {
		t.Errorf("expected NotReady while KCC graph settling")
	}
}

func TestProxyOnPodUpdated_EnsureErrorsPerKind(t *testing.T) {
	scheme := testScheme(t)
	failKinds := []struct {
		name string
		obj  client.Object
	}{
		{"BES", &gcpv1beta1.ComputeBackendService{}},
		{"TP", &gcpv1beta1.ComputeTargetTCPProxy{}},
		{"Addr", &gcpv1beta1.ComputeAddress{}},
		{"FR", &gcpv1beta1.ComputeForwardingRule{}},
		{"FW", &gcpv1beta1.ComputeFirewall{}},
	}
	for _, fk := range failKinds {
		fk := fk
		t.Run(fk.name, func(t *testing.T) {
			gss := gssForTest("gs", "ns1", "u1", 3)
			pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
			svcName := DeriveServiceName(pod.Name, proxySuffix)
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name: svcName, Namespace: "ns1",
				Annotations: map[string]string{
					NEGStatusAnnotationKey: `{"network_endpoint_groups":{"7777":"neg-7777"},"zones":["us-central1-a"]}`,
				},
			}}
			cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, svc).WithInterceptorFuncs(interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
					if sameKind(obj, fk.obj) {
						return errors.New("boom-" + fk.name)
					}
					return c.Create(ctx, obj, opts...)
				},
			}).Build()
			if _, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
				t.Fatalf("expected ensure error for %s", fk.name)
			}
		})
	}
}

func sameKind(a, b client.Object) bool {
	switch a.(type) {
	case *gcpv1beta1.ComputeBackendService:
		_, ok := b.(*gcpv1beta1.ComputeBackendService)
		return ok
	case *gcpv1beta1.ComputeTargetTCPProxy:
		_, ok := b.(*gcpv1beta1.ComputeTargetTCPProxy)
		return ok
	case *gcpv1beta1.ComputeAddress:
		_, ok := b.(*gcpv1beta1.ComputeAddress)
		return ok
	case *gcpv1beta1.ComputeForwardingRule:
		_, ok := b.(*gcpv1beta1.ComputeForwardingRule)
		return ok
	case *gcpv1beta1.ComputeFirewall:
		_, ok := b.(*gcpv1beta1.ComputeFirewall)
		return ok
	}
	return false
}

// ---------------------------------------------------------------------------
// proxy ensureService / ensureFirewall small branches
// ---------------------------------------------------------------------------

func TestProxyEnsureService_UserAnnotationsAndCreateError(t *testing.T) {
	scheme := testScheme(t)
	p := newProxyPlugin(t)
	pod := newGamePod("gs-0", "ns1", nil)
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"),
		confEntry("Annotations", "k=v"),
	})

	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	svc, err := p.ensureService(context.Background(), cli, pod, cfg, "svc", pod.UID, 0, nil)
	if err != nil {
		t.Fatalf("ensureService: %v", err)
	}
	if svc.Annotations["k"] != "v" {
		t.Errorf("user annotation not applied: %v", svc.Annotations)
	}

	errCli := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, _ client.Object, _ ...client.CreateOption) error {
			return errors.New("boom")
		},
	}).Build()
	if _, err := p.ensureService(context.Background(), errCli, pod, cfg, "svc2", pod.UID, 0, nil); err == nil {
		t.Errorf("expected create error")
	}
}

func TestProxyEnsureFirewall_ProjectFallback(t *testing.T) {
	scheme := testScheme(t)
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	p := newProxyPlugin(t)
	p.projectID = "" // force the cfg.ProjectID=="" -> p.projectID fallback branch
	cfg, _ := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"),
		confEntry("Network", "vpc-fallback"),
	})
	if cfg.ProjectID != "" {
		t.Fatalf("precondition: cfg.ProjectID should be empty, got %q", cfg.ProjectID)
	}
	if err := p.ensureFirewall(context.Background(), cli, newGamePod("gs-0", "ns1", nil), cfg, "fw", nil); err != nil {
		t.Fatalf("ensureFirewall: %v", err)
	}
	fw := &gcpv1beta1.ComputeFirewall{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "fw"}, fw); err != nil {
		t.Fatalf("get fw: %v", err)
	}
	if !strings.Contains(fw.Spec.NetworkRef.External, "vpc-fallback") {
		t.Errorf("expected network selflink with vpc-fallback, got %q", fw.Spec.NetworkRef.External)
	}
}

func TestProxyOnPodDeleted_DeleteError(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 1) // ordinal 0 >= replicas 1 -> release path
	pod := newGamePod("gs-1", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	pod.Finalizers = []string{PodFinalizer}
	frName := DeriveStableResourceID(gss.UID, 1, "gpnlb-fr")
	fr := &gcpv1beta1.ComputeForwardingRule{ObjectMeta: metav1.ObjectMeta{Name: frName, Namespace: "ns1"}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, fr).WithInterceptorFuncs(interceptor.Funcs{
		Delete: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.DeleteOption) error {
			if _, ok := obj.(*gcpv1beta1.ComputeForwardingRule); ok {
				return errors.New("boom-fr")
			}
			return nil
		},
	}).Build()
	if perr := newProxyPlugin(t).OnPodDeleted(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected delete error to surface")
	}
}

// ---------------------------------------------------------------------------
// extra parseConfig error branches
// ---------------------------------------------------------------------------

func TestProxyParseConfig_NonNumericAndUtilization(t *testing.T) {
	p := newProxyPlugin(t)
	if _, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"), confEntry("MaxConnectionsPerEndpoint", "x"),
	}); err == nil {
		t.Errorf("expected non-numeric MaxConnectionsPerEndpoint error")
	}
	cfg, err := p.parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"), confEntry("BalancingMode", "UTILIZATION"),
	})
	if err != nil || cfg.BalancingMode != "UTILIZATION" {
		t.Errorf("UTILIZATION should be valid, got cfg=%+v err=%v", cfg, err)
	}
}
