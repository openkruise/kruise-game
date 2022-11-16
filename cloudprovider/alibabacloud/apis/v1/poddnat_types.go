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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&PodDNAT{}, &PodDNATList{})
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodDNAT let you specficy DNAT rule for pod on nat gateway
type PodDNAT struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired state of the PodDNAT.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	Spec PodDNATSpec `json:"spec,omitempty"`

	// 'Status is the current state of the dnat.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#spec-and-status
	// +optional
	Status PodDNATStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodDNATList is a collection of PodDNAT.
type PodDNATList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is the list of PodDNAT.
	Items []PodDNAT `json:"items"`
}

// PodDNATSpec describes the PodDNAT the user wishes to exist.
type PodDNATSpec struct {
	VSwitch      *string       `json:"vswitch,omitempty"` // deprecated
	ENI          *string       `json:"eni,omitempty"`     // deprecated
	ZoneID       *string       `json:"zoneID,omitempty"`
	ExternalIP   *string       `json:"externalIP,omitempty"`
	ExternalPort *string       `json:"externalPort,omitempty"` // deprecated
	InternalIP   *string       `json:"internalIP,omitempty"`   // pod IP may change
	InternalPort *string       `json:"internalPort,omitempty"` // deprecated
	Protocol     *string       `json:"protocol,omitempty"`
	TableId      *string       `json:"tableId,omitempty"` // natGateway ID
	EntryId      *string       `json:"entryId,omitempty"` // deprecated
	PortMapping  []PortMapping `json:"portMapping,omitempty"`
}

type PortMapping struct {
	ExternalPort string `json:"externalPort,omitempty"`
	InternalPort string `json:"internalPort,omitempty"`
}

// PodDNATStatus is the current state of the dnat.
type PodDNATStatus struct {
	// created create status
	// +optional
	Created *string `json:"created,omitempty"` // deprecated

	// entries
	// +optional
	Entries []Entry `json:"entries,omitempty"`
}

// Entry record for forwardEntry
type Entry struct {
	ExternalPort string `json:"externalPort,omitempty"`
	ExternalIP   string `json:"externalIP,omitempty"`
	InternalPort string `json:"internalPort,omitempty"`
	InternalIP   string `json:"internalIP,omitempty"`

	ForwardEntryID string `json:"forwardEntryId,omitempty"`
	IPProtocol     string `json:"ipProtocol,omitempty"`
}
