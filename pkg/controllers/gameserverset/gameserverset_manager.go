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
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"
	"strconv"
	"sync"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
)

type Control interface {
	GameServerScale() error
	UpdateWorkload() error
	SyncStatus() error
	IsNeedToScale() bool
	IsNeedToUpdateWorkload() bool
	SyncPodProbeMarker() (error, bool)
	GetReplicasAfterKilling() *int32
	SyncStsAndPodList(asts *kruiseV1beta1.StatefulSet, gsList []corev1.Pod)
}

const (
	DefaultTimeoutSeconds      = 5
	DefaultInitialDelaySeconds = 10
	DefaultPeriodSeconds       = 3
	DefaultSuccessThreshold    = 1
	DefaultFailureThreshold    = 3
)

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

func NewGameServerSetManager(gss *gameKruiseV1alpha1.GameServerSet, c client.Client, recorder record.EventRecorder) Control {
	return &GameServerSetManager{
		gameServerSet: gss,
		client:        c,
		eventRecorder: recorder,
	}
}

func (manager *GameServerSetManager) SyncStsAndPodList(asts *kruiseV1beta1.StatefulSet, gsList []corev1.Pod) {
	manager.asts = asts
	manager.podList = gsList
}

func (manager *GameServerSetManager) GetReplicasAfterKilling() *int32 {
	gss := manager.gameServerSet
	asts := manager.asts
	podList := manager.podList
	if *gss.Spec.Replicas != *asts.Spec.Replicas || *gss.Spec.Replicas != int32(len(podList)) {
		return manager.gameServerSet.Spec.Replicas
	}
	toKill := 0
	for _, pod := range manager.podList {
		if pod.GetDeletionTimestamp() != nil {
			return manager.gameServerSet.Spec.Replicas
		}
		if pod.GetLabels()[gameKruiseV1alpha1.GameServerOpsStateKey] == string(gameKruiseV1alpha1.Kill) {
			toKill++
		}
	}

	klog.Infof("GameServerSet %s/%s will kill %d GameServers", gss.GetNamespace(), gss.GetName(), toKill)
	return ptr.To[int32](*gss.Spec.Replicas - int32(toKill))
}

// IsNeedToScale checks if the GameServerSet need to scale,
// return True when the replicas or reserveGameServerIds is changed
func (manager *GameServerSetManager) IsNeedToScale() bool {
	gss := manager.gameServerSet
	asts := manager.asts
	gssSpecReserveIds := util.GetReserveOrdinalIntSet(gss.Spec.ReserveGameServerIds)

	// no need to scale
	return !(*gss.Spec.Replicas == *asts.Spec.Replicas &&
		util.StringToOrdinalIntSet(
			gss.GetAnnotations()[gameKruiseV1alpha1.GameServerSetReserveIdsKey], ",",
		).Equal(gssSpecReserveIds))
}

func (manager *GameServerSetManager) GameServerScale() error {
	gss := manager.gameServerSet
	asts := manager.asts
	c := manager.client
	ctx := context.Background()
	var podList []corev1.Pod
	for _, pod := range manager.podList {
		if pod.GetDeletionTimestamp() == nil {
			podList = append(podList, pod)
		}
	}

	currentReplicas := len(podList)
	expectedReplicas := int(*gss.Spec.Replicas)
	as := gss.GetAnnotations()
	specReserveIds := util.GetReserveOrdinalIntSet(asts.Spec.ReserveOrdinals)
	reserveIds := util.GetReserveOrdinalIntSet(
		util.StringToIntStrSlice(as[gameKruiseV1alpha1.GameServerSetReserveIdsKey], ","))
	notExistIds := util.GetSetInANotInB(specReserveIds, reserveIds)
	gssReserveIds := util.GetReserveOrdinalIntSet(gss.Spec.ReserveGameServerIds)

	klog.Infof("GameServers %s/%s already has %d replicas, expect to have %d replicas; With newExplicit: %v; oldExplicit: %v; oldImplicit: %v",
		gss.GetNamespace(), gss.GetName(), currentReplicas, expectedReplicas, gssReserveIds, reserveIds, notExistIds)
	manager.eventRecorder.Eventf(gss, corev1.EventTypeNormal, ScaleReason, "scale from %d to %d", currentReplicas, expectedReplicas)

	newManageIds, newReserveIds := computeToScaleGs(gssReserveIds, reserveIds, notExistIds, expectedReplicas, podList)

	if gss.Spec.GameServerTemplate.ReclaimPolicy == gameKruiseV1alpha1.DeleteGameServerReclaimPolicy {
		err := SyncGameServer(gss, c, newManageIds, util.GetIndexSetFromPodList(podList))
		if err != nil {
			return err
		}
	}

	asts.Spec.ReserveOrdinals = util.OrdinalSetToIntStrSlice(newReserveIds)
	asts.Spec.Replicas = gss.Spec.Replicas
	asts.Spec.ScaleStrategy = &kruiseV1beta1.StatefulSetScaleStrategy{
		MaxUnavailable: gss.Spec.ScaleStrategy.MaxUnavailable,
	}
	err := c.Update(ctx, asts)
	if err != nil {
		klog.Errorf("failed to update workload replicas %s in %s,because of %s.", gss.GetName(), gss.GetNamespace(), err.Error())
		return err
	}

	if gss.Spec.ScaleStrategy.ScaleDownStrategyType == gameKruiseV1alpha1.ReserveIdsScaleDownStrategyType {
		gssReserveIds = newReserveIds
	}
	gssAnnotations := make(map[string]string)
	gssAnnotations[gameKruiseV1alpha1.GameServerSetReserveIdsKey] = util.OrdinalSetToString(gssReserveIds)
	patchGss := map[string]interface{}{"spec": map[string]interface{}{"reserveGameServerIds": util.OrdinalSetToIntStrSlice(gssReserveIds)}, "metadata": map[string]map[string]string{"annotations": gssAnnotations}}
	patchGssBytes, _ := json.Marshal(patchGss)
	err = c.Patch(ctx, gss, client.RawPatch(types.MergePatchType, patchGssBytes))
	if err != nil {
		klog.Errorf("failed to patch GameServerSet %s in %s,because of %s.", gss.GetName(), gss.GetNamespace(), err.Error())
		return err
	}

	return nil
}

// computeToScaleGs is to compute what the id list the pods should be existed in cluster, and what the asts reserve id list should be.
// reserveIds is the explicit id list.
// notExistIds is the implicit id list.
// gssReserveIds is the newest explicit id list.
// pods is the pods that managed by gss now.
func computeToScaleGs(gssReserveIds, reserveIds, notExistIds sets.Set[int], expectedReplicas int, pods []corev1.Pod) (workloadManageIds sets.Set[int], newReverseIds sets.Set[int]) {
	// 1. Get newest implicit list & explicit.
	newAddExplicit := util.GetSetInANotInB(gssReserveIds, reserveIds)
	newDeleteExplicit := util.GetSetInANotInB(reserveIds, gssReserveIds)
	newImplicit := util.GetSetInANotInB(notExistIds, newAddExplicit)
	newImplicit = newImplicit.Union(newDeleteExplicit)
	newExplicit := gssReserveIds

	// 2. Remove the pods ids is in newExplicit.
	workloadManageIds = sets.New[int]()
	var newPods []corev1.Pod
	for _, pod := range pods {
		index := util.GetIndexFromGsName(pod.Name)
		if newExplicit.Has(index) {
			continue
		}
		workloadManageIds.Insert(index)
		newPods = append(newPods, pod)
	}

	// 3. Continue to delete or add pods based on the current and expected number of pods.
	existReplicas := len(workloadManageIds)

	if existReplicas < expectedReplicas {
		// Add pods.
		num := 0
		var toAdd []int
		for i := 0; num < expectedReplicas-existReplicas; i++ {
			if workloadManageIds.Has(i) || newExplicit.Has(i) {
				continue
			}
			if newImplicit.Has(i) {
				newImplicit.Delete(i)
			}
			toAdd = append(toAdd, i)
			num++
		}
		workloadManageIds.Insert(toAdd...)
	} else if existReplicas > expectedReplicas {
		// Delete pods.
		sortedGs := util.DeleteSequenceGs(newPods)
		sort.Sort(sortedGs)
		toDelete := util.GetIndexSetFromPodList(sortedGs[:existReplicas-expectedReplicas])
		workloadManageIds = util.GetSetInANotInB(workloadManageIds, toDelete)
		newImplicit = newImplicit.Union(toDelete)
	}

	return workloadManageIds, newImplicit.Union(newExplicit)
}

func SyncGameServer(gss *gameKruiseV1alpha1.GameServerSet, c client.Client, newManageIds, oldManageIds sets.Set[int]) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addIds := util.GetSetInANotInB(newManageIds, oldManageIds)
	deleteIds := util.GetSetInANotInB(oldManageIds, newManageIds)

	errch := make(chan error, len(addIds)+len(deleteIds))
	var wg sync.WaitGroup
	for _, gsId := range addIds.Union(deleteIds).UnsortedList() {
		wg.Add(1)
		id := gsId
		go func(ctx context.Context) {
			defer wg.Done()
			defer ctx.Done()

			gs := &gameKruiseV1alpha1.GameServer{}
			gsName := gss.Name + "-" + strconv.Itoa(id)
			err := c.Get(ctx, types.NamespacedName{
				Name:      gsName,
				Namespace: gss.Namespace,
			}, gs)
			if err != nil {
				if errors.IsNotFound(err) {
					return
				}
				errch <- err
				return
			}

			if addIds.Has(id) && gs.GetLabels()[gameKruiseV1alpha1.GameServerDeletingKey] == "true" {
				gsLabels := make(map[string]string)
				gsLabels[gameKruiseV1alpha1.GameServerDeletingKey] = "false"
				patchGs := map[string]interface{}{"metadata": map[string]map[string]string{"labels": gsLabels}}
				patchBytes, err := json.Marshal(patchGs)
				if err != nil {
					errch <- err
					return
				}
				err = c.Patch(ctx, gs, client.RawPatch(types.MergePatchType, patchBytes))
				if err != nil && !errors.IsNotFound(err) {
					errch <- err
					return
				}
				klog.Infof("GameServer %s/%s DeletingKey turn into false", gss.Namespace, gsName)
			}

			if deleteIds.Has(id) && gs.GetLabels()[gameKruiseV1alpha1.GameServerDeletingKey] != "true" {
				gsLabels := make(map[string]string)
				gsLabels[gameKruiseV1alpha1.GameServerDeletingKey] = "true"
				patchGs := map[string]interface{}{"metadata": map[string]map[string]string{"labels": gsLabels}}
				patchBytes, err := json.Marshal(patchGs)
				if err != nil {
					errch <- err
					return
				}
				err = c.Patch(ctx, gs, client.RawPatch(types.MergePatchType, patchBytes))
				if err != nil && !errors.IsNotFound(err) {
					errch <- err
					return
				}
				klog.Infof("GameServer %s/%s DeletingKey turn into true, who will be deleted", gss.Namespace, gsName)
			}

		}(ctx)
	}

	wg.Wait()
	close(errch)
	err := <-errch
	if err != nil {
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
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		asts = util.GetNewAstsFromGss(gss.DeepCopy(), asts)
		astsAns := asts.GetAnnotations()
		astsAns[gameKruiseV1alpha1.AstsHashKey] = util.GetAstsHash(manager.gameServerSet)
		asts.SetAnnotations(astsAns)

		return manager.client.Update(context.TODO(), asts)
	})

	return retryErr
}

func (manager *GameServerSetManager) SyncPodProbeMarker() (error, bool) {
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
				return nil, true
			}
			// create ppm
			manager.eventRecorder.Event(gss, corev1.EventTypeNormal, CreatePPMReason, "create PodProbeMarker")
			ppm = createPpm(gss)
			klog.Infof("GameserverSet(%s/%s) create PodProbeMarker(%s)", gss.Namespace, gss.Name, ppm.Name)
			return c.Create(ctx, ppm), false
		}
		return err, false
	}

	// delete ppm
	if sqs == nil {
		klog.Infof("GameserverSet(%s/%s) ServiceQualities is empty, and delete PodProbeMarker", gss.Namespace, gss.Name)
		return c.Delete(ctx, ppm), false
	}

	// update ppm
	if util.GetHash(gss.Spec.ServiceQualities) != ppm.GetAnnotations()[gameKruiseV1alpha1.PpmHashKey] {
		ppm.Spec.Probes = constructProbes(gss)
		by, _ := json.Marshal(ppm.Spec.Probes)
		manager.eventRecorder.Event(gss, corev1.EventTypeNormal, UpdatePPMReason, "update PodProbeMarker")
		klog.Infof("GameserverSet(%s/%s) update PodProbeMarker(%s) body(%s)", gss.Namespace, gss.Name, ppm.Name, string(by))
		return c.Update(ctx, ppm), false
	}
	// Determine PodProbeMarker Status to ensure that PodProbeMarker resources have been processed by kruise-manager
	if ppm.Generation != ppm.Status.ObservedGeneration {
		klog.Infof("GameserverSet(%s/%s) PodProbeMarker(%s) status observedGeneration is inconsistent, and wait a moment", gss.Namespace, gss.Name, ppm.Name)
		return nil, false
	}
	return nil, true
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
		if probe.Probe.TimeoutSeconds == 0 {
			probe.Probe.TimeoutSeconds = DefaultTimeoutSeconds
		}
		if probe.Probe.InitialDelaySeconds == 0 {
			probe.Probe.InitialDelaySeconds = DefaultInitialDelaySeconds
		}
		if probe.Probe.PeriodSeconds == 0 {
			probe.Probe.PeriodSeconds = DefaultPeriodSeconds
		}
		if probe.Probe.SuccessThreshold == 0 {
			probe.Probe.SuccessThreshold = DefaultSuccessThreshold
		}
		if probe.Probe.FailureThreshold == 0 {
			probe.Probe.FailureThreshold = DefaultFailureThreshold
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
		Controller:         ptr.To[bool](true),
		BlockOwnerDeletion: ptr.To[bool](true),
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
		MaintainingReplicas:     ptr.To[int32](int32(maintainingGs)),
		WaitToBeDeletedReplicas: ptr.To[int32](int32(waitToBeDeletedGs)),
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
