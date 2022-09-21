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
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
)

type Control interface {
	SyncToPod() (bool, error)
	SyncToGs(qualities []gameKruiseV1alpha1.ServiceQuality) error
}

type GameServerManager struct {
	gameServer *gameKruiseV1alpha1.GameServer
	pod        *corev1.Pod
	client     client.Client
}

func (manager GameServerManager) SyncToPod() (bool, error) {
	// compare GameServer Spec With Pod
	pod := manager.pod
	gs := manager.gameServer
	podLabels := pod.GetLabels()
	podDeletePriority := podLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey]
	podUpdatePriority := podLabels[gameKruiseV1alpha1.GameServerUpdatePriorityKey]
	podGsOpsState := podLabels[gameKruiseV1alpha1.GameServerOpsStateKey]
	podGsState := podLabels[gameKruiseV1alpha1.GameServerStateKey]

	updated := false
	newLabels := make(map[string]string)
	if gs.Spec.DeletionPriority.String() != podDeletePriority {
		newLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey] = gs.Spec.DeletionPriority.String()
		updated = true
	}
	if gs.Spec.UpdatePriority.String() != podUpdatePriority {
		newLabels[gameKruiseV1alpha1.GameServerUpdatePriorityKey] = gs.Spec.UpdatePriority.String()
		updated = true
	}
	if string(gs.Spec.OpsState) != podGsOpsState {
		newLabels[gameKruiseV1alpha1.GameServerOpsStateKey] = string(gs.Spec.OpsState)
		updated = true
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
		for _, con := range pod.Status.Conditions {
			if con.Type == corev1.PodReady {
				if con.Status == corev1.ConditionTrue {
					gsState = gameKruiseV1alpha1.Ready
				} else {
					gsState = gameKruiseV1alpha1.NotReady
				}
				break
			}
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
		updated = true
	}

	if updated {
		patchPod := map[string]interface{}{"metadata": map[string]map[string]string{"labels": newLabels}}
		patchPodBytes, err := json.Marshal(patchPod)
		if err != nil {
			return updated, err
		}
		err = manager.client.Patch(context.TODO(), pod, client.RawPatch(types.StrategicMergePatchType, patchPodBytes))
		if err != nil && !errors.IsNotFound(err) {
			klog.Errorf("failed to patch Pod %s in %s,because of %s.", pod.GetName(), pod.GetNamespace(), err.Error())
			return updated, err
		}
	}

	return updated, nil
}

func (manager GameServerManager) SyncToGs(qualities []gameKruiseV1alpha1.ServiceQuality) error {
	gs := manager.gameServer
	pod := manager.pod
	podLabels := pod.GetLabels()
	podDeletePriority := intstr.FromString(podLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey])
	podUpdatePriority := intstr.FromString(podLabels[gameKruiseV1alpha1.GameServerUpdatePriorityKey])
	podGsState := gameKruiseV1alpha1.GameServerState(podLabels[gameKruiseV1alpha1.GameServerStateKey])

	gsConditions := gs.Status.ServiceQualitiesCondition
	podConditions := pod.Status.Conditions
	var sqNames []string
	for _, sq := range qualities {
		sqNames = append(sqNames, sq.Name)
	}
	var toExec map[string]string
	var newGsConditions []gameKruiseV1alpha1.ServiceQualityCondition
	for _, pc := range podConditions {
		if util.IsStringInList(string(pc.Type), sqNames) {
			toExecAction := true
			lastActionTransitionTime := metav1.Now()
			for _, gsc := range gsConditions {
				if gsc.Name == string(pc.Type) && gsc.Status == string(pc.Status) {
					toExecAction = false
					lastActionTransitionTime = gsc.LastActionTransitionTime
					break
				}
			}
			serviceQualityCondition := gameKruiseV1alpha1.ServiceQualityCondition{
				Name:                     string(pc.Type),
				Status:                   string(pc.Status),
				LastProbeTime:            pc.LastProbeTime,
				LastTransitionTime:       pc.LastTransitionTime,
				LastActionTransitionTime: lastActionTransitionTime,
			}
			newGsConditions = append(newGsConditions, serviceQualityCondition)
			if toExecAction {
				if toExec == nil {
					toExec = make(map[string]string)
				}
				toExec[string(pc.Type)] = string(pc.Status)
			}
		}
	}
	if toExec != nil {
		var spec gameKruiseV1alpha1.GameServerSpec
		for _, sq := range qualities {
			for name, state := range toExec {
				if sq.Name == name {
					for _, action := range sq.ServiceQualityAction {
						if state == strconv.FormatBool(action.State) {
							spec.DeletionPriority = action.DeletionPriority
							spec.UpdatePriority = action.UpdatePriority
							spec.OpsState = action.OpsState
							spec.NetworkDisabled = action.NetworkDisabled
						}
					}
				}
			}
		}

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
	}

	status := gameKruiseV1alpha1.GameServerStatus{
		PodStatus:                 pod.Status,
		CurrentState:              podGsState,
		DesiredState:              gameKruiseV1alpha1.Ready,
		UpdatePriority:            &podUpdatePriority,
		DeletionPriority:          &podDeletePriority,
		ServiceQualitiesCondition: newGsConditions,
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

func NewGameServerManager(gs *gameKruiseV1alpha1.GameServer, pod *corev1.Pod, c client.Client) Control {
	return &GameServerManager{
		gameServer: gs,
		pod:        pod,
		client:     c,
	}
}
