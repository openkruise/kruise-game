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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
)

// gssOwnerRef returns an OwnerReference pointing at the GameServerSet. KCC CRs
// (ComputeAddress, etc.) and the per-pod Service are anchored on the GSS — not
// the Pod — so a Pod recreate at the same ordinal does NOT cascade-delete the
// reserved IP or load balancer. Cleanup on scale-down / GSS deletion is handled
// explicitly in OnPodDeleted (see shouldReleaseSlot).
func gssOwnerRef(gss *gamekruiseiov1alpha1.GameServerSet) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         gss.APIVersion,
		Kind:               gss.Kind,
		Name:               gss.GetName(),
		UID:                gss.GetUID(),
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// shouldReleaseSlot decides, during OnPodDeleted, whether the GCP resources for
// this replica slot must be torn down (true) or preserved across a transient
// Pod recreate (false).
//
// Release when EITHER:
//   - the owning GameServerSet is gone or being deleted (whole-workload teardown), OR
//   - the Pod's ordinal is >= the GameServerSet's desired replica count
//     (a genuine scale-down — this slot will not come back).
//
// Otherwise the Pod is being recreated at an ordinal that is still in range
// (rolling update, eviction, manual delete) and the reserved IP + LB are kept.
func shouldReleaseSlot(ctx context.Context, c client.Client, namespace, gssName, podName string) bool {
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: gssName}, gss)
	if err != nil {
		// GSS not found → workload deleted → release.
		return true
	}
	if gss.DeletionTimestamp != nil {
		return true
	}
	if gss.Spec.Replicas == nil {
		// Defensive: without a replica count we cannot prove the slot is in
		// range, so keep it (avoid destroying a live IP on ambiguous state).
		return false
	}
	ordinal := util.GetIndexFromGsName(podName)
	return ordinal >= int(*gss.Spec.Replicas)
}
