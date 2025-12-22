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
	// ObjectStoragePausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	ObjectStoragePausedAnnotation = "objectstorage.enterprise.splunk.com/paused"
)

// +kubebuilder:validation:XValidation:rule="self.provider != 's3' || has(self.s3)",message="s3 must be provided when provider is s3"
// ObjectStorageSpec defines the desired state of ObjectStorage
type ObjectStorageSpec struct {
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
	// +kubebuilder:validation:Pattern=`^https?://[^\s/$.?#].[^\s]*$`
	// S3-compatible Service endpoint
	Endpoint string `json:"endpoint"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^s3://[a-z0-9.-]{3,63}(?:/[^\s]+)?$`
	// S3 bucket path
	Path string `json:"path"`
}

// ObjectStorageStatus defines the observed state of ObjectStorage.
type ObjectStorageStatus struct {
	// Phase of the object storage
	Phase Phase `json:"phase"`

	// Resource revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// Auxillary message describing CR status
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ObjectStorage is the Schema for a Splunk Enterprise object storage
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=objectstorages,scope=Namespaced,shortName=os
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of object storage"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of object storage resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
// +kubebuilder:storageversion

// ObjectStorage is the Schema for the objectstorages API
type ObjectStorage struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   ObjectStorageSpec   `json:"spec"`
	Status ObjectStorageStatus `json:"status,omitempty,omitzero"`
}

// DeepCopyObject implements runtime.Object
func (in *ObjectStorage) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// +kubebuilder:object:root=true

// ObjectStorageList contains a list of ObjectStorage
type ObjectStorageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ObjectStorage `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ObjectStorage{}, &ObjectStorageList{})
}

// NewEvent creates a new event associated with the object and ready
// to be published to Kubernetes API
func (os *ObjectStorage) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    os.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "ObjectStorage",
			Namespace:  os.Namespace,
			Name:       os.Name,
			UID:        os.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-object-storage-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/object-storage-controller",
	}
}
