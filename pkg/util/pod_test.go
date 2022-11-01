/*
Copyright 2022 The Kruise Authors.

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
	corev1 "k8s.io/api/core/v1"
	"reflect"
	"testing"
)

func TestGetPodConditionFromList(t *testing.T) {
	tests := []struct {
		podConditions []corev1.PodCondition
		conditionType corev1.PodConditionType
		index         int
		podCondition  *corev1.PodCondition
	}{
		{
			podConditions: []corev1.PodCondition{
				{
					Type:   "game.kruise.io/healthy",
					Status: corev1.ConditionTrue,
				},
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
			conditionType: corev1.PodReady,
			index:         1,
			podCondition: &corev1.PodCondition{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
			},
		},
	}

	for _, test := range tests {
		actualIndex, actualPodCondition := GetPodConditionFromList(test.podConditions, test.conditionType)
		if actualIndex != test.index {
			t.Errorf("expect to get index %v but got %v", test.index, actualIndex)
		}
		if !reflect.DeepEqual(*test.podCondition, *actualPodCondition) {
			t.Errorf("expect to get condition %v but got %v", *test.podCondition, *actualPodCondition)
		}
	}
}
