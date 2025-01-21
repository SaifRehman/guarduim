/*
Copyright 2025.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GuarduimSpec defines the desired state of Guarduim
type GuarduimSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of Guarduim. Edit guarduim_types.go to remove/update
	Username  string `json:"username"` // The username for which we track authentication failures
	Threshold int    `json:"threshold"`
}

// GuarduimStatus defines the observed state of Guarduim
type GuarduimStatus struct {
	FailedAttempts int  `json:"failedAttempts"` // The current number of failed authentication attempts
	IsBlocked      bool `json:"isBlocked"`
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Guarduim is the Schema for the guarduims API
type Guarduim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuarduimSpec   `json:"spec,omitempty"`
	Status GuarduimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GuarduimList contains a list of Guarduim
type GuarduimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Guarduim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Guarduim{}, &GuarduimList{})
}
