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

package v4

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	// KVServicePausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	KVServicePausedAnnotation = "kvservice.enterprise.splunk.com/paused"
)

// CommonSplunkSpec defines the desired state of parameters that are common across all Splunk Enterprise CRD types
type KVServiceSpec struct {
	Spec `json:",inline"`

	// List of one or more Kubernetes volumes. These will be mounted in all pod containers as as /mnt/<name>
	// +optional
	Volumes []corev1.Volume `json:"volumes"`

	// Inline map of default.yml overrides used to initialize the environment
	// +optional
	Defaults string `json:"defaults"`

	// Full path or URL for one or more default.yml files, separated by commas
	// +optional
	// +kubebuilder:validation:Pattern=`^(https?://|file://|/)`
	DefaultsURL string `json:"defaultsUrl,omitempty"`

	// Full path or URL for a Splunk Enterprise license file
	// +optional
	// +kubebuilder:validation:Pattern=`^(https?://|file://|/)`
	LicenseURL string `json:"licenseUrl,omitempty"`

	// Mock to differentiate between UTs and actual reconcile
	Mock bool `json:"Mock"`

	// ServiceAccount is the service account used by the pods deployed by the CRD.
	// If not specified uses the default serviceAccount for the namespace as per
	// https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#use-the-default-service-account-to-access-the-api-server
	// +optional
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	ServiceAccount string `json:"serviceAccount,omitempty"`

	// ExtraEnv refers to extra environment variables to be passed to the Splunk instance containers
	// WARNING: Setting environment variables used by Splunk or Ansible will affect Splunk installation and operation
	// +optional
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`

	// ReadinessInitialDelaySeconds defines initialDelaySeconds(See https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-readiness-probes) for Readiness probe
	// Note: If needed, Operator overrides with a higher value
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3600
	// +kubebuilder:default=30
	ReadinessInitialDelaySeconds int32 `json:"readinessInitialDelaySeconds"`

	// LivenessInitialDelaySeconds defines initialDelaySeconds(See https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-a-liveness-command) for the Liveness probe
	// Note: If needed, Operator overrides with a higher value
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3600
	// +kubebuilder:default=30
	LivenessInitialDelaySeconds int32 `json:"livenessInitialDelaySeconds"`

	// LivenessProbe as defined in https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-a-liveness-command
	// +optional
	LivenessProbe *Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe as defined in https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-readiness-probes
	// +optional
	ReadinessProbe *Probe `json:"readinessProbe,omitempty"`

	// Sets imagePullSecrets if image is being pulled from a private registry.
	// See https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Number of KVService pods
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
}

// KVServiceStatus defines the observed state of KVService
type KVServiceStatus struct {
	// current phase of the kvservice instances
	Phase Phase `json:"phase"`

	// number of desired kvservice instances
	Replicas int32 `json:"replicas"`

	// current number of ready kvservice instances
	ReadyReplicas int32 `json:"readyReplicas"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Resource Revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// Auxillary message describing CR status
	Message string `json:"message"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KVService is the Schema for the kvservices API
type KVService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KVServiceSpec   `json:"spec,omitempty"`
	Status KVServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KVServiceList contains a list of KVService
type KVServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KVService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KVService{}, &KVServiceList{})
}
