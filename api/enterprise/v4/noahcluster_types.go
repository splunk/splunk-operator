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
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// NoahClusterPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	NoahClusterPausedAnnotation = "noahcluster.enterprise.splunk.com/paused"
)

// NoahClusterSpec defines the desired state of NoahCluster
type NoahClusterSpec struct {
	// +kubebuilder:validation:Required
	// Reference to the Kubernetes Secret that contains Noah authentication credentials.
	AuthSecretRef corev1.LocalObjectReference `json:"authSecretRef"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?://[^\s/$.?#].[^\s]*$`
	// Endpoint is the URL of the Noah service.
	Endpoint string `json:"endpoint"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// Tenant is the Noah tenant identifier.
	Tenant string `json:"tenant"`

	// +optional
	// +kubebuilder:default=false
	// CacheWarmScaleOutEnabled enables cache-warm coordination when adding indexers.
	CacheWarmScaleOutEnabled bool `json:"cacheWarmScaleOutEnabled,omitempty"`

	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3600
	// CacheWarmScaleOutTimeoutSeconds is the timeout in seconds for cache-warm scale-out operations.
	CacheWarmScaleOutTimeoutSeconds int32 `json:"cacheWarmScaleOutTimeoutSeconds,omitempty"`
}

// NoahClusterStatus defines the observed state of NoahCluster
type NoahClusterStatus struct {
	// Phase of the NoahCluster
	Phase Phase `json:"phase"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// It corresponds to the metadata.generation which is updated on spec changes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the resource's state.
	// Conditions are: Ready, Progressing, Paused
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`

	// Auxiliary message describing CR status
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// NoahCluster is the Schema for a Noah cluster coordination resource
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=noahclusters,scope=Namespaced,shortName=noah
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of noah cluster"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.endpoint",description="Noah service endpoint"
// +kubebuilder:printcolumn:name="Tenant",type="string",JSONPath=".spec.tenant",description="Noah tenant identifier"
// +kubebuilder:printcolumn:name="CacheWarm",type="boolean",JSONPath=".spec.cacheWarmScaleOutEnabled",description="Cache-warm scale-out enabled"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of noah cluster resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxiliary message describing CR status"
// +kubebuilder:storageversion

// NoahCluster is the Schema for the noahclusters API.
type NoahCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   NoahClusterSpec   `json:"spec"`
	Status NoahClusterStatus `json:"status,omitempty,omitzero"`
}

// DeepCopyObject implements runtime.Object
func (in *NoahCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
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
