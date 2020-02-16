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

package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// CommonSpec defines the desired state of parameters that are common across all CRD types
type CommonSpec struct {
	// Image to use for Splunk pod containers (overrides SPLUNK_IMAGE environment variables)
	Image string `json:"image"`

	// Sets pull policy for all images (either “Always” or the default: “IfNotPresent”)
	// +kubebuilder:validation:Enum=Always;IfNotPresent
	ImagePullPolicy string `json:"imagePullPolicy"`

	// Name of Scheduler to use for pod placement (defaults to “default-scheduler”)
	SchedulerName string `json:"schedulerName"`

	// Kubernetes Affinity rules that control how pods are assigned to particular nodes.
	Affinity corev1.Affinity `json:"affinity"`

	// resource requirements for the pod containers
	Resources corev1.ResourceRequirements `json:"resources"`

	// ServiceTemplate is a template used to create Kubernetes services
	ServiceTemplate corev1.Service `json:"serviceTemplate"`
}

// CommonSplunkSpec defines the desired state of parameters that are common across all Splunk Enterprise CRD types
type CommonSplunkSpec struct {
	CommonSpec `json:",inline"`

	// Name of StorageClass to use for persistent volume claims
	StorageClassName string `json:"storageClassName"`

	// Storage capacity to request for /opt/splunk/etc persistent volume claims (default=”1Gi”)
	EtcStorage string `json:"etcStorage"`

	// Storage capacity to request for /opt/splunk/var persistent volume claims (default=”50Gi”)
	VarStorage string `json:"varStorage"`

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

	// IndexerRef refers to a Splunk Enterprise indexer cluster managed by the operator within Kubernetes
	IndexerRef corev1.ObjectReference `json:"indexerRef"`
}

// MetaObject is used to represent common interfaces of custom resources
type MetaObject interface {
	GetIdentifier() string
	GetNamespace() string
	GetTypeMeta() metav1.TypeMeta
	GetObjectMeta() metav1.Object
	GetObjectKind() schema.ObjectKind
	DeepCopyObject() runtime.Object
}
