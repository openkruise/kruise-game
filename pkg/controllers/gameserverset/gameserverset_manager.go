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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/record"
	"sort"
	"sync"
	"time"

	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
)

type Control interface {
	GameServerScale() error
	UpdateWorkload() error
	SyncStatus() error
	IsNeedToScale() bool
	IsNeedToUpdateWorkload() bool
	SyncPodProbeMarker() error
	SyncGameServerReplicas() error
}

const (
	ScaleReason          = "Scale"
	CreatePPMReason      = "CreatePpm"
	UpdatePPMReason      = "UpdatePpm"
	CreateWorkloadReason = "CreateWorkload"
	UpdateWorkloadReason = "UpdateWorkload"
)

type GameServerSetManager struct {
	gameServerSet *gameKruiseV1alpha1.GameServerSet
	asts          *kruiseV1beta1.StatefulSet
	podList       []corev1.Pod
	client        client.Client
	eventRecorder record.EventRecorder
}

func NewGameServerSetManager(gss *gameKruiseV1alpha1.GameServerSet, asts *kruiseV1beta1.StatefulSet, gsList []corev1.Pod, c client.Client, recorder record.EventRecorder) Control {
	return &GameServerSetManager{
		gameServerSet: gss,
		asts:          asts,
		podList:       gsList,
		client:        c,
		eventRecorder: recorder,
	}
}

func (manager *GameServerSetManager) IsNeedToScale() bool {
	gss := manager.gameServerSet
	asts := manager.asts

	// workload is reconciling its replicas, don't interrupt
	if asts.Status.Replicas != *asts.Spec.Replicas {
		return false
	}

	// no need to scale
	return !(*gss.Spec.Replicas == *asts.Spec.Replicas &&
		util.IsSliceEqual(util.StringToIntSlice(gss.GetAnnotations()[gameKruiseV1alpha1.GameServerSetReserveIdsKey], ","), gss.Spec.ReserveGameServerIds))
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
	notExistIds := util.GetSliceInANotInB(asts.Spec.ReserveOrdinals, reserveIds)
	gssReserveIds := gss.Spec.ReserveGameServerIds

	klog.Infof("GameServers %s/%s already has %d replicas, expect to have %d replicas.", gss.GetNamespace(), gss.GetName(), currentReplicas, expectedReplicas)
	manager.eventRecorder.Eventf(gss, corev1.EventTypeNormal, ScaleReason, "scale from %d to %d", currentReplicas, expectedReplicas)

	asts.Spec.ReserveOrdinals = append(gssReserveIds, computeToScaleGs(gssReserveIds, reserveIds, notExistIds, expectedReplicas, gsList)...)
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

func (manager *GameServerSetManager) SyncGameServerReplicas() error {
	gss := manager.gameServerSet
	gsList := &gameKruiseV1alpha1.GameServerList{}
	err := manager.client.List(context.Background(), gsList, &client.ListOptions{
		Namespace: gss.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			gameKruiseV1alpha1.GameServerOwnerGssKey: gss.GetName(),
		})})
	if err != nil {
		return err
	}
	podIds := util.GetIndexListFromPodList(manager.podList)

	gsMap := make(map[int]int)
	deleteIds := make([]int, 0)
	for id, gs := range gsList.Items {
		gsId := util.GetIndexFromGsName(gs.Name)
		if !util.IsNumInList(gsId, podIds) {
			gsMap[gsId] = id
			deleteIds = append(deleteIds, gsId)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	errch := make(chan error, len(deleteIds))
	var wg sync.WaitGroup
	for _, gsId := range deleteIds {
		wg.Add(1)
		id := gsId
		go func(ctx context.Context) {
			defer wg.Done()
			defer ctx.Done()

			gs := gsList.Items[gsMap[id]].DeepCopy()
			gsLabels := make(map[string]string)
			gsLabels[gameKruiseV1alpha1.GameServerDeletingKey] = "true"
			patchGs := map[string]interface{}{"metadata": map[string]map[string]string{"labels": gsLabels}}
			patchBytes, err := json.Marshal(patchGs)
			if err != nil {
				errch <- err
			}
			err = manager.client.Patch(context.TODO(), gs, client.RawPatch(types.MergePatchType, patchBytes))
			if err != nil && !errors.IsNotFound(err) {
				errch <- err
			}
		}(ctx)
	}
	wg.Wait()
	close(errch)
	err = <-errch
	if err != nil {
		klog.Errorf("failed to delete GameServers %s in %s because of %s.", gss.GetName(), gss.GetNamespace(), err.Error())
		return err
	}

	return nil
}

func (manager *GameServerSetManager) IsNeedToUpdateWorkload() bool {
	return manager.asts.GetAnnotations()[gameKruiseV1alpha1.AstsHashKey] != util.GetAstsHash(manager.gameServerSet)
}

func (manager *GameServerSetManager) UpdateWorkload() error {
	gss := manager.gameServerSet
	asts := manager.asts

	// sync with Advanced StatefulSet
	asts = util.GetNewAstsFromGss(gss.DeepCopy(), asts)
	astsAns := asts.GetAnnotations()
	astsAns[gameKruiseV1alpha1.AstsHashKey] = util.GetAstsHash(manager.gameServerSet)

	patchAsts := map[string]interface{}{"metadata": map[string]map[string]string{"annotations": astsAns}, "spec": asts.Spec}
	patchAstsBytes, err := json.Marshal(patchAsts)
	if err != nil {
		return err
	}
	return manager.client.Patch(context.TODO(), asts, client.RawPatch(types.MergePatchType, patchAstsBytes))
}

func (manager *GameServerSetManager) SyncPodProbeMarker() error {
	gss := manager.gameServerSet
	sqs := gss.Spec.ServiceQualities
	c := manager.client
	ctx := context.Background()

	// get ppm
	ppm := &kruiseV1alpha1.PodProbeMarker{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: gss.GetNamespace(),
		Name:      gss.GetName(),
	}, ppm)
	if err != nil {
		if errors.IsNotFound(err) {
			if sqs == nil {
				return nil
			}
			// create ppm
			manager.eventRecorder.Event(gss, corev1.EventTypeNormal, CreatePPMReason, "create PodProbeMarker")
			return c.Create(ctx, createPpm(gss))
		}
		return err
	}

	// delete ppm
	if sqs == nil {
		return c.Delete(ctx, ppm)
	}

	// update ppm
	if util.GetHash(gss.Spec.ServiceQualities) != ppm.GetAnnotations()[gameKruiseV1alpha1.PpmHashKey] {
		ppm.Spec.Probes = constructProbes(gss)
		manager.eventRecorder.Event(gss, corev1.EventTypeNormal, UpdatePPMReason, "update PodProbeMarker")
		return c.Update(ctx, ppm)
	}
	return nil
}

func constructProbes(gss *gameKruiseV1alpha1.GameServerSet) []kruiseV1alpha1.PodContainerProbe {
	var probes []kruiseV1alpha1.PodContainerProbe
	for _, sq := range gss.Spec.ServiceQualities {
		probe := kruiseV1alpha1.PodContainerProbe{
			Name:          sq.Name,
			ContainerName: sq.ContainerName,
			Probe: kruiseV1alpha1.ContainerProbeSpec{
				Probe: sq.Probe,
			},
			PodConditionType: util.AddPrefixGameKruise(sq.Name),
		}
		probes = append(probes, probe)
	}
	return probes
}

func createPpm(gss *gameKruiseV1alpha1.GameServerSet) *kruiseV1alpha1.PodProbeMarker {
	// set owner reference
	ors := make([]metav1.OwnerReference, 0)
	or := metav1.OwnerReference{
		APIVersion:         gss.APIVersion,
		Kind:               gss.Kind,
		Name:               gss.GetName(),
		UID:                gss.GetUID(),
		Controller:         pointer.BoolPtr(true),
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}
	ors = append(ors, or)
	return &kruiseV1alpha1.PodProbeMarker{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gss.GetName(),
			Namespace: gss.GetNamespace(),
			Annotations: map[string]string{
				gameKruiseV1alpha1.PpmHashKey: util.GetHash(gss.Spec.ServiceQualities),
			},
			OwnerReferences: ors,
		},
		Spec: kruiseV1alpha1.PodProbeMarkerSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{gameKruiseV1alpha1.GameServerOwnerGssKey: gss.GetName()},
			},
			Probes: constructProbes(gss),
		},
	}
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
		ObservedGeneration:      gss.GetGeneration(),
	}
	if equality.Semantic.DeepEqual(gss.Status, status) {
		return nil
	}
	patchStatus := map[string]interface{}{"status": status}
	jsonPatch, err := json.Marshal(patchStatus)
	if err != nil {
		return err
	}
	return c.Status().Patch(ctx, gss, client.RawPatch(types.MergePatchType, jsonPatch))
}
