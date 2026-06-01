/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

// NEGRef identifies one zonal Network Endpoint Group exposed by the in-cluster
// GKE NEG controller in response to a cloud.google.com/neg Service annotation.
type NEGRef struct {
	Name string
	Zone string
}

// SelfLink returns the GCP selfLink form used by ComputeBackendService backends.
func (n NEGRef) SelfLink(projectID string) string {
	return fmt.Sprintf("projects/%s/zones/%s/networkEndpointGroups/%s", projectID, n.Zone, n.Name)
}

// negStatusAnnotation matches the JSON written into Service annotations by the
// GKE NEG controller. Shape:
//   {
//     "network_endpoint_groups": {"<port>":"<neg-name>"},
//     "zones": ["us-central1-a","us-central1-b"]
//   }
type negStatusAnnotation struct {
	NetworkEndpointGroups map[string]string `json:"network_endpoint_groups"`
	Zones                 []string          `json:"zones"`
}

// ParseNEGStatusAnnotation extracts per-port NEG refs (one per zone) from the
// cloud.google.com/neg-status annotation. Returns map[port -> []NEGRef].
// Returns (nil, nil) when the annotation is absent or empty (not yet populated).
func ParseNEGStatusAnnotation(svc *corev1.Service) (map[int32][]NEGRef, error) {
	if svc == nil || svc.Annotations == nil {
		return nil, nil
	}
	raw := svc.Annotations[NEGStatusAnnotationKey]
	if raw == "" {
		return nil, nil
	}
	var ann negStatusAnnotation
	if err := json.Unmarshal([]byte(raw), &ann); err != nil {
		return nil, fmt.Errorf("googlecloud: parse %s on Service %s/%s: %w", NEGStatusAnnotationKey, svc.Namespace, svc.Name, err)
	}
	if len(ann.NetworkEndpointGroups) == 0 || len(ann.Zones) == 0 {
		return nil, nil
	}
	sort.Strings(ann.Zones)
	out := make(map[int32][]NEGRef, len(ann.NetworkEndpointGroups))
	for portStr, negName := range ann.NetworkEndpointGroups {
		port, err := strconv.ParseInt(portStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("googlecloud: invalid port %q in %s on Service %s/%s", portStr, NEGStatusAnnotationKey, svc.Namespace, svc.Name)
		}
		refs := make([]NEGRef, 0, len(ann.Zones))
		for _, z := range ann.Zones {
			refs = append(refs, NEGRef{Name: negName, Zone: z})
		}
		out[int32(port)] = refs
	}
	return out, nil
}

