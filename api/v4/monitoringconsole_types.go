/*
Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
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
	Phase Phase `json:"phase"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Bundle push status tracker
	BundlePushTracker BundlePushInfo `json:"bundlePushInfo"`

	// Resource Revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// App Framework status
	AppContext AppDeploymentContext `json:"appContext,omitempty"`

	// Auxillary message describing CR status
	Message string `json:"message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MonitoringConsole is the Schema for the monitoringconsole API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=monitoringconsoles,scope=Namespaced,shortName=mc
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of monitoring console"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Desired number of monitoring console members"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready monitoring console members"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of monitoring console"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
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

// NewEvent creates a new event associated with the object and ready
// to be published to the kubernetes API.
func (mcnsl *MonitoringConsole) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    mcnsl.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "MonitoringConsole",
			Namespace:  mcnsl.Namespace,
			Name:       mcnsl.Name,
			UID:        mcnsl.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-monitoringconsole-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/monitoringconsole-controller",
	}
}
