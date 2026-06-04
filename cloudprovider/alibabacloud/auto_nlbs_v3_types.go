/*
Copyright 2025 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package alibabacloud

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// AutoNLBs-V3 plugin / PortAllocation 相关常量
// 这些类型与 NLB-Pool-Operator 仓库中的 CRD 定义保持一致
// 在 kruise-game 中本地定义，仅用于 Plugin 侧读写
const (
	// 配置参数名（GameServerSet.spec.network.networkConf）
	NLBPoolNameConfigV4      = "NLBPoolName"
	NLBPoolNamespaceConfigV4 = "NLBPoolNamespace"
	PortProtocolsConfigV4    = "PortProtocols"

	// Pod Annotations — kruise-game 仅写入这些 annotation，
	// PA Controller（独立仓库）通过 watch Pod 来执行所有绑定/释放逻辑
	AnnotationNLBPoolName      = "alibabacloud.com/nlb-pool-name"
	AnnotationNLBPoolNamespace = "alibabacloud.com/nlb-pool-namespace"
	AnnotationNLBPortProtocols = "alibabacloud.com/nlb-port-protocols"
	AnnotationNetworkDisabled  = "alibabacloud.com/nlb-network-disabled"
	AnnotationPAClaim          = "alibabacloud.com/nlb-pa-claim"

	// PortAllocation Labels（由 PA Controller 维护，kruise-game 仅作只读 selector）
	LabelNLBPoolName = "nlbpool.alibabacloud.com/pool"

	// PortAllocation Phase 值
	PortAllocationPhaseAvailable = "Available"
	PortAllocationPhaseBinding   = "Binding"
	PortAllocationPhaseBound     = "Bound"
	PortAllocationPhaseReleasing = "Releasing"
	PortAllocationPhaseDisabled  = "Disabled"
	PortAllocationPhaseFailed    = "Failed"
)

// NLBPoolGroupVersion 定义 PortAllocation CRD 的 GroupVersion，需与 Operator 仓库保持一致
var NLBPoolGroupVersion = schema.GroupVersion{Group: "nlbpool.alibabacloud.com", Version: "v1alpha1"}

// PortAllocation 是 PortAllocation CRD 的 Plugin 侧定义
type PortAllocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PortAllocationSpec   `json:"spec,omitempty"`
	Status PortAllocationStatus `json:"status,omitempty"`
}

// PortAllocationList 是 PortAllocation 的列表类型
type PortAllocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PortAllocation `json:"items"`
}

// PortAllocationSpec 定义端口分配槽位的期望状态
type PortAllocationSpec struct {
	ServerGroups []ServerGroupInfo `json:"serverGroups"`
	Endpoints    []LaneEndpoint    `json:"endpoints"`
	BoundPod     string            `json:"boundPod,omitempty"`
	BoundPodIP   string            `json:"boundPodIP,omitempty"`
}

// ServerGroupInfo 描述一个预创建的 ServerGroup
type ServerGroupInfo struct {
	LogicalPort     int32  `json:"logicalPort"`
	ServerGroupId   string `json:"serverGroupId"`
	ServerGroupName string `json:"serverGroupName"`
}

// LaneEndpoint 每条 lane 的接入端点（对应 PA spec.endpoints）
type LaneEndpoint struct {
	Lane  string         `json:"lane"`
	EIP   string         `json:"eip"`
	Ports []EndpointPort `json:"ports"`
}

// EndpointPort 每条 lane 上某端口的信息
type EndpointPort struct {
	Name          string `json:"name"`
	ListenerPort  int32  `json:"listenerPort"`
	ContainerPort int32  `json:"containerPort,omitempty"`
	Protocol      string `json:"protocol"`
	ListenerId    string `json:"listenerId,omitempty"`
}

// NLBEndpoint （保留向后兼容，不再使用）
type NLBEndpoint struct {
	ISPType   string         `json:"ispType"`
	NLBId     string         `json:"nlbId"`
	EIP       string         `json:"eip"`
	Listeners []ListenerInfo `json:"listeners"`
}

// ListenerInfo 描述一个预创建的 Listener
type ListenerInfo struct {
	ListenerPort   int32  `json:"listenerPort"`
	Protocol       string `json:"protocol"`
	ListenerId     string `json:"listenerId"`
	ServerGroupRef string `json:"serverGroupRef"`
}

// PortAllocationStatus 定义端口分配槽位的观测状态
type PortAllocationStatus struct {
	Phase             string            `json:"phase,omitempty"`
	ExternalAddresses []ExternalAddress `json:"externalAddresses,omitempty"`
	OperationJobId    string            `json:"operationJobId,omitempty"`
	Message           string            `json:"message,omitempty"`
}

// ExternalAddress 对外暴露的地址信息
type ExternalAddress struct {
	Lane  string     `json:"lane"`
	IP    string     `json:"ip"`
	Ports []PortInfo `json:"ports"`
}

// PortInfo 端口信息
type PortInfo struct {
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// ===== runtime.Object 接口实现 (DeepCopy) =====

func (in *PortAllocation) DeepCopyInto(out *PortAllocation) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *PortAllocation) DeepCopy() *PortAllocation {
	if in == nil {
		return nil
	}
	out := new(PortAllocation)
	in.DeepCopyInto(out)
	return out
}

func (in *PortAllocation) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *PortAllocationList) DeepCopyInto(out *PortAllocationList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]PortAllocation, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *PortAllocationList) DeepCopy() *PortAllocationList {
	if in == nil {
		return nil
	}
	out := new(PortAllocationList)
	in.DeepCopyInto(out)
	return out
}

func (in *PortAllocationList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *PortAllocationSpec) DeepCopyInto(out *PortAllocationSpec) {
	*out = *in
	if in.ServerGroups != nil {
		out.ServerGroups = make([]ServerGroupInfo, len(in.ServerGroups))
		copy(out.ServerGroups, in.ServerGroups)
	}
	if in.Endpoints != nil {
		out.Endpoints = make([]LaneEndpoint, len(in.Endpoints))
		for i := range in.Endpoints {
			in.Endpoints[i].DeepCopyInto(&out.Endpoints[i])
		}
	}
}

func (in *LaneEndpoint) DeepCopyInto(out *LaneEndpoint) {
	*out = *in
	if in.Ports != nil {
		out.Ports = make([]EndpointPort, len(in.Ports))
		copy(out.Ports, in.Ports)
	}
}

func (in *PortAllocationStatus) DeepCopyInto(out *PortAllocationStatus) {
	*out = *in
	if in.ExternalAddresses != nil {
		out.ExternalAddresses = make([]ExternalAddress, len(in.ExternalAddresses))
		for i := range in.ExternalAddresses {
			in.ExternalAddresses[i].DeepCopyInto(&out.ExternalAddresses[i])
		}
	}
}

func (in *ExternalAddress) DeepCopyInto(out *ExternalAddress) {
	*out = *in
	if in.Ports != nil {
		out.Ports = make([]PortInfo, len(in.Ports))
		copy(out.Ports, in.Ports)
	}
}

// ===== scheme 注册 =====

// NLBPoolSchemeBuilder 用于将 PortAllocation 类型注册到 runtime.Scheme
var NLBPoolSchemeBuilder = runtime.NewSchemeBuilder(addNLBPoolKnownTypes)

// AddNLBPoolToScheme 将 NLBPool 相关类型注册到给定的 scheme
var AddNLBPoolToScheme = NLBPoolSchemeBuilder.AddToScheme

func addNLBPoolKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(NLBPoolGroupVersion,
		&NLBPool{},
		&NLBPoolList{},
		&PortAllocation{},
		&PortAllocationList{},
	)
	metav1.AddToGroupVersion(scheme, NLBPoolGroupVersion)
	return nil
}

// ===== NLBPool CRD 类型（只读，用于从 NLBPool 自动获取端口配置） =====

// NLBPool 是 NLBPool CRD 的 Plugin 侧只读定义
type NLBPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec NLBPoolSpec `json:"spec,omitempty"`
}

// NLBPoolSpec NLBPool 的 spec（仅保留 plugin 需要的字段）
type NLBPoolSpec struct {
	Ports []NLBPoolPortConfig `json:"ports,omitempty"`
}

// NLBPoolPortConfig NLBPool 中每个端口的配置
type NLBPoolPortConfig struct {
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
	ContainerPort int32  `json:"containerPort,omitempty"`
}

func (in *NLBPool) DeepCopyInto(out *NLBPool) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

func (in *NLBPool) DeepCopy() *NLBPool {
	if in == nil {
		return nil
	}
	out := new(NLBPool)
	in.DeepCopyInto(out)
	return out
}

func (in *NLBPool) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *NLBPoolSpec) DeepCopyInto(out *NLBPoolSpec) {
	*out = *in
	if in.Ports != nil {
		out.Ports = make([]NLBPoolPortConfig, len(in.Ports))
		copy(out.Ports, in.Ports)
	}
}

// NLBPoolList is a list of NLBPool resources
type NLBPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NLBPool `json:"items"`
}

func (in *NLBPoolList) DeepCopyInto(out *NLBPoolList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]NLBPool, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *NLBPoolList) DeepCopy() *NLBPoolList {
	if in == nil {
		return nil
	}
	out := new(NLBPoolList)
	in.DeepCopyInto(out)
	return out
}

func (in *NLBPoolList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
