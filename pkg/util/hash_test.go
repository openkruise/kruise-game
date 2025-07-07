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
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestGetHash(t *testing.T) {
	tests := []struct {
		objectA interface{}
		objectB interface{}
		result  bool
	}{
		{
			objectA: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "containerA",
							Image: "nginx:latest",
						},
					},
				},
			},
			objectB: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "containerB",
							Image: "nginx:latest",
						},
					},
				},
			},
			result: false,
		},
		{
			objectA: nil,
			objectB: nil,
			result:  true,
		},
		{
			objectA: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "containerA",
						},
					},
				},
			},
			objectB: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "containerB",
						},
					},
				},
			},
			result: false,
		},
	}

	for _, test := range tests {
		actual := GetHash(test.objectA) == GetHash(test.objectB)
		expect := test.result
		if expect != actual {
			t.Errorf("expect %v but got %v", expect, actual)
		}
	}
}
