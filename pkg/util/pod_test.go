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

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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

func TestGetDiffContainerNames(t *testing.T) {
	tests := []struct {
		pod      *corev1.Pod
		gss      *gameKruiseV1alpha1.GameServerSet
		expected []string
	}{
		// case 0: one container differs
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "name_A", Image: "v1.0"},
						{Name: "name_B", Image: "v1.0"},
					},
				},
			},
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "name_A", Image: "v1.0"},
									{Name: "name_B", Image: "v2.0"},
								},
							},
						},
					},
				},
			},
			expected: []string{"name_B"},
		},
		// case 1: no containers differ
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "name_A", Image: "v1.0"},
						{Name: "name_B", Image: "v1.0"},
					},
				},
			},
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "name_A", Image: "v1.0"},
									{Name: "name_B", Image: "v1.0"},
								},
							},
						},
					},
				},
			},
			expected: nil,
		},
		// case 2: both containers differ
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "name_A", Image: "v1.0"},
						{Name: "name_B", Image: "v1.0"},
					},
				},
			},
			gss: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{Name: "name_A", Image: "v2.0"},
									{Name: "name_B", Image: "v2.0"},
								},
							},
						},
					},
				},
			},
			expected: []string{"name_A", "name_B"},
		},
	}

	for i, test := range tests {
		actual := GetDiffContainerNames(test.pod, test.gss)
		if !reflect.DeepEqual(actual, test.expected) {
			t.Errorf("case %d: expect GetDiffContainerNames is %v but actually got %v", i, test.expected, actual)
		}
	}
}

// TestGetDiffContainerNamesWithInitContainers covers the restartable init containers (native
// sidecar containers). They report their status in pod.Status.InitContainerStatuses, so they used
// to be invisible to GetDiffContainerNames and the GameServerUpdatingContainers annotation ended
// up empty, leaving the Pod blocked in the PreparingUpdate state.
func TestGetDiffContainerNamesWithInitContainers(t *testing.T) {
	restartAlways := corev1.ContainerRestartPolicyAlways

	podOf := func(initImages, containerImages map[string]string) *corev1.Pod {
		pod := &corev1.Pod{}
		// keep a deterministic order: setup, sidecar
		for _, name := range []string{"setup", "sidecar"} {
			if image, ok := initImages[name]; ok {
				pod.Status.InitContainerStatuses = append(pod.Status.InitContainerStatuses,
					corev1.ContainerStatus{Name: name, Image: image})
			}
		}
		for _, name := range []string{"main"} {
			if image, ok := containerImages[name]; ok {
				pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses,
					corev1.ContainerStatus{Name: name, Image: image})
			}
		}
		return pod
	}

	gssOf := func(sidecarImage string, sidecarRestartable bool, setupImage, mainImage string) *gameKruiseV1alpha1.GameServerSet {
		sidecar := corev1.Container{Name: "sidecar", Image: sidecarImage}
		if sidecarRestartable {
			sidecar.RestartPolicy = &restartAlways
		}
		return &gameKruiseV1alpha1.GameServerSet{
			Spec: gameKruiseV1alpha1.GameServerSetSpec{
				GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
					PodTemplateSpec: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							InitContainers: []corev1.Container{
								{Name: "setup", Image: setupImage},
								sidecar,
							},
							Containers: []corev1.Container{{Name: "main", Image: mainImage}},
						},
					},
				},
			},
		}
	}

	runningInit := map[string]string{"setup": "setup1", "sidecar": "foo1"}
	runningMain := map[string]string{"main": "main1"}

	tests := []struct {
		name     string
		pod      *corev1.Pod
		gss      *gameKruiseV1alpha1.GameServerSet
		expected []string
	}{
		{
			name:     "restartable init container image differs",
			pod:      podOf(runningInit, runningMain),
			gss:      gssOf("foo2", true, "setup1", "main1"),
			expected: []string{"sidecar"},
		},
		{
			name:     "regular init container image differs is skipped",
			pod:      podOf(runningInit, runningMain),
			gss:      gssOf("foo1", true, "setup2", "main1"),
			expected: nil,
		},
		{
			name:     "non restartable init container is skipped even when the image differs",
			pod:      podOf(runningInit, runningMain),
			gss:      gssOf("foo2", false, "setup1", "main1"),
			expected: nil,
		},
		{
			name:     "init container comes before the regular container",
			pod:      podOf(runningInit, runningMain),
			gss:      gssOf("foo2", true, "setup1", "main2"),
			expected: []string{"sidecar", "main"},
		},
		{
			name:     "nothing differs",
			pod:      podOf(runningInit, runningMain),
			gss:      gssOf("foo1", true, "setup1", "main1"),
			expected: nil,
		},
		{
			name:     "nil gss returns nil instead of panicking",
			pod:      podOf(runningInit, runningMain),
			gss:      nil,
			expected: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := GetDiffContainerNames(test.pod, test.gss)
			if !reflect.DeepEqual(actual, test.expected) {
				t.Errorf("expect GetDiffContainerNames is %v but actually got %v", test.expected, actual)
			}
		})
	}
}
