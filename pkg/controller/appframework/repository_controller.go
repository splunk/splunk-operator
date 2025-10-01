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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
)

// AppFrameworkRepositoryReconciler reconciles an AppFrameworkRepository object
type AppFrameworkRepositoryReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	JobManager    *JobManager
	ClientManager *RemoteClientManager
}

// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworkrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworkrepositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworkrepositories/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles AppFrameworkRepository reconciliation
func (r *AppFrameworkRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("repository", req.NamespacedName)
	logger.Info("Starting reconciliation")

	// Fetch the AppFrameworkRepository instance
	repository := &appframeworkv1.AppFrameworkRepository{}
	if err := r.Get(ctx, req.NamespacedName, repository); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Repository not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get repository")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !repository.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, repository)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(repository, AppFrameworkFinalizer) {
		controllerutil.AddFinalizer(repository, AppFrameworkFinalizer)
		if err := r.Update(ctx, repository); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Initialize status if needed
	if repository.Status.Phase == "" {
		repository.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, repository); err != nil {
			logger.Error(err, "Failed to initialize status")
			return ctrl.Result{}, err
		}
	}

	// Validate repository configuration
	if err := r.validateRepository(ctx, repository); err != nil {
		logger.Error(err, "Repository validation failed")
		r.updateRepositoryStatus(ctx, repository, "Error", err.Error())
		r.Recorder.Event(repository, corev1.EventTypeWarning, "ValidationFailed", err.Error())
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}

	// Check if sync is needed
	syncNeeded, err := r.shouldSync(ctx, repository)
	if err != nil {
		logger.Error(err, "Failed to determine if sync is needed")
		return ctrl.Result{}, err
	}

	if syncNeeded {
		logger.Info("Repository sync needed")
		if err := r.scheduleRepositorySync(ctx, repository); err != nil {
			logger.Error(err, "Failed to schedule repository sync")
			r.updateRepositoryStatus(ctx, repository, "Error", err.Error())
			return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
		}
	}

	// Update status to Ready if everything is good
	if repository.Status.Phase != "Ready" {
		r.updateRepositoryStatus(ctx, repository, "Ready", "Repository is ready")
		r.Recorder.Event(repository, corev1.EventTypeNormal, "Ready", "Repository is ready for use")
	}

	// Calculate next reconcile time based on poll interval
	pollInterval := time.Minute * 5 // default
	if repository.Spec.PollInterval != nil {
		pollInterval = repository.Spec.PollInterval.Duration
	}

	logger.Info("Reconciliation completed", "nextReconcile", pollInterval)
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// validateRepository validates the repository configuration
func (r *AppFrameworkRepositoryReconciler) validateRepository(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) error {
	logger := log.FromContext(ctx)

	// Validate storage type and provider combination
	if err := r.validateStorageConfig(repository.Spec); err != nil {
		return fmt.Errorf("invalid storage configuration: %w", err)
	}

	// Validate credentials if specified
	if repository.Spec.SecretRef != "" {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{
			Name:      repository.Spec.SecretRef,
			Namespace: repository.Namespace,
		}
		if err := r.Get(ctx, secretKey, secret); err != nil {
			return fmt.Errorf("failed to get credentials secret %s: %w", repository.Spec.SecretRef, err)
		}

		if err := r.validateCredentials(repository.Spec, secret); err != nil {
			return fmt.Errorf("invalid credentials: %w", err)
		}
	}

	// Test connectivity
	client, err := r.ClientManager.GetClient(ctx, repository)
	if err != nil {
		return fmt.Errorf("failed to create storage client: %w", err)
	}

	if err := client.TestConnection(ctx); err != nil {
		logger.Error(err, "Storage connectivity test failed")
		return fmt.Errorf("storage connectivity test failed: %w", err)
	}

	return nil
}

// validateStorageConfig validates storage type and provider combination
func (r *AppFrameworkRepositoryReconciler) validateStorageConfig(spec appframeworkv1.AppFrameworkRepositorySpec) error {
	validCombinations := map[string][]string{
		"s3":   {"aws", "minio"},
		"blob": {"azure"},
		"gcs":  {"gcp"},
	}

	validProviders, exists := validCombinations[spec.StorageType]
	if !exists {
		return fmt.Errorf("unsupported storage type: %s", spec.StorageType)
	}

	for _, validProvider := range validProviders {
		if spec.Provider == validProvider {
			return nil
		}
	}

	return fmt.Errorf("invalid provider %s for storage type %s", spec.Provider, spec.StorageType)
}

// validateCredentials validates the credentials in the secret
func (r *AppFrameworkRepositoryReconciler) validateCredentials(spec appframeworkv1.AppFrameworkRepositorySpec, secret *corev1.Secret) error {
	switch spec.Provider {
	case "aws":
		if _, exists := secret.Data["s3_access_key"]; !exists {
			return fmt.Errorf("missing s3_access_key in secret")
		}
		if _, exists := secret.Data["s3_secret_key"]; !exists {
			return fmt.Errorf("missing s3_secret_key in secret")
		}
	case "azure":
		if _, exists := secret.Data["azure_sa_name"]; !exists {
			return fmt.Errorf("missing azure_sa_name in secret")
		}
		if _, exists := secret.Data["azure_sa_secret_key"]; !exists {
			return fmt.Errorf("missing azure_sa_secret_key in secret")
		}
	case "gcp":
		if _, exists := secret.Data["key.json"]; !exists {
			return fmt.Errorf("missing key.json in secret")
		}
	case "minio":
		if _, exists := secret.Data["s3_access_key"]; !exists {
			return fmt.Errorf("missing s3_access_key in secret")
		}
		if _, exists := secret.Data["s3_secret_key"]; !exists {
			return fmt.Errorf("missing s3_secret_key in secret")
		}
	default:
		return fmt.Errorf("unsupported provider: %s", spec.Provider)
	}

	return nil
}

// shouldSync determines if a repository sync is needed
func (r *AppFrameworkRepositoryReconciler) shouldSync(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) (bool, error) {
	// Always sync if never synced before
	if repository.Status.LastSyncTime == nil {
		return true, nil
	}

	// Check if poll interval has elapsed
	pollInterval := time.Minute * 5 // default
	if repository.Spec.PollInterval != nil {
		pollInterval = repository.Spec.PollInterval.Duration
	}

	timeSinceLastSync := time.Since(repository.Status.LastSyncTime.Time)
	return timeSinceLastSync >= pollInterval, nil
}

// scheduleRepositorySync creates a job to sync the repository
func (r *AppFrameworkRepositoryReconciler) scheduleRepositorySync(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) error {
	logger := log.FromContext(ctx)

	// Check if there's already a sync job running
	existingJob, err := r.JobManager.GetActiveJob(ctx, repository, "repository-sync")
	if err != nil {
		return fmt.Errorf("failed to check for existing sync job: %w", err)
	}

	if existingJob != nil {
		logger.Info("Repository sync job already running", "job", existingJob.Name)
		return nil
	}

	// Create sync job
	job, err := r.JobManager.CreateRepositorySyncJob(ctx, repository)
	if err != nil {
		return fmt.Errorf("failed to create repository sync job: %w", err)
	}

	logger.Info("Created repository sync job", "job", job.Name)
	r.Recorder.Event(repository, corev1.EventTypeNormal, "SyncScheduled",
		fmt.Sprintf("Repository sync job %s scheduled", job.Name))

	// Update status to indicate sync is in progress
	r.updateRepositoryStatus(ctx, repository, "Syncing", "Repository sync in progress")

	return nil
}

// updateRepositoryStatus updates the repository status
func (r *AppFrameworkRepositoryReconciler) updateRepositoryStatus(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository, phase, message string) {
	logger := log.FromContext(ctx)

	repository.Status.Phase = phase

	// Update conditions
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             phase,
		Message:            message,
	}

	if phase == "Ready" {
		condition.Status = metav1.ConditionTrue
	}

	// Update or add condition
	updated := false
	for i, existingCondition := range repository.Status.Conditions {
		if existingCondition.Type == condition.Type {
			repository.Status.Conditions[i] = condition
			updated = true
			break
		}
	}
	if !updated {
		repository.Status.Conditions = append(repository.Status.Conditions, condition)
	}

	if err := r.Status().Update(ctx, repository); err != nil {
		logger.Error(err, "Failed to update repository status")
	}
}

// handleDeletion handles repository deletion
func (r *AppFrameworkRepositoryReconciler) handleDeletion(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("repository", repository.Name)
	logger.Info("Handling repository deletion")

	// Cancel any running jobs
	if err := r.JobManager.CancelJobsForRepository(ctx, repository); err != nil {
		logger.Error(err, "Failed to cancel jobs for repository")
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Clean up any cached data
	if err := r.cleanupRepositoryData(ctx, repository); err != nil {
		logger.Error(err, "Failed to cleanup repository data")
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(repository, AppFrameworkFinalizer)
	if err := r.Update(ctx, repository); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Repository deletion completed")
	return ctrl.Result{}, nil
}

// cleanupRepositoryData cleans up any cached data for the repository
func (r *AppFrameworkRepositoryReconciler) cleanupRepositoryData(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) error {
	// Implementation would clean up any cached app data, temporary files, etc.
	// This is a placeholder for the actual cleanup logic
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *AppFrameworkRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appframeworkv1.AppFrameworkRepository{}).
		Owns(&batchv1.Job{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

const (
	// AppFrameworkFinalizer is the finalizer used by the app framework controller
	AppFrameworkFinalizer = "appframework.splunk.com/finalizer"
)
