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
	"sort"
	"strconv"
	"sync"

	"github.com/go-logr/logr"
	kruisePub "github.com/openkruise/kruise-api/apps/pub"
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/tracing"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openkruise/kruise-game/pkg/util"
)

type Control interface {
	GameServerScale(context.Context) error
	UpdateWorkload(context.Context) error
	SyncStatus(context.Context) error
	IsNeedToScale() bool
	IsNeedToUpdateWorkload() bool
	SyncPodProbeMarker(context.Context) (error, bool)
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
	logger        logr.Logger
}

func NewGameServerSetManager(gss *gameKruiseV1alpha1.GameServerSet, c client.Client, recorder record.EventRecorder, logger logr.Logger) Control {
	if logger.GetSink() == nil {
		logger = logr.Discard()
	}
	return &GameServerSetManager{
		gameServerSet: gss,
		client:        c,
		eventRecorder: recorder,
		logger:        logger,
	}
}

func sortedOrdinals(set sets.Set[int]) []int {
	if set == nil {
		return nil
	}
	values := set.UnsortedList()
	sort.Ints(values)
	return values
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

	manager.logger.Info("GameServerSet will kill GameServers", tracing.FieldCount, toKill)
	return ptr.To[int32](*gss.Spec.Replicas - int32(toKill))
}

// IsNeedToScale checks if the GameServerSet need to scale,
// return True when the replicas or reserveGameServerIds is changed
func (manager *GameServerSetManager) IsNeedToScale() bool {
	gss := manager.gameServerSet
	asts := manager.asts
	gssSpecReserveIds := util.GetReserveOrdinalIntSet(gss.Spec.ReserveGameServerIds)

	// no need to scale
	return *gss.Spec.Replicas != *asts.Spec.Replicas ||
		!util.StringToOrdinalIntSet(
			gss.GetAnnotations()[gameKruiseV1alpha1.GameServerSetReserveIdsKey], ",",
		).Equal(gssSpecReserveIds)
}

func (manager *GameServerSetManager) GameServerScale(ctx context.Context) error {
	gss := manager.gameServerSet
	asts := manager.asts
	c := manager.client
	logger := manager.logger
	podList := append([]corev1.Pod(nil), manager.podList...)

	currentReplicas := int(*asts.Spec.Replicas)
	expectedReplicas := int(*gss.Spec.Replicas)
	gssAnnotations := gss.GetAnnotations()
	astsSpecReservedOrdinals := util.GetReserveOrdinalIntSet(asts.Spec.ReserveOrdinals)
	gssAnnotationsReservedIds := util.GetReserveOrdinalIntSet(
		util.StringToIntStrSlice(gssAnnotations[gameKruiseV1alpha1.GameServerSetReserveIdsKey], ","))
	astsImplicitReservedIds := util.GetSetInANotInB(astsSpecReservedOrdinals, gssAnnotationsReservedIds)
	gssSpecReservedIds := util.GetReserveOrdinalIntSet(gss.Spec.ReserveGameServerIds)

	logger.Info("Scaling GameServers",
		tracing.FieldCurrentReplicas, currentReplicas,
		tracing.FieldExpectedReplicas, expectedReplicas,
		tracing.FieldReserveIDsSpec, sortedOrdinals(gssSpecReservedIds),
		tracing.FieldReserveIDsAnnotation, sortedOrdinals(gssAnnotationsReservedIds),
		tracing.FieldReserveIDsImplicit, sortedOrdinals(astsImplicitReservedIds),
		tracing.FieldStrategyScaleDown, gss.Spec.ScaleStrategy.ScaleDownStrategyType,
		tracing.FieldStrategyMaxUnavailable, gss.Spec.ScaleStrategy.MaxUnavailable,
		tracing.FieldReclaimPolicy, gss.Spec.GameServerTemplate.ReclaimPolicy,
		tracing.FieldPodCount, len(podList))
	manager.eventRecorder.Eventf(gss, corev1.EventTypeNormal, ScaleReason, "scale from %d to %d", currentReplicas, expectedReplicas)

	newManageIds, newReserveIds := computeToScaleGs(gssSpecReservedIds, gssAnnotationsReservedIds, astsImplicitReservedIds, expectedReplicas, podList)
	logger.Info("Scale plan computed",
		tracing.FieldNewReserveIDs, sortedOrdinals(newReserveIds),
		tracing.FieldNewManageIDs, sortedOrdinals(newManageIds),
		tracing.FieldManagedPods, len(newManageIds))

	if gss.Spec.GameServerTemplate.ReclaimPolicy == gameKruiseV1alpha1.DeleteGameServerReclaimPolicy {
		err := SyncGameServer(ctx, logger, gss, c, newManageIds, util.GetIndexSetFromPodList(podList))
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
		logger.Error(err, "failed to update workload replicas")
		return err
	}

	origGssSpecReservedIds := gssSpecReservedIds.Clone()
	if gss.Spec.ScaleStrategy.ScaleDownStrategyType == gameKruiseV1alpha1.ReserveIdsScaleDownStrategyType {
		gssSpecReservedIds = newReserveIds
	}

	patchAnnotations := map[string]string{}
	newGssAnnotationReservedIdsStr := util.OrdinalSetToString(gssSpecReservedIds)
	if gssAnnotations == nil || gssAnnotations[gameKruiseV1alpha1.GameServerSetReserveIdsKey] != newGssAnnotationReservedIdsStr {
		patchAnnotations[gameKruiseV1alpha1.GameServerSetReserveIdsKey] = newGssAnnotationReservedIdsStr
	}

	gssPatch := map[string]interface{}{}
	if len(patchAnnotations) > 0 {
		gssPatch["metadata"] = map[string]map[string]string{"annotations": patchAnnotations}
	}
	if gss.Spec.ScaleStrategy.ScaleDownStrategyType == gameKruiseV1alpha1.ReserveIdsScaleDownStrategyType && !origGssSpecReservedIds.Equal(gssSpecReservedIds) {
		gssPatch["spec"] = map[string]interface{}{
			"reserveGameServerIds": util.OrdinalSetToIntStrSlice(gssSpecReservedIds),
		}
	}
	if len(gssPatch) == 0 {
		return nil
	}
	patchGssBytes, _ := json.Marshal(gssPatch)
	err = c.Patch(ctx, gss, client.RawPatch(types.MergePatchType, patchGssBytes))
	if err != nil {
		logger.Error(err, "failed to patch GameServerSet for scaling")
		return err
	}

	return nil
}

// computeToScaleGs is to compute what the id list the pods should be existed in cluster, and what the asts reserve id list should be.
// gssSpecReservedIds is the newest explicit id list.
// gssAnnotationsReservedIds is the previous explicit id list.
// astsImplicitReservedIds is the implicit id list.
// pods is the pods that managed by gss now.
func computeToScaleGs(gssSpecReservedIds, gssAnnotationsReservedIds, astsImplicitReservedIds sets.Set[int], expectedReplicas int, pods []corev1.Pod) (workloadManageIds sets.Set[int], newReserveIds sets.Set[int]) {
	// 1. Get newest implicit list & explicit.
	newAddExplicit := util.GetSetInANotInB(gssSpecReservedIds, gssAnnotationsReservedIds)
	newDeleteExplicit := util.GetSetInANotInB(gssAnnotationsReservedIds, gssSpecReservedIds)
	newImplicit := util.GetSetInANotInB(astsImplicitReservedIds, newAddExplicit)
	newImplicit = newImplicit.Union(newDeleteExplicit)
	newExplicit := gssSpecReservedIds

	// 2. Remove the pods ids is in newExplicit.
	workloadManageIds = sets.New[int]()
	var newPods []corev1.Pod
	deletingPodIds := sets.New[int]()
	preDeletingPodIds := sets.New[int]()
	for _, pod := range pods {
		index := util.GetIndexFromGsName(pod.Name)

		// if pod is deleting, exclude it.
		if pod.GetDeletionTimestamp() != nil {
			deletingPodIds.Insert(index)
			continue
		}
		// if pod is preDeleting, exclude it.
		if lifecycleState, exist := pod.GetLabels()[kruisePub.LifecycleStateKey]; exist && lifecycleState == string(kruisePub.LifecycleStatePreparingDelete) {
			preDeletingPodIds.Insert(index)
			continue
		}

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
			if workloadManageIds.Has(i) || newExplicit.Has(i) || preDeletingPodIds.Has(i) {
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

func SyncGameServer(ctx context.Context, logger logr.Logger, gss *gameKruiseV1alpha1.GameServerSet, c client.Client, newManageIds, oldManageIds sets.Set[int]) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	gsLogger := logger.WithValues(
		tracing.FieldGameServerSetNamespace, gss.Namespace,
		tracing.FieldGameServerSetName, gss.Name,
	)

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
				gsLogger.Info("Reset GameServer deleting key", tracing.FieldGameServerName, gsName)
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
				gsLogger.Info("Mark GameServer deleting", tracing.FieldGameServerName, gsName)
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

func (manager *GameServerSetManager) UpdateWorkload(ctx context.Context) error {
	gss := manager.gameServerSet
	asts := manager.asts
	logger := manager.logger
	oldHash := ""
	if asts != nil {
		oldHash = asts.GetAnnotations()[gameKruiseV1alpha1.AstsHashKey]
	}
	newHash := util.GetAstsHash(manager.gameServerSet)
	logger.Info("Updating Advanced StatefulSet from GameServerSet",
		tracing.FieldHashOld, oldHash,
		tracing.FieldHashNew, newHash,
		tracing.FieldServiceName, gss.Spec.ServiceName,
		tracing.FieldPodTemplateRevision, gss.Spec.GameServerTemplate.ResourceVersion,
		tracing.FieldReplicas, ptr.Deref(gss.Spec.Replicas, 0))

	// sync with Advanced StatefulSet
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		asts = util.GetNewAstsFromGss(gss.DeepCopy(), asts)
		astsAns := asts.GetAnnotations()
		astsAns[gameKruiseV1alpha1.AstsHashKey] = newHash
		asts.SetAnnotations(astsAns)

		return manager.client.Update(ctx, asts)
	})

	return retryErr
}

func (manager *GameServerSetManager) SyncPodProbeMarker(ctx context.Context) (error, bool) {
	gss := manager.gameServerSet
	sqs := gss.Spec.ServiceQualities
	c := manager.client
	logger := manager.logger

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
			logger.Info("Creating PodProbeMarker",
				tracing.FieldPodProbeMarker, ppm.Name,
				tracing.FieldServiceQualities, len(sqs),
				tracing.FieldHash, util.GetHash(gss.Spec.ServiceQualities))
			return c.Create(ctx, ppm), false
		}
		return err, false
	}

	// delete ppm
	if sqs == nil {
		logger.Info("Deleting PodProbeMarker because ServiceQualities is empty", tracing.FieldPodProbeMarker, ppm.Name)
		return c.Delete(ctx, ppm), false
	}

	// update ppm
	if util.GetHash(gss.Spec.ServiceQualities) != ppm.GetAnnotations()[gameKruiseV1alpha1.PpmHashKey] {
		hashOld := ppm.GetAnnotations()[gameKruiseV1alpha1.PpmHashKey]
		hashNew := util.GetHash(gss.Spec.ServiceQualities)
		ppm.Spec.Probes = constructProbes(gss)
		ppm.Annotations[gameKruiseV1alpha1.PpmHashKey] = hashNew
		by, _ := json.Marshal(ppm.Spec.Probes)
		manager.eventRecorder.Event(gss, corev1.EventTypeNormal, UpdatePPMReason, "update PodProbeMarker")
		logger.Info("Updating PodProbeMarker",
			tracing.FieldPodProbeMarker, ppm.Name,
			tracing.FieldServiceQualities, len(sqs),
			tracing.FieldHashOld, hashOld,
			tracing.FieldHashNew, hashNew,
			tracing.FieldBody, string(by))
		return c.Update(ctx, ppm), false
	}
	// Determine PodProbeMarker Status to ensure that PodProbeMarker resources have been processed by kruise-manager
	if ppm.Generation != ppm.Status.ObservedGeneration {
		logger.Info("PodProbeMarker observedGeneration inconsistent, waiting",
			tracing.FieldPodProbeMarker, ppm.Name,
			tracing.FieldGeneration, ppm.Generation,
			tracing.FieldObservedGeneration, ppm.Status.ObservedGeneration)
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

func (manager *GameServerSetManager) SyncStatus(ctx context.Context) error {
	gss := manager.gameServerSet
	asts := manager.asts
	c := manager.client
	podList := manager.podList

	maintainingGs := 0
	waitToBeDeletedGs := 0
	preDeleteGs := 0

	for _, pod := range podList {

		podLabels := pod.GetLabels()
		opsState := podLabels[gameKruiseV1alpha1.GameServerOpsStateKey]
		state := podLabels[gameKruiseV1alpha1.GameServerStateKey]

		// ops state
		switch opsState {
		case string(gameKruiseV1alpha1.WaitToDelete):
			waitToBeDeletedGs++
		case string(gameKruiseV1alpha1.Maintaining):
			maintainingGs++
		}

		// state
		switch state {
		case string(gameKruiseV1alpha1.PreDelete):
			preDeleteGs++
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
		PreDeleteReplicas:       ptr.To[int32](int32(preDeleteGs)),
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
