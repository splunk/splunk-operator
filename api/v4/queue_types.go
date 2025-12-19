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

package v4

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// QueuePausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	QueuePausedAnnotation = "queue.enterprise.splunk.com/paused"
)

// +kubebuilder:validation:XValidation:rule="self.provider != 'sqs' || has(self.sqs)",message="sqs must be provided when provider is sqs"
// QueueSpec defines the desired state of Queue
type QueueSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=sqs
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

	// +optional
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
	Endpoint string `json:"endpoint"`

	// +optional
	// List of remote storage volumes
	VolList []VolumeSpec `json:"volumes,omitempty"`
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
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
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

// NewEvent creates a new event associated with the object and ready
// to be published to Kubernetes API
func (os *Queue) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    os.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "Queue",
			Namespace:  os.Namespace,
			Name:       os.Name,
			UID:        os.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-queue-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/queue-controller",
	}
}
