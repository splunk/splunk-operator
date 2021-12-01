/*
Copyright 2021.

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

package v3

import (
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

const (
	// MonitoringConsolePausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	MonitoringConsolePausedAnnotation = "monitoringconsole.enterprise.splunk.com/paused"
)

// MonitoringConsoleSpec defines the desired state of MonitoringConsole
type MonitoringConsoleSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Splunk Enterprise App repository. Specifies remote App location and scope for Splunk App management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`
}

// MonitoringConsoleStatus defines the observed state of MonitoringConsole
type MonitoringConsoleStatus struct {
	// current phase of the monitoring console
	Phase splcommon.Phase `json:"phase"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Bundle push status tracker
	BundlePushTracker BundlePushInfo `json:"bundlePushInfo"`

	// Resource Revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// App Framework status
	AppContext AppDeploymentContext `json:"appContext,omitempty"`
}

//+kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MonitoringConsole is the Schema for the monitoringconsole API
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of monitoring console"
// +kubebuilder:resource:path=monitoringconsoles,scope=Namespaced,shortName=mc
// +kubebuilder:storageversion
type MonitoringConsole struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MonitoringConsoleSpec   `json:"spec,omitempty"`
	Status MonitoringConsoleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MonitoringConsoleList contains a list of MonitoringConsole
type MonitoringConsoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MonitoringConsole `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MonitoringConsole{}, &MonitoringConsoleList{})
}
