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
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	kruisePub "github.com/openkruise/kruise-api/apps/pub"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider/utils"
	"github.com/openkruise/kruise-game/pkg/telemetryfields"
	"github.com/openkruise/kruise-game/pkg/tracing"
	"github.com/openkruise/kruise-game/pkg/util"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	NetworkTotalWaitTime = util.GetNetworkTotalWaitTime()
	NetworkIntervalTime  = util.GetNetworkIntervalTime()
)

const (
	TimeFormat = time.RFC3339
)

const (
	StateReason          = "GsStateChanged"
	GsNetworkStateReason = "GsNetworkState"
)

type Control interface {
	// SyncGsToPod compares the pod with GameServer, and decide whether to update the pod based on the results.
	// When the fields of the pod is different from that of GameServer, pod will be updated.
	SyncGsToPod(context.Context) error
	// SyncPodToGs compares the GameServer with pod, and update the GameServer.
	SyncPodToGs(context.Context, *gameKruiseV1alpha1.GameServerSet) error
	// WaitOrNot compare the current game server network status to decide whether to re-queue.
	WaitOrNot() bool
}

type GameServerManager struct {
	gameServer    *gameKruiseV1alpha1.GameServer
	pod           *corev1.Pod
	client        client.Client
	eventRecorder record.EventRecorder
	logger        logr.Logger
}

func isNeedToSyncMetadata(gss *gameKruiseV1alpha1.GameServerSet, gs *gameKruiseV1alpha1.GameServer) bool {
	return gs.Annotations[gameKruiseV1alpha1.GsTemplateMetadataHashKey] != util.GetGsTemplateMetadataHash(gss)
}

func syncMetadataFromGss(gss *gameKruiseV1alpha1.GameServerSet) metav1.ObjectMeta {
	templateLabels := gss.Spec.GameServerTemplate.GetLabels()
	templateAnnotations := gss.Spec.GameServerTemplate.GetAnnotations()
	if templateAnnotations == nil {
		templateAnnotations = make(map[string]string)
	}
	templateAnnotations[gameKruiseV1alpha1.GsTemplateMetadataHashKey] = util.GetGsTemplateMetadataHash(gss)
	return metav1.ObjectMeta{
		Labels:      templateLabels,
		Annotations: templateAnnotations,
	}
}

func (manager GameServerManager) SyncGsToPod(ctx context.Context) error {
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
	// tolerate nil pointers in spec priorities
	var gsDpStr, gsUpStr string
	if gs.Spec.DeletionPriority != nil {
		gsDpStr = gs.Spec.DeletionPriority.String()
	}
	if gs.Spec.UpdatePriority != nil {
		gsUpStr = gs.Spec.UpdatePriority.String()
	}
	if gsDpStr != podDeletePriority {
		newLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey] = gsDpStr
		if podDeletePriority != "" {
			manager.eventRecorder.Eventf(gs, corev1.EventTypeNormal, StateReason, "DeletionPriority turn from %s to %s ", podDeletePriority, gsDpStr)
		}
	}
	if gsUpStr != podUpdatePriority {
		newLabels[gameKruiseV1alpha1.GameServerUpdatePriorityKey] = gsUpStr
		if podUpdatePriority != "" {
			manager.eventRecorder.Eventf(gs, corev1.EventTypeNormal, StateReason, "UpdatePriority turn from %s to %s ", podUpdatePriority, gsUpStr)
		}
	}
	if string(gs.Spec.OpsState) != podGsOpsState {
		newLabels[gameKruiseV1alpha1.GameServerOpsStateKey] = string(gs.Spec.OpsState)
		newAnnotations[gameKruiseV1alpha1.GameServerOpsStateLastChangedTime] = time.Now().Format(TimeFormat)
		if podGsOpsState != "" {
			eventType := corev1.EventTypeNormal
			if gs.Spec.OpsState == gameKruiseV1alpha1.Maintaining {
				eventType = corev1.EventTypeWarning
			}
			manager.eventRecorder.Eventf(gs, eventType, StateReason, "OpsState turn from %s to %s ", podGsOpsState, string(gs.Spec.OpsState))
		}
	}
	currentNetworkDisabled := strconv.FormatBool(ptr.Deref(gs.Spec.NetworkDisabled, false))
	if podNetworkDisabled != currentNetworkDisabled {
		newLabels[gameKruiseV1alpha1.GameServerNetworkDisabled] = currentNetworkDisabled
		if podNetworkDisabled != "" {
			manager.eventRecorder.Eventf(gs, corev1.EventTypeNormal, StateReason, "NetworkDisabled turn from %s to %s ", podNetworkDisabled, currentNetworkDisabled)
		}
	}

	var gsState gameKruiseV1alpha1.GameServerState
	switch pod.Status.Phase {
	case corev1.PodRunning:
		// GameServer Deleting
		if !pod.DeletionTimestamp.IsZero() {
			gsState = gameKruiseV1alpha1.Deleting
			break
		}
		// GameServer Updating
		lifecycleState, exist := pod.GetLabels()[kruisePub.LifecycleStateKey]
		if exist && lifecycleState == string(kruisePub.LifecycleStateUpdating) {
			gsState = gameKruiseV1alpha1.Updating
			break
		}
		// GameServer PreUpdate
		if exist && lifecycleState == string(kruisePub.LifecycleStatePreparingUpdate) {
			gsState = gameKruiseV1alpha1.PreUpdate
			break
		}
		// GameServer PreDelete
		if exist && lifecycleState == string(kruisePub.LifecycleStatePreparingDelete) {
			gsState = gameKruiseV1alpha1.PreDelete
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
			newAnnotations[gameKruiseV1alpha1.GameServerStateLastChangedTime] = time.Now().Format(TimeFormat)
			manager.eventRecorder.Eventf(gs, eventType, StateReason, "State turn from %s to %s ", podGsState, string(gsState))
		}
	}

	if pod.Annotations[gameKruiseV1alpha1.GameServerNetworkType] != "" {
		oldTime, err := time.Parse(TimeFormat, pod.Annotations[gameKruiseV1alpha1.GameServerNetworkTriggerTime])
		if err != nil {
			manager.logger.Error(err, "failed to parse previous network trigger time",
				telemetryfields.FieldGameServerNamespace, gs.Namespace,
				telemetryfields.FieldGameServerName, gs.Name)
			newAnnotations[gameKruiseV1alpha1.GameServerNetworkTriggerTime] = time.Now().Format(TimeFormat)
		} else {
			timeSinceOldTrigger := time.Since(oldTime)
			timeSinceNetworkTransition := time.Since(gs.Status.NetworkStatus.LastTransitionTime.Time)
			if timeSinceOldTrigger > NetworkIntervalTime && timeSinceNetworkTransition < NetworkTotalWaitTime {
				manager.logger.V(4).Info("network trigger conditions met, updating trigger time",
					telemetryfields.FieldGameServerNamespace, gs.Namespace,
					telemetryfields.FieldGameServerName, gs.Name)
				newAnnotations[gameKruiseV1alpha1.GameServerNetworkTriggerTime] = time.Now().Format(TimeFormat)
			}
		}
	}

	// sync annotations from gs to pod
	for gsKey, gsValue := range gs.GetAnnotations() {
		if util.IsHasPrefixGsSyncToPod(gsKey) {
			podValue, exist := pod.GetAnnotations()[gsKey]
			if exist && (podValue == gsValue) {
				continue
			}
			newAnnotations[gsKey] = gsValue
		}
	}

	// sync labels from gs to pod
	for gsKey, gsValue := range gs.GetLabels() {
		if util.IsHasPrefixGsSyncToPod(gsKey) {
			podValue, exist := pod.GetLabels()[gsKey]
			if exist && (podValue == gsValue) {
				continue
			}
			newLabels[gsKey] = gsValue
		}
	}

	// sync pod containers when the containers(images) in GameServer are different from that in pod.
	containers := manager.syncPodContainers(gs.Spec.Containers, pod.DeepCopy().Spec.Containers)

	if len(newLabels) != 0 || len(newAnnotations) != 0 || containers != nil {
		addManagerSpanEvent(ctx, "gameserver.manager.patch_pod",
			tracing.AttrGameServerName(gs.GetName()),
			attribute.String("pod.name", pod.GetName()),
			attribute.Int("labels", len(newLabels)),
			attribute.Int("annotations", len(newAnnotations)),
			attribute.Bool("containersUpdated", containers != nil),
		)

		// Add traceparent annotation to propagate trace context to Webhook
		spanContext := trace.SpanContextFromContext(ctx)
		if spanContext.IsValid() {
			traceparent := tracing.GenerateTraceparent(spanContext)
			if traceparent != "" {
				if len(newAnnotations) == 0 {
					newAnnotations = make(map[string]string)
				}
				newAnnotations["game.kruise.io/traceparent"] = traceparent
			}
		}

		patchPod := make(map[string]interface{})
		if len(newLabels) != 0 || len(newAnnotations) != 0 {
			patchPod["metadata"] = map[string]map[string]string{"labels": newLabels, "annotations": newAnnotations}
		}
		if containers != nil {
			patchPod["spec"] = map[string]interface{}{"containers": containers}
		}
		patchPodBytes, err := json.Marshal(patchPod)
		if err != nil {
			return err
		}
		err = manager.client.Patch(ctx, pod, client.RawPatch(types.StrategicMergePatchType, patchPodBytes))
		if err != nil && !errors.IsNotFound(err) {
			manager.logger.Error(err, "failed to patch Pod",
				telemetryfields.FieldGameServerNamespace, pod.GetNamespace(),
				telemetryfields.FieldGameServerName, pod.GetName())
			return err
		}
	}

	return nil
}

func (manager GameServerManager) SyncPodToGs(ctx context.Context, gss *gameKruiseV1alpha1.GameServerSet) error {
	gs := manager.gameServer
	pod := manager.pod
	oldGsSpec := gs.Spec.DeepCopy()
	oldGsLabels := gs.GetLabels()
	oldGsAnnotations := gs.GetAnnotations()
	oldGsStatus := *gs.Status.DeepCopy()

	// sync DeletePriority/UpdatePriority/State
	podLabels := pod.GetLabels()
	podDeletePriority := intstr.FromString(podLabels[gameKruiseV1alpha1.GameServerDeletePriorityKey])
	podUpdatePriority := intstr.FromString(podLabels[gameKruiseV1alpha1.GameServerUpdatePriorityKey])
	podGsState := gameKruiseV1alpha1.GameServerState(podLabels[gameKruiseV1alpha1.GameServerStateKey])

	// sync Service Qualities
	sqConditions := syncServiceQualities(gss.Spec.ServiceQualities, pod.Status.Conditions, gs)

	// sync Metadata from Gss
	if isNeedToSyncMetadata(gss, gs) {
		gsMetadata := syncMetadataFromGss(gss)
		gs.SetLabels(util.MergeMapString(gs.GetLabels(), gsMetadata.GetLabels()))
		gs.SetAnnotations(util.MergeMapString(gs.GetAnnotations(), gsMetadata.GetAnnotations()))
	}

	if !reflect.DeepEqual(oldGsSpec, gs.Spec) || !reflect.DeepEqual(oldGsLabels, gs.GetLabels()) || !reflect.DeepEqual(oldGsAnnotations, gs.GetAnnotations()) {
		// Build a minimal patch to avoid clobbering fields updated concurrently by users/tests.
		// Only include fields we actually changed and that the controller owns.
		patch := make(map[string]interface{})

		// Spec subfields: restrict to opsState, updatePriority, deletionPriority, networkDisabled
		if !reflect.DeepEqual(oldGsSpec, gs.Spec) {
			specPatch := make(map[string]interface{})
			// opsState
			if oldGsSpec.OpsState != gs.Spec.OpsState {
				specPatch["opsState"] = gs.Spec.OpsState
			}
			// updatePriority
			if (oldGsSpec.UpdatePriority == nil) != (gs.Spec.UpdatePriority == nil) ||
				(oldGsSpec.UpdatePriority != nil && gs.Spec.UpdatePriority != nil && *oldGsSpec.UpdatePriority != *gs.Spec.UpdatePriority) {
				specPatch["updatePriority"] = gs.Spec.UpdatePriority
			}
			// deletionPriority
			if (oldGsSpec.DeletionPriority == nil) != (gs.Spec.DeletionPriority == nil) ||
				(oldGsSpec.DeletionPriority != nil && gs.Spec.DeletionPriority != nil && *oldGsSpec.DeletionPriority != *gs.Spec.DeletionPriority) {
				specPatch["deletionPriority"] = gs.Spec.DeletionPriority
			}
			// networkDisabled
			oldNetworkDisabled := ptr.Deref(oldGsSpec.NetworkDisabled, false)
			newNetworkDisabled := ptr.Deref(gs.Spec.NetworkDisabled, false)
			if (oldGsSpec.NetworkDisabled == nil) != (gs.Spec.NetworkDisabled == nil) || oldNetworkDisabled != newNetworkDisabled {
				specPatch["networkDisabled"] = gs.Spec.NetworkDisabled
			}
			if len(specPatch) > 0 {
				patch["spec"] = specPatch
			}
		}

		// Metadata changes (labels/annotations) are safe to include fully
		if !reflect.DeepEqual(oldGsLabels, gs.GetLabels()) || !reflect.DeepEqual(oldGsAnnotations, gs.GetAnnotations()) {
			patch["metadata"] = map[string]interface{}{
				"labels":      gs.GetLabels(),
				"annotations": gs.GetAnnotations(),
			}
		}

		if len(patch) > 0 {
			specFieldCount := 0
			if specPatch, ok := patch["spec"].(map[string]interface{}); ok {
				specFieldCount = len(specPatch)
			}
			_, metadataChanged := patch["metadata"]
			addManagerSpanEvent(ctx, "gameserver.manager.patch_gameserver",
				tracing.AttrGameServerName(gs.GetName()),
				attribute.Int("specFields", specFieldCount),
				attribute.Bool("metadataChanged", metadataChanged),
			)
			jsonPatchSpec, err := json.Marshal(patch)
			if err != nil {
				return err
			}
			err = manager.client.Patch(ctx, gs, client.RawPatch(types.MergePatchType, jsonPatchSpec))
			if err != nil && !errors.IsNotFound(err) {
				manager.logger.Error(err, "failed to patch GameServer spec/metadata",
					telemetryfields.FieldGameServerNamespace, gs.GetNamespace(),
					telemetryfields.FieldGameServerName, gs.GetName())
				return err
			}
		}
	}

	// get gs conditions
	conditions, err := getConditions(ctx, manager.client, gs, manager.eventRecorder)
	if err != nil {
		manager.logger.Error(err, "failed to get GameServer conditions",
			telemetryfields.FieldGameServerNamespace, gs.GetNamespace(),
			telemetryfields.FieldGameServerName, gs.GetName())
		return err
	}

	// patch gs status
	newStatus := gameKruiseV1alpha1.GameServerStatus{
		PodStatus:                 pod.Status,
		CurrentState:              podGsState,
		DesiredState:              gameKruiseV1alpha1.Ready,
		UpdatePriority:            &podUpdatePriority,
		DeletionPriority:          &podDeletePriority,
		ServiceQualitiesCondition: sqConditions,
		NetworkStatus:             manager.syncNetworkStatus(),
		LastTransitionTime:        oldGsStatus.LastTransitionTime,
		Conditions:                conditions,
	}
	if !reflect.DeepEqual(oldGsStatus, newStatus) {
		newStatus.LastTransitionTime = metav1.Now()
		patchStatus := map[string]interface{}{"status": newStatus}
		addManagerSpanEvent(ctx, "gameserver.manager.patch_status",
			tracing.AttrGameServerName(gs.GetName()),
			attribute.String("currentState", string(newStatus.CurrentState)),
			attribute.String("desiredState", string(newStatus.DesiredState)),
			attribute.String("network.desired", string(newStatus.NetworkStatus.DesiredNetworkState)),
			attribute.String("network.current", string(newStatus.NetworkStatus.CurrentNetworkState)),
		)
		jsonPatchStatus, err := json.Marshal(patchStatus)
		if err != nil {
			return err
		}
		err = manager.client.Status().Patch(ctx, gs, client.RawPatch(types.MergePatchType, jsonPatchStatus))
		if err != nil && !errors.IsNotFound(err) {
			manager.logger.Error(err, "failed to patch GameServer status",
				telemetryfields.FieldGameServerNamespace, gs.GetNamespace(),
				telemetryfields.FieldGameServerName, gs.GetName())
			return err
		}
	}

	return nil
}

func (manager GameServerManager) WaitOrNot() bool {
	networkStatus := manager.gameServer.Status.NetworkStatus
	alreadyWait := time.Since(networkStatus.LastTransitionTime.Time)
	if networkStatus.DesiredNetworkState != networkStatus.CurrentNetworkState {
		if alreadyWait < NetworkTotalWaitTime {
			manager.logger.Info("waiting for network state",
				telemetryfields.FieldGameServerNamespace, manager.gameServer.GetNamespace(),
				telemetryfields.FieldGameServerName, manager.gameServer.GetName(),
				telemetryfields.FieldDesired, networkStatus.DesiredNetworkState,
				telemetryfields.FieldCurrent, networkStatus.CurrentNetworkState,
				telemetryfields.FieldRemaining, NetworkTotalWaitTime-alreadyWait)
			return true
		} else {
			manager.eventRecorder.Eventf(manager.gameServer, corev1.EventTypeWarning, GsNetworkStateReason, "Network wait timeout: waited %v, max %v", alreadyWait, NetworkTotalWaitTime)
		}
	}
	return false
}

func addManagerSpanEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return
	}
	span.AddEvent(name, trace.WithAttributes(attrs...))
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
		gsNetworkStatus.CurrentNetworkState = gameKruiseV1alpha1.NetworkNotReady
		gsNetworkStatus.LastTransitionTime = metav1.Now()
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

func syncServiceQualities(serviceQualities []gameKruiseV1alpha1.ServiceQuality, podConditions []corev1.PodCondition, gs *gameKruiseV1alpha1.GameServer) []gameKruiseV1alpha1.ServiceQualityCondition {
	var newGsConditions []gameKruiseV1alpha1.ServiceQualityCondition
	sqConditionsMap := make(map[string]gameKruiseV1alpha1.ServiceQualityCondition)
	for _, sqc := range gs.Status.ServiceQualitiesCondition {
		sqConditionsMap[sqc.Name] = sqc
	}
	timeNow := metav1.Now()
	for _, sq := range serviceQualities {
		var newSqCondition gameKruiseV1alpha1.ServiceQualityCondition
		newSqCondition.Name = sq.Name
		index, podCondition := util.GetPodConditionFromList(podConditions, corev1.PodConditionType(util.AddPrefixGameKruise(sq.Name)))
		if index != -1 {
			podConditionMessage := strings.ReplaceAll(podCondition.Message, "|", "")
			podConditionMessage = strings.ReplaceAll(podConditionMessage, "\n", "")
			newSqCondition.Status = string(podCondition.Status)
			newSqCondition.Result = podConditionMessage
			newSqCondition.LastProbeTime = podCondition.LastProbeTime
			var lastActionTransitionTime metav1.Time
			sqCondition, exist := sqConditionsMap[sq.Name]
			if !exist || ((sqCondition.Status != string(podCondition.Status) || (sqCondition.Result != podConditionMessage)) && (sqCondition.LastActionTransitionTime.IsZero() || !sq.Permanent)) {
				// exec action (only apply fields explicitly set in action)
				for _, action := range sq.ServiceQualityAction {
					state, err := strconv.ParseBool(string(podCondition.Status))
					if err == nil && state == action.State && (action.Result == "" || podConditionMessage == action.Result) {
						if action.DeletionPriority != nil {
							gs.Spec.DeletionPriority = action.DeletionPriority
						}
						if action.UpdatePriority != nil {
							gs.Spec.UpdatePriority = action.UpdatePriority
						}
						if action.OpsState != "" {
							gs.Spec.OpsState = action.OpsState
						}
						if action.NetworkDisabled != nil {
							gs.Spec.NetworkDisabled = ptr.To(ptr.Deref(action.NetworkDisabled, false))
						}
						gs.SetLabels(util.MergeMapString(gs.GetLabels(), action.Labels))
						gs.SetAnnotations(util.MergeMapString(gs.GetAnnotations(), action.Annotations))
						lastActionTransitionTime = timeNow
					}
				}
			} else {
				lastActionTransitionTime = sqCondition.LastActionTransitionTime
			}
			newSqCondition.LastActionTransitionTime = lastActionTransitionTime
		}

		// Set LastTransitionTime, which depends on which value, the LastActionTransitionTime or LastProbeTime, is closer to the current time.
		if timeNow.Sub(newSqCondition.LastActionTransitionTime.Time) < timeNow.Sub(newSqCondition.LastProbeTime.Time) {
			newSqCondition.LastTransitionTime = newSqCondition.LastActionTransitionTime
		} else {
			newSqCondition.LastTransitionTime = newSqCondition.LastProbeTime
		}
		newGsConditions = append(newGsConditions, newSqCondition)
	}
	return newGsConditions
}

func (manager GameServerManager) syncPodContainers(gsContainers []gameKruiseV1alpha1.GameServerContainer, podContainers []corev1.Container) []corev1.Container {
	var newContainers []corev1.Container
	for _, podContainer := range podContainers {
		for _, gsContainer := range gsContainers {
			if gsContainer.Name == podContainer.Name {
				var newContainer corev1.Container
				newContainer.Name = podContainer.Name
				changed := false
				if gsContainer.Image != "" && gsContainer.Image != podContainer.Image {
					newContainer.Image = gsContainer.Image
					changed = true
				}

				if changed {
					newContainers = append(newContainers, newContainer)
				}
			}
		}
	}

	return newContainers
}

func NewGameServerManager(gs *gameKruiseV1alpha1.GameServer, pod *corev1.Pod, c client.Client, recorder record.EventRecorder, logger logr.Logger) Control {
	return &GameServerManager{
		gameServer:    gs,
		pod:           pod,
		client:        c,
		eventRecorder: recorder,
		logger:        logger,
	}
}
