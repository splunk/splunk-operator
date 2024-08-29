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


const (
	// SplunkAppPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	SplunkAppPausedAnnotation = "splunkapp.enterprise.splunk.com/paused"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// SplunkAppSpec defines the desired state of the SplunkApp CRD
type SplunkAppSpec struct {
	GitRepo    string `json:"gitRepo"`
	Branch     string `json:"branch"`
	SecretRef  string `json:"secretRef"`
	S3Bucket   string `json:"s3Bucket"`
	S3Region   string `json:"s3Region"`
	IrsaRoleArn string `json:"irsaRoleArn"`
}

// SplunkAppStatus defines the observed state of the SplunkApp CRD
type SplunkAppStatus struct {
	// current phase of the search head cluster
	Phase Phase `json:"phase"`
	AppName    string `json:"appName"`
	Version    string `json:"version"`
	GitCommitID string `json:"gitCommitID"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// SplunkApp is the Schema for the splunkapps API
type SplunkApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SplunkAppSpec   `json:"spec,omitempty"`
	Status SplunkAppStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// SplunkAppList contains a list of SplunkApp
type SplunkAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SplunkApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SplunkApp{}, &SplunkAppList{})
}
