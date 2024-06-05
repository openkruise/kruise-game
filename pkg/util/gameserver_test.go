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

package util

import (
	"reflect"
	"sort"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

func TestGetIndexFromGsName(t *testing.T) {
	tests := []struct {
		name   string
		result int
	}{
		{
			name:   "xxx-23-3",
			result: 3,
		},
		{
			name:   "www_3-12",
			result: 12,
		},
	}

	for _, test := range tests {
		actual := GetIndexFromGsName(test.name)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestDeleteSequenceGs(t *testing.T) {
	tests := []struct {
		before []corev1.Pod
		after  []int
	}{
		{
			before: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "10",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.Maintaining),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.WaitToDelete),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			after: []int{2, 0, 3, 1},
		},
		{
			before: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.Allocated),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.Maintaining),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.WaitToDelete),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.None),
							gameKruiseV1alpha1.GameServerDeletePriorityKey: "0",
						},
					},
				},
			},
			after: []int{2, 3, 0, 1},
		},
		{
			before: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-0",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.Allocated),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-1",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.Maintaining),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-2",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.WaitToDelete),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-3",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.None),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-4",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: string(gameKruiseV1alpha1.Kill),
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "xxx-5",
						Labels: map[string]string{
							gameKruiseV1alpha1.GameServerOpsStateKey: "user-define",
						},
					},
				},
			},
			after: []int{4, 2, 5, 3, 0, 1},
		},
	}

	for caseNum, test := range tests {
		after := DeleteSequenceGs(test.before)
		sort.Sort(after)
		expect := test.after
		actual := GetIndexListFromPodList(after)
		for i := 0; i < len(actual); i++ {
			if expect[i] != actual[i] {
				t.Errorf("case %d: expect %v but got %v", caseNum, expect, actual)
			}
		}
	}
}

func TestAddPrefixGameKruise(t *testing.T) {
	tests := []struct {
		s      string
		result string
	}{
		{
			s:      "healthy",
			result: "game.kruise.io/healthy",
		},
	}

	for _, test := range tests {
		actual := AddPrefixGameKruise(test.s)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestRemovePrefixGameKruise(t *testing.T) {
	tests := []struct {
		s      string
		result string
	}{
		{
			s:      "game.kruise.io/healthy",
			result: "healthy",
		},
		{
			s:      "game/healthy",
			result: "game/healthy",
		},
	}

	for _, test := range tests {
		actual := RemovePrefixGameKruise(test.s)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestGetGsTemplateMetadataHash(t *testing.T) {
	tests := []struct {
		gssA   *gameKruiseV1alpha1.GameServerSet
		gssB   *gameKruiseV1alpha1.GameServerSet
		result bool
	}{
		{
			gssA: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								GenerateName: "xxx",
								Labels: map[string]string{
									"a": "x",
								},
							},
						},
					},
				},
			},
			gssB: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"a": "x",
								},
							},
						},
					},
				},
			},
			result: true,
		},
		{
			gssA: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"a": "x",
								},
								Annotations: map[string]string{
									"a": "x",
								},
							},
						},
					},
				},
			},
			gssB: &gameKruiseV1alpha1.GameServerSet{
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"a": "x",
								},
							},
						},
					},
				},
			},
			result: false,
		},
	}

	for _, test := range tests {
		actual := GetGsTemplateMetadataHash(test.gssA) == GetGsTemplateMetadataHash(test.gssB)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}

func TestIsAllowNotReadyContainers(t *testing.T) {
	tests := []struct {
		networkConfParams         []gameKruiseV1alpha1.NetworkConfParams
		isAllowNotReadyContainers bool
	}{
		{
			networkConfParams: []gameKruiseV1alpha1.NetworkConfParams{
				{
					Name:  gameKruiseV1alpha1.AllowNotReadyContainersNetworkConfName,
					Value: "xxx",
				},
			},
			isAllowNotReadyContainers: true,
		},
		{
			networkConfParams: []gameKruiseV1alpha1.NetworkConfParams{
				{
					Name:  "xxx",
					Value: "xxx",
				},
			},
			isAllowNotReadyContainers: false,
		},
	}

	for i, test := range tests {
		actual := IsAllowNotReadyContainers(test.networkConfParams)
		expect := test.isAllowNotReadyContainers
		if actual != expect {
			t.Errorf("case %d: expect isAllowNotReadyContainers is %v but actually got %v", i, expect, actual)
		}
	}
}

func TestInitGameServer(t *testing.T) {
	updatePriority := intstr.FromInt(0)
	deletionPriority := intstr.FromInt(0)

	tests := []struct {
		gss  *gameKruiseV1alpha1.GameServerSet
		name string
		gs   *gameKruiseV1alpha1.GameServer
	}{
		{
			gss: &gameKruiseV1alpha1.GameServerSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "GameServerSet",
					APIVersion: "game.kruise.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0",
					UID:       "xxx0",
				},
				Spec: gameKruiseV1alpha1.GameServerSetSpec{
					GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{
								Labels: map[string]string{
									"label-key": "label-value",
								},
							},
						},
					},
				},
			},
			name: "case0-1",
			gs: &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "case0-1",
					Namespace: "xxx",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind:               "GameServerSet",
							APIVersion:         "game.kruise.io/v1alpha1",
							Name:               "case0",
							UID:                "xxx0",
							Controller:         ptr.To[bool](true),
							BlockOwnerDeletion: ptr.To[bool](true),
						},
					},
					Labels: map[string]string{
						"label-key":                              "label-value",
						gameKruiseV1alpha1.GameServerOwnerGssKey: "case0",
					},
					Annotations: map[string]string{
						gameKruiseV1alpha1.GsTemplateMetadataHashKey: GetHash(metav1.ObjectMeta{
							Labels: map[string]string{
								"label-key": "label-value",
							},
						}),
					},
				},
				Spec: gameKruiseV1alpha1.GameServerSpec{
					NetworkDisabled:  false,
					OpsState:         gameKruiseV1alpha1.None,
					UpdatePriority:   &updatePriority,
					DeletionPriority: &deletionPriority,
				},
			},
		},
	}

	for i, test := range tests {
		expect := test.gs
		actual := InitGameServer(test.gss, test.name)
		if !reflect.DeepEqual(expect, actual) {
			t.Errorf("case %d: expect generated GameServer is %v but actually got %v", i, expect, actual)
		}
	}
}
