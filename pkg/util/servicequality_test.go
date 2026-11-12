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
	"testing"

	gamev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func TestMatchServiceQualityResult(t *testing.T) {
	tests := []struct {
		name    string
		status  corev1.ConditionStatus
		message string
		state   bool
		result  string
		want    bool
	}{
		{"true with normalized result", corev1.ConditionTrue, "|ready\n", true, "ready", true},
		{"false state", corev1.ConditionFalse, "offline", false, "offline", true},
		{"state mismatch", corev1.ConditionFalse, "ready", true, "ready", false},
		{"result mismatch", corev1.ConditionTrue, "failed", true, "ready", false},
		{"unknown", corev1.ConditionUnknown, "ready", true, "ready", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchServiceQualityResult(tt.status, tt.message, tt.state, tt.result); got != tt.want {
				t.Fatalf("MatchServiceQualityResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAstsHashIgnoresCompletionConditionOrder(t *testing.T) {
	gss := &gamev1alpha1.GameServerSet{Spec: gamev1alpha1.GameServerSetSpec{
		UpdateStrategy: gamev1alpha1.UpdateStrategy{RollingUpdate: &gamev1alpha1.RollingUpdateStatefulSetStrategy{
			CompletionPolicy: &gamev1alpha1.UpdateCompletionPolicy{ServiceQualities: []gamev1alpha1.ServiceQualityCompletionCondition{
				{Name: "b", State: true}, {Name: "a", State: false},
			}},
		}},
	}}
	first := GetAstsHash(gss)
	if gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities[0].Name != "b" {
		t.Fatal("hash computation must not reorder the GSS spec")
	}
	reordered := gss.DeepCopy()
	conditions := reordered.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities
	conditions[0], conditions[1] = conditions[1], conditions[0]
	if first != GetAstsHash(reordered) {
		t.Fatal("condition list order must not change the ASTS hash")
	}
}

func TestAstsHashWithoutCompletionPolicyMatchesLegacyHash(t *testing.T) {
	for _, missingStrategy := range []bool{false, true} {
		gss := &gamev1alpha1.GameServerSet{Spec: gamev1alpha1.GameServerSetSpec{
			GameServerTemplate: gamev1alpha1.GameServerTemplate{PodTemplateSpec: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "v1"}}},
			}},
			UpdateStrategy: gamev1alpha1.UpdateStrategy{RollingUpdate: &gamev1alpha1.RollingUpdateStatefulSetStrategy{}},
		}}
		if missingStrategy {
			gss.Spec.UpdateStrategy.RollingUpdate = nil
		}
		legacyHash := GetHash(astsToUpdate{UpdateStrategy: gss.Spec.UpdateStrategy, Template: gss.Spec.GameServerTemplate})
		if got := GetAstsHash(gss); got != legacyHash {
			t.Fatalf("unconfigured GSS hash changed: got %s, want %s", got, legacyHash)
		}
	}
}
