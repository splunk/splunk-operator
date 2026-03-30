package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Your AppRuntime, AppRuntimeList, AppRuntimeSpec, AppRuntimeStatus structs with kubebuilder marker comments.

type AppRuntimeSpec struct {
	Replicas int32  `json:"replicas"`
	Image    string `json:"image"`
}

type AppRuntimeStatus struct {
	// current phase of the App Runtime pod
	Phase Phase `json:"phase"`

	// Auxiliary message describing CR status
	Message string `json:"message"`
}

// AppRuntime is the Schema for a App Runtime component
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=appruntimes,scope=Namespaced,shortName=ar
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type AppRuntime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppRuntimeSpec   `json:"spec,omitempty"`
	Status AppRuntimeStatus `json:"status,omitempty"`
}

// AppRuntimeList contains a list of AppRuntime
// +kubebuilder:object:root=true
type AppRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []AppRuntime `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppRuntime{}, &AppRuntimeList{})
}

// todo mb: NewEvent
