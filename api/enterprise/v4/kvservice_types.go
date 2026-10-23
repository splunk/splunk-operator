// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	// KVServicePausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	KVServicePausedAnnotation = "kvservice.enterprise.splunk.com/paused"
)

// KVServiceSpec defines the desired state of KVService.
type KVServiceSpec struct {
	// Common workload spec
	CommonWorkloadSpec `json:",inline"`

	// LivenessInitialDelaySeconds overrides the default 30-second liveness delay
	// when livenessProbe.initialDelaySeconds is not specified.
	// +optional
	// +kubebuilder:validation:Minimum=0
	LivenessInitialDelaySeconds int32 `json:"livenessInitialDelaySeconds,omitempty"`

	// ReadinessInitialDelaySeconds overrides the default 30-second readiness delay
	// when readinessProbe.initialDelaySeconds is not specified.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReadinessInitialDelaySeconds int32 `json:"readinessInitialDelaySeconds,omitempty"`

	// LivenessProbe overrides HTTP probe timings. Defaults are a 30-second delay,
	// a 1-second timeout, a 5-second period, and a failure threshold of 3.
	// +optional
	LivenessProbe *Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe overrides HTTP probe timings. Defaults are a 30-second delay,
	// a 1-second timeout, a 5-second period, and a failure threshold of 3.
	// +optional
	ReadinessProbe *Probe `json:"readinessProbe,omitempty"`

	// Replicas is the desired number of KVService pods. Defaults to 1 when omitted.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
}

// KVServiceStatus defines the observed state of KVService
type KVServiceStatus struct {
	// current phase of the kvservice instances
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

	// Replicas is the observed number of KVService pods,
	// exposed through the scale subresource.
	Replicas int32 `json:"replicas"`

	// current number of ready kvservice instances
	ReadyReplicas int32 `json:"readyReplicas"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Resource Revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KVService is the Schema for the kvservices API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=kvservices,scope=Namespaced,shortName=kvs
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of kvservice pods"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Number of desired kvservice pods"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready kvservice pods"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of kvservice resource"
// +kubebuilder:storageversion
type KVService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   KVServiceSpec   `json:"spec"`
	Status KVServiceStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// KVServiceList contains a list of KVService
type KVServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KVService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KVService{}, &KVServiceList{})
}

// GetCerts returns the certificate declarations used by the cert Secret watch.
func (cr *KVService) GetCerts() []CertSpec {
	return cr.Spec.Certs
}
