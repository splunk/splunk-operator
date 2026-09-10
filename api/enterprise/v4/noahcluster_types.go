// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v4

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:XValidation:rule="self.endpoint == oldSelf.endpoint",message="endpoint is immutable once created"
// +kubebuilder:validation:XValidation:rule="self.tenant == oldSelf.tenant",message="tenant is immutable once created"

// NoahClusterSpec defines the desired state of NoahCluster
type NoahClusterSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != ''",message="authSecretRef.name must not be empty"
	// +kubebuilder:validation:XValidation:rule="!has(self.name) || self.name == '' || self.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?([.][a-z0-9]([-a-z0-9]*[a-z0-9])?)*$')",message="authSecretRef.name must be a valid DNS-1123 subdomain"
	// AuthSecretRef names the Secret holding the Noah pass4SymmKey under the
	// "pass4SymmKey" key. It is a local reference, so the Secret must be in the
	// same namespace as this NoahCluster.
	AuthSecretRef corev1.LocalObjectReference `json:"authSecretRef"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="isURL(self)",message="endpoint must be a valid URL"
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (url(self).getScheme() == 'http' || url(self).getScheme() == 'https')",message="endpoint scheme must be http or https"
	// +kubebuilder:validation:XValidation:rule="isURL(self) && url(self).getHostname() != ''",message="endpoint must include a host"
	// +kubebuilder:validation:XValidation:rule="!self.contains('@')",message="endpoint must not contain credentials"
	// +kubebuilder:validation:XValidation:rule="isURL(self) && (url(self).getEscapedPath() == '' || url(self).getEscapedPath() == '/')",message="endpoint must not contain a path"
	// +kubebuilder:validation:XValidation:rule="!self.contains('?')",message="endpoint must not contain a query"
	// +kubebuilder:validation:XValidation:rule="!self.contains('#')",message="endpoint must not contain a fragment"
	// Endpoint is the base URL of the Noah service. It must be an absolute http or https URL naming a host,
	// with no credentials, path, query, or fragment. A single trailing "/" is accepted and normalised away.
	Endpoint string `json:"endpoint"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self.trim() == self",message="tenant must not have leading or trailing whitespace"
	// Tenant scopes peer membership and bucket maps to one logical Splunk
	// deployment, preventing workloads from registering in or reading another
	// tenant. There is no default: every installation chooses an explicit,
	// stable value agreed with Noah.
	Tenant string `json:"tenant"`

	// +optional
	// +kubebuilder:default=true
	// CacheWarmScaleOutEnabled controls whether scale-out waits for each new peer to become up
	// before adding the next ordinal. When false, scale-out advances after peer registration.
	// Final readiness always requires every peer to be up.
	CacheWarmScaleOutEnabled *bool `json:"cacheWarmScaleOutEnabled,omitempty"`

	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3600
	// CacheWarmScaleOutTimeoutSeconds is the maximum time to wait for a peer to become up when
	// cache-warm coordination is enabled. Zero disables the timeout.
	CacheWarmScaleOutTimeoutSeconds *int32 `json:"cacheWarmScaleOutTimeoutSeconds,omitempty"`
}

// NoahClusterStatus is intentionally empty. NoahCluster is configuration-only;
// operational state is reported by each referencing workload.
type NoahClusterStatus struct{}

// +kubebuilder:object:root=true

// NoahCluster is shared, configuration-only connection detail for Splunk
// workloads that use Noah. Because several resources (e.g. IndexerClusters and SearchHeadClusters)
// can reference the same NoahCluster with differing health, no single controller can maintain a status
// consistently for all of them. Operational state is reported on each referencing workload instead.
// +k8s:openapi-gen=true
// +kubebuilder:resource:path=noahclusters,scope=Namespaced,shortName=noah
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.endpoint",description="Noah service endpoint"
// +kubebuilder:printcolumn:name="Tenant",type="string",JSONPath=".spec.tenant",description="Noah tenant identifier"
// +kubebuilder:printcolumn:name="CacheWarm",type="boolean",JSONPath=".spec.cacheWarmScaleOutEnabled",description="Cache-warm scale-out enabled"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of noah cluster resource"
// +kubebuilder:storageversion
type NoahCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   NoahClusterSpec   `json:"spec"`
	Status NoahClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// NoahClusterList contains a list of NoahCluster
type NoahClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NoahCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NoahCluster{}, &NoahClusterList{})
}
