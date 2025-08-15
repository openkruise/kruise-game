/*Copyright 2022 The Kruise Authors.

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

package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type fakeFieldIndexer struct {
	called bool
	err    error
}

func (f *fakeFieldIndexer) IndexField(ctx context.Context, obj client.Object, field string, extractFunc client.IndexerFunc) error {
	f.called = true
	return f.err
}

type fakeManager struct {
	manager.Manager
	indexer client.FieldIndexer
}

func (f *fakeManager) GetFieldIndexer() client.FieldIndexer {
	return f.indexer
}

func TestSetupWithManager_Success(t *testing.T) {
	// Backup and restore the original funcs
	originalFuncs := controllerAddFuncs
	defer func() { controllerAddFuncs = originalFuncs }()

	called := []string{}

	controllerAddFuncs = []func(manager.Manager) error{
		func(m manager.Manager) error {
			called = append(called, "gameserver")
			return nil
		},
		func(m manager.Manager) error {
			called = append(called, "gameserverset")
			return nil
		},
	}

	indexer := &fakeFieldIndexer{}
	mgr := &fakeManager{indexer: indexer}

	err := SetupWithManager(mgr)
	assert.NoError(t, err)
	assert.True(t, indexer.called)
	assert.Equal(t, []string{"gameserver", "gameserverset"}, called)
}

func TestSetupWithManager_IndexerFails(t *testing.T) {
	mgr := &fakeManager{
		indexer: &fakeFieldIndexer{
			err: errors.New("indexer failure"),
		},
	}

	err := SetupWithManager(mgr)
	assert.Error(t, err)
	assert.EqualError(t, err, "indexer failure")
}

func TestSetupWithManager_NoKindMatchError(t *testing.T) {
	originalFuncs := controllerAddFuncs
	defer func() { controllerAddFuncs = originalFuncs }()

	// Simulate NoKindMatchError
	controllerAddFuncs = []func(manager.Manager) error{
		func(m manager.Manager) error {
			return &metav1.NoKindMatchError{
				GroupKind: schema.GroupKind{
					Group: "game.kruise.io",
					Kind:  "GameServer",
				},
			}
		},
	}

	mgr := &fakeManager{
		indexer: &fakeFieldIndexer{},
	}

	err := SetupWithManager(mgr)
	assert.NoError(t, err)
}
