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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"testing"

	gamekruisev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/client/clientset/versioned/fake"
	kruisegameinformers "github.com/openkruise/kruise-game/pkg/client/informers/externalversions"
)

func TestNewController(t *testing.T) {
	// Create fake clientset
	clientset := fake.NewSimpleClientset()

	// Create informer factory
	informerFactory := kruisegameinformers.NewSharedInformerFactory(clientset, 0)

	// Test successful controller creation
	controller, err := NewController(informerFactory)
	assert.NoError(t, err)
	assert.NotNil(t, controller)
	assert.NotNil(t, controller.gameServerLister)
	assert.NotNil(t, controller.gameServerSetLister)
}

func TestController_RecordGsWhenAdd(t *testing.T) {
	// Create fake clientset
	clientset := fake.NewSimpleClientset()

	// Create informer factory
	informerFactory := kruisegameinformers.NewSharedInformerFactory(clientset, 0)
	controller, err := NewController(informerFactory)
	assert.NoError(t, err)

	GameServersTotal.WithLabelValues().Add(0)

	// Get initial metric values
	initialTotal := testutil.ToFloat64(GameServersTotal)
	initialStateCount := testutil.ToFloat64(GameServersStateCount.WithLabelValues(string(gamekruisev1alpha1.Creating)))
	initialOpsStateCount := testutil.ToFloat64(GameServersOpsStateCount.WithLabelValues(string(gamekruisev1alpha1.WaitToDelete), "test-gss", "default"))

	// Create a GameServer object
	gs := &gamekruisev1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gs",
			Namespace: "default",
			Labels: map[string]string{
				"game.kruise.io/owner-gss": "test-gss",
			},
		},
		Spec: gamekruisev1alpha1.GameServerSpec{
			OpsState: gamekruisev1alpha1.None,
		},
		Status: gamekruisev1alpha1.GameServerStatus{
			CurrentState: gamekruisev1alpha1.Creating,
			DeletionPriority: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 10,
			},
			UpdatePriority: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 5,
			},
		},
	}

	// Call recordGsWhenAdd method
	controller.recordGsWhenAdd(gs)

	// Verify metrics are correctly increased
	assert.Equal(t, initialTotal+1, testutil.ToFloat64(GameServersTotal))
	assert.Equal(t, initialStateCount+1, testutil.ToFloat64(GameServersStateCount.WithLabelValues(string(gamekruisev1alpha1.Creating))))
	assert.Equal(t, initialOpsStateCount+1, testutil.ToFloat64(GameServersOpsStateCount.WithLabelValues(string(gamekruisev1alpha1.None), "test-gss", "default")))

	// Verify priority metrics
	assert.Equal(t, float64(10), testutil.ToFloat64(GameServerDeletionPriority.WithLabelValues("test-gs", "default")))
	assert.Equal(t, float64(5), testutil.ToFloat64(GameServerUpdatePriority.WithLabelValues("test-gs", "default")))
}

func TestController_RecordGsWhenUpdate(t *testing.T) {
	// Create fake clientset
	clientset := fake.NewSimpleClientset()

	// Create informer factory
	informerFactory := kruisegameinformers.NewSharedInformerFactory(clientset, 0)
	controller, err := NewController(informerFactory)
	assert.NoError(t, err)

	// Create old GameServer object
	oldGs := &gamekruisev1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gs",
			Namespace: "default",
			Labels: map[string]string{
				"game.kruise.io/owner-gss": "test-gss",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: gamekruisev1alpha1.GameServerSpec{
			OpsState: gamekruisev1alpha1.WaitToDelete,
		},
		Status: gamekruisev1alpha1.GameServerStatus{
			CurrentState: gamekruisev1alpha1.Creating,
			NetworkStatus: gamekruisev1alpha1.NetworkStatus{
				CurrentNetworkState: gamekruisev1alpha1.NetworkNotReady,
			},
			DeletionPriority: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 10,
			},
			UpdatePriority: &intstr.IntOrString{
				Type:   intstr.Int,
				IntVal: 5,
			},
		},
	}

	// Create new GameServer object
	newGs := oldGs.DeepCopy()
	newGs.Status.CurrentState = gamekruisev1alpha1.Ready
	newGs.Spec.OpsState = gamekruisev1alpha1.None
	newGs.Status.NetworkStatus.CurrentNetworkState = gamekruisev1alpha1.NetworkReady
	newGs.Status.DeletionPriority = &intstr.IntOrString{
		Type:   intstr.Int,
		IntVal: 20,
	}
	newGs.Status.UpdatePriority = &intstr.IntOrString{
		Type:   intstr.Int,
		IntVal: 15,
	}

	// Get initial metric values
	initialReadyCount := testutil.ToFloat64(GameServersStateCount.WithLabelValues(string(gamekruisev1alpha1.Ready)))
	initialCreatingCount := testutil.ToFloat64(GameServersStateCount.WithLabelValues(string(gamekruisev1alpha1.Creating)))
	initialNoneOpsCount := testutil.ToFloat64(GameServersOpsStateCount.WithLabelValues(string(gamekruisev1alpha1.None), "test-gss", "default"))
	initialWaitToDeleteOpsCount := testutil.ToFloat64(GameServersOpsStateCount.WithLabelValues(string(gamekruisev1alpha1.WaitToDelete), "test-gss", "default"))

	// Call recordGsWhenUpdate method
	controller.recordGsWhenUpdate(oldGs, newGs)

	// Verify metrics are correctly updated
	assert.Equal(t, initialReadyCount+1, testutil.ToFloat64(GameServersStateCount.WithLabelValues(string(gamekruisev1alpha1.Ready))))
	assert.Equal(t, initialCreatingCount-1, testutil.ToFloat64(GameServersStateCount.WithLabelValues(string(gamekruisev1alpha1.Creating))))
	assert.Equal(t, initialNoneOpsCount+1, testutil.ToFloat64(GameServersOpsStateCount.WithLabelValues(string(gamekruisev1alpha1.None), "test-gss", "default")))
	assert.Equal(t, initialWaitToDeleteOpsCount-1, testutil.ToFloat64(GameServersOpsStateCount.WithLabelValues(string(gamekruisev1alpha1.WaitToDelete), "test-gss", "default")))

	// Verify priority metrics are updated
	assert.Equal(t, float64(20), testutil.ToFloat64(GameServerDeletionPriority.WithLabelValues("test-gs", "default")))
	assert.Equal(t, float64(15), testutil.ToFloat64(GameServerUpdatePriority.WithLabelValues("test-gs", "default")))

	// Verify duration metrics are set (greater than 0)
	assert.Greater(t, testutil.ToFloat64(GameServerReadyDuration.WithLabelValues("test-gs", "default", "test-gss")), float64(0))
	assert.Greater(t, testutil.ToFloat64(GameServerNetworkReadyDuration.WithLabelValues("test-gs", "default", "test-gss")), float64(0))
}

func TestController_RecordGsWhenDelete(t *testing.T) {
	// Create fake clientset
	clientset := fake.NewSimpleClientset()

	// Create informer factory
	informerFactory := kruisegameinformers.NewSharedInformerFactory(clientset, 0)
	controller, err := NewController(informerFactory)
	assert.NoError(t, err)

	// Create a GameServer object
	gs := &gamekruisev1alpha1.GameServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-gs",
			Namespace: "default",
			Labels: map[string]string{
				"game.kruise.io/owner-gss": "test-gss",
			},
		},
		Spec: gamekruisev1alpha1.GameServerSpec{
			OpsState: gamekruisev1alpha1.WaitToDelete,
		},
		Status: gamekruisev1alpha1.GameServerStatus{
			CurrentState: gamekruisev1alpha1.Creating,
		},
	}

	// First add the GameServer to set initial metrics
	controller.recordGsWhenAdd(gs)

	// Get initial metric values after adding
	initialCreatingCount := testutil.ToFloat64(GameServersStateCount.WithLabelValues(string(gamekruisev1alpha1.Creating)))
	initialWaitToDeleteOpsCount := testutil.ToFloat64(GameServersOpsStateCount.WithLabelValues(string(gamekruisev1alpha1.WaitToDelete), "test-gss", "default"))

	// Call recordGsWhenDelete method
	controller.recordGsWhenDelete(gs)

	// Verify metrics are correctly decreased
	assert.Equal(t, initialCreatingCount-1, testutil.ToFloat64(GameServersStateCount.WithLabelValues(string(gamekruisev1alpha1.Creating))))
	assert.Equal(t, initialWaitToDeleteOpsCount-1, testutil.ToFloat64(GameServersOpsStateCount.WithLabelValues(string(gamekruisev1alpha1.WaitToDelete), "test-gss", "default")))

	// Verify priority metrics are deleted (should return 0 when not found)
	assert.Equal(t, 0, testutil.CollectAndCount(GameServerDeletionPriority))
	assert.Equal(t, 0, testutil.CollectAndCount(GameServerUpdatePriority))
}
