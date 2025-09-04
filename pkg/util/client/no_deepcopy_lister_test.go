/*
Copyright 2022 The Kruise Authors.
Copyright 2018 The Kubernetes Authors.

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

package client

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mockInformer struct {
	indexer toolscache.Indexer
}

func (i *mockInformer) AddEventHandler(handler toolscache.ResourceEventHandler) (toolscache.ResourceEventHandlerRegistration, error) {
	return nil, nil
}

func (i *mockInformer) AddEventHandlerWithResyncPeriod(handler toolscache.ResourceEventHandler, resyncPeriod time.Duration) (toolscache.ResourceEventHandlerRegistration, error) {
	return nil, nil
}

func (i *mockInformer) GetStore() toolscache.Store {
	return i.indexer
}

func (i *mockInformer) GetController() toolscache.Controller {
	return nil
}

func (i *mockInformer) Run(stopCh <-chan struct{}) {}

func (i *mockInformer) HasSynced() bool {
	return true
}

func (i *mockInformer) LastSyncResourceVersion() string {
	return ""
}

func (i *mockInformer) SetWatchErrorHandler(handler toolscache.WatchErrorHandler) error {
	return nil
}

func (i *mockInformer) IsStopped() bool {
	return false
}

func (i *mockInformer) AddIndexers(indexers toolscache.Indexers) error {
	return i.indexer.AddIndexers(indexers)
}

func (i *mockInformer) GetIndexer() toolscache.Indexer {
	return i.indexer
}

func (i *mockInformer) RemoveEventHandler(handle toolscache.ResourceEventHandlerRegistration) error {
	return nil
}

func (i *mockInformer) SetTransform(handler toolscache.TransformFunc) error {
	return nil
}

var _ toolscache.SharedIndexInformer = &mockInformer{}

type mockCache struct {
	informer cache.Informer
}

func (c *mockCache) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return fmt.Errorf("not implemented")
}
func (c *mockCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	return fmt.Errorf("not implemented")
}
func (c *mockCache) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return c.informer, nil
}
func (c *mockCache) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind, opts ...cache.InformerGetOption) (cache.Informer, error) {
	return c.informer, nil
}
func (c *mockCache) Start(ctx context.Context) error {
	return nil
}
func (c *mockCache) WaitForCacheSync(ctx context.Context) bool {
	return true
}
func (c *mockCache) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	return nil
}
func (c *mockCache) RemoveInformer(ctx context.Context, obj client.Object) error {
	return nil
}

func newTestLister(t *testing.T, scheme *runtime.Scheme, objs ...runtime.Object) *noDeepCopyLister {
	indexer := toolscache.NewIndexer(toolscache.MetaNamespaceKeyFunc, toolscache.Indexers{
		toolscache.NamespaceIndex: toolscache.MetaNamespaceIndexFunc,
		FieldIndexName("metadata.name"): func(obj interface{}) ([]string, error) {
			meta, err := apimeta.Accessor(obj)
			if err != nil {
				return nil, err
			}
			allNamespacesKey := KeyToNamespacedKey("", meta.GetName())

			if meta.GetNamespace() == "" {
				return []string{allNamespacesKey}, nil
			}

			namespacedKey := KeyToNamespacedKey(meta.GetNamespace(), meta.GetName())
			return []string{namespacedKey, allNamespacesKey}, nil
		},
	})

	for _, obj := range objs {
		err := indexer.Add(obj)
		assert.NoError(t, err)
	}

	mockInformer := &mockInformer{indexer: indexer}
	mockCache := &mockCache{informer: mockInformer}

	return &noDeepCopyLister{
		cache:  mockCache,
		scheme: scheme,
	}
}

func TestNoDeepCopyLister_List(t *testing.T) {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	assert.NoError(t, err)

	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "ns1", Labels: map[string]string{"app": "app1"}}}
	pod2 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "ns1", Labels: map[string]string{"app": "app2"}}}
	pod3 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3", Namespace: "ns2", Labels: map[string]string{"app": "app1"}}}
	pod4 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-4", Namespace: "ns2", Labels: map[string]string{"app": "app2"}}}

	lister := newTestLister(t, scheme, pod1, pod2, pod3, pod4)

	u1 := &unstructured.Unstructured{}
	u1.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Pod"))
	u1.SetName("pod-1")
	u1.SetNamespace("ns1")
	u1.SetLabels(map[string]string{"app": "app1"})

	unstructuredLister := newTestLister(t, scheme, u1)

	nonExactSelector, err := fields.ParseSelector("metadata.name!=pod-1")
	assert.NoError(t, err)

	testCases := []struct {
		name          string
		lister        *noDeepCopyLister
		listObj       client.ObjectList
		opts          []client.ListOption
		expectedNames []string
		expectErr     bool
	}{
		{
			name:          "List all pods in all namespaces",
			lister:        lister,
			listObj:       &corev1.PodList{},
			opts:          []client.ListOption{},
			expectedNames: []string{"pod-1", "pod-2", "pod-3", "pod-4"},
		},
		{
			name:          "List pods in a specific namespace",
			lister:        lister,
			listObj:       &corev1.PodList{},
			opts:          []client.ListOption{client.InNamespace("ns1")},
			expectedNames: []string{"pod-1", "pod-2"},
		},
		{
			name:          "List pods with a label selector",
			lister:        lister,
			listObj:       &corev1.PodList{},
			opts:          []client.ListOption{client.MatchingLabels{"app": "app1"}},
			expectedNames: []string{"pod-1", "pod-3"},
		},
		{
			name:    "List pods with namespace and label selector",
			lister:  lister,
			listObj: &corev1.PodList{},
			opts: []client.ListOption{
				client.InNamespace("ns2"),
				client.MatchingLabels{"app": "app2"},
			},
			expectedNames: []string{"pod-4"},
		},
		{
			name:          "List with a label selector that matches nothing",
			lister:        lister,
			listObj:       &corev1.PodList{},
			opts:          []client.ListOption{client.MatchingLabels{"app": "non-existent"}},
			expectedNames: []string{},
		},
		{
			name:    "List with a field selector",
			lister:  lister,
			listObj: &corev1.PodList{},
			opts: []client.ListOption{
				client.MatchingFields{"metadata.name": "pod-2"},
			},
			expectedNames: []string{"pod-2"},
		},
		{
			name:    "List with a field selector and namespace",
			lister:  lister,
			listObj: &corev1.PodList{},
			opts: []client.ListOption{
				client.InNamespace("ns1"),
				client.MatchingFields{"metadata.name": "pod-1"},
			},
			expectedNames: []string{"pod-1"},
		},
		{
			name:      "List with a non-exact field selector (should fail)",
			lister:    lister,
			listObj:   &corev1.PodList{},
			opts:      []client.ListOption{client.MatchingFieldsSelector{Selector: nonExactSelector}},
			expectErr: true,
		},
		{
			name:    "List with a limit",
			lister:  lister,
			listObj: &corev1.PodList{},
			opts: []client.ListOption{
				client.MatchingLabels{"app": "app1"},
				client.Limit(1),
			},
			expectedNames: []string{"pod-1"},
		},
		{
			name:   "List unstructured objects",
			lister: unstructuredLister,
			listObj: func() *unstructured.UnstructuredList {
				list := &unstructured.UnstructuredList{}
				list.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("PodList"))
				return list
			}(),
			opts:          []client.ListOption{client.InNamespace("ns1")},
			expectedNames: []string{"pod-1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			listObj := tc.listObj.DeepCopyObject().(client.ObjectList)

			err := tc.lister.List(context.Background(), listObj, tc.opts...)

			if tc.expectErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			items, err := apimeta.ExtractList(listObj)
			assert.NoError(t, err)

			actualNames := make([]string, len(items))
			for i, item := range items {
				meta, err := apimeta.Accessor(item)
				assert.NoError(t, err)
				actualNames[i] = meta.GetName()
			}

			assert.ElementsMatch(t, tc.expectedNames, actualNames)
		})
	}
}

func TestRequiresExactMatch(t *testing.T) {
	equals, _ := fields.ParseSelector("foo=bar")
	doubleEquals, _ := fields.ParseSelector("foo==bar")
	notEquals, _ := fields.ParseSelector("foo!=bar")
	multiple, _ := fields.ParseSelector("foo=bar,baz=qux")

	tests := []struct {
		name           string
		selector       fields.Selector
		expectedField  string
		expectedVal    string
		expectedResult bool
	}{
		{"equals", equals, "foo", "bar", true},
		{"double equals", doubleEquals, "foo", "bar", true},
		{"not equals", notEquals, "", "", false},
		{"multiple requirements", multiple, "", "", false},
		{"no requirements", fields.Everything(), "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, val, ok := requiresExactMatch(tt.selector)
			assert.Equal(t, tt.expectedResult, ok)
			assert.Equal(t, tt.expectedField, field)
			assert.Equal(t, tt.expectedVal, val)
		})
	}
}

func TestKeyToNamespacedKey(t *testing.T) {
	assert.Equal(t, "my-ns/my-key", KeyToNamespacedKey("my-ns", "my-key"))
	assert.Equal(t, "__all_namespaces/my-key", KeyToNamespacedKey("", "my-key"))
}
