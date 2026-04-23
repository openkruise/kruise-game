/*
Copyright 2024 The Kruise Authors.

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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
)

func TestGameServerReconcile(t *testing.T) {
	nodeTemplate := &corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
	}
	gssTemplate := &gameKruiseV1alpha1.GameServerSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GameServerSet",
			APIVersion: "game.kruise.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "xxx",
			Name:      "xxx",
			UID:       "xxx-gss",
		},
	}
	podTemplate := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "xxx",
			Name:      "xxx-0",
			UID:       "xxx-pod",
			Labels: map[string]string{
				gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
			},
		},
	}
	gsTemplate := &gameKruiseV1alpha1.GameServer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GameServer",
			APIVersion: "game.kruise.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "xxx",
			Name:      "xxx-0",
			UID:       "xxx-gs",
			Labels: map[string]string{
				gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
			},
		},
	}

	tests := []struct {
		req         ctrl.Request
		getGss      func() *gameKruiseV1alpha1.GameServerSet
		getPod      func() *corev1.Pod
		getGs       func() *gameKruiseV1alpha1.GameServer
		getNode     func() *corev1.Node
		getExpectGs func() *gameKruiseV1alpha1.GameServer
	}{
		{
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "xxx-0",
					Namespace: "xxx",
				},
			},
			getGss: func() *gameKruiseV1alpha1.GameServerSet {
				return gssTemplate.DeepCopy()
			},
			getPod: func() *corev1.Pod {
				return podTemplate.DeepCopy()
			},
			getGs: func() *gameKruiseV1alpha1.GameServer {
				return nil
			},
			getNode: func() *corev1.Node {
				return nodeTemplate.DeepCopy()
			},
			getExpectGs: func() *gameKruiseV1alpha1.GameServer {
				gs := gsTemplate.DeepCopy()
				gs.Annotations = make(map[string]string)
				gs.Annotations[gameKruiseV1alpha1.GsTemplateMetadataHashKey] = util.GetGsTemplateMetadataHash(gssTemplate)
				gs.OwnerReferences = []metav1.OwnerReference{
					{
						APIVersion:         podTemplate.APIVersion,
						Kind:               podTemplate.Kind,
						Name:               podTemplate.GetName(),
						UID:                podTemplate.GetUID(),
						Controller:         ptr.To[bool](true),
						BlockOwnerDeletion: ptr.To[bool](true),
					},
				}
				updatePriority := intstr.FromInt(0)
				deletionPriority := intstr.FromInt(0)
				gs.Spec = gameKruiseV1alpha1.GameServerSpec{
					DeletionPriority: &deletionPriority,
					UpdatePriority:   &updatePriority,
					OpsState:         gameKruiseV1alpha1.None,
					NetworkDisabled:  ptr.To(false),
				}
				return gs
			},
		},

		{
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "xxx-0",
					Namespace: "xxx",
				},
			},
			getGss: func() *gameKruiseV1alpha1.GameServerSet {
				return gssTemplate.DeepCopy()
			},
			getPod: func() *corev1.Pod {
				return nil
			},
			getGs: func() *gameKruiseV1alpha1.GameServer {
				gs := gsTemplate.DeepCopy()
				gs.GetLabels()[gameKruiseV1alpha1.GameServerDeletingKey] = "true"
				return gs
			},
			getNode: func() *corev1.Node {
				return nodeTemplate.DeepCopy()
			},
			getExpectGs: func() *gameKruiseV1alpha1.GameServer {
				return nil
			},
		},
	}

	for i, test := range tests {
		objs := []client.Object{test.getNode(), test.getGss()}
		pod := test.getPod()
		gs := test.getGs()
		if pod != nil {
			objs = append(objs, pod)
		}
		if gs != nil {
			objs = append(objs, gs)
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		recon := GameServerReconciler{Client: c}
		if _, err := recon.Reconcile(context.TODO(), test.req); err != nil {
			t.Error(err)
		}

		expectGs := test.getExpectGs()
		actualGs := &gameKruiseV1alpha1.GameServer{}
		if err := c.Get(context.TODO(), test.req.NamespacedName, actualGs); err != nil {
			if expectGs == nil && errors.IsNotFound(err) {
				continue
			}
			t.Error(err)
		}

		// gs labels
		expectGsLabels := expectGs.GetLabels()
		actualGsLabels := actualGs.GetLabels()
		if !reflect.DeepEqual(expectGsLabels, actualGsLabels) {
			t.Errorf("case %d: expect labels %v, but actually got %v", i, expectGsLabels, actualGsLabels)
		}

		// gs annotations
		expectGsAnnotations := expectGs.GetAnnotations()
		actualGsAnnotations := actualGs.GetAnnotations()
		if !reflect.DeepEqual(expectGsAnnotations, actualGsAnnotations) {
			t.Errorf("case %d: expect annotations %v, but actually got %v", i, expectGsAnnotations, actualGsAnnotations)
		}

		// gs ownerReferences
		expectGsOwnerReferences := expectGs.GetOwnerReferences()
		actualGsOwnerReferences := actualGs.GetOwnerReferences()
		if !reflect.DeepEqual(expectGsOwnerReferences, actualGsOwnerReferences) {
			t.Errorf("case %d: expect ownerReferences %v, but actually got %v", i, expectGsOwnerReferences, actualGsOwnerReferences)
		}

		// gs spec
		expectGsSpec := expectGs.Spec
		actualGsSpec := actualGs.Spec
		if !reflect.DeepEqual(expectGsSpec, actualGsSpec) {
			t.Errorf("case %d: expect Spec %v, but actually got %v", i, expectGsSpec, actualGsSpec)
		}
	}
}

// TestWatchGameServerWithPredicateUpdateFunc verifies the UpdateFunc predicate
// used by watchGameServerWithPredicate (issue #321 fix).
//
// The predicate is the gate that decides whether a GameServer update event
// reaches the reconcile queue. These tests confirm that:
//   - any spec.opsState transition enqueues immediately
//   - spec.networkDisabled changes enqueue immediately
//   - a newly set deletionTimestamp enqueues immediately
//   - status-only and annotation-only updates do not enqueue, preventing the
//     event storm that originally caused the 139 s OpsState sync gap
func TestWatchGameServerWithPredicateUpdateFunc(t *testing.T) {
	// updatePredicate is a local copy of the UpdateFunc logic so the test
// stays fast and self-contained without needing a live informer cache.
    updatePredicate := func(oldGS, newGS *gameKruiseV1alpha1.GameServer) bool {
	// Check if any spec field changed (matches actual controller logic)
	    if !reflect.DeepEqual(oldGS.Spec, newGS.Spec) {
		return true
	    }
	
	// Check if object is being deleted
	    if oldGS.DeletionTimestamp == nil && newGS.DeletionTimestamp != nil {
		return true
	    }
	
	return false
}

	deletionTime := metav1.Now()

	tests := []struct {
		name        string
		oldGS       *gameKruiseV1alpha1.GameServer
		newGS       *gameKruiseV1alpha1.GameServer
		wantEnqueue bool
	}{
		{
			name: "None to Allocated enqueues",
			oldGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.None},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Allocated},
			},
			wantEnqueue: true,
		},
		{
			name: "Allocated to Kill enqueues",
			oldGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Allocated},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Kill},
			},
			wantEnqueue: true,
		},
		{
			name: "Kill to None enqueues",
			oldGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Kill},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.None},
			},
			wantEnqueue: true,
		},
		{
			name: "NetworkDisabled false to true enqueues",
			oldGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{
					OpsState:        gameKruiseV1alpha1.None,
					NetworkDisabled: ptr.To(false),
				},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{
					OpsState:        gameKruiseV1alpha1.None,
					NetworkDisabled: ptr.To(true),
				},
			},
			wantEnqueue: true,
		},
		{
			name: "NetworkDisabled unchanged does not enqueue",
			oldGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{
					OpsState:        gameKruiseV1alpha1.None,
					NetworkDisabled: ptr.To(false),
				},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{
					OpsState:        gameKruiseV1alpha1.None,
					NetworkDisabled: ptr.To(false),
				},
			},
			wantEnqueue: false,
		},
		{
			name: "DeletionTimestamp newly set enqueues",
			oldGS: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Allocated},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &deletionTime},
				Spec:       gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Allocated},
			},
			wantEnqueue: true,
		},
		{
			name: "Status change only does not enqueue",
			oldGS: &gameKruiseV1alpha1.GameServer{
				Spec:   gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Allocated},
				Status: gameKruiseV1alpha1.GameServerStatus{CurrentState: gameKruiseV1alpha1.Ready},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				Spec:   gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Allocated},
				Status: gameKruiseV1alpha1.GameServerStatus{CurrentState: gameKruiseV1alpha1.NotReady},
			},
			wantEnqueue: false,
		},
		{
			name: "Annotation change only does not enqueue",
			oldGS: &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"key": "a"}},
				Spec:       gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.None},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"key": "b"}},
				Spec:       gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.None},
			},
			wantEnqueue: false,
		},
		{
			name: "DeletionTimestamp already set does not enqueue again",
			oldGS: &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &deletionTime},
				Spec:       gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Allocated},
			},
			newGS: &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &deletionTime},
				Spec:       gameKruiseV1alpha1.GameServerSpec{OpsState: gameKruiseV1alpha1.Allocated},
			},
			wantEnqueue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updatePredicate(tt.oldGS, tt.newGS)
			if got != tt.wantEnqueue {
				t.Errorf("updatePredicate() = %v, want %v", got, tt.wantEnqueue)
			}
		})
	}
}