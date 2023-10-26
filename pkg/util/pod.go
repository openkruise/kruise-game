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

func IsContainersPreInplaceUpdating(pod *corev1.Pod, gss *gameKruiseV1alpha1.GameServerSet, containerNames []string) bool {
	var diffNames []string
	for _, actual := range pod.Status.ContainerStatuses {
		for _, expect := range gss.Spec.GameServerTemplate.Spec.Containers {
			if actual.Name == expect.Name && actual.Image != expect.Image {
				diffNames = append(diffNames, actual.Name)
			}
		}
	}
	for _, containerName := range containerNames {
		if IsStringInList(containerName, diffNames) {
			return true
		}
	}
	return false
}
