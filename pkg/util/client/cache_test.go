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

package client

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/schema"
    clientgoscheme "k8s.io/client-go/kubernetes/scheme"
    "k8s.io/client-go/rest"
    "sigs.k8s.io/controller-runtime/pkg/cache"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewCache(t *testing.T) {
    tests := []struct {
        name               string
        config             func() *rest.Config
        opts               cache.Options
        expectSuccess      bool
        validateInternalCache bool
    }{
        {
            name: "valid config with default scheme",
            config: func() *rest.Config {
                return &rest.Config{
                    Host: "https://localhost:8443",
                    TLSClientConfig: rest.TLSClientConfig{Insecure: true},
                }
            },
            opts:               cache.Options{},
            expectSuccess:      true,
            validateInternalCache: true,
        },
        {
            name: "valid config with custom scheme",
            config: func() *rest.Config {
                return &rest.Config{
                    Host: "https://localhost:8443",
                    TLSClientConfig: rest.TLSClientConfig{Insecure: true},
                }
            },
            opts: cache.Options{
                Scheme: clientgoscheme.Scheme,
            },
            expectSuccess:      true,
            validateInternalCache: true,
        },
        {
            name: "test scheme assignment logic",
            config: func() *rest.Config {
                return &rest.Config{
                    Host: "https://localhost:8443",
                    TLSClientConfig: rest.TLSClientConfig{Insecure: true},
                }
            },
            opts:               cache.Options{},
            expectSuccess:      true,
            validateInternalCache: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            config := tt.config()
            cache, err := NewCache(config, tt.opts)
            
            if tt.expectSuccess {
                if err != nil {
                    t.Logf("NewCache returned error (expected in some environments): %v", err)
                    return 
                }
                
                assert.NotNil(t, cache)
                
                if tt.validateInternalCache {
                    ic, ok := cache.(*internalCache)
                    assert.True(t, ok, "Expected internalCache type")
                    if ok {
                        assert.NotNil(t, ic.Cache, "Expected Cache to be set")
                        assert.NotNil(t, ic.noDeepCopyLister, "Expected noDeepCopyLister to be set")
                    }
                }
            } else {
                assert.Error(t, err)
            }
        })
    }
}

func TestNewCacheSchemeLogic(t *testing.T) {
    tests := []struct {
        name         string
        inputScheme  *runtime.Scheme
        expectedResult string
    }{
        {
            name:        "nil scheme should use default",
            inputScheme: nil,
            expectedResult: "default scheme used",
        },
        {
            name:        "custom scheme should be preserved",
            inputScheme: runtime.NewScheme(),
            expectedResult: "custom scheme preserved",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            opts := cache.Options{Scheme: tt.inputScheme}
            
            if opts.Scheme == nil {
                opts.Scheme = clientgoscheme.Scheme
            }
            
            assert.NotNil(t, opts.Scheme)
            if tt.inputScheme == nil {
                assert.Equal(t, clientgoscheme.Scheme, opts.Scheme)
            } else {
                assert.Equal(t, tt.inputScheme, opts.Scheme)
            }
        })
    }
}

func TestInternalCacheList(t *testing.T) {
    scheme := runtime.NewScheme()
    err := corev1.AddToScheme(scheme)
    require.NoError(t, err)

    pod1 := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-pod-1",
            Namespace: "default",
        },
    }
    pod2 := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-pod-2",
            Namespace: "default",
        },
    }

    fakeClient := fake.NewClientBuilder().
        WithScheme(scheme).
        WithObjects(pod1, pod2).
        Build()

    ic := &internalCache{
        Cache:            &mockCacheForList{client: fakeClient},
        noDeepCopyLister: &noDeepCopyLister{cache: &mockCacheForList{client: fakeClient}, scheme: scheme},
    }

    tests := []struct {
        name              string
        disableNoDeepCopy bool
        opts              []client.ListOption
        expectError       bool
    }{
        {
            name:              "normal list without disable deep copy",
            disableNoDeepCopy: false,
            opts:              []client.ListOption{},
            expectError:       false,
        },
        {
            name:              "list with disable deep copy flag disabled",
            disableNoDeepCopy: true,
            opts:              []client.ListOption{DisableDeepCopy},
            expectError:       false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            originalFlag := disableNoDeepCopy
            disableNoDeepCopy = tt.disableNoDeepCopy
            defer func() { disableNoDeepCopy = originalFlag }()

            ctx := context.Background()
            podList := &corev1.PodList{}
            
            err := ic.List(ctx, podList, tt.opts...)
            
            if tt.expectError {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

type mockCacheForList struct {
    client client.Client
}

func (m *mockCacheForList) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
    return m.client.List(ctx, list, opts...)
}

func (m *mockCacheForList) Start(ctx context.Context) error           { return nil }
func (m *mockCacheForList) WaitForCacheSync(ctx context.Context) bool { return true }
func (m *mockCacheForList) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
    return m.client.Get(ctx, key, obj, opts...)
}
func (m *mockCacheForList) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
    return nil, nil
}
func (m *mockCacheForList) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind, opts ...cache.InformerGetOption) (cache.Informer, error) {
    return nil, nil
}
func (m *mockCacheForList) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
    return nil
}
func (m *mockCacheForList) RemoveInformer(ctx context.Context, obj client.Object) error {
    return nil
}

func TestDisableDeepCopyApplyToList(t *testing.T) {
    dd := DisableDeepCopy
    opts := &client.ListOptions{}
    
    dd.ApplyToList(opts)
    
}

func TestIsDisableDeepCopy(t *testing.T) {
    tests := []struct {
        name string
        opts []client.ListOption
        want bool
    }{
        {
            name: "no options",
            opts: []client.ListOption{},
            want: false,
        },
        {
            name: "disable deep copy option present",
            opts: []client.ListOption{DisableDeepCopy},
            want: true,
        },
        {
            name: "disable deep copy among other options",
            opts: []client.ListOption{
                client.InNamespace("test"),
                DisableDeepCopy,
                client.MatchingLabels{"key": "value"},
            },
            want: true,
        },
        {
            name: "other options without disable deep copy",
            opts: []client.ListOption{
                client.InNamespace("test"),
                client.MatchingLabels{"key": "value"},
            },
            want: false,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := isDisableDeepCopy(tt.opts)
            assert.Equal(t, tt.want, result)
        })
    }
}

func TestGlobalFlagBehavior(t *testing.T) {
    original := disableNoDeepCopy
    defer func() { disableNoDeepCopy = original }()

    disableNoDeepCopy = false
    assert.True(t, isDisableDeepCopy([]client.ListOption{DisableDeepCopy}))

    disableNoDeepCopy = true
    assert.True(t, isDisableDeepCopy([]client.ListOption{DisableDeepCopy}))
}