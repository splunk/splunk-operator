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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TenantConfigSpec defines namespace-specific configuration overrides for the Platform SDK.
//
// Tenant admins create TenantConfigs to override PlatformConfig defaults for their namespace.
// This enables multi-tenancy with different settings per tenant.
type TenantConfigSpec struct {
	// Secrets configuration overrides.
	// Overrides the cluster-wide PlatformConfig secret settings for this namespace.
	// +optional
	Secrets SecretConfig `json:"secrets,omitempty"`

	// Certificates configuration overrides.
	// Overrides the cluster-wide PlatformConfig certificate settings for this namespace.
	// +optional
	Certificates CertificatesConfig `json:"certificates,omitempty"`
}

// TenantConfigStatus defines the observed state of TenantConfig.
type TenantConfigStatus struct {
	// Conditions represent the latest available observations of the config's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=tc
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Observed Generation",type="integer",JSONPath=".status.observedGeneration"

// TenantConfig is the Schema for the tenantconfigs API.
//
// TenantConfig defines namespace-specific overrides for the Platform SDK.
// This enables multi-tenancy by allowing different settings per namespace.
type TenantConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TenantConfigSpec   `json:"spec,omitempty"`
	Status TenantConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TenantConfigList contains a list of TenantConfig.
type TenantConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TenantConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TenantConfig{}, &TenantConfigList{})
}
