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

// GenAIDeploymentSpec defines the desired state of GenAIDeployment
type GenAIDeploymentSpec struct {
	SaisService     SaisServiceSpec `json:"saisService"`               // Configuration for SaisService
	S3Bucket        VolumeSpec      `json:"s3Bucket"`                  // S3 bucket or other remote storage configuration
	Scale           int32           `json:"scale"`                     // Number of replicas for scaling
	ServiceAccount  string          `json:"serviceAccount,omitempty"`  // Service Account for IRSA authentication
	RayService      RayServiceSpec  `json:"rayService,omitempty"`      // Ray service configuration with embedded RayClusterSpec
	VectorDbService VectorDbSpec    `json:"vectordbService,omitempty"` // VectorDB service configuration
}

// SaisServiceSpec defines the configuration for a single SaisService deployment
type SaisServiceSpec struct {
	Image                     string                            `json:"image"`                               // Container image for the service
	Resources                 corev1.ResourceRequirements       `json:"resources"`                           // Resource requirements for the container (CPU, Memory)
	Volume                    corev1.Volume                     `json:"volume,omitempty"`                    // Volume specifications using corev1.Volume
	Replicas                  int32                             `json:"replicas"`                            // Number of replicas for the service
	SchedulerName             string                            `json:"schedulerName"`                       // Name of Scheduler to use for pod placement (defaults to “default-scheduler”)
	Affinity                  corev1.Affinity                   `json:"affinity"`                            // Kubernetes Affinity rules that control how pods are assigned to particular nodes.
	Tolerations               []corev1.Toleration               `json:"tolerations,omitempty"`               // Pod's tolerations for Kubernetes node's taint
	ServiceTemplate           corev1.Service                    `json:"serviceTemplate"`                     // ServiceTemplate is a template used to create Kubernetes services
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"` // TopologySpreadConstraint https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/
}

type HeadGroup struct {
	Resources corev1.ResourceRequirements `json:"resources"` // Resource requirements for the container (CPU, Memory)
	NumCpus   string                      `json:"numCpus,omitempty"`
}

type WorkerGroup struct {
	Resources corev1.ResourceRequirements `json:"resources"` // Resource requirements for the container (CPU, Memory)
	NumCpus   string                      `json:"numCpus,omitempty"`
	Replicas  int32                       `json:"replicas,omitempty"`
}

// RayServiceSpec defines the Ray service configuration
type RayServiceSpec struct {
	Enabled     bool        `json:"enabled"`          // Whether RayService is enabled
	Config      string      `json:"config,omitempty"` // Additional RayService configuration
	Image       string      `json:"image,omitempty"`
	HeadGroup   HeadGroup   `json:"headGroup,omitempty"`
	WorkerGroup WorkerGroup `json:"workerGroup,omitempty"`
}

// VectorDbSpec defines the VectorDB service configuration
type VectorDbSpec struct {
	Image                     string                            `json:"image"`                               // Container image for the service
	Enabled                   bool                              `json:"enabled"`                             // Whether VectorDBService is enabled
	Config                    string                            `json:"config,omitempty"`                    // Additional VectorDB configuration
	Resources                 corev1.ResourceRequirements       `json:"resources"`                           // Resource requirements for the container (CPU, Memory)
	Volume                    corev1.Volume                     `json:"volume,omitempty"`                    // Volume specifications using corev1.Volume
	Replicas                  int32                             `json:"replicas"`                            // Number of replicas for the service
	Affinity                  corev1.Affinity                   `json:"affinity"`                            // Kubernetes Affinity rules that control how pods are assigned to particular nodes.
	Tolerations               []corev1.Toleration               `json:"tolerations,omitempty"`               // Pod's tolerations for Kubernetes node's taint
	ServiceTemplate           corev1.Service                    `json:"serviceTemplate"`                     // ServiceTemplate is a template used to create Kubernetes services
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"` // TopologySpreadConstraint https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/
}

// GenAIDeploymentStatus defines the observed state of GenAIDeployment
type GenAIDeploymentStatus struct {
	SaisServiceStatus   SaisServiceStatus `json:"saisServiceStatus,omitempty"`   // Status of the SaisService
	VectorDbStatus      VectorDbStatus    `json:"vectorDbStatus,omitempty"`      // Status of the VectorDB service
	RayClusterStatus    RayClusterStatus  `json:"rayClusterStatus,omitempty"`    // Status of the RayCluster
	GenAIWorkloadStatus string            `json:"genAIWorkloadStatus,omitempty"` // Overall status of the GenAI workload
}

// SaisServiceStatus defines the status of the SaisService deployment
type SaisServiceStatus struct {
	Name     string `json:"name"`     // Name of the SaisService deployment
	Replicas int32  `json:"replicas"` // Number of replicas running
	Status   string `json:"status"`   // Current status of the deployment (e.g., "Running", "Failed")
	Message  string `json:"message"`  // Additional information about the service status
}

// VectorDbStatus defines the status of the VectorDB service
type VectorDbStatus struct {
	Enabled bool   `json:"enabled"` // Whether the VectorDB service is enabled
	Status  string `json:"status"`  // Current status of the VectorDB service (e.g., "Running", "Failed")
	Message string `json:"message"` // Additional information about the VectorDB service status
}

// RayClusterStatus defines the status of the RayCluster
type RayClusterStatus struct {
	ClusterName string             `json:"clusterName"`          // Name of the RayCluster
	State       string             `json:"state"`                // Current state of the RayCluster (e.g., "Running", "Failed")
	Conditions  []metav1.Condition `json:"conditions,omitempty"` // Conditions describing the state of the RayCluster
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GenAIDeployment is the Schema for the genaideployments API
type GenAIDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GenAIDeploymentSpec   `json:"spec,omitempty"`
	Status GenAIDeploymentStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GenAIDeploymentList contains a list of GenAIDeployment
type GenAIDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GenAIDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GenAIDeployment{}, &GenAIDeploymentList{})
}
