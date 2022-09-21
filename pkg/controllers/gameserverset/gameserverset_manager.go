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

package gameserverset

import (
	"context"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"
)

type Control interface {
	GameServerScale() error
	UpdateWorkload() error
	SyncStatus() error
	IsNeedToScale() bool
	IsNeedToUpdateWorkload() bool
	SyncPodProbeMarker() error
}

type GameServerSetManager struct {
	gameServerSet *gameKruiseV1alpha1.GameServerSet
	asts          *kruiseV1beta1.StatefulSet
	podList       []corev1.Pod
	client        client.Client
}

func NewGameServerSetManager(gss *gameKruiseV1alpha1.GameServerSet, asts *kruiseV1beta1.StatefulSet, gsList []corev1.Pod, c client.Client) Control {
	return &GameServerSetManager{
		gameServerSet: gss,
		asts:          asts,
		podList:       gsList,
		client:        c,
	}
}

func (manager *GameServerSetManager) IsNeedToScale() bool {
	gss := manager.gameServerSet
	asts := manager.asts
	gsList := manager.podList

	currentReplicas := len(gsList)
	workloadReplicas := int(*asts.Spec.Replicas)
	expectedReplicas := int(*gss.Spec.Replicas)

	// workload is reconciling its replicas, don't interrupt
	if currentReplicas != workloadReplicas {
		return false
	}

	// no need to scale
	return !(expectedReplicas == currentReplicas && util.IsSliceEqual(util.StringToIntSlice(gss.GetAnnotations()[gameKruiseV1alpha1.GameServerSetReserveIdsKey], ","), gss.Spec.ReserveGameServerIds))
}

func (manager *GameServerSetManager) GameServerScale() error {
	gss := manager.gameServerSet
	asts := manager.asts
	gsList := manager.podList
	c := manager.client
	ctx := context.Background()

	currentReplicas := len(gsList)
	expectedReplicas := int(*gss.Spec.Replicas)
	as := gss.GetAnnotations()
	reserveIds := util.StringToIntSlice(as[gameKruiseV1alpha1.GameServerSetReserveIdsKey], ",")
	notExistIds := util.StringToIntSlice(as[gameKruiseV1alpha1.GameServerSetNotExistIdsKey], ",")
	gssReserveIds := gss.Spec.ReserveGameServerIds

	klog.Infof("GameServers %s/%s already has %d replicas, expect to have %d replicas.", gss.GetNamespace(), gss.GetName(), currentReplicas, expectedReplicas)

	newNotExistIds := computeToScaleGs(gssReserveIds, reserveIds, notExistIds, expectedReplicas, gsList)

	asts.Spec.ReserveOrdinals = append(gssReserveIds, newNotExistIds...)
	asts.Spec.Replicas = gss.Spec.Replicas
	asts.Spec.ScaleStrategy = &kruiseV1beta1.StatefulSetScaleStrategy{
		MaxUnavailable: gss.Spec.ScaleStrategy.MaxUnavailable,
	}
	err := c.Update(ctx, asts)
	if err != nil {
		klog.Errorf("failed to update workload replicas %s in %s,because of %s.", gss.GetName(), gss.GetNamespace(), err.Error())
		return err
	}

	gssAnnotations := make(map[string]string)
	gssAnnotations[gameKruiseV1alpha1.GameServerSetReserveIdsKey] = util.IntSliceToString(gssReserveIds, ",")
	gssAnnotations[gameKruiseV1alpha1.GameServerSetNotExistIdsKey] = util.IntSliceToString(newNotExistIds, ",")
	patchGss := map[string]interface{}{"metadata": map[string]map[string]string{"annotations": gssAnnotations}}
	patchGssBytes, _ := json.Marshal(patchGss)
	err = c.Patch(ctx, gss, client.RawPatch(types.MergePatchType, patchGssBytes))
	if err != nil {
		klog.Errorf("failed to patch GameServerSet %s in %s,because of %s.", gss.GetName(), gss.GetNamespace(), err.Error())
		return err
	}

	return nil
}

func computeToScaleGs(gssReserveIds, reserveIds, notExistIds []int, expectedReplicas int, pods []corev1.Pod) []int {
	workloadManageIds := util.GetIndexListFromPodList(pods)

	var toAdd []int
	var toDelete []int

	// 1. compute reserved GameServerIds, firstly

	// 1.a. to delete those new reserved GameServers already in workloadManageIds
	toDelete = util.GetSliceInAandInB(util.GetSliceInANotInB(gssReserveIds, reserveIds), workloadManageIds)

	// 1.b. to add those remove-reserved GameServers already in workloadManageIds
	existLastIndex := -1
	if len(workloadManageIds) != 0 {
		sort.Ints(workloadManageIds)
		existLastIndex = workloadManageIds[len(workloadManageIds)-1]
	}
	for _, id := range util.GetSliceInANotInB(reserveIds, gssReserveIds) {
		if existLastIndex > id {
			toAdd = append(toAdd, id)
		}
	}

	// 2. compute remain GameServerIds, secondly

	numToAdd := expectedReplicas - len(pods) + len(toDelete) - len(toAdd)
	if numToAdd < 0 {

		// 2.a to delete GameServers according to DeleteSequence
		sortedGs := util.DeleteSequenceGs(pods)
		sort.Sort(sortedGs)
		toDelete = append(toDelete, util.GetIndexListFromPodList(sortedGs[:-numToAdd])...)
	} else {

		// 2.b to add GameServers, firstly add those in add notExistIds, secondly add those in future sequence
		numNotExist := len(notExistIds)
		if numNotExist < numToAdd {
			toAdd = append(toAdd, notExistIds...)
			times := 0
			for i := existLastIndex + 1; times < numToAdd-numNotExist; i++ {
				if !util.IsNumInList(i, gssReserveIds) {
					toAdd = append(toAdd, i)
					times++
				}
			}
		} else {
			toAdd = append(toAdd, notExistIds[:numToAdd]...)
		}
	}

	newManageIds := append(workloadManageIds, util.GetSliceInANotInB(toAdd, workloadManageIds)...)
	newManageIds = util.GetSliceInANotInB(newManageIds, toDelete)
	var newNotExistIds []int
	if len(newManageIds) != 0 {
		sort.Ints(newManageIds)
		for i := 0; i < newManageIds[len(newManageIds)-1]; i++ {
			if !util.IsNumInList(i, newManageIds) && !util.IsNumInList(i, gssReserveIds) {
				newNotExistIds = append(newNotExistIds, i)
			}
		}
	}

	return newNotExistIds
}

func (manager *GameServerSetManager) IsNeedToUpdateWorkload() bool {
	return manager.asts.GetLabels()[gameKruiseV1alpha1.AstsHashKey] != util.GetAstsHash(manager.gameServerSet)
}

func (manager *GameServerSetManager) UpdateWorkload() error {
	gss := manager.gameServerSet
	asts := manager.asts

	// sync with Advanced StatefulSet
	asts = util.GetNewAstsFromGss(gss, asts)
	astsLabels := asts.GetLabels()
	astsLabels[gameKruiseV1alpha1.AstsHashKey] = util.GetAstsHash(manager.gameServerSet)
	asts.SetLabels(astsLabels)
	return manager.client.Update(context.Background(), asts)
}

func (manager *GameServerSetManager) SyncPodProbeMarker() error {
	return nil
}

func (manager *GameServerSetManager) SyncStatus() error {
	gss := manager.gameServerSet
	asts := manager.asts
	c := manager.client
	ctx := context.Background()
	podList := manager.podList

	maintainingGs := 0
	waitToBeDeletedGs := 0

	for _, pod := range podList {

		podLabels := pod.GetLabels()
		opsState := podLabels[gameKruiseV1alpha1.GameServerOpsStateKey]

		// ops state
		switch opsState {
		case string(gameKruiseV1alpha1.WaitToDelete):
			waitToBeDeletedGs++
		case string(gameKruiseV1alpha1.Maintaining):
			maintainingGs++
		}
	}

	status := gameKruiseV1alpha1.GameServerSetStatus{
		Replicas:                *gss.Spec.Replicas,
		CurrentReplicas:         int32(len(podList)),
		AvailableReplicas:       asts.Status.AvailableReplicas,
		ReadyReplicas:           asts.Status.ReadyReplicas,
		UpdatedReplicas:         asts.Status.UpdatedReplicas,
		UpdatedReadyReplicas:    asts.Status.UpdatedReadyReplicas,
		MaintainingReplicas:     pointer.Int32Ptr(int32(maintainingGs)),
		WaitToBeDeletedReplicas: pointer.Int32Ptr(int32(waitToBeDeletedGs)),
		LabelSelector:           asts.Status.LabelSelector,
	}

	patchStatus := map[string]interface{}{"status": status}
	jsonPatch, err := json.Marshal(patchStatus)
	if err != nil {
		return err
	}
	return c.Status().Patch(ctx, gss, client.RawPatch(types.MergePatchType, jsonPatch))
}
