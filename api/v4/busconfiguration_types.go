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
	// BusConfigurationPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	BusConfigurationPausedAnnotation = "busconfiguration.enterprise.splunk.com/paused"
)

// BusConfigurationSpec defines the desired state of BusConfiguration
type BusConfigurationSpec struct {
	Type string `json:"type"`

	SQS SQSSpec `json:"sqs"`
}

type SQSSpec struct {
	QueueName string `json:"queueName"`

	AuthRegion string `json:"authRegion"`

	Endpoint string `json:"endpoint"`

	LargeMessageStoreEndpoint string `json:"largeMessageStoreEndpoint"`

	LargeMessageStorePath string `json:"largeMessageStorePath"`

	DeadLetterQueueName string `json:"deadLetterQueueName"`
}

// BusConfigurationStatus defines the observed state of BusConfiguration.
type BusConfigurationStatus struct {
	// Phase of the bus configuration
	Phase Phase `json:"phase"`

	// Resource revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// Auxillary message describing CR status
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// BusConfiguration is the Schema for a Splunk Enterprise bus configuration
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=busconfigurations,scope=Namespaced,shortName=bus
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of bus configuration"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of bus configuration resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
// +kubebuilder:storageversion

// BusConfiguration is the Schema for the busconfigurations API
type BusConfiguration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   BusConfigurationSpec   `json:"spec"`
	Status BusConfigurationStatus `json:"status,omitempty,omitzero"`
}

// DeepCopyObject implements runtime.Object
func (in *BusConfiguration) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// +kubebuilder:object:root=true

// BusConfigurationList contains a list of BusConfiguration
type BusConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BusConfiguration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BusConfiguration{}, &BusConfigurationList{})
}

// NewEvent creates a new event associated with the object and ready
// to be published to Kubernetes API
func (bc *BusConfiguration) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    bc.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "BusConfiguration",
			Namespace:  bc.Namespace,
			Name:       bc.Name,
			UID:        bc.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-busconfiguration-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/busconfiguration-controller",
	}
}
