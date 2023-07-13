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
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sort"
	"testing"
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
	}

	for _, test := range tests {
		after := DeleteSequenceGs(test.before)
		sort.Sort(after)
		expect := test.after
		actual := GetIndexListFromPodList(after)
		for i := 0; i < len(actual); i++ {
			if expect[i] != actual[i] {
				t.Errorf("expect %v but got %v", expect, actual)
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
