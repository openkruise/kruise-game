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
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
)

// AddressSpec captures the per-pod parameters used to build a KCC ComputeAddress.
type AddressSpec struct {
	// Name is the Kubernetes metadata.name (also defaults the GCP resourceID).
	Name string
	// Namespace places the CR.
	Namespace string
	// Location is "global" or a region (e.g. "us-central1").
	Location string
	// AddressType is "EXTERNAL" (default) or "INTERNAL".
	AddressType string
	// NetworkTier is "PREMIUM" (default) or "STANDARD". Ignored for INTERNAL.
	NetworkTier string
	// ProjectID, when non-empty, is written as the project-id annotation.
	ProjectID string
	// NetworkRef is required for INTERNAL addresses.
	NetworkRef string
	// SubnetworkRef is required for INTERNAL addresses.
	SubnetworkRef string
	// ResourceID overrides the GCP-side name; defaults to Name.
	ResourceID string
	// OwnerRefs cascade-delete the address when the owning K8s object goes away.
	OwnerRefs []metav1.OwnerReference
}

// EnsureComputeAddress creates or updates the ComputeAddress CR described by
// spec, idempotent against repeated reconciles. The actual GCP IP allocation
// is performed by KCC asynchronously; callers should poll with
// WaitForAddressReady before referencing the IP.
func EnsureComputeAddress(ctx context.Context, c client.Client, spec AddressSpec) (*gcpv1beta1.ComputeAddress, error) {
	if spec.Location == "" {
		return nil, fmt.Errorf("googlecloud: ComputeAddress Location must be set")
	}
	addrType := spec.AddressType
	if addrType == "" {
		addrType = "EXTERNAL"
	}
	addr := &gcpv1beta1.ComputeAddress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
		},
	}
	mutate := func() error {
		if addr.Labels == nil {
			addr.Labels = map[string]string{}
		}
		addr.Labels[ResourceTagKey] = ResourceTagValue
		if spec.ProjectID != "" {
			if addr.Annotations == nil {
				addr.Annotations = map[string]string{}
			}
			addr.Annotations[ProjectIDAnnotation] = spec.ProjectID
		}
		if len(spec.OwnerRefs) > 0 {
			addr.OwnerReferences = spec.OwnerRefs
		}
		addr.Spec.Location = spec.Location
		addr.Spec.AddressType = ptr.To(addrType)
		if spec.ResourceID != "" {
			addr.Spec.ResourceID = ptr.To(spec.ResourceID)
		}
		if addrType == "EXTERNAL" {
			tier := spec.NetworkTier
			if tier == "" {
				tier = "PREMIUM"
			}
			addr.Spec.NetworkTier = ptr.To(tier)
			addr.Spec.IPVersion = ptr.To("IPV4")
			addr.Spec.NetworkRef = nil
			addr.Spec.SubnetworkRef = nil
		} else {
			addr.Spec.NetworkTier = nil
			// GCP rejects an Internal Address that sets BOTH network and
			// subnetwork — subnetwork is the canonical anchor and implies the
			// network. Only set NetworkRef when no subnetwork is provided.
			if spec.SubnetworkRef != "" {
				addr.Spec.SubnetworkRef = subnetRefOrSelfLink(spec.ProjectID, spec.Location, spec.SubnetworkRef)
				addr.Spec.NetworkRef = nil
			} else if spec.NetworkRef != "" {
				addr.Spec.NetworkRef = networkRefOrSelfLink(spec.ProjectID, spec.NetworkRef, "global/networks")
			}
		}
		return nil
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, c, addr, mutate); err != nil {
		return nil, fmt.Errorf("googlecloud: create/update ComputeAddress %s/%s: %w", spec.Namespace, spec.Name, err)
	}
	return addr, nil
}

// networkRefOrSelfLink builds an External selfLink for a native GCP VPC when
// the user passed a bare name. If the name already looks like a selfLink path
// (contains a slash) we pass it through verbatim.
func networkRefOrSelfLink(projectID, name, kindPath string) *gcpv1beta1.ResourceRef {
	if projectID == "" || strings.Contains(name, "/") {
		// Without a project ID we can't synthesize a selfLink; fall back to
		// the KCC-name form and let KCC try to resolve it.
		return &gcpv1beta1.ResourceRef{Name: name}
	}
	return &gcpv1beta1.ResourceRef{
		External: fmt.Sprintf("projects/%s/%s/%s", projectID, kindPath, name),
	}
}

// subnetRefOrSelfLink synthesizes a regional subnetwork selfLink.
func subnetRefOrSelfLink(projectID, location, name string) *gcpv1beta1.ResourceRef {
	if projectID == "" || location == "" || strings.Contains(name, "/") {
		return &gcpv1beta1.ResourceRef{Name: name}
	}
	return &gcpv1beta1.ResourceRef{
		External: fmt.Sprintf("projects/%s/regions/%s/subnetworks/%s", projectID, location, name),
	}
}

// WaitForAddressReady returns the allocated IP from a ComputeAddress's status,
// plus whether it is fully Ready. (ip, ready, err). The caller polls in its
// reconcile loop.
func WaitForAddressReady(ctx context.Context, c client.Client, key types.NamespacedName) (ip string, ready bool, err error) {
	addr := &gcpv1beta1.ComputeAddress{}
	if err := c.Get(ctx, key, addr); err != nil {
		if apierrors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, err
	}
	if !IsKCCReady(addr.Status.Conditions, derefInt64(addr.Status.ObservedGeneration), addr.Generation) {
		return "", false, nil
	}
	if addr.Status.ObservedState != nil && addr.Status.ObservedState.Address != nil {
		return *addr.Status.ObservedState.Address, true, nil
	}
	return "", false, nil
}
