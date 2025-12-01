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

// Metric names
const (
	MetricGameServersStateCount          = "okg_gameservers_state_count"
	MetricGameServersOpsStateCount       = "okg_gameservers_opsState_count"
	MetricGameServersTotal               = "okg_gameservers_total"
	MetricGameServerSetsReplicasCount    = "okg_gameserversets_replicas_count"
	MetricGameServerDeletionPriority     = "okg_gameserver_deletion_priority"
	MetricGameServerUpdatePriority       = "okg_gameserver_update_priority"
	MetricGameServerReadyDuration        = "okg_gameserver_ready_duration_seconds"
	MetricGameServerNetworkReadyDuration = "okg_gameserver_network_ready_duration_seconds"
)

// Metric label names
const (
	LabelState     = "state"
	LabelOpsState  = "opsState"
	LabelGSSName   = "gssName"
	LabelGSSNs     = "gssNs"
	LabelGSName    = "gsName"
	LabelGSNs      = "gsNs"
	LabelGSStatus  = "gsStatus"
	LabelNamespace = "namespace"
)

func init() {
	metrics.Registry.MustRegister(GameServersStateCount)
	metrics.Registry.MustRegister(GameServersOpsStateCount)
	metrics.Registry.MustRegister(GameServersTotal)
	metrics.Registry.MustRegister(GameServerSetsReplicasCount)
	metrics.Registry.MustRegister(GameServerDeletionPriority)
	metrics.Registry.MustRegister(GameServerUpdatePriority)
	metrics.Registry.MustRegister(GameServerReadyDuration)
	metrics.Registry.MustRegister(GameServerNetworkReadyDuration)
}

var (
	GameServersStateCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricGameServersStateCount,
			Help: "The number of gameservers per state",
		},
		[]string{LabelState},
	)
	GameServersOpsStateCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricGameServersOpsStateCount,
			Help: "The number of gameservers per opsState",
		},
		[]string{LabelOpsState, LabelGSSName, LabelNamespace},
	)
	GameServersTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: MetricGameServersTotal,
			Help: "The total of gameservers",
		},
		[]string{},
	)
	GameServerSetsReplicasCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricGameServerSetsReplicasCount,
			Help: "The number of replicas per gameserverset)",
		},
		[]string{LabelGSSName, LabelGSSNs, LabelGSStatus},
	)
	GameServerDeletionPriority = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricGameServerDeletionPriority,
			Help: "The deletionPriority of gameserver.)",
		},
		[]string{LabelGSName, LabelGSNs},
	)
	GameServerUpdatePriority = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricGameServerUpdatePriority,
			Help: "The updatePriority of gameserver.)",
		},
		[]string{LabelGSName, LabelGSNs},
	)
	GameServerReadyDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricGameServerReadyDuration,
			Help: "duration seconds of gameserver from creating to ready",
		},
		[]string{LabelGSName, LabelGSNs, LabelGSSName},
	)
	GameServerNetworkReadyDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: MetricGameServerNetworkReadyDuration,
			Help: "duration seconds of gameserver from creating to network ready",
		},
		[]string{LabelGSName, LabelGSNs, LabelGSSName},
	)
)
