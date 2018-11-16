package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SplunkInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []SplunkInstance `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SplunkInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              SplunkInstanceSpec   `json:"spec"`
	Status            SplunkInstanceStatus `json:"status,omitempty"`
}

type SplunkInstanceSpec struct {
	Standalones int `json:"standalones"`
	Indexers int `json:"indexers"`
	SearchHeads int `json:"searchHeads"`
	EnableDFS bool `json:"enableDFS"`
	SparkWorkers int `json:"sparkWorkers"`
}
type SplunkInstanceStatus struct {
	// Fill me
}
