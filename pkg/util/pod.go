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
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// GetPodConditionFromList extracts the provided condition from the given list of condition and
// returns the index of the condition and the condition. Returns -1 and nil if the condition is not present.
func GetPodConditionFromList(conditions []corev1.PodCondition, conditionType corev1.PodConditionType) (int, *corev1.PodCondition) {
	if conditions == nil {
		return -1, nil
	}
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return i, &conditions[i]
		}
	}
	return -1, nil
}

// isRestartableInitContainer returns true if the init container is a restartable init container,
// a.k.a. the native sidecar container that has `restartPolicy: Always`.
//
// Only such an init container can be in-place updated: kubelet restarts an init container on
// image change only when its restartPolicy is Always, so changing the image of a regular init
// container makes the workload controller recreate the Pod instead.
func isRestartableInitContainer(container *corev1.Container) bool {
	return container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways
}

// GetDiffContainerNames returns the names of containers whose current image
// (from pod.Status.ContainerStatuses / pod.Status.InitContainerStatuses) differs from the desired
// image (from gss.Spec.GameServerTemplate.Spec.Containers / .Spec.InitContainers).
//
// Restartable init containers (native sidecar containers) are covered as well, because they can be
// in-place updated too. They report their status in pod.Status.InitContainerStatuses rather than in
// pod.Status.ContainerStatuses, so looking up the latter only would return an empty result and the
// GameServerUpdatingContainers annotation would tell nothing to the lifecycle hooks, leaving the
// Pod blocked in the PreparingUpdate state forever.
//
// Regular init containers are skipped on purpose: their image change is not in-place updatable, so
// reporting them as "updating" would be misleading.
func GetDiffContainerNames(pod *corev1.Pod, gss *gameKruiseV1alpha1.GameServerSet) []string {
	if pod == nil || gss == nil {
		return nil
	}

	var diffNames []string
	// Init containers come first, matching the order of the Pod spec.
	for _, actual := range pod.Status.InitContainerStatuses {
		for i := range gss.Spec.GameServerTemplate.Spec.InitContainers {
			expect := &gss.Spec.GameServerTemplate.Spec.InitContainers[i]
			if actual.Name == expect.Name && actual.Image != expect.Image && isRestartableInitContainer(expect) {
				diffNames = append(diffNames, actual.Name)
			}
		}
	}
	for _, actual := range pod.Status.ContainerStatuses {
		for _, expect := range gss.Spec.GameServerTemplate.Spec.Containers {
			if actual.Name == expect.Name && actual.Image != expect.Image {
				diffNames = append(diffNames, actual.Name)
			}
		}
	}
	return diffNames
}

func IsContainersPreInplaceUpdating(pod *corev1.Pod, gss *gameKruiseV1alpha1.GameServerSet, containerNames []string) bool {
	diffNames := GetDiffContainerNames(pod, gss)
	for _, containerName := range containerNames {
		if IsStringInList(containerName, diffNames) {
			return true
		}
	}
	return false
}
