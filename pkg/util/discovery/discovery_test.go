/*
Copyright 2025 The Kruise Authors.

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

package discovery

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	ktesting "k8s.io/client-go/testing"
)

type mockDiscoveryWithError struct {
	fakediscovery.FakeDiscovery 
}

func (m *mockDiscoveryWithError) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	return nil, fmt.Errorf("simulated generic api server error")
}

func shortBackoff() wait.Backoff {
	return wait.Backoff{
		Steps:    1,
		Duration: 1 * time.Millisecond,
		Factor:   1.0,
	}
}

func TestDiscoverGVKWithClient(t *testing.T) {
	podGVK := corev1.SchemeGroupVersion.WithKind("Pod")
	unfoundGVK := schema.GroupVersionKind{Group: "foo.kruise.io", Version: "v1", Kind: "Bar"}

	resources := []*metav1.APIResourceList{
		{
			GroupVersion: corev1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod"}},
		},
	}

	testCases := []struct {
		name            string
		gvkToFind       schema.GroupVersionKind
		discoveryClient discovery.DiscoveryInterface
		expect          bool
	}{
		{
			name:      "GVK is found in discovery",
			gvkToFind: podGVK,
			discoveryClient: &fakediscovery.FakeDiscovery{
				Fake: &ktesting.Fake{Resources: resources},
			},
			expect: true,
		},
		{
			name:      "GVK is NOT found in discovery",
			gvkToFind: unfoundGVK,
			discoveryClient: &fakediscovery.FakeDiscovery{
				Fake: &ktesting.Fake{Resources: resources},
			},
			expect: false,
		},
		{
			name:            "API server returns a generic error",
			gvkToFind:       podGVK,
			discoveryClient: &mockDiscoveryWithError{}, 
			expect:          true,                      
		},
	}

	// Temporarily replace the backoff to make tests run fast
	originalBackoff := backOff
	backOff = shortBackoff()
	defer func() { backOff = originalBackoff }()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := discoverGVKWithClient(tc.discoveryClient, tc.gvkToFind)
			if result != tc.expect {
				t.Errorf("Expected result to be %v, but got %v", tc.expect, result)
			}
		})
	}
}

func TestDiscoverObject(t *testing.T) {
	testScheme := runtime.NewScheme()
	testScheme.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Pod{})

	originalScheme := internalScheme
	internalScheme = testScheme
	defer func() { internalScheme = originalScheme }()

	t.Run("Object is NOT registered in scheme", func(t *testing.T) {
		if DiscoverObject(&corev1.Service{}) {
			t.Errorf("Expected false for unregistered object, got true")
		}
	})
}
