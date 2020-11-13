// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// StandaloneSpec defines the desired state of a Splunk Enterprise standalone instances.
type StandaloneSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Number of standalone pods
	Replicas int32 `json:"replicas"`

	// SparkRef refers to a Spark cluster managed by the operator within Kubernetes
	// When defined, Data Fabric Search (DFS) will be enabled and configured to use the Spark cluster.
	SparkRef corev1.ObjectReference `json:"sparkRef"`

	// Image to use for Spark pod containers (overrides RELATED_IMAGE_SPLUNK_SPARK environment variables)
	SparkImage string `json:"sparkImage"`

	//Splunk Smartstore configuration. Refer to indexes.conf.spec and server.conf.spec on docs.splunk.com
	SmartStore SmartStoreSpec `json:"smartstore,omitempty"`
}

// StandaloneStatus defines the observed state of a Splunk Enterprise standalone instances.
type StandaloneStatus struct {
	// current phase of the standalone instances
	Phase splcommon.Phase `json:"phase"`

	// number of desired standalone instances
	Replicas int32 `json:"replicas"`

	// current number of ready standalone instances
	ReadyReplicas int32 `json:"readyReplicas"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	//Splunk Smartstore configuration. Refer to indexes.conf.spec and server.conf.spec on docs.splunk.com
	SmartStore SmartStoreSpec `json:"smartstore,omitempty"`

	// Resource Revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Standalone is the Schema for a Splunk Enterprise standalone instances.
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=standalones,scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of standalone instances"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Number of desired standalone instances"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready standalone instances"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of standalone resource"
type Standalone struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StandaloneSpec   `json:"spec,omitempty"`
	Status StandaloneStatus `json:"status,omitempty"`
}

// blank assignment to verify that Standalone implements splcommon.MetaObject
var _ splcommon.MetaObject = &Standalone{}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StandaloneList contains a list of Standalone
type StandaloneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Standalone `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Standalone{}, &StandaloneList{})
}
