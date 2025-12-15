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
	// LargeMessageStorePausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	LargeMessageStorePausedAnnotation = "largemessagestore.enterprise.splunk.com/paused"
)

// +kubebuilder:validation:XValidation:rule="self.provider != 's3' || has(self.s3)",message="s3 must be provided when provider is s3"
// LargeMessageStoreSpec defines the desired state of LargeMessageStore
type LargeMessageStoreSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=s3
	// Provider of queue resources
	Provider string `json:"provider"`

	// +kubebuilder:validation:Required
	// s3 specific inputs
	S3 S3Spec `json:"s3"`
}

type S3Spec struct {
	// +optional
	// +kubebuilder:validation:Pattern=`^https://s3(?:-fips)?\.[a-z]+-[a-z]+(?:-[a-z]+)?-\d+\.amazonaws\.com(?:\.cn)?(?:/[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*)?$`
	// S3-compatible Service endpoint
	Endpoint string `json:"endpoint"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^s3://[a-z0-9.-]{3,63}(?:/[^\s]+)?$`
	// S3 bucket path
	Path string `json:"path"`
}

// LargeMessageStoreStatus defines the observed state of LargeMessageStore.
type LargeMessageStoreStatus struct {
	// Phase of the large message store
	Phase Phase `json:"phase"`

	// Resource revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// Auxillary message describing CR status
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// LargeMessageStore is the Schema for a Splunk Enterprise large message store
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=largemessagestores,scope=Namespaced,shortName=lms
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of large message store"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of large message store resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
// +kubebuilder:storageversion

// LargeMessageStore is the Schema for the largemessagestores API
type LargeMessageStore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   LargeMessageStoreSpec   `json:"spec"`
	Status LargeMessageStoreStatus `json:"status,omitempty,omitzero"`
}

// DeepCopyObject implements runtime.Object
func (in *LargeMessageStore) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// +kubebuilder:object:root=true

// LargeMessageStoreList contains a list of LargeMessageStore
type LargeMessageStoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LargeMessageStore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LargeMessageStore{}, &LargeMessageStoreList{})
}

// NewEvent creates a new event associated with the object and ready
// to be published to Kubernetes API
func (bc *LargeMessageStore) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    bc.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "LargeMessageStore",
			Namespace:  bc.Namespace,
			Name:       bc.Name,
			UID:        bc.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-large-message-store-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/large-message-store-controller",
	}
}
