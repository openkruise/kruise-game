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
