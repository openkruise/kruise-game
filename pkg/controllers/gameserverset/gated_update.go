/*
Copyright 2026 The Kruise Authors.

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
	"encoding/json"
	"fmt"
	"time"

	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruisev1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gamev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/telemetryfields"
	"github.com/openkruise/kruise-game/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	updateProgressingCondition      = util.ServiceQualityUpdateProgressing
	observationCheckpointKey        = "game.kruise.io/update-observation-checkpoint"
	gatedTemplateHashKey            = "game.kruise.io/gated-template-hash"
	maxCheckpointWritesPerReconcile = 32
)

type observationCheckpoint struct {
	UpdateGeneration int64       `json:"updateGeneration"`
	PodUID           string      `json:"podUID"`
	Revision         string      `json:"revision"`
	ObservedAt       metav1.Time `json:"observedAt"`
}

// reconcileGatedUpdate owns ASTS update partition while a gated rollout is active.
// handled=false means the legacy update path should handle this reconcile.
func (r *GameServerSetReconciler) reconcileGatedUpdate(ctx context.Context, gss *gamev1alpha1.GameServerSet, asts *kruisev1beta1.StatefulSet, pods []corev1.Pod) (handled bool, result ctrl.Result, err error) {
	ru := gss.Spec.UpdateStrategy.RollingUpdate
	if ru == nil || ru.CompletionPolicy == nil {
		return false, ctrl.Result{}, nil
	}
	if gss.Spec.Replicas == nil || asts.Spec.Replicas == nil {
		return true, ctrl.Result{}, fmt.Errorf("gated update requires known replica counts")
	}

	expected := util.GetNewAstsFromGss(gss.DeepCopy(), asts.DeepCopy())
	// The API server defaults the ASTS PodTemplate, so comparing it directly
	// with the template built from GSS would report a change on every reconcile.
	desiredTemplateHash := util.GetHash(expected.Spec.Template)
	observedTemplateHash := asts.Annotations[gatedTemplateHashKey]
	if observedTemplateHash == "" {
		// Older ASTS objects lack the template fingerprint. Compare a server-
		// defaulted, dry-run candidate to the live template so a simultaneous
		// policy/template change can still start with a closed window.
		if asts.Annotations[gamev1alpha1.AstsHashKey] != util.GetAstsHash(gss) {
			candidate := asts.DeepCopy()
			candidate.Spec.Template = expected.Spec.Template
			if err := r.Update(ctx, candidate, client.DryRunAll); err != nil {
				return true, ctrl.Result{RequeueAfter: 10 * time.Second}, fmt.Errorf("default previous ASTS template with dry-run update: %w", err)
			}
			if !equality.Semantic.DeepEqual(candidate.Spec.Template, asts.Spec.Template) {
				observedTemplateHash = "legacy-template-differs"
			}
		}
		if observedTemplateHash == "" {
			copy := asts.DeepCopy()
			if copy.Annotations == nil {
				copy.Annotations = make(map[string]string)
			}
			copy.Annotations[gatedTemplateHashKey] = desiredTemplateHash
			return true, ctrl.Result{Requeue: true}, r.Update(ctx, copy)
		}
	}
	templateChanged := desiredTemplateHash != observedTemplateHash
	progress := gss.Status.UpdateProgress
	if templateChanged {
		if progress == nil || progress.UpdateGeneration != gss.Generation {
			gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{
				UpdateGeneration: gss.Generation,
				StartedAt:        metav1.Now(),
			}
			return true, ctrl.Result{Requeue: true}, r.updateGatedStatus(
				ctx, gss, "PreparingUpdate", "Recording the new update before changing the workload", metav1.ConditionTrue,
			)
		}
		// Template and closed partition are written in one ASTS API update.
		expected.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(*gss.Spec.Replicas)
		setAstsHash(expected, gss)
		return true, ctrl.Result{Requeue: true}, r.Update(ctx, expected)
	}
	if progress == nil {
		if condition := meta.FindStatusCondition(gss.Status.Conditions, updateProgressingCondition); condition != nil && condition.Status != metav1.ConditionFalse {
			return true, ctrl.Result{RequeueAfter: 10 * time.Second}, r.updateGatedStatus(
				ctx, gss, "MissingUpdateProgress", "Active update progress is missing; refusing to release more slots", metav1.ConditionUnknown,
			)
		}
		return false, ctrl.Result{}, nil // Policy added without a new template: do not take over an existing update.
	}
	if progress.UpdateGeneration <= 0 || progress.UpdateGeneration > gss.Generation {
		return true, ctrl.Result{RequeueAfter: 10 * time.Second}, r.updateGatedStatus(
			ctx, gss, "InvalidUpdateProgress", "Update progress is inconsistent; refusing to release more slots", metav1.ConditionUnknown,
		)
	}

	if asts.Spec.UpdateStrategy.RollingUpdate == nil || asts.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
		return true, ctrl.Result{RequeueAfter: 5 * time.Second}, r.updateGatedStatus(
			ctx, gss, "InvalidPartition", "ASTS partition is missing", metav1.ConditionUnknown,
		)
	}
	currentPartition := *asts.Spec.UpdateStrategy.RollingUpdate.Partition
	targetPartition := ptr.Deref(ru.Partition, 0)
	replicas := *gss.Spec.Replicas
	if targetPartition < 0 || targetPartition > replicas || currentPartition < 0 || currentPartition > replicas {
		return true, ctrl.Result{RequeueAfter: 5 * time.Second}, r.updateGatedStatus(
			ctx, gss, "InvalidPartition", "GSS or ASTS partition is outside the valid range", metav1.ConditionUnknown,
		)
	}
	// A completed rollout may prepare a higher partition through ordinary
	// strategy sync. Lowering it still opens more Pods and must remain gated.
	if !util.IsServiceQualityGatedUpdateActive(gss) && targetPartition >= currentPartition {
		return false, ctrl.Result{}, nil
	}
	if currentPartition < targetPartition {
		return true, ctrl.Result{RequeueAfter: 5 * time.Second}, r.updateGatedStatus(
			ctx, gss, "InvalidPartition", "GSS or ASTS partition is outside the valid range", metav1.ConditionUnknown,
		)
	}
	if *asts.Spec.Replicas != replicas || len(pods) != int(replicas) {
		return true, ctrl.Result{RequeueAfter: 5 * time.Second}, r.updateGatedStatus(
			ctx, gss, "WaitingForReplicas", "Waiting for ASTS and Pod count to converge", metav1.ConditionTrue,
		)
	}

	// Non-template strategy edits must not reset the partition managed by this controller.
	if asts.Annotations[gamev1alpha1.AstsHashKey] != util.GetAstsHash(gss) {
		expected.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(currentPartition)
		setAstsHash(expected, gss)
		return true, ctrl.Result{Requeue: true}, r.Update(ctx, expected)
	}
	if asts.Status.ObservedGeneration < asts.Generation || asts.Status.UpdateRevision == "" {
		return true, ctrl.Result{RequeueAfter: 5 * time.Second}, r.updateGatedStatus(
			ctx, gss, "WaitingForRevision", "Waiting for ASTS to recognize the target revision", metav1.ConditionTrue,
		)
	}
	limit, err := gatedMaxUnavailable(ru.MaxUnavailable, replicas)
	if err != nil {
		return true, ctrl.Result{RequeueAfter: 5 * time.Second}, r.updateGatedStatus(ctx, gss, "InvalidMaxUnavailable", err.Error(), metav1.ConditionUnknown)
	}
	completed, nextCheck, checkpointsWritten, err := r.observeUpdatedPods(ctx, gss, asts.Status.UpdateRevision, pods, time.Now())
	if err != nil {
		return true, ctrl.Result{}, err
	}
	if checkpointsWritten {
		return true, ctrl.Result{Requeue: true}, nil
	}
	opened := replicas - currentPartition
	if completed > opened {
		completed = opened
	}
	pending := opened - completed
	progress.CompletedReplicas = completed
	progress.PendingCompletionReplicas = pending

	if !ru.Paused {
		allowed := completed + limit
		if maxAllowed := replicas - targetPartition; allowed > maxAllowed {
			allowed = maxAllowed
		}
		if desiredPartition := replicas - allowed; desiredPartition < currentPartition {
			copy := asts.DeepCopy()
			copy.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(desiredPartition)
			if err := r.Update(ctx, copy); err != nil {
				return true, ctrl.Result{}, fmt.Errorf("advance ASTS %s partition from %d to %d: %w", client.ObjectKeyFromObject(asts), currentPartition, desiredPartition, err)
			}
			logging.FromContextWithTrace(ctx).Info("Advanced ServiceQuality-gated update partition",
				telemetryfields.FieldGameServerSetNamespace, gss.Namespace,
				telemetryfields.FieldGameServerSetName, gss.Name,
				telemetryfields.FieldUpdatePartitionOld, currentPartition,
				telemetryfields.FieldUpdatePartitionNew, desiredPartition,
				telemetryfields.FieldUpdatePartitionTarget, targetPartition,
				telemetryfields.FieldUpdateCompletedReplicas, completed,
				telemetryfields.FieldUpdatePendingReplicas, pending,
				telemetryfields.FieldStrategyMaxUnavailable, limit,
			)
			return true, ctrl.Result{Requeue: true}, r.updateGatedStatus(ctx, gss, "Updating", "Opened the next business observation slot", metav1.ConditionTrue)
		}
	}
	status := metav1.ConditionTrue
	reason, message := "WaitingForBusinessObservation", "Waiting for updated GameServers to pass ServiceQuality observation"
	if currentPartition == targetPartition && pending == 0 {
		status, reason, message = metav1.ConditionFalse, "Completed", "All released GameServers passed business observation"
	} else if ru.Paused {
		reason, message = "Paused", "Update is paused"
	}
	if err := r.updateGatedStatus(ctx, gss, reason, message, status); err != nil {
		return true, ctrl.Result{}, err
	}
	if !nextCheck.IsZero() {
		return true, ctrl.Result{RequeueAfter: max(time.Until(nextCheck), time.Second)}, nil
	}
	if status == metav1.ConditionTrue {
		return true, ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return true, ctrl.Result{}, nil
}

func setAstsHash(asts *kruisev1beta1.StatefulSet, gss *gamev1alpha1.GameServerSet) {
	if asts.Annotations == nil {
		asts.Annotations = make(map[string]string)
	}
	asts.Annotations[gamev1alpha1.AstsHashKey] = util.GetAstsHash(gss)
	asts.Annotations[gatedTemplateHashKey] = util.GetHash(asts.Spec.Template)
}

func (r *GameServerSetReconciler) updateGatedStatus(ctx context.Context, gss *gamev1alpha1.GameServerSet, reason, message string, status metav1.ConditionStatus) error {
	current := &gamev1alpha1.GameServerSet{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(gss), current); err != nil {
		return fmt.Errorf("read GSS %s before updating gated status: %w", client.ObjectKeyFromObject(gss), err)
	}
	if current.Generation != gss.Generation {
		return nil // A newer GSS update will reconcile its own status.
	}
	original := current.DeepCopy()
	current.Status.UpdateProgress = gss.Status.UpdateProgress.DeepCopy()
	meta.SetStatusCondition(&current.Status.Conditions, metav1.Condition{
		Type: updateProgressingCondition, Status: status,
		ObservedGeneration: gss.Generation, Reason: reason, Message: message,
	})
	if equality.Semantic.DeepEqual(original.Status, current.Status) {
		gss.Status = current.Status
		gss.ResourceVersion = current.ResourceVersion
		return nil
	}
	// A new GSS may not have a status object yet. A merge patch containing only
	// updateProgress/conditions would omit the required replica counters and be
	// rejected by the CRD schema; a full status update includes their zero values.
	if err := r.Status().Update(ctx, current); err != nil {
		return fmt.Errorf("update gated status for GSS %s with reason %s: %w", client.ObjectKeyFromObject(gss), reason, err)
	}
	oldCondition := meta.FindStatusCondition(original.Status.Conditions, updateProgressingCondition)
	conditionChanged := oldCondition == nil || oldCondition.Status != status || oldCondition.Reason != reason
	resumed := oldCondition != nil && oldCondition.Reason == "Paused" &&
		status == metav1.ConditionTrue && reason != "Paused" && reason != "PreparingUpdate"
	// Keep routine waiting and slot releases in Conditions and logs; emit
	// events only for lifecycle changes and new or changed warnings.
	if r.recorder != nil && conditionChanged &&
		(status == metav1.ConditionUnknown || reason == "PreparingUpdate" || reason == "Paused" || reason == "Completed" || resumed) {
		eventType := corev1.EventTypeNormal
		if status == metav1.ConditionUnknown {
			eventType = corev1.EventTypeWarning
		}
		eventReason := reason
		if resumed {
			eventReason = "Resumed"
		}
		r.recorder.Eventf(current, eventType, "UpdateProgressChanged", "%s: %s", eventReason, message)
	}
	gss.Status = current.Status
	gss.ResourceVersion = current.ResourceVersion
	return nil
}

func gatedMaxUnavailable(value *intstr.IntOrString, replicas int32) (int32, error) {
	if value == nil {
		return 1, nil
	}
	n, err := intstr.GetScaledValueFromIntOrPercent(value, int(replicas), false)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		n = 1
	}
	return int32(n), nil
}

func gatedPartitionAfterScale(oldReplicas, oldPartition, newReplicas, targetPartition int32) int32 {
	partition := newReplicas - (oldReplicas - oldPartition)
	if partition < targetPartition {
		partition = targetPartition
	}
	if partition > newReplicas {
		partition = newReplicas
	}
	return partition
}

func (r *GameServerSetReconciler) observeUpdatedPods(ctx context.Context, gss *gamev1alpha1.GameServerSet, revision string, pods []corev1.Pod, now time.Time) (completed int32, nextCheck time.Time, checkpointsWritten bool, err error) {
	writes := 0
	for i := range pods {
		pod := &pods[i]
		if !podUpdateFinished(pod, revision) {
			continue
		}
		checkpoint, valid := readObservationCheckpoint(pod, gss.Status.UpdateProgress.UpdateGeneration, revision)
		if !valid {
			if writes == maxCheckpointWritesPerReconcile {
				return completed, nextCheck, true, nil
			}
			checkpoint = observationCheckpoint{
				UpdateGeneration: gss.Status.UpdateProgress.UpdateGeneration,
				PodUID:           string(pod.UID),
				Revision:         revision,
				ObservedAt:       metav1.NewTime(now),
			}
			serialized, marshalErr := json.Marshal(checkpoint)
			if marshalErr != nil {
				return 0, time.Time{}, false, fmt.Errorf("serialize observation checkpoint for Pod %s: %w", client.ObjectKeyFromObject(pod), marshalErr)
			}
			old := pod.DeepCopy()
			if pod.Annotations == nil {
				pod.Annotations = make(map[string]string)
			}
			pod.Annotations[observationCheckpointKey] = string(serialized)
			if patchErr := r.Patch(ctx, pod, client.MergeFromWithOptions(old, client.MergeFromWithOptimisticLock{})); patchErr != nil {
				return 0, time.Time{}, false, fmt.Errorf("write observation checkpoint for Pod %s: %w", client.ObjectKeyFromObject(pod), patchErr)
			}
			writes++
			checkpointsWritten = true
			continue
		}
		passed, due := gameServerObservationPassed(gss, pod, checkpoint, now)
		if passed {
			completed++
		} else if !due.IsZero() && (nextCheck.IsZero() || due.Before(nextCheck)) {
			nextCheck = due
		}
	}
	return completed, nextCheck, checkpointsWritten, nil
}

func readObservationCheckpoint(pod *corev1.Pod, generation int64, revision string) (observationCheckpoint, bool) {
	var checkpoint observationCheckpoint
	if err := json.Unmarshal([]byte(pod.Annotations[observationCheckpointKey]), &checkpoint); err != nil {
		return checkpoint, false
	}
	return checkpoint, checkpoint.UpdateGeneration == generation &&
		checkpoint.PodUID == string(pod.UID) &&
		checkpoint.Revision == revision &&
		!checkpoint.ObservedAt.IsZero()
}

func podUpdateFinished(pod *corev1.Pod, revision string) bool {
	if pod.DeletionTimestamp != nil || pod.Labels[appsv1.ControllerRevisionHashLabelKey] != revision {
		return false
	}
	if _, grace := appspub.GetInPlaceUpdateGrace(pod); grace {
		return false
	}
	if raw, exists := appspub.GetInPlaceUpdateState(pod); exists {
		var state appspub.InPlaceUpdateState
		if json.Unmarshal([]byte(raw), &state) != nil || state.Revision != revision ||
			len(state.NextContainerImages) > 0 || len(state.NextContainerRefMetadata) > 0 ||
			len(state.NextContainerResources) > 0 || state.PreCheckBeforeNext != nil {
			return false
		}
		_, condition := util.GetPodConditionFromList(pod.Status.Conditions, appspub.InPlaceUpdateReady)
		return condition != nil && condition.Status == corev1.ConditionTrue
	}
	if _, condition := util.GetPodConditionFromList(pod.Status.Conditions, appspub.InPlaceUpdateReady); condition != nil && condition.Status != corev1.ConditionTrue {
		return false
	}
	return true
}

func gameServerObservationPassed(gss *gamev1alpha1.GameServerSet, pod *corev1.Pod, checkpoint observationCheckpoint, now time.Time) (bool, time.Time) {
	due := checkpoint.ObservedAt.Time
	matches := true
	for _, required := range gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities {
		var probe *gamev1alpha1.ServiceQuality
		for i := range gss.Spec.ServiceQualities {
			if gss.Spec.ServiceQualities[i].Name == required.Name {
				probe = &gss.Spec.ServiceQualities[i]
				break
			}
		}
		if probe == nil || probe.ContainerName == "" {
			return false, time.Time{}
		}
		var startedAt time.Time
		containerFound := false
		for _, container := range pod.Status.ContainerStatuses {
			if container.Name == probe.ContainerName {
				containerFound = true
				if container.State.Running != nil {
					startedAt = container.State.Running.StartedAt.Time
				}
				break
			}
		}
		if !containerFound || (required.State && startedAt.IsZero()) {
			return false, time.Time{}
		}
		_, condition := util.GetPodConditionFromList(pod.Status.Conditions, corev1.PodConditionType(util.AddPrefixGameKruise(required.Name)))
		if condition == nil || !util.MatchServiceQualityResult(condition.Status, condition.Message, required.State, required.Result) {
			matches = false
		}
		// Only a matching condition newer than both this observation and the
		// current container start may pass early. An unchanged matching condition
		// can still pass after the fallback window without a new transition.
		if condition != nil && condition.LastTransitionTime.After(checkpoint.ObservedAt.Time) &&
			(startedAt.IsZero() || condition.LastTransitionTime.After(startedAt)) &&
			util.MatchServiceQualityResult(condition.Status, condition.Message, required.State, required.Result) {
			continue
		}
		probeDue := checkpoint.ObservedAt.Time
		if window := gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds; window != nil {
			if startedAt.After(probeDue) {
				probeDue = startedAt
			}
			probeDue = probeDue.Add(time.Duration(*window) * time.Second)
		} else {
			period := probe.PeriodSeconds
			if period == 0 {
				period = DefaultPeriodSeconds
			}
			initialDelay := probe.InitialDelaySeconds
			if initialDelay == 0 {
				initialDelay = DefaultInitialDelaySeconds
			}
			failureThreshold := probe.FailureThreshold
			if failureThreshold == 0 {
				failureThreshold = DefaultFailureThreshold
			}
			timeout := probe.TimeoutSeconds
			if timeout == 0 {
				timeout = DefaultTimeoutSeconds
			}
			if !startedAt.IsZero() {
				if delayEnd := startedAt.Add(time.Duration(initialDelay) * time.Second); delayEnd.After(probeDue) {
					probeDue = delayEnd
				}
			}
			// Allow a full scheduling interval and execution timeout per failure,
			// including the wait before the first probe, plus writeback slack.
			seconds := (int64(period)+int64(timeout))*int64(failureThreshold) + 2
			probeDue = probeDue.Add(time.Duration(seconds) * time.Second)
		}
		if probeDue.After(due) {
			due = probeDue
		}
	}
	if now.Before(due) {
		return false, due
	}
	return matches, time.Time{}
}
