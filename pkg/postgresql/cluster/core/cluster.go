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

package core

import (
	"context"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	password "github.com/sethvargo/go-password/password"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	log "sigs.k8s.io/controller-runtime/pkg/log"
)

// PostgresClusterService is the application service entry point called by the primary adapter (reconciler).
func PostgresClusterService(ctx context.Context, rc *ReconcileContext, req ctrl.Request) (ctrl.Result, error) {
	c := rc.Client
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PostgresCluster", "name", req.Name, "namespace", req.Namespace)

	var cnpgCluster *cnpgv1.Cluster
	var poolerEnabled bool
	var postgresSecretName string
	secret := &corev1.Secret{}

	// 1. Fetch the PostgresCluster instance, stop if not found.
	postgresCluster := &enterprisev4.PostgresCluster{}
	if err := c.Get(ctx, req.NamespacedName, postgresCluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PostgresCluster deleted, skipping reconciliation")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Unable to fetch PostgresCluster")
		return ctrl.Result{}, err
	}
	if postgresCluster.Status.Resources == nil {
		postgresCluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}

	updateStatus := func(conditionType conditionTypes, status metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileClusterPhases) error {
		return setStatus(ctx, c, postgresCluster, conditionType, status, reason, message, phase)
	}

	// Finalizer handling must come before any other processing.
	if err := handleFinalizer(ctx, rc, postgresCluster, secret); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PostgresCluster already deleted, skipping finalizer update")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to handle finalizer")
		rc.emitWarning(postgresCluster, EventCleanupFailed, fmt.Sprintf("Cleanup failed: %v", err))
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterDeleteFailed,
			fmt.Sprintf("Failed to delete resources during cleanup: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	if postgresCluster.GetDeletionTimestamp() != nil {
		logger.Info("PostgresCluster is being deleted, cleanup complete")
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(postgresCluster, PostgresClusterFinalizerName) {
		controllerutil.AddFinalizer(postgresCluster, PostgresClusterFinalizerName)
		if err := c.Update(ctx, postgresCluster); err != nil {
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
	clusterClass := &enterprisev4.PostgresClusterClass{}
	if err := c.Get(ctx, client.ObjectKey{Name: postgresCluster.Spec.Class}, clusterClass); err != nil {
		logger.Error(err, "Unable to fetch referenced PostgresClusterClass", "className", postgresCluster.Spec.Class)
		rc.emitWarning(postgresCluster, EventClusterClassNotFound, fmt.Sprintf("ClusterClass %s not found", postgresCluster.Spec.Class))
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterClassNotFound,
			fmt.Sprintf("ClusterClass %s not found: %v", postgresCluster.Spec.Class, err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// 3. Merge PostgresClusterSpec on top of PostgresClusterClass defaults.
	mergedConfig, err := getMergedConfig(clusterClass, postgresCluster)
	if err != nil {
		logger.Error(err, "Failed to merge PostgresCluster configuration")
		rc.emitWarning(postgresCluster, EventConfigMergeFailed, fmt.Sprintf("Failed to merge configuration: %v", err))
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonInvalidConfiguration,
			fmt.Sprintf("Failed to merge configuration: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// 4. Resolve or derive the superuser secret name.
	if postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.SuperUserSecretRef != nil {
		postgresSecretName = postgresCluster.Status.Resources.SuperUserSecretRef.Name
		logger.Info("Using existing secret from status", "name", postgresSecretName)
	} else {
		postgresSecretName = fmt.Sprintf("%s%s", postgresCluster.Name, defaultSecretSuffix)
		logger.Info("Generating new secret name", "name", postgresSecretName)
	}

	secretExists, secretErr := clusterSecretExists(ctx, c, postgresCluster.Namespace, postgresSecretName, secret)
	if secretErr != nil {
		logger.Error(secretErr, "Failed to check if PostgresCluster secret exists", "name", postgresSecretName)
		rc.emitWarning(postgresCluster, EventSecretReconcileFailed, fmt.Sprintf("Failed to check secret existence: %v", secretErr))
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonUserSecretFailed,
			fmt.Sprintf("Failed to check secret existence: %v", secretErr), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, secretErr
	}
	if !secretExists {
		logger.Info("Creating PostgresCluster secret", "name", postgresSecretName)
		if err := ensureClusterSecret(ctx, c, rc.Scheme, postgresCluster, postgresSecretName, secret); err != nil {
			logger.Error(err, "Failed to ensure PostgresCluster secret", "name", postgresSecretName)
			rc.emitWarning(postgresCluster, EventSecretReconcileFailed, fmt.Sprintf("Failed to generate cluster secret: %v", err))
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonUserSecretFailed,
				fmt.Sprintf("Failed to generate PostgresCluster secret: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		if err := c.Status().Update(ctx, postgresCluster); err != nil {
			if apierrors.IsConflict(err) {
				logger.Info("Conflict after secret creation, will requeue")
				return ctrl.Result{Requeue: true}, nil
			}
			logger.Error(err, "Failed to update status after secret creation")
			return ctrl.Result{}, err
		}
		rc.emitNormal(postgresCluster, EventSecretReady, fmt.Sprintf("Superuser secret %s created", postgresSecretName))
		logger.Info("SuperUserSecretRef persisted to status")
	}

	// Re-attach ownerRef if it was stripped (e.g. by a Retain-policy deletion of a previous cluster).
	hasOwnerRef, ownerRefErr := controllerutil.HasOwnerReference(secret.GetOwnerReferences(), postgresCluster, rc.Scheme)
	if ownerRefErr != nil {
		logger.Error(ownerRefErr, "Failed to check owner reference on Secret")
		return ctrl.Result{}, fmt.Errorf("failed to check owner reference on secret: %w", ownerRefErr)
	}
	if secretExists && !hasOwnerRef {
		logger.Info("Connecting existing secret to PostgresCluster by adding owner reference", "name", postgresSecretName)
		rc.emitNormal(postgresCluster, EventClusterAdopted, fmt.Sprintf("Adopted existing CNPG cluster and secret %s", postgresSecretName))
		originalSecret := secret.DeepCopy()
		if err := ctrl.SetControllerReference(postgresCluster, secret, rc.Scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set controller reference on existing secret: %w", err)
		}
		if err := patchObject(ctx, c, originalSecret, secret, "Secret"); err != nil {
			logger.Error(err, "Failed to patch existing secret with controller reference")
			rc.emitWarning(postgresCluster, EventSecretReconcileFailed, fmt.Sprintf("Failed to patch existing secret: %v", err))
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonSuperUserSecretFailed,
				fmt.Sprintf("Failed to patch existing secret: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		logger.Info("Existing secret linked successfully")
	}

	if postgresCluster.Status.Resources.SuperUserSecretRef == nil {
		postgresCluster.Status.Resources.SuperUserSecretRef = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
			Key:                  secretKeyPassword,
		}
	}

	// 5. Build desired CNPG Cluster spec.
	desiredSpec := buildCNPGClusterSpec(mergedConfig, postgresSecretName)

	// 6. Fetch existing CNPG Cluster or create it.
	existingCNPG := &cnpgv1.Cluster{}
	err = c.Get(ctx, types.NamespacedName{Name: postgresCluster.Name, Namespace: postgresCluster.Namespace}, existingCNPG)
	switch {
	case apierrors.IsNotFound(err):
		logger.Info("CNPG Cluster not found, creating", "name", postgresCluster.Name)
		newCluster := buildCNPGCluster(rc.Scheme, postgresCluster, mergedConfig, postgresSecretName)
		if err := c.Create(ctx, newCluster); err != nil {
			logger.Error(err, "Failed to create CNPG Cluster")
			rc.emitWarning(postgresCluster, EventClusterCreateFailed, fmt.Sprintf("Failed to create CNPG cluster: %v", err))
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterBuildFailed,
				fmt.Sprintf("Failed to create CNPG Cluster: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		rc.emitNormal(postgresCluster, EventClusterCreating, "CNPG cluster created, waiting for healthy state")
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterBuildSucceeded,
			"CNPG Cluster created", pendingClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		logger.Info("CNPG Cluster created successfully, requeueing for status update", "name", postgresCluster.Name)
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	case err != nil:
		logger.Error(err, "Failed to get CNPG Cluster")
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterGetFailed,
			fmt.Sprintf("Failed to get CNPG Cluster: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// 7. Patch CNPG Cluster spec if drift detected.
	cnpgCluster = existingCNPG
	currentNormalized := normalizeCNPGClusterSpec(cnpgCluster.Spec, mergedConfig.Spec.PostgreSQLConfig)
	desiredNormalized := normalizeCNPGClusterSpec(desiredSpec, mergedConfig.Spec.PostgreSQLConfig)

	if !equality.Semantic.DeepEqual(currentNormalized, desiredNormalized) {
		logger.Info("Detected drift in CNPG Cluster spec, patching", "name", cnpgCluster.Name)
		originalCluster := cnpgCluster.DeepCopy()
		cnpgCluster.Spec = desiredSpec

		switch patchErr := patchObject(ctx, c, originalCluster, cnpgCluster, "CNPGCluster"); {
		case apierrors.IsConflict(patchErr):
			logger.Info("Conflict occurred while updating CNPG Cluster, requeueing", "name", cnpgCluster.Name)
			return ctrl.Result{Requeue: true}, nil
		case patchErr != nil:
			logger.Error(patchErr, "Failed to patch CNPG Cluster", "name", cnpgCluster.Name)
			rc.emitWarning(postgresCluster, EventClusterUpdateFailed, fmt.Sprintf("Failed to patch CNPG cluster: %v", patchErr))
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterPatchFailed,
				fmt.Sprintf("Failed to patch CNPG Cluster: %v", patchErr), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, patchErr
		default:
			rc.emitNormal(postgresCluster, EventClusterUpdated, "CNPG cluster spec updated")
			logger.Info("CNPG Cluster patched successfully, requeueing for status update", "name", cnpgCluster.Name)
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
	}

	// 7a. Reconcile ManagedRoles.
	if err := reconcileManagedRoles(ctx, c, postgresCluster, cnpgCluster); err != nil {
		logger.Error(err, "Failed to reconcile managed roles")
		rc.emitWarning(postgresCluster, EventManagedRolesFailed, fmt.Sprintf("Failed to reconcile managed roles: %v", err))
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonManagedRolesFailed,
			fmt.Sprintf("Failed to reconcile managed roles: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// 7b. Reconcile Connection Pooler.
	poolerEnabled = mergedConfig.Spec.ConnectionPoolerEnabled != nil && *mergedConfig.Spec.ConnectionPoolerEnabled
	switch {
	case !poolerEnabled:
		if err := deleteConnectionPoolers(ctx, c, postgresCluster); err != nil {
			logger.Error(err, "Failed to delete connection poolers")
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to delete connection poolers: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		postgresCluster.Status.ConnectionPoolerStatus = nil
		meta.RemoveStatusCondition(&postgresCluster.Status.Conditions, string(poolerReady))

	case !poolerExists(ctx, c, postgresCluster, readWriteEndpoint) || !poolerExists(ctx, c, postgresCluster, readOnlyEndpoint):
		if mergedConfig.CNPG == nil || mergedConfig.CNPG.ConnectionPooler == nil {
			logger.Info("Connection pooler enabled but no config found in class or cluster spec, skipping",
				"class", postgresCluster.Spec.Class, "cluster", postgresCluster.Name)
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerConfigMissing,
				fmt.Sprintf("Connection pooler is enabled but no config found in class %q or cluster %q",
					postgresCluster.Spec.Class, postgresCluster.Name), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, nil
		}
		if cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy {
			logger.Info("CNPG Cluster not healthy yet, pending pooler creation", "clusterPhase", cnpgCluster.Status.Phase)
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonCNPGClusterNotHealthy,
				"Waiting for CNPG cluster to become healthy before creating poolers", pendingClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
		if err := createOrUpdateConnectionPoolers(ctx, c, rc.Scheme, postgresCluster, mergedConfig, cnpgCluster); err != nil {
			logger.Error(err, "Failed to reconcile connection pooler")
			rc.emitWarning(postgresCluster, EventPoolerReconcileFailed, fmt.Sprintf("Failed to reconcile connection pooler: %v", err))
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to reconcile connection pooler: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		logger.Info("Connection Poolers created, requeueing to check readiness")
		if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerCreating,
			"Connection poolers are being provisioned", provisioningClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil

	case func() bool {
		rwPooler := &cnpgv1.Pooler{}
		rwErr := c.Get(ctx, types.NamespacedName{
			Name:      poolerResourceName(postgresCluster.Name, readWriteEndpoint),
			Namespace: postgresCluster.Namespace,
		}, rwPooler)
		roPooler := &cnpgv1.Pooler{}
		roErr := c.Get(ctx, types.NamespacedName{
			Name:      poolerResourceName(postgresCluster.Name, readOnlyEndpoint),
			Namespace: postgresCluster.Namespace,
		}, roPooler)
		return rwErr != nil || roErr != nil || !arePoolersReady(rwPooler, roPooler)
	}():
		logger.Info("Connection Poolers are not ready yet, requeueing")
		if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerCreating,
			"Connection poolers are being provisioned", pendingClusterPhase); statusErr != nil {
			if apierrors.IsConflict(statusErr) {
				logger.Info("Conflict updating pooler status, will requeue")
				return ctrl.Result{Requeue: true}, nil
			}
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil

	default:
		oldConditions := make([]metav1.Condition, len(postgresCluster.Status.Conditions))
		copy(oldConditions, postgresCluster.Status.Conditions)
		if err := syncPoolerStatus(ctx, c, postgresCluster); err != nil {
			logger.Error(err, "Failed to sync pooler status")
			rc.emitWarning(postgresCluster, EventPoolerReconcileFailed, fmt.Sprintf("Failed to sync pooler status: %v", err))
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to sync pooler status: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		rc.emitPoolerReadyTransition(postgresCluster, oldConditions)
	}

	// 8. Reconcile ConfigMap when CNPG cluster is healthy.
	if cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy {
		logger.Info("CNPG Cluster is ready, reconciling ConfigMap for connection details")
		desiredCM, err := generateConfigMap(ctx, c, rc.Scheme, postgresCluster, cnpgCluster, postgresSecretName)
		if err != nil {
			logger.Error(err, "Failed to generate ConfigMap")
			rc.emitWarning(postgresCluster, EventConfigMapReconcileFailed, fmt.Sprintf("Failed to reconcile ConfigMap: %v", err))
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonConfigMapFailed,
				fmt.Sprintf("Failed to generate ConfigMap: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desiredCM.Name, Namespace: desiredCM.Namespace}}
		createOrUpdateResult, err := controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
			cm.Data = desiredCM.Data
			cm.Annotations = desiredCM.Annotations
			cm.Labels = desiredCM.Labels
			if !metav1.IsControlledBy(cm, postgresCluster) {
				if err := ctrl.SetControllerReference(postgresCluster, cm, rc.Scheme); err != nil {
					return fmt.Errorf("set controller reference failed: %w", err)
				}
			}
			return nil
		})
		if err != nil {
			logger.Error(err, "Failed to reconcile ConfigMap", "name", desiredCM.Name)
			rc.emitWarning(postgresCluster, EventConfigMapReconcileFailed, fmt.Sprintf("Failed to reconcile ConfigMap: %v", err))
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonConfigMapFailed,
				fmt.Sprintf("Failed to reconcile ConfigMap: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		switch createOrUpdateResult {
		case controllerutil.OperationResultCreated:
			rc.emitNormal(postgresCluster, EventConfigMapReady, fmt.Sprintf("ConfigMap %s created", desiredCM.Name))
			logger.Info("ConfigMap created", "name", desiredCM.Name)
		case controllerutil.OperationResultUpdated:
			rc.emitNormal(postgresCluster, EventConfigMapReady, fmt.Sprintf("ConfigMap %s updated", desiredCM.Name))
			logger.Info("ConfigMap updated", "name", desiredCM.Name)
		default:
			logger.Info("ConfigMap unchanged", "name", desiredCM.Name)
		}
		if postgresCluster.Status.Resources.ConfigMapRef == nil {
			postgresCluster.Status.Resources.ConfigMapRef = &corev1.LocalObjectReference{Name: desiredCM.Name}
		}
	}

	// 9. Final status sync.
	var oldPhase string
	if postgresCluster.Status.Phase != nil {
		oldPhase = *postgresCluster.Status.Phase
	}
	if err := syncStatus(ctx, c, postgresCluster, cnpgCluster); err != nil {
		logger.Error(err, "Failed to sync status")
		if apierrors.IsConflict(err) {
			logger.Info("Conflict during status update, will requeue")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to sync status: %w", err)
	}
	var newPhase string
	if postgresCluster.Status.Phase != nil {
		newPhase = *postgresCluster.Status.Phase
	}
	rc.emitClusterPhaseTransition(postgresCluster, oldPhase, newPhase)
	if cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy {
		rwPooler := &cnpgv1.Pooler{}
		rwErr := c.Get(ctx, types.NamespacedName{
			Name:      poolerResourceName(postgresCluster.Name, readWriteEndpoint),
			Namespace: postgresCluster.Namespace,
		}, rwPooler)
		roPooler := &cnpgv1.Pooler{}
		roErr := c.Get(ctx, types.NamespacedName{
			Name:      poolerResourceName(postgresCluster.Name, readOnlyEndpoint),
			Namespace: postgresCluster.Namespace,
		}, roPooler)
		if rwErr == nil && roErr == nil && arePoolersReady(rwPooler, roPooler) {
			logger.Info("Poolers are ready, syncing pooler status")
			poolerOldConditions := make([]metav1.Condition, len(postgresCluster.Status.Conditions))
			copy(poolerOldConditions, postgresCluster.Status.Conditions)
			_ = syncPoolerStatus(ctx, c, postgresCluster)
			rc.emitPoolerReadyTransition(postgresCluster, poolerOldConditions)
		}
	}
	logger.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

// getMergedConfig overlays PostgresCluster spec on top of the class defaults.
// Class values are used only where the cluster spec is silent.
func getMergedConfig(class *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) (*MergedConfig, error) {
	result := cluster.Spec.DeepCopy()

	// Config is optional on the class — apply defaults only when provided.
	if defaults := class.Spec.Config; defaults != nil {
		if result.Instances == nil {
			result.Instances = defaults.Instances
		}
		if result.PostgresVersion == nil {
			result.PostgresVersion = defaults.PostgresVersion
		}
		if result.Resources == nil {
			result.Resources = defaults.Resources
		}
		if result.Storage == nil {
			result.Storage = defaults.Storage
		}
		if len(result.PostgreSQLConfig) == 0 {
			result.PostgreSQLConfig = defaults.PostgreSQLConfig
		}
		if len(result.PgHBA) == 0 {
			result.PgHBA = defaults.PgHBA
		}
	}

	if result.Instances == nil || result.PostgresVersion == nil || result.Storage == nil {
		return nil, fmt.Errorf("invalid configuration for class %s: instances, postgresVersion and storage are required", class.Name)
	}
	if result.PostgreSQLConfig == nil {
		result.PostgreSQLConfig = make(map[string]string)
	}
	if result.PgHBA == nil {
		result.PgHBA = make([]string, 0)
	}
	if result.Resources == nil {
		result.Resources = &corev1.ResourceRequirements{}
	}

	return &MergedConfig{Spec: result, CNPG: class.Spec.CNPG}, nil
}

// buildCNPGClusterSpec builds the desired CNPG ClusterSpec.
// IMPORTANT: any field added here must also appear in normalizeCNPGClusterSpec,
// otherwise spec drift will be silently ignored.
func buildCNPGClusterSpec(cfg *MergedConfig, secretName string) cnpgv1.ClusterSpec {
	return cnpgv1.ClusterSpec{
		ImageName: fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", *cfg.Spec.PostgresVersion),
		Instances: int(*cfg.Spec.Instances),
		PostgresConfiguration: cnpgv1.PostgresConfiguration{
			Parameters: cfg.Spec.PostgreSQLConfig,
			PgHBA:      cfg.Spec.PgHBA,
		},
		SuperuserSecret:       &cnpgv1.LocalObjectReference{Name: secretName},
		EnableSuperuserAccess: ptr.To(true),
		Bootstrap: &cnpgv1.BootstrapConfiguration{
			InitDB: &cnpgv1.BootstrapInitDB{
				Database: defaultDatabaseName,
				Owner:    superUsername,
				Secret:   &cnpgv1.LocalObjectReference{Name: secretName},
			},
		},
		StorageConfiguration: cnpgv1.StorageConfiguration{
			Size: cfg.Spec.Storage.String(),
		},
		Resources: *cfg.Spec.Resources,
	}
}

func buildCNPGCluster(scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, secretName string) *cnpgv1.Cluster {
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cfg, secretName),
	}
	ctrl.SetControllerReference(cluster, cnpg, scheme)
	return cnpg
}

func normalizeCNPGClusterSpec(spec cnpgv1.ClusterSpec, customDefinedParameters map[string]string) normalizedCNPGClusterSpec {
	normalized := normalizedCNPGClusterSpec{
		ImageName:   spec.ImageName,
		Instances:   spec.Instances,
		StorageSize: spec.StorageConfiguration.Size,
		Resources:   spec.Resources,
	}
	if len(customDefinedParameters) > 0 {
		normalized.CustomDefinedParameters = make(map[string]string)
		for k := range customDefinedParameters {
			normalized.CustomDefinedParameters[k] = spec.PostgresConfiguration.Parameters[k]
		}
	}
	if len(spec.PostgresConfiguration.PgHBA) > 0 {
		normalized.PgHBA = spec.PostgresConfiguration.PgHBA
	}
	if spec.Bootstrap != nil && spec.Bootstrap.InitDB != nil {
		normalized.DefaultDatabase = spec.Bootstrap.InitDB.Database
		normalized.Owner = spec.Bootstrap.InitDB.Owner
	}
	return normalized
}

// reconcileManagedRoles synchronizes ManagedRoles from PostgresCluster spec to CNPG Cluster managed.roles.
func reconcileManagedRoles(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	logger := log.FromContext(ctx)

	if len(cluster.Spec.ManagedRoles) == 0 {
		logger.Info("No managed roles to reconcile")
		return nil
	}

	desiredRoles := make([]cnpgv1.RoleConfiguration, 0, len(cluster.Spec.ManagedRoles))
	for _, role := range cluster.Spec.ManagedRoles {
		r := cnpgv1.RoleConfiguration{
			Name:   role.Name,
			Ensure: cnpgv1.EnsureAbsent,
		}
		// Exists bool replaces the old Ensure string enum ("present"/"absent").
		if role.Exists {
			r.Ensure = cnpgv1.EnsurePresent
			r.Login = true
		}
		if role.PasswordSecretRef != nil {
			// Pass only the secret name to CNPG — CNPG always reads the "password" key.
			r.PasswordSecret = &cnpgv1.LocalObjectReference{Name: role.PasswordSecretRef.LocalObjectReference.Name}
		}
		desiredRoles = append(desiredRoles, r)
	}

	var currentRoles []cnpgv1.RoleConfiguration
	if cnpgCluster.Spec.Managed != nil {
		currentRoles = cnpgCluster.Spec.Managed.Roles
	}

	if equality.Semantic.DeepEqual(currentRoles, desiredRoles) {
		logger.Info("CNPG Cluster roles already match desired state, no update needed")
		return nil
	}

	logger.Info("CNPG Cluster roles differ from desired state, updating",
		"currentCount", len(currentRoles), "desiredCount", len(desiredRoles))

	originalCluster := cnpgCluster.DeepCopy()
	if cnpgCluster.Spec.Managed == nil {
		cnpgCluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	cnpgCluster.Spec.Managed.Roles = desiredRoles

	if err := c.Patch(ctx, cnpgCluster, client.MergeFrom(originalCluster)); err != nil {
		return fmt.Errorf("failed to patch CNPG Cluster with managed roles: %w", err)
	}
	logger.Info("Successfully updated CNPG Cluster with managed roles", "roleCount", len(desiredRoles))
	return nil
}

func poolerResourceName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s%s%s", clusterName, defaultPoolerSuffix, poolerType)
}

func poolerExists(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, poolerType string) bool {
	pooler := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(cluster.Name, poolerType),
		Namespace: cluster.Namespace,
	}, pooler)
	if apierrors.IsNotFound(err) {
		return false
	}
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to check pooler existence", "type", poolerType)
		return false
	}
	return true
}

func arePoolersReady(rwPooler, roPooler *cnpgv1.Pooler) bool {
	return isPoolerReady(rwPooler) && isPoolerReady(roPooler)
}

// isPoolerReady checks if a pooler has all instances scheduled.
// CNPG PoolerStatus only tracks scheduled instances, not ready pods.
func isPoolerReady(pooler *cnpgv1.Pooler) bool {
	desired := int32(1)
	if pooler.Spec.Instances != nil {
		desired = *pooler.Spec.Instances
	}
	return pooler.Status.Instances >= desired
}

func poolerInstanceCount(p *cnpgv1.Pooler) (desired, scheduled int32) {
	desired = 1
	if p.Spec.Instances != nil {
		desired = *p.Spec.Instances
	}
	return desired, p.Status.Instances
}

// createOrUpdateConnectionPoolers creates RW and RO poolers if they don't exist.
func createOrUpdateConnectionPoolers(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster) error {
	if err := createConnectionPooler(ctx, c, scheme, cluster, cfg, cnpgCluster, readWriteEndpoint); err != nil {
		return fmt.Errorf("failed to reconcile RW pooler: %w", err)
	}
	if err := createConnectionPooler(ctx, c, scheme, cluster, cfg, cnpgCluster, readOnlyEndpoint); err != nil {
		return fmt.Errorf("failed to reconcile RO pooler: %w", err)
	}
	return nil
}

func createConnectionPooler(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string) error {
	poolerName := poolerResourceName(cluster.Name, poolerType)
	existing := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	log.FromContext(ctx).Info("Creating CNPG Pooler", "name", poolerName, "type", poolerType)
	return c.Create(ctx, buildCNPGPooler(scheme, cluster, cfg, cnpgCluster, poolerType))
}

func buildCNPGPooler(scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string) *cnpgv1.Pooler {
	pc := cfg.CNPG.ConnectionPooler
	instances := *pc.Instances
	mode := cnpgv1.PgBouncerPoolMode(*pc.Mode)
	pooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, poolerType), Namespace: cluster.Namespace},
		Spec: cnpgv1.PoolerSpec{
			Cluster:   cnpgv1.LocalObjectReference{Name: cnpgCluster.Name},
			Instances: &instances,
			Type:      cnpgv1.PoolerType(poolerType),
			PgBouncer: &cnpgv1.PgBouncerSpec{
				PoolMode:   mode,
				Parameters: pc.Config,
			},
		},
	}
	ctrl.SetControllerReference(cluster, pooler, scheme)
	return pooler
}

// deleteConnectionPoolers removes RW and RO poolers if they exist.
func deleteConnectionPoolers(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster) error {
	logger := log.FromContext(ctx)
	for _, poolerType := range []string{readWriteEndpoint, readOnlyEndpoint} {
		poolerName := poolerResourceName(cluster.Name, poolerType)
		if !poolerExists(ctx, c, cluster, poolerType) {
			continue
		}
		pooler := &cnpgv1.Pooler{}
		if err := c.Get(ctx, types.NamespacedName{Name: poolerName, Namespace: cluster.Namespace}, pooler); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get pooler %s: %w", poolerName, err)
		}
		logger.Info("Deleting CNPG Pooler", "name", poolerName)
		if err := c.Delete(ctx, pooler); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete pooler %s: %w", poolerName, err)
		}
	}
	return nil
}

// syncPoolerStatus populates ConnectionPoolerStatus and the PoolerReady condition.
func syncPoolerStatus(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster) error {
	rwPooler := &cnpgv1.Pooler{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(cluster.Name, readWriteEndpoint),
		Namespace: cluster.Namespace,
	}, rwPooler); err != nil {
		return err
	}

	roPooler := &cnpgv1.Pooler{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      poolerResourceName(cluster.Name, readOnlyEndpoint),
		Namespace: cluster.Namespace,
	}, roPooler); err != nil {
		return err
	}

	cluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{Enabled: true}
	rwDesired, rwScheduled := poolerInstanceCount(rwPooler)
	roDesired, roScheduled := poolerInstanceCount(roPooler)

	return setStatus(ctx, c, cluster, poolerReady, metav1.ConditionTrue, reasonAllInstancesReady,
		fmt.Sprintf("%s: %d/%d, %s: %d/%d", readWriteEndpoint, rwScheduled, rwDesired, readOnlyEndpoint, roScheduled, roDesired),
		readyClusterPhase)
}

// syncStatus maps CNPG Cluster state to PostgresCluster status.
func syncStatus(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	cluster.Status.ProvisionerRef = &corev1.ObjectReference{
		APIVersion: "postgresql.cnpg.io/v1",
		Kind:       "Cluster",
		Namespace:  cnpgCluster.Namespace,
		Name:       cnpgCluster.Name,
		UID:        cnpgCluster.UID,
	}

	var phase reconcileClusterPhases
	var condStatus metav1.ConditionStatus
	var reason conditionReasons
	var message string

	switch cnpgCluster.Status.Phase {
	case cnpgv1.PhaseHealthy:
		phase, condStatus, reason, message = readyClusterPhase, metav1.ConditionTrue, reasonCNPGClusterHealthy, "Cluster is up and running"
	case cnpgv1.PhaseFirstPrimary, cnpgv1.PhaseCreatingReplica, cnpgv1.PhaseWaitingForInstancesToBeActive:
		phase, condStatus, reason = provisioningClusterPhase, metav1.ConditionFalse, reasonCNPGProvisioning
		message = fmt.Sprintf("CNPG cluster provisioning: %s", cnpgCluster.Status.Phase)
	case cnpgv1.PhaseSwitchover:
		phase, condStatus, reason, message = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGSwitchover, "Cluster changing primary node"
	case cnpgv1.PhaseFailOver:
		phase, condStatus, reason, message = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGFailingOver, "Pod missing, need to change primary"
	case cnpgv1.PhaseInplacePrimaryRestart, cnpgv1.PhaseInplaceDeletePrimaryRestart:
		phase, condStatus, reason = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGRestarting
		message = fmt.Sprintf("CNPG cluster restarting: %s", cnpgCluster.Status.Phase)
	case cnpgv1.PhaseUpgrade, cnpgv1.PhaseMajorUpgrade, cnpgv1.PhaseUpgradeDelayed, cnpgv1.PhaseOnlineUpgrading:
		phase, condStatus, reason = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGUpgrading
		message = fmt.Sprintf("CNPG cluster upgrading: %s", cnpgCluster.Status.Phase)
	case cnpgv1.PhaseApplyingConfiguration:
		phase, condStatus, reason, message = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGApplyingConfig, "Configuration change is being applied"
	case cnpgv1.PhaseReplicaClusterPromotion:
		phase, condStatus, reason, message = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGPromoting, "Replica is being promoted to primary"
	case cnpgv1.PhaseWaitingForUser:
		phase, condStatus, reason, message = failedClusterPhase, metav1.ConditionFalse, reasonCNPGWaitingForUser, "Action from the user is required"
	case cnpgv1.PhaseUnrecoverable:
		phase, condStatus, reason, message = failedClusterPhase, metav1.ConditionFalse, reasonCNPGUnrecoverable, "Cluster failed, needs manual intervention"
	case cnpgv1.PhaseCannotCreateClusterObjects:
		phase, condStatus, reason, message = failedClusterPhase, metav1.ConditionFalse, reasonCNPGProvisioningFailed, "Cluster resources cannot be created"
	case cnpgv1.PhaseUnknownPlugin, cnpgv1.PhaseFailurePlugin:
		phase, condStatus, reason = failedClusterPhase, metav1.ConditionFalse, reasonCNPGPluginError
		message = fmt.Sprintf("CNPG plugin error: %s", cnpgCluster.Status.Phase)
	case cnpgv1.PhaseImageCatalogError, cnpgv1.PhaseArchitectureBinaryMissing:
		phase, condStatus, reason = failedClusterPhase, metav1.ConditionFalse, reasonCNPGImageError
		message = fmt.Sprintf("CNPG image error: %s", cnpgCluster.Status.Phase)
	case "":
		phase, condStatus, reason, message = pendingClusterPhase, metav1.ConditionFalse, reasonCNPGProvisioning, "CNPG cluster is pending creation"
	default:
		phase, condStatus, reason = provisioningClusterPhase, metav1.ConditionFalse, reasonCNPGProvisioning
		message = fmt.Sprintf("CNPG cluster phase: %s", cnpgCluster.Status.Phase)
	}

	return setStatus(ctx, c, cluster, clusterReady, condStatus, reason, message, phase)
}

// setStatus sets the phase, condition and persists the status.
func setStatus(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, condType conditionTypes, status metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileClusterPhases) error {
	p := string(phase)
	cluster.Status.Phase = &p
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               string(condType),
		Status:             status,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
	if err := c.Status().Update(ctx, cluster); err != nil {
		return fmt.Errorf("failed to update PostgresCluster status: %w", err)
	}
	return nil
}

// generateConfigMap builds a ConfigMap with connection details for the PostgresCluster.
func generateConfigMap(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, secretName string) (*corev1.ConfigMap, error) {
	cmName := fmt.Sprintf("%s%s", cluster.Name, defaultConfigMapSuffix)
	if cluster.Status.Resources != nil && cluster.Status.Resources.ConfigMapRef != nil {
		cmName = cluster.Status.Resources.ConfigMapRef.Name
	}

	data := map[string]string{
		"CLUSTER_RW_ENDPOINT":   fmt.Sprintf("%s-rw.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		"CLUSTER_RO_ENDPOINT":   fmt.Sprintf("%s-ro.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		"CLUSTER_R_ENDPOINT":    fmt.Sprintf("%s-r.%s", cnpgCluster.Name, cnpgCluster.Namespace),
		"DEFAULT_CLUSTER_PORT":  defaultPort,
		"SUPER_USER_NAME":       superUsername,
		"SUPER_USER_SECRET_REF": secretName,
	}
	if poolerExists(ctx, c, cluster, readWriteEndpoint) && poolerExists(ctx, c, cluster, readOnlyEndpoint) {
		data["CLUSTER_POOLER_RW_ENDPOINT"] = fmt.Sprintf("%s.%s", poolerResourceName(cnpgCluster.Name, readWriteEndpoint), cnpgCluster.Namespace)
		data["CLUSTER_POOLER_RO_ENDPOINT"] = fmt.Sprintf("%s.%s", poolerResourceName(cnpgCluster.Name, readOnlyEndpoint), cnpgCluster.Namespace)
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cluster.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "postgrescluster-controller"},
		},
		Data: data,
	}
	if err := ctrl.SetControllerReference(cluster, cm, scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}
	return cm, nil
}

// ensureClusterSecret creates the superuser secret if it doesn't exist and persists the ref to status.
func ensureClusterSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, secretName string, secret *corev1.Secret) error {
	err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cluster.Namespace}, secret)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if apierrors.IsNotFound(err) {
		pw, err := generatePassword()
		if err != nil {
			return err
		}
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: cluster.Namespace},
			StringData: map[string]string{"username": superUsername, "password": pw},
			Type:       corev1.SecretTypeOpaque,
		}
		if err := ctrl.SetControllerReference(cluster, newSecret, scheme); err != nil {
			return err
		}
		if err := c.Create(ctx, newSecret); err != nil {
			return err
		}
	}
	if cluster.Status.Resources == nil {
		cluster.Status.Resources = &enterprisev4.PostgresClusterResources{}
	}
	cluster.Status.Resources.SuperUserSecretRef = &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
		Key:                  secretKeyPassword,
	}
	return nil
}

func clusterSecretExists(ctx context.Context, c client.Client, namespace, name string, secret *corev1.Secret) (bool, error) {
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// deleteCNPGCluster deletes the CNPG Cluster if it exists.
func deleteCNPGCluster(ctx context.Context, c client.Client, cnpgCluster *cnpgv1.Cluster) error {
	logger := log.FromContext(ctx)
	if cnpgCluster == nil {
		logger.Info("CNPG Cluster not found, skipping deletion")
		return nil
	}
	logger.Info("Deleting CNPG Cluster", "name", cnpgCluster.Name)
	if err := c.Delete(ctx, cnpgCluster); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to delete CNPG Cluster: %w", err)
	}
	return nil
}

// handleFinalizer processes deletion cleanup: removes poolers, then deletes or orphans the CNPG Cluster
// based on ClusterDeletionPolicy, then removes the finalizer.
func handleFinalizer(ctx context.Context, rc *ReconcileContext, cluster *enterprisev4.PostgresCluster, secret *corev1.Secret) error {
	c := rc.Client
	scheme := rc.Scheme
	logger := log.FromContext(ctx)
	if cluster.GetDeletionTimestamp() == nil {
		logger.Info("PostgresCluster not marked for deletion, skipping finalizer logic")
		return nil
	}
	if !controllerutil.ContainsFinalizer(cluster, PostgresClusterFinalizerName) {
		logger.Info("Finalizer not present on PostgresCluster, skipping finalizer logic")
		return nil
	}

	cnpgCluster := &cnpgv1.Cluster{}
	err := c.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cnpgCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cnpgCluster = nil
			logger.Info("CNPG cluster not found during cleanup")
		} else {
			return fmt.Errorf("failed to fetch CNPG cluster during cleanup: %w", err)
		}
	}
	logger.Info("Processing finalizer cleanup for PostgresCluster")

	// Emit deletion event — policy is resolved below, use the raw pointer for the message.
	policy := ""
	if cluster.Spec.ClusterDeletionPolicy != nil {
		policy = *cluster.Spec.ClusterDeletionPolicy
	}
	rc.emitNormal(cluster, EventClusterDeleting, fmt.Sprintf("Starting cleanup (policy: %s)", policy))

	if err := deleteConnectionPoolers(ctx, c, cluster); err != nil {
		logger.Error(err, "Failed to delete connection poolers during cleanup")
		return fmt.Errorf("failed to delete connection poolers: %w", err)
	}

	switch policy {
	case clusterDeletionPolicyDelete:
		logger.Info("ClusterDeletionPolicy is 'Delete', deleting CNPG Cluster and associated resources")
		if cnpgCluster != nil {
			if err := deleteCNPGCluster(ctx, c, cnpgCluster); err != nil {
				logger.Error(err, "Failed to delete CNPG Cluster during finalizer cleanup")
				return fmt.Errorf("failed to delete CNPG Cluster during finalizer cleanup: %w", err)
			}
		} else {
			logger.Info("CNPG Cluster not found, skipping deletion")
		}

	case clusterDeletionPolicyRetain:
		logger.Info("ClusterDeletionPolicy is 'Retain', removing owner references to orphan CNPG Cluster")
		if cnpgCluster != nil {
			originalCNPG := cnpgCluster.DeepCopy()
			refRemoved, err := removeOwnerRef(scheme, cluster, cnpgCluster)
			if err != nil {
				return fmt.Errorf("failed to remove owner reference from CNPG cluster: %w", err)
			}
			if !refRemoved {
				logger.Info("Owner reference already removed from CNPG Cluster, skipping patch")
			}
			if err := patchObject(ctx, c, originalCNPG, cnpgCluster, "CNPGCluster"); err != nil {
				return fmt.Errorf("failed to patch CNPG cluster after removing owner reference: %w", err)
			}
			logger.Info("Removed owner reference from CNPG Cluster")
		}

		// Remove owner reference from the superuser Secret to prevent cascading deletion.
		if cluster.Status.Resources != nil && cluster.Status.Resources.SuperUserSecretRef != nil {
			secretName := cluster.Status.Resources.SuperUserSecretRef.Name
			if err := c.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cluster.Namespace}, secret); err != nil {
				if !apierrors.IsNotFound(err) {
					logger.Error(err, "Failed to fetch Secret during cleanup")
					return fmt.Errorf("failed to fetch secret during cleanup: %w", err)
				}
				logger.Info("Secret not found, skipping owner reference removal", "secret", secretName)
			} else {
				originalSecret := secret.DeepCopy()
				refRemoved, err := removeOwnerRef(scheme, cluster, secret)
				if err != nil {
					return fmt.Errorf("failed to remove owner reference from Secret: %w", err)
				}
				if refRemoved {
					if err := patchObject(ctx, c, originalSecret, secret, "Secret"); err != nil {
						return fmt.Errorf("failed to patch Secret after removing owner reference: %w", err)
					}
				}
				logger.Info("Removed owner reference from Secret")
			}
		}

	default:
		logger.Info("Unknown ClusterDeletionPolicy", "policy", policy)
	}

	controllerutil.RemoveFinalizer(cluster, PostgresClusterFinalizerName)
	if err := c.Update(ctx, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("PostgresCluster already deleted, skipping finalizer update")
			return nil
		}
		logger.Error(err, "Failed to remove finalizer from PostgresCluster")
		return fmt.Errorf("failed to remove finalizer: %w", err)
	}
	rc.emitNormal(cluster, EventCleanupComplete, "Cleanup complete")
	logger.Info("Finalizer removed, cleanup complete")
	return nil
}

func removeOwnerRef(scheme *runtime.Scheme, owner, obj client.Object) (bool, error) {
	hasRef, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), owner, scheme)
	if err != nil {
		return false, fmt.Errorf("failed to check owner reference: %w", err)
	}
	if !hasRef {
		return false, nil
	}
	if err := controllerutil.RemoveOwnerReference(owner, obj, scheme); err != nil {
		return false, fmt.Errorf("failed to remove owner reference: %w", err)
	}
	return true, nil
}

// patchObject patches obj from original; treats NotFound as a no-op.
func patchObject(ctx context.Context, c client.Client, original, obj client.Object, kind objectKind) error {
	logger := log.FromContext(ctx)
	if err := c.Patch(ctx, obj, client.MergeFrom(original)); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Object not found, skipping patch", "kind", kind, "name", obj.GetName())
			return nil
		}
		return fmt.Errorf("failed to patch %s object: %w", kind, err)
	}
	logger.Info("Patched object successfully", "kind", kind, "name", obj.GetName())
	return nil
}

func generatePassword() (string, error) {
	const (
		length  = 32
		digits  = 8
		symbols = 0
	)
	return password.Generate(length, digits, symbols, false, true)
}
