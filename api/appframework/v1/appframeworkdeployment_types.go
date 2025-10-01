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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppFrameworkDeploymentSpec defines the desired state of AppFrameworkDeployment
type AppFrameworkDeploymentSpec struct {
	// AppName is the name of the app to deploy
	AppName string `json:"appName"`

	// AppSource is the source where the app is located
	AppSource string `json:"appSource"`

	// TargetPods lists the pods where the app should be deployed
	TargetPods []string `json:"targetPods"`

	// Operation defines what operation to perform
	// +kubebuilder:validation:Enum=install;update;uninstall
	Operation string `json:"operation"`

	// Scope defines the installation scope
	// +kubebuilder:validation:Enum=local;cluster;clusterWithPreConfig;premiumApps
	// +optional
	Scope string `json:"scope,omitempty"`

	// Priority defines deployment priority (higher numbers = higher priority)
	// +kubebuilder:default=0
	// +optional
	Priority *int32 `json:"priority,omitempty"`

	// DeletionPolicy defines deletion behavior (for uninstall operations)
	// +optional
	DeletionPolicy *DeletionPolicy `json:"deletionPolicy,omitempty"`

	// GracePeriod is the wait time before executing the operation
	// +optional
	GracePeriod *metav1.Duration `json:"gracePeriod,omitempty"`

	// ForceDelete bypasses safety checks (use with caution)
	// +kubebuilder:default=false
	// +optional
	ForceDelete *bool `json:"forceDelete,omitempty"`

	// JobTemplate defines the job specification for this deployment
	// +optional
	JobTemplate *JobTemplateSpec `json:"jobTemplate,omitempty"`

	// DeletionConditions define conditions that must be met before deletion
	// +optional
	DeletionConditions []DeletionCondition `json:"deletionConditions,omitempty"`

	// RollbackPolicy defines rollback behavior
	// +optional
	RollbackPolicy *RollbackPolicy `json:"rollbackPolicy,omitempty"`

	// Dependencies define apps that must be installed first
	// +optional
	Dependencies []AppDependency `json:"dependencies,omitempty"`
}

// JobTemplateSpec defines the job template for app operations
type JobTemplateSpec struct {
	// Spec is the job specification
	Spec batchv1.JobSpec `json:"spec"`

	// Labels to apply to the job
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations to apply to the job
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// DeletionCondition defines a condition that must be met before deletion
type DeletionCondition struct {
	// Type is the condition type
	Type string `json:"type"`

	// CheckCommand is the command to run to check the condition
	CheckCommand []string `json:"checkCommand"`

	// ExpectedResult is what the command should return for the condition to pass
	ExpectedResult string `json:"expectedResult"`

	// Timeout is the maximum time to wait for the condition
	// +kubebuilder:default="300s"
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// FailurePolicy defines what to do if the condition fails
	// +kubebuilder:validation:Enum=Ignore;Fail;Retry
	// +kubebuilder:default="Fail"
	// +optional
	FailurePolicy string `json:"failurePolicy,omitempty"`
}

// RollbackPolicy defines rollback behavior
type RollbackPolicy struct {
	// Enabled controls whether rollback is available
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// RetentionPeriod is how long to keep rollback data
	// +kubebuilder:default="72h"
	// +optional
	RetentionPeriod *metav1.Duration `json:"retentionPeriod,omitempty"`

	// BackupLocation is where to store rollback data
	// +optional
	BackupLocation string `json:"backupLocation,omitempty"`

	// AutoRollback defines automatic rollback conditions
	// +optional
	AutoRollback *AutoRollbackPolicy `json:"autoRollback,omitempty"`
}

// AutoRollbackPolicy defines automatic rollback conditions
type AutoRollbackPolicy struct {
	// Enabled controls whether auto-rollback is enabled
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// FailureThreshold is the number of failures before auto-rollback
	// +kubebuilder:default=3
	// +optional
	FailureThreshold *int32 `json:"failureThreshold,omitempty"`

	// TimeWindow is the time window to count failures
	// +kubebuilder:default="1h"
	// +optional
	TimeWindow *metav1.Duration `json:"timeWindow,omitempty"`
}

// AppDependency defines a dependency on another app
type AppDependency struct {
	// Name is the name of the dependent app
	Name string `json:"name"`

	// Source is the source of the dependent app
	// +optional
	Source string `json:"source,omitempty"`

	// MinVersion is the minimum required version
	// +optional
	MinVersion string `json:"minVersion,omitempty"`

	// Required indicates if this dependency is required
	// +kubebuilder:default=true
	// +optional
	Required *bool `json:"required,omitempty"`
}

// AppFrameworkDeploymentStatus defines the observed state of AppFrameworkDeployment
type AppFrameworkDeploymentStatus struct {
	// Phase represents the current deployment phase
	// +kubebuilder:validation:Enum=Pending;Scheduled;Running;Completed;Failed;Cancelled
	Phase string `json:"phase,omitempty"`

	// StartTime is when the deployment started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the deployment completed
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// DeletionScheduledTime is when deletion was scheduled (for uninstall operations)
	// +optional
	DeletionScheduledTime *metav1.Time `json:"deletionScheduledTime,omitempty"`

	// GracePeriodExpiry is when the grace period expires
	// +optional
	GracePeriodExpiry *metav1.Time `json:"gracePeriodExpiry,omitempty"`

	// JobRef references the job executing this deployment
	// +optional
	JobRef *corev1.ObjectReference `json:"jobRef,omitempty"`

	// RollbackAvailable indicates if rollback is possible
	// +optional
	RollbackAvailable *bool `json:"rollbackAvailable,omitempty"`

	// BackupRef references the backup for rollback
	// +optional
	BackupRef string `json:"backupRef,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Progress tracks deployment progress
	// +optional
	Progress *DeploymentProgress `json:"progress,omitempty"`

	// ExecutionHistory tracks execution attempts
	// +optional
	ExecutionHistory []ExecutionRecord `json:"executionHistory,omitempty"`

	// DependencyStatus tracks dependency resolution
	// +optional
	DependencyStatus []DependencyStatus `json:"dependencyStatus,omitempty"`
}

// DeploymentProgress tracks deployment progress
type DeploymentProgress struct {
	// TotalSteps is the total number of steps in the deployment
	TotalSteps int32 `json:"totalSteps"`

	// CompletedSteps is the number of completed steps
	CompletedSteps int32 `json:"completedSteps"`

	// CurrentStep describes the current step being executed
	// +optional
	CurrentStep string `json:"currentStep,omitempty"`

	// EstimatedCompletion is the estimated completion time
	// +optional
	EstimatedCompletion *metav1.Time `json:"estimatedCompletion,omitempty"`
}

// ExecutionRecord tracks a single execution attempt
type ExecutionRecord struct {
	// AttemptNumber is the attempt number (1-based)
	AttemptNumber int32 `json:"attemptNumber"`

	// StartTime is when this attempt started
	StartTime metav1.Time `json:"startTime"`

	// EndTime is when this attempt ended
	// +optional
	EndTime *metav1.Time `json:"endTime,omitempty"`

	// Status is the result of this attempt
	// +kubebuilder:validation:Enum=Running;Succeeded;Failed;Cancelled
	Status string `json:"status"`

	// ErrorMessage describes any error that occurred
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// JobRef references the job for this attempt
	// +optional
	JobRef *corev1.ObjectReference `json:"jobRef,omitempty"`
}

// DependencyStatus tracks the status of a dependency
type DependencyStatus struct {
	// Name is the dependency name
	Name string `json:"name"`

	// Status is the dependency status
	// +kubebuilder:validation:Enum=Pending;Satisfied;Failed;NotFound
	Status string `json:"status"`

	// Version is the resolved version
	// +optional
	Version string `json:"version,omitempty"`

	// Message provides additional information
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="App",type="string",JSONPath=".spec.appName"
// +kubebuilder:printcolumn:name="Operation",type="string",JSONPath=".spec.operation"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Progress",type="string",JSONPath=".status.progress.completedSteps"
// +kubebuilder:printcolumn:name="Started",type="date",JSONPath=".status.startTime"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AppFrameworkDeployment is the Schema for the appframeworkdeployments API
type AppFrameworkDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppFrameworkDeploymentSpec   `json:"spec,omitempty"`
	Status AppFrameworkDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AppFrameworkDeploymentList contains a list of AppFrameworkDeployment
type AppFrameworkDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppFrameworkDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppFrameworkDeployment{}, &AppFrameworkDeploymentList{})
}
