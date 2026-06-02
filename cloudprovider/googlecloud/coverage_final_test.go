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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
)

// ---------------------------------------------------------------------------
// parseConfig: empty trailing-segment "continue" branches
// ---------------------------------------------------------------------------

func TestParseConfig_TrailingCommaSegments(t *testing.T) {
	// Passthrough: trailing comma in PortProtocols and Annotations -> empty
	// segment skipped via continue.
	pc, err := newPlugin(t).parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP,"),
		confEntry("Annotations", "k=v,"),
	})
	if err != nil || len(pc.TargetPorts) != 1 || pc.Annotations["k"] != "v" {
		t.Fatalf("passthrough trailing-comma parse wrong: cfg=%+v err=%v", pc, err)
	}

	// Proxy: trailing comma in Annotations -> empty segment skipped.
	gc, err := newProxyPlugin(t).parseConfig([]gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("Port", "7777"),
		confEntry("Annotations", "k=v,"),
	})
	if err != nil || gc.Annotations["k"] != "v" {
		t.Fatalf("proxy trailing-comma parse wrong: cfg=%+v err=%v", gc, err)
	}
}

// ---------------------------------------------------------------------------
// proxy OnPodUpdated: parseConfig error + ensureService error
// ---------------------------------------------------------------------------

func TestProxyOnPodUpdated_ParseError(t *testing.T) {
	scheme := testScheme(t)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "bad")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	if _, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected parse error")
	}
}

func TestProxyOnPodUpdated_EnsureServiceError(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("Port", "7777")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
			if _, ok := obj.(*corev1.Service); ok {
				return errors.New("boom-svc")
			}
			return nil
		},
	}).Build()
	if _, perr := newProxyPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected ensureService error")
	}
}

// ---------------------------------------------------------------------------
// passthrough OnPodUpdated: EnsureComputeAddress error + ensureService error
// + AllowNotReadyContainers GSS-resolution error
// ---------------------------------------------------------------------------

func TestPassthroughOnPodUpdated_EnsureAddressError(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
			if _, ok := obj.(*gcpv1beta1.ComputeAddress); ok {
				return errors.New("boom-addr")
			}
			return nil
		},
	}).Build()
	if _, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected EnsureComputeAddress error")
	}
}

func TestPassthroughOnPodUpdated_EnsureServiceError(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{confEntry("PortProtocols", "7777/UDP")})
	// Pre-seed a Ready address so EnsureComputeAddress takes the update path and
	// WaitForAddressReady succeeds; then fail the Service create at ensureService.
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "203.0.113.8")
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr).WithInterceptorFuncs(interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if _, ok := obj.(*corev1.Service); ok {
				return errors.New("boom-svc")
			}
			return c.Create(ctx, obj, opts...)
		},
	}).Build()
	if _, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected ensureService error")
	}
}

func TestPassthroughOnPodUpdated_AllowNotReadyGssError(t *testing.T) {
	scheme := testScheme(t)
	// No GSS object -> AllowNotReadyContainers' GetGameServerSetOfPod errors,
	// surfaced as a PluginError (covers the perr!=nil branch).
	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
		confEntry("AllowNotReadyContainers", "true"),
	})
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "203.0.113.9")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.9"}}},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod, addr, svc).Build()
	if _, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr == nil {
		t.Fatalf("expected AllowNotReadyContainers GSS-resolution error to surface")
	} else if !strings.Contains(strings.ToLower(perr.Error()), "not found") {
		t.Logf("note: surfaced error = %v", perr.Error())
	}
}
