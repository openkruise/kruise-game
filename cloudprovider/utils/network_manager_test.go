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

package utils

import (
	"testing"

	"github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewNetworkManager(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name           string
		pod            *corev1.Pod
		expectNil      bool
		expectType     string
		expectConfs    int
		expectDisabled bool
	}{
		{
			name: "valid pod with network type",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType: "Kubernetes-HostPort",
					},
				},
			},
			expectNil:      false,
			expectType:     "Kubernetes-HostPort",
			expectDisabled: false,
		},
		{
			name: "pod without network type annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			},
			expectNil: true,
		},
		// NOTE: Removed "pod with nil annotations map" test case as it's logically
		// redundant with "pod without network type annotation" - in Go, reading from
		// a nil map behaves the same as reading a missing key from a valid map.
		{
			name: "pod with empty network type annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType: "", // empty string, not missing
					},
				},
			},
			// NOTE: Current implementation treats empty string as valid network type
			// This may be a design smell - empty string should likely return nil
			expectNil:      false,
			expectType:     "",
			expectDisabled: false,
		},
		{
			name: "pod with network type and config",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType: "AlibabaCloud-NLB",
						v1alpha1.GameServerNetworkConf: `[{"name":"Port","value":"80"},{"name":"Protocol","value":"TCP"}]`,
					},
				},
			},
			expectNil:      false,
			expectType:     "AlibabaCloud-NLB",
			expectConfs:    2,
			expectDisabled: false,
		},
		{
			name: "pod with invalid network config json syntax",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType: "AlibabaCloud-NLB",
						v1alpha1.GameServerNetworkConf: `invalid-json`,
					},
				},
			},
			// NOTE: Returns nil silently on invalid JSON - this is a design smell
			// Ideally should return (*NetworkManager, error) to report parsing failures
			expectNil: true,
		},
		{
			name: "pod with network disabled label true",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType: "Kubernetes-HostPort",
					},
					Labels: map[string]string{
						v1alpha1.GameServerNetworkDisabled: "true",
					},
				},
			},
			expectNil:      false,
			expectType:     "Kubernetes-HostPort",
			expectDisabled: true,
		},
		{
			name: "pod with network disabled label false",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType: "Kubernetes-HostPort",
					},
					Labels: map[string]string{
						v1alpha1.GameServerNetworkDisabled: "false",
					},
				},
			},
			expectNil:      false,
			expectType:     "Kubernetes-HostPort",
			expectDisabled: false,
		},
		{
			name: "pod with invalid network disabled label value",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType: "Kubernetes-HostPort",
					},
					Labels: map[string]string{
						v1alpha1.GameServerNetworkDisabled: "not-a-bool", // invalid boolean
					},
				},
			},
			// NOTE: Current implementation logs warning but still creates manager
			// with networkDisabled=false (default). This is lenient error handling.
			expectNil:      false,
			expectType:     "Kubernetes-HostPort",
			expectDisabled: false, // Explicitly test that invalid value defaults to false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.pod).Build()
			nm := NewNetworkManager(tt.pod, fakeClient)

			if tt.expectNil {
				if nm != nil {
					t.Errorf("expected nil NetworkManager, got non-nil")
				}
				return
			}

			if nm == nil {
				t.Fatalf("expected non-nil NetworkManager, got nil")
			}

			if nm.GetNetworkType() != tt.expectType {
				t.Errorf("expected network type %s, got %s", tt.expectType, nm.GetNetworkType())
			}

			if tt.expectConfs > 0 && len(nm.GetNetworkConfig()) != tt.expectConfs {
				t.Errorf("expected %d network configs, got %d", tt.expectConfs, len(nm.GetNetworkConfig()))
			}

			// Explicitly test the disabled state for all non-nil cases
			if nm.GetNetworkDisabled() != tt.expectDisabled {
				t.Errorf("expected network disabled %v, got %v", tt.expectDisabled, nm.GetNetworkDisabled())
			}
		})
	}
}

func TestNetworkManager_GetNetworkStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name        string
		pod         *corev1.Pod
		expectNil   bool
		expectError bool
	}{
		{
			name: "pod with valid network status",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType:   "Kubernetes-HostPort",
						v1alpha1.GameServerNetworkStatus: `{"currentNetworkState":"Ready","internalAddresses":[{"ip":"10.0.0.1"}]}`,
					},
				},
			},
			expectNil:   false,
			expectError: false,
		},
		{
			name: "pod without network status annotation (nil)",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType: "Kubernetes-HostPort",
						// GameServerNetworkStatus not present
					},
				},
			},
			expectNil:   true,
			expectError: false,
		},
		{
			name: "pod with empty string network status annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType:   "Kubernetes-HostPort",
						v1alpha1.GameServerNetworkStatus: "", // empty string, not missing
					},
				},
			},
			// Empty string should return nil status, no error
			expectNil:   true,
			expectError: false,
		},
		{
			name: "pod with invalid network status json",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType:   "Kubernetes-HostPort",
						v1alpha1.GameServerNetworkStatus: `invalid-json`,
					},
				},
			},
			expectNil:   true,
			expectError: true,
		},
		{
			name: "pod with valid json but empty status object",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						v1alpha1.GameServerNetworkType:   "Kubernetes-HostPort",
						v1alpha1.GameServerNetworkStatus: `{}`, // valid JSON, empty object
					},
				},
			},
			expectNil:   false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.pod).Build()
			nm := NewNetworkManager(tt.pod, fakeClient)

			if nm == nil {
				t.Fatalf("expected non-nil NetworkManager")
			}

			status, err := nm.GetNetworkStatus()

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if tt.expectNil && status != nil {
				t.Errorf("expected nil network status, got non-nil")
			}

			if !tt.expectNil && status == nil && !tt.expectError {
				t.Errorf("expected non-nil network status, got nil")
			}
		})
	}
}

func TestNetworkManager_UpdateNetworkStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Annotations: map[string]string{
				v1alpha1.GameServerNetworkType: "Kubernetes-HostPort",
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pod).Build()
	nm := NewNetworkManager(pod, fakeClient)

	if nm == nil {
		t.Fatalf("expected non-nil NetworkManager")
	}

	networkStatus := v1alpha1.NetworkStatus{
		CurrentNetworkState: v1alpha1.NetworkReady,
		InternalAddresses: []v1alpha1.NetworkAddress{
			{
				IP: "10.0.0.1",
			},
		},
	}

	updatedPod, err := nm.UpdateNetworkStatus(networkStatus, pod)
	if err != nil {
		t.Errorf("unexpected error updating network status: %v", err)
	}

	if updatedPod.Annotations[v1alpha1.GameServerNetworkStatus] == "" {
		t.Errorf("expected network status annotation to be set")
	}
}
