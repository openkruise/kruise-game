/*
Copyright 2023 The Kruise Authors.

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

package gameserver

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
)

const (
	pvNotFoundReason  string = "PersistentVolume Not Found"
	pvcNotFoundReason string = "PersistentVolumeClaim Not Found"
)

func getConditions(ctx context.Context, c client.Client, gs *gamekruiseiov1alpha1.GameServer, eventRecorder record.EventRecorder) ([]gamekruiseiov1alpha1.GameServerCondition, error) {
	var gsConditions []gamekruiseiov1alpha1.GameServerCondition
	now := metav1.Now()
	oldConditions := gs.Status.Conditions
	pod := &corev1.Pod{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      gs.Name,
		Namespace: gs.Namespace,
	}, pod)
	if err != nil {
		return nil, err
	}

	podCondition := getPodConditions(pod.DeepCopy())
	oldPodCondition := getGsCondition(oldConditions, gamekruiseiov1alpha1.PodNormal)
	if !isConditionEqual(podCondition, oldPodCondition) {
		podCondition.LastTransitionTime = now
		if podCondition.Status == corev1.ConditionFalse {
			eventRecorder.Event(gs, corev1.EventTypeWarning, podCondition.Reason, podCondition.Message)
		}
	} else {
		podCondition.LastTransitionTime = oldPodCondition.LastTransitionTime
	}
	gsConditions = append(gsConditions, podCondition)

	if pod.Spec.NodeName != "" {
		node := &corev1.Node{}
		err = c.Get(ctx, types.NamespacedName{
			Name: pod.Spec.NodeName,
		}, node)
		if err != nil {
			return nil, err
		}
		nodeCondition := getNodeConditions(node.DeepCopy())
		oldNodeCondition := getGsCondition(oldConditions, gamekruiseiov1alpha1.NodeNormal)
		if !isConditionEqual(nodeCondition, oldNodeCondition) {
			nodeCondition.LastTransitionTime = now
			if nodeCondition.Status == corev1.ConditionFalse {
				eventRecorder.Event(gs, corev1.EventTypeWarning, nodeCondition.Reason, nodeCondition.Message)
			}
		} else {
			nodeCondition.LastTransitionTime = oldNodeCondition.LastTransitionTime
		}
		gsConditions = append(gsConditions, nodeCondition)
	}

	var pvs []*corev1.PersistentVolume
	var volumeNotFoundConditions []gamekruiseiov1alpha1.GameServerCondition
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil {
			// get pvc
			pvc := &corev1.PersistentVolumeClaim{}
			err = c.Get(ctx, types.NamespacedName{
				Name:      volume.PersistentVolumeClaim.ClaimName,
				Namespace: gs.Namespace,
			}, pvc)
			if err != nil {
				if errors.IsNotFound(err) {
					volumeNotFoundConditions = append(volumeNotFoundConditions, pvcNotFoundCondition(gs.Namespace, pvc.Name))
					continue
				}
				return nil, err
			}

			// get pv
			pvName := pvc.Spec.VolumeName
			if pvName == "" {
				volumeNotFoundConditions = append(volumeNotFoundConditions, pvNotFoundCondition(gs.Namespace, pvc.Name))
				continue
			}
			pv := &corev1.PersistentVolume{}
			err = c.Get(ctx, types.NamespacedName{
				Name: pvName,
			}, pv)
			if err != nil {
				if errors.IsNotFound(err) {
					volumeNotFoundConditions = append(volumeNotFoundConditions, pvNotFoundCondition(gs.Namespace, pvc.Name))
					continue
				}
				return nil, err
			}
			pvs = append(pvs, pv)
		}
	}
	pvCondition := polyCondition(append(volumeNotFoundConditions, getPersistentVolumeConditions(pvs)))
	oldPvCondition := getGsCondition(oldConditions, gamekruiseiov1alpha1.PersistentVolumeNormal)
	if !isConditionEqual(pvCondition, oldPvCondition) {
		pvCondition.LastTransitionTime = now
		if pvCondition.Status == corev1.ConditionFalse {
			eventRecorder.Event(gs, corev1.EventTypeWarning, pvCondition.Reason, pvCondition.Message)
		}
	} else {
		pvCondition.LastTransitionTime = oldPvCondition.LastTransitionTime
	}
	gsConditions = append(gsConditions, pvCondition)

	return gsConditions, nil
}

func getPodConditions(pod *corev1.Pod) gamekruiseiov1alpha1.GameServerCondition {
	var message string
	var reason string

	// pod status
	if pod.Status.Reason != "" {
		message, reason = polyMessageReason(message, reason, pod.Status.Message, pod.Status.Reason)
	}

	// pod conditions
	for _, condition := range pod.Status.Conditions {
		switch condition.Type {
		case corev1.PodScheduled, corev1.PodInitialized, corev1.ContainersReady:
			if condition.Status != corev1.ConditionTrue {
				message, reason = polyMessageReason(message, reason, condition.Message, condition.Reason)
			}
		case corev1.PodReady:
			_, containersReadyCondition := util.GetPodConditionFromList(pod.Status.Conditions, corev1.ContainersReady)
			if containersReadyCondition != nil && containersReadyCondition.Status == corev1.ConditionTrue && condition.Status == corev1.ConditionFalse {
				message, reason = polyMessageReason(message, reason, condition.Message, condition.Reason)
			}
		}
	}

	// containers status
	initContainerMessage, initContainerReason := getContainerStatusMessageReason(pod.Status.InitContainerStatuses)
	if initContainerMessage != "" && initContainerReason != "" {
		message, reason = polyMessageReason(message, reason, initContainerMessage, initContainerReason)
	}
	containerMessage, containerReason := getContainerStatusMessageReason(pod.Status.ContainerStatuses)
	if containerMessage != "" && containerReason != "" {
		message, reason = polyMessageReason(message, reason, containerMessage, containerReason)
	}

	if message == "" && reason == "" {
		return gamekruiseiov1alpha1.GameServerCondition{
			Type:   gamekruiseiov1alpha1.PodNormal,
			Status: corev1.ConditionTrue,
		}
	}

	return gamekruiseiov1alpha1.GameServerCondition{
		Type:    gamekruiseiov1alpha1.PodNormal,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
}

func getContainerStatusMessageReason(containerStatus []corev1.ContainerStatus) (string, string) {
	var message string
	var reason string
	for _, status := range containerStatus {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			// get Waiting state reason
			message, reason = polyMessageReason(message, reason, status.State.Waiting.Message, status.State.Waiting.Reason)
		}
		if status.State.Terminated != nil && status.State.Terminated.Reason != "" {
			// get Terminated state reason
			newMessage := status.State.Terminated.Message + " ExitCode: " + strconv.FormatInt(int64(status.State.Terminated.ExitCode), 10)
			newReason := "ContainerTerminated:" + status.State.Terminated.Reason
			message, reason = polyMessageReason(message, reason, newMessage, newReason)
		} else if status.LastTerminationState.Terminated != nil && status.LastTerminationState.Terminated.Reason != "" && status.State.Running == nil {
			// get LastTerminated state reason
			newMessage := status.LastTerminationState.Terminated.Message + " ExitCode: " + strconv.FormatInt(int64(status.LastTerminationState.Terminated.ExitCode), 10)
			newReason := "ContainerTerminated:" + status.LastTerminationState.Terminated.Reason
			message, reason = polyMessageReason(message, reason, newMessage, newReason)
		}
	}
	return message, reason
}

func getNodeConditions(node *corev1.Node) gamekruiseiov1alpha1.GameServerCondition {
	var message string
	var reason string

	for _, condition := range node.Status.Conditions {
		switch condition.Type {
		case corev1.NodeReady, "SufficientIP":
			if condition.Status != corev1.ConditionTrue {
				message, reason = polyMessageReason(message, reason, condition.Message, string(condition.Type)+":"+condition.Reason)
			}
		default:
			if condition.Status != corev1.ConditionFalse {
				message, reason = polyMessageReason(message, reason, condition.Message, string(condition.Type)+":"+condition.Reason)
			}
		}
	}

	if message == "" && reason == "" {
		return gamekruiseiov1alpha1.GameServerCondition{
			Type:   gamekruiseiov1alpha1.NodeNormal,
			Status: corev1.ConditionTrue,
		}
	}

	return gamekruiseiov1alpha1.GameServerCondition{
		Type:    gamekruiseiov1alpha1.NodeNormal,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
}

func getPersistentVolumeConditions(pvs []*corev1.PersistentVolume) gamekruiseiov1alpha1.GameServerCondition {
	var message string
	var reason string

	for _, pv := range pvs {
		if pv.Status.Reason != "" {
			message, reason = polyMessageReason(message, reason, pv.Status.Message, pv.Status.Reason)
		}
	}

	if message == "" && reason == "" {
		return gamekruiseiov1alpha1.GameServerCondition{
			Type:   gamekruiseiov1alpha1.PersistentVolumeNormal,
			Status: corev1.ConditionTrue,
		}
	}

	return gamekruiseiov1alpha1.GameServerCondition{
		Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
}

func pvcNotFoundCondition(namespace, pvcName string) gamekruiseiov1alpha1.GameServerCondition {
	return gamekruiseiov1alpha1.GameServerCondition{
		Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
		Status:  corev1.ConditionFalse,
		Reason:  pvcNotFoundReason,
		Message: fmt.Sprintf("There is no pvc named %s/%s in cluster", namespace, pvcName),
	}
}

func pvNotFoundCondition(namespace, pvcName string) gamekruiseiov1alpha1.GameServerCondition {
	return gamekruiseiov1alpha1.GameServerCondition{
		Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
		Status:  corev1.ConditionFalse,
		Reason:  pvNotFoundReason,
		Message: fmt.Sprintf("There is no pv which pvc %s/%s is bound with", namespace, pvcName),
	}
}

func polyCondition(conditions []gamekruiseiov1alpha1.GameServerCondition) gamekruiseiov1alpha1.GameServerCondition {
	// remove null conditions
	var noNullConditions []gamekruiseiov1alpha1.GameServerCondition
	for _, condition := range conditions {
		if !reflect.DeepEqual(condition, gamekruiseiov1alpha1.GameServerCondition{}) {
			noNullConditions = append(noNullConditions, condition)
		}
	}

	ret := gamekruiseiov1alpha1.GameServerCondition{}
	isAllTrue := true
	for _, condition := range noNullConditions {
		if condition.Status == corev1.ConditionFalse {
			isAllTrue = false
			break
		}
	}
	for _, condition := range noNullConditions {
		if !isAllTrue && condition.Status == corev1.ConditionTrue {
			continue
		}
		if reflect.DeepEqual(ret, gamekruiseiov1alpha1.GameServerCondition{}) {
			ret = condition
		} else {
			if condition.Type != ret.Type {
				continue
			}
			ret.Message, ret.Reason = polyMessageReason(ret.Message, ret.Reason, condition.Message, condition.Reason)
		}
	}
	return ret
}

func polyMessageReason(message, reason string, newMessage, newReason string) (string, string) {
	if message == "" && reason == "" {
		return newMessage, newReason
	}
	var retMessage string
	var retReason string
	if strings.Contains(reason, newReason) {
		retReason = reason
	} else {
		retReason = reason + "; " + newReason
	}
	if strings.Contains(message, newMessage) {
		retMessage = message
	} else {
		retMessage = message + "; " + newMessage
	}

	return retMessage, retReason
}

func getGsCondition(conditions []gamekruiseiov1alpha1.GameServerCondition, conditionType gamekruiseiov1alpha1.GameServerConditionType) gamekruiseiov1alpha1.GameServerCondition {
	if conditions == nil {
		return gamekruiseiov1alpha1.GameServerCondition{}
	}
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition
		}
	}
	return gamekruiseiov1alpha1.GameServerCondition{}
}

func isConditionEqual(a, b gamekruiseiov1alpha1.GameServerCondition) bool {
	if a.Type != b.Type {
		return false
	}
	if a.Status != b.Status {
		return false
	}
	if a.Message != b.Message {
		return false
	}
	if a.Reason != b.Reason {
		return false
	}
	return true
}

func isConditionsEqual(a, b []gamekruiseiov1alpha1.GameServerCondition) bool {
	if len(a) != len(b) {
		return false
	}

	for _, aCondition := range a {
		found := false
		for _, bCondition := range b {
			if aCondition.Type == bCondition.Type {
				found = true
				if !isConditionEqual(aCondition, bCondition) {
					return false
				}
			}
		}
		if !found {
			return false
		}
	}

	return true
}
