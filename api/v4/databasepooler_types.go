package v4

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +kubebuilder:validation:Enum=rw;ro
type PoolerMode string

type DatabasePoolerSpec struct {
	ClusterRef LocalRef   `json:"clusterRef"`
	Mode       PoolerMode `json:"mode"` // rw|ro
	// You can extend with size/limits here later
}

type DatabasePoolerStatus struct {
	Ready       bool               `json:"ready,omitempty"`
	ServiceName string             `json:"serviceName,omitempty"`
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=dbpooler
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=.spec.clusterRef.name
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=.spec.mode
type DatabasePooler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DatabasePoolerSpec   `json:"spec,omitempty"`
	Status            DatabasePoolerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type DatabasePoolerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DatabasePooler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DatabasePooler{}, &DatabasePoolerList{})
}
