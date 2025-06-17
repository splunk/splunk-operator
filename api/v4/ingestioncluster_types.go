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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	// IngestionClusterPausedAnnotation is the annotation that pauses the reconciliation (triggers
	// an immediate requeue)
	IngestionClusterPausedAnnotation = "ingestioncluster.enterprise.splunk.com/paused"
)

// IngestionClusterSpec defines the desired state of IngestionCluster
type IngestionClusterSpec struct {
	CommonSplunkSpec `json:",inline"`

	// Number of standalone pods
	Replicas int32 `json:"replicas"`

	PushBus BusSpec `json:"pushBus,omitempty"`

	// Splunk Enterprise App repository. Specifies remote App location and scope for Splunk App management
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`
}

// BusType enumerates the supported message bus providers.
// +kubebuilder:default="sqs"
type BusType string

const (
	BusTypeSQS        BusType = "sqs"
	BusTypeKafka      BusType = "kafka"
	BusTypePubSub     BusType = "pubsub"
	BusTypeServiceBus BusType = "servicebus"
)

// BusSpec is the union of all supported bus provider configurations.
type BusSpec struct {
	// Type of bus – drives which sub-config is populated
	// +kubebuilder:validation:Enum=sqs;kafka;pubsub;servicebus
	// +kubebuilder:default=sqs
	Type BusType `json:"type"`

	// AWS SQS configuration. Required if Type == BusTypeSQS
	// +optional
	SQS *SQSBusConfig `json:"sqs,omitempty"`

	// Apache Kafka configuration. Required if Type == BusTypeKafka
	// +optional
	Kafka *KafkaBusConfig `json:"kafka,omitempty"`

	// GCP Pub/Sub configuration. Required if Type == BusTypePubSub
	// +optional
	PubSub *PubSubBusConfig `json:"pubsub,omitempty"`

	// Azure Service Bus configuration. Required if Type == BusTypeServiceBus
	// +optional
	ServiceBus *ServiceBusBusConfig `json:"servicebus,omitempty"`
}

// SQSBusConfig holds AWS SQS-specific settings.
type SQSBusConfig struct {
	QueueName string `json:"queueName"`
	// how to encode the messages on the wire (e.g. s2s, json)
	// +optional
	// +kubebuilder:default="s2s"
	EncodingFormat    string            `json:"encodingFormat,omitempty"`
	AuthRegion        string            `json:"authRegion"`
	Endpoint          string            `json:"endpoint"`
	AccessKeyRef      SecretKeyRef      `json:"accessKeyRef"`
	SecretKeyRef      SecretKeyRef      `json:"secretKeyRef"`
	LargeMessageStore LargeMessageStore `json:"largeMessageStore,omitempty"`
	// +kubebuilder:default=""
	DeadLetterQueueName string `json:"deadLetterQueueName,omitempty"`
	// +kubebuilder:default=3
	MaxRetriesPerPart int `json:"maxRetriesPerPart,omitempty"`
	// +kubebuilder:default="max_count"
	RetryPolicy string `json:"retryPolicy,omitempty"`
	// +kubebuilder:default="4s"
	SendInterval string `json:"sendInterval,omitempty"`
}

// KafkaBusConfig holds Apache Kafka-specific settings.
type KafkaBusConfig struct {
	Brokers []string `json:"brokers"`
	Topic   string   `json:"topic"`
	// +optional
	TLSSecretRef *SecretKeyRef `json:"tlsSecretRef,omitempty"`
	// +optional
	SASLSecretRef *SecretKeyRef `json:"saslSecretRef,omitempty"`
	// add further Kafka parameters as needed
}

// PubSubBusConfig holds GCP Pub/Sub-specific settings.
type PubSubBusConfig struct {
	Project      string         `json:"project"`
	Topic        string         `json:"topic"`
	Subscription string         `json:"subscription"`
	Auth         PubSubAuthSpec `json:"auth"`
}

// PubSubAuthSpec defines authentication for Pub/Sub.
type PubSubAuthSpec struct {
	// +kubebuilder:validation:Enum=keyFile
	// +kubebuilder:default="keyFile"
	Type          string       `json:"type"`
	KeyFileSecret SecretKeyRef `json:"keyFileSecret"`
}

// ServiceBusBusConfig holds Azure Service Bus-specific settings.
type ServiceBusBusConfig struct {
	Namespace string `json:"namespace"`
	QueueName string `json:"queueName"`
	// +kubebuilder:default="Amqp"
	TransportType string         `json:"transportType,omitempty"`
	Auth          ServiceBusAuth `json:"auth"`
}

// ServiceBusAuth defines authentication for Azure Service Bus.
type ServiceBusAuth struct {
	// +kubebuilder:validation:Enum=connectionString
	// +kubebuilder:default="connectionString"
	Type                   string       `json:"type"`
	ConnectionStringSecret SecretKeyRef `json:"connectionStringSecret"`
}

// SecretKeyRef is a reference to a key within a Kubernetes Secret.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// LargeMessageStore holds S3 or blob store settings for large messages.
type LargeMessageStore struct {
	// +kubebuilder:default=""
	Endpoint string `json:"endpoint,omitempty"`
	// +kubebuilder:default=""
	Path string `json:"path,omitempty"`
}

// IngestionClusterStatus defines the observed state of IngestionCluster
type IngestionClusterStatus struct {
	// current phase of the standalone instances
	Phase Phase `json:"phase"`

	// number of desired standalone instances
	Replicas int32 `json:"replicas"`

	// current number of ready standalone instances
	ReadyReplicas int32 `json:"readyReplicas"`

	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector"`

	// Resource Revision tracker
	ResourceRevMap map[string]string `json:"resourceRevMap"`

	// App Framework Context
	AppContext AppDeploymentContext `json:"appContext"`

	// Telemetry App installation flag
	TelAppInstalled bool `json:"telAppInstalled"`

	// Auxillary message describing CR status
	Message string `json:"message"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Standalone is the Schema for a Splunk Enterprise ingestion cluster instances.
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:path=ingestionclusters,scope=Namespaced,shortName=ing
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Status of ingestion clsuter instances"
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.replicas",description="Number of desired ingest cluster instances"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyReplicas",description="Current number of ready ingestion cluster instances"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of ingestion cluster resource"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.message",description="Auxillary message describing CR status"
// +kubebuilder:storageversion
type IngestionCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IngestionClusterSpec   `json:"spec,omitempty"`
	Status IngestionClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IngestionClusterList contains a list of IngestionCluster
type IngestionClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IngestionCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IngestionCluster{}, &IngestionClusterList{})
}

// NewEvent creates a new event associated with the object and ready
// to be published to the kubernetes API.
func (standln *IngestionCluster) NewEvent(eventType, reason, message string) corev1.Event {
	t := metav1.Now()
	return corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: reason + "-",
			Namespace:    standln.ObjectMeta.Namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "Standalone",
			Namespace:  standln.Namespace,
			Name:       standln.Name,
			UID:        standln.UID,
			APIVersion: GroupVersion.String(),
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "splunk-ingestion-cluster-controller",
		},
		FirstTimestamp:      t,
		LastTimestamp:       t,
		Count:               1,
		Type:                eventType,
		ReportingController: "enterprise.splunk.com/ingestion-cluster-controller",
	}
}
