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
	// ClusterManagerPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	ClusterManagerPausedAnnotation = "clustermanager.enterprise.splunk.com/paused"
	// ClusterManagerPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	ClusterManagerMaintenanceAnnotation = "clustermanager.enterprise.splunk.com/maintenance"
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
	Phase Phase `json:"phase"`

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

	// Telemetry App installation flag
	TelAppInstalled bool `json:"telAppInstalled"`

	// Indicates if the cluster is in maintenance mode.
	MaintenanceMode bool `json:"maintenanceMode"`

	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`

	// ErrorMessage shows current error if there are any
	ErrorMessage string `json:"errorMessage"`
}

// BundlePushInfo Indicates if bundle push required
type BundlePushInfo struct {
	NeedToPushMasterApps  bool  `json:"needToPushMasterApps"` // NeedToPushMasterApps is an exception needed for dual support
	NeedToPushManagerApps bool  `json:"needToPushManagerApps"`
	LastCheckInterval     int64 `json:"lastCheckInterval"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterManager is the Schema for the cluster manager API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=clustermanagers,scope=Namespaced,shortName=cmanager-idxc
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Phase of the cluster manager"
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
			Namespace:    cmstr.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "SearchHeadCluster",
			Namespace:  cmstr.GetNamespace(),
			Name:       cmstr.GetName(),
			UID:        cmstr.GetUID(),
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
