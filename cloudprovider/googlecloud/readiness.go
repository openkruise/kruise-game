/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package googlecloud

import (
	gcpv1beta1 "github.com/openkruise/kruise-game/cloudprovider/googlecloud/apis/compute/v1beta1"
)

// IsKCCReady returns true when a KCC object reports Ready=True AND has caught
// up with the current generation. Both checks are needed: a stale "Ready" on a
// just-updated spec would otherwise look fresh when it is not.
func IsKCCReady(conditions []gcpv1beta1.Condition, observedGeneration, generation int64) bool {
	if observedGeneration < generation {
		return false
	}
	for _, c := range conditions {
		if c.Type == "Ready" {
			return c.Status == "True"
		}
	}
	return false
}

// CondReason extracts the Reason of the Ready condition, useful for log lines.
func CondReason(conditions []gcpv1beta1.Condition) string {
	for _, c := range conditions {
		if c.Type == "Ready" {
			return c.Reason
		}
	}
	return ""
}

// CondMessage extracts the Message of the Ready condition.
func CondMessage(conditions []gcpv1beta1.Condition) string {
	for _, c := range conditions {
		if c.Type == "Ready" {
			return c.Message
		}
	}
	return ""
}

// derefInt64 collapses *int64 to int64 with a zero default.
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
