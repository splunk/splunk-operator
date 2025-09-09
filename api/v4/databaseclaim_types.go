/*
Copyright 2021.

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

package v4

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DatabaseClaimSpec struct {
	ClassName            string               `json:"className"`
	Template             *DatabaseClusterSpec `json:"template,omitempty"`   // optional starting point
	Parameters           map[string]string    `json:"parameters,omitempty"` // allowed keys per ClassPolicy
	ConnectionSecretName string               `json:"connectionSecretName,omitempty"`
}

type DatabaseClaimStatus struct {
	Phase      string                       `json:"phase,omitempty"`
	BoundRef   *corev1.LocalObjectReference `json:"boundRef,omitempty"`
	Conditions []metav1.Condition           `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=dbclaim
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=.spec.className
// +kubebuilder:printcolumn:name="Bound",type=string,JSONPath=.status.boundRef.name
type DatabaseClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DatabaseClaimSpec   `json:"spec,omitempty"`
	Status            DatabaseClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseClaimList contains a list of DatabaseClaim.
type DatabaseClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseClaim{}, &DatabaseClaimList{})
}
