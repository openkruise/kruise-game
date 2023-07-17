package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodEIPSpec defines the desired state of PodEIP
type PodEIPSpec struct {
	// +kubebuilder:validation:Required
	AllocationID string `json:"allocationID"`

	BandwidthPackageID string `json:"bandwidthPackageID,omitempty"`

	// +kubebuilder:validation:Required
	AllocationType AllocationType `json:"allocationType"`
}

// AllocationType ip type and release strategy
type AllocationType struct {
	// +kubebuilder:default:=Auto
	// +kubebuilder:validation:Required
	Type IPAllocType `json:"type"`
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Follow;TTL;Never
	ReleaseStrategy ReleaseStrategy `json:"releaseStrategy"`
	ReleaseAfter    string          `json:"releaseAfter,omitempty"` // go type 5m0s
}

// +kubebuilder:validation:Enum=Auto;Static

// IPAllocType is the type for eip alloc strategy
type IPAllocType string

// IPAllocType
const (
	IPAllocTypeAuto   IPAllocType = "Auto"
	IPAllocTypeStatic IPAllocType = "Static"
)

// +kubebuilder:validation:Enum=Follow;TTL;Never

// ReleaseStrategy is the type for eip release strategy
type ReleaseStrategy string

// ReleaseStrategy
const (
	ReleaseStrategyFollow ReleaseStrategy = "Follow" // default policy
	ReleaseStrategyTTL    ReleaseStrategy = "TTL"
	ReleaseStrategyNever  ReleaseStrategy = "Never"
)

// PodEIPStatus defines the observed state of PodEIP
type PodEIPStatus struct {
	// eni
	NetworkInterfaceID string `json:"networkInterfaceID,omitempty"`
	PrivateIPAddress   string `json:"privateIPAddress,omitempty"`
	// eip
	EipAddress            string `json:"eipAddress,omitempty"`
	ISP                   string `json:"isp,omitempty"`
	InternetChargeType    string `json:"internetChargeType,omitempty"`
	ResourceGroupID       string `json:"resourceGroupID,omitempty"`
	Name                  string `json:"name,omitempty"`
	PublicIpAddressPoolID string `json:"publicIpAddressPoolID,omitempty"`
	Status                string `json:"status,omitempty"`

	// BandwidthPackageID
	BandwidthPackageID string `json:"bandwidthPackageID,omitempty"`

	// PodLastSeen is the timestamp when pod resource last seen
	PodLastSeen metav1.Time `json:"podLastSeen,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PodEIP is the Schema for the podeips API
type PodEIP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodEIPSpec   `json:"spec,omitempty"`
	Status PodEIPStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PodEIPList contains a list of PodEIP
type PodEIPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodEIP `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodEIP{}, &PodEIPList{})
}
