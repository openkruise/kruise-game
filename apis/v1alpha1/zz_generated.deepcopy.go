//go:build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"github.com/openkruise/kruise-api/apps/pub"
	"k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServer) DeepCopyInto(out *GameServer) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServer.
func (in *GameServer) DeepCopy() *GameServer {
	if in == nil {
		return nil
	}
	out := new(GameServer)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GameServer) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerCondition) DeepCopyInto(out *GameServerCondition) {
	*out = *in
	in.LastProbeTime.DeepCopyInto(&out.LastProbeTime)
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerCondition.
func (in *GameServerCondition) DeepCopy() *GameServerCondition {
	if in == nil {
		return nil
	}
	out := new(GameServerCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerContainer) DeepCopyInto(out *GameServerContainer) {
	*out = *in
	in.Resources.DeepCopyInto(&out.Resources)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerContainer.
func (in *GameServerContainer) DeepCopy() *GameServerContainer {
	if in == nil {
		return nil
	}
	out := new(GameServerContainer)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerList) DeepCopyInto(out *GameServerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]GameServer, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerList.
func (in *GameServerList) DeepCopy() *GameServerList {
	if in == nil {
		return nil
	}
	out := new(GameServerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GameServerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerSet) DeepCopyInto(out *GameServerSet) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerSet.
func (in *GameServerSet) DeepCopy() *GameServerSet {
	if in == nil {
		return nil
	}
	out := new(GameServerSet)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GameServerSet) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerSetList) DeepCopyInto(out *GameServerSetList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]GameServerSet, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerSetList.
func (in *GameServerSetList) DeepCopy() *GameServerSetList {
	if in == nil {
		return nil
	}
	out := new(GameServerSetList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *GameServerSetList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerSetSpec) DeepCopyInto(out *GameServerSetSpec) {
	*out = *in
	if in.Replicas != nil {
		in, out := &in.Replicas, &out.Replicas
		*out = new(int32)
		**out = **in
	}
	in.GameServerTemplate.DeepCopyInto(&out.GameServerTemplate)
	if in.ReserveGameServerIds != nil {
		in, out := &in.ReserveGameServerIds, &out.ReserveGameServerIds
		*out = make([]int, len(*in))
		copy(*out, *in)
	}
	if in.ServiceQualities != nil {
		in, out := &in.ServiceQualities, &out.ServiceQualities
		*out = make([]ServiceQuality, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.UpdateStrategy.DeepCopyInto(&out.UpdateStrategy)
	in.ScaleStrategy.DeepCopyInto(&out.ScaleStrategy)
	if in.Network != nil {
		in, out := &in.Network, &out.Network
		*out = new(Network)
		(*in).DeepCopyInto(*out)
	}
	if in.Lifecycle != nil {
		in, out := &in.Lifecycle, &out.Lifecycle
		*out = new(pub.Lifecycle)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerSetSpec.
func (in *GameServerSetSpec) DeepCopy() *GameServerSetSpec {
	if in == nil {
		return nil
	}
	out := new(GameServerSetSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerSetStatus) DeepCopyInto(out *GameServerSetStatus) {
	*out = *in
	if in.MaintainingReplicas != nil {
		in, out := &in.MaintainingReplicas, &out.MaintainingReplicas
		*out = new(int32)
		**out = **in
	}
	if in.WaitToBeDeletedReplicas != nil {
		in, out := &in.WaitToBeDeletedReplicas, &out.WaitToBeDeletedReplicas
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerSetStatus.
func (in *GameServerSetStatus) DeepCopy() *GameServerSetStatus {
	if in == nil {
		return nil
	}
	out := new(GameServerSetStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerSpec) DeepCopyInto(out *GameServerSpec) {
	*out = *in
	if in.UpdatePriority != nil {
		in, out := &in.UpdatePriority, &out.UpdatePriority
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.DeletionPriority != nil {
		in, out := &in.DeletionPriority, &out.DeletionPriority
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.Containers != nil {
		in, out := &in.Containers, &out.Containers
		*out = make([]GameServerContainer, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerSpec.
func (in *GameServerSpec) DeepCopy() *GameServerSpec {
	if in == nil {
		return nil
	}
	out := new(GameServerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerStatus) DeepCopyInto(out *GameServerStatus) {
	*out = *in
	in.NetworkStatus.DeepCopyInto(&out.NetworkStatus)
	in.PodStatus.DeepCopyInto(&out.PodStatus)
	if in.ServiceQualitiesCondition != nil {
		in, out := &in.ServiceQualitiesCondition, &out.ServiceQualitiesCondition
		*out = make([]ServiceQualityCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.UpdatePriority != nil {
		in, out := &in.UpdatePriority, &out.UpdatePriority
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.DeletionPriority != nil {
		in, out := &in.DeletionPriority, &out.DeletionPriority
		*out = new(intstr.IntOrString)
		**out = **in
	}
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]GameServerCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerStatus.
func (in *GameServerStatus) DeepCopy() *GameServerStatus {
	if in == nil {
		return nil
	}
	out := new(GameServerStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GameServerTemplate) DeepCopyInto(out *GameServerTemplate) {
	*out = *in
	in.PodTemplateSpec.DeepCopyInto(&out.PodTemplateSpec)
	if in.VolumeClaimTemplates != nil {
		in, out := &in.VolumeClaimTemplates, &out.VolumeClaimTemplates
		*out = make([]v1.PersistentVolumeClaim, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GameServerTemplate.
func (in *GameServerTemplate) DeepCopy() *GameServerTemplate {
	if in == nil {
		return nil
	}
	out := new(GameServerTemplate)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *KVParams) DeepCopyInto(out *KVParams) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new KVParams.
func (in *KVParams) DeepCopy() *KVParams {
	if in == nil {
		return nil
	}
	out := new(KVParams)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Network) DeepCopyInto(out *Network) {
	*out = *in
	if in.NetworkConf != nil {
		in, out := &in.NetworkConf, &out.NetworkConf
		*out = make([]NetworkConfParams, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Network.
func (in *Network) DeepCopy() *Network {
	if in == nil {
		return nil
	}
	out := new(Network)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetworkAddress) DeepCopyInto(out *NetworkAddress) {
	*out = *in
	if in.Ports != nil {
		in, out := &in.Ports, &out.Ports
		*out = make([]NetworkPort, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.PortRange != nil {
		in, out := &in.PortRange, &out.PortRange
		*out = new(NetworkPortRange)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetworkAddress.
func (in *NetworkAddress) DeepCopy() *NetworkAddress {
	if in == nil {
		return nil
	}
	out := new(NetworkAddress)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetworkConfParams) DeepCopyInto(out *NetworkConfParams) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetworkConfParams.
func (in *NetworkConfParams) DeepCopy() *NetworkConfParams {
	if in == nil {
		return nil
	}
	out := new(NetworkConfParams)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetworkPort) DeepCopyInto(out *NetworkPort) {
	*out = *in
	if in.Port != nil {
		in, out := &in.Port, &out.Port
		*out = new(intstr.IntOrString)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetworkPort.
func (in *NetworkPort) DeepCopy() *NetworkPort {
	if in == nil {
		return nil
	}
	out := new(NetworkPort)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetworkPortRange) DeepCopyInto(out *NetworkPortRange) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetworkPortRange.
func (in *NetworkPortRange) DeepCopy() *NetworkPortRange {
	if in == nil {
		return nil
	}
	out := new(NetworkPortRange)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetworkStatus) DeepCopyInto(out *NetworkStatus) {
	*out = *in
	if in.InternalAddresses != nil {
		in, out := &in.InternalAddresses, &out.InternalAddresses
		*out = make([]NetworkAddress, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ExternalAddresses != nil {
		in, out := &in.ExternalAddresses, &out.ExternalAddresses
		*out = make([]NetworkAddress, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.CreateTime.DeepCopyInto(&out.CreateTime)
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetworkStatus.
func (in *NetworkStatus) DeepCopy() *NetworkStatus {
	if in == nil {
		return nil
	}
	out := new(NetworkStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RollingUpdateStatefulSetStrategy) DeepCopyInto(out *RollingUpdateStatefulSetStrategy) {
	*out = *in
	if in.Partition != nil {
		in, out := &in.Partition, &out.Partition
		*out = new(int32)
		**out = **in
	}
	if in.MaxUnavailable != nil {
		in, out := &in.MaxUnavailable, &out.MaxUnavailable
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.InPlaceUpdateStrategy != nil {
		in, out := &in.InPlaceUpdateStrategy, &out.InPlaceUpdateStrategy
		*out = new(pub.InPlaceUpdateStrategy)
		**out = **in
	}
	if in.MinReadySeconds != nil {
		in, out := &in.MinReadySeconds, &out.MinReadySeconds
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RollingUpdateStatefulSetStrategy.
func (in *RollingUpdateStatefulSetStrategy) DeepCopy() *RollingUpdateStatefulSetStrategy {
	if in == nil {
		return nil
	}
	out := new(RollingUpdateStatefulSetStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ScaleStrategy) DeepCopyInto(out *ScaleStrategy) {
	*out = *in
	in.StatefulSetScaleStrategy.DeepCopyInto(&out.StatefulSetScaleStrategy)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ScaleStrategy.
func (in *ScaleStrategy) DeepCopy() *ScaleStrategy {
	if in == nil {
		return nil
	}
	out := new(ScaleStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceQuality) DeepCopyInto(out *ServiceQuality) {
	*out = *in
	in.Probe.DeepCopyInto(&out.Probe)
	if in.ServiceQualityAction != nil {
		in, out := &in.ServiceQualityAction, &out.ServiceQualityAction
		*out = make([]ServiceQualityAction, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceQuality.
func (in *ServiceQuality) DeepCopy() *ServiceQuality {
	if in == nil {
		return nil
	}
	out := new(ServiceQuality)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceQualityAction) DeepCopyInto(out *ServiceQualityAction) {
	*out = *in
	in.GameServerSpec.DeepCopyInto(&out.GameServerSpec)
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceQualityAction.
func (in *ServiceQualityAction) DeepCopy() *ServiceQualityAction {
	if in == nil {
		return nil
	}
	out := new(ServiceQualityAction)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceQualityCondition) DeepCopyInto(out *ServiceQualityCondition) {
	*out = *in
	in.LastProbeTime.DeepCopyInto(&out.LastProbeTime)
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
	in.LastActionTransitionTime.DeepCopyInto(&out.LastActionTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceQualityCondition.
func (in *ServiceQualityCondition) DeepCopy() *ServiceQualityCondition {
	if in == nil {
		return nil
	}
	out := new(ServiceQualityCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UpdateStrategy) DeepCopyInto(out *UpdateStrategy) {
	*out = *in
	if in.RollingUpdate != nil {
		in, out := &in.RollingUpdate, &out.RollingUpdate
		*out = new(RollingUpdateStatefulSetStrategy)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UpdateStrategy.
func (in *UpdateStrategy) DeepCopy() *UpdateStrategy {
	if in == nil {
		return nil
	}
	out := new(UpdateStrategy)
	in.DeepCopyInto(out)
	return out
}
