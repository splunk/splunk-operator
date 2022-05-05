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

package v3

import (
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

const (
	// ClusterManagerPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	ClusterManagerPausedAnnotation = "clustermanager.enterprise.splunk.com/paused"
)

// ClusterManagerSpec defines the desired state of ClusterManager
type ClusterManagerSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Splunk Smartstore configuration. Refer to indexes.conf.spec and server.conf.spec on docs.splunk.com
	SmartStore SmartStoreSpec `json:"smartstore,omitempty"`

	// Splunk Enterprise App repository. Specifies remote App location and scope for Splunk App management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`
}

// ClusterManagerStatus defines the observed state of ClusterManager
type ClusterManagerStatus struct {
	// current phase of the cluster manager
	Phase splcommon.Phase `json:"phase"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Splunk Smartstore configuration. Refer to indexes.conf.spec and server.conf.spec on docs.splunk.com
	SmartStore SmartStoreSpec `json:"smartstore,omitempty"`

	// Bundle push status tracker
	BundlePushTracker BundlePushInfo `json:"bundlePushInfo"`

	// Resource Revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// App Framework status
	AppContext AppDeploymentContext `json:"appContext"`
}

// BundlePushInfo Indicates if bundle push required
type BundlePushInfo struct {
	NeedToPushManagerApps bool  `json:"needToPushManagerApps"`
	LastCheckInterval     int64 `json:"lastCheckInterval"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterManager is the Schema for the cluster manager API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=clustermanagers,scope=Namespaced,shortName=cm-idxc
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of cluster manager"
// +kubebuilder:printcolumn:name="Manager",type="string",JSONPath=".status.clusterManagerPhase",description="Status of cluster manager"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Desired number of indexer peers"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready indexer peers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of cluster manager"
// +kubebuilder:storageversion
type ClusterManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterManagerSpec   `json:"spec,omitempty"`
	Status ClusterManagerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterManagerList contains a list of ClusterManager
type ClusterManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterManager{}, &ClusterManagerList{})
}

// NewEvent creates a new event associated with the object and ready
// to be published to the kubernetes API.
func (cmstr *ClusterManager) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    cmstr.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "Clustermanager",
			Namespace:  cmstr.Namespace,
			Name:       cmstr.Name,
			UID:        cmstr.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-clustermanager-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/clustermanager-controller",
	}
}
