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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	// NoahClusterPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	NoahClusterPausedAnnotation = "noahcluster.enterprise.splunk.com/paused"
)

// NoahClusterSpec defines the desired state of NoahCluster
type NoahClusterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Database is an example field of NoahCluster. Edit noahcluster_types.go to remove/update
	Database string `json:"database,omitempty"`
	//DatabaseHost
	DatabaseHost string `json:"databaseHost,omitempty"`
	// DatabaseHostReader
	DatabaseHostReader string `json:"databaseHostReader,omitempty"`
	// EnableDatabaseAccessLogging
	EnableDatabaseAccessLogging bool `json:"enableDatabaseAccessLogging,omitempty"`
	// AuthEnv
	AuthEnv string `json:"authEnv,omitempty"`
	// AutoCreateTenants
	AutoCreateTenants bool `json:"autoCreateTenants,omitempty"`
	// Region
	Region string `json:"region,omitempty"`
	// AuthMock
	AuthMock string `json:"authMock,omitempty"`

	//DatabaseSecretRef
	DatabaseSecretRef string `json:"databaseSecretRef"`

	CommonSplunkSpec `json:",inline"`
}

// NoahClusterStatus defines the observed state of NoahCluster
type NoahClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// Foo is an example field of NoahCluster. Edit noahcluster_types.go to remove/update
	Database                    string `json:"database,omitempty"`
	DatabaseHost                string `json:"databaseHost,omitempty"`
	DatabaseHostReader          string `json:"databaseHostReader,omitempty"`
	EnableDatabaseAccessLogging bool   `json:"enableDatabaseAccessLogging,omitempty"`
	AuthEnv                     string `json:"authEnv,omitempty"`
	AutoCreateTenants           string `json:"autoCreateTenants,omitempty"`
	Region                      string `json:"region,omitempty"`
	AuthMock                    string `json:"authMock,omitempty"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`
	// current phase of the noah manager
	Phase Phase `json:"phase"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NoahCluster is the Schema for the noahclusters API
type NoahCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NoahClusterSpec   `json:"spec,omitempty"`
	Status NoahClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NoahClusterList contains a list of NoahCluster
type NoahClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NoahCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NoahCluster{}, &NoahClusterList{})
}
