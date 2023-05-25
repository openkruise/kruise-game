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

package gameserver

import (
	"context"
	kruisePub "github.com/openkruise/kruise-api/apps/pub"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"time"
)

var (
	NetworkTotalWaitTime = util.GetNetworkTotalWaitTime()
	NetworkIntervalTime  = util.GetNetworkIntervalTime()
)

const (
	TimeFormat = "2006-01-02 15:04:05"
)

const (
	StateReason = "GsStateChanged"
)

type Control interface {
	// SyncGsToPod compares the pod with GameServer, and decide whether to update the pod based on the results.
	// When the fields of the pod is different from that of GameServer, pod will be updated.
	SyncGsToPod() error
	// SyncPodToGs compares the GameServer with pod, and update the GameServer.
	SyncPodToGs(*gameKruiseV1alpha1.GameServerSet) error
	// WaitOrNot compare the current game server network status to decide whether to re-queue.
	WaitOrNot() bool
}

type GameServerManager struct {
	gameServer    *gameKruiseV1alpha1.GameServer
	pod           *corev1.Pod
	client        client.Client
	eventRecorder record.EventRecorder
}

func (manager GameServerManager) SyncGsToPod() error {
	pod := manager.pod
	gs := manager.gameServer
	podLabels := pod.GetLabels()
	podDeletePriority := podLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey]
	podUpdatePriority := podLabels[gameKruiseV1alpha1.GameServerUpdatePriorityKey]
	podGsOpsState := podLabels[gameKruiseV1alpha1.GameServerOpsStateKey]
	podGsState := podLabels[gameKruiseV1alpha1.GameServerStateKey]
	podNetworkDisabled := podLabels[gameKruiseV1alpha1.GameServerNetworkDisabled]

	newLabels := make(map[string]string)
	newAnnotations := make(map[string]string)
	if gs.Spec.DeletionPriority.String() != podDeletePriority {
		newLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey] = gs.Spec.DeletionPriority.String()
		if podDeletePriority != "" {
			manager.eventRecorder.Eventf(gs, corev1.EventTypeNormal, StateReason, "DeletionPriority turn from %s to %s ", podDeletePriority, gs.Spec.DeletionPriority.String())
		}
	}
	if gs.Spec.UpdatePriority.String() != podUpdatePriority {
		newLabels[gameKruiseV1alpha1.GameServerUpdatePriorityKey] = gs.Spec.UpdatePriority.String()
		if podUpdatePriority != "" {
			manager.eventRecorder.Eventf(gs, corev1.EventTypeNormal, StateReason, "UpdatePriority turn from %s to %s ", podUpdatePriority, gs.Spec.UpdatePriority.String())
		}
	}
	if string(gs.Spec.OpsState) != podGsOpsState {
		newLabels[gameKruiseV1alpha1.GameServerOpsStateKey] = string(gs.Spec.OpsState)
		if podGsOpsState != "" {
			eventType := corev1.EventTypeNormal
			if gs.Spec.OpsState == gameKruiseV1alpha1.Maintaining {
				eventType = corev1.EventTypeWarning
			}
			manager.eventRecorder.Eventf(gs, eventType, StateReason, "OpsState turn from %s to %s ", podGsOpsState, string(gs.Spec.OpsState))
		}
	}
	if podNetworkDisabled != strconv.FormatBool(gs.Spec.NetworkDisabled) {
		newLabels[gameKruiseV1alpha1.GameServerNetworkDisabled] = strconv.FormatBool(gs.Spec.NetworkDisabled)
		if podNetworkDisabled != "" {
			manager.eventRecorder.Eventf(gs, corev1.EventTypeNormal, StateReason, "NetworkDisabled turn from %s to %s ", podNetworkDisabled, strconv.FormatBool(gs.Spec.NetworkDisabled))
		}
	}

	var gsState gameKruiseV1alpha1.GameServerState
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// GameServer Updating
		lifecycleState, exist := pod.GetLabels()[kruisePub.LifecycleStateKey]
		if exist && (lifecycleState == string(kruisePub.LifecycleStateUpdating) || lifecycleState == string(kruisePub.LifecycleStatePreparingUpdate)) {
			gsState = gameKruiseV1alpha1.Updating
			break
		}
		// GameServer Deleting
		if !pod.DeletionTimestamp.IsZero() {
			gsState = gameKruiseV1alpha1.Deleting
			break
		}
		// GameServer Ready / NotReady
		_, condition := util.GetPodConditionFromList(pod.Status.Conditions, corev1.PodReady)
		if condition != nil {
			if condition.Status == corev1.ConditionTrue {
				gsState = gameKruiseV1alpha1.Ready
			} else {
				gsState = gameKruiseV1alpha1.NotReady
			}
			break
		}
	case corev1.PodFailed:
		gsState = gameKruiseV1alpha1.Crash
	case corev1.PodPending:
		gsState = gameKruiseV1alpha1.Creating
	default:
		gsState = gameKruiseV1alpha1.Unknown
	}
	if string(gsState) != podGsState {
		newLabels[gameKruiseV1alpha1.GameServerStateKey] = string(gsState)
		if podGsState != "" {
			eventType := corev1.EventTypeNormal
			if gsState == gameKruiseV1alpha1.Crash {
				eventType = corev1.EventTypeWarning
			}
			manager.eventRecorder.Eventf(gs, eventType, StateReason, "State turn from %s to %s ", podGsState, string(gsState))
		}
	}

	if gsState == gameKruiseV1alpha1.Ready && pod.Annotations[gameKruiseV1alpha1.GameServerNetworkType] != "" {
		oldTime, err := time.Parse(TimeFormat, pod.Annotations[gameKruiseV1alpha1.GameServerNetworkTriggerTime])
		if (err == nil && time.Since(oldTime) > NetworkIntervalTime && time.Since(gs.Status.NetworkStatus.LastTransitionTime.Time) < NetworkTotalWaitTime) || (pod.Annotations[gameKruiseV1alpha1.GameServerNetworkTriggerTime] == "") {
			newAnnotations[gameKruiseV1alpha1.GameServerNetworkTriggerTime] = time.Now().Format(TimeFormat)
		}
	}

	if len(newLabels) != 0 || len(newAnnotations) != 0 {
		patchPod := map[string]interface{}{"metadata": map[string]map[string]string{"labels": newLabels, "annotations": newAnnotations}}
		patchPodBytes, err := json.Marshal(patchPod)
		if err != nil {
			return err
		}
		err = manager.client.Patch(context.TODO(), pod, client.RawPatch(types.StrategicMergePatchType, patchPodBytes))
		if err != nil && !errors.IsNotFound(err) {
			klog.Errorf("failed to patch Pod %s in %s,because of %s.", pod.GetName(), pod.GetNamespace(), err.Error())
			return err
		}
	}

	return nil
}

func (manager GameServerManager) SyncPodToGs(gss *gameKruiseV1alpha1.GameServerSet) error {
	gs := manager.gameServer
	pod := manager.pod

	// sync DeletePriority/UpdatePriority/State
	podLabels := pod.GetLabels()
	podDeletePriority := intstr.FromString(podLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey])
	podUpdatePriority := intstr.FromString(podLabels[gameKruiseV1alpha1.GameServerUpdatePriorityKey])
	podGsState := gameKruiseV1alpha1.GameServerState(podLabels[gameKruiseV1alpha1.GameServerStateKey])

	// sync Service Qualities
	spec, newGsConditions := syncServiceQualities(gss.Spec.ServiceQualities, pod.Status.Conditions, gs.Status.ServiceQualitiesCondition)

	// patch gs spec
	patchSpec := map[string]interface{}{"spec": spec}
	jsonPatchSpec, err := json.Marshal(patchSpec)
	if err != nil {
		return err
	}
	err = manager.client.Patch(context.TODO(), gs, client.RawPatch(types.MergePatchType, jsonPatchSpec))
	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("failed to patch GameServer spec %s in %s,because of %s.", gs.GetName(), gs.GetNamespace(), err.Error())
		return err
	}

	// patch gs status
	status := gameKruiseV1alpha1.GameServerStatus{
		PodStatus:                 pod.Status,
		CurrentState:              podGsState,
		DesiredState:              gameKruiseV1alpha1.Ready,
		UpdatePriority:            &podUpdatePriority,
		DeletionPriority:          &podDeletePriority,
		ServiceQualitiesCondition: newGsConditions,
		NetworkStatus:             manager.syncNetworkStatus(),
		LastTransitionTime:        metav1.Now(),
	}
	patchStatus := map[string]interface{}{"status": status}
	jsonPatchStatus, err := json.Marshal(patchStatus)
	if err != nil {
		return err
	}
	err = manager.client.Status().Patch(context.TODO(), gs, client.RawPatch(types.MergePatchType, jsonPatchStatus))
	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("failed to patch GameServer Status %s in %s,because of %s.", gs.GetName(), gs.GetNamespace(), err.Error())
		return err
	}

	return nil
}

func (manager GameServerManager) WaitOrNot() bool {
	networkStatus := manager.gameServer.Status.NetworkStatus
	alreadyWait := time.Since(networkStatus.LastTransitionTime.Time)
	if networkStatus.DesiredNetworkState != networkStatus.CurrentNetworkState && alreadyWait < NetworkTotalWaitTime {
		klog.Infof("GameServer %s/%s DesiredNetworkState: %s CurrentNetworkState: %s. %v remaining",
			manager.gameServer.GetNamespace(), manager.gameServer.GetName(), networkStatus.DesiredNetworkState, networkStatus.CurrentNetworkState, NetworkTotalWaitTime-alreadyWait)
		return true
	}
	return false
}

func (manager GameServerManager) syncNetworkStatus() gameKruiseV1alpha1.NetworkStatus {
	// No Network, return default
	gsNetworkStatus := manager.gameServer.Status.NetworkStatus
	nm := utils.NewNetworkManager(manager.pod, manager.client)
	if nm == nil {
		return gameKruiseV1alpha1.NetworkStatus{}
	}

	// NetworkStatus Init
	if reflect.DeepEqual(gsNetworkStatus, gameKruiseV1alpha1.NetworkStatus{}) {
		return gameKruiseV1alpha1.NetworkStatus{
			NetworkType:         nm.GetNetworkType(),
			DesiredNetworkState: gameKruiseV1alpha1.NetworkReady,
			CreateTime:          metav1.Now(),
			LastTransitionTime:  metav1.Now(),
		}
	}

	// when pod NetworkStatus is nil
	podNetworkStatus, _ := nm.GetNetworkStatus()
	if podNetworkStatus == nil {
		return gsNetworkStatus
	}

	gsNetworkStatus.InternalAddresses = podNetworkStatus.InternalAddresses
	gsNetworkStatus.ExternalAddresses = podNetworkStatus.ExternalAddresses
	gsNetworkStatus.CurrentNetworkState = podNetworkStatus.CurrentNetworkState

	if gsNetworkStatus.DesiredNetworkState != desiredNetworkState(nm.GetNetworkDisabled()) {
		gsNetworkStatus.DesiredNetworkState = desiredNetworkState(nm.GetNetworkDisabled())
		gsNetworkStatus.LastTransitionTime = metav1.Now()
	}

	return gsNetworkStatus
}

func desiredNetworkState(disabled bool) gameKruiseV1alpha1.NetworkState {
	if disabled {
		return gameKruiseV1alpha1.NetworkNotReady
	}
	return gameKruiseV1alpha1.NetworkReady
}

func syncServiceQualities(serviceQualities []gameKruiseV1alpha1.ServiceQuality, podConditions []corev1.PodCondition, sqConditions []gameKruiseV1alpha1.ServiceQualityCondition) (gameKruiseV1alpha1.GameServerSpec, []gameKruiseV1alpha1.ServiceQualityCondition) {
	var spec gameKruiseV1alpha1.GameServerSpec
	var newGsConditions []gameKruiseV1alpha1.ServiceQualityCondition
	sqConditionsMap := make(map[string]gameKruiseV1alpha1.ServiceQualityCondition)
	for _, sqc := range sqConditions {
		sqConditionsMap[sqc.Name] = sqc
	}
	for _, sq := range serviceQualities {
		var newSqCondition gameKruiseV1alpha1.ServiceQualityCondition
		newSqCondition.Name = sq.Name
		index, podCondition := util.GetPodConditionFromList(podConditions, corev1.PodConditionType(util.AddPrefixGameKruise(sq.Name)))
		if index != -1 {
			newSqCondition.Status = string(podCondition.Status)
			newSqCondition.LastProbeTime = podCondition.LastProbeTime
			var lastActionTransitionTime metav1.Time
			sqCondition, exist := sqConditionsMap[sq.Name]
			if !exist || (sqCondition.Status != string(podCondition.Status) && (sqCondition.LastActionTransitionTime.IsZero() || !sq.Permanent)) {
				// exec action
				for _, action := range sq.ServiceQualityAction {
					state, err := strconv.ParseBool(string(podCondition.Status))
					if err == nil && state == action.State {
						spec.DeletionPriority = action.DeletionPriority
						spec.UpdatePriority = action.UpdatePriority
						spec.OpsState = action.OpsState
						spec.NetworkDisabled = action.NetworkDisabled
						lastActionTransitionTime = metav1.Now()
					}
				}
			} else {
				lastActionTransitionTime = sqCondition.LastActionTransitionTime
			}
			newSqCondition.LastActionTransitionTime = lastActionTransitionTime
		}
		newSqCondition.LastTransitionTime = metav1.Now()
		newGsConditions = append(newGsConditions, newSqCondition)
	}
	return spec, newGsConditions
}

func NewGameServerManager(gs *gameKruiseV1alpha1.GameServer, pod *corev1.Pod, c client.Client, recorder record.EventRecorder) Control {
	return &GameServerManager{
		gameServer:    gs,
		pod:           pod,
		client:        c,
		eventRecorder: recorder,
	}
}
