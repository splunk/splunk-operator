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
	// BusPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	BusPausedAnnotation = "bus.enterprise.splunk.com/paused"
)

// +kubebuilder:validation:XValidation:rule="self.provider != 'sqs' || has(self.sqs)",message="sqs must be provided when provider is sqs"
// BusSpec defines the desired state of Bus
type BusSpec struct {
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

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^(?:us|ap|eu|me|af|sa|ca|cn|il)(?:-[a-z]+){1,3}-\d$`
	// Region of the resources
	Region string `json:"region"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// Name of the dead letter queue resource
	DLQ string `json:"dlq"`

	// +optional
	// +kubebuilder:validation:Pattern=`^https://sqs(?:-fips)?\.[a-z]+-[a-z]+(?:-[a-z]+)?-\d+\.amazonaws\.com(?:\.cn)?(?:/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*)?$`
	// Amazon SQS Service endpoint
	Endpoint string `json:"endpoint"`

	// +optional
	// List of remote storage volumes
	VolList []VolumeSpec `json:"volumes,omitempty"`
}

// BusStatus defines the observed state of Bus
type BusStatus struct {
	// Phase of the bus
	Phase Phase `json:"phase"`

	// Resource revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// Auxillary message describing CR status
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Bus is the Schema for a Splunk Enterprise bus
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=buses,scope=Namespaced,shortName=bus
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of bus"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of bus resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
// +kubebuilder:storageversion

// Bus is the Schema for the buses API
type Bus struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   BusSpec   `json:"spec"`
	Status BusStatus `json:"status,omitempty,omitzero"`
}

// DeepCopyObject implements runtime.Object
func (in *Bus) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// +kubebuilder:object:root=true

// BusList contains a list of Bus
type BusList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Bus `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Bus{}, &BusList{})
}

// NewEvent creates a new event associated with the object and ready
// to be published to Kubernetes API
func (bc *Bus) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    bc.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "Bus",
			Namespace:  bc.Namespace,
			Name:       bc.Name,
			UID:        bc.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-bus-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/bus-controller",
	}
}
