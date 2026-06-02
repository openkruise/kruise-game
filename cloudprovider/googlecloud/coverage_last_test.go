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
	"testing"

	kruisePub "github.com/openkruise/kruise-api/apps/pub"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
	provideroptions "github.com/openkruise/kruise-game/cloudprovider/options"
)

// VerifyKCCInstalled: group present but one required kind missing -> the
// "CRDs missing" error branch (install only five of the six CRDs).
func TestVerifyKCCInstalled_EnvtestMissingOneKind(t *testing.T) {
	cfg := startEnvtest(t)
	all := kccComputeCRDs()
	partial := all[:len(all)-1] // drop ComputeFirewall
	if _, err := installPartial(t, cfg, partial); err != nil {
		t.Fatalf("install partial CRDs: %v", err)
	}
	err := VerifyKCCInstalled(cfg)
	if err == nil {
		t.Fatalf("expected missing-kind error when one CRD is absent")
	}
}

// Init returns the VerifyKCCInstalled error when KCC is absent (no CRDs).
func TestInit_EnvtestVerifyError(t *testing.T) {
	cfg := startEnvtest(t)
	writeKubeconfig(t, cfg)
	opts := provideroptions.GoogleCloudOptions{
		Enable: true, ProjectID: "demo-project",
		PassthroughNLB: provideroptions.PassthroughNLBOptions{Enable: true},
		GlobalProxyNLB: provideroptions.GlobalProxyNLBOptions{Enable: true},
	}
	c := newEnvtestClient(t, cfg)
	if err := (&PassthroughNlbPlugin{}).Init(c, opts, context.Background()); err == nil {
		t.Errorf("passthrough Init should fail when KCC CRDs absent")
	}
	if err := (&GlobalProxyNlbPlugin{}).Init(c, opts, context.Background()); err == nil {
		t.Errorf("proxy Init should fail when KCC CRDs absent")
	}
}

// passthrough OnPodUpdated: AllowNotReadyContainers flips PublishNotReadyAddresses
// during a PreparingUpdate, so the plugin persists the Service (changed=true ->
// c.Update path).
func TestPassthroughOnPodUpdated_AllowNotReadyChanged(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	gss.Spec.Network = &gamekruiseiov1alpha1.Network{
		NetworkConf: []gamekruiseiov1alpha1.NetworkConfParams{
			confEntry(gamekruiseiov1alpha1.AllowNotReadyContainersNetworkConfName, "game"),
		},
	}
	gss.Spec.GameServerTemplate.Spec.Containers = []corev1.Container{{Name: "game", Image: "img:v2"}}

	pod := podWithStatus("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/UDP"),
		confEntry("AllowNotReadyContainers", "true"),
	})
	// PreparingUpdate + a container whose running image differs from the template
	// makes IsContainersPreInplaceUpdating true.
	pod.Labels[kruisePub.LifecycleStateKey] = string(kruisePub.LifecycleStatePreparingUpdate)
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "game", Image: "img:v1"}}

	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := withReadyAddress(&gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}, "203.0.113.11")
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.11"}}},
		},
	}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr, svc).Build()
	if _, perr := newPlugin(t).OnPodUpdated(cli, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodUpdated: %v", perr)
	}
	// The Service must have been re-persisted with PublishNotReadyAddresses=true.
	got := &corev1.Service{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: addrName}, got); err != nil {
		t.Fatalf("get svc: %v", err)
	}
	if !got.Spec.PublishNotReadyAddresses {
		t.Errorf("expected PublishNotReadyAddresses=true after AllowNotReadyContainers update")
	}
}
