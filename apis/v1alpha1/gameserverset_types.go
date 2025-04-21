/*
Copyright 2022 The Kruise Authors.

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

package v1alpha1

import (
	appspub "github.com/openkruise/kruise-api/apps/pub"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	GameServerOwnerGssKey      = "game.kruise.io/owner-gss"
	GameServerSetReserveIdsKey = "game.kruise.io/reserve-ids"
	AstsHashKey                = "game.kruise.io/asts-hash"
	PpmHashKey                 = "game.kruise.io/ppm-hash"
	GsTemplateMetadataHashKey  = "game.kruise.io/gsTemplate-metadata-hash"
)

const (
	InplaceUpdateNotReadyBlocker = "game.kruise.io/inplace-update-not-ready-blocker"
)

// GameServerSetSpec defines the desired state of GameServerSet
type GameServerSetSpec struct {
	// replicas is the desired number of replicas of the given Template.
	// These are replicas in the sense that they are instantiations of the
	// same Template, but individual replicas also have a consistent identity.
	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Minimum=0
	Replicas *int32 `json:"replicas"`
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	GameServerTemplate   GameServerTemplate   `json:"gameServerTemplate,omitempty"`
	ServiceName          string               `json:"serviceName,omitempty"`
	ReserveGameServerIds []intstr.IntOrString `json:"reserveGameServerIds,omitempty"`
	ServiceQualities     []ServiceQuality     `json:"serviceQualities,omitempty"`
	UpdateStrategy       UpdateStrategy       `json:"updateStrategy,omitempty"`
	ScaleStrategy        ScaleStrategy        `json:"scaleStrategy,omitempty"`
	Network              *Network             `json:"network,omitempty"`
	Lifecycle            *appspub.Lifecycle   `json:"lifecycle,omitempty"`
}

type GameServerTemplate struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	corev1.PodTemplateSpec `json:",inline"`
	VolumeClaimTemplates   []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty"`
	// ReclaimPolicy indicates the reclaim policy for GameServer.
	// Default is Cascade.
	ReclaimPolicy GameServerReclaimPolicy `json:"reclaimPolicy,omitempty"`
}

type GameServerReclaimPolicy string

const (
	// CascadeGameServerReclaimPolicy indicates that GameServer is deleted when the pod is deleted.
	// The age of GameServer is exactly the same as that of the pod.
	CascadeGameServerReclaimPolicy GameServerReclaimPolicy = "Cascade"
	// DeleteGameServerReclaimPolicy indicates that GameServers will be deleted when replicas of GameServerSet decreases.
	// The GameServer will not be deleted when the corresponding pod is deleted due to manual deletion, update, eviction, etc.
	DeleteGameServerReclaimPolicy GameServerReclaimPolicy = "Delete"
)

type Network struct {
	NetworkType string              `json:"networkType,omitempty"`
	NetworkConf []NetworkConfParams `json:"networkConf,omitempty"`
}

type NetworkConfParams KVParams

const (
	AllowNotReadyContainersNetworkConfName = "AllowNotReadyContainers"
)

type KVParams struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

type UpdateStrategy struct {
	// Type indicates the type of the StatefulSetUpdateStrategy.
	// Default is RollingUpdate.
	// +optional
	Type apps.StatefulSetUpdateStrategyType `json:"type,omitempty"`
	// RollingUpdate is used to communicate parameters when Type is RollingUpdateStatefulSetStrategyType.
	// +optional
	RollingUpdate *RollingUpdateStatefulSetStrategy `json:"rollingUpdate,omitempty"`
}

type RollingUpdateStatefulSetStrategy struct {
	// Partition indicates the ordinal at which the StatefulSet should be partitioned by default.
	// But if unorderedUpdate has been set:
	//   - Partition indicates the number of pods with non-updated revisions when rolling update.
	//   - It means controller will update $(replicas - partition) number of pod.
	// Default value is 0.
	// +optional
	Partition *int32 `json:"partition,omitempty"`
	// The maximum number of pods that can be unavailable during the update.
	// Value can be an absolute number (ex: 5) or a percentage of desired pods (ex: 10%).
	// Absolute number is calculated from percentage by rounding down.
	// Also, maxUnavailable can just be allowed to work with Parallel podManagementPolicy.
	// Defaults to 1.
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
	// PodUpdatePolicy indicates how pods should be updated
	// Default value is "ReCreate"
	// +optional
	PodUpdatePolicy kruiseV1beta1.PodUpdateStrategyType `json:"podUpdatePolicy,omitempty"`
	// Paused indicates that the StatefulSet is paused.
	// Default value is false
	// +optional
	Paused bool `json:"paused,omitempty"`
	// UnorderedUpdate contains strategies for non-ordered update.
	// If it is not nil, pods will be updated with non-ordered sequence.
	// Noted that UnorderedUpdate can only be allowed to work with Parallel podManagementPolicy
	// +optional
	// UnorderedUpdate *kruiseV1beta1.UnorderedUpdateStrategy `json:"unorderedUpdate,omitempty"`
	// InPlaceUpdateStrategy contains strategies for in-place update.
	// +optional
	InPlaceUpdateStrategy *appspub.InPlaceUpdateStrategy `json:"inPlaceUpdateStrategy,omitempty"`
	// MinReadySeconds indicates how long will the pod be considered ready after it's updated.
	// MinReadySeconds works with both OrderedReady and Parallel podManagementPolicy.
	// It affects the pod scale up speed when the podManagementPolicy is set to be OrderedReady.
	// Combined with MaxUnavailable, it affects the pod update speed regardless of podManagementPolicy.
	// Default value is 0, max is 300.
	// +optional
	MinReadySeconds *int32 `json:"minReadySeconds,omitempty"`
}

type ScaleStrategy struct {
	kruiseV1beta1.StatefulSetScaleStrategy `json:",inline"`
	// ScaleDownStrategyType indicates the scaling down strategy.
	// Default is GeneralScaleDownStrategyType
	// +optional
	ScaleDownStrategyType ScaleDownStrategyType `json:"scaleDownStrategyType,omitempty"`
}

// ScaleDownStrategyType is a string enumeration type that enumerates
// all possible scale down strategies for the GameServerSet controller.
// +enum
type ScaleDownStrategyType string

const (
	// GeneralScaleDownStrategyType will first consider the ReserveGameServerIds
	// field when game server scaling down. When the number of reserved game servers
	// does not meet the scale down number, continue to select and delete the game
	// servers from the current game server list.
	GeneralScaleDownStrategyType ScaleDownStrategyType = "General"
	// ReserveIdsScaleDownStrategyType will backfill the sequence numbers into
	// ReserveGameServerIds field when GameServers scale down, whether set by
	// ReserveGameServerIds field or the GameServerSet controller chooses to remove it.
	ReserveIdsScaleDownStrategyType ScaleDownStrategyType = "ReserveIds"
)

// GameServerSetStatus defines the observed state of GameServerSet
type GameServerSetStatus struct {
	// The generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// replicas from advancedStatefulSet
	Replicas                int32  `json:"replicas"`
	ReadyReplicas           int32  `json:"readyReplicas"`
	AvailableReplicas       int32  `json:"availableReplicas"`
	CurrentReplicas         int32  `json:"currentReplicas"`
	UpdatedReplicas         int32  `json:"updatedReplicas"`
	UpdatedReadyReplicas    int32  `json:"updatedReadyReplicas,omitempty"`
	MaintainingReplicas     *int32 `json:"maintainingReplicas,omitempty"`
	WaitToBeDeletedReplicas *int32 `json:"waitToBeDeletedReplicas,omitempty"`
	// LabelSelector is label selectors for query over pods that should match the replica count used by HPA.
	LabelSelector string `json:"labelSelector,omitempty"`
}

//+genclient
//+kubebuilder:object:root=true
//+kubebuilder:printcolumn:name="DESIRED",type="integer",JSONPath=".spec.replicas",description="The desired number of GameServers."
//+kubebuilder:printcolumn:name="CURRENT",type="integer",JSONPath=".status.replicas",description="The number of currently all GameServers."
//+kubebuilder:printcolumn:name="UPDATED",type="integer",JSONPath=".status.updatedReplicas",description="The number of GameServers updated."
//+kubebuilder:printcolumn:name="READY",type="integer",JSONPath=".status.readyReplicas",description="The number of GameServers ready."
//+kubebuilder:printcolumn:name="Maintaining",type="integer",JSONPath=".status.maintainingReplicas",description="The number of GameServers Maintaining."
//+kubebuilder:printcolumn:name="WaitToBeDeleted",type="integer",JSONPath=".status.waitToBeDeletedReplicas",description="The number of GameServers WaitToBeDeleted."
//+kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp",description="The age of GameServerSet."
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.labelSelector
//+kubebuilder:resource:shortName=gss

// GameServerSet is the Schema for the gameserversets API
type GameServerSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GameServerSetSpec   `json:"spec,omitempty"`
	Status GameServerSetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GameServerSetList contains a list of GameServerSet
type GameServerSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GameServerSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GameServerSet{}, &GameServerSetList{})
}
