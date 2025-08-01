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

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	ktesting "k8s.io/client-go/testing"
)

type mockDiscovery struct {
	discovery.DiscoveryInterface
	serverResourcesFunc func(groupVersion string) (*metav1.APIResourceList, error)
}

func (m *mockDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if m.serverResourcesFunc != nil {
		return m.serverResourcesFunc(groupVersion)
	}
	panic("serverResourcesFunc not implemented for mockDiscovery")
}

func TestDiscoverGVKWithClient(t *testing.T) {
	podGVK := corev1.SchemeGroupVersion.WithKind("Pod")
	unfoundGVK := schema.GroupVersionKind{Group: "foo.kruise.io", Version: "v1", Kind: "Bar"}

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
				Fake: &ktesting.Fake{Resources: []*metav1.APIResourceList{
					{
						GroupVersion: corev1.SchemeGroupVersion.String(),
						APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod"}},
					},
				}},
			},
			expect: true,
		},
		{
			name:      "GVK is NOT found in discovery (errKindNotFound)",
			gvkToFind: unfoundGVK,
			discoveryClient: &fakediscovery.FakeDiscovery{
				Fake: &ktesting.Fake{Resources: []*metav1.APIResourceList{
					{
						GroupVersion: unfoundGVK.GroupVersion().String(),
						APIResources: []metav1.APIResource{{Name: "someother", Kind: "SomeOther"}},
					},
				}},
			},
			expect: false,
		},
		{
			name:      "API server returns a generic error",
			gvkToFind: podGVK,
			discoveryClient: &mockDiscovery{
				serverResourcesFunc: func(groupVersion string) (*metav1.APIResourceList, error) {
					return nil, fmt.Errorf("simulated generic api server error")
				},
			},
			expect: true, 
		},
		{
			name:      "API server returns a not found error",
			gvkToFind: unfoundGVK,
			discoveryClient: &mockDiscovery{
				serverResourcesFunc: func(groupVersion string) (*metav1.APIResourceList, error) {
					return nil, errors.NewNotFound(schema.GroupResource{Group: unfoundGVK.Group}, "resource not found")
				},
			},
			expect: false, 
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := discoverGVKWithClient(tc.discoveryClient, tc.gvkToFind)
			if result != tc.expect {
				t.Errorf("Expected result to be %v, but got %v", tc.expect, result)
			}
		})
	}
}

func TestDiscoverGVK(t *testing.T) {
	if !DiscoverGVK(schema.GroupVersionKind{}) {
		t.Errorf("DiscoverGVK should return true when the generic client is nil")
	}
}

func TestDiscoverObject(t *testing.T) {
	t.Run("Object registered and client is nil", func(t *testing.T) {
		schemeWithPod := runtime.NewScheme()
		schemeWithPod.AddKnownTypes(corev1.SchemeGroupVersion, &corev1.Pod{})

		originalScheme := internalScheme
		internalScheme = schemeWithPod
		defer func() { internalScheme = originalScheme }()

		if !DiscoverObject(&corev1.Pod{}) {
			t.Errorf("Expected true for a registered object when the client is nil, but got false")
		}
	})

	t.Run("Object is NOT registered in scheme", func(t *testing.T) {
		// Setup a completely empty scheme.
		emptyScheme := runtime.NewScheme()

		originalScheme := internalScheme
		internalScheme = emptyScheme
		defer func() { internalScheme = originalScheme }()

		if DiscoverObject(&corev1.Service{}) {
			t.Errorf("Expected false for an unregistered object, but got true")
		}
	})
}

func TestAddToScheme(t *testing.T) {
	AddToSchemes = append(AddToSchemes, gamekruiseiov1alpha1.AddToScheme)
	defer func() { AddToSchemes = runtime.SchemeBuilder{} }() 

	s := runtime.NewScheme()
	err := AddToScheme(s)
	if err != nil {
		t.Fatalf("AddToScheme() failed: %v", err)
	}

	gvks, _, err := s.ObjectKinds(&gamekruiseiov1alpha1.GameServer{})
	if err != nil || len(gvks) == 0 {
		t.Errorf("GameServer kind was not added to the scheme")
	}
}
