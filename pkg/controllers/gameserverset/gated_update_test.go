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
	"testing"
	"time"

	"github.com/go-logr/logr"
	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruisev1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gamev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testGatedGSS() *gamev1alpha1.GameServerSet {
	return &gamev1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", Generation: 3},
		Spec: gamev1alpha1.GameServerSetSpec{
			Replicas: ptr.To[int32](2),
			GameServerTemplate: gamev1alpha1.GameServerTemplate{PodTemplateSpec: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main", Image: "v2"}}},
			}},
			ServiceQualities: []gamev1alpha1.ServiceQuality{{Name: "ready", ContainerName: "main", Probe: corev1.Probe{PeriodSeconds: 3, InitialDelaySeconds: 1}}},
			UpdateStrategy: gamev1alpha1.UpdateStrategy{RollingUpdate: &gamev1alpha1.RollingUpdateStatefulSetStrategy{
				CompletionPolicy: &gamev1alpha1.UpdateCompletionPolicy{
					ServiceQualities: []gamev1alpha1.ServiceQualityCompletionCondition{
						{
							Name:   "ready",
							State:  true,
							Result: "ready",
						},
					},
				},
			}},
		},
	}
}

func TestPodUpdateFinished(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{appsv1.ControllerRevisionHashLabelKey: "new"}}}
	if !podUpdateFinished(pod, "new") {
		t.Fatal("recreated pod at target revision should finish without in-place state")
	}
	state, err := json.Marshal(appspub.InPlaceUpdateState{Revision: "new"})
	if err != nil {
		t.Fatal(err)
	}
	pod.Annotations = map[string]string{appspub.InPlaceUpdateStateKey: string(state)}
	if podUpdateFinished(pod, "new") {
		t.Fatal("in-place update without InPlaceUpdateReady must not finish")
	}
	pod.Status.Conditions = []corev1.PodCondition{{Type: appspub.InPlaceUpdateReady, Status: corev1.ConditionTrue}}
	if !podUpdateFinished(pod, "new") {
		t.Fatal("finished in-place update should pass")
	}
	pod.Status.Conditions[0].Status = corev1.ConditionFalse
	if podUpdateFinished(pod, "new") {
		t.Fatal("in-place update marked not-ready must not finish")
	}
}

func TestGatedUpdateIgnoresAPIServerTemplateDefaults(t *testing.T) {
	gss := testGatedGSS()
	asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
	asts.Spec.Replicas = ptr.To[int32](2)
	setAstsHash(asts, gss)
	asts.Spec.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
	asts.Spec.Template.Spec.Containers[0].TerminationMessagePath = corev1.TerminationMessagePathDefault
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(gss, asts).Build()
	r := &GameServerSetReconciler{Client: client}
	handled, _, err := r.reconcileGatedUpdate(context.Background(), gss, asts, nil)
	if err != nil || handled || gss.Status.UpdateProgress != nil {
		t.Fatalf("API defaults must not start a rollout: handled=%v progress=%v err=%v", handled, gss.Status.UpdateProgress, err)
	}
}

func TestGatedUpdateBootstrapsLegacyTemplateIdentity(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "policy only", true: "policy and template"}[changed], func(t *testing.T) {
			gss := testGatedGSS()
			asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
			asts.Spec.Replicas = ptr.To[int32](2)
			asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](0)
			if changed {
				asts.Spec.Template.Spec.Containers[0].Image = "v1"
			}
			asts.Annotations = map[string]string{gamev1alpha1.AstsHashKey: "legacy-hash"}
			client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gamev1alpha1.GameServerSet{}).WithObjects(gss, asts).Build()
			r := &GameServerSetReconciler{Client: client}
			if handled, _, err := r.reconcileGatedUpdate(context.Background(), gss, asts, nil); err != nil || !handled {
				t.Fatalf("legacy bootstrap: handled=%v err=%v", handled, err)
			}
			currentGSS := &gamev1alpha1.GameServerSet{}
			if err := client.Get(context.Background(), types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, currentGSS); err != nil {
				t.Fatal(err)
			}
			if changed != (currentGSS.Status.UpdateProgress != nil) {
				t.Fatalf("template changed=%v, progress=%v", changed, currentGSS.Status.UpdateProgress)
			}
		})
	}
}

func TestGameServerObservationPassed(t *testing.T) {
	gss := testGatedGSS()
	now := time.Now().Truncate(time.Second)
	checkpoint := observationCheckpoint{ObservedAt: metav1.NewTime(now)}
	pod := &corev1.Pod{Status: corev1.PodStatus{
		ContainerStatuses: []corev1.ContainerStatus{
			{
				Name: "main",
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{
						StartedAt: metav1.NewTime(now.Add(-20 * time.Second)),
					},
				},
			},
		},
		Conditions: []corev1.PodCondition{{Type: "game.kruise.io/ready", Status: corev1.ConditionTrue, Message: "ready"}},
	}}
	if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(25*time.Second)); passed {
		t.Fatal("stale success must not pass before the failure threshold can be observed")
	}
	if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(26*time.Second)); !passed {
		t.Fatal("unchanged success should pass after the fallback window")
	}
	pod.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(now.Add(time.Second))
	if passed, due := gameServerObservationPassed(gss, pod, checkpoint, now.Add(26*time.Second)); passed || !due.Equal(now.Add(28*time.Second)) {
		t.Fatalf("restarted probe container must honor initial delay, passed=%v due=%v", passed, due)
	}
	pod.Status.Conditions[0].Status = corev1.ConditionFalse
	pod.Status.Conditions[0].LastTransitionTime = metav1.NewTime(now.Add(2 * time.Second))
	if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(3*time.Second)); passed {
		t.Fatal("a fresh failure must not release the slot")
	}
	pod.Status.Conditions[0].Status = corev1.ConditionTrue
	pod.Status.Conditions[0].LastTransitionTime = metav1.NewTime(now.Add(3 * time.Second))
	if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(4*time.Second)); !passed {
		t.Fatal("fresh false-to-true transition should release the slot before the fallback window")
	}
	pod.Status.Conditions[0].Message = "failed"
	if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(10*time.Second)); passed {
		t.Fatal("result mismatch must not pass")
	}
	gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities[0].State = false
	gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities[0].Result = "offline"
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}
	pod.Status.Conditions[0] = corev1.PodCondition{
		Type:               "game.kruise.io/ready",
		Status:             corev1.ConditionFalse,
		Message:            "offline",
		LastTransitionTime: metav1.NewTime(now.Add(3 * time.Second)),
	}
	if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(4*time.Second)); !passed {
		t.Fatal("a reported false condition may pass when the probed container has terminated")
	}
}

func TestGameServerObservationAfterContainerRestart(t *testing.T) {
	base := time.Date(2026, time.October, 27, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		transition time.Duration
		checkAt    time.Duration
		wantPassed bool
		wantDue    time.Duration
	}{
		{name: "pre-restart success waits", transition: 5 * time.Second, checkAt: 21 * time.Second, wantDue: 76 * time.Second},
		{name: "success at restart waits", transition: 20 * time.Second, checkAt: 21 * time.Second, wantDue: 76 * time.Second},
		{name: "post-restart success passes early", transition: 21 * time.Second, checkAt: 22 * time.Second, wantPassed: true},
		{name: "unchanged success waits until deadline", transition: 5 * time.Second, checkAt: 75 * time.Second, wantDue: 76 * time.Second},
		{name: "unchanged success passes at deadline", transition: 5 * time.Second, checkAt: 76 * time.Second, wantPassed: true},
		{name: "unchanged success stays passed", transition: 5 * time.Second, checkAt: 120 * time.Second, wantPassed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gss := testGatedGSS()
			gss.Spec.ServiceQualities[0].InitialDelaySeconds = 30
			gss.Spec.ServiceQualities[0].FailureThreshold = 3
			checkpoint := observationCheckpoint{ObservedAt: metav1.NewTime(base)}
			pod := &corev1.Pod{Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "main",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{
								StartedAt: metav1.NewTime(base.Add(20 * time.Second)),
							},
						},
					},
				},
				Conditions: []corev1.PodCondition{
					{
						Type:               "game.kruise.io/ready",
						Status:             corev1.ConditionTrue,
						Message:            "ready",
						LastTransitionTime: metav1.NewTime(base.Add(tc.transition)),
					},
				},
			}}
			passed, due := gameServerObservationPassed(gss, pod, checkpoint, base.Add(tc.checkAt))
			var wantDue time.Time
			if tc.wantDue != 0 {
				wantDue = base.Add(tc.wantDue)
			}
			if passed != tc.wantPassed || !due.Equal(wantDue) {
				t.Fatalf("passed=%v due=%v, want passed=%v due=%v", passed, due, tc.wantPassed, wantDue)
			}
		})
	}
}

func TestGameServerObservationUsesEachProbeContainerStart(t *testing.T) {
	gss := testGatedGSS()
	gss.Spec.ServiceQualities[0].InitialDelaySeconds = 30
	gss.Spec.ServiceQualities[0].FailureThreshold = 3
	otherProbe := gss.Spec.ServiceQualities[0].DeepCopy()
	otherProbe.Name, otherProbe.ContainerName = "other-ready", "other"
	gss.Spec.ServiceQualities = append(gss.Spec.ServiceQualities, *otherProbe)
	gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities = append(
		gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities,
		gamev1alpha1.ServiceQualityCompletionCondition{Name: "other-ready", State: true, Result: "ready"},
	)
	base := time.Date(2026, time.October, 27, 0, 0, 0, 0, time.UTC)
	checkpoint := observationCheckpoint{ObservedAt: metav1.NewTime(base)}
	pod := &corev1.Pod{Status: corev1.PodStatus{
		ContainerStatuses: []corev1.ContainerStatus{
			{Name: "main", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(base.Add(-time.Minute))}}},
			{Name: "other", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(base.Add(20 * time.Second))}}},
		},
		Conditions: []corev1.PodCondition{
			{Type: "game.kruise.io/ready", Status: corev1.ConditionTrue, Message: "ready", LastTransitionTime: metav1.NewTime(base.Add(5 * time.Second))},
			{Type: "game.kruise.io/other-ready", Status: corev1.ConditionTrue, Message: "ready", LastTransitionTime: metav1.NewTime(base.Add(5 * time.Second))},
		},
	}}
	if passed, due := gameServerObservationPassed(gss, pod, checkpoint, base.Add(21*time.Second)); passed || !due.Equal(base.Add(76*time.Second)) {
		t.Fatalf("restarted probe must block early completion despite another fresh probe, passed=%v due=%v", passed, due)
	}
	if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, base.Add(76*time.Second)); !passed {
		t.Fatal("all unchanged matching conditions must pass once the restarted probe window expires")
	}
}

func TestGameServerObservationUsesConfiguredFailureThreshold(t *testing.T) {
	gss := testGatedGSS()
	gss.Spec.ServiceQualities[0].PeriodSeconds = 4
	gss.Spec.ServiceQualities[0].FailureThreshold = 2
	now := time.Now().Truncate(time.Second)
	checkpoint := observationCheckpoint{ObservedAt: metav1.NewTime(now)}
	pod := &corev1.Pod{Status: corev1.PodStatus{
		ContainerStatuses: []corev1.ContainerStatus{
			{
				Name: "main",
				State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{
						StartedAt: metav1.NewTime(now.Add(-time.Minute)),
					},
				},
			},
		},
		Conditions: []corev1.PodCondition{
			{
				Type:               "game.kruise.io/ready",
				Status:             corev1.ConditionTrue,
				Message:            "ready",
				LastTransitionTime: metav1.NewTime(now.Add(-time.Minute)),
			},
		},
	}}
	if passed, due := gameServerObservationPassed(gss, pod, checkpoint, now.Add(19*time.Second)); passed || !due.Equal(now.Add(20*time.Second)) {
		t.Fatalf("custom threshold must delay a stale success until 20s, passed=%v due=%v", passed, due)
	}
	if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(20*time.Second)); !passed {
		t.Fatal("matching condition must pass after configured fallback window")
	}
}

func TestGameServerObservationWindow(t *testing.T) {
	base := time.Date(2026, time.October, 28, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name       string
		window     *int32
		timeout    int32
		startedAt  time.Duration
		observedAt time.Duration
		checkAt    time.Duration
		transition time.Duration
		status     corev1.ConditionStatus
		result     string
		wantPassed bool
		wantDue    time.Duration
	}{
		{name: "default window covers slow failures", checkAt: 21 * time.Second, wantDue: 36 * time.Second},
		{name: "default window accepts unchanged success at expiry", checkAt: 36 * time.Second, wantPassed: true},
		{name: "automatic window uses configured timeout", timeout: 20, checkAt: 36 * time.Second, wantDue: 81 * time.Second},
		{name: "default window blocks reported failure at expiry", checkAt: 36 * time.Second, status: corev1.ConditionFalse},
		{name: "custom window waits", window: ptr.To[int32](60), checkAt: 59 * time.Second, wantDue: 60 * time.Second},
		{name: "custom window accepts unchanged success at expiry", window: ptr.To[int32](60), checkAt: 60 * time.Second, wantPassed: true},
		{name: "custom window may be shorter than automatic", window: ptr.To[int32](1), checkAt: time.Second, wantPassed: true},
		{name: "custom window starts after container restart", window: ptr.To[int32](60), startedAt: 20 * time.Second, checkAt: 60 * time.Second, wantDue: 80 * time.Second},
		{name: "custom window starts after observation", window: ptr.To[int32](60), observedAt: 20 * time.Second, checkAt: 60 * time.Second, wantDue: 80 * time.Second},
		{name: "fresh matching condition passes early", window: ptr.To[int32](60), transition: time.Second, checkAt: 2 * time.Second, wantPassed: true},
		{name: "custom window blocks false at expiry", window: ptr.To[int32](60), checkAt: 60 * time.Second, status: corev1.ConditionFalse},
		{name: "custom window blocks unknown at expiry", window: ptr.To[int32](60), checkAt: 60 * time.Second, status: corev1.ConditionUnknown},
		{name: "custom window blocks wrong result at expiry", window: ptr.To[int32](60), checkAt: 60 * time.Second, result: "not-ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gss := testGatedGSS()
			gss.Spec.ServiceQualities[0].Probe = corev1.Probe{}
			gss.Spec.ServiceQualities[0].TimeoutSeconds = tc.timeout
			gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = tc.window
			condition := corev1.PodCondition{
				Type: "game.kruise.io/ready", Status: corev1.ConditionTrue, Message: "ready",
				LastTransitionTime: metav1.NewTime(base.Add(-time.Minute)),
			}
			if tc.status != "" {
				condition.Status = tc.status
			}
			if tc.result != "" {
				condition.Message = tc.result
			}
			if tc.transition != 0 {
				condition.LastTransitionTime = metav1.NewTime(base.Add(tc.transition))
			}
			pod := &corev1.Pod{Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{{Name: "main", State: corev1.ContainerState{
					Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(base.Add(tc.startedAt))},
				}}},
				Conditions: []corev1.PodCondition{condition},
			}}
			checkpoint := observationCheckpoint{ObservedAt: metav1.NewTime(base.Add(tc.observedAt))}
			passed, due := gameServerObservationPassed(gss, pod, checkpoint, base.Add(tc.checkAt))
			var wantDue time.Time
			if tc.wantDue != 0 {
				wantDue = base.Add(tc.wantDue)
			}
			if passed != tc.wantPassed || !due.Equal(wantDue) {
				t.Fatalf("passed=%v due=%v, want passed=%v due=%v", passed, due, tc.wantPassed, wantDue)
			}
		})
	}
}

func TestGatedUpdateClosesWindowBeforeTemplateChange(t *testing.T) {
	ctx := context.Background()
	gss := testGatedGSS()
	asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Annotations: map[string]string{
				gamev1alpha1.AstsHashKey: "old",
			},
		},
	})
	asts.Spec.Replicas = ptr.To[int32](2)
	asts.Spec.Template.Spec.Containers[0].Image = "v1"
	asts.Annotations[gatedTemplateHashKey] = util.GetHash(asts.Spec.Template)
	asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](0)
	asts.Status.UpdateRevision = "old-revision"
	client := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&gamev1alpha1.GameServerSet{}, &kruisev1beta1.StatefulSet{}).
		WithObjects(gss, asts).Build()
	r := &GameServerSetReconciler{Client: client}
	key := types.NamespacedName{Name: "test", Namespace: "default"}
	reload := func() (*gamev1alpha1.GameServerSet, *kruisev1beta1.StatefulSet) {
		currentGSS := &gamev1alpha1.GameServerSet{}
		currentASTS := &kruisev1beta1.StatefulSet{}
		if err := client.Get(ctx, key, currentGSS); err != nil {
			t.Fatal(err)
		}
		if err := client.Get(ctx, key, currentASTS); err != nil {
			t.Fatal(err)
		}
		return currentGSS, currentASTS
	}

	if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, nil); err != nil {
		t.Fatal(err)
	}
	gss, asts = reload()
	if gss.Status.UpdateProgress == nil || asts.Spec.Template.Spec.Containers[0].Image != "v1" {
		t.Fatal("update progress must be persisted before changing ASTS template")
	}
	if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, nil); err != nil {
		t.Fatal(err)
	}
	_, asts = reload()
	if asts.Spec.Template.Spec.Containers[0].Image != "v2" || *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 2 {
		t.Fatal("new template must be installed with a closed update window")
	}
	asts.Status.ObservedGeneration = asts.Generation
	asts.Status.UpdateRevision = "new-revision"
	if err := client.Status().Update(ctx, asts); err != nil {
		t.Fatal(err)
	}
	gss, asts = reload()
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					appsv1.ControllerRevisionHashLabelKey: "old-revision",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					appsv1.ControllerRevisionHashLabelKey: "old-revision",
				},
			},
		},
	}
	if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, pods); err != nil {
		t.Fatal(err)
	}
	_, asts = reload()
	if *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 1 {
		t.Fatalf("first window partition = %d, want 1", *asts.Spec.UpdateStrategy.RollingUpdate.Partition)
	}

	gss, asts = reload()
	checkpoint, err := json.Marshal(observationCheckpoint{
		UpdateGeneration: gss.Status.UpdateProgress.UpdateGeneration,
		PodUID:           "updated-pod", Revision: "new-revision",
		ObservedAt: metav1.NewTime(time.Now().Add(-time.Minute)),
	})
	if err != nil {
		t.Fatal(err)
	}
	pods[0] = corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID: "updated-pod",
			Labels: map[string]string{
				appsv1.ControllerRevisionHashLabelKey: "new-revision",
			},
			Annotations: map[string]string{
				observationCheckpointKey: string(checkpoint),
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "main",
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.NewTime(time.Now().Add(-time.Minute)),
						},
					},
				},
			},
			Conditions: []corev1.PodCondition{{Type: "game.kruise.io/ready", Status: corev1.ConditionTrue, Message: "ready"}},
		},
	}
	if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, pods); err != nil {
		t.Fatal(err)
	}
	gss, asts = reload()
	if *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 0 {
		t.Fatal("one completed GameServer should release one more slot")
	}
	pods[0].Status.Conditions[0].Status = corev1.ConditionFalse
	if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, pods); err != nil {
		t.Fatal(err)
	}
	gss, asts = reload()
	if *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 0 || gss.Status.UpdateProgress.PendingCompletionReplicas != 2 {
		t.Fatal("business regression must occupy the window without increasing partition")
	}
	pods[0].Status.Conditions[0].Status = corev1.ConditionTrue
	pods[1] = *pods[0].DeepCopy()
	pods[1].UID = "second-updated-pod"
	secondCheckpoint := observationCheckpoint{
		UpdateGeneration: gss.Status.UpdateProgress.UpdateGeneration,
		PodUID:           "second-updated-pod",
		Revision:         "new-revision",
		ObservedAt:       metav1.NewTime(time.Now().Add(-time.Minute)),
	}
	serialized, err := json.Marshal(secondCheckpoint)
	if err != nil {
		t.Fatal(err)
	}
	pods[1].Annotations[observationCheckpointKey] = string(serialized)
	if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, pods); err != nil {
		t.Fatal(err)
	}
	gss, asts = reload()
	if condition := gss.Status.Conditions[0]; condition.Status != metav1.ConditionFalse {
		t.Fatalf("completed rollout condition = %s, want False", condition.Status)
	}
	pods[0].Status.Conditions[0].Status = corev1.ConditionFalse
	if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, pods); err != nil {
		t.Fatal(err)
	}
	gss, _ = reload()
	if gss.Status.Conditions[0].Status != metav1.ConditionFalse {
		t.Fatal("health changes after completion must not reopen rollout")
	}
}

func TestGatedUpdateCancellationRestoresOrdinaryStrategy(t *testing.T) {
	for _, tc := range []struct {
		name           string
		partition      *int32
		paused         bool
		removeStrategy bool
		readdEarly     bool
	}{
		{name: "default partition"},
		{name: "explicit partition", partition: ptr.To[int32](1)},
		{name: "paused remains paused", paused: true},
		{name: "remove rolling update", removeStrategy: true},
		{name: "readd immediately after cleanup", readdEarly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			gss := testGatedGSS()
			gss.Spec.UpdateStrategy.RollingUpdate.Partition = tc.partition
			gss.Spec.UpdateStrategy.RollingUpdate.Paused = tc.paused
			gss.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable = ptr.To(intstr.FromInt(2))
			gss.Spec.UpdateStrategy.RollingUpdate.PodUpdatePolicy = kruisev1beta1.InPlaceIfPossiblePodUpdateStrategyType
			gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{UpdateGeneration: gss.Generation, PendingCompletionReplicas: 1}
			gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionTrue, Reason: "WaitingForBusinessObservation", LastTransitionTime: metav1.Now()}}
			asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
			asts.Spec.Replicas = gss.Spec.Replicas
			setAstsHash(asts, gss)
			asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](2)
			asts.Status.UpdateRevision = "new-revision"
			beforeTemplate := asts.Spec.Template.DeepCopy()
			policy := gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.DeepCopy()
			gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			if tc.removeStrategy {
				gss.Spec.UpdateStrategy.RollingUpdate = nil
			}
			gss.Generation++
			want := util.GetNewAstsFromGss(gss.DeepCopy(), asts.DeepCopy())
			pods := []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "test-0", Namespace: gss.Namespace, Labels: map[string]string{gamev1alpha1.GameServerOwnerGssKey: gss.Name}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-1", Namespace: gss.Namespace, Labels: map[string]string{gamev1alpha1.GameServerOwnerGssKey: gss.Name}}},
			}
			for i := range pods {
				pods[i].Status.Conditions = []corev1.PodCondition{{Type: "game.kruise.io/ready", Status: corev1.ConditionFalse}}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&gamev1alpha1.GameServerSet{}, &kruisev1beta1.StatefulSet{}, &corev1.Pod{}).
				WithObjects(gss, asts, createPpm(gss), &pods[0], &pods[1]).Build()
			r := &GameServerSetReconciler{Client: c, Scheme: scheme, recorder: record.NewFakeRecorder(100)}
			key := client.ObjectKeyFromObject(gss)
			// 取消时先清理进度，再恢复工作负载的 partition。
			if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, key, gss); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(ctx, key, asts); err != nil {
				t.Fatal(err)
			}
			if gss.Status.UpdateProgress != nil || len(gss.Status.Conditions) != 0 || *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 2 {
				t.Fatal("old progress must be cleared before restoring the workload partition")
			}
			if tc.readdEarly {
				gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = policy
				gss.Generation++
				if err := c.Update(ctx, gss); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Get(ctx, key, asts); err != nil {
				t.Fatal(err)
			}
			if !equality.Semantic.DeepEqual(asts.Spec.UpdateStrategy, want.Spec.UpdateStrategy) || !equality.Semantic.DeepEqual(asts.Spec.Template, *beforeTemplate) {
				t.Fatalf("cancellation must restore the configured strategy without changing the target template: %+v", asts.Spec.UpdateStrategy)
			}
			if _, exists := asts.Annotations[gatedTemplateHashKey]; exists && !tc.readdEarly {
				t.Fatal("cancellation must clear the gated template fingerprint")
			}
			if err := c.Get(ctx, key, gss); err != nil {
				t.Fatal(err)
			}
			if gss.Status.UpdateProgress != nil || len(gss.Status.Conditions) != 0 {
				t.Fatalf("cancellation left stale gated status: %+v", gss.Status)
			}
			for i := range pods {
				current := &corev1.Pod{}
				if err := c.Get(ctx, client.ObjectKeyFromObject(&pods[i]), current); err != nil {
					t.Fatal(err)
				}
				if !equality.Semantic.DeepEqual(current.Status, pods[i].Status) {
					t.Fatal("cancellation must not fake passing business conditions")
				}
			}
			if !tc.removeStrategy && !tc.readdEarly {
				gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = policy
				gss.Generation++
				if err := c.Update(ctx, gss); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 3; i++ {
					if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
						t.Fatal(err)
					}
				}
				if err := c.Get(ctx, key, gss); err != nil {
					t.Fatal(err)
				}
				if gss.Status.UpdateProgress != nil {
					t.Fatal("adding a policy without a new template must not resurrect the cancelled rollout")
				}
			}
		})
	}
}

func TestGatedUpdateStrategyEditsKeepObservationRound(t *testing.T) {
	for _, tc := range []struct {
		name          string
		window        *int32
		changeActions bool
		wantPartition int32
	}{
		{name: "extend window", window: ptr.To[int32](60), wantPartition: 1},
		{name: "shorten window", window: ptr.To[int32](1)},
		{name: "restore default window"},
		{name: "change actions", window: ptr.To[int32](1), changeActions: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			gss := testGatedGSS()
			gss.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](0)
			gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = ptr.To[int32](1)
			gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{UpdateGeneration: gss.Generation, StartedAt: metav1.NewTime(now.Add(-time.Minute))}
			gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionTrue, Reason: "WaitingForBusinessObservation", LastTransitionTime: metav1.Now()}}
			asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
			asts.Spec.Replicas = gss.Spec.Replicas
			setAstsHash(asts, gss)
			asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](1)
			asts.Status.UpdateRevision = "new-revision"
			checkpoint := observationCheckpoint{UpdateGeneration: gss.Generation, PodUID: "pod-1", Revision: "new-revision", ObservedAt: metav1.NewTime(now.Add(-30 * time.Second))}
			raw, err := json.Marshal(checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			pods := []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "test-0", Namespace: gss.Namespace, Labels: map[string]string{appsv1.ControllerRevisionHashLabelKey: "old-revision"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-1", Namespace: gss.Namespace, UID: "pod-1", Labels: map[string]string{appsv1.ControllerRevisionHashLabelKey: "new-revision"}, Annotations: map[string]string{observationCheckpointKey: string(raw)}}, Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{{Name: "main", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(now.Add(-time.Minute))}}}},
					Conditions:        []corev1.PodCondition{{Type: "game.kruise.io/ready", Status: corev1.ConditionTrue, Message: "ready", LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Minute))}},
				}},
			}
			gss.Generation++
			gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ObservationWindowSeconds = tc.window
			if tc.changeActions {
				gss.Spec.ServiceQualities[0].Permanent = true
				gss.Spec.ServiceQualities[0].ServiceQualityAction = []gamev1alpha1.ServiceQualityAction{{State: true, Labels: map[string]string{"delete-block": "false"}}}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gamev1alpha1.GameServerSet{}, &kruisev1beta1.StatefulSet{}).WithObjects(gss, asts, &pods[0], &pods[1]).Build()
			r := &GameServerSetReconciler{Client: c}
			key := client.ObjectKeyFromObject(gss)
			for i := 0; i < 2; i++ {
				if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, pods); err != nil {
					t.Fatal(err)
				}
				if err := c.Get(ctx, key, asts); err != nil {
					t.Fatal(err)
				}
			}
			if *asts.Spec.UpdateStrategy.RollingUpdate.Partition != tc.wantPartition || gss.Status.UpdateProgress.UpdateGeneration != checkpoint.UpdateGeneration {
				t.Fatalf("strategy edit must use the current window without resetting progress: partition=%d progress=%+v", *asts.Spec.UpdateStrategy.RollingUpdate.Partition, gss.Status.UpdateProgress)
			}
			if err := c.Get(ctx, client.ObjectKeyFromObject(&pods[1]), &pods[1]); err != nil {
				t.Fatal(err)
			}
			if pods[1].Annotations[observationCheckpointKey] != string(raw) {
				t.Fatal("strategy edits must not reset the observation checkpoint")
			}
		})
	}
}

func TestCompletedGatedUpdatePartitionChanges(t *testing.T) {
	for _, tc := range []struct {
		name             string
		currentPartition int32
		targetPartition  int32
		wantHandled      bool
	}{
		{name: "prepare next partial update", targetPartition: 1},
		{name: "keep completed partition"},
		{name: "continue partial update", currentPartition: 1, wantHandled: true},
		{name: "continue within capacity", currentPartition: 2, wantHandled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gss := testGatedGSS()
			gss.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(tc.currentPartition)
			gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{
				UpdateGeneration:  gss.Generation,
				CompletedReplicas: 2 - tc.currentPartition,
				StartedAt:         metav1.Now(),
			}
			gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionFalse, Reason: "Completed", LastTransitionTime: metav1.Now()}}
			asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
			asts.Spec.Replicas = ptr.To[int32](2)
			setAstsHash(asts, gss)
			asts.Status.UpdateRevision = "revision"
			gss.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To(tc.targetPartition)
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gamev1alpha1.GameServerSet{}).WithObjects(gss, asts).Build()
			r := &GameServerSetReconciler{Client: c}
			ctx := context.Background()
			handled, _, err := r.reconcileGatedUpdate(ctx, gss, asts, []corev1.Pod{{}, {}})
			if err != nil || handled != tc.wantHandled {
				t.Fatalf("handled=%v, want %v; err=%v", handled, tc.wantHandled, err)
			}
			if !handled {
				manager := NewGameServerSetManager(gss, c, nil, logr.Discard())
				manager.SyncStsAndPodList(asts, nil)
				if manager.IsNeedToUpdateWorkload() {
					if err := manager.UpdateWorkload(ctx); err != nil {
						t.Fatal(err)
					}
				}
				stored := &kruisev1beta1.StatefulSet{}
				if err := c.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, stored); err != nil {
					t.Fatal(err)
				}
				if *stored.Spec.UpdateStrategy.RollingUpdate.Partition != tc.targetPartition || util.GetHash(stored.Spec.Template) != asts.Annotations[gatedTemplateHashKey] {
					t.Fatal("ordinary strategy sync must set the requested partition without changing the template")
				}
				if gss.Status.Conditions[0].Status != metav1.ConditionFalse || gss.Status.Conditions[0].Reason != "Completed" {
					t.Fatal("strategy preparation must preserve Completed")
				}
			} else {
				key := types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}
				if err := c.Get(ctx, key, asts); err != nil {
					t.Fatal(err)
				}
				if handled, _, err := r.reconcileGatedUpdate(ctx, gss, asts, []corev1.Pod{{}, {}}); err != nil || !handled {
					t.Fatalf("continued update must remain gated, handled=%v err=%v", handled, err)
				}
				if err := c.Get(ctx, key, asts); err != nil {
					t.Fatal(err)
				}
				if *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 1 || gss.Status.Conditions[0].Status != metav1.ConditionTrue {
					t.Fatal("continued update must respect one slot of capacity and resume the active state")
				}
			}
		})
	}
}

func TestCompletedGatedUpdateStartsNewTemplateRound(t *testing.T) {
	gss := testGatedGSS()
	gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{UpdateGeneration: gss.Generation, CompletedReplicas: 2, StartedAt: metav1.Now()}
	gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionFalse, Reason: "Completed", LastTransitionTime: metav1.Now()}}
	asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
	asts.Spec.Replicas = ptr.To[int32](2)
	setAstsHash(asts, gss)
	gss.Generation++
	gss.Spec.GameServerTemplate.Spec.Containers[0].Image = "v3"
	gss.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](1)
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gamev1alpha1.GameServerSet{}).WithObjects(gss, asts).Build()
	r := &GameServerSetReconciler{Client: c}
	ctx := context.Background()
	for step := 0; step < 2; step++ {
		if handled, _, err := r.reconcileGatedUpdate(ctx, gss, asts, nil); err != nil || !handled {
			t.Fatalf("new template must remain gated, handled=%v err=%v", handled, err)
		}
		key := types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}
		if err := c.Get(ctx, key, gss); err != nil {
			t.Fatal(err)
		}
		if err := c.Get(ctx, key, asts); err != nil {
			t.Fatal(err)
		}
		if step == 0 && (gss.Status.UpdateProgress.UpdateGeneration != gss.Generation || gss.Status.Conditions[0].Status != metav1.ConditionTrue || asts.Spec.Template.Spec.Containers[0].Image != "v2") {
			t.Fatal("new round must be persisted before changing the completed template")
		}
	}
	if asts.Spec.Template.Spec.Containers[0].Image != "v3" || *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 2 {
		t.Fatal("new template must be installed with a closed window despite the old Completed state")
	}
}

func TestGatedMaxUnavailable(t *testing.T) {
	tests := []struct {
		value *intstr.IntOrString
		want  int32
	}{
		{nil, 1},
		{ptr.To(intstr.FromInt32(2)), 2},
		{ptr.To(intstr.FromString("25%")), 2},
		{ptr.To(intstr.FromString("1%")), 1},
	}
	for _, tt := range tests {
		got, err := gatedMaxUnavailable(tt.value, 10)
		if err != nil || got != tt.want {
			t.Fatalf("gatedMaxUnavailable(%v) = %d, %v; want %d", tt.value, got, err, tt.want)
		}
	}
}

func TestGatedPartitionAfterScale(t *testing.T) {
	tests := []struct {
		oldReplicas, oldPartition, newReplicas, targetPartition, want int32
	}{
		{10, 8, 12, 0, 10}, // Keep two slots open while scaling up.
		{10, 8, 6, 0, 4},   // Keep two slots open while scaling down.
		{10, 8, 1, 0, 0},   // Cannot preserve more open slots than new replicas.
		{10, 8, 6, 5, 5},   // Respect the user's final partition.
	}
	for _, tt := range tests {
		if got := gatedPartitionAfterScale(tt.oldReplicas, tt.oldPartition, tt.newReplicas, tt.targetPartition); got != tt.want {
			t.Fatalf("gatedPartitionAfterScale(%d, %d, %d, %d) = %d, want %d", tt.oldReplicas, tt.oldPartition, tt.newReplicas, tt.targetPartition, got, tt.want)
		}
	}
}

func TestGatedUpdateMissingProgressFailsClosed(t *testing.T) {
	gss := testGatedGSS()
	asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
	asts.Spec.Replicas = ptr.To[int32](2)
	asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](2)
	setAstsHash(asts, gss)
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gamev1alpha1.GameServerSet{}).WithObjects(gss, asts).Build()
	r := &GameServerSetReconciler{Client: client}
	ctx := context.Background()
	if handled, _, err := r.reconcileGatedUpdate(ctx, gss, asts, nil); err != nil || handled {
		t.Fatalf("policy added without template change should use legacy path: handled=%v err=%v", handled, err)
	}

	gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionTrue, Reason: "Updating", LastTransitionTime: metav1.Now()}}
	if err := client.Status().Update(ctx, gss); err != nil {
		t.Fatal(err)
	}
	if handled, _, err := r.reconcileGatedUpdate(ctx, gss, asts, nil); err != nil || !handled {
		t.Fatalf("missing active progress must be handled without release: handled=%v err=%v", handled, err)
	}
	stored := &gamev1alpha1.GameServerSet{}
	if err := client.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Conditions[0].Status != metav1.ConditionUnknown || *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 2 {
		t.Fatal("missing active progress should report Unknown and keep the partition closed")
	}
}

func TestGatedUpdateWithoutPolicyUsesLegacyPath(t *testing.T) {
	for _, status := range []metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionUnknown} {
		for _, missingStrategy := range []bool{false, true} {
			gss := testGatedGSS()
			gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			if missingStrategy {
				gss.Spec.UpdateStrategy.RollingUpdate = nil
			}
			gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{UpdateGeneration: 3, StartedAt: metav1.Now()}
			gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: status}}
			before := util.GetHash(gss)
			// A nil client/workload verifies there are no gated reads or writes.
			r := &GameServerSetReconciler{}
			handled, result, err := r.reconcileGatedUpdate(context.Background(), gss, nil, nil)
			if handled || err != nil || result.Requeue || result.RequeueAfter != 0 || util.IsServiceQualityGatedUpdateActive(gss) || before != util.GetHash(gss) {
				t.Fatalf("unconfigured GSS must be untouched: status=%s missingStrategy=%v handled=%v result=%v err=%v", status, missingStrategy, handled, result, err)
			}
		}
	}
}

func TestInvalidProgressAndMissingPodHoldPartition(t *testing.T) {
	for _, tc := range []struct {
		name       string
		generation int64
		wantReason string
	}{
		{"zero generation", 0, "InvalidUpdateProgress"},
		{"negative generation", -1, "InvalidUpdateProgress"},
		{"future generation", 4, "InvalidUpdateProgress"},
		{"missing pod", 3, "WaitingForReplicas"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			gss := testGatedGSS()
			gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{UpdateGeneration: tc.generation, StartedAt: metav1.Now()}
			asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace}})
			asts.Spec.Replicas = ptr.To[int32](2)
			asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](1)
			setAstsHash(asts, gss)
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&gamev1alpha1.GameServerSet{}).WithObjects(gss, asts).Build()
			r := &GameServerSetReconciler{Client: c}
			for i := 0; i < 2; i++ {
				if handled, _, err := r.reconcileGatedUpdate(ctx, gss, asts, []corev1.Pod{{}}); err != nil || !handled {
					t.Fatalf("handled=%v err=%v", handled, err)
				}
				if err := c.Get(ctx, types.NamespacedName{Name: gss.Name, Namespace: gss.Namespace}, gss); err != nil {
					t.Fatal(err)
				}
				if err := c.Get(ctx, types.NamespacedName{Name: asts.Name, Namespace: asts.Namespace}, asts); err != nil {
					t.Fatal(err)
				}
				if *asts.Spec.UpdateStrategy.RollingUpdate.Partition != 1 {
					t.Fatal("invalid progress or missing Pod released another slot")
				}
				if len(gss.Status.Conditions) != 1 || gss.Status.Conditions[0].Reason != tc.wantReason {
					t.Fatalf("conditions=%v", gss.Status.Conditions)
				}
				if tc.wantReason == "InvalidUpdateProgress" && gss.Status.Conditions[0].Status != metav1.ConditionUnknown {
					t.Fatal("invalid progress must report Unknown")
				}
			}
		})
	}
}

func TestObservationANDAndMissingSignals(t *testing.T) {
	now := time.Date(2026, 10, 26, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		status  corev1.ConditionStatus
		message string
		missing bool
		want    bool
	}{
		{"both match", corev1.ConditionTrue, "ready", false, true},
		{"second false", corev1.ConditionFalse, "ready", false, false},
		{"second unknown", corev1.ConditionUnknown, "ready", false, false},
		{"second missing", corev1.ConditionTrue, "ready", true, false},
		{"second wrong result", corev1.ConditionTrue, "wrong", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gss := testGatedGSS()
			probe := gss.Spec.ServiceQualities[0]
			probe.Name = "aux"
			probe.PeriodSeconds = 5
			gss.Spec.ServiceQualities = append(gss.Spec.ServiceQualities, probe)
			gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities = append(gss.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy.ServiceQualities,
				gamev1alpha1.ServiceQualityCompletionCondition{Name: "aux", State: true, Result: "ready"})
			pod := &corev1.Pod{Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "main",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{
								StartedAt: metav1.NewTime(now.Add(-time.Minute)),
							},
						},
					},
				},
				Conditions: []corev1.PodCondition{
					{
						Type:               "game.kruise.io/ready",
						Status:             corev1.ConditionTrue,
						Message:            "ready",
						LastTransitionTime: metav1.NewTime(now.Add(time.Second)),
					},
				},
			}}
			if !tc.missing {
				pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{Type: "game.kruise.io/aux", Status: tc.status, Message: tc.message})
			}
			checkpoint := observationCheckpoint{ObservedAt: metav1.NewTime(now)}
			if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(31*time.Second)); passed {
				t.Fatal("fresh first signal cannot bypass the second probe window")
			}
			if passed, _ := gameServerObservationPassed(gss, pod, checkpoint, now.Add(32*time.Second)); passed != tc.want {
				t.Fatalf("passed=%v want=%v", passed, tc.want)
			}
		})
	}
}

func TestCheckpointIdentity(t *testing.T) {
	now := metav1.NewTime(time.Date(2026, 10, 26, 13, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		name, raw string
		want      bool
	}{
		{"valid", `{"updateGeneration":3,"podUID":"uid","revision":"rev","observedAt":"2026-10-26T13:00:00Z"}`, true},
		{"missing", "", false},
		{"invalid json", "{", false},
		{"old generation", `{"updateGeneration":2,"podUID":"uid","revision":"rev","observedAt":"2026-10-26T13:00:00Z"}`, false},
		{"old uid", `{"updateGeneration":3,"podUID":"old","revision":"rev","observedAt":"2026-10-26T13:00:00Z"}`, false},
		{"old revision", `{"updateGeneration":3,"podUID":"uid","revision":"old","observedAt":"2026-10-26T13:00:00Z"}`, false},
		{"empty time", `{"updateGeneration":3,"podUID":"uid","revision":"rev"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "uid", Annotations: map[string]string{observationCheckpointKey: tc.raw}}}
			cp, valid := readObservationCheckpoint(pod, 3, "rev")
			if valid != tc.want {
				t.Fatalf("valid=%v want=%v", valid, tc.want)
			}
			if valid && !cp.ObservedAt.Equal(&now) {
				t.Fatal("valid checkpoint changed observation time")
			}
		})
	}
}

func TestFortyCheckpointsConverge(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 13, 0, 0, 0, time.UTC)
	gss := testGatedGSS()
	gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{UpdateGeneration: 3, StartedAt: metav1.NewTime(now)}
	objects := make([]client.Object, 0, 40)
	for i := 0; i < 40; i++ {
		objects = append(objects, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod-%02d", i),
				Namespace: "default",
				UID:       types.UID(fmt.Sprintf("uid-%02d", i)),
				Labels: map[string]string{
					appsv1.ControllerRevisionHashLabelKey: "new",
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "main",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{
								StartedAt: metav1.NewTime(now.Add(-time.Minute)),
							},
						},
					},
				},
				Conditions: []corev1.PodCondition{{Type: "game.kruise.io/ready", Status: corev1.ConditionTrue, Message: "ready"}},
			}})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	r := &GameServerSetReconciler{Client: c}
	load := func() []corev1.Pod {
		var list corev1.PodList
		if err := c.List(ctx, &list); err != nil {
			t.Fatal(err)
		}
		return list.Items
	}
	count := func() int {
		n := 0
		for _, p := range load() {
			if _, valid := readObservationCheckpoint(&p, 3, "new"); valid {
				n++
			}
		}
		return n
	}
	if _, _, _, err := r.observeUpdatedPods(ctx, gss, "new", load(), now); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 32 {
		t.Fatalf("first reconcile wrote %d checkpoints, want 32", n)
	}
	if _, _, _, err := r.observeUpdatedPods(ctx, gss, "new", load(), now); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 40 {
		t.Fatalf("second reconcile has %d checkpoints, want 40", n)
	}
	before := load()
	n, _, wrote, err := r.observeUpdatedPods(ctx, gss, "new", load(), now.Add(26*time.Second))
	if err != nil || wrote || n != 40 {
		t.Fatalf("convergence completed=%d wrote=%v err=%v", n, wrote, err)
	}
	for i, p := range load() {
		if p.Annotations[observationCheckpointKey] != before[i].Annotations[observationCheckpointKey] {
			t.Fatal("repeat reconcile reset a valid checkpoint")
		}
	}
}

func TestInPlaceUpdatePendingAndDeleting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		state    appspub.InPlaceUpdateState
		deleting bool
		want     bool
	}{
		{name: "finished", state: appspub.InPlaceUpdateState{Revision: "new"}, want: true},
		{name: "wrong revision", state: appspub.InPlaceUpdateState{Revision: "old"}},
		{name: "pending images", state: appspub.InPlaceUpdateState{Revision: "new", NextContainerImages: map[string]string{"main": "v2"}}},
		{name: "deleting", state: appspub.InPlaceUpdateState{Revision: "new"}, deleting: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.state)
			if err != nil {
				t.Fatal(err)
			}
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						appsv1.ControllerRevisionHashLabelKey: "new",
					},
					Annotations: map[string]string{
						appspub.InPlaceUpdateStateKey: string(raw),
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   appspub.InPlaceUpdateReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			if tc.deleting {
				now := metav1.Now()
				pod.DeletionTimestamp = &now
			}
			if got := podUpdateFinished(pod, "new"); got != tc.want {
				t.Fatalf("finished=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestUpdateGatedStatusSkipsNewerGeneration(t *testing.T) {
	for _, tc := range []struct {
		name          string
		generation    int64
		withoutPolicy bool
	}{
		{name: "same generation", generation: 3},
		{name: "new round", generation: 4},
		{name: "policy removed", generation: 4, withoutPolicy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			stale := testGatedGSS()
			stale.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{
				UpdateGeneration: stale.Generation,
				StartedAt:        metav1.Now(),
			}
			current := stale.DeepCopy()
			current.Generation = tc.generation
			current.Status.UpdateProgress.UpdateGeneration = tc.generation
			current.Status.Conditions = []metav1.Condition{{
				Type: updateProgressingCondition, Status: metav1.ConditionTrue, Reason: "PreparingUpdate",
			}}
			if tc.withoutPolicy {
				current.Spec.UpdateStrategy.RollingUpdate.CompletionPolicy = nil
			}
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&gamev1alpha1.GameServerSet{}).WithObjects(current).Build()
			r := &GameServerSetReconciler{Client: c}
			if err := c.Get(ctx, client.ObjectKeyFromObject(current), current); err != nil {
				t.Fatal(err)
			}
			before := util.GetHash(current.Status)
			if err := r.updateGatedStatus(ctx, stale, "Completed", "Old round finished", metav1.ConditionFalse); err != nil {
				t.Fatal(err)
			}
			stored := &gamev1alpha1.GameServerSet{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(current), stored); err != nil {
				t.Fatal(err)
			}
			if tc.generation != stale.Generation {
				if util.GetHash(stored.Status) != before {
					t.Fatalf("old reconcile changed newer status: %+v", stored.Status)
				}
			} else if stored.Status.Conditions[0].Reason != "Completed" ||
				stored.Status.Conditions[0].Status != metav1.ConditionFalse {
				t.Fatalf("same-generation status update was skipped: %+v", stored.Status)
			}
		})
	}
}

func TestGatedUpdateWithoutStartedAtStillObservesBusinessConditions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		message    string
		missing    bool
		wantReason string
	}{
		{name: "matching condition", message: "ready", wantReason: "Completed"},
		{name: "mismatching condition", message: "failed", wantReason: "WaitingForBusinessObservation"},
		{name: "missing condition", missing: true, wantReason: "WaitingForBusinessObservation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			gss := testGatedGSS()
			gss.Spec.Replicas = ptr.To[int32](1)
			gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{UpdateGeneration: gss.Generation}
			gss.Status.Conditions = []metav1.Condition{{Type: updateProgressingCondition, Status: metav1.ConditionTrue}}
			asts := util.GetNewAstsFromGss(gss.DeepCopy(), &kruisev1beta1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: gss.Name, Namespace: gss.Namespace},
			})
			asts.Spec.Replicas = ptr.To[int32](1)
			asts.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](0)
			asts.Status.ObservedGeneration = asts.Generation
			asts.Status.UpdateRevision = "new"
			setAstsHash(asts, gss)
			checkpoint, err := json.Marshal(observationCheckpoint{
				UpdateGeneration: gss.Generation, PodUID: "uid", Revision: "new",
				ObservedAt: metav1.NewTime(now.Add(-time.Minute)),
			})
			if err != nil {
				t.Fatal(err)
			}
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uid", Labels: map[string]string{appsv1.ControllerRevisionHashLabelKey: "new"},
					Annotations: map[string]string{observationCheckpointKey: string(checkpoint)},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{{
						Name: "main", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.NewTime(now.Add(-time.Hour)),
						}},
					}},
				},
			}
			if !tc.missing {
				pod.Status.Conditions = []corev1.PodCondition{{
					Type: "game.kruise.io/ready", Status: corev1.ConditionTrue, Message: tc.message,
				}}
			}
			c := fake.NewClientBuilder().WithScheme(scheme).
				WithStatusSubresource(&gamev1alpha1.GameServerSet{}).WithObjects(gss, asts).Build()
			r := &GameServerSetReconciler{Client: c}
			if _, _, err := r.reconcileGatedUpdate(ctx, gss, asts, []corev1.Pod{pod}); err != nil {
				t.Fatal(err)
			}
			stored := &gamev1alpha1.GameServerSet{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(gss), stored); err != nil {
				t.Fatal(err)
			}
			if stored.Status.Conditions[0].Reason != tc.wantReason {
				t.Fatalf("reason=%s, want %s", stored.Status.Conditions[0].Reason, tc.wantReason)
			}
		})
	}
}

func TestUpdateGatedStatusEventsOnlyReportLifecycleAndWarnings(t *testing.T) {
	ctx := context.Background()
	gss := testGatedGSS()
	gss.Status.UpdateProgress = &gamev1alpha1.GameServerSetUpdateProgress{
		UpdateGeneration: gss.Generation, StartedAt: metav1.Now(),
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&gamev1alpha1.GameServerSet{}).WithObjects(gss).Build()
	recorder := record.NewFakeRecorder(10)
	r := &GameServerSetReconciler{Client: c, recorder: recorder}
	for _, tc := range []struct {
		name        string
		reason      string
		status      metav1.ConditionStatus
		wantEvent   bool
		eventReason string
	}{
		{name: "initial state", reason: "PreparingUpdate", status: metav1.ConditionTrue, wantEvent: true},
		{name: "release slots", reason: "Updating", status: metav1.ConditionTrue},
		{name: "counters only change", reason: "Updating", status: metav1.ConditionTrue},
		{name: "wait for business", reason: "WaitingForBusinessObservation", status: metav1.ConditionTrue},
		{name: "release more slots", reason: "Updating", status: metav1.ConditionTrue},
		{name: "wait for revision", reason: "WaitingForRevision", status: metav1.ConditionTrue},
		{name: "pause", reason: "Paused", status: metav1.ConditionTrue, wantEvent: true},
		{name: "repeated pause", reason: "Paused", status: metav1.ConditionTrue},
		{name: "resume", reason: "WaitingForBusinessObservation", status: metav1.ConditionTrue, wantEvent: true, eventReason: "Resumed"},
		{name: "wait after resume", reason: "WaitingForRevision", status: metav1.ConditionTrue},
		{name: "first warning", reason: "InvalidPartition", status: metav1.ConditionUnknown, wantEvent: true},
		{name: "warning reason changes", reason: "InvalidUpdateProgress", status: metav1.ConditionUnknown, wantEvent: true},
		{name: "repeated warning", reason: "InvalidUpdateProgress", status: metav1.ConditionUnknown},
		{name: "warning resolved", reason: "WaitingForBusinessObservation", status: metav1.ConditionTrue},
		{name: "completed", reason: "Completed", status: metav1.ConditionFalse, wantEvent: true},
		{name: "repeated completed", reason: "Completed", status: metav1.ConditionFalse},
		{name: "next round", reason: "PreparingUpdate", status: metav1.ConditionTrue, wantEvent: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gss.Status.UpdateProgress.CompletedReplicas++
			if err := r.updateGatedStatus(ctx, gss, tc.reason, "State changed", tc.status); err != nil {
				t.Fatal(err)
			}
			select {
			case event := <-recorder.Events:
				if !tc.wantEvent {
					t.Fatalf("routine or repeated state produced an event: %s", event)
				}
				eventType := corev1.EventTypeNormal
				if tc.status == metav1.ConditionUnknown {
					eventType = corev1.EventTypeWarning
				}
				eventReason := tc.eventReason
				if eventReason == "" {
					eventReason = tc.reason
				}
				want := fmt.Sprintf("%s UpdateProgressChanged %s: State changed", eventType, eventReason)
				if event != want {
					t.Fatalf("event=%q, want %q", event, want)
				}
			default:
				if tc.wantEvent {
					t.Fatal("lifecycle or warning change did not produce an event")
				}
			}
		})
	}
}
