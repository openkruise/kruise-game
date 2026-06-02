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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
)

// TestDeriveStableResourceID_StableAcrossPodRecreate is the core guarantee of
// the IP-stability rework: the GCP resourceID depends only on (gssUID, ordinal),
// so a Pod recreate (new Pod UID, same ordinal) yields the identical resourceID
// and therefore re-adopts the same reserved IP.
func TestDeriveStableResourceID_StableAcrossPodRecreate(t *testing.T) {
	gssUID := types.UID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	a := DeriveStableResourceID(gssUID, 0, "pnlb-addr")
	b := DeriveStableResourceID(gssUID, 0, "pnlb-addr") // "after pod recreate"
	if a != b {
		t.Fatalf("resourceID not stable across recreate: %q vs %q", a, b)
	}
	if len(a) > 63 {
		t.Fatalf("resourceID over 63 chars: %d / %q", len(a), a)
	}
}

// TestDeriveStableResourceID_DistinctPerOrdinal ensures replicas don't collide.
func TestDeriveStableResourceID_DistinctPerOrdinal(t *testing.T) {
	gssUID := types.UID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		id := DeriveStableResourceID(gssUID, i, "pnlb-addr")
		if seen[id] {
			t.Fatalf("ordinal %d collided: %q", i, id)
		}
		seen[id] = true
	}
}

// TestDeriveStableResourceID_DistinctPerGSS ensures two GameServerSets (even
// same ordinal) don't share GCP resources.
func TestDeriveStableResourceID_DistinctPerGSS(t *testing.T) {
	a := DeriveStableResourceID(types.UID("11111111-1111-1111-1111-111111111111"), 0, "pnlb-addr")
	b := DeriveStableResourceID(types.UID("22222222-2222-2222-2222-222222222222"), 0, "pnlb-addr")
	if a == b {
		t.Fatalf("different GSS UIDs collided: %q", a)
	}
}

// TestDeriveStableResourceID_DistinctPerSuffix ensures the 6 proxy resources
// for one slot get distinct names.
func TestDeriveStableResourceID_DistinctPerSuffix(t *testing.T) {
	gssUID := types.UID("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	suffixes := []string{"gpnlb-hc", "gpnlb-bes", "gpnlb-tp", "gpnlb-addr", "gpnlb-fr", "gpnlb-fw"}
	seen := map[string]bool{}
	for _, s := range suffixes {
		id := DeriveStableResourceID(gssUID, 0, s)
		if seen[id] {
			t.Fatalf("suffix %q collided: %q", s, id)
		}
		seen[id] = true
	}
}

func gssForTest(name, ns string, uid types.UID, replicas int32) *gamekruiseiov1alpha1.GameServerSet {
	return &gamekruiseiov1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, UID: uid},
		Spec:       gamekruiseiov1alpha1.GameServerSetSpec{Replicas: ptr.To(replicas)},
	}
}

func TestShouldReleaseSlot(t *testing.T) {
	scheme := testScheme(t)

	t.Run("in-range ordinal is preserved (pod recreate)", func(t *testing.T) {
		gss := gssForTest("gs", "ns1", "u1", 3)
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss).Build()
		// ordinal 0 < replicas 3 → keep
		if shouldReleaseSlot(context.Background(), cli, "ns1", "gs", "gs-0") {
			t.Fatal("ordinal 0 of replicas=3 should be preserved")
		}
	})

	t.Run("out-of-range ordinal is released (scale-down)", func(t *testing.T) {
		gss := gssForTest("gs", "ns1", "u1", 2)
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss).Build()
		// ordinal 2 >= replicas 2 → release
		if !shouldReleaseSlot(context.Background(), cli, "ns1", "gs", "gs-2") {
			t.Fatal("ordinal 2 of replicas=2 should be released")
		}
	})

	t.Run("missing GSS is released (workload deleted)", func(t *testing.T) {
		cli := fake.NewClientBuilder().WithScheme(scheme).Build()
		if !shouldReleaseSlot(context.Background(), cli, "ns1", "gs", "gs-0") {
			t.Fatal("missing GSS should release")
		}
	})

	t.Run("GSS being deleted is released", func(t *testing.T) {
		gss := gssForTest("gs", "ns1", "u1", 3)
		now := metav1.Now()
		gss.DeletionTimestamp = &now
		gss.Finalizers = []string{"x"} // fake client requires a finalizer to accept a deletionTimestamp
		cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss).Build()
		if !shouldReleaseSlot(context.Background(), cli, "ns1", "gs", "gs-0") {
			t.Fatal("deleting GSS should release")
		}
	})
}

// TestOnPodDeleted_PreservesOnRecreate verifies that, for an in-range ordinal,
// OnPodDeleted keeps the ComputeAddress + Service (so the new Pod keeps its IP)
// and only removes the finalizer.
func TestOnPodDeleted_PreservesOnRecreate(t *testing.T) {
	scheme := testScheme(t)
	gss := gssForTest("gs", "ns1", "u1", 3)
	pod := newGamePod("gs-0", "ns1", []gamekruiseiov1alpha1.NetworkConfParams{
		confEntry("PortProtocols", "7777/TCP"),
		confEntry("Region", "asia-east1"),
	})
	pod.Finalizers = []string{PodFinalizer}
	addrName := DeriveServiceName(pod.Name, passthroughSuffix)
	addr := &gcpv1beta1.ComputeAddress{ObjectMeta: metav1.ObjectMeta{Name: addrName, Namespace: "ns1"}}
	cli := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, pod, addr).Build()
	p := newPlugin(t)
	if perr := p.OnPodDeleted(cli, pod, context.Background()); perr != nil {
		t.Fatalf("OnPodDeleted: %v", perr)
	}
	// Address must still exist (preserved across recreate).
	got := &gcpv1beta1.ComputeAddress{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: addrName}, got); err != nil {
		t.Fatalf("ComputeAddress should be preserved on in-range pod recreate, got: %v", err)
	}
	// Finalizer should be removed so the Pod can complete deletion.
	gotPod := &corev1.Pod{}
	if err := cli.Get(context.Background(), types.NamespacedName{Namespace: "ns1", Name: "gs-0"}, gotPod); err == nil {
		if containsFinalizer(gotPod.Finalizers, PodFinalizer) {
			t.Fatal("finalizer should be removed on recreate")
		}
	}
}
