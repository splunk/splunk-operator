package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// NOTE: Do not make struct fields private (i.e. make sure all struct fields start with an uppercase) otherwise the
//		 serializer will not be able to access those fields and they will be left blank

type SplunkConfigSpec struct {
	SplunkPassword string `json:"splunkPassword"`
	SplunkStartArgs string `json:"splunkStartArgs"`
	DefaultsConfigMapName string `json:"defaultsConfigMapName"`
	EnableDFS bool `json:"enableDFS"`
}

type SplunkTopologySpec struct {
	Standalones int `json:"standalones"`
	Indexers int `json:"indexers"`
	SearchHeads int `json:"searchHeads"`
	SparkWorkers int `json:"sparkWorkers"`
}

// SplunkEnterpriseSpec defines the desired state of SplunkEnterprise
type SplunkEnterpriseSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	Config SplunkConfigSpec `json:"config"`
	Topology SplunkTopologySpec `json:"topology"`
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