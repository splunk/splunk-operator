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
	"path/filepath"
	"strings"
	"time"

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
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// AppFrameworkSyncReconciler reconciles an AppFrameworkSync object
type AppFrameworkSyncReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	JobManager        *JobManager
	ClientManager     *RemoteClientManager
	DeploymentManager *DeploymentManager
}

// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworksyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworksyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworksyncs/finalizers,verbs=update
// +kubebuilder:rbac:groups=appframework.splunk.com,resources=appframeworkdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=standalones;clustermanagers;searchheadclusters;licensemanagers;monitoringconsoles,verbs=get;list;watch

// Reconcile handles AppFrameworkSync reconciliation
func (r *AppFrameworkSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("sync", req.NamespacedName)
	logger.Info("Starting sync reconciliation")

	// Fetch the AppFrameworkSync instance
	sync := &appframeworkv1.AppFrameworkSync{}
	if err := r.Get(ctx, req.NamespacedName, sync); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Sync not found, likely deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get sync")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !sync.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, sync)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(sync, AppFrameworkFinalizer) {
		controllerutil.AddFinalizer(sync, AppFrameworkFinalizer)
		if err := r.Update(ctx, sync); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Initialize status if needed
	if sync.Status.Phase == "" {
		sync.Status.Phase = "Pending"
		if err := r.Status().Update(ctx, sync); err != nil {
			logger.Error(err, "Failed to initialize status")
			return ctrl.Result{}, err
		}
	}

	// Validate sync configuration
	if err := r.validateSync(ctx, sync); err != nil {
		logger.Error(err, "Sync validation failed")
		r.updateSyncStatus(ctx, sync, "Error", err.Error())
		return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
	}

	// Get repository
	repository, err := r.getRepository(ctx, sync)
	if err != nil {
		logger.Error(err, "Failed to get repository")
		r.updateSyncStatus(ctx, sync, "Error", fmt.Sprintf("Repository not found: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
	}

	// Check if repository is ready
	if repository.Status.Phase != "Ready" {
		logger.Info("Repository not ready, waiting", "repositoryPhase", repository.Status.Phase)
		r.updateSyncStatus(ctx, sync, "Pending", "Waiting for repository to be ready")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Get target Splunk CR
	targetCR, err := r.getTargetCR(ctx, sync)
	if err != nil {
		logger.Error(err, "Failed to get target CR")
		r.updateSyncStatus(ctx, sync, "Error", fmt.Sprintf("Target CR not found: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
	}

	// Check if target CR is ready
	if !r.isTargetCRReady(targetCR) {
		logger.Info("Target CR not ready, waiting", "targetPhase", r.getTargetCRPhase(targetCR))
		r.updateSyncStatus(ctx, sync, "Pending", "Waiting for target CR to be ready")
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Perform sync
	if err := r.performSync(ctx, sync, repository, targetCR); err != nil {
		logger.Error(err, "Sync failed")
		r.updateSyncStatus(ctx, sync, "Error", fmt.Sprintf("Sync failed: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
	}

	// Update status to Synced
	r.updateSyncStatus(ctx, sync, "Synced", "Sync completed successfully")

	// Calculate next sync time
	syncInterval := time.Minute * 10 // default
	if repository.Spec.PollInterval != nil {
		syncInterval = repository.Spec.PollInterval.Duration
	}

	logger.Info("Sync reconciliation completed", "nextSync", syncInterval)
	return ctrl.Result{RequeueAfter: syncInterval}, nil
}

// validateSync validates the sync configuration
func (r *AppFrameworkSyncReconciler) validateSync(ctx context.Context, sync *appframeworkv1.AppFrameworkSync) error {
	// Validate repository reference
	if sync.Spec.RepositoryRef.Name == "" {
		return fmt.Errorf("repository reference name is required")
	}

	// Validate target reference
	if sync.Spec.TargetRef.Name == "" {
		return fmt.Errorf("target reference name is required")
	}

	// Validate supported target kinds
	supportedKinds := map[string]bool{
		"Standalone":        true,
		"ClusterManager":    true,
		"SearchHeadCluster": true,
		"LicenseManager":    true,
		"MonitoringConsole": true,
	}

	if !supportedKinds[sync.Spec.TargetRef.Kind] {
		return fmt.Errorf("unsupported target kind: %s", sync.Spec.TargetRef.Kind)
	}

	// Validate app sources
	if len(sync.Spec.AppSources) == 0 {
		return fmt.Errorf("at least one app source is required")
	}

	for _, appSource := range sync.Spec.AppSources {
		if appSource.Name == "" {
			return fmt.Errorf("app source name is required")
		}
		if appSource.Location == "" {
			return fmt.Errorf("app source location is required")
		}
		if appSource.Scope == "" {
			return fmt.Errorf("app source scope is required")
		}

		// Validate scope values
		validScopes := map[string]bool{
			"local":                true,
			"cluster":              true,
			"clusterWithPreConfig": true,
			"premiumApps":          true,
		}

		if !validScopes[appSource.Scope] {
			return fmt.Errorf("invalid app source scope: %s", appSource.Scope)
		}
	}

	return nil
}

// getRepository gets the referenced repository
func (r *AppFrameworkSyncReconciler) getRepository(ctx context.Context, sync *appframeworkv1.AppFrameworkSync) (*appframeworkv1.AppFrameworkRepository, error) {
	namespace := sync.Spec.RepositoryRef.Namespace
	if namespace == "" {
		namespace = sync.Namespace
	}

	repository := &appframeworkv1.AppFrameworkRepository{}
	key := types.NamespacedName{
		Name:      sync.Spec.RepositoryRef.Name,
		Namespace: namespace,
	}

	if err := r.Get(ctx, key, repository); err != nil {
		return nil, fmt.Errorf("failed to get repository %s: %w", key, err)
	}

	return repository, nil
}

// getTargetCR gets the target Splunk CR
func (r *AppFrameworkSyncReconciler) getTargetCR(ctx context.Context, sync *appframeworkv1.AppFrameworkSync) (client.Object, error) {
	namespace := sync.Spec.TargetRef.Namespace
	if namespace == "" {
		namespace = sync.Namespace
	}

	key := types.NamespacedName{
		Name:      sync.Spec.TargetRef.Name,
		Namespace: namespace,
	}

	var targetCR client.Object

	switch sync.Spec.TargetRef.Kind {
	case "Standalone":
		targetCR = &enterpriseApi.Standalone{}
	case "ClusterManager":
		targetCR = &enterpriseApi.ClusterManager{}
	case "SearchHeadCluster":
		targetCR = &enterpriseApi.SearchHeadCluster{}
	case "LicenseManager":
		targetCR = &enterpriseApi.LicenseManager{}
	case "MonitoringConsole":
		targetCR = &enterpriseApi.MonitoringConsole{}
	default:
		return nil, fmt.Errorf("unsupported target kind: %s", sync.Spec.TargetRef.Kind)
	}

	if err := r.Get(ctx, key, targetCR); err != nil {
		return nil, fmt.Errorf("failed to get target CR %s: %w", key, err)
	}

	return targetCR, nil
}

// isTargetCRReady checks if the target CR is ready
func (r *AppFrameworkSyncReconciler) isTargetCRReady(targetCR client.Object) bool {
	phase := r.getTargetCRPhase(targetCR)
	return phase == "Ready"
}

// getTargetCRPhase gets the phase of the target CR
func (r *AppFrameworkSyncReconciler) getTargetCRPhase(targetCR client.Object) string {
	switch cr := targetCR.(type) {
	case *enterpriseApi.Standalone:
		return string(cr.Status.Phase)
	case *enterpriseApi.ClusterManager:
		return string(cr.Status.Phase)
	case *enterpriseApi.SearchHeadCluster:
		return string(cr.Status.Phase)
	case *enterpriseApi.LicenseManager:
		return string(cr.Status.Phase)
	case *enterpriseApi.MonitoringConsole:
		return string(cr.Status.Phase)
	default:
		return "Unknown"
	}
}

// performSync performs the actual sync operation
func (r *AppFrameworkSyncReconciler) performSync(ctx context.Context, sync *appframeworkv1.AppFrameworkSync, repository *appframeworkv1.AppFrameworkRepository, targetCR client.Object) error {
	logger := log.FromContext(ctx)

	// Update status to indicate sync is in progress
	r.updateSyncStatus(ctx, sync, "Syncing", "Sync in progress")

	// Get current apps from repository
	currentApps, err := r.getCurrentAppsFromRepository(ctx, sync, repository)
	if err != nil {
		return fmt.Errorf("failed to get current apps from repository: %w", err)
	}

	// Get existing synced apps
	existingSyncedApps := make(map[string]appframeworkv1.SyncedApp)
	for _, app := range sync.Status.SyncedApps {
		existingSyncedApps[app.Name] = app
	}

	// Detect apps that need to be installed/updated
	var appsToInstall []AppToSync
	for _, currentApp := range currentApps {
		existingApp, exists := existingSyncedApps[currentApp.Name]

		if !exists {
			// New app - needs installation
			appsToInstall = append(appsToInstall, AppToSync{
				Name:      currentApp.Name,
				Source:    currentApp.Source,
				Checksum:  currentApp.Checksum,
				Operation: "install",
			})
		} else if existingApp.Checksum != currentApp.Checksum {
			// App updated - needs update
			appsToInstall = append(appsToInstall, AppToSync{
				Name:      currentApp.Name,
				Source:    currentApp.Source,
				Checksum:  currentApp.Checksum,
				Operation: "update",
			})
		}
	}

	// Detect apps that need to be deleted (if pruning is enabled)
	var appsToDelete []AppToSync
	if sync.Spec.SyncPolicy != nil && sync.Spec.SyncPolicy.Prune != nil && *sync.Spec.SyncPolicy.Prune {
		currentAppNames := make(map[string]bool)
		for _, app := range currentApps {
			currentAppNames[app.Name] = true
		}

		for _, existingApp := range sync.Status.SyncedApps {
			if !currentAppNames[existingApp.Name] {
				// App no longer exists in repository - needs deletion
				appsToDelete = append(appsToDelete, AppToSync{
					Name:      existingApp.Name,
					Source:    existingApp.Source,
					Operation: "uninstall",
				})
			}
		}
	}

	// Create deployments for apps that need installation/update
	for _, app := range appsToInstall {
		if err := r.createAppDeployment(ctx, sync, targetCR, app); err != nil {
			logger.Error(err, "Failed to create deployment for app", "app", app.Name, "operation", app.Operation)
			// Continue with other apps rather than failing completely
		}
	}

	// Create deployments for apps that need deletion
	for _, app := range appsToDelete {
		if err := r.createAppDeployment(ctx, sync, targetCR, app); err != nil {
			logger.Error(err, "Failed to create deletion deployment for app", "app", app.Name)
			// Continue with other apps rather than failing completely
		}
	}

	// Update sync statistics
	r.updateSyncStatistics(ctx, sync, currentApps)

	logger.Info("Sync operation completed",
		"appsToInstall", len(appsToInstall),
		"appsToDelete", len(appsToDelete))

	return nil
}

// AppToSync represents an app that needs to be synced
type AppToSync struct {
	Name      string
	Source    string
	Checksum  string
	Operation string // install, update, uninstall
}

// getCurrentAppsFromRepository gets the current apps from the repository
func (r *AppFrameworkSyncReconciler) getCurrentAppsFromRepository(ctx context.Context, sync *appframeworkv1.AppFrameworkSync, repository *appframeworkv1.AppFrameworkRepository) ([]AppToSync, error) {
	var allApps []AppToSync

	for _, appSource := range sync.Spec.AppSources {
		// Build full path
		fullPath := filepath.Join(repository.Spec.Path, appSource.Location)

		// Get apps from repository
		apps, err := r.ClientManager.GetAppsList(ctx, repository, fullPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get apps from source %s: %w", appSource.Name, err)
		}

		// Filter apps based on include/exclude patterns
		filteredApps := r.filterApps(apps, appSource.IncludePatterns, appSource.ExcludePatterns)

		// Convert to AppToSync
		for _, app := range filteredApps {
			allApps = append(allApps, AppToSync{
				Name:     extractAppName(app.Key),
				Source:   appSource.Name,
				Checksum: app.ETag,
			})
		}
	}

	return allApps, nil
}

// filterApps filters apps based on include/exclude patterns
func (r *AppFrameworkSyncReconciler) filterApps(apps []*RemoteObject, includePatterns, excludePatterns []string) []*RemoteObject {
	var filteredApps []*RemoteObject

	for _, app := range apps {
		appName := extractAppName(app.Key)

		// Check if app should be included
		included := len(includePatterns) == 0 // Include all if no patterns specified
		for _, pattern := range includePatterns {
			if matched, _ := filepath.Match(pattern, appName); matched {
				included = true
				break
			}
		}

		if !included {
			continue
		}

		// Check if app should be excluded
		excluded := false
		for _, pattern := range excludePatterns {
			if matched, _ := filepath.Match(pattern, appName); matched {
				excluded = true
				break
			}
		}

		if !excluded {
			filteredApps = append(filteredApps, app)
		}
	}

	return filteredApps
}

// extractAppName extracts the app name from the full key path
func extractAppName(key string) string {
	return filepath.Base(key)
}

// createAppDeployment creates an AppFrameworkDeployment for an app
func (r *AppFrameworkSyncReconciler) createAppDeployment(ctx context.Context, sync *appframeworkv1.AppFrameworkSync, targetCR client.Object, app AppToSync) error {
	logger := log.FromContext(ctx)

	// Get target pods for the deployment
	targetPods, err := r.getTargetPods(ctx, targetCR)
	if err != nil {
		return fmt.Errorf("failed to get target pods: %w", err)
	}

	// Find app source configuration
	var appSourceConfig *appframeworkv1.AppSourceSpec
	for _, source := range sync.Spec.AppSources {
		if source.Name == app.Source {
			appSourceConfig = &source
			break
		}
	}

	if appSourceConfig == nil {
		return fmt.Errorf("app source configuration not found: %s", app.Source)
	}

	// Create deployment name
	deploymentName := fmt.Sprintf("%s-%s-%s-%d",
		sync.Name,
		strings.ToLower(app.Operation),
		sanitizeName(app.Name),
		time.Now().Unix())

	deployment := &appframeworkv1.AppFrameworkDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: sync.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "app-framework",
				"app.kubernetes.io/component":  "deployment",
				"app.kubernetes.io/managed-by": "app-framework-controller",
				"appframework.splunk.com/sync": sync.Name,
				"appframework.splunk.com/app":  sanitizeName(app.Name),
			},
		},
		Spec: appframeworkv1.AppFrameworkDeploymentSpec{
			AppName:    app.Name,
			AppSource:  app.Source,
			TargetPods: targetPods,
			Operation:  app.Operation,
			Scope:      appSourceConfig.Scope,
		},
	}

	// Set deletion policy if specified
	if appSourceConfig.DeletionPolicy != nil {
		deployment.Spec.DeletionPolicy = appSourceConfig.DeletionPolicy
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(sync, deployment, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the deployment
	if err := r.Create(ctx, deployment); err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	logger.Info("Created app deployment",
		"deployment", deployment.Name,
		"app", app.Name,
		"operation", app.Operation)

	return nil
}

// getTargetPods gets the target pods for a Splunk CR
func (r *AppFrameworkSyncReconciler) getTargetPods(ctx context.Context, targetCR client.Object) ([]string, error) {
	// This is a simplified implementation
	// In a real implementation, you would query the actual pods
	switch cr := targetCR.(type) {
	case *enterpriseApi.Standalone:
		pods := make([]string, cr.Spec.Replicas)
		for i := int32(0); i < cr.Spec.Replicas; i++ {
			pods[i] = fmt.Sprintf("splunk-%s-standalone-%d", cr.Name, i)
		}
		return pods, nil
	case *enterpriseApi.ClusterManager:
		return []string{fmt.Sprintf("splunk-%s-cluster-manager-0", cr.Name)}, nil
	case *enterpriseApi.SearchHeadCluster:
		return []string{fmt.Sprintf("splunk-%s-deployer-0", cr.Name)}, nil
	case *enterpriseApi.LicenseManager:
		return []string{fmt.Sprintf("splunk-%s-license-manager-0", cr.Name)}, nil
	case *enterpriseApi.MonitoringConsole:
		return []string{fmt.Sprintf("splunk-%s-monitoring-console-0", cr.Name)}, nil
	default:
		return nil, fmt.Errorf("unsupported target CR type")
	}
}

// sanitizeName sanitizes a name for use in Kubernetes resources
func sanitizeName(name string) string {
	// Remove file extensions and invalid characters
	name = strings.TrimSuffix(name, ".tgz")
	name = strings.TrimSuffix(name, ".spl")
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, ".", "-")

	// Ensure it doesn't start or end with non-alphanumeric characters
	name = strings.Trim(name, "-")

	// Limit length
	if len(name) > 50 {
		name = name[:50]
	}

	return name
}

// updateSyncStatistics updates the sync statistics
func (r *AppFrameworkSyncReconciler) updateSyncStatistics(ctx context.Context, sync *appframeworkv1.AppFrameworkSync, currentApps []AppToSync) {
	if sync.Status.SyncStatistics == nil {
		sync.Status.SyncStatistics = &appframeworkv1.SyncStatistics{}
	}

	sync.Status.SyncStatistics.TotalApps = int32(len(currentApps))

	// Count installed, failed, and pending apps
	installedCount := int32(0)
	failedCount := int32(0)
	pendingCount := int32(0)

	for _, syncedApp := range sync.Status.SyncedApps {
		switch syncedApp.Status {
		case "Installed":
			installedCount++
		case "Failed":
			failedCount++
		case "Pending", "Installing", "Updating":
			pendingCount++
		}
	}

	sync.Status.SyncStatistics.InstalledApps = installedCount
	sync.Status.SyncStatistics.FailedApps = failedCount
	sync.Status.SyncStatistics.PendingApps = pendingCount

	// Update last sync time
	now := metav1.Now()
	sync.Status.LastSyncTime = &now
	if sync.Status.Phase == "Synced" {
		sync.Status.LastSuccessfulSyncTime = &now
	}

	r.Status().Update(ctx, sync)
}

// updateSyncStatus updates the sync status
func (r *AppFrameworkSyncReconciler) updateSyncStatus(ctx context.Context, sync *appframeworkv1.AppFrameworkSync, phase, message string) {
	logger := log.FromContext(ctx)

	sync.Status.Phase = phase

	// Update conditions
	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             phase,
		Message:            message,
	}

	if phase == "Synced" {
		condition.Status = metav1.ConditionTrue
	}

	// Update or add condition
	updated := false
	for i, existingCondition := range sync.Status.Conditions {
		if existingCondition.Type == condition.Type {
			sync.Status.Conditions[i] = condition
			updated = true
			break
		}
	}
	if !updated {
		sync.Status.Conditions = append(sync.Status.Conditions, condition)
	}

	if err := r.Status().Update(ctx, sync); err != nil {
		logger.Error(err, "Failed to update sync status")
	}
}

// handleDeletion handles sync deletion
func (r *AppFrameworkSyncReconciler) handleDeletion(ctx context.Context, sync *appframeworkv1.AppFrameworkSync) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("sync", sync.Name)
	logger.Info("Handling sync deletion")

	// Cancel any running deployments
	if err := r.cancelDeploymentsForSync(ctx, sync); err != nil {
		logger.Error(err, "Failed to cancel deployments for sync")
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(sync, AppFrameworkFinalizer)
	if err := r.Update(ctx, sync); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Sync deletion completed")
	return ctrl.Result{}, nil
}

// cancelDeploymentsForSync cancels all deployments for a sync
func (r *AppFrameworkSyncReconciler) cancelDeploymentsForSync(ctx context.Context, sync *appframeworkv1.AppFrameworkSync) error {
	// Implementation would cancel/delete all AppFrameworkDeployments owned by this sync
	// This is a placeholder for the actual cleanup logic
	return nil
}

// SetupWithManager sets up the controller with the Manager
func (r *AppFrameworkSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appframeworkv1.AppFrameworkSync{}).
		Owns(&appframeworkv1.AppFrameworkDeployment{}).
		WithOptions(ctrl.Options{
			MaxConcurrentReconciles: 3,
		}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
