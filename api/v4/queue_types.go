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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// QueuePausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	QueuePausedAnnotation = "queue.enterprise.splunk.com/paused"
)

// +kubebuilder:validation:XValidation:rule="self.provider == oldSelf.provider",message="provider is immutable once created"
// +kubebuilder:validation:XValidation:rule="self.sqs.name == oldSelf.sqs.name",message="sqs.name is immutable once created"
// +kubebuilder:validation:XValidation:rule="self.sqs.authRegion == oldSelf.sqs.authRegion",message="sqs.authRegion is immutable once created"
// +kubebuilder:validation:XValidation:rule="self.sqs.dlq == oldSelf.sqs.dlq",message="sqs.dlq is immutable once created"
// +kubebuilder:validation:XValidation:rule="self.sqs.endpoint == oldSelf.sqs.endpoint",message="sqs.endpoint is immutable once created"
// +kubebuilder:validation:XValidation:rule="(self.provider != 'sqs' && self.provider != 'sqs_cp') || has(self.sqs)",message="sqs must be provided when provider is sqs or sqs_cp"
// QueueSpec defines the desired state of Queue
type QueueSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=sqs;sqs_cp
	// Provider of queue resources
	Provider string `json:"provider"`

	// +kubebuilder:validation:Required
	// sqs specific inputs
	SQS SQSSpec `json:"sqs"`
}

type SQSSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// Name of the queue
	Name string `json:"name"`

	// +kubebuilder:validation:Pattern=`^(?:us|ap|eu|me|af|sa|ca|cn|il)(?:-[a-z]+){1,3}-\d$`
	// Auth Region of the resources
	AuthRegion string `json:"authRegion"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// Name of the dead letter queue resource
	DLQ string `json:"dlq"`

	// +optional
	// +kubebuilder:validation:Pattern=`^https?://[^\s/$.?#].[^\s]*$`
	// Amazon SQS Service endpoint
	Endpoint string `json:"endpoint,omitempty"`

	// +optional
	// List of remote storage volumes
	VolList []SQSVolumeSpec `json:"volumes,omitempty"`
}

// SQSVolumeSpec defines a volume reference for SQS queue authentication
type SQSVolumeSpec struct {
	// Remote volume name
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Remote volume secret ref
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	SecretRef string `json:"secretRef"`
}

// QueueStatus defines the observed state of Queue
type QueueStatus struct {
	// Phase of the queue
	Phase Phase `json:"phase"`

	// Resource revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// Auxillary message describing CR status
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Queue is the Schema for a Splunk Enterprise queue
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=queues,scope=Namespaced,shortName=queue
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of queue"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of queue resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
// +kubebuilder:storageversion

// Queue is the Schema for the queues API
type Queue struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   QueueSpec   `json:"spec"`
	Status QueueStatus `json:"status,omitempty,omitzero"`
}

// DeepCopyObject implements runtime.Object
func (in *Queue) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// +kubebuilder:object:root=true

// QueueList contains a list of Queue
type QueueList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Queue `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Queue{}, &QueueList{})
}
