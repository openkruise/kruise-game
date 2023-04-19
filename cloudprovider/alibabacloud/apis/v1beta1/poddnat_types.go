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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodDNATSpec defines the desired state of PodDNAT
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

// PodDNATStatus defines the observed state of PodDNAT
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

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PodDNAT is the Schema for the poddnats API
type PodDNAT struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodDNATSpec   `json:"spec,omitempty"`
	Status PodDNATStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PodDNATList contains a list of PodDNAT
type PodDNATList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodDNAT `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodDNAT{}, &PodDNATList{})
}
