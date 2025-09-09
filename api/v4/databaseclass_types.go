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

type DatabaseClassSpec struct {
	ManagedBy string       `json:"managedBy"` // CNPG | External | AWSAurora | ...
	Version   string       `json:"version,omitempty"`
	CNPG      *CNPGSpec    `json:"cnpg,omitempty"` // reuse CNPGSpec from DatabaseCluster
	Backup    *BackupSpec  `json:"backup,omitempty"`
	Policy    *ClassPolicy `json:"policy,omitempty"`
}

type ClassPolicy struct {
	TLSRequired          bool     `json:"tlsRequired,omitempty"`
	AllowInlineOverrides []string `json:"allowInlineOverrides,omitempty"`
}

// DatabaseClassStatus defines the observed state of DatabaseClass.
type DatabaseClassStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=dbclass
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=.spec.managedBy
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=.spec.version

// DatabaseClass is the Schema for the databaseclasses API.
type DatabaseClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DatabaseClassSpec   `json:"spec,omitempty"`
	Status DatabaseClassStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseClassList contains a list of DatabaseClass.
type DatabaseClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabaseClass `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabaseClass{}, &DatabaseClassList{})
}
