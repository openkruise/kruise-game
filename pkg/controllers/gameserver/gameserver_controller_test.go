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
	"sigs.k8s.io/controller-runtime/pkg/event"

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

func TestGameServerUpdatePredicate(t *testing.T) {
	base := &gameKruiseV1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "xxx",
			Name:      "xxx-0",
			Annotations: map[string]string{
				"plain": "old",
			},
		},
		Spec: gameKruiseV1alpha1.GameServerSpec{
			OpsState: gameKruiseV1alpha1.None,
		},
		Status: gameKruiseV1alpha1.GameServerStatus{
			CurrentState: gameKruiseV1alpha1.Ready,
		},
	}
	deletionTime := metav1.Now()

	tests := []struct {
		name string
		new  func() *gameKruiseV1alpha1.GameServer
		want bool
	}{
		{
			name: "spec opsState change enqueues",
			new: func() *gameKruiseV1alpha1.GameServer {
				gs := base.DeepCopy()
				gs.Spec.OpsState = gameKruiseV1alpha1.Allocated
				return gs
			},
			want: true,
		},
		{
			name: "gs-sync annotation change enqueues",
			new: func() *gameKruiseV1alpha1.GameServer {
				gs := base.DeepCopy()
				gs.Annotations["gs-sync/match-id"] = "match-1"
				return gs
			},
			want: true,
		},
		{
			name: "plain annotation change does not enqueue",
			new: func() *gameKruiseV1alpha1.GameServer {
				gs := base.DeepCopy()
				gs.Annotations["plain"] = "new"
				return gs
			},
			want: false,
		},
		{
			name: "status-only change does not enqueue",
			new: func() *gameKruiseV1alpha1.GameServer {
				gs := base.DeepCopy()
				gs.Status.CurrentState = gameKruiseV1alpha1.NotReady
				return gs
			},
			want: false,
		},
		{
			name: "deletion timestamp enqueues",
			new: func() *gameKruiseV1alpha1.GameServer {
				gs := base.DeepCopy()
				gs.DeletionTimestamp = &deletionTime
				return gs
			},
			want: true,
		},
	}

	pred := newGameServerPredicate()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pred.Update(event.TypedUpdateEvent[*gameKruiseV1alpha1.GameServer]{
				ObjectOld: base.DeepCopy(),
				ObjectNew: tt.new(),
			})
			if got != tt.want {
				t.Fatalf("expect enqueue=%v, got %v", tt.want, got)
			}
		})
	}
}
