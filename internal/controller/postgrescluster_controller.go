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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logs "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// PostgresClusterReconciler reconciles PostgresCluster resources.
type PostgresClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// EffectiveClusterConfig holds the effective PostgresCluster spec and CNPG settings after class defaults are applied.
type EffectiveClusterConfig struct {
	ClusterSpec       *enterprisev4.PostgresClusterSpec
	ProvisionerConfig *enterprisev4.CNPGConfig
}

// normalizedManagedRole holds only the fields this controller sets on a CNPG RoleConfiguration.
// CNPG's admission webhook populates defaults (ConnectionLimit: -1, Inherit: true) that would
// cause equality.Semantic.DeepEqual to always report a diff — we compare only what we own.
type normalizedManagedRole struct {
	Name           string
	Ensure         cnpgv1.EnsureOption
	Login          bool
	PasswordSecret string
}

// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusterclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters/status,verbs=get
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=poolers/status,verbs=get

// Reconcile drives PostgresCluster toward the desired CNPG resources and status.
func (r *PostgresClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logs.FromContext(ctx)
	logger.Info("Reconciling PostgresCluster", "name", req.Name, "namespace", req.Namespace)

	var cnpgCluster *cnpgv1.Cluster
	var poolerEnabled bool
	var postgresSecretName string
	secret := &corev1.Secret{}

	// Phase: ResourceFetch
	postgresCluster := &enterprisev4.PostgresCluster{}
	if getPGClusterErr := r.Get(ctx, req.NamespacedName, postgresCluster); getPGClusterErr != nil {
		if apierrors.IsNotFound(getPGClusterErr) {
			logger.Info("PostgresCluster deleted, skipping reconciliation")
			return ctrl.Result{}, nil
		}
		logger.Error(getPGClusterErr, "Unable to fetch PostgresCluster")
		return ctrl.Result{}, getPGClusterErr
	}
	persistedStatus := postgresCluster.Status.DeepCopy()

	if postgresCluster.Status.Resources == nil {
		postgresCluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}

	// Keep condition and phase updates consistent across the reconcile flow.
	updateStatus := func(
		conditionType conditionTypes,
		status metav1.ConditionStatus,
		reason conditionReasons,
		message string,
		phase reconcileClusterPhases) {
		r.updateStatus(postgresCluster, conditionType, status, reason, message, phase)
	}

	// Phase: FinalizerHandling
	// Handle deletion before any create or patch path so cleanup wins over reconciliation.
	finalizerErr := r.handleFinalizer(ctx, postgresCluster, secret, cnpgCluster)
	if finalizerErr != nil {
		if apierrors.IsNotFound(finalizerErr) {
			logger.Info("PostgresCluster already deleted, skipping finalizer update")
			return ctrl.Result{}, nil
		}

		logger.Error(finalizerErr, "Failed to handle finalizer")
		updateStatus(
			clusterReady,
			metav1.ConditionFalse,
			reasonClusterDeleteFailed,
			fmt.Sprintf("Failed to delete resources during cleanup: %v", finalizerErr),
			failedClusterPhase)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
		return ctrl.Result{}, finalizerErr
	}

	if postgresCluster.GetDeletionTimestamp() != nil {
		logger.Info("PostgresCluster is being deleted, cleanup complete")
		return ctrl.Result{}, nil
	}

	// Register the finalizer before creating managed resources.
	if !controllerutil.ContainsFinalizer(postgresCluster, postgresClusterFinalizerName) {
		controllerutil.AddFinalizer(postgresCluster, postgresClusterFinalizerName)
		if updateErr := r.Update(ctx, postgresCluster); updateErr != nil {
			if apierrors.IsConflict(updateErr) {
				logger.Info("Conflict while adding finalizer, will retry on next reconcile")
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(updateErr, "Failed to add finalizer to PostgresCluster")
			return ctrl.Result{}, updateErr
		}
		logger.Info("Finalizer added successfully")
		return ctrl.Result{Requeue: true}, nil
	}

	// Phase: ClassResolution
	postgresClusterClass := &enterprisev4.PostgresClusterClass{}
	if getClusterClassErr := r.Get(ctx, client.ObjectKey{Name: postgresCluster.Spec.Class}, postgresClusterClass); getClusterClassErr != nil {
		logger.Error(getClusterClassErr, "Unable to fetch referenced PostgresClusterClass", "className", postgresCluster.Spec.Class)
		updateStatus(
			clusterReady,
			metav1.ConditionFalse,
			reasonClusterClassNotFound,
			fmt.Sprintf("ClusterClass %s not found: %v", postgresCluster.Spec.Class, getClusterClassErr),
			failedClusterPhase)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
		return ctrl.Result{}, getClusterClassErr
	}

	// Phase: ConfigurationMerging
	// Merge PostgresCluster overrides on top of PostgresClusterClass defaults.
	mergedConfig, mergeErr := r.getMergedConfig(postgresClusterClass, postgresCluster)
	if mergeErr != nil {
		logger.Error(mergeErr, "Failed to merge PostgresCluster configuration")
		updateStatus(
			clusterReady,
			metav1.ConditionFalse,
			reasonInvalidConfiguration,
			fmt.Sprintf("Failed to merge configuration: %v", mergeErr),
			failedClusterPhase)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
		return ctrl.Result{}, mergeErr
	}

	// Phase: CredentialProvisioning
	// The superuser secret must exist before the CNPG Cluster can be created or updated.
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
		updateStatus(
			clusterReady,
			metav1.ConditionFalse,
			reasonUserSecretFailed,
			fmt.Sprintf("Failed to check secret existence: %v", secretExistErr),
			failedClusterPhase)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
		return ctrl.Result{}, secretExistErr
	}

	if !postgresClusterSecretExists {
		logger.Info("Creating PostgresCluster secret", "name", postgresSecretName)
		if generateSecretErr := r.generateSecret(ctx, postgresCluster, postgresSecretName); generateSecretErr != nil {
			logger.Error(generateSecretErr, "Failed to ensure PostgresCluster secret", "name", postgresSecretName)
			updateStatus(
				clusterReady,
				metav1.ConditionFalse,
				reasonUserSecretFailed,
				fmt.Sprintf("Failed to generate PostgresCluster secret: %v", generateSecretErr),
				failedClusterPhase)
			_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
			return ctrl.Result{}, generateSecretErr
		}
		logger.Info("PostgresCluster secret created successfully", "name", postgresSecretName)
	}

	// Re-link an existing secret if its owner reference was removed.
	if postgresClusterSecretExists {
		restoredSecretOwnerRef, restoreErr := r.restoreOwnerRef(ctx, postgresCluster, secret, "Secret")
		if restoreErr != nil {
			logger.Error(restoreErr, "Failed to restore owner reference on Secret")
			updateStatus(
				clusterReady,
				metav1.ConditionFalse,
				reasonSuperUserSecretFailed,
				fmt.Sprintf("Failed to link existing secret: %v", restoreErr),
				failedClusterPhase)
			_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
			return ctrl.Result{}, restoreErr
		}
		if restoredSecretOwnerRef {
			logger.Info("Existing secret linked successfully")
		}
	}

	if postgresCluster.Status.Resources == nil {
		postgresCluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}
	if postgresCluster.Status.Resources.SecretRef == nil {
		postgresCluster.Status.Resources.SecretRef = &corev1.LocalObjectReference{Name: postgresSecretName}
		return r.persistStatus(ctx, postgresCluster, persistedStatus)
	}

	// Phase: ClusterSpecConstruction
	desiredSpec := r.buildCNPGClusterSpec(mergedConfig, postgresSecretName)

	// Phase: ClusterReconciliation
	// Create the CNPG Cluster on first reconcile, otherwise compare and patch drift.
	existingCNPG := &cnpgv1.Cluster{}
	getErr := r.Get(ctx, types.NamespacedName{Name: postgresCluster.Name, Namespace: postgresCluster.Namespace}, existingCNPG)

	if apierrors.IsNotFound(getErr) {
		// CNPG Cluster doesn't exist yet. Create it and return so status can be observed on the next pass.
		logger.Info("CNPG Cluster not found, creating", "name", postgresCluster.Name)
		newCluster := r.buildCNPGCluster(postgresCluster, mergedConfig, postgresSecretName)
		if createErr := r.Create(ctx, newCluster); createErr != nil {
			logger.Error(createErr, "Failed to create CNPG Cluster")
			updateStatus(
				clusterReady,
				metav1.ConditionFalse,
				reasonClusterBuildFailed,
				fmt.Sprintf("Failed to create CNPG Cluster: %v", createErr),
				failedClusterPhase)
			_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
			return ctrl.Result{}, createErr
		}

		updateStatus(
			clusterReady,
			metav1.ConditionFalse,
			reasonClusterBuildSucceeded,
			"CNPG Cluster created",
			provisioningClusterPhase)
		if result, persistErr := r.persistStatus(ctx, postgresCluster, persistedStatus); persistErr != nil || result != (ctrl.Result{}) {
			return result, persistErr
		}
		logger.Info("CNPG Cluster created successfully,", "name", postgresCluster.Name)
		return ctrl.Result{}, nil

	}
	if getErr != nil {
		logger.Error(getErr, "Failed to get CNPG Cluster")
		updateStatus(
			clusterReady,
			metav1.ConditionFalse,
			reasonClusterGetFailed,
			fmt.Sprintf("Failed to get CNPG Cluster: %v", getErr),
			failedClusterPhase)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
		return ctrl.Result{}, getErr
	}

	cnpgCluster = existingCNPG
	// Re-link an existing CNPG Cluster if its owner reference was removed.
	if restoredClusterOwnerRef, restoreErr := r.restoreOwnerRef(ctx, postgresCluster, cnpgCluster, "CNPGCluster"); restoreErr != nil {
		logger.Error(restoreErr, "Failed to restore owner reference on CNPG Cluster")
		updateStatus(
			clusterReady,
			metav1.ConditionFalse,
			reasonClusterPatchFailed,
			fmt.Sprintf("Failed to link existing CNPG Cluster: %v", restoreErr),
			failedClusterPhase)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
		return ctrl.Result{}, restoreErr
	} else if restoredClusterOwnerRef {
		logger.Info("Existing CNPG Cluster linked successfully", "cluster", cnpgCluster.Name)
	}

	// Patch the CNPG Cluster when the live spec differs from the desired spec.
	currentNormalizedSpec := normalizeCNPGClusterSpec(cnpgCluster.Spec, mergedConfig.ClusterSpec.PostgreSQLConfig)
	desiredNormalizedSpec := normalizeCNPGClusterSpec(desiredSpec, mergedConfig.ClusterSpec.PostgreSQLConfig)

	if !equality.Semantic.DeepEqual(currentNormalizedSpec, desiredNormalizedSpec) {
		logger.Info("Detected drift in CNPG Cluster spec, patching", "name", cnpgCluster.Name)
		originalCluster := cnpgCluster.DeepCopy()
		cnpgCluster.Spec = desiredSpec
		if patchErr := r.patchObject(ctx, originalCluster, cnpgCluster, "CNPGCluster"); patchErr != nil {
			if apierrors.IsConflict(patchErr) {
				logger.Info("Conflict occurred while updating CNPG Cluster, requeueing", "name", cnpgCluster.Name)
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(patchErr, "Failed to patch CNPG Cluster", "name", cnpgCluster.Name)
			updateStatus(
				clusterReady,
				metav1.ConditionFalse,
				reasonClusterPatchFailed,
				fmt.Sprintf("Failed to patch CNPG Cluster: %v", patchErr),
				failedClusterPhase)
			_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
			return ctrl.Result{}, patchErr
		}
		logger.Info("CNPG Cluster patched successfully", "name", cnpgCluster.Name)
		return ctrl.Result{}, nil
	}

	// Phase: ManagedRoleReconciliation
	if managedRolesErr := r.reconcileManagedRoles(ctx, postgresCluster, cnpgCluster); managedRolesErr != nil {
		logger.Error(managedRolesErr, "Failed to reconcile managed roles")
		updateStatus(
			clusterReady,
			metav1.ConditionFalse,
			reasonManagedRolesFailed,
			fmt.Sprintf("Failed to reconcile managed roles: %v", managedRolesErr),
			failedClusterPhase)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
		return ctrl.Result{}, managedRolesErr
	}

	// Phase: ClusterStatusProjection
	// Project CNPG status before later phase-specific early returns so cluster status stays current.
	clusterConditionStatus, clusterReason, clusterMessage, clusterPhase := r.syncStatus(postgresCluster, cnpgCluster)

	logger.Info(
		"Mapped CNPG status to PostgresCluster", "cnpgPhase",
		cnpgCluster.Status.Phase, "postgresClusterPhase",
		clusterPhase, "conditionStatus",
		clusterConditionStatus, "reason",
		clusterReason, "message",
		clusterMessage)

	updateStatus(
		clusterReady,
		clusterConditionStatus,
		clusterReason,
		clusterMessage,
		clusterPhase,
	)

	// Phase: PoolerReconciliation
	poolerEnabled = mergedConfig.ClusterSpec.ConnectionPoolerEnabled != nil && *mergedConfig.ClusterSpec.ConnectionPoolerEnabled
	if poolerEnabled {
		if mergedConfig.ProvisionerConfig.ConnectionPooler == nil {
			logger.Info("Connection pooler enabled but no config found in class or cluster spec",
				"class", postgresCluster.Spec.Class,
				"cluster", postgresCluster.Name,
			)
			updateStatus(
				poolerReady,
				metav1.ConditionFalse,
				reasonPoolerConfigMissing,
				fmt.Sprintf("Connection pooler is enabled but no config found in class %q or cluster %q",
					postgresCluster.Spec.Class, postgresCluster.Name),
				failedClusterPhase,
			)
			return r.persistStatus(ctx, postgresCluster, persistedStatus)
		}
		if createPoolerErr := r.createConnectionPoolers(ctx, postgresCluster, mergedConfig, cnpgCluster); createPoolerErr != nil {
			logger.Error(createPoolerErr, "Failed to create connection poolers")
			updateStatus(
				poolerReady,
				metav1.ConditionFalse,
				reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to create connection poolers: %v", createPoolerErr),
				failedClusterPhase,
			)
			_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
			return ctrl.Result{}, createPoolerErr
		}

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
		if rwErr != nil || roErr != nil || !r.arePoolersReady(rwPooler, roPooler) {
			logger.Info("Connection poolers are not ready yet, requeueing")
			updateStatus(
				poolerReady,
				metav1.ConditionFalse,
				reasonPoolerCreating,
				"Connection poolers are being provisioned",
				provisioningClusterPhase,
			)
			if result, persistErr := r.persistStatus(ctx, postgresCluster, persistedStatus); persistErr != nil || result != (ctrl.Result{}) {
				return result, persistErr
			}
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}

		message, err := r.syncPoolerStatus(ctx, postgresCluster)
		if err != nil {
			updateStatus(
				poolerReady,
				metav1.ConditionFalse,
				reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to sync pooler status: %v", err),
				failedClusterPhase,
			)
			_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
			return ctrl.Result{}, err
		}

		updateStatus(
			poolerReady,
			metav1.ConditionTrue,
			reasonAllInstancesReady,
			fmt.Sprintf("All connection poolers are ready: %s", message),
			clusterPhase,
		)
	} else {
		if err := r.deleteConnectionPoolers(ctx, postgresCluster); err != nil {
			logger.Error(err, "Failed to delete connection poolers")
			updateStatus(
				poolerReady,
				metav1.ConditionFalse,
				reasonPoolerReconciliationFailed,
				"Failed to delete connection poolers",
				failedClusterPhase,
			)
			_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
			return ctrl.Result{}, err
		}
		if r.poolerExists(ctx, postgresCluster, readWriteEndpoint) || r.poolerExists(ctx, postgresCluster, readOnlyEndpoint) {
			updateStatus(
				poolerReady,
				metav1.ConditionFalse,
				reasonPoolerCreating,
				"Connection poolers are being deleted",
				provisioningClusterPhase,
			)
			if result, persistErr := r.persistStatus(ctx, postgresCluster, persistedStatus); persistErr != nil || result != (ctrl.Result{}) {
				return result, persistErr
			}
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
		postgresCluster.Status.ConnectionPoolerStatus = nil
		meta.RemoveStatusCondition(&postgresCluster.Status.Conditions, string(poolerReady))
	}

	// Phase: ConnectionMetadata
	// Publish connection details after the cluster and optional poolers reach the desired state.
	desiredConfigMap, err := r.generateConfigMap(postgresCluster, postgresSecretName, poolerEnabled)
	if err != nil {
		logger.Error(err, "Failed to generate ConfigMap")
		updateStatus(
			configMapReady,
			metav1.ConditionFalse,
			reasonConfigMapFailed,
			fmt.Sprintf("Failed to generate ConfigMap: %v", err),
			failedClusterPhase,
		)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
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
		configMap.Labels = desiredConfigMap.Labels
		configMap.Annotations = desiredConfigMap.Annotations

		if !metav1.IsControlledBy(configMap, postgresCluster) {
			if err := ctrl.SetControllerReference(postgresCluster, configMap, r.Scheme); err != nil {
				return fmt.Errorf("set controller reference failed: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		logger.Error(err, "Failed to reconcile ConfigMap", "name", desiredConfigMap.Name)
		updateStatus(
			configMapReady,
			metav1.ConditionFalse,
			reasonConfigMapFailed,
			fmt.Sprintf("Failed to reconcile ConfigMap: %v", err),
			failedClusterPhase,
		)
		_ = r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus)
		return ctrl.Result{}, err
	}

	switch createOrUpdateResult {
	case controllerutil.OperationResultCreated:
		logger.Info("ConfigMap created", "name", desiredConfigMap.Name)
	case controllerutil.OperationResultUpdated:
		logger.Info("ConfigMap updated", "name", desiredConfigMap.Name)
	case controllerutil.OperationResultNone:
		logger.Info("ConfigMap unchanged", "name", desiredConfigMap.Name)
	}

	if postgresCluster.Status.Resources.ConfigMapRef == nil ||
		postgresCluster.Status.Resources.ConfigMapRef.Name != desiredConfigMap.Name {
		postgresCluster.Status.Resources.ConfigMapRef = &corev1.LocalObjectReference{Name: desiredConfigMap.Name}
		logger.Info("ConfigMap reference updated in status", "configMap", desiredConfigMap.Name)
	}
	// Phase: ReadyStatus
	// Persist the final ConfigMap status update and finish the reconcile pass.
	updateStatus(
		configMapReady,
		metav1.ConditionTrue,
		reasonConfigMapsCreated,
		fmt.Sprintf("ConfigMap is ready: %s", desiredConfigMap.Name),
		clusterPhase,
	)
	if result, persistErr := r.persistStatus(ctx, postgresCluster, persistedStatus); persistErr != nil || result != (ctrl.Result{}) {
		return result, persistErr
	}
	logger.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

// getMergedConfig applies PostgresClusterClass defaults and validates the required resulting fields.
func (r *PostgresClusterReconciler) getMergedConfig(clusterClass *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) (*EffectiveClusterConfig, error) {
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

	return &EffectiveClusterConfig{
		ClusterSpec:       resultConfig,
		ProvisionerConfig: clusterClass.Spec.CNPG,
	}, nil
}

// buildCNPGClusterSpec builds the desired CNPG ClusterSpec from the merged configuration.
// IMPORTANT: any field added here must also be added to normalizedCNPGClusterSpec and normalizeCNPGClusterSpec,
// otherwise it will not be included in drift detection and changes will be silently ignored.
func (r *PostgresClusterReconciler) buildCNPGClusterSpec(mergedConfig *EffectiveClusterConfig, secretName string) cnpgv1.ClusterSpec {

	// 3. Build the Spec
	spec := cnpgv1.ClusterSpec{
		ImageName: fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *mergedConfig.ClusterSpec.PostgresVersion),
		Instances: int(*mergedConfig.ClusterSpec.Instances),
		PostgresConfiguration: cnpgv1.PostgresConfiguration{
			Parameters: mergedConfig.ClusterSpec.PostgreSQLConfig,
			PgHBA:      mergedConfig.ClusterSpec.PgHBA,
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
			Size: mergedConfig.ClusterSpec.Storage.String(),
		},
		Resources: *mergedConfig.ClusterSpec.Resources,
	}

	return spec
}

// buildCNPGCluster builds the CNPG Cluster object for the merged PostgresCluster configuration.
func (r *PostgresClusterReconciler) buildCNPGCluster(
	postgresCluster *enterprisev4.PostgresCluster,
	mergedConfig *EffectiveClusterConfig,
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

// createConnectionPoolers ensures both RW and RO CNPG Pooler resources exist by creating missing poolers.
func (r *PostgresClusterReconciler) createConnectionPoolers(
	ctx context.Context,
	postgresCluster *enterprisev4.PostgresCluster,
	mergedConfig *EffectiveClusterConfig,
	cnpgCluster *cnpgv1.Cluster,
) error {
	// Ensure the RW pooler exists.
	if err := r.createConnectionPooler(ctx, postgresCluster, mergedConfig, cnpgCluster, readWriteEndpoint); err != nil {
		return fmt.Errorf("failed to reconcile RW pooler: %w", err)
	}

	// Ensure the RO pooler exists.
	if err := r.createConnectionPooler(ctx, postgresCluster, mergedConfig, cnpgCluster, readOnlyEndpoint); err != nil {
		return fmt.Errorf("failed to reconcile RO pooler: %w", err)
	}

	return nil
}

// poolerExists reports whether the named pooler resource exists.
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

// createConnectionPooler creates a CNPG Pooler resource when it is missing.
// Existing poolers are left unchanged by design.
func (r *PostgresClusterReconciler) createConnectionPooler(
	ctx context.Context,
	postgresCluster *enterprisev4.PostgresCluster,
	mergedConfig *EffectiveClusterConfig,
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
		pooler := r.buildCNPGPooler(postgresCluster, mergedConfig, cnpgCluster, poolerType)
		return r.Create(ctx, pooler)
	}

	return err
}

// buildCNPGPooler builds the desired CNPG Pooler object for the given pooler type.
func (r *PostgresClusterReconciler) buildCNPGPooler(
	postgresCluster *enterprisev4.PostgresCluster,
	mergedConfig *EffectiveClusterConfig,
	cnpgCluster *cnpgv1.Cluster,
	poolerType string,
) *cnpgv1.Pooler {
	cfg := mergedConfig.ProvisionerConfig.ConnectionPooler
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

// syncStatus maps CNPG Cluster state onto PostgresCluster status and refreshes ProvisionerRef.
func (r *PostgresClusterReconciler) syncStatus(
	postgresCluster *enterprisev4.PostgresCluster,
	cnpgCluster *cnpgv1.Cluster,
) (metav1.ConditionStatus, conditionReasons, string, reconcileClusterPhases) {
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
	return conditionStatus, reason, message, clusterPhase

}

// updateStatus is a convenience wrapper that updates a condition and the phase together.
// For cases where you need to update multiple conditions before persisting, use updateCondition instead.
func (r *PostgresClusterReconciler) updateStatus(
	postgresCluster *enterprisev4.PostgresCluster,
	conditionType conditionTypes,
	status metav1.ConditionStatus,
	reason conditionReasons,
	message string,
	phase reconcileClusterPhases,
) {
	r.updateCondition(postgresCluster, conditionType, status, reason, message)
	postgresCluster.Status.Phase = string(phase)
}

// updateCondition updates a single status condition in memory without persisting.
// Call persistStatus after updating all desired conditions.
func (r *PostgresClusterReconciler) updateCondition(
	postgresCluster *enterprisev4.PostgresCluster,
	conditionType conditionTypes,
	status metav1.ConditionStatus,
	reason conditionReasons,
	message string,
) {
	meta.SetStatusCondition(&postgresCluster.Status.Conditions, metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: postgresCluster.Generation,
	})
}

// persistStatus persists status changes and converts update conflicts into reconcile retries.
func (r *PostgresClusterReconciler) persistStatus(
	ctx context.Context,
	postgresCluster *enterprisev4.PostgresCluster,
	persistedStatus *enterprisev4.PostgresClusterStatus,
) (ctrl.Result, error) {
	if persistErr := r.persistStatusIfChanged(ctx, postgresCluster, persistedStatus); persistErr != nil {
		if apierrors.IsConflict(persistErr) {
			logs.FromContext(ctx).Info("Conflict while updating status, will retry on next reconcile")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, persistErr
	}
	return ctrl.Result{}, nil
}

// persistStatusIfChanged updates status only when it differs from the last persisted snapshot.
func (r *PostgresClusterReconciler) persistStatusIfChanged(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, lastPostgresClusterStatus *enterprisev4.PostgresClusterStatus) error {
	if !equality.Semantic.DeepEqual(postgresCluster.Status, *lastPostgresClusterStatus) {
		if err := r.Status().Update(ctx, postgresCluster); err != nil {
			return err
		}
		*lastPostgresClusterStatus = *postgresCluster.Status.DeepCopy()
	}
	return nil
}

// syncPoolerStatus populates ConnectionPoolerStatus and returns a summary message.
// Callers are responsible for updating PoolerReady after this succeeds.
func (r *PostgresClusterReconciler) syncPoolerStatus(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster) (string, error) {
	rwPooler := &cnpgv1.Pooler{}
	if rwErr := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, readWriteEndpoint),
		Namespace: postgresCluster.Namespace,
	}, rwPooler); rwErr != nil {
		return "", rwErr
	}

	roPooler := &cnpgv1.Pooler{}
	if roErr := r.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(postgresCluster.Name, readOnlyEndpoint),
		Namespace: postgresCluster.Namespace,
	}, roPooler); roErr != nil {
		return "", roErr
	}

	postgresCluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{
		Enabled: true,
	}

	rwDesired, rwScheduled := r.getPoolerInstanceCount(rwPooler)
	roDesired, roScheduled := r.getPoolerInstanceCount(roPooler)

	return fmt.Sprintf("%s: %d/%d, %s: %d/%d",
		readWriteEndpoint, rwScheduled, rwDesired,
		readOnlyEndpoint, roScheduled, roDesired,
	), nil
}

// isPoolerReady checks if a pooler has all instances scheduled.
// Note: CNPG PoolerStatus only tracks scheduled instances, not ready pods.
func (r *PostgresClusterReconciler) isPoolerReady(pooler *cnpgv1.Pooler) bool {
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
func (r *PostgresClusterReconciler) arePoolersReady(rwPooler, roPooler *cnpgv1.Pooler) bool {
	return r.isPoolerReady(rwPooler) && r.isPoolerReady(roPooler)
}

// normalizeManagedRole projects a CNPG RoleConfiguration down to only the fields this controller controls.
// CNPG's admission webhook populates defaults on the live object (ConnectionLimit: -1, Inherit: true)
// that are absent from our desired slice — normalizing both sides before comparison prevents a
// permanent diff that would re-patch on every reconcile.
func normalizeManagedRole(r cnpgv1.RoleConfiguration) normalizedManagedRole {
	secret := ""
	if r.PasswordSecret != nil {
		secret = r.PasswordSecret.Name
	}
	return normalizedManagedRole{
		Name:           r.Name,
		Ensure:         r.Ensure,
		Login:          r.Login,
		PasswordSecret: secret,
	}
}

// normalizeManagedRoles applies normalizeManagedRole to each RoleConfiguration in the slice.
func normalizeManagedRoles(roles []cnpgv1.RoleConfiguration) []normalizedManagedRole {
	result := make([]normalizedManagedRole, 0, len(roles))
	for _, r := range roles {
		result = append(result, normalizeManagedRole(r))
	}
	return result
}

// buildCNPGRole converts a single PostgresCluster ManagedRole to its CNPG RoleConfiguration equivalent.
// Absent roles are marked for removal; Login is only meaningful for present roles.
func buildCNPGRole(role enterprisev4.ManagedRole) cnpgv1.RoleConfiguration {
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
		cnpgRole.PasswordSecret = &cnpgv1.LocalObjectReference{Name: role.PasswordSecretRef.Name}
	}
	return cnpgRole
}

// reconcileManagedRoles synchronizes ManagedRoles from PostgresCluster spec to CNPG Cluster managed.roles.
func (r *PostgresClusterReconciler) reconcileManagedRoles(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	logger := logs.FromContext(ctx)

	desired := make([]cnpgv1.RoleConfiguration, 0, len(postgresCluster.Spec.ManagedRoles))
	for _, role := range postgresCluster.Spec.ManagedRoles {
		desired = append(desired, buildCNPGRole(role))
	}

	var current []cnpgv1.RoleConfiguration
	if cnpgCluster.Spec.Managed != nil {
		current = cnpgCluster.Spec.Managed.Roles
	}

	if equality.Semantic.DeepEqual(normalizeManagedRoles(current), normalizeManagedRoles(desired)) {
		logger.Info("CNPG Cluster roles already match desired state, no update needed")
		return nil
	}

	logger.Info("Detected drift in managed roles, patching", "count", len(desired))
	originalCluster := cnpgCluster.DeepCopy()
	if cnpgCluster.Spec.Managed == nil {
		cnpgCluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	cnpgCluster.Spec.Managed.Roles = desired

	if err := r.patchObject(ctx, originalCluster, cnpgCluster, "CNPGCluster"); err != nil {
		return fmt.Errorf("patching managed roles: %w", err)
	}

	logger.Info("Successfully updated managed roles", "count", len(desired))
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

// generateConfigMap builds the desired ConfigMap with connection details for the PostgresCluster.
func (r *PostgresClusterReconciler) generateConfigMap(
	postgresCluster *enterprisev4.PostgresCluster,
	secretName string,
	poolerEnabled bool,
) (*corev1.ConfigMap, error) {
	configMapName := fmt.Sprintf("%s%s", postgresCluster.Name, defaultConfigMapSuffix)
	if postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.ConfigMapRef != nil {
		configMapName = postgresCluster.Status.Resources.ConfigMapRef.Name
	}

	data := map[string]string{
		"CLUSTER_RW_ENDPOINT":   fmt.Sprintf("%s-rw.%s", postgresCluster.Name, postgresCluster.Namespace),
		"CLUSTER_RO_ENDPOINT":   fmt.Sprintf("%s-ro.%s", postgresCluster.Name, postgresCluster.Namespace),
		"CLUSTER_R_ENDPOINT":    fmt.Sprintf("%s-r.%s", postgresCluster.Name, postgresCluster.Namespace),
		"DEFAULT_CLUSTER_PORT":  defaultPort,
		"SUPER_USER_NAME":       superUsername,
		"SUPER_USER_SECRET_REF": secretName,
	}

	if poolerEnabled {
		data["CLUSTER_POOLER_RW_ENDPOINT"] = fmt.Sprintf("%s.%s", poolerResourceName(postgresCluster.Name, readWriteEndpoint), postgresCluster.Namespace)
		data["CLUSTER_POOLER_RO_ENDPOINT"] = fmt.Sprintf("%s.%s", poolerResourceName(postgresCluster.Name, readOnlyEndpoint), postgresCluster.Namespace)
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

// generateSecret creates the superuser Secret when it is missing.
func (r *PostgresClusterReconciler) generateSecret(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, secretName string) error {
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: postgresCluster.Namespace}, existing)

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
		// Set owner reference
		if err := ctrl.SetControllerReference(postgresCluster, secret, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, secret); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

// deleteCNPGCluster deletes the CNPG Cluster resource if it exists.
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

// handleFinalizer performs deletion-time cleanup and removes the finalizer when cleanup succeeds.
func (r *PostgresClusterReconciler) handleFinalizer(ctx context.Context, postgresCluster *enterprisev4.PostgresCluster, secret *corev1.Secret, cnpgCluster *cnpgv1.Cluster) error {
	logger := logs.FromContext(ctx)
	if postgresCluster.GetDeletionTimestamp() == nil {
		logger.Info("PostgresCluster not marked for deletion, skipping finalizer logic")
		return nil
	}
	if !controllerutil.ContainsFinalizer(postgresCluster, postgresClusterFinalizerName) {
		logger.Info("Finalizer not present on PostgresCluster, skipping finalizer logic")
		return nil
	}
	if cnpgCluster == nil {
		cnpgCluster = &cnpgv1.Cluster{}
	}

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
		logger.Info("CNPG Cluster not found")
	case clusterDeletionPolicyRetain:
		logger.Info("ClusterDeletionPolicy is 'Retain', proceeding to remove owner references and retain CNPG Cluster")
		// Remove owner reference from CNPG Cluster to prevent its deletion.
		if cnpgCluster != nil {
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
		}
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
				refRemoved, err := r.removeOwnerRef(postgresCluster, secret, "Secret")
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

// clusterSecretExists returns whether the secret is present and propagates lookup errors.
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

// removeOwnerRef removes the owner's reference from the object and reports whether it changed the object.
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

// restoreOwnerRef adds the PostgresCluster owner reference back to an existing object when it is missing.
func (r *PostgresClusterReconciler) restoreOwnerRef(ctx context.Context, owner client.Object, obj client.Object, objKind objectKind) (bool, error) {
	hasOwnerRef, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), owner, r.Scheme)
	if err != nil {
		return false, fmt.Errorf("failed to check owner reference on %s: %w", objKind, err)
	}
	if hasOwnerRef {
		return false, nil
	}

	logger := logs.FromContext(ctx)
	logger.Info("Connecting existing object to PostgresCluster by adding owner reference", "kind", objKind, "name", obj.GetName())

	originalObj, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return false, fmt.Errorf("failed to deep copy %s object", objKind)
	}

	if err := ctrl.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return false, fmt.Errorf("failed to set controller reference on existing %s: %w", objKind, err)
	}

	if err := r.patchObject(ctx, originalObj, obj, objKind); err != nil {
		return false, err
	}

	return true, nil
}

// patchObject applies a merge patch and treats NotFound as already converged.
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

// SetupWithManager registers the controller for PostgresCluster resources and owned CNPG Clusters.
func (r *PostgresClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresCluster{}, builder.WithPredicates(postgresClusterPredicator())).
		Owns(&cnpgv1.Cluster{}, builder.WithPredicates(cnpgClusterPredicator())).
		Owns(&cnpgv1.Pooler{}, builder.WithPredicates(cnpgPoolerPredicator())).
		Owns(&corev1.Secret{}, builder.WithPredicates(secretPredicator())).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(configMapPredicator())).
		Named("postgresCluster").
		Complete(r)
}

func deletionTimestampChanged(oldObj, newObj metav1.Object) bool {
	return !equality.Semantic.DeepEqual(oldObj.GetDeletionTimestamp(), newObj.GetDeletionTimestamp())
}

func ownerReferencesChanged(oldObj, newObj metav1.Object) bool {
	return !equality.Semantic.DeepEqual(oldObj.GetOwnerReferences(), newObj.GetOwnerReferences())
}

// cnpgClusterPredicator filters CNPG Cluster events to only trigger reconciles on creation, deletion, or phase changes.
func cnpgClusterPredicator() predicate.Predicate {

	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldTypeOK := e.ObjectOld.(*cnpgv1.Cluster)
			newObj, newTypeOK := e.ObjectNew.(*cnpgv1.Cluster)
			if !oldTypeOK || !newTypeOK {
				return true
			}
			return oldObj.Status.Phase != newObj.Status.Phase ||
				ownerReferencesChanged(oldObj, newObj)
		},
		GenericFunc: func(event.GenericEvent) bool {
			return false
		},
	}
}

// postgresClusterPredicator filters PostgresCluster events to trigger reconciles on creation, deletion, generation changes, deletion timestamp changes, or finalizer changes.
func postgresClusterPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldTypeOK := e.ObjectOld.(*enterprisev4.PostgresCluster)
			newObj, newTypeOK := e.ObjectNew.(*enterprisev4.PostgresCluster)
			if !oldTypeOK || !newTypeOK {
				return true
			}
			if oldObj.Generation != newObj.Generation {
				return true
			}
			if deletionTimestampChanged(oldObj, newObj) {
				return true
			}
			if postgresClusterFinalizerName != "" && (controllerutil.ContainsFinalizer(oldObj, postgresClusterFinalizerName) != controllerutil.ContainsFinalizer(newObj, postgresClusterFinalizerName)) {
				return true
			}
			return false
		},
		GenericFunc: func(event.GenericEvent) bool {
			return false
		},
	}
}

// cnpgPoolerPredicator filters CNPG Pooler events to trigger reconciles on creation, deletion, or instance count changes.
func cnpgPoolerPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldTypeOK := e.ObjectOld.(*cnpgv1.Pooler)
			newObj, newTypeOK := e.ObjectNew.(*cnpgv1.Pooler)
			if !oldTypeOK || !newTypeOK {
				return true
			}
			return oldObj.Status.Instances != newObj.Status.Instances
		},
		GenericFunc: func(event.GenericEvent) bool {
			return false
		},
	}
}

// secretPredicator filters Secret events to trigger reconciles on creation, deletion, or owner reference changes.
func secretPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldTypeOK := e.ObjectOld.(*corev1.Secret)
			newObj, newTypeOK := e.ObjectNew.(*corev1.Secret)
			if !oldTypeOK || !newTypeOK {
				return true
			}
			return ownerReferencesChanged(oldObj, newObj)
		},
		GenericFunc: func(event.GenericEvent) bool {
			return false
		},
	}
}

// configMapPredicator filters ConfigMap events to trigger reconciles on creation, deletion, data/label/annotation changes, or owner reference changes.
func configMapPredicator() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldObj, oldTypeOK := e.ObjectOld.(*corev1.ConfigMap)
			newObj, newTypeOK := e.ObjectNew.(*corev1.ConfigMap)
			if !oldTypeOK || !newTypeOK {
				return true
			}
			return !equality.Semantic.DeepEqual(oldObj.Data, newObj.Data) ||
				!equality.Semantic.DeepEqual(oldObj.Labels, newObj.Labels) ||
				!equality.Semantic.DeepEqual(oldObj.Annotations, newObj.Annotations) ||
				ownerReferencesChanged(oldObj, newObj)
		},
		GenericFunc: func(event.GenericEvent) bool {
			return false
		},
	}
}
