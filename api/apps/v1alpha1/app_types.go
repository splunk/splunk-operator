// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// AppPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	AppPausedAnnotation = "apps.splunk.com/paused"
)

// AppTargetRef defines the target environment the app should bind to.
type AppTargetRef struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// AppPackageSpec defines the package location within the source.
type AppPackageSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Path string `json:"path"`
}

// AppSourceSpec defines the app source details.
type AppSourceRef struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// AppSpec defines the desired state of App.
type AppSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	AppID string `json:"appID"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// +kubebuilder:validation:Required
	TargetRef AppTargetRef `json:"targetRef"`

	// +kubebuilder:validation:Required
	SourceRef AppSourceRef `json:"sourceRef"`

	// +kubebuilder:validation:Required
	Package AppPackageSpec `json:"package"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Scope string `json:"scope"`
}

// AppStatus defines the observed state of App.
type AppStatus struct {
	// Phase of the app resource
	Phase string `json:"phase,omitempty"`

	// Auxillary message describing CR status
	Message string `json:"message,omitempty"`

	// InstalledVersion is the app version installed on target
	InstalledVersion string `json:"installedVersion,omitempty"`

	// Artifact tracks the resolved app package details
	Artifact *AppArtifactStatus `json:"artifact,omitempty"`

	// ObservedGeneration tracks the latest reconciled generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the app
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// AppArtifactStatus defines resolved app artifact details.
type AppArtifactStatus struct {
	// ResolvedPath is the resolved artifact path within the source
	ResolvedPath string `json:"resolvedPath,omitempty"`

	// Etag is the artifact hash or ETag used for change detection
	Etag string `json:"etag,omitempty"`

	// LastFetchedTime is the last time the artifact was fetched
	LastFetchedTime metav1.Time `json:"lastFetchedTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// App is the Schema for the apps API.
// +k8s:openapi-gen=true
// +kubebuilder:resource:path=apps,scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of app"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of app resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
// +kubebuilder:storageversion
type App struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   AppSpec   `json:"spec"`
	Status AppStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// AppList contains a list of App.
type AppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []App `json:"items"`
}

func init() {
	SchemeBuilder.Register(&App{}, &AppList{})
}
