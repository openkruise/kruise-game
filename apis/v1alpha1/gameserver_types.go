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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	GameServerStateKey           = "game.kruise.io/gs-state"
	GameServerOpsStateKey        = "game.kruise.io/gs-opsState"
	GameServerUpdatePriorityKey  = "game.kruise.io/gs-update-priority"
	GameServerDeletePriorityKey  = "game.kruise.io/gs-delete-priority"
	GameServerDeletingKey        = "game.kruise.io/gs-deleting"
	GameServerNetworkType        = "game.kruise.io/network-type"
	GameServerNetworkConf        = "game.kruise.io/network-conf"
	GameServerNetworkDisabled    = "game.kruise.io/network-disabled"
	GameServerNetworkStatus      = "game.kruise.io/network-status"
	GameServerNetworkTriggerTime = "game.kruise.io/network-trigger-time"
)

// GameServerSpec defines the desired state of GameServer
type GameServerSpec struct {
	OpsState         OpsState            `json:"opsState,omitempty"`
	UpdatePriority   *intstr.IntOrString `json:"updatePriority,omitempty"`
	DeletionPriority *intstr.IntOrString `json:"deletionPriority,omitempty"`
	NetworkDisabled  bool                `json:"networkDisabled,omitempty"`
	// Containers can be used to make the corresponding GameServer container fields
	// different from the fields defined by GameServerTemplate in GameServerSetSpec.
	Containers []GameServerContainer `json:"containers,omitempty"`
}

type GameServerContainer struct {
	// Name indicates the name of the container to update.
	Name string `json:"name"`
	// Image indicates the image of the container to update.
	// When Image updated, pod.spec.containers[*].image will be updated immediately.
	Image string `json:"image,omitempty"`
	// Resources indicates the resources of the container to update.
	// When Resources updated, pod.spec.containers[*].Resources will be not updated immediately,
	// which will be updated when pod recreate.
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

type GameServerState string

const (
	Unknown  GameServerState = "Unknown"
	Creating GameServerState = "Creating"
	Ready    GameServerState = "Ready"
	NotReady GameServerState = "NotReady"
	Crash    GameServerState = "Crash"
	Updating GameServerState = "Updating"
	Deleting GameServerState = "Deleting"
)

type OpsState string

const (
	Maintaining  OpsState = "Maintaining"
	WaitToDelete OpsState = "WaitToBeDeleted"
	None         OpsState = "None"
	Allocated    OpsState = "Allocated"
	Kill         OpsState = "Kill"
)

type ServiceQuality struct {
	corev1.Probe  `json:",inline"`
	Name          string `json:"name"`
	ContainerName string `json:"containerName,omitempty"`
	// Whether to make GameServerSpec not change after the ServiceQualityAction is executed.
	// When Permanent is true, regardless of the detection results, ServiceQualityAction will only be executed once.
	// When Permanent is false, ServiceQualityAction can be executed again even though ServiceQualityAction has been executed.
	Permanent            bool                   `json:"permanent"`
	ServiceQualityAction []ServiceQualityAction `json:"serviceQualityAction,omitempty"`
}

type ServiceQualityCondition struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
	// Result indicate the probe message returned by the script
	Result                   string      `json:"result,omitempty"`
	LastProbeTime            metav1.Time `json:"lastProbeTime,omitempty"`
	LastTransitionTime       metav1.Time `json:"lastTransitionTime,omitempty"`
	LastActionTransitionTime metav1.Time `json:"lastActionTransitionTime,omitempty"`
}

type ServiceQualityAction struct {
	State bool `json:"state"`
	// Result indicate the probe message returned by the script.
	// When Result is defined, it would exec action only when the according Result is actually returns.
	Result         string `json:"result,omitempty"`
	GameServerSpec `json:",inline"`
}

// GameServerStatus defines the observed state of GameServer
type GameServerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	DesiredState              GameServerState           `json:"desiredState,omitempty"`
	CurrentState              GameServerState           `json:"currentState,omitempty"`
	NetworkStatus             NetworkStatus             `json:"networkStatus,omitempty"`
	PodStatus                 corev1.PodStatus          `json:"podStatus,omitempty"`
	ServiceQualitiesCondition []ServiceQualityCondition `json:"serviceQualitiesConditions,omitempty"`
	// Lifecycle defines the lifecycle hooks for Pods pre-delete, in-place update.
	UpdatePriority     *intstr.IntOrString `json:"updatePriority,omitempty"`
	DeletionPriority   *intstr.IntOrString `json:"deletionPriority,omitempty"`
	LastTransitionTime metav1.Time         `json:"lastTransitionTime,omitempty"`
	// Conditions is an array of current observed GameServer conditions.
	// +optional
	Conditions []GameServerCondition `json:"conditions,omitempty" `
}

type GameServerCondition struct {
	// Type is the type of the condition.
	Type GameServerConditionType `json:"type"`
	// Status is the status of the condition.
	// Can be True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// Last time we probed the condition.
	// +optional
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// Unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

type GameServerConditionType string

const (
	NodeNormal             GameServerConditionType = "NodeNormal"
	PersistentVolumeNormal GameServerConditionType = "PersistentVolumeNormal"
	PodNormal              GameServerConditionType = "PodNormal"
)

type NetworkStatus struct {
	NetworkType         string           `json:"networkType,omitempty"`
	InternalAddresses   []NetworkAddress `json:"internalAddresses,omitempty"`
	ExternalAddresses   []NetworkAddress `json:"externalAddresses,omitempty"`
	DesiredNetworkState NetworkState     `json:"desiredNetworkState,omitempty"`
	CurrentNetworkState NetworkState     `json:"currentNetworkState,omitempty"`
	CreateTime          metav1.Time      `json:"createTime,omitempty"`
	LastTransitionTime  metav1.Time      `json:"lastTransitionTime,omitempty"`
}

type NetworkState string

const (
	NetworkReady    NetworkState = "Ready"
	NetworkWaiting  NetworkState = "Waiting"
	NetworkNotReady NetworkState = "NotReady"
)

type NetworkAddress struct {
	IP string `json:"ip"`
	// TODO add IPv6
	Ports     []NetworkPort     `json:"ports,omitempty"`
	PortRange *NetworkPortRange `json:"portRange,omitempty"`
	EndPoint  string            `json:"endPoint,omitempty"`
}

type NetworkPort struct {
	Name     string              `json:"name"`
	Protocol corev1.Protocol     `json:"protocol,omitempty"`
	Port     *intstr.IntOrString `json:"port,omitempty"`
}

type NetworkPortRange struct {
	Protocol  corev1.Protocol `json:"protocol,omitempty"`
	PortRange string          `json:"portRange,omitempty"`
}

//+genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="STATE",type="string",JSONPath=".status.currentState",description="The current state of GameServer"
//+kubebuilder:printcolumn:name="OPSSTATE",type="string",JSONPath=".spec.opsState",description="The operations state of GameServer"
//+kubebuilder:printcolumn:name="DP",type="string",JSONPath=".status.deletionPriority",description="The current deletionPriority of GameServer"
//+kubebuilder:printcolumn:name="UP",type="string",JSONPath=".status.updatePriority",description="The current updatePriority of GameServer"
//+kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp",description="The age of GameServer"
//+kubebuilder:resource:shortName=gs

// GameServer is the Schema for the gameservers API
type GameServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GameServerSpec   `json:"spec,omitempty"`
	Status GameServerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GameServerList contains a list of GameServer
type GameServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GameServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GameServer{}, &GameServerList{})
}
