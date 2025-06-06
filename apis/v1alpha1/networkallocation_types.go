package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NetworkAllocationSpec defines desired port allocation
type NetworkAllocationSpec struct {
	// LbID indicates load balancer ID
	LbID string `json:"lbId"`
	// Port allocated for this pod
	Port int32 `json:"port"`
	// Protocol is network protocol
	Protocol string `json:"protocol,omitempty"`
	// PodRef references the owning Pod
	PodRef corev1.ObjectReference `json:"podRef"`
}

// NetworkAllocationStatus defines the observed state
type NetworkAllocationStatus struct {
	// State indicates current allocation state
	State string `json:"state,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NetworkAllocation is the Schema for network allocations
type NetworkAllocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkAllocationSpec   `json:"spec,omitempty"`
	Status NetworkAllocationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NetworkAllocationList contains a list of NetworkAllocation
type NetworkAllocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkAllocation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetworkAllocation{}, &NetworkAllocationList{})
}
