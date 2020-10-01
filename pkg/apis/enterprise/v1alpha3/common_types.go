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

package v1alpha3

import (
	corev1 "k8s.io/api/core/v1"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

const (
	// APIVersion is a string representation of this API
	APIVersion = "enterprise.splunk.com/v1alpha3"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// CommonSplunkSpec defines the desired state of parameters that are common across all Splunk Enterprise CRD types
type CommonSplunkSpec struct {
	splcommon.Spec `json:",inline"`

	// Name of StorageClass to use for persistent volume claims
	StorageClassName string `json:"storageClassName"`

	// Storage capacity to request for /opt/splunk/etc persistent volume claims (default=”1Gi”)
	EtcStorage string `json:"etcStorage"`

	// Storage capacity to request for /opt/splunk/var persistent volume claims (default=”50Gi”)
	VarStorage string `json:"varStorage"`

	// If true, ephemeral (emptyDir) storage will be used for /opt/splunk/etc and /opt/splunk/var volumes
	EphemeralStorage bool `json:"ephemeralStorage"`

	// List of one or more Kubernetes volumes. These will be mounted in all pod containers as as /mnt/<name>
	Volumes []corev1.Volume `json:"volumes"`

	// Inline map of default.yml overrides used to initialize the environment
	Defaults string `json:"defaults"`

	// Full path or URL for one or more default.yml files, separated by commas
	DefaultsURL string `json:"defaultsUrl"`

	// Full path or URL for a Splunk Enterprise license file
	LicenseURL string `json:"licenseUrl"`

	// LicenseMasterRef refers to a Splunk Enterprise license master managed by the operator within Kubernetes
	LicenseMasterRef corev1.ObjectReference `json:"licenseMasterRef"`

	// ClusterMasterRef refers to a Splunk Enterprise indexer cluster managed by the operator within Kubernetes
	ClusterMasterRef corev1.ObjectReference `json:"clusterMasterRef"`

	// Mock to differentiate between UTs and actual reconcile
	Mock bool `json:"Mock"`
}

// SmartStoreSpec defines Splunk indexes and remote storage volume configuration
type SmartStoreSpec struct {
	// List of remote storage volumes
	VolList []VolumeSpec `json:"volumes,omitempty"`

	// List of Splunk indexes
	IndexList []IndexSpec `json:"indexes,omitempty"`
}

// VolumeSpec defines remote volume name and remote volume URI
type VolumeSpec struct {
	// Remote volume name
	Name string `json:"name"`

	// Remote volume URI
	Endpoint string `json:"endpoint"`

	// Remote volume path
	Path string `json:"path"`
}

// IndexSpec defines Splunk index name and storage path
type IndexSpec struct {
	// Splunk index name
	Name string `json:"name"`

	// Index location relative to the remote volume path
	RemotePath string `json:"remotePath"`

	// Remote Volume name
	VolName string `json:"volumeName"`
}
