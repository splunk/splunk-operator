// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
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

package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// NOTE: Do not make struct fields private (i.e. make sure all struct fields start with an uppercase) otherwise the
//		 serializer will not be able to access those fields and they will be left blank

// SplunkResourcesSpec defines the resource requirements for a SplunkEnterprise deployment
type SplunkResourcesSpec struct {
	SplunkCPURequest     string `json:"splunkCpuRequest"`
	SparkCPURequest      string `json:"sparkCpuRequest"`
	SplunkMemoryRequest  string `json:"splunkMemoryRequest"`
	SparkMemoryRequest   string `json:"sparkMemoryRequest"`
	SplunkCPULimit       string `json:"splunkCpuLimit"`
	SparkCPULimit        string `json:"sparkCpuLimit"`
	SplunkMemoryLimit    string `json:"splunkMemoryLimit"`
	SparkMemoryLimit     string `json:"sparkMemoryLimit"`
	SplunkEtcStorage     string `json:"splunkEtcStorage"`
	SplunkVarStorage     string `json:"splunkVarStorage"`
	SplunkIndexerStorage string `json:"splunkIndexerStorage"`
}

// SplunkTopologySpec defines the topology for a SplunkEnterprise deployment
type SplunkTopologySpec struct {
	Standalones  int `json:"standalones"`
	Indexers     int `json:"indexers"`
	SearchHeads  int `json:"searchHeads"`
	SparkWorkers int `json:"sparkWorkers"`
}

// SplunkEnterpriseSpec defines the desired state of SplunkEnterprise
type SplunkEnterpriseSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	EnableDFS        bool                `json:"enableDFS"`
	SparkImage       string              `json:"sparkImage"`
	SplunkImage      string              `json:"splunkImage"`
	SplunkVolumes    []v1.Volume         `json:"splunkVolumes"`
	DefaultsURL      string              `json:"defaultsUrl"`
	LicenseURL       string              `json:"licenseUrl"`
	ImagePullPolicy  string              `json:"imagePullPolicy"`
	StorageClassName string              `json:"storageClassName"`
	SchedulerName    string              `json:"schedulerName"`
	Affinity         *v1.Affinity        `json:"affinity"`
	Resources        SplunkResourcesSpec `json:"resources"`
	Topology         SplunkTopologySpec  `json:"topology"`
	Defaults         string              `json:"defaults"`
}

// SplunkEnterpriseStatus defines the observed state of SplunkEnterprise
type SplunkEnterpriseStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SplunkEnterprise is the Schema for the splunkenterprises API
// +k8s:openapi-gen=true
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
