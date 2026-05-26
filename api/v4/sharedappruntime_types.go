package v4

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SharedAppRuntimeSpec struct {
	// AppPodImage is the image used for shared app pods (dispatcher + NsJail).
	AppPodImage string `json:"appPodImage"`

	// SplunkImage is used by the per-app-pod init container to copy /opt/splunk/bin
	// and /opt/splunk/lib until Task #5 replaces this with per-instance bin/lib PVCs.
	SplunkImage string `json:"splunkImage"`

	// Apps is the list of Splunk app names to create shared pods for.
	// One Pod per (node, app) is created. Until the app-discovery sidecar (Task #3)
	// lands, this list is authoritative.
	Apps []string `json:"apps,omitempty"`
}

type SharedAppRuntimeStatus struct {
	Phase   Phase  `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`

	// ReconciledPods is the list of Pod names the controller currently owns,
	// in the form "appruntime-<nodeId>-<app>".
	ReconciledPods []string `json:"reconciledPods,omitempty"`
}

// SharedAppRuntime is the Schema for the SharedAppRuntime component.
// One CR per namespace manages the pool of shared app pods, one Pod per (node, app).
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=sharedappruntimes,scope=Namespaced,shortName=sar
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type SharedAppRuntime struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SharedAppRuntimeSpec   `json:"spec,omitempty"`
	Status SharedAppRuntimeStatus `json:"status,omitempty"`
}

// SharedAppRuntimeList contains a list of SharedAppRuntime.
// +kubebuilder:object:root=true
type SharedAppRuntimeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SharedAppRuntime `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SharedAppRuntime{}, &SharedAppRuntimeList{})
}
