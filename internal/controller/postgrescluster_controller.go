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
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logs "sigs.k8s.io/controller-runtime/pkg/log"
)

// PostgresClusterReconciler reconciles a PostgresCluster object
type PostgresClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Used for applying changes both from PostgresCluster and PostgresClusterClass.
type MergedConfig struct {
	Spec *enterprisev4.PostgresClusterSpec
	CNPG *enterprisev4.CNPGConfig
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusterclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers/status,verbs=get

// Main reconciliation loop for PostgresCluster.
func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logs.FromContext(ctx)
	logger.Info("Reconciling PostgresCluster", "name", req.Name, "namespace", req.Namespace)

	var cnpgCluster *cnpgv1.Cluster
	var poolerEnabled bool
	var postgresSecretName string
	secret := &corev1.Secret{}

	// 1. Fetch the PostgresCluster instance, stop, if not found.
	postgresCluster := &enterprisev4.PostgresCluster{}
	if getPGClusterErr := r.Get(ctx, req.NamespacedName, postgresCluster); getPGClusterErr != nil {
		if apierrors.IsNotFound(getPGClusterErr) {
			logger.Info("PostgresCluster deleted, skipping reconciliation")
			return ctrl.Result{}, nil
		}
		logger.Error(getPGClusterErr, "Unable to fetch PostgresCluster")
		return ctrl.Result{}, getPGClusterErr
	}
	if postgresCluster.Status.Resources == nil {
		postgresCluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}

	// helper function to update status with less boilerplate.
	updateStatus := func(conditionType conditionTypes, status metav1.ConditionStatus, reason conditionReasons, message string, clusterPhase reconcileClusterPhases) error {
		return (r.updateStatus(ctx, postgresCluster, conditionType, status, reason, message, clusterPhase))
	}

	// finalizer handling must be done before any other processing, to ensure cleanup on deletion and to prevent creating CNPG clusters for PostgresCluster instances that are being deleted.
	finalizerErr := r.handleFinalizer(ctx, postgresCluster, secret)
	if finalizerErr != nil {
		if apierrors.IsNotFound(finalizerErr) {
			logger.Info("PostgresCluster already deleted, skipping finalizer update")
			return ctrl.Result{}, nil
		}
		logger.Error(finalizerErr, "Failed to handle finalizer")
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterDeleteFailed, fmt.Sprintf("Failed to delete resources during cleanup: %v", finalizerErr), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, finalizerErr
	}
	if postgresCluster.GetDeletionTimestamp() != nil {
		logger.Info("PostgresCluster is being deleted, cleanup complete")
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(postgresCluster, postgresClusterFinalizerName) {
		controllerutil.AddFinalizer(postgresCluster, postgresClusterFinalizerName)
		if err := r.Update(ctx, postgresCluster); err != nil {
			if apierrors.IsConflict(err) {
				logger.Info("Conflict while adding finalizer, will retry on next reconcile")
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, "Failed to add finalizer to PostgresCluster")
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		logger.Info("Finalizer added successfully")
		return ctrl.Result{}, nil
	}

	// 2. Load the referenced PostgresClusterClass.
	postgresClusterClass := &enterprisev4.PostgresClusterClass{}
	if getClusterClassErr := r.Get(ctx, client.ObjectKey{Name: postgresCluster.Spec.Class}, postgresClusterClass); getClusterClassErr != nil {
		logger.Error(getClusterClassErr, "Unable to fetch referenced PostgresClusterClass", "className", postgresCluster.Spec.Class)
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterClassNotFound, fmt.Sprintf("ClusterClass %s not found: %v", postgresCluster.Spec.Class, getClusterClassErr), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, getClusterClassErr
	}

	// 3. Create the merged configuration by overlaying PostgresClusterSpec on top of PostgresClusterClass defaults.
	mergedConfig, mergeErr := r.getMergedConfig(postgresClusterClass, postgresCluster)
	if mergeErr != nil {
		logger.Error(mergeErr, "Failed to merge PostgresCluster configuration")
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonInvalidConfiguration, fmt.Sprintf("Failed to merge configuration: %v", mergeErr), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, mergeErr
	}

	// 4. Ensure PostgresCluster secret exists before creating CNPG cluster and create if required.
	if postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.SecretRef != nil {
		postgresSecretName = postgresCluster.Status.Resources.SecretRef.Name
		logger.Info("Using existing secret from status", "name", postgresSecretName)
	} else {
		postgresSecretName = fmt.Sprintf("%s%s", postgresCluster.Name, defaultSecretSuffix)
		logger.Info("Generating new secret name", "name", postgresSecretName)
	}

	postgresClusterSecretExists, secretExistErr := r.clusterSecretExists(ctx, postgresCluster.Namespace, postgresSecretName, secret)
	if secretExistErr != nil {
		logger.Error(secretExistErr, "Failed to check if PostgresCluster secret exists", "name", postgresSecretName)
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonUserSecretFailed, fmt.Sprintf("Failed to check secret existence: %v", secretExistErr), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, secretExistErr
	}
	if !postgresClusterSecretExists {
		logger.Info("Creating PostgresCluster secret", "name", postgresSecretName)
		if generateSecretErr := r.generateSecret(ctx, postgresCluster, postgresSecretName, secret); generateSecretErr != nil {
			logger.Error(generateSecretErr, "Failed to ensure PostgresCluster secret", "name", postgresSecretName)
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonUserSecretFailed, fmt.Sprintf("Failed to generate PostgresCluster secret: %v", generateSecretErr), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, generateSecretErr
		}
		if err := r.Status().Update(ctx, postgresCluster); err != nil {
			if apierrors.IsConflict(err) {
				logger.Info("Conflict after secret creation, will requeue")
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, "Failed to update status after secret creation")
			return ctrl.Result{}, err
		}
		logger.Info("SecretRef persisted to status")
	}
	// We need to restore OwnerReference for existing secret, if it was removed, otherwise secret will be orphaned
	hasOwnerRef, ownerRefErr := controllerutil.HasOwnerReference(secret.GetOwnerReferences(), postgresCluster, r.Scheme)

	if ownerRefErr != nil {
		logger.Error(ownerRefErr, "Failed to check owner reference on Secret")
		return ctrl.Result{}, fmt.Errorf("failed to check owner reference on secret: %w", ownerRefErr)
	}

	if postgresClusterSecretExists && !hasOwnerRef {
		logger.Info("Connecting existing secret to PostgresCluster by adding owner reference", "name", postgresSecretName)
		originalSecret := secret.DeepCopy()
		if err := ctrl.SetControllerReference(postgresCluster, secret, r.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set controller reference on existing secret: %w", err)
		}
		logger.Info("Successfully connected existing Secret to PostgresCluster", "secret", secret.Name, "cluster", postgresCluster.Name)

		if err := r.patchObject(ctx, originalSecret, secret, "Secret"); err != nil {
			logger.Error(err, "failed to patch existing secret with controller reference.")
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonSuperUserSecretFailed, fmt.Sprintf("Failed to patch existing secret: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		logger.Info("Existing secret linked successfully")
	}

	if postgresCluster.Status.Resources.SecretRef == nil {
		postgresCluster.Status.Resources.SecretRef = &corev1.LocalObjectReference{Name: postgresSecretName}
	}

	// 5. Build the desired CNPG Cluster spec based on the merged configuration.
	desiredSpec := r.buildCNPGClusterSpec(mergedConfig, postgresSecretName)

	// 6. Fetch existing CNPG Cluster or create it if it doesn't exist yet.
	existingCNPG := &cnpgv1.Cluster{}
	err := r.Get(ctx, types.NamespacedName{Name: postgresCluster.Name, Namespace: postgresCluster.Namespace}, existingCNPG)
	switch {
	case apierrors.IsNotFound(err):
		// CNPG Cluster doesn't exist, create it and requeue for status update.
		logger.Info("CNPG Cluster not found, creating", "name", postgresCluster.Name)
		newCluster := r.buildCNPGCluster(postgresCluster, mergedConfig, postgresSecretName)
		if err = r.Create(ctx, newCluster); err != nil {
			logger.Error(err, "Failed to create CNPG Cluster")
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterBuildFailed, fmt.Sprintf("Failed to create CNPG Cluster: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterBuildSucceeded, "CNPG Cluster created", pendingClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		logger.Info("CNPG Cluster created successfully, requeueing for status update", "name", postgresCluster.Name)
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	case err != nil:
		logger.Error(err, "Failed to get CNPG Cluster")
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterGetFailed, fmt.Sprintf("Failed to get CNPG Cluster: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// 7. If CNPG Cluster exists, compare the current spec with the desired spec and update if necessary.
	cnpgCluster = existingCNPG
	currentNormalizedSpec := normalizeCNPGClusterSpec(cnpgCluster.Spec, mergedConfig.Spec.PostgreSQLConfig)
	desiredNormalizedSpec := normalizeCNPGClusterSpec(desiredSpec, mergedConfig.Spec.PostgreSQLConfig)

	if !equality.Semantic.DeepEqual(currentNormalizedSpec, desiredNormalizedSpec) {
		logger.Info("Detected drift in CNPG Cluster spec, patching", "name", cnpgCluster.Name)
		originalCluster := cnpgCluster.DeepCopy()
		cnpgCluster.Spec = desiredSpec

		switch patchErr := r.patchObject(ctx, originalCluster, cnpgCluster, "CNPGCluster"); {
		case apierrors.IsConflict(patchErr):
			logger.Info("Conflict occurred while updating CNPG Cluster, requeueing", "name", cnpgCluster.Name)
			return ctrl.Result{Requeue: true}, nil

		case patchErr != nil:
			logger.Error(patchErr, "Failed to patch CNPG Cluster", "name", cnpgCluster.Name)
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterPatchFailed, fmt.Sprintf("Failed to patch CNPG Cluster: %v", patchErr), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, patchErr

		default:
			logger.Info("CNPG Cluster patched successfully, requeueing for status update", "name", cnpgCluster.Name)
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
	}

	// 7a. Reconcile ManagedRoles from PostgresCluster to CNPG Cluster
	if err := r.reconcileManagedRoles(ctx, postgresCluster, cnpgCluster); err != nil {
		logger.Error(err, "Failed to reconcile managed roles")
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonManagedRolesFailed, fmt.Sprintf("Failed to reconcile managed roles: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// 7b. Reconcile Connection Pooler
	poolerEnabled = mergedConfig.Spec.ConnectionPoolerEnabled != nil && *mergedConfig.Spec.ConnectionPoolerEnabled
	switch {
	case !poolerEnabled:
		// Pooler disabled — delete if they exist
		if err := r.deleteConnectionPoolers(ctx, postgresCluster); err != nil {
			logger.Error(err, "Failed to delete connection poolers")
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed, fmt.Sprintf("Failed to delete connection poolers: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		postgresCluster.Status.ConnectionPoolerStatus = nil
		meta.RemoveStatusCondition(&postgresCluster.Status.Conditions, string(poolerReady))

	case !r.poolerExists(ctx, postgresCluster, readWriteEndpoint) || !r.poolerExists(ctx, postgresCluster, readOnlyEndpoint):
		if mergedConfig.CNPG.ConnectionPooler == nil {
			logger.Info("Connection pooler enabled but no config found in class or cluster spec, skipping",
				"class", postgresCluster.Spec.Class,
				"cluster", postgresCluster.Name,
			)
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerConfigMissing,
				fmt.Sprintf("Connection pooler is enabled but no config found in class %q or cluster %q",
					postgresCluster.Spec.Class, postgresCluster.Name),
				failedClusterPhase,
			); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, nil
		}
		if cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy {
			logger.Info("CNPG Cluster not healthy yet, pending pooler creation", "clusterPhase", cnpgCluster.Status.Phase)
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonCNPGClusterNotHealthy,
				"Waiting for CNPG cluster to become healthy before creating poolers", pendingClusterPhase,
			); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
		if err := r.createOrUpdateConnectionPooler(ctx, postgresCluster, mergedConfig, cnpgCluster); err != nil {
			logger.Error(err, "Failed to reconcile connection pooler")
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to reconcile connection pooler: %v", err), failedClusterPhase,
			); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		logger.Info("Connection Poolers created, requeueing to check readiness")
		if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerCreating,
			"Connection poolers are being provisioned", provisioningClusterPhase,
		); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	case !r.arePoolersReady(ctx, postgresCluster):
		// Poolers exist but not ready yet
		logger.Info("Connection Poolers are not ready yet, requeueing")
		if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerCreating, "Connection poolers are being provisioned", pendingClusterPhase); statusErr != nil {
			if apierrors.IsConflict(statusErr) {
				logger.Info("Conflict updating pooler status, will requeue")
				return ctrl.Result{Requeue: true}, nil
			}
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	default:
		if err := r.syncPoolerStatus(ctx, postgresCluster); err != nil {
			logger.Error(err, "Failed to sync pooler status")
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed, fmt.Sprintf("Failed to sync pooler status: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
	}

	// 8. If CNPG cluster is ready, generate ConfigMap or use existing
	if cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy {
		logger.Info("CNPG Cluster is ready, reconciling ConfigMap for connection details")
		desiredConfigMap, err := r.generateConfigMap(ctx, postgresCluster, cnpgCluster, postgresSecretName)
		if err != nil {
			logger.Error(err, "Failed to generate ConfigMap")
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonConfigMapFailed, fmt.Sprintf("Failed to generate ConfigMap: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      desiredConfigMap.Name,
				Namespace: desiredConfigMap.Namespace,
			},
		}
		createOrUpdateResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
			configMap.Data = desiredConfigMap.Data
			configMap.Annotations = desiredConfigMap.Annotations
			configMap.Labels = desiredConfigMap.Labels

			if !metav1.IsControlledBy(configMap, postgresCluster) {
				if err := ctrl.SetControllerReference(postgresCluster, configMap, r.Scheme); err != nil {
					return fmt.Errorf("set controller reference failed: %w", err)
				}
			}
			return nil
		})

		if err != nil {
			logger.Error(err, "Failed to reconcile ConfigMap", "name", desiredConfigMap.Name)
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonConfigMapFailed, fmt.Sprintf("Failed to reconcile ConfigMap: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		switch createOrUpdateResult {
		case controllerutil.OperationResultCreated:
			logger.Info("ConfigMap created", "name", desiredConfigMap.Name)
		case controllerutil.OperationResultUpdated:
			logger.Info("ConfigMap updated", "name", desiredConfigMap.Name)
		default:
			logger.Info("ConfigMap unchanged", "name", desiredConfigMap.Name)
		}
		if postgresCluster.Status.Resources.ConfigMapRef == nil {
			postgresCluster.Status.Resources.ConfigMapRef = &corev1.LocalObjectReference{Name: desiredConfigMap.Name}
		}
	}

	// 9. Final status update and sync
	if err := r.syncStatus(ctx, postgresCluster, cnpgCluster); err != nil {
		logger.Error(err, "Failed to sync status")
		if apierrors.IsConflict(err) {
			logger.Info("Conflict during status update, will requeue")
			return ctrl.Result{Requeue: true}, nil // Don't return error, just requeue
		}
		return ctrl.Result{}, fmt.Errorf("failed to sync status: %w", err)
	}
	if cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy && r.arePoolersReady(ctx, postgresCluster) {
		logger.Info("Poolers are ready, syncing pooler status")
		r.syncPoolerStatus(ctx, postgresCluster)
	}
	logger.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

// getMergedConfig merges the configuration from the PostgresClusterClass into the PostgresClusterSpec, giving precedence to the PostgresClusterSpec values.
func (r *PostgresClusterReconciler) getMergedConfig(clusterClass *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) (*MergedConfig, error) {
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

	if resultConfig.Instances == nil || resultConfig.PostgresVersion == nil || resultConfig.Storage == nil {
		return nil, fmt.Errorf("invalid configuration for class %s: instances, postgresVersion and storage are required", clusterClass.Name)
	}

	if resultConfig.PostgreSQLConfig == nil {
		resultConfig.PostgreSQLConfig = make(map[string]string)
	}
	if resultConfig.PgHBA == nil {
		resultConfig.PgHBA = make([]string, 0)
	}
	if resultConfig.Resources == nil {
		resultConfig.Resources = &corev1.ResourceRequirements{}
	}

	return &MergedConfig{
		Spec: resultConfig,
		CNPG: clusterClass.Spec.CNPG,
	}, nil
}

// buildCNPGClusterSpec builds the desired CNPG ClusterSpec.
// IMPORTANT: any field added here must also be added to normalizedCNPGClusterSpec and normalizeCNPGClusterSpec,
// otherwise it will not be included in drift detection and changes will be silently ignored.
func (r *PostgresClusterReconciler) buildCNPGClusterSpec(mergedConfig *MergedConfig, secretName string) cnpgv1.ClusterSpec {

	// 3. Build the Spec
	spec := cnpgv1.ClusterSpec{
		ImageName: fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *mergedConfig.Spec.PostgresVersion),
		Instances: int(*mergedConfig.Spec.Instances),
		PostgresConfiguration: cnpgv1.PostgresConfiguration{
			Parameters: mergedConfig.Spec.PostgreSQLConfig,
			PgHBA:      mergedConfig.Spec.PgHBA,
		},
		SuperuserSecret: &cnpgv1.LocalObjectReference{
			Name: secretName,
		},
		EnableSuperuserAccess: ptr.To(true),

		Bootstrap: &cnpgv1.BootstrapConfiguration{
			InitDB: &cnpgv1.BootstrapInitDB{
				Database: defaultDatabaseName,
				Owner:    superUsername,
				Secret: &cnpgv1.LocalObjectReference{
					Name: secretName,
				},
			},
		},
		StorageConfiguration: cnpgv1.StorageConfiguration{
			Size: mergedConfig.Spec.Storage.String(),
		},
		Resources: *mergedConfig.Spec.Resources,
	}

	return spec
}

// build CNPGCluster builds the CNPG Cluster object based on the PostgresCluster resource and merged configuration.
func (r *PostgresClusterReconciler) buildCNPGCluster(
	postgresCluster *enterprisev4.PostgresCluster,
	mergedConfig *MergedConfig,
	secretName string,
) *cnpgv1.Cluster {
	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      postgresCluster.Name,
			Namespace: postgresCluster.Namespace,
		},
		Spec: r.buildCNPGClusterSpec(mergedConfig, secretName),
	}
	ctrl.SetControllerReference(postgresCluster, cnpgCluster, r.Scheme)
	return cnpgCluster
}

// poolerResourceName returns the CNPG Pooler resource name for a given cluster and type (rw/ro).
func poolerResourceName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s%s%s", clusterName, defaultPoolerSuffix, poolerType)
}

// createOrUpdateConnectionPooler creates or updates CNPG Pooler resources.
func (r *PostgresClusterReconciler) createOrUpdateConnectionPooler(
	ctx context.Context,
	postgresCluster *enterprisev4.PostgresCluster,
	mergedConfig *MergedConfig,
	cnpgCluster *cnpgv1.Cluster,
) error {
	// Create/Update RW Pooler
	if err := r.createConnectionPooler(ctx, postgresCluster, mergedConfig, cnpgCluster, readWriteEndpoint); err != nil {
		return fmt.Errorf("failed to reconcile RW pooler: %w", err)
	}

	// Create/Update RO Pooler
	if err := r.createConnectionPooler(ctx, postgresCluster, mergedConfig, cnpgCluster, readOnlyEndpoint); err != nil {
		return fmt.Errorf("failed to reconcile RO pooler: %w", err)
	}

	return nil
}

// poolerExists checks if a pooler resource exists and returns it
func (r *PostgresClusterReconciler) poolerExists(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, poolerType string) bool {
	pooler := &cnpgv1.Pooler{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, poolerType),
		Namespace: postgresCluster.Namespace,
	}, pooler)

	if apierrors.IsNotFound(err) {
		return false
	}
	if err != nil {
		logs.FromContext(ctx).Error(err, "Failed to check pooler existence", "type", poolerType)
		return false
	}
	return true
}

// deleteConnectionPoolers removes RW and RO pooler resources if they exist.
func (r *PostgresClusterReconciler) deleteConnectionPoolers(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster) error {
	logger := logs.FromContext(ctx)

	for _, poolerType := range []string{readWriteEndpoint, readOnlyEndpoint} {
		poolerName := poolerResourceName(postgresCluster.Name, poolerType)
		exists := r.poolerExists(ctx, postgresCluster, poolerType)
		if !exists {
			continue
		}

		pooler := &cnpgv1.Pooler{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      poolerName,
			Namespace: postgresCluster.Namespace,
		}, pooler); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get pooler %s: %w", poolerName, err)
		}

		logger.Info("Deleting CNPG Pooler", "name", poolerName)
		if err := r.Delete(ctx, pooler); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete pooler %s: %w", poolerName, err)
		}
	}
	return nil
}

// createConnectionPooler creates a CNPG Pooler resource if it doesn't exist.
func (r *PostgresClusterReconciler) createConnectionPooler(
	ctx context.Context,
	postgresCluster *enterprisev4.PostgresCluster,
	mergedConfig *MergedConfig,
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
		r.updateStatus(ctx, postgresCluster, poolerReady, metav1.ConditionFalse, reasonPoolerCreating, fmt.Sprintf("Creating %s pooler", poolerType), pendingClusterPhase)
		pooler := r.buildCNPGPooler(postgresCluster, mergedConfig, cnpgCluster, poolerType)
		return r.Create(ctx, pooler)
	}

	return err
}

// buildCNPGPooler constructs a CNPG Pooler object.
func (r *PostgresClusterReconciler) buildCNPGPooler(
	postgresCluster *enterprisev4.PostgresCluster,
	mergedConfig *MergedConfig,
	cnpgCluster *cnpgv1.Cluster,
	poolerType string,
) *cnpgv1.Pooler {
	cfg := mergedConfig.CNPG.ConnectionPooler
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

// syncStatus maps CNPG Cluster state to PostgresCluster object and handles pooler status.
func (r *PostgresClusterReconciler) syncStatus(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {

	// 1. Set ProvisionerRef
	postgresCluster.Status.ProvisionerRef = &corev1.ObjectReference{
		APIVersion: "postgresql.cnpg.io/v1",
		Kind:       "Cluster",
		Namespace:  cnpgCluster.Namespace,
		Name:       cnpgCluster.Name,
		UID:        cnpgCluster.UID,
	}

	// Map CNPG Phase to PostgresCluster Phase/Conditions
	var clusterPhase reconcileClusterPhases
	var conditionStatus metav1.ConditionStatus
	var reason conditionReasons
	var message string

	switch cnpgCluster.Status.Phase {
	case cnpgv1.PhaseHealthy:
		clusterPhase = readyClusterPhase
		conditionStatus = metav1.ConditionTrue
		reason = reasonCNPGClusterHealthy
		message = "Cluster is up and running"

	case cnpgv1.PhaseFirstPrimary,
		cnpgv1.PhaseCreatingReplica,
		cnpgv1.PhaseWaitingForInstancesToBeActive:
		clusterPhase = provisioningClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGProvisioning
		message = fmt.Sprintf("CNPG cluster provisioning: %s", cnpgCluster.Status.Phase)

	case cnpgv1.PhaseSwitchover:
		clusterPhase = configuringClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGSwitchover
		message = "Cluster changing primary node"

	case cnpgv1.PhaseFailOver:
		clusterPhase = configuringClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGFailingOver
		message = "Pod missing, need to change primary"

	case cnpgv1.PhaseInplacePrimaryRestart,
		cnpgv1.PhaseInplaceDeletePrimaryRestart:
		clusterPhase = configuringClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGRestarting
		message = fmt.Sprintf("CNPG cluster restarting: %s", cnpgCluster.Status.Phase)

	case cnpgv1.PhaseUpgrade,
		cnpgv1.PhaseMajorUpgrade,
		cnpgv1.PhaseUpgradeDelayed,
		cnpgv1.PhaseOnlineUpgrading:
		clusterPhase = configuringClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGUpgrading
		message = fmt.Sprintf("CNPG cluster upgrading: %s", cnpgCluster.Status.Phase)

	case cnpgv1.PhaseApplyingConfiguration:
		clusterPhase = configuringClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGApplyingConfig
		message = "Configuration change is being applied"

	case cnpgv1.PhaseReplicaClusterPromotion:
		clusterPhase = configuringClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGPromoting
		message = "Replica is being promoted to primary"

	case cnpgv1.PhaseWaitingForUser:
		clusterPhase = failedClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGWaitingForUser
		message = "Action from the user is required"

	case cnpgv1.PhaseUnrecoverable:
		clusterPhase = failedClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGUnrecoverable
		message = "Cluster failed, needs manual intervention"

	case cnpgv1.PhaseCannotCreateClusterObjects:
		clusterPhase = failedClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGProvisioningFailed
		message = "Cluster resources cannot be created"

	case cnpgv1.PhaseUnknownPlugin,
		cnpgv1.PhaseFailurePlugin:
		clusterPhase = failedClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGPluginError
		message = fmt.Sprintf("CNPG plugin error: %s", cnpgCluster.Status.Phase)

	case cnpgv1.PhaseImageCatalogError,
		cnpgv1.PhaseArchitectureBinaryMissing:
		clusterPhase = failedClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGImageError
		message = fmt.Sprintf("CNPG image error: %s", cnpgCluster.Status.Phase)

	case "":
		clusterPhase = pendingClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGProvisioning
		message = "CNPG cluster is pending creation"

	default:
		clusterPhase = provisioningClusterPhase
		conditionStatus = metav1.ConditionFalse
		reason = reasonCNPGProvisioning
		message = fmt.Sprintf("CNPG cluster clusterPhase: %s", cnpgCluster.Status.Phase)
	}

	return r.updateStatus(ctx, postgresCluster, clusterReady, conditionStatus, reason, message, clusterPhase)
}

// updateStatus sets the clusterPhase, condition and persists the status to Kubernetes.
func (r *PostgresClusterReconciler) updateStatus(
	ctx context.Context,
	postgresCluster *enterprisev4.PostgresCluster,
	conditionType conditionTypes,
	status metav1.ConditionStatus,
	reason conditionReasons,
	message string,
	clusterPhase reconcileClusterPhases,
) error {
	postgresCluster.Status.Phase = string(clusterPhase)
	meta.SetStatusCondition(&postgresCluster.Status.Conditions, metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: postgresCluster.Generation,
	})
	if err := r.Status().Update(ctx, postgresCluster); err != nil {
		return fmt.Errorf("failed to update PostgresCluster status: %w", err)
	}
	return nil
}

// syncPoolerStatus populates ConnectionPoolerStatus and the PoolerReady condition.
// Called only when poolers are confirmed ready by the reconciler.
func (r *PostgresClusterReconciler) syncPoolerStatus(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster) error {
	rwPooler := &cnpgv1.Pooler{}
	if rwErr := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, readWriteEndpoint),
		Namespace: postgresCluster.Namespace,
	}, rwPooler); rwErr != nil {
		return rwErr
	}

	roPooler := &cnpgv1.Pooler{}
	if roErr := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, readOnlyEndpoint),
		Namespace: postgresCluster.Namespace,
	}, roPooler); roErr != nil {
		return roErr
	}

	postgresCluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{
		Enabled: true,
	}

	rwDesired, rwScheduled := r.getPoolerInstanceCount(rwPooler)
	roDesired, roScheduled := r.getPoolerInstanceCount(roPooler)

	if err := r.updateStatus(
		ctx,
		postgresCluster,
		poolerReady,
		metav1.ConditionTrue,
		reasonAllInstancesReady,
		fmt.Sprintf("%s: %d/%d, %s: %d/%d", readWriteEndpoint, rwScheduled, rwDesired, readOnlyEndpoint, roScheduled, roDesired),
		readyClusterPhase); err != nil {
		return err
	}
	return nil
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

// arePoolersReady checks if both RW and RO poolers have all instances scheduled.
func (r *PostgresClusterReconciler) arePoolersReady(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster) bool {
	rwPooler := &cnpgv1.Pooler{}
	rwErr := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, readWriteEndpoint),
		Namespace: postgresCluster.Namespace,
	}, rwPooler)

	roPooler := &cnpgv1.Pooler{}
	roErr := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, readOnlyEndpoint),
		Namespace: postgresCluster.Namespace,
	}, roPooler)

	return r.isPoolerReady(rwPooler, rwErr) && r.isPoolerReady(roPooler, roErr)
}

// reconcileManagedRoles synchronizes ManagedRoles from PostgresCluster spec to CNPG Cluster managed.roles using diff-based patching
func (r *PostgresClusterReconciler) reconcileManagedRoles(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	logger := logs.FromContext(ctx)

	// If no managed roles in PostgresCluster spec, nothing to do for now
	// TODO: Should we remove roles from CNPG if they're removed from PostgresCluster?
	if len(postgresCluster.Spec.ManagedRoles) == 0 {
		logger.Info("No managed roles to reconcile")
		return nil
	}

	// Convert PostgresCluster ManagedRoles to CNPG RoleConfiguration format
	desiredRoles := []cnpgv1.RoleConfiguration{}
	for _, role := range postgresCluster.Spec.ManagedRoles {
		cnpgRole := cnpgv1.RoleConfiguration{
			Name: role.Name,
		}

		if role.Ensure == "absent" {
			cnpgRole.Ensure = cnpgv1.EnsureAbsent
		} else {
			cnpgRole.Ensure = cnpgv1.EnsurePresent
			cnpgRole.Login = true
		}

		if role.PasswordSecretRef != nil {
			cnpgRole.PasswordSecret = &cnpgv1.LocalObjectReference{
				Name: role.PasswordSecretRef.Name,
			}
		}

		desiredRoles = append(desiredRoles, cnpgRole)
	}

	var currentRoles []cnpgv1.RoleConfiguration
	if cnpgCluster.Spec.Managed != nil && cnpgCluster.Spec.Managed.Roles != nil {
		currentRoles = cnpgCluster.Spec.Managed.Roles
	}

	if equality.Semantic.DeepEqual(currentRoles, desiredRoles) {
		logger.Info("CNPG Cluster roles already match desired state, no update needed")
		return nil
	}

	logger.Info("CNPG Cluster roles differ from desired state, updating",
		"currentCount", len(currentRoles),
		"desiredCount", len(desiredRoles))

	originalCluster := cnpgCluster.DeepCopy()

	if cnpgCluster.Spec.Managed == nil {
		cnpgCluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	cnpgCluster.Spec.Managed.Roles = desiredRoles

	if err := r.Patch(ctx, cnpgCluster, client.MergeFrom(originalCluster)); err != nil {
		return fmt.Errorf("failed to patch CNPG Cluster with managed roles: %w", err)
	}

	logger.Info("Successfully updated CNPG Cluster with managed roles", "roleCount", len(desiredRoles))
	return nil
}

// normalizedCNPGClusterSpec is a subset of cnpgv1.ClusterSpec fields that we care about for drift detection.
// Any field that is included in buildCNPGClusterSpec and should be considered for drift detection must be added here, and populated in normalizeCNPGClusterSpec.
func normalizeCNPGClusterSpec(spec cnpgv1.ClusterSpec, customDefinedParameters map[string]string) normalizedCNPGClusterSpec {
	normalizedConf := normalizedCNPGClusterSpec{
		ImageName: spec.ImageName,
		Instances: spec.Instances,
		// Parameters intentionally excluded — CNPG injects defaults that we don't change
		StorageSize: spec.StorageConfiguration.Size,
		Resources:   spec.Resources,
	}

	if len(customDefinedParameters) > 0 {
		normalizedConf.CustomDefinedParameters = make(map[string]string)
		for k := range customDefinedParameters {
			normalizedConf.CustomDefinedParameters[k] = spec.PostgresConfiguration.Parameters[k]
		}
	}
	if len(spec.PostgresConfiguration.PgHBA) > 0 {
		normalizedConf.PgHBA = spec.PostgresConfiguration.PgHBA
	}

	if spec.Bootstrap != nil && spec.Bootstrap.InitDB != nil {
		normalizedConf.DefaultDatabase = spec.Bootstrap.InitDB.Database
		normalizedConf.Owner = spec.Bootstrap.InitDB.Owner
	}
	return normalizedConf
}

// generateConfigMap generates a ConfigMap with connection details for the PostgresCluster.
func (r *PostgresClusterReconciler) generateConfigMap(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, secretName string) (*corev1.ConfigMap, error) {
	configMapName := fmt.Sprintf("%s%s", postgresCluster.Name, defaultConfigMapSuffix)
	if postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.ConfigMapRef != nil {
		configMapName = postgresCluster.Status.Resources.ConfigMapRef.Name
	}

	data := map[string]string{
		"CLUSTER_RW_ENDPOINT":   fmt.Sprintf("%s-rw.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		"CLUSTER_RO_ENDPOINT":   fmt.Sprintf("%s-ro.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		"CLUSTER_R_ENDPOINT":    fmt.Sprintf("%s-r.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		"DEFAULT_CLUSTER_PORT":  defaultPort,
		"SUPER_USER_NAME":       superUsername,
		"SUPER_USER_SECRET_REF": secretName,
	}
	if r.poolerExists(ctx, postgresCluster, readWriteEndpoint) && r.poolerExists(ctx, postgresCluster, readOnlyEndpoint) {
		data["CLUSTER_POOLER_RW_ENDPOINT"] = fmt.Sprintf("%s.%s", poolerResourceName(cnpgCluster.Name, readWriteEndpoint), cnpgCluster.Namespace)
		data["CLUSTER_POOLER_RO_ENDPOINT"] = fmt.Sprintf("%s.%s", poolerResourceName(cnpgCluster.Name, readOnlyEndpoint), cnpgCluster.Namespace)
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: postgresCluster.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "postgrescluster-controller"},
		},
		Data: data,
	}
	if err := ctrl.SetControllerReference(postgresCluster, configMap, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}
	return configMap, nil
}

// generateSecret creates a Kubernetes Secret with credentials for the default postgres user if it doesn't already exist.
func (r *PostgresClusterReconciler) generateSecret(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, secretName string, secret *corev1.Secret) error {
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: postgresCluster.Namespace}, secret)

	// If secret does not exist, create it
	if apierrors.IsNotFound(err) {
		password, err := generatePassword()
		if err != nil {
			return err
		}
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: postgresCluster.Namespace,
			},
			StringData: map[string]string{
				"username": superUsername,
				"password": password,
			},
			Type: corev1.SecretTypeOpaque,
		}
		if err := ctrl.SetControllerReference(postgresCluster, secret, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, secret); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if postgresCluster.Status.Resources == nil {
		postgresCluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}
	postgresCluster.Status.Resources.SecretRef = &corev1.LocalObjectReference{Name: secretName}

	return nil
}

// deleteCNPGCluster deletes the CNPG cluster and its associated resources if they exist.
func (r *PostgresClusterReconciler) deleteCNPGCluster(ctx context.Context, cnpgCluster *cnpgv1.Cluster) error {
	logger := logs.FromContext(ctx)
	// TODO: add logic to decide to delete cluster if one has customer DBs configured, to prevent data loss
	if cnpgCluster == nil {
		logger.Info("CNPG Cluster not found, skipping deletion")
		return nil
	}
	logger.Info("Deleting CNPG Cluster", "name", cnpgCluster.Name)
	if err := r.Delete(ctx, cnpgCluster); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete CNPG Cluster: %w", err)
	}
	return nil
}

// handleFinalizer processes the finalizer logic when a PostgresCluster is being deleted, including cleanup of associated CNPG Cluster and connection poolers based on the specified deletion policy.
func (r *PostgresClusterReconciler) handleFinalizer(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, secret *corev1.Secret) error {
	logger := logs.FromContext(ctx)
	if postgresCluster.GetDeletionTimestamp() == nil {
		logger.Info("PostgresCluster not marked for deletion, skipping finalizer logic")
		return nil
	}
	if !controllerutil.ContainsFinalizer(postgresCluster, postgresClusterFinalizerName) {
		logger.Info("Finalizer not present on PostgresCluster, skipping finalizer logic")
		return nil
	}

	cnpgCluster := &cnpgv1.Cluster{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      postgresCluster.Name,
		Namespace: postgresCluster.Namespace,
	}, cnpgCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cnpgCluster = nil
			logger.Info("CNPG cluster not found during cleanup")
		} else {
			return fmt.Errorf("failed to fetch CNPG cluster during cleanup: %w", err)
		}
	}
	logger.Info("Processing finalizer cleanup for PostgresCluster")

	// Always delete connection poolers if they exist.
	if err := r.deleteConnectionPoolers(ctx, postgresCluster); err != nil {
		logger.Error(err, "Failed to delete connection poolers during cleanup")
		return fmt.Errorf("failed to delete connection poolers: %w", err)
	}

	switch postgresCluster.Spec.ClusterDeletionPolicy {
	case clusterDeletionPolicyDelete:
		logger.Info("ClusterDeletionPolicy is 'Delete', proceeding to delete CNPG Cluster and associated resources")
		if cnpgCluster != nil {
			if err := r.deleteCNPGCluster(ctx, cnpgCluster); err != nil {
				logger.Error(err, "Failed to delete CNPG Cluster during finalizer cleanup")
				return fmt.Errorf("failed to delete CNPG Cluster during finalizer cleanup: %w", err)
			}
		}
		logger.Info("CNPG Cluster not found, skipping deletion")
	case clusterDeletionPolicyRetain:
		logger.Info("ClusterDeletionPolicy is 'Retain', proceeding to remove owner references and retain CNPG Cluster")
		// Remove owner reference from CNPG Cluster to prevent its deletion.
		originalCNPG := cnpgCluster.DeepCopy()
		refRemoved, err := r.removeOwnerRef(postgresCluster, cnpgCluster, "CNPGCluster")
		if err != nil {
			return fmt.Errorf("failed to remove owner reference from CNPG cluster: %w", err)
		}
		if !refRemoved {
			logger.Info("Owner reference already removed/not set from CNPG Cluster, skipping patch")
		}
		if err := r.patchObject(ctx, originalCNPG, cnpgCluster, "CNPGCluster"); err != nil {
			return fmt.Errorf("failed to patch CNPG cluster after removing owner reference: %w", err)
		}
		logger.Info("Removed owner reference from CNPG Cluster")

		// Remove owner reference from Secret to prevent its  deletion.
		if postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.SecretRef != nil {
			secretName := postgresCluster.Status.Resources.SecretRef.Name
			if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: postgresCluster.Namespace}, secret); err != nil {
				if !apierrors.IsNotFound(err) {
					logger.Error(err, "Failed to fetch Secret during cleanup")
					return fmt.Errorf("failed to fetch secret during cleanup: %w", err)
				}
				logger.Info("Secret not found, skipping owner reference removal", "secret", secretName)
			}
			if secret != nil {
				originalSecret := secret.DeepCopy()
				refRemoved, err = r.removeOwnerRef(postgresCluster, secret, "Secret")
				if err != nil {
					return fmt.Errorf("failed to remove owner reference from Secret: %w", err)
				}
				if refRemoved {
					if err := r.patchObject(ctx, originalSecret, secret, "Secret"); err != nil {
						return fmt.Errorf("failed to patch Secret after removing owner reference: %w", err)
					}
				}
				logger.Info("Removed owner reference from Secret")
			}
		}
	default:
		logger.Info("Unknown ClusterDeletionPolicy", "policy", postgresCluster.Spec.ClusterDeletionPolicy)
	}

	// Remove finalizer after successful cleanup
	controllerutil.RemoveFinalizer(postgresCluster, postgresClusterFinalizerName)
	if err := r.Update(ctx, postgresCluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PostgresCluster already deleted, skipping finalizer update")
			return nil
		}
		logger.Error(err, "Failed to remove finalizer from PostgresCluster")
		return fmt.Errorf("failed to remove finalizer: %w", err)
	}

	logger.Info("Finalizer removed, cleanup complete")
	return nil
}

// clusterSecretExists treats any non-NotFound error as absence — the subsequent Create will
// surface the real API error if the cluster has a deeper problem.
func (r *PostgresClusterReconciler) clusterSecretExists(ctx context.Context, namespace, secretName string, secret *corev1.Secret) (clusterSecretExists bool, secretExistErr error) {
	logger := logs.FromContext(ctx)
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		logger.Error(err, "Failed to check secret existence", "secret", secretName)
		return false, err
	}
	logger.Info("Secret already exists", "secret", secretName)
	return true, nil
}

// removeOwnerRef removes the owner reference from the object and returns whether it was removed or not.
func (r *PostgresClusterReconciler) removeOwnerRef(owner client.Object, obj client.Object, objKind objectKind) (bool, error) {
	hasOwnerRef, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), owner, r.Scheme)

	if err != nil {
		return false, fmt.Errorf("failed to check owner reference on %s: %w", objKind, err)
	}
	if !hasOwnerRef {
		return false, nil
	}
	if err := controllerutil.RemoveOwnerReference(owner, obj, r.Scheme); err != nil {
		return false, fmt.Errorf("failed to remove owner reference from %s: %w", objKind, err)
	}
	return true, nil
}

// patchObject attempts to patch the object and treats NotFound as a non-error, since the object may have already been deleted by Kubernetes.
func (r *PostgresClusterReconciler) patchObject(ctx context.Context, original client.Object, obj client.Object, objKind objectKind) error {
	logger := logs.FromContext(ctx)
	if err := r.Patch(ctx, obj, client.MergeFrom(original)); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Object not found, skipping patch", "kind", objKind, "name", obj.GetName())
			return nil
		}
		return fmt.Errorf("failed to patch %s object: %w", objKind, err)
	}
	logger.Info("Patched object successfully", "kind", objKind, "name", obj.GetName())
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresCluster{}).
		Owns(&cnpgv1.Cluster{}).
		Owns(&cnpgv1.Pooler{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Named("postgresCluster").
		Complete(r)
}
