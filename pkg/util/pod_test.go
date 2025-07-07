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
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
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

func TestIsContainersPreInplaceUpdating(t *testing.T) {
	tests := []struct {
		pod            *corev1.Pod
		gss            *gameKruiseV1alpha1.GameServerSet
		containerNames []string
		isUpdating     bool
	}{
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "name_A",
							Image: "v1.0",
						},
						{
							Name:  "name_B",
							Image: "v1.0",
						},
					},
				},
			},
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "name_A",
										Image: "v1.0",
									},
									{
										Name:  "name_B",
										Image: "v2.0",
									},
								},
							},
						},
					},
				},
			},
			containerNames: []string{"name_B"},
			isUpdating:     true,
		},
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "name_A",
							Image: "v1.0",
						},
						{
							Name:  "name_B",
							Image: "v1.0",
						},
					},
				},
			},
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "name_A",
										Image: "v1.0",
									},
									{
										Name:  "name_B",
										Image: "v2.0",
									},
								},
							},
						},
					},
				},
			},
			containerNames: []string{"name_A"},
			isUpdating:     false,
		},
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "name_A",
							Image: "v1.0",
						},
						{
							Name:  "name_B",
							Image: "v1.0",
						},
					},
				},
			},
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "name_A",
										Image: "v1.0",
									},
									{
										Name:  "name_B",
										Image: "v1.0",
									},
								},
							},
						},
					},
				},
			},
			containerNames: []string{"name_B"},
			isUpdating:     false,
		},
	}

	for i, test := range tests {
		actual := IsContainersPreInplaceUpdating(test.pod, test.gss, test.containerNames)
		expect := test.isUpdating
		if actual != expect {
			t.Errorf("case %d: expect IsContainersPreInplaceUpdating is %v but actually got %v", i, expect, actual)
		}
	}
}
