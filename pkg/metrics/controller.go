/*
Copyright 2023 The Kruise Authors.

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
	"context"
	"errors"
	"sync"

	gamekruisev1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	kruisegamevisions "github.com/openkruise/kruise-game/pkg/client/informers/externalversions"
	kruisegamelister "github.com/openkruise/kruise-game/pkg/client/listers/apis/v1alpha1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type Controller struct {
	gameServerLister             kruisegamelister.GameServerLister
	gameServerSetLister          kruisegamelister.GameServerSetLister
	gameServerSynced             cache.InformerSynced
	gameServerSetSynced          cache.InformerSynced
	stateLock                    sync.Mutex
	opsStateLock                 sync.Mutex
	gameServerStateLastChange    map[string]float64
	gameServerOpsStateLastChange map[string]float64
}

func NewController(kruisegameInformerFactory kruisegamevisions.SharedInformerFactory) (*Controller, error) {
	gameServer := kruisegameInformerFactory.Game().V1alpha1().GameServers()
	gsInformer := gameServer.Informer()

	gameServerSet := kruisegameInformerFactory.Game().V1alpha1().GameServerSets()
	gssInformer := gameServerSet.Informer()

	c := &Controller{
		gameServerLister:             gameServer.Lister(),
		gameServerSetLister:          gameServerSet.Lister(),
		gameServerSynced:             gsInformer.HasSynced,
		gameServerSetSynced:          gssInformer.HasSynced,
		stateLock:                    sync.Mutex{},
		opsStateLock:                 sync.Mutex{},
		gameServerStateLastChange:    make(map[string]float64),
		gameServerOpsStateLastChange: make(map[string]float64),
	}

	if _, err := gsInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.recordGsWhenAdd,
		UpdateFunc: c.recordGsWhenUpdate,
		DeleteFunc: c.recordGsWhenDelete,
	}); err != nil {
		return nil, err
	}

	if _, err := gssInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			c.recordGssWhenChange(newObj)
		},
		DeleteFunc: c.recordGssWhenDelete,
	}); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Controller) recordGsWhenAdd(obj interface{}) {
	gs, ok := obj.(*gamekruisev1alpha1.GameServer)
	if !ok {
		return
	}

	GameServersTotal.WithLabelValues().Inc()

	state := string(gs.Status.CurrentState)
	opsState := string(gs.Spec.OpsState)
	gssName := gs.Labels["game.kruise.io/owner-gss"]

	GameServersStateCount.WithLabelValues(state).Inc()
	GameServersOpsStateCount.WithLabelValues(opsState, gssName, gs.Namespace).Inc()

	dp := 0
	up := 0
	if gs.Status.DeletionPriority != nil {
		dp = gs.Status.DeletionPriority.IntValue()
	}
	if gs.Status.UpdatePriority != nil {
		up = gs.Status.UpdatePriority.IntValue()
	}
	GameServerDeletionPriority.WithLabelValues(gs.Name, gs.Namespace).Set(float64(dp))
	GameServerUpdatePriority.WithLabelValues(gs.Name, gs.Namespace).Set(float64(up))
}

func (c *Controller) recordGsWhenUpdate(oldObj, newObj interface{}) {
	oldGs, ok := oldObj.(*gamekruisev1alpha1.GameServer)
	if !ok {
		return
	}

	newGs, ok := newObj.(*gamekruisev1alpha1.GameServer)
	if !ok {
		return
	}

	oldState := string(oldGs.Status.CurrentState)
	oldOpsState := string(oldGs.Spec.OpsState)
	newState := string(newGs.Status.CurrentState)
	newOpsState := string(newGs.Spec.OpsState)

	gssName := newGs.Labels["game.kruise.io/owner-gss"]

	if oldState != newState {
		GameServersStateCount.WithLabelValues(newState).Inc()
		GameServersStateCount.WithLabelValues(oldState).Dec()
	}
	if oldOpsState != newOpsState {
		GameServersOpsStateCount.WithLabelValues(newOpsState, gssName, newGs.Namespace).Inc()
		GameServersOpsStateCount.WithLabelValues(oldOpsState, gssName, newGs.Namespace).Dec()
	}

	newDp := 0
	newUp := 0
	if newGs.Status.DeletionPriority != oldGs.Status.DeletionPriority {
		newDp = newGs.Status.DeletionPriority.IntValue()
	}
	if newGs.Status.UpdatePriority != oldGs.Status.UpdatePriority {
		newUp = newGs.Status.UpdatePriority.IntValue()
	}
	GameServerDeletionPriority.WithLabelValues(newGs.Name, newGs.Namespace).Set(float64(newDp))
	GameServerUpdatePriority.WithLabelValues(newGs.Name, newGs.Namespace).Set(float64(newUp))
}

func (c *Controller) recordGsWhenDelete(obj interface{}) {
	gs, ok := obj.(*gamekruisev1alpha1.GameServer)
	if !ok {
		return
	}

	state := string(gs.Status.CurrentState)
	opsState := string(gs.Spec.OpsState)
	gssName := gs.Labels["game.kruise.io/owner-gss"]

	GameServersStateCount.WithLabelValues(state).Dec()
	GameServersOpsStateCount.WithLabelValues(opsState, gssName, gs.Namespace).Dec()
	GameServerDeletionPriority.DeleteLabelValues(gs.Name, gs.Namespace)
	GameServerUpdatePriority.DeleteLabelValues(gs.Name, gs.Namespace)
}

func (c *Controller) recordGssWhenChange(obj interface{}) {
	gss, ok := obj.(*gamekruisev1alpha1.GameServerSet)
	if !ok {
		return
	}

	GameServerSetsReplicasCount.WithLabelValues(gss.Name, gss.Namespace, "current").Set(float64(gss.Status.CurrentReplicas))
	GameServerSetsReplicasCount.WithLabelValues(gss.Name, gss.Namespace, "ready").Set(float64(gss.Status.ReadyReplicas))
	GameServerSetsReplicasCount.WithLabelValues(gss.Name, gss.Namespace, "available").Set(float64(gss.Status.AvailableReplicas))
	if gss.Status.MaintainingReplicas != nil {
		GameServerSetsReplicasCount.WithLabelValues(gss.Name, gss.Namespace, "maintaining").Set(float64(*gss.Status.MaintainingReplicas))
	}
	if gss.Status.WaitToBeDeletedReplicas != nil {
		GameServerSetsReplicasCount.WithLabelValues(gss.Name, gss.Namespace, "waitToBeDeleted").Set(float64(*gss.Status.WaitToBeDeletedReplicas))
	}
}

func (c *Controller) recordGssWhenDelete(obj interface{}) {
	gss, ok := obj.(*gamekruisev1alpha1.GameServerSet)
	if !ok {
		return
	}

	GameServerSetsReplicasCount.DeleteLabelValues(gss.Name, gss.Namespace, "current")
	GameServerSetsReplicasCount.DeleteLabelValues(gss.Name, gss.Namespace, "ready")
	GameServerSetsReplicasCount.DeleteLabelValues(gss.Name, gss.Namespace, "available")
	GameServerSetsReplicasCount.DeleteLabelValues(gss.Name, gss.Namespace, "maintaining")
	GameServerSetsReplicasCount.DeleteLabelValues(gss.Name, gss.Namespace, "waitToBeDeleted")
}

func (c *Controller) Run(ctx context.Context) error {
	klog.Info("Wait for metrics controller cache sync")
	if !cache.WaitForCacheSync(ctx.Done(), c.gameServerSynced, c.gameServerSetSynced) {
		return errors.New("failed to wait for caches to sync")
	}
	<-ctx.Done()
	return nil
}
