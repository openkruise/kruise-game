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
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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

func TestGameServerReconcileRequeuesOnConflict(t *testing.T) {
	gss := &gameKruiseV1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "xxx", Name: "xxx"},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "xxx",
			Name:      "xxx-0",
			Labels:    map[string]string{gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx"},
		},
		Spec: corev1.PodSpec{NodeName: "node-A"},
	}
	gs := &gameKruiseV1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "xxx",
			Name:      "xxx-0",
			Labels:    map[string]string{gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx"},
		},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-A"}}

	statusPatches := 0
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(gss, pod, gs, node).WithStatusSubresource(gs).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourcePatch: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				statusPatches++
				return errors.NewConflict(schema.GroupResource{Group: "game.kruise.io", Resource: "gameservers"},
					obj.GetName(), fmt.Errorf("the object has been modified"))
			},
		}).Build()
	recon := GameServerReconciler{Client: c, recorder: record.NewFakeRecorder(100)}

	result, err := recon.Reconcile(context.TODO(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "xxx", Name: "xxx-0"},
	})
	if err != nil {
		t.Fatalf("expect conflict to be retried without error, got %v", err)
	}
	if statusPatches == 0 {
		t.Fatal("expect GameServer status to be patched")
	}
	if !result.Requeue {
		t.Errorf("expect requeue after conflict, got %+v", result)
	}
}
