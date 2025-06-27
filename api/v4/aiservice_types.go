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
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// AIServiceSpec defines the desired state of AIService
type AIServiceSpec struct {
	// SplunkConfiguration instance reference
	Version 		   string                 `json:"version,omitempty"`
	TaskVolume          AiVolumeSpec           `json:"taskVolume,omitempty"`
	SplunkConfiguration SplunkConfiguration    `json:"splunkConfiguration,omitempty"`
	VectorDbUrl         string                 `json:"vectorDbUrl"`
	AIPlatformUrl       string                 `json:"aiPlatformUrl,omitempty"`
	AIPlatformRef       corev1.ObjectReference `json:"aiPlatformRef,omitempty"`
	Replicas            int32                  `json:"replicas,omitempty"`
	ServiceAccountName  string                 `json:"serviceAccountName,omitempty"`
	//Port specifies the default port for the service
	Port        int32               `json:"port,omitempty" default:"80"`
	Env         map[string]string   `json:"env,omitempty"`
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// node affinity configuraiton
	Affinity corev1.Affinity `json:"affinity,omitempty"`
	// resources k8s resources cpu, memory
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// metrics configuration
	Metrics MetricsConfig `json:"metrics,omitempty"`
	// mtls configuration
	MTLS MTLSConfig `json:"mtls,omitempty"`
	// ServiceTemplate is a template used to create Kubernetes services
	ServiceTemplate corev1.Service `json:"serviceTemplate"`
}

type MetricsConfig struct {
	// Enable scraping of SAIA metrics
	Enabled bool `json:"enabled,omitempty"`
	// Path under /metrics, default "/metrics"
	Path string `json:"path,omitempty"`
	// Port name or number, default "metrics"
	Port int32 `json:"port,omitempty"`
}

type MTLSConfig struct {
	// Enable or disable mTLS on the SAIA service
	Enabled bool `json:"enabled"`
	// If Enabled, how to request the cert
	IssuerRef  cmmeta.ObjectReference `json:"issuerRef,omitempty"`
	SecretName string                 `json:"secretName,omitempty"`
	DNSNames   []string               `json:"dnsNames,omitempty"`
	// Let users declare “I don’t want operator-managed TLS” even if Enabled=true,
	// e.g. they’re on Istio and will terminate externally.
	Termination string `json:"termination,omitempty"` // "operator" or "mesh"
}

// AIServiceStatus defines the observed state of AIService
type AIServiceStatus struct {
	SchemaJobId        string             `json:"schemaJobId,omitempty"`
	VectorDbStatus     string             `json:"vectorDbStatus,omitempty"`
	PlatformStatus     string             `json:"platformStatus,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
}

// AIService is the Schema for the aiservices API
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=aiservices,scope=Namespaced,shortName=saia
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type AIService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AIServiceSpec   `json:"spec,omitempty"`
	Status AIServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AIServiceList contains a list of AIService
type AIServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AIService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AIService{}, &AIServiceList{})
}
