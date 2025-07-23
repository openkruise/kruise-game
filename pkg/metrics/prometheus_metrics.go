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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

func init() {
	metrics.Registry.MustRegister(GameServersStateCount)
	metrics.Registry.MustRegister(GameServersOpsStateCount)
	metrics.Registry.MustRegister(GameServersTotal)
	metrics.Registry.MustRegister(GameServerSetsReplicasCount)
	metrics.Registry.MustRegister(GameServerDeletionPriority)
	metrics.Registry.MustRegister(GameServerUpdatePriority)
}

var (
	GameServersStateCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "okg_gameservers_state_count",
			Help: "The number of gameservers per state",
		},
		[]string{"state"},
	)
	GameServersOpsStateCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "okg_gameservers_opsState_count",
			Help: "The number of gameservers per opsState",
		},
		[]string{"opsState", "gssName", "namespace"},
	)
	GameServersTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "okg_gameservers_total",
			Help: "The total of gameservers",
		},
		[]string{},
	)
	GameServerSetsReplicasCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "okg_gameserversets_replicas_count",
			Help: "The number of replicas per gameserverset)",
		},
		[]string{"gssName", "gssNs", "gsStatus"},
	)
	GameServerDeletionPriority = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "okg_gameserver_deletion_priority",
			Help: "The deletionPriority of gameserver.)",
		},
		[]string{"gsName", "gsNs"},
	)
	GameServerUpdatePriority = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "okg_gameserver_update_priority",
			Help: "The updatePriority of gameserver.)",
		},
		[]string{"gsName", "gsNs"},
	)
)
