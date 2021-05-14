// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

package v1

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

// ClusterMasterSpec defines the desired state of ClusterMaster
type ClusterMasterSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Splunk Smartstore configuration. Refer to indexes.conf.spec and server.conf.spec on docs.splunk.com
	SmartStore SmartStoreSpec `json:"smartstore,omitempty"`

	// Splunk Enterprise App repository. Specifies remote App location and scope for Splunk App management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`
}

// ClusterMasterStatus defines the observed state of ClusterMaster
type ClusterMasterStatus struct {
	// current phase of the cluster master
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
	NeedToPushMasterApps bool  `json:"needToPushMasterApps"`
	LastCheckInterval    int64 `json:"lastCheckInterval"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterMaster is the Schema for the clustermasters API
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of cluster master"
// +kubebuilder:resource:path=clustermasters,scope=Namespaced,shortName=cm-idxc
// +kubebuilder:storageversion
type ClusterMaster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterMasterSpec   `json:"spec,omitempty"`
	Status ClusterMasterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterMasterList contains a list of ClusterMaster
type ClusterMasterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterMaster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterMaster{}, &ClusterMasterList{})
}
