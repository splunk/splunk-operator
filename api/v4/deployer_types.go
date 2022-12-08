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
	// DeployerPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	DeployerPausedAnnotation = "deployer.enterprise.splunk.com/paused"
)

// DeployerSpec defines the desired state of a Splunk Enterprise Deployer
type DeployerSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Splunk Enterprise App repository. Specifies remote App location and scope for Splunk App management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`
}

// DeployerMemberStatus is used to track the status of Deployer
type DeployerMemberStatus struct {
	// Name of the deployer
	Name string `json:"name"`

	// Indicates the status of the member.
	Status string `json:"status"`
}

// DeployerStatus defines the observed state of a Splunk Enterprise Deployer
type DeployerStatus struct {
	// current phase of the deployer
	Phase Phase `json:"phase"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Indicates resource version of namespace scoped secret
	NamespaceSecretResourceVersion string `json:"namespace_scoped_secret_resource_version"`

	// App Framework Context
	AppContext AppDeploymentContext `json:"appContext"`

	// Telemetry App installation flag
	TelAppInstalled bool `json:"telAppInstalled"`
}

// Deployer is the Schema for a Splunk Enterprise Deployer
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=deployers,scope=Namespaced,shortName=dep
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of deployer"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of deployer"
// +kubebuilder:storageversion
type Deployer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeployerSpec   `json:"spec,omitempty"`
	Status DeployerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DeployerList contains a list of Deployer
type DeployerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Deployer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Deployer{}, &DeployerList{})
}

// NewEvent creates a new event associated with the object and ready
// to be published to the kubernetes API.
func (deployerstr *Deployer) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    deployerstr.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "Deployer",
			Namespace:  deployerstr.Namespace,
			Name:       deployerstr.Name,
			UID:        deployerstr.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-Deployer-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/Deployer-controller",
	}
}
