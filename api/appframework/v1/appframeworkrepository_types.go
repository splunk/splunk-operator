// Copyright (c) 2018-2024 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppFrameworkRepositorySpec defines the desired state of AppFrameworkRepository
type AppFrameworkRepositorySpec struct {
	// StorageType defines the type of remote storage (s3, blob, gcs)
	// +kubebuilder:validation:Enum=s3;blob;gcs
	StorageType string `json:"storageType"`

	// Provider defines the storage provider (aws, azure, gcp, minio)
	// +kubebuilder:validation:Enum=aws;azure;gcp;minio
	Provider string `json:"provider"`

	// Endpoint is the URL of the remote storage endpoint
	Endpoint string `json:"endpoint"`

	// Region is the storage region (not required for all providers)
	// +optional
	Region string `json:"region,omitempty"`

	// SecretRef is the name of the secret containing storage credentials
	// +optional
	SecretRef string `json:"secretRef,omitempty"`

	// Path is the base path in the storage bucket where apps are located
	Path string `json:"path"`

	// PollInterval defines how often to check for app changes
	// +kubebuilder:default="300s"
	// +optional
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`

	// RetryPolicy defines retry behavior for storage operations
	// +optional
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`

	// DeletionPolicy defines how to handle app deletions
	// +optional
	DeletionPolicy *DeletionPolicy `json:"deletionPolicy,omitempty"`

	// TLS configuration for secure connections
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`
}

// RetryPolicy defines retry behavior for operations
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts
	// +kubebuilder:default=3
	// +optional
	MaxRetries *int32 `json:"maxRetries,omitempty"`

	// BackoffMultiplier is the multiplier for exponential backoff
	// +kubebuilder:default=2
	// +optional
	BackoffMultiplier *float64 `json:"backoffMultiplier,omitempty"`

	// InitialInterval is the initial retry interval
	// +kubebuilder:default="30s"
	// +optional
	InitialInterval *metav1.Duration `json:"initialInterval,omitempty"`

	// MaxInterval is the maximum retry interval
	// +kubebuilder:default="300s"
	// +optional
	MaxInterval *metav1.Duration `json:"maxInterval,omitempty"`
}

// DeletionPolicy defines how app deletions are handled
type DeletionPolicy struct {
	// Enabled controls whether automatic app deletion is enabled
	// +kubebuilder:default=false
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Strategy defines the deletion strategy
	// +kubebuilder:validation:Enum=immediate;graceful;manual
	// +kubebuilder:default="graceful"
	// +optional
	Strategy string `json:"strategy,omitempty"`

	// GracePeriod is the wait time before deletion (for graceful strategy)
	// +kubebuilder:default="300s"
	// +optional
	GracePeriod *metav1.Duration `json:"gracePeriod,omitempty"`

	// Safeguards define protection mechanisms
	// +optional
	Safeguards *DeletionSafeguards `json:"safeguards,omitempty"`

	// RetentionPolicy defines backup and retention behavior
	// +optional
	RetentionPolicy *RetentionPolicy `json:"retentionPolicy,omitempty"`

	// Notifications define alert configurations
	// +optional
	Notifications *NotificationConfig `json:"notifications,omitempty"`
}

// DeletionSafeguards define protection mechanisms for app deletion
type DeletionSafeguards struct {
	// RequireConfirmation lists apps that require manual confirmation
	// +optional
	RequireConfirmation []string `json:"requireConfirmation,omitempty"`

	// ProtectedApps lists apps that cannot be automatically deleted
	// +optional
	ProtectedApps []string `json:"protectedApps,omitempty"`

	// BulkDeletionThreshold prevents deletion of too many apps at once
	// +kubebuilder:default=5
	// +optional
	BulkDeletionThreshold *int32 `json:"bulkDeletionThreshold,omitempty"`

	// BackupBeforeDeletion creates backups before deletion
	// +kubebuilder:default=true
	// +optional
	BackupBeforeDeletion *bool `json:"backupBeforeDeletion,omitempty"`

	// ApprovalWorkflow defines manual approval requirements
	// +optional
	ApprovalWorkflow *ApprovalWorkflow `json:"approvalWorkflow,omitempty"`
}

// ApprovalWorkflow defines manual approval requirements
type ApprovalWorkflow struct {
	// Required indicates if approval is required
	// +kubebuilder:default=false
	Required bool `json:"required"`

	// Approvers lists users or groups who can approve deletions
	// +optional
	Approvers []string `json:"approvers,omitempty"`

	// Timeout is the approval timeout period
	// +kubebuilder:default="24h"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`
}

// RetentionPolicy defines backup and retention behavior
type RetentionPolicy struct {
	// KeepDeletedApps controls whether to keep backups of deleted apps
	// +kubebuilder:default=true
	// +optional
	KeepDeletedApps *bool `json:"keepDeletedApps,omitempty"`

	// RetentionDuration is how long to keep deleted app backups
	// +kubebuilder:default="72h"
	// +optional
	RetentionDuration *metav1.Duration `json:"retentionDuration,omitempty"`

	// BackupLocation is where to store app backups
	// +optional
	BackupLocation string `json:"backupLocation,omitempty"`

	// CompressBackups controls whether to compress backup files
	// +kubebuilder:default=true
	// +optional
	CompressBackups *bool `json:"compressBackups,omitempty"`

	// CleanupSchedule defines when to clean up old backups
	// +optional
	CleanupSchedule *CleanupSchedule `json:"cleanupSchedule,omitempty"`
}

// CleanupSchedule defines backup cleanup timing
type CleanupSchedule struct {
	// Enabled controls whether automatic cleanup is enabled
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Schedule is a cron expression for cleanup timing
	// +kubebuilder:default="0 2 * * *"
	Schedule string `json:"schedule"`
}

// NotificationConfig defines alert configurations
type NotificationConfig struct {
	// Webhook URL for deletion notifications
	// +optional
	Webhook string `json:"webhook,omitempty"`

	// Slack channel for notifications
	// +optional
	Slack string `json:"slack,omitempty"`

	// Email addresses for notifications
	// +optional
	Email []string `json:"email,omitempty"`

	// Events defines which events trigger notifications
	// +optional
	Events []string `json:"events,omitempty"`

	// Templates define custom notification templates
	// +optional
	Templates map[string]string `json:"templates,omitempty"`
}

// TLSConfig defines TLS configuration
type TLSConfig struct {
	// Enabled controls whether TLS is enabled
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// InsecureSkipVerify skips certificate verification
	// +kubebuilder:default=false
	// +optional
	InsecureSkipVerify *bool `json:"insecureSkipVerify,omitempty"`

	// CACertSecret is the name of secret containing CA certificates
	// +optional
	CACertSecret string `json:"caCertSecret,omitempty"`
}

// AppFrameworkRepositoryStatus defines the observed state of AppFrameworkRepository
type AppFrameworkRepositoryStatus struct {
	// Phase represents the current phase of the repository
	// +kubebuilder:validation:Enum=Pending;Ready;Error;Syncing
	Phase string `json:"phase,omitempty"`

	// LastSyncTime is when the repository was last successfully synced
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// AvailableApps lists apps currently available in the repository
	// +optional
	AvailableApps []RepositoryApp `json:"availableApps,omitempty"`

	// DeletionHistory tracks recent app deletions
	// +optional
	DeletionHistory []DeletionRecord `json:"deletionHistory,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// SyncErrors tracks recent synchronization errors
	// +optional
	SyncErrors []SyncError `json:"syncErrors,omitempty"`

	// Statistics provide repository usage metrics
	// +optional
	Statistics *RepositoryStatistics `json:"statistics,omitempty"`
}

// RepositoryApp represents an app in the repository
type RepositoryApp struct {
	// Name is the app filename
	Name string `json:"name"`

	// Checksum is the app file checksum
	Checksum string `json:"checksum"`

	// Size is the app file size in bytes
	Size int64 `json:"size"`

	// LastModified is when the app was last modified
	LastModified metav1.Time `json:"lastModified"`

	// Path is the full path to the app in the repository
	Path string `json:"path"`
}

// DeletionRecord tracks an app deletion event
type DeletionRecord struct {
	// AppName is the name of the deleted app
	AppName string `json:"appName"`

	// DeletedAt is when the deletion was detected
	DeletedAt metav1.Time `json:"deletedAt"`

	// Reason describes why the app was deleted
	Reason string `json:"reason"`

	// BackupLocation is where the app backup is stored
	// +optional
	BackupLocation string `json:"backupLocation,omitempty"`

	// RollbackAvailable indicates if rollback is possible
	RollbackAvailable bool `json:"rollbackAvailable"`
}

// SyncError represents a synchronization error
type SyncError struct {
	// Timestamp when the error occurred
	Timestamp metav1.Time `json:"timestamp"`

	// Message describes the error
	Message string `json:"message"`

	// RetryCount is the number of retry attempts
	RetryCount int32 `json:"retryCount"`
}

// RepositoryStatistics provide usage metrics
type RepositoryStatistics struct {
	// TotalApps is the total number of apps in the repository
	TotalApps int32 `json:"totalApps"`

	// TotalSize is the total size of all apps in bytes
	TotalSize int64 `json:"totalSize"`

	// LastSyncDuration is how long the last sync took
	LastSyncDuration metav1.Duration `json:"lastSyncDuration"`

	// SyncSuccessRate is the percentage of successful syncs
	SyncSuccessRate float64 `json:"syncSuccessRate"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.endpoint"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Apps",type="integer",JSONPath=".status.statistics.totalApps"
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=".status.lastSyncTime"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AppFrameworkRepository is the Schema for the appframeworkrepositories API
type AppFrameworkRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppFrameworkRepositorySpec   `json:"spec,omitempty"`
	Status AppFrameworkRepositoryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppFrameworkRepositoryList contains a list of AppFrameworkRepository
type AppFrameworkRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppFrameworkRepository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppFrameworkRepository{}, &AppFrameworkRepositoryList{})
}
