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

// AutoNLBs-V3 plugin 仅与 NLB-Pool-Operator 的 PortAllocation CRD 交互。
// 这里只定义 plugin 真正读写的最小字段子集，避免与上游 CRD schema drift。
// 完整 schema 见 https://github.com/chrisliu1995/AlibabaCloud-NLB-Pool-Operator
const (
	// NLBPoolNameConfig is the GameServerSet network conf parameter name
	// pointing to an existing NLBPool CR in the same namespace.
	NLBPoolNameConfig = "NLBPoolName"

	// AnnotationNLBPoolName 由 kruise-game 写入 Pod，告知 PA Controller 该 Pod 想绑定的 pool。
	AnnotationNLBPoolName = "alibabacloud.com/nlb-pool-name"

	// AnnotationNetworkDisabled 由 kruise-game 写入 Pod 触发 PA Controller 执行网络隔离。
	AnnotationNetworkDisabled = "alibabacloud.com/nlb-network-disabled"

	// AnnotationPAClaim 由 PA Controller 写入 Pod，记录该 Pod 绑定到的 PortAllocation CR 名称。
	AnnotationPAClaim = "alibabacloud.com/nlb-pa-claim"

	// PortAllocation Phase 值（与 NLB-Pool-Operator status.phase 对齐）。
	PortAllocationPhaseBound = "Bound"
)

// NLBPoolGroupVersion 是 PortAllocation CRD 的 GroupVersion，需与 NLB-Pool-Operator 一致。
var NLBPoolGroupVersion = schema.GroupVersion{Group: "nlbpool.alibabacloud.com", Version: "v1alpha1"}

// PortAllocation 是 plugin 侧的最小本地定义，只声明 plugin 实际读写的字段。
type PortAllocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PortAllocationSpec   `json:"spec,omitempty"`
	Status PortAllocationStatus `json:"status,omitempty"`
}

// PortAllocationList is needed for runtime.Object scheme registration.
type PortAllocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PortAllocation `json:"items"`
}

// PortAllocationSpec — plugin 仅消费 Endpoints + BoundPod。
type PortAllocationSpec struct {
	BoundPod  string         `json:"boundPod,omitempty"`
	Endpoints []LaneEndpoint `json:"endpoints,omitempty"`
}

// LaneEndpoint 描述一条接入线路上的 EIP 与端口映射。
type LaneEndpoint struct {
	Lane  string         `json:"lane"`
	EIP   string         `json:"eip"`
	Ports []EndpointPort `json:"ports"`
}

// EndpointPort 描述某 lane 上一个端口的 listener 与对应 container port。
type EndpointPort struct {
	Name          string `json:"name"`
	ListenerPort  int32  `json:"listenerPort"`
	ContainerPort int32  `json:"containerPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
}

// PortAllocationStatus — plugin 仅读取 Phase 字段判断绑定状态。
type PortAllocationStatus struct {
	Phase string `json:"phase,omitempty"`
}

// ===== runtime.Object 接口实现 (DeepCopy) =====

func (in *PortAllocation) DeepCopyInto(out *PortAllocation) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
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

// ===== scheme 注册 =====

// NLBPoolSchemeBuilder 用于将 PortAllocation 类型注册到 runtime.Scheme。
var NLBPoolSchemeBuilder = runtime.NewSchemeBuilder(addNLBPoolKnownTypes)

// AddNLBPoolToScheme 将 PortAllocation 相关类型注册到给定的 scheme。
var AddNLBPoolToScheme = NLBPoolSchemeBuilder.AddToScheme

func addNLBPoolKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(NLBPoolGroupVersion,
		&PortAllocation{},
		&PortAllocationList{},
	)
	metav1.AddToGroupVersion(scheme, NLBPoolGroupVersion)
	return nil
}
