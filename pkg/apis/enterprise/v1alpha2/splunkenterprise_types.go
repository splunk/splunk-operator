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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

// SplunkResourcesSpec defines the resource requirements for a SplunkEnterprise deployment
type SplunkResourcesSpec struct {
	// Sets the CPU request (minimum) for Splunk pods (default=”0.1”)
	SplunkCPURequest string `json:"splunkCpuRequest"`

	// Sets the CPU request (minimum) for Spark pods (default=”0.1”)
	SparkCPURequest string `json:"sparkCpuRequest"`

	// Sets the memory request (minimum) for Splunk pods (default=”1Gi”)
	SplunkMemoryRequest string `json:"splunkMemoryRequest"`

	// Sets the memory request (minimum) for Spark pods (default=”1Gi”)
	SparkMemoryRequest string `json:"sparkMemoryRequest"`

	// Sets the CPU limit (maximum) for Splunk pods (default=”4”)
	SplunkCPULimit string `json:"splunkCpuLimit"`

	// Sets the CPU limit (maximum) for Spark pods (default=”4”)
	SparkCPULimit string `json:"sparkCpuLimit"`

	// Sets the memory limit (maximum) for Splunk pods (default=”8Gi”)
	SplunkMemoryLimit string `json:"splunkMemoryLimit"`

	// Sets the memory limit (maximum) for Spark pods (default=”8Gi”)
	SparkMemoryLimit string `json:"sparkMemoryLimit"`

	// Storage capacity to request for Splunk etc volume claims (default=”1Gi”)
	SplunkEtcStorage string `json:"splunkEtcStorage"`

	// Storage capacity to request for Splunk var volume claims (default=”50Gi”)
	SplunkVarStorage string `json:"splunkVarStorage"`

	// Storage capacity to request for Splunk var volume claims on indexers (default=”200Gi”)
	SplunkIndexerStorage string `json:"splunkIndexerStorage"`
}

// SplunkTopologySpec defines the topology for a SplunkEnterprise deployment
type SplunkTopologySpec struct {
	// The number of standalone instances to deploy.
	// +kubebuilder:validation:Minimum=0
	Standalones int `json:"standalones"`

	// The number of indexers to deploy. If this number is greater than 0, a cluster master will also be deployed.
	// +kubebuilder:validation:Minimum=0
	Indexers int `json:"indexers"`

	// The number of search heads to deploy. If this number is greater than 0, a deployer will also be deployed.
	// +kubebuilder:validation:Minimum=0
	SearchHeads int `json:"searchHeads"`

	// The number of spark workers to launch (defaults to 0 if enableDFS is false, or 1 if enableDFS is true)
	// +kubebuilder:validation:Minimum=0
	SparkWorkers int `json:"sparkWorkers"`
}

// SplunkEnterpriseSpec defines the desired state of SplunkEnterprise
type SplunkEnterpriseSpec struct {
	// If this is true, DFS will be installed and enabled on all searchHeads and a spark cluster will be created.
	EnableDFS bool `json:"enableDFS"`

	// Docker image to use for Spark instances (overrides SPARK_IMAGE environment variables)
	SparkImage string `json:"sparkImage"`

	// Docker image to use for Splunk instances (overrides SPLUNK_IMAGE environment variables)
	SplunkImage string `json:"splunkImage"`

	// List of one or more Kubernetes volumes. These will be mounted in all Splunk containers as as /mnt/<name>
	SplunkVolumes []v1.Volume `json:"splunkVolumes"`

	// Inline map of default.yml overrides used to initialize the environment
	Defaults string `json:"defaults"`

	// Full path or URL for one or more default.yml files, separated by commas
	DefaultsURL string `json:"defaultsUrl"`

	// Full path or URL for a Splunk Enterprise license file
	LicenseURL string `json:"licenseUrl"`

	// Sets pull policy for all images (either “Always” or the default: “IfNotPresent”)
	// +kubebuilder:validation:Enum=Always;IfNotPresent
	ImagePullPolicy string `json:"imagePullPolicy"`

	// Name of StorageClass to use for persistent volume claims
	StorageClassName string `json:"storageClassName"`

	// Name of Scheduler to use for pod placement (defaults to “default-scheduler”)
	SchedulerName string `json:"schedulerName"`

	// Kubernetes Affinity rules that control how pods are assigned to particular nodes
	Affinity *v1.Affinity `json:"affinity"`

	// resource requirements for a SplunkEnterprise deployment
	Resources SplunkResourcesSpec `json:"resources"`

	// desired topology for a SplunkEnterprise deployment
	Topology SplunkTopologySpec `json:"topology"`
}

// SplunkEnterpriseStatus defines the observed state of SplunkEnterprise
type SplunkEnterpriseStatus struct {
	// current phase of the search head cluster
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	SearchPhase string `json:"searchPhase"`

	// current phase of the indexer cluster
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	IndexingPhase string `json:"indexingPhase"`

	// current phase of the dfs workers
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	DfsPhase string `json:"dfsPhase"`

	// current phase of the standalone instances
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	StandalonePhase string `json:"standalonePhase"`

	// current phase of the license master
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	LicenseMasterPhase string `json:"licenseMasterPhase"`

	// current phase of the cluster master
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	ClusterMasterPhase string `json:"clusterMasterPhase"`

	// current phase of the deployer
	// +kubebuilder:validation:Enum=pending;ready;scaleup;scaledown;updating
	DeployerPhase string `json:"deployerPhase"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SplunkEnterprise is the Schema for the splunkenterprises API
//
// NAME   SEARCH        INDEXING        DFS             STANDALONE        LM      CM      DEP
// demo   ready (5/5)   scaleup (3/5)   pending (3/5)   scaledown (5/3)   ready   ready   ready
//
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=splunkenterprises,scope=Namespaced,shortName=enterprise;se
// +kubebuilder:printcolumn:name="Search",type="integer",JSONPath=".spec.status.searchPhase",description="Status of search head cluster"
// +kubebuilder:printcolumn:name="Indexing",type="integer",JSONPath=".spec.status.indexingPhase",description="Status of indexer cluster"
// +kubebuilder:printcolumn:name="DFS",type="integer",JSONPath=".spec.topology.dfsPhase",description="Status of DFS spark workers"
// +kubebuilder:printcolumn:name="Standalone",type="integer",JSONPath=".spec.topology.standalonePhase",description="Status of standlone instances"
// +kubebuilder:printcolumn:name="LM",type="string",JSONPath=".status.licenseMasterPhase",description="Status of license master"
// +kubebuilder:printcolumn:name="CM",type="string",JSONPath=".status.clusterMasterPhase",description="Status of cluster master"
// +kubebuilder:printcolumn:name="Dep",type="string",JSONPath=".status.deployerPhase",description="Status of deployer"
type SplunkEnterprise struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SplunkEnterpriseSpec   `json:"spec,omitempty"`
	Status SplunkEnterpriseStatus `json:"status,omitempty"`
}

// GetIdentifier is a convenience function to return unique identifier for the Splunk enterprise deployment
func (cr *SplunkEnterprise) GetIdentifier() string {
	return cr.GetObjectMeta().GetName()
}

// GetNamespace is a convenience function to return namespace for a Splunk enterprise deployment
func (cr *SplunkEnterprise) GetNamespace() string {
	return cr.GetObjectMeta().GetNamespace()
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SplunkEnterpriseList contains a list of SplunkEnterprise
type SplunkEnterpriseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SplunkEnterprise `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SplunkEnterprise{}, &SplunkEnterpriseList{})
}
