package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GuarduimSpec defines the desired state of Guarduim
type GuarduimSpec struct {
	Username  string `json:"username"`
	Threshold int    `json:"threshold"`
}

// GuarduimStatus defines the observed state of Guarduim
type GuarduimStatus struct {
	FailureCount int  `json:"failureCount"`
	Blocked      bool `json:"blocked"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:path=guarduims,scope=Namespaced

// Guarduim is the Schema for the guarduims API
type Guarduim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              GuarduimSpec   `json:"spec,omitempty"`
	Status            GuarduimStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GuarduimList contains a list of Guarduim
type GuarduimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Guarduim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Guarduim{}, &GuarduimList{})
}
