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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	// IngestorClusterPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	IngestorClusterPausedAnnotation = "ingestorcluster.enterprise.splunk.com/paused"
)

// IngestorClusterSpec defines the spec of Ingestor Cluster pods
type IngestorClusterSpec struct {
	// Common SPlunk spec
	CommonSplunkSpec `json:",inline"`

	// Number of ingestion pods
	Replicas int32 `json:"replicas"`

	// Splunk Enterprise app repository that specifies remote app location and scope for Splunk app management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`

	// Push Bus spec
	PushBus PushBusSpec `json:"pushBus"`

	// Service account name
	ServiceAccountName string `json:"serviceAccountName"`
}

// Helper types
// Only SQS as of now
type PushBusSpec struct {
	Type string `json:"type"`

	SQS SQSSpec `json:"sqs"`

	PipelineConfig PipelineConfigSpec `json:"pipelineConfig"`
}

type SQSSpec struct {
	QueueName string `json:"queueName"`

	AuthRegion string `json:"authRegion"`

	Endpoint string `json:"endpoint"`
}

type PipelineConfigSpec struct {
	RemoteQueueRuleset bool `json:"remoteQueueRuleset"`

	RuleSet bool `json:"ruleSet"`

	RemoteQueueTyping bool `json:"remoteQueueTyping"`

	RemoteQueueOutput bool `json:"remoteQueueOutput"`

	Typing bool `json:"typing"`

	IndexerPipe bool `json:"indexerPipe"`
}

// IngestorClusterStatus defines the observed state of Ingestor Cluster pods
type IngestorClusterStatus struct {
	// Phase of the ingestion pods
	Phase Phase `json:"phase"`

	// Number of desired ingestion pods
	Replicas int32 `json:"replicas"`

	// Number of ready ingestion pods
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
}

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

// DeepCopyObject implements common.MetaObject.
func (ic *IngestorCluster) DeepCopyObject() runtime.Object {
	panic("unimplemented")
}

// GetAnnotations implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetAnnotations of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetAnnotations() map[string]string {
	panic("unimplemented")
}

// GetCreationTimestamp implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetCreationTimestamp of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetCreationTimestamp() metav1.Time {
	panic("unimplemented")
}

// GetDeletionGracePeriodSeconds implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetDeletionGracePeriodSeconds of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetDeletionGracePeriodSeconds() *int64 {
	panic("unimplemented")
}

// GetDeletionTimestamp implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetDeletionTimestamp of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetDeletionTimestamp() *metav1.Time {
	panic("unimplemented")
}

// GetFinalizers implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetFinalizers of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetFinalizers() []string {
	panic("unimplemented")
}

// GetGenerateName implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetGenerateName of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetGenerateName() string {
	panic("unimplemented")
}

// GetGeneration implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetGeneration of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetGeneration() int64 {
	panic("unimplemented")
}

// GetLabels implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetLabels of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetLabels() map[string]string {
	panic("unimplemented")
}

// GetManagedFields implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetManagedFields of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetManagedFields() []metav1.ManagedFieldsEntry {
	panic("unimplemented")
}

// GetName implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetName of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetName() string {
	panic("unimplemented")
}

// GetNamespace implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetNamespace of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetNamespace() string {
	panic("unimplemented")
}

// GetObjectKind implements common.MetaObject.
// Subtle: this method shadows the method (TypeMeta).GetObjectKind of IngestorCluster.TypeMeta.
func (ic *IngestorCluster) GetObjectKind() schema.ObjectKind {
	panic("unimplemented")
}

// GetObjectMeta implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetObjectMeta of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetObjectMeta() metav1.Object {
	panic("unimplemented")
}

// GetOwnerReferences implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetOwnerReferences of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetOwnerReferences() []metav1.OwnerReference {
	panic("unimplemented")
}

// GetResourceVersion implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetResourceVersion of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetResourceVersion() string {
	panic("unimplemented")
}

// GetSelfLink implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetSelfLink of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetSelfLink() string {
	panic("unimplemented")
}

// GetUID implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).GetUID of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) GetUID() types.UID {
	panic("unimplemented")
}

// GroupVersionKind implements common.MetaObject.
// Subtle: this method shadows the method (TypeMeta).GroupVersionKind of IngestorCluster.TypeMeta.
func (ic *IngestorCluster) GroupVersionKind() schema.GroupVersionKind {
	panic("unimplemented")
}

// SetAnnotations implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetAnnotations of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetAnnotations(annotations map[string]string) {
	panic("unimplemented")
}

// SetCreationTimestamp implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetCreationTimestamp of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetCreationTimestamp(timestamp metav1.Time) {
	panic("unimplemented")
}

// SetDeletionGracePeriodSeconds implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetDeletionGracePeriodSeconds of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetDeletionGracePeriodSeconds(*int64) {
	panic("unimplemented")
}

// SetDeletionTimestamp implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetDeletionTimestamp of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetDeletionTimestamp(timestamp *metav1.Time) {
	panic("unimplemented")
}

// SetFinalizers implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetFinalizers of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetFinalizers(finalizers []string) {
	panic("unimplemented")
}

// SetGenerateName implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetGenerateName of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetGenerateName(name string) {
	panic("unimplemented")
}

// SetGeneration implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetGeneration of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetGeneration(generation int64) {
	panic("unimplemented")
}

// SetGroupVersionKind implements common.MetaObject.
// Subtle: this method shadows the method (TypeMeta).SetGroupVersionKind of IngestorCluster.TypeMeta.
func (ic *IngestorCluster) SetGroupVersionKind(kind schema.GroupVersionKind) {
	panic("unimplemented")
}

// SetLabels implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetLabels of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetLabels(labels map[string]string) {
	panic("unimplemented")
}

// SetManagedFields implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetManagedFields of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetManagedFields(managedFields []metav1.ManagedFieldsEntry) {
	panic("unimplemented")
}

// SetName implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetName of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetName(name string) {
	panic("unimplemented")
}

// SetNamespace implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetNamespace of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetNamespace(namespace string) {
	panic("unimplemented")
}

// SetOwnerReferences implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetOwnerReferences of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetOwnerReferences([]metav1.OwnerReference) {
	panic("unimplemented")
}

// SetResourceVersion implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetResourceVersion of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetResourceVersion(version string) {
	panic("unimplemented")
}

// SetSelfLink implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetSelfLink of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetSelfLink(selfLink string) {
	panic("unimplemented")
}

// SetUID implements common.MetaObject.
// Subtle: this method shadows the method (ObjectMeta).SetUID of IngestorCluster.ObjectMeta.
func (ic *IngestorCluster) SetUID(uid types.UID) {
	panic("unimplemented")
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
