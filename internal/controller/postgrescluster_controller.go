/*
Copyright 2026.

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

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	logs "sigs.k8s.io/controller-runtime/pkg/log"
)

// PostgresClusterReconciler reconciles a PostgresCluster object
type PostgresClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	retryDelaytimer = time.Second * 15
)

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusterclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers/status,verbs=get

// Main reconciliation loop for PostgresCluster.
func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	logger := logs.FromContext(ctx)
	logger.Info("Reconciling PostgresCluster", "name", req.Name, "namespace", req.Namespace)

	// Initialize as nil so syncStatus knows if the object was actually found/created.
	var cnpgCluster *cnpgv1.Cluster

	// 1. Fetch the PostgresCluster instance, stop, if not found.
	postgresCluster := &enterprisev4.PostgresCluster{}
	if getPGClusterErr := r.Get(ctx, req.NamespacedName, postgresCluster); getPGClusterErr != nil {
		if apierrors.IsNotFound(getPGClusterErr) {
			logger.Info("PostgresCluster deleted, skipping reconciliation")
			return res, nil
		}
		logger.Error(getPGClusterErr, "Unable to fetch PostgresCluster")
		return res, getPGClusterErr
	}

	// This deferred function will run at the end of the reconciliation process, regardless of whether it exits early due to an error or completes successfully.
	// It ensures that we always attempt to sync the status of the PostgresCluster based on the final state of the CNPG Cluster and any errors that may have occurred.
	defer func() {
		if syncErr := r.syncStatus(ctx, postgresCluster, cnpgCluster, err); syncErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to sync status in deferred function: %w", syncErr))
		}
	}()

	// 2. Load the referenced PostgresClusterClass.
	postgresClusterClass := &enterprisev4.PostgresClusterClass{}
	if getClusterClassErr := r.Get(ctx, client.ObjectKey{Name: postgresCluster.Spec.Class}, postgresClusterClass); getClusterClassErr != nil {
		logger.Error(getClusterClassErr, "Unable to fetch referenced PostgresClusterClass", "className", postgresCluster.Spec.Class)
		r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterClassNotFound", getClusterClassErr.Error())
		return res, getClusterClassErr
	}

	// 3. Create the merged configuration by overlaying PostgresClusterSpec on top of PostgresClusterClass defaults.
	mergedConfig := r.getMergedConfig(postgresClusterClass, postgresCluster)

	// 4. Build the desired CNPG Cluster spec based on the merged configuration.
	desiredSpec := r.buildCNPGClusterSpec(mergedConfig)

	// 5. If CNPG cluster doesn't exist, create it.
	existingCNPG := &cnpgv1.Cluster{}
	getCNPGClusterErr := r.Get(ctx, types.NamespacedName{Name: postgresCluster.Name, Namespace: postgresCluster.Namespace}, existingCNPG)
	if apierrors.IsNotFound(getCNPGClusterErr) {
		logger.Info("CNPG Cluster not found, creating:", "name", postgresCluster.Name)
		cnpgCluster = r.buildCNPGCluster(postgresCluster, mergedConfig)
		if buildCNPGClusterErr := r.Create(ctx, cnpgCluster); buildCNPGClusterErr != nil {
			logger.Error(buildCNPGClusterErr, "Failed to create CNPG Cluster")
			r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterBuildFailed", buildCNPGClusterErr.Error())
			return res, buildCNPGClusterErr
		}
		r.setCondition(postgresCluster, "Ready", metav1.ConditionTrue, "ClusterBuildSucceeded", fmt.Sprintf("CNPG cluster build Succeeded: %s", postgresCluster.Name))
		logger.Info("CNPG Cluster created successfully, requeueing for status update", "name", postgresCluster.Name)
		return ctrl.Result{RequeueAfter: retryDelaytimer}, nil
	} else if getCNPGClusterErr != nil {
		logger.Error(getCNPGClusterErr, "Failed to get CNPG Cluster")
		r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterGetFailed", getCNPGClusterErr.Error())
		return res, getCNPGClusterErr
	} else {
		cnpgCluster = existingCNPG
	}

	// 6. Synchronize the existing CNPG Cluster spec with the desired configuration.
	if err := r.Get(ctx, types.NamespacedName{Name: cnpgCluster.Name, Namespace: cnpgCluster.Namespace}, cnpgCluster); err != nil {
		logger.Error(err, "Failed to fetch CNPG Cluster for update check", "name", cnpgCluster.Name)
		r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterGetFailed", fmt.Sprintf("Failed to fetch CNPG Cluster for update check: %v", err))
		return res, err
	}
	if !equality.Semantic.DeepEqual(cnpgCluster.Spec, desiredSpec) {
		logger.Info("Desired CNPG Cluster spec is different from the current spec, need to patch", "name", cnpgCluster.Name)
		originalCluster := cnpgCluster.DeepCopy()
		cnpgCluster.Spec = desiredSpec
		if patchCNPGClusterErr := r.Patch(ctx, cnpgCluster, client.MergeFrom(originalCluster)); patchCNPGClusterErr != nil {
			if apierrors.IsConflict(patchCNPGClusterErr) {
				logger.Info("Conflict occurred while updating CNPG Cluster, requeueing", "name", cnpgCluster.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(patchCNPGClusterErr, "Failed to patch CNPG Cluster", "name", cnpgCluster.Name)
			r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterUpdateFailed", patchCNPGClusterErr.Error())
			return res, patchCNPGClusterErr
		}
	}
	// 7. Reconcile Connection Pooler if enabled in class
	requeuePooler, poolerErr := r.reconcileConnectionPooler(ctx, postgresCluster, postgresClusterClass, cnpgCluster)
	if poolerErr != nil {
		logger.Error(poolerErr, "Failed to reconcile connection pooler")
		r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "PoolerReconciliationFailed", poolerErr.Error())
		return res, poolerErr
	}
	if requeuePooler {
		return ctrl.Result{RequeueAfter: retryDelaytimer}, nil
	}

	// 8. Report progress back to the user and manage the reconciliation lifecycle.
	logger.Info("Reconciliation completed successfully", "name", postgresCluster.Name)
	r.setCondition(postgresCluster, "Ready", metav1.ConditionTrue, "ClusterUpdateSucceeded", fmt.Sprintf("Reconciliation completed successfully: %s", postgresCluster.Name))
	return res, nil
}

// getMergedConfig merges the configuration from the PostgresClusterClass into the PostgresClusterSpec, giving precedence to the PostgresClusterSpec values.
func (r *PostgresClusterReconciler) getMergedConfig(clusterClass *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) *enterprisev4.PostgresClusterSpec {
	resultConfig := cluster.Spec.DeepCopy()
	classDefaults := clusterClass.Spec.Config

	if resultConfig.Instances == nil {
		resultConfig.Instances = classDefaults.Instances
	}
	if resultConfig.PostgresVersion == nil {
		resultConfig.PostgresVersion = classDefaults.PostgresVersion
	}
	if resultConfig.Resources == nil {
		resultConfig.Resources = classDefaults.Resources
	}
	if resultConfig.Storage == nil {
		resultConfig.Storage = classDefaults.Storage
	}
	if len(resultConfig.PostgreSQLConfig) == 0 {
		resultConfig.PostgreSQLConfig = classDefaults.PostgreSQLConfig
	}
	if len(resultConfig.PgHBA) == 0 {
		resultConfig.PgHBA = classDefaults.PgHBA
	}

	// Ensure that maps and slices are initialized to empty if they are still nil after merging, to prevent potential nil pointer dereferences later on.
	if resultConfig.PostgreSQLConfig == nil {
		resultConfig.PostgreSQLConfig = make(map[string]string)
	}
	if resultConfig.PgHBA == nil {
		resultConfig.PgHBA = make([]string, 0)
	}
	// Ensure that Resources is initialized to an empty struct if it's still nil after merging, to prevent potential nil pointer dereferences later on.
	if resultConfig.Resources == nil {
		resultConfig.Resources = &corev1.ResourceRequirements{}
	}

	return resultConfig
}

// buildCNPGClusterSpec builds the desired CNPG ClusterSpec and returns an error if mandatory fields are missing.
func (r *PostgresClusterReconciler) buildCNPGClusterSpec(mergedConfig *enterprisev4.PostgresClusterSpec) cnpgv1.ClusterSpec {

	// 3. Build the Spec
	spec := cnpgv1.ClusterSpec{
		ImageName: fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *mergedConfig.PostgresVersion),
		Instances: int(*mergedConfig.Instances),
		PostgresConfiguration: cnpgv1.PostgresConfiguration{
			Parameters: mergedConfig.PostgreSQLConfig,
			PgHBA:      mergedConfig.PgHBA,
		},
		Bootstrap: &cnpgv1.BootstrapConfiguration{
			InitDB: &cnpgv1.BootstrapInitDB{
				Database: "postgres",
				Owner:    "postgres",
			},
		},
		StorageConfiguration: cnpgv1.StorageConfiguration{
			Size: mergedConfig.Storage.String(),
		},
		Resources: *mergedConfig.Resources,
	}

	return spec
}

// build CNPGCluster builds the CNPG Cluster object based on the PostgresCluster resource and merged configuration.
func (r *PostgresClusterReconciler) buildCNPGCluster(postgresCluster *enterprisev4.PostgresCluster, mergedConfig *enterprisev4.PostgresClusterSpec) *cnpgv1.Cluster {

	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresCluster.Name,
			Namespace: postgresCluster.Namespace,
		},
		Spec: r.buildCNPGClusterSpec(mergedConfig),
	}

	ctrl.SetControllerReference(postgresCluster, cnpgCluster, r.Scheme)
	return cnpgCluster
}

// poolerResourceName returns the CNPG Pooler resource name for a given cluster and type (rw/ro).
func poolerResourceName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s-pooler-%s", clusterName, poolerType)
}

// isConnectionPoolerEnabled determines if connection pooler should be active.
func (r *PostgresClusterReconciler) isConnectionPoolerEnabled(class *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) bool {
	if cluster.Spec.ConnectionPoolerEnabled != nil {
		return *cluster.Spec.ConnectionPoolerEnabled
	}

	return class.Spec.Config.ConnectionPoolerEnabled != nil &&
		*class.Spec.Config.ConnectionPoolerEnabled
}

// reconcileConnectionPooler creates or deletes CNPG Pooler resources based on the effective enabled state.
// Returns (requeue, error) — requeue is true when poolers were just created and may not be ready yet.
func (r *PostgresClusterReconciler) reconcileConnectionPooler(
	ctx context.Context,
	postgresCluster *enterprisev4.PostgresCluster,
	class *enterprisev4.PostgresClusterClass,
	cnpgCluster *cnpgv1.Cluster,
) (bool, error) {
	logger := logs.FromContext(ctx)

	if !r.isConnectionPoolerEnabled(class, postgresCluster) {
		// Skip deletion if the cluster is not healthy — owner references handle cleanup via GC.
		if cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy {
			return false, nil
		}
		if err := r.deleteConnectionPoolers(ctx, postgresCluster); err != nil {
			return false, err
		}
		postgresCluster.Status.ConnectionPoolerStatus = nil
		meta.RemoveStatusCondition(&postgresCluster.Status.Conditions, "PoolerReady")
		return false, nil
	}

	if cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy {
		logger.Info("CNPG Cluster not healthy, waiting before creating poolers")
		r.setCondition(postgresCluster, "PoolerReady", metav1.ConditionFalse, "ClusterNotHealthy", "Waiting for CNPG cluster to become healthy before creating poolers")
		return false, nil
	}

	if class.Spec.CNPG == nil || class.Spec.CNPG.ConnectionPooler == nil {
		logger.Info("Connection pooler enabled but config missing in class", "class", class.Name)
		r.setCondition(postgresCluster, "PoolerReady", metav1.ConditionFalse, "PoolerConfigMissing", fmt.Sprintf("Connection pooler is enabled but cnpg.connectionPooler config is missing in class %s", class.Name))
		return false, nil
	}

	// Create/Update RW Pooler
	if err := r.ensureConnectionPooler(ctx, postgresCluster, class, cnpgCluster, "rw"); err != nil {
		return false, fmt.Errorf("failed to reconcile RW pooler: %w", err)
	}

	// Create/Update RO Pooler
	if err := r.ensureConnectionPooler(ctx, postgresCluster, class, cnpgCluster, "ro"); err != nil {
		return false, fmt.Errorf("failed to reconcile RO pooler: %w", err)
	}

	// Check if poolers are ready — requeue if they're still provisioning.
	poolersReady := r.syncPoolerStatus(ctx, postgresCluster)
	return !poolersReady, nil
}

// deleteConnectionPoolers removes RW and RO pooler resources if they exist.
func (r *PostgresClusterReconciler) deleteConnectionPoolers(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster) error {
	logger := logs.FromContext(ctx)

	for _, poolerType := range []string{"rw", "ro"} {
		poolerName := poolerResourceName(postgresCluster.Name, poolerType)
		pooler := &cnpgv1.Pooler{}

		err := r.Get(ctx, types.NamespacedName{
			Name:      poolerName,
			Namespace: postgresCluster.Namespace,
		}, pooler)

		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to get pooler %s: %w", poolerName, err)
		}

		logger.Info("Deleting CNPG Pooler", "name", poolerName)
		if err := r.Delete(ctx, pooler); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete pooler %s: %w", poolerName, err)
		}
	}

	return nil
}

// ensureConnectionPooler creates a CNPG Pooler resource if it doesn't exist.
// ensureConnectionPooler creates a CNPG Pooler resource if it doesn't exist.
func (r *PostgresClusterReconciler) ensureConnectionPooler(
	ctx context.Context,
	postgresCluster *enterprisev4.PostgresCluster,
	class *enterprisev4.PostgresClusterClass,
	cnpgCluster *cnpgv1.Cluster,
	poolerType string,
) error {
	poolerName := poolerResourceName(postgresCluster.Name, poolerType)

	existingPooler := &cnpgv1.Pooler{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      poolerName,
		Namespace: postgresCluster.Namespace,
	}, existingPooler)

	if apierrors.IsNotFound(err) {
		logs.FromContext(ctx).Info("Creating CNPG Pooler", "name", poolerName, "type", poolerType)
		r.setCondition(postgresCluster, "PoolerReady", metav1.ConditionFalse, "PoolerCreating", fmt.Sprintf("Creating %s pooler", poolerType))
		pooler := r.buildCNPGPooler(postgresCluster, class, cnpgCluster, poolerType)
		return r.Create(ctx, pooler)
	}

	return err
}

// buildCNPGPooler constructs a CNPG Pooler object.
func (r *PostgresClusterReconciler) buildCNPGPooler(
	postgresCluster *enterprisev4.PostgresCluster,
	class *enterprisev4.PostgresClusterClass,
	cnpgCluster *cnpgv1.Cluster,
	poolerType string,
) *cnpgv1.Pooler {
	cfg := class.Spec.CNPG.ConnectionPooler
	poolerName := poolerResourceName(postgresCluster.Name, poolerType)

	instances := *cfg.Instances
	mode := cnpgv1.PgBouncerPoolMode(*cfg.Mode)

	pooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolerName,
			Namespace: postgresCluster.Namespace,
		},
		Spec: cnpgv1.PoolerSpec{
			Cluster: cnpgv1.LocalObjectReference{
				Name: cnpgCluster.Name,
			},
			Instances: &instances,
			Type:      cnpgv1.PoolerType(poolerType),
			PgBouncer: &cnpgv1.PgBouncerSpec{
				PoolMode:   mode,
				Parameters: cfg.Config,
			},
		},
	}

	ctrl.SetControllerReference(postgresCluster, pooler, r.Scheme)
	return pooler
}

// syncStatus maps CNPG Cluster state to PostgresCluster object.
func (r *PostgresClusterReconciler) syncStatus(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, err error) error {
	// will use Patch as we did for main reconciliation loop.
	latestPGCluster := client.MergeFrom(postgresCluster.DeepCopy())

	// If there's an error, we set the status to Error and include the error message in the condition.
	if err != nil {
		postgresCluster.Status.Phase = "Error"
		r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "Error", fmt.Sprintf("Error during reconciliation: %v", err))
		// CNPG not existing, set status to Pending. Direct running `switch` without CNPG cluster in place will cause a panic, so we need to check for that first.
	} else if cnpgCluster == nil {
		postgresCluster.Status.Phase = "Pending"
		r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterNotFound", "Underlying CNPG cluster object has not been created yet")
		// Cluster exists, map the CNPG Cluster status to our PostgresCluster status.
	} else {
		switch cnpgCluster.Status.Phase {
		case cnpgv1.PhaseHealthy:
			postgresCluster.Status.Phase = "Ready"
			r.setCondition(postgresCluster, "Ready", metav1.ConditionTrue, "ClusterHealthy", "CNPG cluster is in healthy state")
		case cnpgv1.PhaseUnrecoverable:
			postgresCluster.Status.Phase = "Failed"
			r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterCreationFailed", "CNPG cluster is in unrecoverable state")
		case "":
			postgresCluster.Status.Phase = "Pending"
			r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterPending", "CNPG cluster is pending creation")
		default:
			postgresCluster.Status.Phase = "Provisioning"
			r.setCondition(postgresCluster, "Ready", metav1.ConditionFalse, "ClusterProvisioning", "CNPG cluster is being provisioned")
		}
		// Set the reference to the CNPG Cluster in the status.
		postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
			APIVersion: "postgresql.cnpg.io/v1",
			Kind:       "Cluster",
			Namespace:  cnpgCluster.Namespace,
			Name:       cnpgCluster.Name,
			UID:        cnpgCluster.UID,
		}

		// ConnectionPoolerStatus and PoolerReady condition are set by reconcileConnectionPooler.
	}

	if patchErr := r.Status().Patch(ctx, postgresCluster, latestPGCluster); patchErr != nil {
		return fmt.Errorf("failed to patch PostgresCluster status: %w", patchErr)
	}
	return nil
}

// setCondition sets a condition on the PostgresCluster status.
func (r *PostgresClusterReconciler) setCondition(postgresCluster *enterprisev4.PostgresCluster, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&postgresCluster.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: postgresCluster.Generation,
	})
}

// syncPoolerStatus populates ConnectionPoolerStatus and the PoolerReady condition.
// It returns true when all poolers are ready, false otherwise.
// The caller decides how pooler readiness affects the overall phase.
func (r *PostgresClusterReconciler) syncPoolerStatus(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster) bool {
	rwPooler := &cnpgv1.Pooler{}
	rwErr := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, "rw"),
		Namespace: postgresCluster.Namespace,
	}, rwPooler)

	roPooler := &cnpgv1.Pooler{}
	roErr := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, "ro"),
		Namespace: postgresCluster.Namespace,
	}, roPooler)

	postgresCluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{
		Enabled: true,
	}

	rwReady := r.isPoolerReady(rwPooler, rwErr)
	roReady := r.isPoolerReady(roPooler, roErr)

	if rwReady && roReady {
		rwDesired, rwScheduled := r.getPoolerInstanceCount(rwPooler)
		roDesired, roScheduled := r.getPoolerInstanceCount(roPooler)
		r.setCondition(postgresCluster, "PoolerReady", metav1.ConditionTrue, "AllInstancesReady", fmt.Sprintf("RW: %d/%d, RO: %d/%d", rwScheduled, rwDesired, roScheduled, roDesired))
		return true
	}

	rwStatus := r.getPoolerStatusString(rwPooler, rwErr)
	roStatus := r.getPoolerStatusString(roPooler, roErr)
	r.setCondition(postgresCluster, "PoolerReady", metav1.ConditionFalse, "PoolersNotReady", fmt.Sprintf("RW: %s, RO: %s", rwStatus, roStatus))
	return false
}

// isPoolerReady checks if a pooler has all instances scheduled.
// Note: CNPG PoolerStatus only tracks scheduled instances, not ready pods.
func (r *PostgresClusterReconciler) isPoolerReady(pooler *cnpgv1.Pooler, err error) bool {
	if err != nil {
		return false
	}
	desiredInstances := int32(1)
	if pooler.Spec.Instances != nil {
		desiredInstances = *pooler.Spec.Instances
	}
	return pooler.Status.Instances >= desiredInstances
}

// getPoolerInstanceCount returns the number of scheduled instances for a pooler.
func (r *PostgresClusterReconciler) getPoolerInstanceCount(pooler *cnpgv1.Pooler) (desired int32, scheduled int32) {
	desired = int32(1)
	if pooler.Spec.Instances != nil {
		desired = *pooler.Spec.Instances
	}
	return desired, pooler.Status.Instances
}

// getPoolerStatusString returns a human-readable status string for a pooler.
func (r *PostgresClusterReconciler) getPoolerStatusString(pooler *cnpgv1.Pooler, err error) string {
	if apierrors.IsNotFound(err) {
		return "not found"
	}
	if err != nil {
		return "error"
	}
	desired, scheduled := r.getPoolerInstanceCount(pooler)
	return fmt.Sprintf("%d/%d", scheduled, desired)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresCluster{}).
		Owns(&cnpgv1.Cluster{}).
		Owns(&cnpgv1.Pooler{}).
		Named("postgresCluster").
		Complete(r)
}
