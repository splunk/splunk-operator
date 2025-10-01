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

package appframework

import (
	"context"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
)

// AppFrameworkDeploymentReconciler reconciles an AppFrameworkDeployment object
type AppFrameworkDeploymentReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Recorder        record.EventRecorder
	JobManager      *JobManager
	ClientManager   *RemoteClientManager
	DeletionManager *DeletionManager
}

// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworkdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworkdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworkdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles AppFrameworkDeployment reconciliation
func (r *AppFrameworkDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("deployment", req.NamespacedName)
	logger.Info("Starting deployment reconciliation")

	// Fetch the AppFrameworkDeployment instance
	deployment := &appframeworkv1.AppFrameworkDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Deployment not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get deployment")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !deployment.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, deployment)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(deployment, AppFrameworkFinalizer) {
		controllerutil.AddFinalizer(deployment, AppFrameworkFinalizer)
		if err := r.Update(ctx, deployment); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Initialize status if needed
	if deployment.Status.Phase == "" {
		deployment.Status.Phase = "Pending"
		deployment.Status.Progress = &appframeworkv1.DeploymentProgress{
			TotalSteps:     r.getTotalStepsForOperation(deployment.Spec.Operation),
			CompletedSteps: 0,
		}
		if err := r.Status().Update(ctx, deployment); err != nil {
			logger.Error(err, "Failed to initialize status")
			return ctrl.Result{}, err
		}
	}

	// Validate deployment
	if err := r.validateDeployment(ctx, deployment); err != nil {
		logger.Error(err, "Deployment validation failed")
		r.updateDeploymentStatus(ctx, deployment, "Failed", err.Error())
		r.Recorder.Event(deployment, corev1.EventTypeWarning, "ValidationFailed", err.Error())
		return ctrl.Result{}, nil // Don't requeue validation failures
	}

	// Handle different operations
	switch deployment.Spec.Operation {
	case "install", "update":
		return r.handleInstallUpdate(ctx, deployment)
	case "uninstall":
		return r.handleUninstall(ctx, deployment)
	default:
		err := fmt.Errorf("unsupported operation: %s", deployment.Spec.Operation)
		logger.Error(err, "Unsupported operation")
		r.updateDeploymentStatus(ctx, deployment, "Failed", err.Error())
		return ctrl.Result{}, nil
	}
}

// validateDeployment validates the deployment configuration
func (r *AppFrameworkDeploymentReconciler) validateDeployment(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) error {
	// Validate required fields
	if deployment.Spec.AppName == "" {
		return fmt.Errorf("app name is required")
	}

	if len(deployment.Spec.TargetPods) == 0 {
		return fmt.Errorf("at least one target pod is required")
	}

	// Validate operation
	validOperations := map[string]bool{
		"install":   true,
		"update":    true,
		"uninstall": true,
	}

	if !validOperations[deployment.Spec.Operation] {
		return fmt.Errorf("invalid operation: %s", deployment.Spec.Operation)
	}

	// Validate scope
	if deployment.Spec.Scope != "" {
		validScopes := map[string]bool{
			"local":                true,
			"cluster":              true,
			"clusterWithPreConfig": true,
			"premiumApps":          true,
		}

		if !validScopes[deployment.Spec.Scope] {
			return fmt.Errorf("invalid scope: %s", deployment.Spec.Scope)
		}
	}

	// Validate deletion conditions for uninstall operations
	if deployment.Spec.Operation == "uninstall" {
		for _, condition := range deployment.Spec.DeletionConditions {
			if condition.Type == "" {
				return fmt.Errorf("deletion condition type is required")
			}
			if len(condition.CheckCommand) == 0 {
				return fmt.Errorf("deletion condition check command is required")
			}
		}
	}

	return nil
}

// handleInstallUpdate handles install and update operations
func (r *AppFrameworkDeploymentReconciler) handleInstallUpdate(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check if grace period needs to be respected
	if deployment.Spec.GracePeriod != nil && deployment.Status.StartTime == nil {
		// Schedule for later execution
		now := metav1.Now()
		deployment.Status.StartTime = &now
		gracePeriodExpiry := metav1.NewTime(now.Add(deployment.Spec.GracePeriod.Duration))
		deployment.Status.GracePeriodExpiry = &gracePeriodExpiry

		r.updateDeploymentStatus(ctx, deployment, "Scheduled", "Waiting for grace period to expire")

		return ctrl.Result{RequeueAfter: deployment.Spec.GracePeriod.Duration}, nil
	}

	// Check if grace period has expired
	if deployment.Status.GracePeriodExpiry != nil && time.Now().Before(deployment.Status.GracePeriodExpiry.Time) {
		remaining := time.Until(deployment.Status.GracePeriodExpiry.Time)
		logger.Info("Grace period not expired yet", "remaining", remaining)
		return ctrl.Result{RequeueAfter: remaining}, nil
	}

	// Check dependencies
	if len(deployment.Spec.Dependencies) > 0 {
		satisfied, err := r.checkDependencies(ctx, deployment)
		if err != nil {
			logger.Error(err, "Failed to check dependencies")
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		if !satisfied {
			logger.Info("Dependencies not satisfied, waiting")
			r.updateDeploymentStatus(ctx, deployment, "Pending", "Waiting for dependencies")
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
	}

	// Get or create repository reference
	repository, err := r.getRepositoryForDeployment(ctx, deployment)
	if err != nil {
		logger.Error(err, "Failed to get repository")
		r.updateDeploymentStatus(ctx, deployment, "Failed", fmt.Sprintf("Repository error: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}

	// Check if download job exists and is complete
	downloadJob, err := r.getJobForDeployment(ctx, deployment, "download")
	if err != nil {
		logger.Error(err, "Failed to check download job")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	if downloadJob == nil {
		// Create download job
		logger.Info("Creating download job")
		downloadJob, err = r.JobManager.CreateAppDownloadJob(ctx, deployment, repository)
		if err != nil {
			logger.Error(err, "Failed to create download job")
			r.updateDeploymentStatus(ctx, deployment, "Failed", fmt.Sprintf("Download job creation failed: %v", err))
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}

		r.updateDeploymentStatus(ctx, deployment, "Running", "Download job created")
		r.updateProgress(ctx, deployment, 1, "Downloading app package")
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Check download job status
	if !r.isJobComplete(downloadJob) {
		if r.isJobFailed(downloadJob) {
			r.updateDeploymentStatus(ctx, deployment, "Failed", "Download job failed")
			return ctrl.Result{}, nil
		}
		// Still running
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Download complete, check install job
	installJob, err := r.getJobForDeployment(ctx, deployment, "install")
	if err != nil {
		logger.Error(err, "Failed to check install job")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	if installJob == nil {
		// Create install job
		logger.Info("Creating install job")
		installJob, err = r.JobManager.CreateAppInstallJob(ctx, deployment)
		if err != nil {
			logger.Error(err, "Failed to create install job")
			r.updateDeploymentStatus(ctx, deployment, "Failed", fmt.Sprintf("Install job creation failed: %v", err))
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}

		r.updateProgress(ctx, deployment, 2, "Installing app")
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Check install job status
	if !r.isJobComplete(installJob) {
		if r.isJobFailed(installJob) {
			r.updateDeploymentStatus(ctx, deployment, "Failed", "Install job failed")
			return ctrl.Result{}, nil
		}
		// Still running
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Installation complete
	r.updateProgress(ctx, deployment, 3, "Installation completed")
	r.updateDeploymentStatus(ctx, deployment, "Completed", "App installation completed successfully")

	// Create backup for rollback if enabled
	if deployment.Spec.RollbackPolicy != nil && deployment.Spec.RollbackPolicy.Enabled != nil && *deployment.Spec.RollbackPolicy.Enabled {
		if err := r.createRollbackBackup(ctx, deployment); err != nil {
			logger.Error(err, "Failed to create rollback backup")
			// Don't fail the deployment for backup issues
		}
	}

	logger.Info("App deployment completed successfully")
	r.Recorder.Event(deployment, corev1.EventTypeNormal, "DeploymentCompleted",
		fmt.Sprintf("App %s deployed successfully", deployment.Spec.AppName))

	return ctrl.Result{}, nil
}

// handleUninstall handles uninstall operations with enhanced safety mechanisms
func (r *AppFrameworkDeploymentReconciler) handleUninstall(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Check deletion policy and safety mechanisms
	if deployment.Spec.DeletionPolicy != nil {
		// Check if deletion is enabled
		if deployment.Spec.DeletionPolicy.Enabled != nil && !*deployment.Spec.DeletionPolicy.Enabled {
			r.updateDeploymentStatus(ctx, deployment, "Failed", "App deletion is disabled by policy")
			return ctrl.Result{}, nil
		}

		// Check safeguards
		if deployment.Spec.DeletionPolicy.Safeguards != nil {
			if err := r.DeletionManager.CheckSafeguards(ctx, deployment); err != nil {
				logger.Error(err, "Deletion safeguards check failed")
				r.updateDeploymentStatus(ctx, deployment, "Failed", fmt.Sprintf("Safeguards check failed: %v", err))
				return ctrl.Result{}, nil
			}
		}

		// Handle grace period for graceful deletion
		if deployment.Spec.DeletionPolicy.Strategy == "graceful" {
			if deployment.Status.DeletionScheduledTime == nil {
				// Schedule deletion
				now := metav1.Now()
				deployment.Status.DeletionScheduledTime = &now

				gracePeriod := time.Minute * 5 // default
				if deployment.Spec.DeletionPolicy.GracePeriod != nil {
					gracePeriod = deployment.Spec.DeletionPolicy.GracePeriod.Duration
				}

				gracePeriodExpiry := metav1.NewTime(now.Add(gracePeriod))
				deployment.Status.GracePeriodExpiry = &gracePeriodExpiry

				r.updateDeploymentStatus(ctx, deployment, "Scheduled", "App deletion scheduled")
				r.Recorder.Event(deployment, corev1.EventTypeWarning, "DeletionScheduled",
					fmt.Sprintf("App %s scheduled for deletion in %v", deployment.Spec.AppName, gracePeriod))

				return ctrl.Result{RequeueAfter: gracePeriod}, nil
			}

			// Check if grace period has expired
			if deployment.Status.GracePeriodExpiry != nil && time.Now().Before(deployment.Status.GracePeriodExpiry.Time) {
				remaining := time.Until(deployment.Status.GracePeriodExpiry.Time)
				logger.Info("Deletion grace period not expired yet", "remaining", remaining)
				return ctrl.Result{RequeueAfter: remaining}, nil
			}
		}

		// Handle manual approval requirement
		if deployment.Spec.DeletionPolicy.Strategy == "manual" {
			approved, err := r.DeletionManager.CheckApproval(ctx, deployment)
			if err != nil {
				logger.Error(err, "Failed to check deletion approval")
				return ctrl.Result{RequeueAfter: time.Minute}, nil
			}
			if !approved {
				r.updateDeploymentStatus(ctx, deployment, "Pending", "Waiting for manual approval")
				return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
			}
		}
	}

	// Check deletion conditions
	if len(deployment.Spec.DeletionConditions) > 0 {
		satisfied, err := r.checkDeletionConditions(ctx, deployment)
		if err != nil {
			logger.Error(err, "Failed to check deletion conditions")
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		if !satisfied {
			r.updateDeploymentStatus(ctx, deployment, "Pending", "Deletion conditions not satisfied")
			return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
		}
	}

	// Create backup before deletion if enabled
	if deployment.Spec.DeletionPolicy != nil &&
		deployment.Spec.DeletionPolicy.Safeguards != nil &&
		deployment.Spec.DeletionPolicy.Safeguards.BackupBeforeDeletion != nil &&
		*deployment.Spec.DeletionPolicy.Safeguards.BackupBeforeDeletion {

		if err := r.createDeletionBackup(ctx, deployment); err != nil {
			logger.Error(err, "Failed to create deletion backup")
			r.updateDeploymentStatus(ctx, deployment, "Failed", fmt.Sprintf("Backup creation failed: %v", err))
			return ctrl.Result{}, nil
		}
	}

	// Check if uninstall job exists
	uninstallJob, err := r.getJobForDeployment(ctx, deployment, "uninstall")
	if err != nil {
		logger.Error(err, "Failed to check uninstall job")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	if uninstallJob == nil {
		// Create uninstall job
		logger.Info("Creating uninstall job")
		uninstallJob, err = r.JobManager.CreateAppUninstallJob(ctx, deployment)
		if err != nil {
			logger.Error(err, "Failed to create uninstall job")
			r.updateDeploymentStatus(ctx, deployment, "Failed", fmt.Sprintf("Uninstall job creation failed: %v", err))
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}

		r.updateDeploymentStatus(ctx, deployment, "Running", "Uninstall job created")
		r.updateProgress(ctx, deployment, 1, "Uninstalling app")
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Check uninstall job status
	if !r.isJobComplete(uninstallJob) {
		if r.isJobFailed(uninstallJob) {
			r.updateDeploymentStatus(ctx, deployment, "Failed", "Uninstall job failed")
			return ctrl.Result{}, nil
		}
		// Still running
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Uninstall complete
	r.updateProgress(ctx, deployment, 2, "App uninstalled successfully")
	r.updateDeploymentStatus(ctx, deployment, "Completed", "App uninstall completed successfully")

	logger.Info("App uninstall completed successfully")
	r.Recorder.Event(deployment, corev1.EventTypeNormal, "UninstallCompleted",
		fmt.Sprintf("App %s uninstalled successfully", deployment.Spec.AppName))

	return ctrl.Result{}, nil
}

// getTotalStepsForOperation returns the total number of steps for an operation
func (r *AppFrameworkDeploymentReconciler) getTotalStepsForOperation(operation string) int32 {
	switch operation {
	case "install", "update":
		return 3 // download, install, complete
	case "uninstall":
		return 2 // uninstall, complete
	default:
		return 1
	}
}

// getRepositoryForDeployment gets the repository for a deployment
func (r *AppFrameworkDeploymentReconciler) getRepositoryForDeployment(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (*appframeworkv1.AppFrameworkRepository, error) {
	// This is a simplified implementation
	// In reality, you would need to trace back through the AppFrameworkSync to find the repository
	// For now, we'll assume there's a label or annotation that references the repository

	// This would need to be implemented based on how you want to link deployments to repositories
	return nil, fmt.Errorf("repository lookup not implemented")
}

// getJobForDeployment gets a job for a deployment and job type
func (r *AppFrameworkDeploymentReconciler) getJobForDeployment(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, jobType string) (*batchv1.Job, error) {
	// Implementation would look for jobs with appropriate labels
	// This is a placeholder
	return nil, nil
}

// isJobComplete checks if a job is complete
func (r *AppFrameworkDeploymentReconciler) isJobComplete(job *batchv1.Job) bool {
	return job.Status.CompletionTime != nil
}

// isJobFailed checks if a job has failed
func (r *AppFrameworkDeploymentReconciler) isJobFailed(job *batchv1.Job) bool {
	return job.Status.Failed > 0
}

// checkDependencies checks if all dependencies are satisfied
func (r *AppFrameworkDeploymentReconciler) checkDependencies(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (bool, error) {
	// Implementation would check if dependent apps are installed
	// This is a placeholder
	return true, nil
}

// checkDeletionConditions checks if deletion conditions are satisfied
func (r *AppFrameworkDeploymentReconciler) checkDeletionConditions(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (bool, error) {
	// Implementation would run the check commands and verify results
	// This is a placeholder
	return true, nil
}

// createRollbackBackup creates a backup for rollback purposes
func (r *AppFrameworkDeploymentReconciler) createRollbackBackup(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) error {
	// Implementation would create a backup of the app
	// This is a placeholder
	return nil
}

// createDeletionBackup creates a backup before deletion
func (r *AppFrameworkDeploymentReconciler) createDeletionBackup(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) error {
	// Implementation would create a backup of the app before deletion
	// This is a placeholder
	return nil
}

// updateProgress updates the deployment progress
func (r *AppFrameworkDeploymentReconciler) updateProgress(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, completedSteps int32, currentStep string) {
	if deployment.Status.Progress == nil {
		deployment.Status.Progress = &appframeworkv1.DeploymentProgress{}
	}

	deployment.Status.Progress.CompletedSteps = completedSteps
	deployment.Status.Progress.CurrentStep = currentStep

	// Estimate completion time based on progress
	if completedSteps > 0 && deployment.Status.StartTime != nil {
		elapsed := time.Since(deployment.Status.StartTime.Time)
		totalEstimated := time.Duration(float64(elapsed) / float64(completedSteps) * float64(deployment.Status.Progress.TotalSteps))
		estimatedCompletion := metav1.NewTime(deployment.Status.StartTime.Add(totalEstimated))
		deployment.Status.Progress.EstimatedCompletion = &estimatedCompletion
	}

	r.Status().Update(ctx, deployment)
}

// updateDeploymentStatus updates the deployment status
func (r *AppFrameworkDeploymentReconciler) updateDeploymentStatus(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment, phase, message string) {
	logger := log.FromContext(ctx)

	deployment.Status.Phase = phase

	if phase == "Completed" || phase == "Failed" || phase == "Cancelled" {
		now := metav1.Now()
		deployment.Status.CompletionTime = &now
	}

	// Update conditions
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             phase,
		Message:            message,
	}

	if phase == "Completed" {
		condition.Status = metav1.ConditionTrue
	}

	// Update or add condition
	updated := false
	for i, existingCondition := range deployment.Status.Conditions {
		if existingCondition.Type == condition.Type {
			deployment.Status.Conditions[i] = condition
			updated = true
			break
		}
	}
	if !updated {
		deployment.Status.Conditions = append(deployment.Status.Conditions, condition)
	}

	if err := r.Status().Update(ctx, deployment); err != nil {
		logger.Error(err, "Failed to update deployment status")
	}
}

// handleDeletion handles deployment deletion
func (r *AppFrameworkDeploymentReconciler) handleDeletion(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("deployment", deployment.Name)
	logger.Info("Handling deployment deletion")

	// Cancel any running jobs
	if err := r.cancelJobsForDeployment(ctx, deployment); err != nil {
		logger.Error(err, "Failed to cancel jobs for deployment")
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(deployment, AppFrameworkFinalizer)
	if err := r.Update(ctx, deployment); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Deployment deletion completed")
	return ctrl.Result{}, nil
}

// cancelJobsForDeployment cancels all jobs for a deployment
func (r *AppFrameworkDeploymentReconciler) cancelJobsForDeployment(ctx context.Context, deployment *appframeworkv1.AppFrameworkDeployment) error {
	// Implementation would cancel/delete all jobs owned by this deployment
	// This is a placeholder
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *AppFrameworkDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appframeworkv1.AppFrameworkDeployment{}).
		Owns(&batchv1.Job{}).
		WithOptions(ctrl.Options{
			MaxConcurrentReconciles: 10,
		}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
