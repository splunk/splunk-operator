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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppFrameworkSyncSpec defines the desired state of AppFrameworkSync
type AppFrameworkSyncSpec struct {
	// RepositoryRef references the AppFrameworkRepository to sync from
	RepositoryRef RepositoryReference `json:"repositoryRef"`

	// TargetRef references the Splunk CR to sync apps to
	TargetRef TargetReference `json:"targetRef"`

	// AppSources define the app sources to sync
	AppSources []AppSourceSpec `json:"appSources"`

	// SyncPolicy defines synchronization behavior
	// +optional
	SyncPolicy *SyncPolicy `json:"syncPolicy,omitempty"`

	// DeletionPolicy overrides repository deletion policy for this sync
	// +optional
	DeletionPolicy *DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// RepositoryReference references an AppFrameworkRepository
type RepositoryReference struct {
	// Name is the name of the AppFrameworkRepository
	Name string `json:"name"`

	// Namespace is the namespace of the AppFrameworkRepository
	// If empty, uses the same namespace as the AppFrameworkSync
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// TargetReference references a Splunk CR
type TargetReference struct {
	// APIVersion of the target Splunk CR
	APIVersion string `json:"apiVersion"`

	// Kind of the target Splunk CR (Standalone, ClusterManager, etc.)
	Kind string `json:"kind"`

	// Name of the target Splunk CR
	Name string `json:"name"`

	// Namespace of the target Splunk CR
	// If empty, uses the same namespace as the AppFrameworkSync
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// AppSourceSpec defines an app source configuration
type AppSourceSpec struct {
	// Name is a logical name for this app source
	Name string `json:"name"`

	// Location is the path relative to the repository base path
	Location string `json:"location"`

	// Scope defines where apps are installed (local, cluster, premiumApps)
	// +kubebuilder:validation:Enum=local;cluster;clusterWithPreConfig;premiumApps
	Scope string `json:"scope"`

	// VolumeName references the volume to use (if different from default)
	// +optional
	VolumeName string `json:"volumeName,omitempty"`

	// IncludePatterns define which apps to include (glob patterns)
	// +optional
	IncludePatterns []string `json:"includePatterns,omitempty"`

	// ExcludePatterns define which apps to exclude (glob patterns)
	// +optional
	ExcludePatterns []string `json:"excludePatterns,omitempty"`

	// DeletionPolicy overrides the default deletion policy for this source
	// +optional
	DeletionPolicy *DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// SyncPolicy defines synchronization behavior
type SyncPolicy struct {
	// Automatic controls whether sync happens automatically
	// +kubebuilder:default=true
	// +optional
	Automatic *bool `json:"automatic,omitempty"`

	// Prune controls whether to remove apps not in the repository
	// +kubebuilder:default=false
	// +optional
	Prune *bool `json:"prune,omitempty"`

	// SyncTimeout is the maximum time to wait for sync completion
	// +kubebuilder:default="1800s"
	// +optional
	SyncTimeout *metav1.Duration `json:"syncTimeout,omitempty"`

	// RetryPolicy defines retry behavior for sync operations
	// +optional
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`

	// Hooks define pre/post sync operations
	// +optional
	Hooks *SyncHooks `json:"hooks,omitempty"`
}

// SyncHooks define operations to run before/after sync
type SyncHooks struct {
	// PreSync operations to run before sync
	// +optional
	PreSync []SyncHook `json:"preSync,omitempty"`

	// PostSync operations to run after sync
	// +optional
	PostSync []SyncHook `json:"postSync,omitempty"`
}

// SyncHook defines a single hook operation
type SyncHook struct {
	// Name is a descriptive name for the hook
	Name string `json:"name"`

	// Command to execute
	Command []string `json:"command"`

	// Args for the command
	// +optional
	Args []string `json:"args,omitempty"`

	// Env defines environment variables
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// FailurePolicy defines what to do if the hook fails
	// +kubebuilder:validation:Enum=Ignore;Fail
	// +kubebuilder:default="Fail"
	// +optional
	FailurePolicy string `json:"failurePolicy,omitempty"`
}

// AppFrameworkSyncStatus defines the observed state of AppFrameworkSync
type AppFrameworkSyncStatus struct {
	// Phase represents the current sync phase
	// +kubebuilder:validation:Enum=Pending;Syncing;Synced;Error;Degraded
	Phase string `json:"phase,omitempty"`

	// LastSyncTime is when sync was last attempted
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// LastSuccessfulSyncTime is when sync last succeeded
	// +optional
	LastSuccessfulSyncTime *metav1.Time `json:"lastSuccessfulSyncTime,omitempty"`

	// SyncedApps lists apps currently synced
	// +optional
	SyncedApps []SyncedApp `json:"syncedApps,omitempty"`

	// PendingDeletions lists apps scheduled for deletion
	// +optional
	PendingDeletions []PendingDeletion `json:"pendingDeletions,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// SyncStatistics provide sync metrics
	// +optional
	SyncStatistics *SyncStatistics `json:"syncStatistics,omitempty"`

	// ActiveJobs lists currently running jobs
	// +optional
	ActiveJobs []ActiveJob `json:"activeJobs,omitempty"`
}

// SyncedApp represents a successfully synced app
type SyncedApp struct {
	// Name is the app name
	Name string `json:"name"`

	// Source is the app source name
	Source string `json:"source"`

	// Checksum is the current app checksum
	Checksum string `json:"checksum"`

	// Status is the current deployment status
	// +kubebuilder:validation:Enum=Pending;Installing;Installed;Updating;Failed
	Status string `json:"status"`

	// LastUpdated is when the app was last updated
	LastUpdated metav1.Time `json:"lastUpdated"`

	// TargetPods lists pods where the app is installed
	// +optional
	TargetPods []string `json:"targetPods,omitempty"`

	// JobRef references the job handling this app
	// +optional
	JobRef *corev1.ObjectReference `json:"jobRef,omitempty"`
}

// PendingDeletion represents an app scheduled for deletion
type PendingDeletion struct {
	// AppName is the name of the app to be deleted
	AppName string `json:"appName"`

	// Source is the app source name
	Source string `json:"source"`

	// ScheduledAt is when deletion was scheduled
	ScheduledAt metav1.Time `json:"scheduledAt"`

	// GracePeriodExpiry is when the grace period expires
	// +optional
	GracePeriodExpiry *metav1.Time `json:"gracePeriodExpiry,omitempty"`

	// Reason describes why deletion was scheduled
	Reason string `json:"reason"`

	// RequiresApproval indicates if manual approval is needed
	RequiresApproval bool `json:"requiresApproval"`

	// ApprovalStatus tracks approval state
	// +optional
	ApprovalStatus *ApprovalStatus `json:"approvalStatus,omitempty"`
}

// ApprovalStatus tracks manual approval state
type ApprovalStatus struct {
	// Status of the approval request
	// +kubebuilder:validation:Enum=Pending;Approved;Rejected;Expired
	Status string `json:"status"`

	// RequestedAt is when approval was requested
	RequestedAt metav1.Time `json:"requestedAt"`

	// ApprovedBy is who approved the request
	// +optional
	ApprovedBy string `json:"approvedBy,omitempty"`

	// ApprovedAt is when the request was approved
	// +optional
	ApprovedAt *metav1.Time `json:"approvedAt,omitempty"`

	// Comments from the approver
	// +optional
	Comments string `json:"comments,omitempty"`
}

// SyncStatistics provide sync operation metrics
type SyncStatistics struct {
	// TotalApps is the total number of apps being managed
	TotalApps int32 `json:"totalApps"`

	// InstalledApps is the number of successfully installed apps
	InstalledApps int32 `json:"installedApps"`

	// FailedApps is the number of apps with installation failures
	FailedApps int32 `json:"failedApps"`

	// PendingApps is the number of apps pending installation
	PendingApps int32 `json:"pendingApps"`

	// LastSyncDuration is how long the last sync took
	// +optional
	LastSyncDuration *metav1.Duration `json:"lastSyncDuration,omitempty"`

	// SyncSuccessRate is the percentage of successful syncs
	SyncSuccessRate float64 `json:"syncSuccessRate"`
}

// ActiveJob represents a currently running job
type ActiveJob struct {
	// Name is the job name
	Name string `json:"name"`

	// Type is the job type (download, install, uninstall)
	Type string `json:"type"`

	// AppName is the app being processed
	AppName string `json:"appName"`

	// StartedAt is when the job started
	StartedAt metav1.Time `json:"startedAt"`

	// Phase is the current job phase
	Phase string `json:"phase"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Repository",type="string",JSONPath=".spec.repositoryRef.name"
// +kubebuilder:printcolumn:name="Target",type="string",JSONPath=".spec.targetRef.name"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Apps",type="integer",JSONPath=".status.syncStatistics.totalApps"
// +kubebuilder:printcolumn:name="Installed",type="integer",JSONPath=".status.syncStatistics.installedApps"
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=".status.lastSuccessfulSyncTime"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AppFrameworkSync is the Schema for the appframeworksyncs API
type AppFrameworkSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppFrameworkSyncSpec   `json:"spec,omitempty"`
	Status AppFrameworkSyncStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppFrameworkSyncList contains a list of AppFrameworkSync
type AppFrameworkSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppFrameworkSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppFrameworkSync{}, &AppFrameworkSyncList{})
}
