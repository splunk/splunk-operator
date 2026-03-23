package core

import (
	"context"
	"fmt"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/sethvargo/go-password/password"
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
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PostgresClusterService is the entry point called by the thin controller adapter.
// It owns the full reconciliation flow for a PostgresCluster CR.
func PostgresClusterService(ctx context.Context, c client.Client, scheme *runtime.Scheme, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PostgresCluster", "name", req.Name, "namespace", req.Namespace)

	var cnpgCluster *cnpgv1.Cluster
	var postgresSecretName string
	secret := &corev1.Secret{}

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

	// Finalizer must run before any provisioning to ensure cleanup on deletion.
	if err := handleFinalizer(ctx, c, scheme, postgresCluster, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to handle finalizer")
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

	if !controllerutil.ContainsFinalizer(postgresCluster, postgresClusterFinalizerName) {
		controllerutil.AddFinalizer(postgresCluster, postgresClusterFinalizerName)
		if err := c.Update(ctx, postgresCluster); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Load referenced class — defines defaults for instances, storage, postgres version, etc.
	postgresClusterClass := &enterprisev4.PostgresClusterClass{}
	if err := c.Get(ctx, client.ObjectKey{Name: postgresCluster.Spec.Class}, postgresClusterClass); err != nil {
		logger.Error(err, "Unable to fetch referenced PostgresClusterClass", "className", postgresCluster.Spec.Class)
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterClassNotFound,
			fmt.Sprintf("ClusterClass %s not found: %v", postgresCluster.Spec.Class, err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	mergedConfig, err := getMergedConfig(postgresClusterClass, postgresCluster)
	if err != nil {
		logger.Error(err, "Failed to merge PostgresCluster configuration")
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonInvalidConfiguration,
			fmt.Sprintf("Failed to merge configuration: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Resolve the secret name: reuse the one written to status on first run, generate on first run.
	if postgresCluster.Status.Resources != nil && postgresCluster.Status.Resources.SuperUserSecretRef != nil {
		postgresSecretName = postgresCluster.Status.Resources.SuperUserSecretRef.Name
	} else {
		postgresSecretName = postgresCluster.Name + defaultSecretSuffix
	}

	secretExists, err := clusterSecretExists(ctx, c, postgresCluster.Namespace, postgresSecretName, secret)
	if err != nil {
		logger.Error(err, "Failed to check secret existence", "name", postgresSecretName)
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonUserSecretFailed,
			fmt.Sprintf("Failed to check secret existence: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	if !secretExists {
		if err := ensureClusterSecret(ctx, c, scheme, postgresCluster, postgresSecretName, secret); err != nil {
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonUserSecretFailed,
				fmt.Sprintf("Failed to generate PostgresCluster secret: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		if err := c.Status().Update(ctx, postgresCluster); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
	}

	// Re-attach ownerRef if it was stripped (e.g. by a Retain-policy deletion of a previous cluster).
	hasOwnerRef, err := controllerutil.HasOwnerReference(secret.GetOwnerReferences(), postgresCluster, scheme)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to check owner reference on secret: %w", err)
	}
	if secretExists && !hasOwnerRef {
		original := secret.DeepCopy()
		if err := ctrl.SetControllerReference(postgresCluster, secret, scheme); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to set controller reference on existing secret: %w", err)
		}
		if err := patchObject(ctx, c, original, secret, "Secret"); err != nil {
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonSuperUserSecretFailed,
				fmt.Sprintf("Failed to patch existing secret: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
	}

	if postgresCluster.Status.Resources.SuperUserSecretRef == nil {
		postgresCluster.Status.Resources.SuperUserSecretRef = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: postgresSecretName},
			Key:                  secretKeyPassword,
		}
	}

	desiredSpec := buildCNPGClusterSpec(mergedConfig, postgresSecretName)

	existingCNPG := &cnpgv1.Cluster{}
	err = c.Get(ctx, types.NamespacedName{Name: postgresCluster.Name, Namespace: postgresCluster.Namespace}, existingCNPG)
	switch {
	case apierrors.IsNotFound(err):
		newCluster := buildCNPGCluster(scheme, postgresCluster, mergedConfig, postgresSecretName)
		if err := c.Create(ctx, newCluster); err != nil {
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterBuildFailed,
				fmt.Sprintf("Failed to create CNPG Cluster: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterBuildSucceeded,
			"CNPG Cluster created", pendingClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil

	case err != nil:
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterGetFailed,
			fmt.Sprintf("Failed to get CNPG Cluster: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	cnpgCluster = existingCNPG
	currentNorm := normalizeCNPGClusterSpec(cnpgCluster.Spec, mergedConfig.Spec.PostgreSQLConfig)
	desiredNorm := normalizeCNPGClusterSpec(desiredSpec, mergedConfig.Spec.PostgreSQLConfig)

	if !equality.Semantic.DeepEqual(currentNorm, desiredNorm) {
		logger.Info("Detected drift in CNPG Cluster spec, patching", "name", cnpgCluster.Name)
		original := cnpgCluster.DeepCopy()
		cnpgCluster.Spec = desiredSpec
		switch patchErr := patchObject(ctx, c, original, cnpgCluster, "CNPGCluster"); {
		case apierrors.IsConflict(patchErr):
			return ctrl.Result{Requeue: true}, nil
		case patchErr != nil:
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterPatchFailed,
				fmt.Sprintf("Failed to patch CNPG Cluster: %v", patchErr), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, patchErr
		default:
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
	}

	if err := reconcileManagedRoles(ctx, c, postgresCluster, cnpgCluster); err != nil {
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonManagedRolesFailed,
			fmt.Sprintf("Failed to reconcile managed roles: %v", err), failedClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	poolerEnabled := mergedConfig.Spec.ConnectionPoolerEnabled != nil && *mergedConfig.Spec.ConnectionPoolerEnabled
	switch {
	case !poolerEnabled:
		if err := deleteConnectionPoolers(ctx, c, postgresCluster); err != nil {
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to delete connection poolers: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		postgresCluster.Status.ConnectionPoolerStatus = nil
		meta.RemoveStatusCondition(&postgresCluster.Status.Conditions, string(poolerReady))

	case !poolerExists(ctx, c, postgresCluster, readWriteEndpoint) || !poolerExists(ctx, c, postgresCluster, readOnlyEndpoint):
		if mergedConfig.CNPG.ConnectionPooler == nil {
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerConfigMissing,
				fmt.Sprintf("Connection pooler is enabled but no config found in class %q or cluster %q",
					postgresCluster.Spec.Class, postgresCluster.Name),
				failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, nil
		}
		if cnpgCluster.Status.Phase != cnpgv1.PhaseHealthy {
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonCNPGClusterNotHealthy,
				"Waiting for CNPG cluster to become healthy before creating poolers", pendingClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
		if err := createOrUpdateConnectionPoolers(ctx, c, scheme, postgresCluster, mergedConfig, cnpgCluster); err != nil {
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to reconcile connection pooler: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerCreating,
			"Connection poolers are being provisioned", provisioningClusterPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil

	case !arePoolersReady(ctx, c, postgresCluster):
		if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerCreating,
			"Connection poolers are being provisioned", pendingClusterPhase); statusErr != nil {
			if apierrors.IsConflict(statusErr) {
				return ctrl.Result{Requeue: true}, nil
			}
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil

	default:
		if err := syncPoolerStatus(ctx, c, postgresCluster); err != nil {
			if statusErr := updateStatus(poolerReady, metav1.ConditionFalse, reasonPoolerReconciliationFailed,
				fmt.Sprintf("Failed to sync pooler status: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
	}

	if cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy {
		desiredCM, err := generateConfigMap(ctx, c, scheme, postgresCluster, cnpgCluster, postgresSecretName)
		if err != nil {
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonConfigMapFailed,
				fmt.Sprintf("Failed to generate ConfigMap: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desiredCM.Name, Namespace: desiredCM.Namespace}}
		if _, err := controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
			cm.Data = desiredCM.Data
			cm.Annotations = desiredCM.Annotations
			cm.Labels = desiredCM.Labels
			if !metav1.IsControlledBy(cm, postgresCluster) {
				return ctrl.SetControllerReference(postgresCluster, cm, scheme)
			}
			return nil
		}); err != nil {
			if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonConfigMapFailed,
				fmt.Sprintf("Failed to reconcile ConfigMap: %v", err), failedClusterPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		if postgresCluster.Status.Resources.ConfigMapRef == nil {
			postgresCluster.Status.Resources.ConfigMapRef = &corev1.LocalObjectReference{Name: desiredCM.Name}
		}
	}

	if err := syncStatus(ctx, c, postgresCluster, cnpgCluster); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to sync status: %w", err)
	}
	if cnpgCluster.Status.Phase == cnpgv1.PhaseHealthy && arePoolersReady(ctx, c, postgresCluster) {
		_ = syncPoolerStatus(ctx, c, postgresCluster)
	}

	logger.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

// getMergedConfig overlays PostgresCluster spec on top of the class defaults.
// Class values are used only where the cluster spec is silent.
func getMergedConfig(class *enterprisev4.PostgresClusterClass, cluster *enterprisev4.PostgresCluster) (*MergedConfig, error) {
	result := cluster.Spec.DeepCopy()
	defaults := class.Spec.Config

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

	if result.Instances == nil || result.PostgresVersion == nil || result.Storage == nil {
		return nil, fmt.Errorf("invalid configuration for class %s: instances, postgresVersion and storage are required", class.Name)
	}

	if result.PostgreSQLConfig == nil {
		result.PostgreSQLConfig = make(map[string]string)
	}
	if result.PgHBA == nil {
		result.PgHBA = []string{}
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
		StorageConfiguration: cnpgv1.StorageConfiguration{Size: cfg.Spec.Storage.String()},
		Resources:            *cfg.Spec.Resources,
	}
}

func buildCNPGCluster(scheme *runtime.Scheme, postgresCluster *enterprisev4.PostgresCluster, cfg *MergedConfig, secretName string) *cnpgv1.Cluster {
	c := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: postgresCluster.Name, Namespace: postgresCluster.Namespace},
		Spec:       buildCNPGClusterSpec(cfg, secretName),
	}
	ctrl.SetControllerReference(postgresCluster, c, scheme)
	return c
}

// normalizeCNPGClusterSpec extracts only the fields we set so equality checks
// ignore CNPG-injected defaults that would otherwise cause constant re-patches.
func normalizeCNPGClusterSpec(spec cnpgv1.ClusterSpec, customParams map[string]string) normalizedCNPGClusterSpec {
	norm := normalizedCNPGClusterSpec{
		ImageName:   spec.ImageName,
		Instances:   spec.Instances,
		StorageSize: spec.StorageConfiguration.Size,
		Resources:   spec.Resources,
	}
	if len(customParams) > 0 {
		norm.CustomDefinedParameters = make(map[string]string)
		for k := range customParams {
			norm.CustomDefinedParameters[k] = spec.PostgresConfiguration.Parameters[k]
		}
	}
	if len(spec.PostgresConfiguration.PgHBA) > 0 {
		norm.PgHBA = spec.PostgresConfiguration.PgHBA
	}
	if spec.Bootstrap != nil && spec.Bootstrap.InitDB != nil {
		norm.DefaultDatabase = spec.Bootstrap.InitDB.Database
		norm.Owner = spec.Bootstrap.InitDB.Owner
	}
	return norm
}

func reconcileManagedRoles(ctx context.Context, c client.Client, postgresCluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	logger := log.FromContext(ctx)

	if len(postgresCluster.Spec.ManagedRoles) == 0 {
		return nil
	}

	desired := make([]cnpgv1.RoleConfiguration, 0, len(postgresCluster.Spec.ManagedRoles))
	for _, role := range postgresCluster.Spec.ManagedRoles {
		r := cnpgv1.RoleConfiguration{Name: role.Name}
		r.Ensure = cnpgv1.EnsureAbsent
		if role.Exists {
			r.Ensure = cnpgv1.EnsurePresent
			r.Login = true
		}
		if role.PasswordSecretRef != nil {
			r.PasswordSecret = &cnpgv1.LocalObjectReference{Name: role.PasswordSecretRef.Name}
		}
		desired = append(desired, r)
	}

	var current []cnpgv1.RoleConfiguration
	if cnpgCluster.Spec.Managed != nil {
		current = cnpgCluster.Spec.Managed.Roles
	}
	if equality.Semantic.DeepEqual(current, desired) {
		return nil
	}

	logger.Info("Updating CNPG Cluster managed roles", "desiredCount", len(desired))
	original := cnpgCluster.DeepCopy()
	if cnpgCluster.Spec.Managed == nil {
		cnpgCluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	cnpgCluster.Spec.Managed.Roles = desired
	if err := c.Patch(ctx, cnpgCluster, client.MergeFrom(original)); err != nil {
		return fmt.Errorf("patching CNPG Cluster managed roles: %w", err)
	}
	return nil
}

func poolerResourceName(clusterName, poolerType string) string {
	return fmt.Sprintf("%s%s%s", clusterName, defaultPoolerSuffix, poolerType)
}

func poolerExists(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, poolerType string) bool {
	p := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{Name: poolerResourceName(cluster.Name, poolerType), Namespace: cluster.Namespace}, p)
	return err == nil
}

func arePoolersReady(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster) bool {
	rw := &cnpgv1.Pooler{}
	rwErr := c.Get(ctx, types.NamespacedName{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace}, rw)
	ro := &cnpgv1.Pooler{}
	roErr := c.Get(ctx, types.NamespacedName{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace}, ro)
	return isPoolerReady(rw, rwErr) && isPoolerReady(ro, roErr)
}

func isPoolerReady(p *cnpgv1.Pooler, err error) bool {
	if err != nil {
		return false
	}
	desired := int32(1)
	if p.Spec.Instances != nil {
		desired = *p.Spec.Instances
	}
	return p.Status.Instances >= desired
}

func createOrUpdateConnectionPoolers(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster) error {
	if err := createPoolerIfMissing(ctx, c, scheme, cluster, cfg, cnpgCluster, readWriteEndpoint); err != nil {
		return fmt.Errorf("reconciling RW pooler: %w", err)
	}
	if err := createPoolerIfMissing(ctx, c, scheme, cluster, cfg, cnpgCluster, readOnlyEndpoint); err != nil {
		return fmt.Errorf("reconciling RO pooler: %w", err)
	}
	return nil
}

func createPoolerIfMissing(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string) error {
	name := poolerResourceName(cluster.Name, poolerType)
	existing := &cnpgv1.Pooler{}
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	log.FromContext(ctx).Info("Creating CNPG Pooler", "name", name, "type", poolerType)
	return c.Create(ctx, buildCNPGPooler(scheme, cluster, cfg, cnpgCluster, poolerType))
}

func buildCNPGPooler(scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpgCluster *cnpgv1.Cluster, poolerType string) *cnpgv1.Pooler {
	pc := cfg.CNPG.ConnectionPooler
	instances := *pc.Instances
	pooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, poolerType), Namespace: cluster.Namespace},
		Spec: cnpgv1.PoolerSpec{
			Cluster:   cnpgv1.LocalObjectReference{Name: cnpgCluster.Name},
			Instances: &instances,
			Type:      cnpgv1.PoolerType(poolerType),
			PgBouncer: &cnpgv1.PgBouncerSpec{
				PoolMode:   cnpgv1.PgBouncerPoolMode(*pc.Mode),
				Parameters: pc.Config,
			},
		},
	}
	ctrl.SetControllerReference(cluster, pooler, scheme)
	return pooler
}

func deleteConnectionPoolers(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster) error {
	for _, poolerType := range []string{readWriteEndpoint, readOnlyEndpoint} {
		name := poolerResourceName(cluster.Name, poolerType)
		p := &cnpgv1.Pooler{}
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: cluster.Namespace}, p); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting pooler %s: %w", name, err)
		}
		if err := c.Delete(ctx, p); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting pooler %s: %w", name, err)
		}
	}
	return nil
}

func syncPoolerStatus(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster) error {
	rw := &cnpgv1.Pooler{}
	if err := c.Get(ctx, types.NamespacedName{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace}, rw); err != nil {
		return err
	}
	ro := &cnpgv1.Pooler{}
	if err := c.Get(ctx, types.NamespacedName{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace}, ro); err != nil {
		return err
	}

	cluster.Status.ConnectionPoolerStatus = &enterprisev4.ConnectionPoolerStatus{Enabled: true}
	rwDesired, rwScheduled := poolerInstanceCount(rw)
	roDesired, roScheduled := poolerInstanceCount(ro)
	return setStatus(ctx, c, cluster, poolerReady, metav1.ConditionTrue, reasonAllInstancesReady,
		fmt.Sprintf("%s: %d/%d, %s: %d/%d", readWriteEndpoint, rwScheduled, rwDesired, readOnlyEndpoint, roScheduled, roDesired),
		readyClusterPhase)
}

func poolerInstanceCount(p *cnpgv1.Pooler) (desired, scheduled int32) {
	desired = 1
	if p.Spec.Instances != nil {
		desired = *p.Spec.Instances
	}
	return desired, p.Status.Instances
}

// syncStatus maps every known CNPG phase to a PostgresCluster phase/condition pair.
func syncStatus(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	cluster.Status.ProvisionerRef = &corev1.ObjectReference{
		APIVersion: "postgresql.cnpg.io/v1",
		Kind:       "Cluster",
		Namespace:  cnpgCluster.Namespace,
		Name:       cnpgCluster.Name,
		UID:        cnpgCluster.UID,
	}

	var phase reconcileClusterPhases
	var status metav1.ConditionStatus
	var reason conditionReasons
	var message string

	switch cnpgCluster.Status.Phase {
	case cnpgv1.PhaseHealthy:
		phase, status, reason, message = readyClusterPhase, metav1.ConditionTrue, reasonCNPGClusterHealthy, "Cluster is up and running"
	case cnpgv1.PhaseFirstPrimary, cnpgv1.PhaseCreatingReplica, cnpgv1.PhaseWaitingForInstancesToBeActive:
		phase, status, reason = provisioningClusterPhase, metav1.ConditionFalse, reasonCNPGProvisioning
		message = fmt.Sprintf("CNPG cluster provisioning: %s", cnpgCluster.Status.Phase)
	case cnpgv1.PhaseSwitchover:
		phase, status, reason, message = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGSwitchover, "Cluster changing primary node"
	case cnpgv1.PhaseFailOver:
		phase, status, reason, message = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGFailingOver, "Pod missing, need to change primary"
	case cnpgv1.PhaseInplacePrimaryRestart, cnpgv1.PhaseInplaceDeletePrimaryRestart:
		phase, status, reason = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGRestarting
		message = fmt.Sprintf("CNPG cluster restarting: %s", cnpgCluster.Status.Phase)
	case cnpgv1.PhaseUpgrade, cnpgv1.PhaseMajorUpgrade, cnpgv1.PhaseUpgradeDelayed, cnpgv1.PhaseOnlineUpgrading:
		phase, status, reason = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGUpgrading
		message = fmt.Sprintf("CNPG cluster upgrading: %s", cnpgCluster.Status.Phase)
	case cnpgv1.PhaseApplyingConfiguration:
		phase, status, reason, message = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGApplyingConfig, "Configuration change is being applied"
	case cnpgv1.PhaseReplicaClusterPromotion:
		phase, status, reason, message = configuringClusterPhase, metav1.ConditionFalse, reasonCNPGPromoting, "Replica is being promoted to primary"
	case cnpgv1.PhaseWaitingForUser:
		phase, status, reason, message = failedClusterPhase, metav1.ConditionFalse, reasonCNPGWaitingForUser, "Action from the user is required"
	case cnpgv1.PhaseUnrecoverable:
		phase, status, reason, message = failedClusterPhase, metav1.ConditionFalse, reasonCNPGUnrecoverable, "Cluster failed, needs manual intervention"
	case cnpgv1.PhaseCannotCreateClusterObjects:
		phase, status, reason, message = failedClusterPhase, metav1.ConditionFalse, reasonCNPGProvisioningFailed, "Cluster resources cannot be created"
	case cnpgv1.PhaseUnknownPlugin, cnpgv1.PhaseFailurePlugin:
		phase, status, reason = failedClusterPhase, metav1.ConditionFalse, reasonCNPGPluginError
		message = fmt.Sprintf("CNPG plugin error: %s", cnpgCluster.Status.Phase)
	case cnpgv1.PhaseImageCatalogError, cnpgv1.PhaseArchitectureBinaryMissing:
		phase, status, reason = failedClusterPhase, metav1.ConditionFalse, reasonCNPGImageError
		message = fmt.Sprintf("CNPG image error: %s", cnpgCluster.Status.Phase)
	case "":
		phase, status, reason, message = pendingClusterPhase, metav1.ConditionFalse, reasonCNPGProvisioning, "CNPG cluster is pending creation"
	default:
		phase, status, reason = provisioningClusterPhase, metav1.ConditionFalse, reasonCNPGProvisioning
		message = fmt.Sprintf("CNPG cluster phase: %s", cnpgCluster.Status.Phase)
	}

	return setStatus(ctx, c, cluster, clusterReady, status, reason, message, phase)
}

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
		return fmt.Errorf("updating PostgresCluster status: %w", err)
	}
	return nil
}

func generateConfigMap(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, secretName string) (*corev1.ConfigMap, error) {
	name := cluster.Name + defaultConfigMapSuffix
	if cluster.Status.Resources != nil && cluster.Status.Resources.ConfigMapRef != nil {
		name = cluster.Status.Resources.ConfigMapRef.Name
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
			Name:      name,
			Namespace: cluster.Namespace,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "postgrescluster-controller"},
		},
		Data: data,
	}
	if err := ctrl.SetControllerReference(cluster, cm, scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference on ConfigMap: %w", err)
	}
	return cm, nil
}

// ensureClusterSecret creates the superuser secret when it doesn't exist and records
// its name in status so future reconciles can find it even if it's been renamed.
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

func handleFinalizer(ctx context.Context, c client.Client, scheme *runtime.Scheme, cluster *enterprisev4.PostgresCluster, secret *corev1.Secret) error {
	logger := log.FromContext(ctx)
	if cluster.GetDeletionTimestamp() == nil {
		return nil
	}
	if !controllerutil.ContainsFinalizer(cluster, postgresClusterFinalizerName) {
		return nil
	}

	cnpgCluster := &cnpgv1.Cluster{}
	err := c.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, cnpgCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cnpgCluster = nil
		} else {
			return fmt.Errorf("fetching CNPG cluster during cleanup: %w", err)
		}
	}

	if err := deleteConnectionPoolers(ctx, c, cluster); err != nil {
		return fmt.Errorf("deleting connection poolers: %w", err)
	}

	policy := ""
	if cluster.Spec.ClusterDeletionPolicy != nil {
		policy = *cluster.Spec.ClusterDeletionPolicy
	}
	switch policy {
	case clusterDeletionPolicyDelete:
		if cnpgCluster != nil {
			if err := c.Delete(ctx, cnpgCluster); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting CNPG Cluster: %w", err)
			}
		}
	case clusterDeletionPolicyRetain:
		if cnpgCluster != nil {
			original := cnpgCluster.DeepCopy()
			if removed, err := removeOwnerRef(scheme, cluster, cnpgCluster); err != nil {
				return fmt.Errorf("removing owner ref from CNPG cluster: %w", err)
			} else if removed {
				if err := patchObject(ctx, c, original, cnpgCluster, "CNPGCluster"); err != nil {
					return fmt.Errorf("patching CNPG cluster after owner ref removal: %w", err)
				}
			}
		}
		if cluster.Status.Resources != nil && cluster.Status.Resources.SuperUserSecretRef != nil {
			if err := c.Get(ctx, types.NamespacedName{Name: cluster.Status.Resources.SuperUserSecretRef.Name, Namespace: cluster.Namespace}, secret); err != nil {
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("fetching secret during cleanup: %w", err)
				}
			} else {
				original := secret.DeepCopy()
				if removed, err := removeOwnerRef(scheme, cluster, secret); err != nil {
					return fmt.Errorf("removing owner ref from Secret: %w", err)
				} else if removed {
					if err := patchObject(ctx, c, original, secret, "Secret"); err != nil {
						return fmt.Errorf("patching Secret after owner ref removal: %w", err)
					}
				}
			}
		}
	default:
		logger.Info("Unknown ClusterDeletionPolicy", "policy", cluster.Spec.ClusterDeletionPolicy)
	}

	controllerutil.RemoveFinalizer(cluster, postgresClusterFinalizerName)
	if err := c.Update(ctx, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("removing finalizer: %w", err)
	}
	return nil
}

func removeOwnerRef(scheme *runtime.Scheme, owner, obj client.Object) (bool, error) {
	has, err := controllerutil.HasOwnerReference(obj.GetOwnerReferences(), owner, scheme)
	if err != nil || !has {
		return false, err
	}
	if err := controllerutil.RemoveOwnerReference(owner, obj, scheme); err != nil {
		return false, err
	}
	return true, nil
}

func patchObject(ctx context.Context, c client.Client, original, obj client.Object, kind objectKind) error {
	if err := c.Patch(ctx, obj, client.MergeFrom(original)); err != nil {
		if apierrors.IsNotFound(err) {
			log.FromContext(ctx).Info("Object not found during patch, skipping", "kind", kind, "name", obj.GetName())
			return nil
		}
		return fmt.Errorf("patching %s: %w", kind, err)
	}
	return nil
}

// generatePassword uses crypto/rand (via sethvargo/go-password) — predictable passwords
// are unacceptable for credentials that protect live database access.
func generatePassword() (string, error) {
	const (
		length  = 32
		digits  = 8
		symbols = 0
	)
	return password.Generate(length, digits, symbols, false, true)
}
