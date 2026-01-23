/*
Copyright 2025.

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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// IngestorClusterPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	IngestorClusterPausedAnnotation = "ingestorcluster.enterprise.splunk.com/paused"
)

// +kubebuilder:validation:XValidation:rule="self.queueRef == oldSelf.queueRef",message="queueRef is immutable once created"
// +kubebuilder:validation:XValidation:rule="self.objectStorageRef == oldSelf.objectStorageRef",message="objectStorageRef is immutable once created"
// IngestorClusterSpec defines the spec of Ingestor Cluster
type IngestorClusterSpec struct {
	// Common Splunk spec
	CommonSplunkSpec `json:",inline"`

	// Number of ingestor pods
	Replicas int32 `json:"replicas"`

	// Splunk Enterprise app repository that specifies remote app location and scope for Splunk app management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`

	// +kubebuilder:validation:Required
	// Queue reference
	QueueRef corev1.ObjectReference `json:"queueRef"`

	// +kubebuilder:validation:Required
	// Object Storage reference
	ObjectStorageRef corev1.ObjectReference `json:"objectStorageRef"`
}

// IngestorClusterStatus defines the observed state of Ingestor Cluster
type IngestorClusterStatus struct {
	// Phase of the ingestor pods
	Phase Phase `json:"phase"`

	// Number of desired ingestor pods
	Replicas int32 `json:"replicas"`

	// Number of ready ingestor pods
	ReadyReplicas int32 `json:"readyReplicas"`

	// Selector for pods used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Resource revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// App Framework context
	AppContext AppDeploymentContext `json:"appContext"`

	// Telemetry App installation flag
	TelAppInstalled bool `json:"telAppInstalled"`

	// Auxillary message describing CR status
	Message string `json:"message"`

	// Queue and bucket access secret version
	QueueBucketAccessSecretVersion string `json:"queueBucketAccessSecretVersion,omitempty"`

	// Rolling restart status
	RestartStatus RestartStatus `json:"restartStatus,omitempty"`
}

// RestartStatus tracks the state of rolling restart operations
type RestartStatus struct {
	// Phase of restart operation
	Phase RestartPhase `json:"phase,omitempty"`

	// Human-readable message describing current restart state
	// Examples:
	// - "2/3 pods need restart (server.conf modified)"
	// - "Restarting pod 47 (48/95)"
	// - "Configuration reloaded successfully on all 100 pods, no restarts needed"
	Message string `json:"message,omitempty"`

	// Total number of pods in the cluster
	TotalPods int32 `json:"totalPods,omitempty"`

	// Number of pods that need restart
	PodsNeedingRestart int32 `json:"podsNeedingRestart,omitempty"`

	// Number of pods successfully restarted in current operation
	PodsRestarted int32 `json:"podsRestarted,omitempty"`

	// Last time we checked if restart was required
	LastCheckTime *metav1.Time `json:"lastCheckTime,omitempty"`

	// Last time a restart operation started (used for timeout detection)
	LastRestartTime *metav1.Time `json:"lastRestartTime,omitempty"`
}

// RestartPhase represents the phase of a restart operation
type RestartPhase string

const (
	// RestartPhaseNone indicates no restart is needed or in progress
	RestartPhaseNone RestartPhase = ""

	// RestartPhasePending indicates restart is needed but not yet started
	RestartPhasePending RestartPhase = "Pending"

	// RestartPhaseInProgress indicates restart operation is currently running
	RestartPhaseInProgress RestartPhase = "InProgress"

	// RestartPhaseCompleted indicates restart operation completed successfully
	RestartPhaseCompleted RestartPhase = "Completed"

	// RestartPhaseFailed indicates restart operation failed
	RestartPhaseFailed RestartPhase = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// IngestorCluster is the Schema for a Splunk Enterprise ingestor cluster pods
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=ingestorclusters,scope=Namespaced,shortName=ing
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of ingestor cluster pods"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Number of desired ingestor cluster pods"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready ingestor cluster pods"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of ingestor cluster resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
// +kubebuilder:storageversion

// IngestorCluster is the Schema for the ingestorclusters API
type IngestorCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	Spec   IngestorClusterSpec   `json:"spec"`
	Status IngestorClusterStatus `json:"status,omitempty,omitzero"`
}

// DeepCopyObject implements runtime.Object
func (in *IngestorCluster) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// +kubebuilder:object:root=true

// IngestorClusterList contains a list of IngestorCluster
type IngestorClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IngestorCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IngestorCluster{}, &IngestorClusterList{})
}

// NewEvent creates a new event associated with the object and ready
// to be published to Kubernetes API
func (ic *IngestorCluster) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    ic.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "IngestorCluster",
			Namespace:  ic.Namespace,
			Name:       ic.Name,
			UID:        ic.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-ingestorcluster-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/ingestorcluster-controller",
	}
}
