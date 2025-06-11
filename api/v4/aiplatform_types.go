/*
Copyright 2024.

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
	rayv1 "github.com/ray-project/kuberay/ray-operator/apis/ray/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AIPlatform is the Schema for the AIPlatform API
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=aiplatforms,scope=Namespaced,shortName=spai
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type AIPlatform struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AIPlatformSpec   `json:"spec,omitempty"`
	Status AIPlatformStatus `json:"status,omitempty"`
}

// AIPlatformSpec defines the desired state
type AIPlatformSpec struct {

	Volume      AiVolumeSpec `json:"volume,omitempty"`
	// s3://bucket/artifacts
	// s3://bucket/tasks - get rid of this

	AppsVolume      AiVolumeSpec `json:"appsVolume,omitempty"`
	ArtifactsVolume AiVolumeSpec `json:"artifactsVolume,omitempty"`

	Features:
	   // saia-spl
	      serviceAccountName string `json:"serviceAccountName,omitempty"`
	   // saia-sec
		  serviceAccountName string `json:"serviceAccountName,omitempty"`

	HeadGroupSpec          HeadGroupSpec   `json:"headGroupSpec,omitempty"`
	WorkerGroupSpec        WorkerGroupSpec `json:"workerGroupSpec,omitempty"`
	DefaultAcceleratorType string          `json:"defaultAcceleratorType"`
	// Which sidecars to inject
	Sidecars SidecarConfig `json:"sidecars,omitempty"`

	// cert-manager Certificate for mTLS
	CertificateRef string `json:"certificateRef,omitempty"`

	// Cluster domain (default: cluster.local)
	// +kubebuilder:default=cluster.local
	ClusterDomain string `json:"clusterDomain,omitempty"`

	// SplunkConfiguration instance reference
	SplunkConfiguration SplunkConfiguration `json:"splunkConfiguration,omitempty"`

	Weaviate       WeaviateSpec     `json:"weaviate,omitempty"`
	weaviateStorage PersistentVolumeClaim `json:"storage,omitempty"`
	SchedulingSpec `json:",inline"` // inlines NodeSelector, Tolerations, Affinity
	Ingress       `json:",inline"`
}

type WeaviateSpec struct {
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas"`
	//Image              string                      `json:"image"`
	Resources          corev1.ResourceRequirements `json:"resources,omitempty"`
	ServiceAccountName string                      `json:"serviceAccountName,omitempty"`
	SchedulingSpec     `json:",inline"`            // inlines NodeSelector, Tolerations, Affinity
}

type HeadGroupSpec struct {
	ServiceAccountName string           `json:"serviceAccountName,omitempty"`
	SchedulingSpec     `json:",inline"` // inlines NodeSelector, Tolerations, Affinity
	// image registries for Ray
	ImageRegistry string `json:"imageRegistry,omitempty"`
}

type WorkerGroupSpec struct {
	ServiceAccountName string           `json:"serviceAccountName,omitempty"`
	ImageRegistry      string           `json:"imageRegistry,omitempty"`
	GPUConfigs         []GPUConfig      `json:"gpuConfigs,omitempty"`
	SchedulingSpec     `json:",inline"` // inlines NodeSelector, Tolerations, Affinity
}

// GPUConfig defines one worker-tier with scheduling and accelerator settings.
type GPUConfig struct {
	Tier        string                      `json:"tier"`
	MinReplicas int32                       `json:"minReplicas"`
	MaxReplicas int32                       `json:"maxReplicas"`
	GPUsPerPod  int32                       `json:"gpusPerPod"`
	Resources   corev1.ResourceRequirements `json:"resources,omitempty"`
}

// SchedulingSpec exposes common pod-scheduling knobs.
type SchedulingSpec struct {
	NodeSelector map[string]string   `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration `json:"tolerations,omitempty"`
	Affinity     *corev1.Affinity    `json:"affinity,omitempty"`
}

type SplunkConfiguration struct {
	// Name of the SplunkConfiguration instance
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:MinLength=1
	CRName string `json:"crName,omitempty"`
	// Namespace of the SplunkConfiguration instance
	// +kubebuilder:validation:Pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:MinLength=1
	CRNamespace string `json:"crNamespace,omitempty"`
	// Splunk secret reference
	SecretRef corev1.SecretReference `json:"secretRef,omitempty"`
	Endpoint  string                 `json:"endpoint,omitempty"`
	Token     string                 `json:"token,omitempty"`
}

// ReplicasSpec sets min/max worker replicas
type ReplicasSpec struct {
	Min int32 `json:"min,omitempty"`
	Max int32 `json:"max,omitempty"`
}

// MachineClass configures CPU, memory, GPU per-worker
type MachineClass struct {
	ResourceRequirements corev1.ResourceRequirements `json:"resourceRequirements,omitempty"`
	GPU                  int32                       `json:"gpu,omitempty"`
	EphimeralStorage     string                      `json:"ephemeral-storage,omitempty"` // e.g. "100Gi"
}

// SidecarConfig toggles injection of sidecars
type SidecarConfig struct {
	// +kubebuilder:default=true
	Envoy bool `json:"envoy,omitempty"`
	// +kubebuilder:default=true
	FluentBit bool `json:"fluentBit,omitempty"`
	// +kubebuilder:default=true
	Otel bool `json:"otel,omitempty"`
	// +kubebuilder:default=true
	PrometheusOperator bool `json:"prometheusOperator,omitempty"`
}

type AiVolumeSpec struct {
	// Remote volume URI in the format s3://bucketname/<path prefix>
	Path string `json:"path"` // s3://bucketname/<path prefix> or gs://bucketname/<path prefix> or azure://containername/<path prefix>

	// optional override endpoint (only really needed for S3-compatible like MinIO)
	Endpoint string `json:"endpoint,omitempty"`

	// Region of the remote storage volume where apps reside. Used for aws, if provided. Not used for minio and azure.
	Region string `json:"region"`

	// Secret object name
	SecretRef string `json:"secretRef"`
}

// AIPlatformStatus defines observed state
type AIPlatformStatus struct {
	RayServiceName      string              `json:"rayServiceName,omitempty"`
	VectorDbServiceName string              `json:"vectorDbServiceName,omitempty"`
	RayServiceStatus    rayv1.ServiceStatus `json:"rayServiceStatus,omitempty"`
	Conditions          []metav1.Condition  `json:"conditions,omitempty"`
	ObservedGeneration  int64               `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
type AIPlatformList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AIPlatform `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AIPlatform{}, &AIPlatformList{})
}
