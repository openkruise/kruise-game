/*
Copyright 2026 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"strings"

	gamev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const ServiceQualityUpdateProgressing = "UpdateProgressing"

func HasCompletionPolicy(gss *gamev1alpha1.GameServerSet) bool {
	return gss.Spec.UpdateStrategy.RollingUpdate != nil && gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy != nil
}

func IsServiceQualityGatedUpdateActive(gss *gamev1alpha1.GameServerSet) bool {
	if !HasCompletionPolicy(gss) || gss.Status.UpdateProgress == nil {
		return false
	}
	condition := meta.FindStatusCondition(gss.Status.Conditions, ServiceQualityUpdateProgressing)
	return condition == nil || condition.Status != metav1.ConditionFalse
}

// MatchServiceQualityResult matches completion conditions using the existing
// ServiceQuality action semantics without changing the action execution path.
func MatchServiceQualityResult(status corev1.ConditionStatus, message string, state bool, result string) bool {
	if status != corev1.ConditionTrue && status != corev1.ConditionFalse {
		return false
	}
	if (status == corev1.ConditionTrue) != state {
		return false
	}
	return result == "" || strings.ReplaceAll(strings.ReplaceAll(message, "|", ""), "\n", "") == result
}
