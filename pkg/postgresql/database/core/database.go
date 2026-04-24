package core

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"slices"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/sethvargo/go-password/password"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NewDBRepoFunc constructs a DBRepo adapter for the given host and database.
// Injected by the controller so the core never imports the pgx adapter directly.
type NewDBRepoFunc func(ctx context.Context, host, dbName, password string) (DBRepo, error)

type secretReconcileError struct {
	message string
	reason  conditionReasons
}

type secretMissingPolicy int

const (
	createSecretIfMissing secretMissingPolicy = iota
	reportSecretDriftIfMissing
)

func (e *secretReconcileError) Error() string {
	return e.message
}

func requeueOnConflict(ctx context.Context, err error, category reconcileConflictCategory, action string) (ctrl.Result, error, bool) {
	if !errors.IsConflict(err) {
		return ctrl.Result{}, err, false
	}

	// Keep the category stable so future metrics or events can aggregate conflict sources.
	log.FromContext(ctx).Info(
		"Conflict during PostgresDatabase reconciliation, will requeue",
		"category", category,
		"action", action,
	)
	return ctrl.Result{Requeue: true}, nil, true
}

// PostgresDatabaseService is the application service entry point called by the primary adapter (reconciler).
// newDBRepo is injected to keep the core free of pgx imports.
func PostgresDatabaseService(
	ctx context.Context,
	rc *ReconcileContext,
	postgresDB *enterprisev4.PostgresDatabase,
	newDBRepo NewDBRepoFunc,
) (ctrl.Result, error) {
	c := rc.Client
	logger := log.FromContext(ctx).WithValues("postgresDatabase", postgresDB.Name)
	ctx = log.IntoContext(ctx, logger)
	logger.Info("Reconciling PostgresDatabase")
	wasReady := postgresDB.Status.Phase != nil && *postgresDB.Status.Phase == string(readyDBPhase)
	previouslyProvisionedDatabases := existingDatabaseStatus(postgresDB)

	updateStatus := func(conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
		return persistStatus(ctx, c, rc.Metrics, postgresDB, conditionType, conditionStatus, reason, message, phase)
	}

	// Finalizer: cleanup on deletion, register on creation.
	if postgresDB.GetDeletionTimestamp() != nil {
		if err := handleDeletion(ctx, rc, postgresDB); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDeletion, "handling deletion"); ok {
				return result, conflictErr
			}
			logger.Error(err, "Failed to clean up PostgresDatabase")
			rc.emitWarning(postgresDB, EventCleanupFailed, fmt.Sprintf("Cleanup failed: %v", err))
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(postgresDB, postgresDatabaseFinalizerName) {
		controllerutil.AddFinalizer(postgresDB, postgresDatabaseFinalizerName)
		if err := c.Update(ctx, postgresDB); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictFinalizer, "adding finalizer"); ok {
				return result, conflictErr
			}
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		logger.Info("Finalizer added successfully")
		return ctrl.Result{}, nil
	}

	// Phase: ClusterValidation
	cluster, err := fetchCluster(ctx, c, postgresDB)
	if err != nil {
		if errors.IsNotFound(err) {
			rc.emitWarning(postgresDB, EventClusterNotFound, fmt.Sprintf("PostgresCluster %s not found", postgresDB.Spec.ClusterRef.Name))
			if err := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterNotFound, "Cluster CR not found", pendingDBPhase); err != nil {
				if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictClusterStatus, "persisting cluster not found status"); ok {
					return result, conflictErr
				}
				return ctrl.Result{}, err
			}
			return ctrl.Result{RequeueAfter: clusterNotFoundRetryDelay}, nil
		}
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterInfoFetchFailed,
			"Can't reach Cluster CR due to transient errors", pendingDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictClusterStatus, "persisting cluster fetch failure status"); ok {
				return result, conflictErr
			}
			logger.Error(statusErr, "Failed to persist cluster status")
		}
		return ctrl.Result{}, err
	}
	clusterStatus := getClusterReadyStatus(cluster)
	logger.Info("Cluster validation completed", "clusterRef", postgresDB.Spec.ClusterRef.Name, "status", clusterStatus)

	switch clusterStatus {
	case ClusterNotReady, ClusterNoProvisionerRef:
		rc.emitWarning(postgresDB, EventClusterNotReady, "Referenced PostgresCluster is not ready yet")
		if err := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterProvisioning, "Cluster is not in ready state yet", pendingDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictClusterStatus, "persisting cluster provisioning status"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil

	case ClusterReady:
		rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, clusterReady, EventClusterValidated, "Referenced PostgresCluster is ready")
		if err := updateStatus(clusterReady, metav1.ConditionTrue, reasonClusterAvailable, "Cluster is operational", provisioningDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictClusterStatus, "persisting cluster ready status"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
	}

	// Phase: RoleConflictCheck — verify no other SSA field manager already owns our roles.
	roleConflicts := getRoleConflicts(postgresDB, cluster)
	if len(roleConflicts) > 0 {
		conflictMsg := fmt.Sprintf("Role conflict: %s. "+
			"If you deleted a previous PostgresDatabase, recreate it with the original name to re-adopt the orphaned resources.",
			strings.Join(roleConflicts, ", "))
		conflictErr := fmt.Errorf("role conflict detected: %s", strings.Join(roleConflicts, ", "))
		logger.Error(conflictErr, "Failed to validate managed role ownership", "conflicts", roleConflicts)
		rc.emitWarning(postgresDB, EventRoleConflict, conflictMsg)
		errs := []error{conflictErr}
		if statusErr := updateStatus(rolesReady, metav1.ConditionFalse, reasonRoleConflict, conflictMsg, failedDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictRoleConflictStatus, "persisting role conflict status"); ok {
				return result, conflictErr
			}
			logger.Error(statusErr, "Failed to persist role conflict status")
			errs = append(errs, fmt.Errorf("failed to update status: %w", statusErr))
		}
		return ctrl.Result{}, stderrors.Join(errs...)
	}

	// We need the CNPG Cluster directly because PostgresCluster status does not yet
	// surface managed role reconciliation state.
	cnpgCluster := &cnpgv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.ProvisionerRef.Name,
		Namespace: cluster.Status.ProvisionerRef.Namespace,
	}, cnpgCluster); err != nil {
		logger.Error(err, "Failed to fetch CNPG Cluster", "cluster", cluster.Status.ProvisionerRef.Name)
		return ctrl.Result{}, err
	}

	// Phase: CredentialProvisioning — secrets must exist before roles are patched.
	// CNPG rejects a PasswordSecretRef pointing at a missing secret.
	if err := reconcileRoleSecrets(ctx, c, rc.Scheme, postgresDB, previouslyProvisionedDatabases); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictSecretsReconcile, "reconciling user secrets"); ok {
			return result, conflictErr
		}
		var secretErr *secretReconcileError
		if stderrors.As(err, &secretErr) {
			rc.emitWarning(postgresDB, EventRolesSecretsDriftDetected, secretErr.message)
			if statusErr := updateStatus(secretsReady, metav1.ConditionFalse, secretErr.reason,
				secretErr.message, provisioningDBPhase); statusErr != nil {
				if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictSecretsStatus, "persisting secret drift status"); ok {
					return result, conflictErr
				}
				logger.Error(statusErr, "Failed to persist secret drift status")
			}
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}
		rc.emitWarning(postgresDB, EventRoleSecretsFailed, fmt.Sprintf("Failed to reconcile user secrets: %v", err))
		if statusErr := updateStatus(secretsReady, metav1.ConditionFalse, reasonSecretsCreationFailed,
			fmt.Sprintf("Failed to reconcile user secrets: %v", err), provisioningDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictSecretsStatus, "persisting secret failure status"); ok {
				return result, conflictErr
			}
			logger.Error(statusErr, "Failed to persist secrets status")
		}
		return ctrl.Result{}, err
	}
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, secretsReady, EventSecretsReady, fmt.Sprintf("All secrets provisioned for %d databases", len(postgresDB.Spec.Databases)))
	if err := updateStatus(secretsReady, metav1.ConditionTrue, reasonSecretsCreated,
		fmt.Sprintf("All secrets provisioned for %d databases", len(postgresDB.Spec.Databases)), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictSecretsStatus, "persisting secrets ready status"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, err
	}

	// Phase: ConnectionMetadata — ConfigMaps carry connection info consumers need as soon
	// as databases are ready, so they are created alongside secrets.
	endpoints := resolveClusterEndpoints(cluster, cnpgCluster, postgresDB.Namespace)
	if err := reconcileRoleConfigMaps(ctx, c, rc.Scheme, postgresDB, endpoints); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictConfigMapsReconcile, "reconciling configmaps"); ok {
			return result, conflictErr
		}
		rc.emitWarning(postgresDB, EventAccessConfigFailed, fmt.Sprintf("Failed to reconcile ConfigMaps: %v", err))
		if statusErr := updateStatus(configMapsReady, metav1.ConditionFalse, reasonConfigMapsCreationFailed,
			fmt.Sprintf("Failed to reconcile ConfigMaps: %v", err), provisioningDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictConfigMapsStatus, "persisting configmaps failure status"); ok {
				return result, conflictErr
			}
			logger.Error(statusErr, "Failed to persist configmaps status")
		}
		return ctrl.Result{}, err
	}
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, configMapsReady, EventConfigMapsReady, fmt.Sprintf("All ConfigMaps provisioned for %d databases", len(postgresDB.Spec.Databases)))
	if err := updateStatus(configMapsReady, metav1.ConditionTrue, reasonConfigMapsCreated,
		fmt.Sprintf("All ConfigMaps provisioned for %d databases", len(postgresDB.Spec.Databases)), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictConfigMapsStatus, "persisting configmaps ready status"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, err
	}

	// Phase: RoleProvisioning
	fieldManager := fieldManagerName(postgresDB.Name)
	desired := buildDesiredRoles(postgresDB.Name, postgresDB.Spec.Databases)
	rolesToAdd := findAddedRoleNames(cluster, desired)
	rolesToRemove := absentRolesByName(findRemovedRoleNames(cluster, fieldManager, desired))
	allRoles := append(desired, rolesToRemove...)

	if len(rolesToAdd) > 0 || len(rolesToRemove) > 0 {
		logger.Info("Managed roles patch started", "addCount", len(rolesToAdd), "removeCount", len(rolesToRemove))
		if err := patchManagedRoles(ctx, c, fieldManager, cluster, allRoles); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictManagedRolesPatch, "patching managed roles"); ok {
				return result, conflictErr
			}
			logger.Error(err, "Failed to patch managed roles", "roleCount", len(allRoles))
			rc.emitWarning(postgresDB, EventManagedRolesPatchFailed, fmt.Sprintf("Failed to patch managed roles: %v", err))
			if statusErr := updateStatus(rolesReady, metav1.ConditionFalse, reasonRolesCreationFailed,
				fmt.Sprintf("Failed to patch managed roles: %v", err), failedDBPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to persist roles status")
			}
			return ctrl.Result{}, err
		}
		logger.Info("Managed roles patched", "roleCount", len(allRoles))
		rc.emitNormal(postgresDB, EventRoleReconciliationStarted, fmt.Sprintf("Patched managed roles: %d to add, %d to remove", len(rolesToAdd), len(rolesToRemove)))
		if err := updateStatus(rolesReady, metav1.ConditionFalse, reasonWaitingForCNPG,
			fmt.Sprintf("Waiting for roles to be reconciled: %d to add, %d to remove", len(rolesToAdd), len(rolesToRemove)), provisioningDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRolesStatus, "persisting roles waiting status"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	roleNames := getDesiredRoles(postgresDB)
	notReadyRoles, err := verifyRolesReady(ctx, roleNames, cnpgCluster)
	if err != nil {
		rc.emitWarning(postgresDB, EventRoleFailed, fmt.Sprintf("Role reconciliation failed: %v", err))
		if statusErr := updateStatus(rolesReady, metav1.ConditionFalse, reasonRolesCreationFailed,
			fmt.Sprintf("Role creation failed: %v", err), failedDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictRolesStatus, "persisting role failure status"); ok {
				return result, conflictErr
			}
			logger.Error(statusErr, "Failed to persist roles status")
		}
		return ctrl.Result{}, err
	}
	if len(notReadyRoles) > 0 {
		if err := updateStatus(rolesReady, metav1.ConditionFalse, reasonWaitingForCNPG,
			fmt.Sprintf("Waiting for roles to be reconciled: %v", notReadyRoles), provisioningDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRolesStatus, "persisting roles pending status"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, rolesReady, EventRolesReady, fmt.Sprintf("Roles reconciled: %d active, %d removed", len(rolesToAdd), len(rolesToRemove)))
	if err := updateStatus(rolesReady, metav1.ConditionTrue, reasonRolesAvailable,
		fmt.Sprintf("Roles reconciled: %d active, %d removed", len(rolesToAdd), len(rolesToRemove)), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRolesStatus, "persisting roles ready status"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, err
	}

	// Phase: DatabaseProvisioning
	adopted, err := reconcileCNPGDatabases(ctx, c, rc.Scheme, postgresDB, cluster)
	if err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictCNPGDatabasesReconcile, "reconciling CNPG databases"); ok {
			return result, conflictErr
		}
		logger.Error(err, "Failed to reconcile CNPG Databases")
		rc.emitWarning(postgresDB, EventDatabasesReconcileFailed, fmt.Sprintf("Failed to reconcile databases: %v", err))
		if statusErr := updateStatus(databasesReady, metav1.ConditionFalse, reasonDatabaseReconcileFailed,
			fmt.Sprintf("Failed to reconcile databases: %v", err), failedDBPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to persist databases status")
		}
		return ctrl.Result{}, err
	}
	if len(adopted) > 0 {
		rc.emitNormal(postgresDB, EventResourcesAdopted, fmt.Sprintf("Adopted retained databases: %v", adopted))
	}

	notReadyDBs, err := verifyDatabasesReady(ctx, c, postgresDB)
	if err != nil {
		logger.Error(err, "Failed to verify database readiness")
		return ctrl.Result{}, err
	}
	if len(notReadyDBs) > 0 {
		rc.emitOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, databasesReady, EventDatabaseReconciliationStarted, fmt.Sprintf("Reconciling %d databases, waiting for readiness", len(postgresDB.Spec.Databases)))
		if err := updateStatus(databasesReady, metav1.ConditionFalse, reasonWaitingForCNPG,
			fmt.Sprintf("Waiting for databases to be ready: %v", notReadyDBs), provisioningDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting databases pending status"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, databasesReady, EventDatabasesReady, fmt.Sprintf("All %d databases ready", len(postgresDB.Spec.Databases)))
	if err := updateStatus(databasesReady, metav1.ConditionTrue, reasonDatabasesAvailable,
		fmt.Sprintf("All %d databases ready", len(postgresDB.Spec.Databases)), readyDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting databases ready status"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, err
	}

	// Phase: RWRolePrivileges
	// Skipped when no new databases are detected — ALTER DEFAULT PRIVILEGES covers tables
	// added by migrations on existing databases. Re-runs for all databases when a new one
	// is added (idempotent for existing ones, required for the new one).
	if hasNewDatabases(postgresDB) {
		// Read from our own status — we created this secret and wrote the SecretKeySelector
		// (name + key) when the cluster was provisioned. This avoids depending on CNPG's
		// spec field and makes the key explicit.
		if cluster.Status.Resources == nil || cluster.Status.Resources.SuperUserSecretRef == nil {
			return ctrl.Result{}, fmt.Errorf("postgresCluster %s has no superuser secret ref in status", cluster.Name)
		}
		superSecretRef := cluster.Status.Resources.SuperUserSecretRef
		superSecret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{
			Name:      superSecretRef.Name,
			Namespace: postgresDB.Namespace,
		}, superSecret); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to fetch superuser secret %s: %w", superSecretRef.Name, err)
		}
		pw, ok := superSecret.Data[superSecretRef.Key]
		if !ok {
			return ctrl.Result{}, fmt.Errorf("superuser secret %s missing %q key", superSecretRef.Name, superSecretRef.Key)
		}

		dbNames := make([]string, 0, len(postgresDB.Spec.Databases))
		for _, dbSpec := range postgresDB.Spec.Databases {
			dbNames = append(dbNames, dbSpec.Name)
		}

		if err := reconcileRWRolePrivileges(ctx, endpoints.RWHost, string(pw), dbNames, newDBRepo); err != nil {
			rc.emitWarning(postgresDB, EventPrivilegesGrantFailed, fmt.Sprintf("Failed to grant RW role privileges: %v", err))
			if statusErr := updateStatus(privilegesReady, metav1.ConditionFalse, reasonPrivilegesGrantFailed,
				fmt.Sprintf("Failed to grant RW role privileges: %v", err), provisioningDBPhase); statusErr != nil {
				if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictPrivilegesStatus, "persisting privileges failure status"); ok {
					return result, conflictErr
				}
				logger.Error(statusErr, "Failed to persist privileges status")
			}
			return ctrl.Result{}, err
		}
		rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, privilegesReady, EventPrivilegesReady, fmt.Sprintf("RW role privileges granted for all %d databases", len(postgresDB.Spec.Databases)))
		if err := updateStatus(privilegesReady, metav1.ConditionTrue, reasonPrivilegesGranted,
			fmt.Sprintf("RW role privileges granted for all %d databases", len(postgresDB.Spec.Databases)), readyDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictPrivilegesStatus, "persisting privileges ready status"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
	}

	if !wasReady {
		rc.emitNormal(postgresDB, EventPostgresDatabaseReady, fmt.Sprintf("PostgresDatabase %s is ready", postgresDB.Name))
	}
	postgresDB.Status.Databases = populateDatabaseStatus(postgresDB)
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation

	if err := c.Status().Update(ctx, postgresDB); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictFinalStatus, "persisting final status"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, fmt.Errorf("failed to persist final status: %w", err)
	}

	logger.Info("PostgresDatabase reconciliation completed")
	return ctrl.Result{}, nil
}

// reconcileRWRolePrivileges calls the DBRepo port for each database.
// Errors are collected so all databases are attempted before returning.
func reconcileRWRolePrivileges(
	ctx context.Context,
	rwHost, superPassword string,
	dbNames []string,
	newDBRepo NewDBRepoFunc,
) error {
	logger := log.FromContext(ctx)
	var errs []error
	for _, dbName := range dbNames {
		repo, err := newDBRepo(ctx, rwHost, dbName, superPassword)
		if err != nil {
			errs = append(errs, fmt.Errorf("connecting to database %s: %w", dbName, err))
			continue
		}
		if err := repo.ExecGrants(ctx, dbName); err != nil {
			errs = append(errs, fmt.Errorf("granting RW privileges on database %s: %w", dbName, err))
			continue
		}
		logger.Info("RW role privileges granted", "database", dbName, "rwRole", rwRoleName(dbName))
	}
	return stderrors.Join(errs...)
}

func fetchCluster(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase) (*enterprisev4.PostgresCluster, error) {
	cluster := &enterprisev4.PostgresCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}

func getClusterReadyStatus(cluster *enterprisev4.PostgresCluster) clusterReadyStatus {
	if cluster.Status.Phase == nil || *cluster.Status.Phase != string(ClusterReady) {
		return ClusterNotReady
	}
	if cluster.Status.ProvisionerRef == nil {
		return ClusterNoProvisionerRef
	}
	return ClusterReady
}

func getDesiredRoles(postgresDB *enterprisev4.PostgresDatabase) []string {
	users := make([]string, 0, len(postgresDB.Spec.Databases)*2)
	for _, dbSpec := range postgresDB.Spec.Databases {
		users = append(users, adminRoleName(dbSpec.Name), rwRoleName(dbSpec.Name))
	}
	return users
}

func existingDatabaseStatus(postgresDB *enterprisev4.PostgresDatabase) map[string]struct{} {
	if postgresDB.Status.Phase == nil || *postgresDB.Status.Phase != string(readyDBPhase) {
		return map[string]struct{}{}
	}
	existing := make(map[string]struct{}, len(postgresDB.Status.Databases))
	for _, database := range postgresDB.Status.Databases {
		existing[database.Name] = struct{}{}
	}
	return existing
}

// rolesMatchClusterSpec returns true if desired and actual contain the same roles
// (by name and Exists state), regardless of order.

func getRoleConflicts(postgresDB *enterprisev4.PostgresDatabase, cluster *enterprisev4.PostgresCluster) []string {
	myManager := fieldManagerName(postgresDB.Name)
	desired := make(map[string]struct{}, len(postgresDB.Spec.Databases)*2)
	for _, dbSpec := range postgresDB.Spec.Databases {
		desired[adminRoleName(dbSpec.Name)] = struct{}{}
		desired[rwRoleName(dbSpec.Name)] = struct{}{}
	}
	roleOwners := managedRoleOwners(cluster.ManagedFields)
	var conflicts []string
	for roleName := range desired {
		if owner, exists := roleOwners[roleName]; exists && owner != myManager {
			conflicts = append(conflicts, fmt.Sprintf("%s (owned by %s)", roleName, owner))
		}
	}
	return conflicts
}

func managedRoleOwners(managedFields []metav1.ManagedFieldsEntry) map[string]string {
	owners := make(map[string]string)
	for _, mf := range managedFields {
		if mf.FieldsV1 == nil {
			continue
		}
		for _, name := range parseRoleNames(mf.FieldsV1.Raw) {
			owners[name] = mf.Manager
		}
	}
	return owners
}

func parseRoleNames(raw []byte) []string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	spec, _ := fields["f:spec"].(map[string]any)
	roles, _ := spec["f:managedRoles"].(map[string]any)
	var names []string
	for key := range roles {
		var k struct{ Name string }
		if err := json.Unmarshal([]byte(strings.TrimPrefix(key, "k:")), &k); err == nil && k.Name != "" {
			names = append(names, k.Name)
		}
	}
	return names
}

func patchManagedRoles(ctx context.Context, c client.Client, fieldManager string, cluster *enterprisev4.PostgresCluster, roles []enterprisev4.ManagedRole) error {
	rolePatch, err := buildManagedRolesPatch(cluster, roles, c.Scheme())
	if err != nil {
		return fmt.Errorf("building managed roles patch: %w", err)
	}
	if err := c.Patch(ctx, rolePatch, client.Apply, client.FieldOwner(fieldManager)); err != nil {
		return fmt.Errorf("patching managed roles: %w", err)
	}
	return nil
}

func verifyRolesReady(_ context.Context, expectedRoles []string, cnpgCluster *cnpgv1.Cluster) ([]string, error) {
	if cnpgCluster.Status.ManagedRolesStatus.CannotReconcile != nil {
		for _, userName := range expectedRoles {
			if errs, exists := cnpgCluster.Status.ManagedRolesStatus.CannotReconcile[userName]; exists {
				return nil, fmt.Errorf("reconciling user %s: %v", userName, errs)
			}
		}
	}
	reconciled := cnpgCluster.Status.ManagedRolesStatus.ByStatus[cnpgv1.RoleStatusReconciled]
	var notReady []string
	for _, userName := range expectedRoles {
		if !slices.Contains(reconciled, userName) {
			notReady = append(notReady, userName)
		}
	}
	return notReady, nil
}

func reconcileCNPGDatabases(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, cluster *enterprisev4.PostgresCluster) ([]string, error) {
	logger := log.FromContext(ctx)
	var adopted []string
	for _, dbSpec := range postgresDB.Spec.Databases {
		cnpgDBName := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)
		reAdopted := false
		cnpgDB := &cnpgv1.Database{
			ObjectMeta: metav1.ObjectMeta{Name: cnpgDBName, Namespace: postgresDB.Namespace},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, c, cnpgDB, func() error {
			cnpgDB.Spec = buildCNPGDatabaseSpec(cluster.Status.ProvisionerRef.Name, dbSpec)
			reAdopted = cnpgDB.Annotations[annotationRetainedFrom] == postgresDB.Name
			if reAdopted {
				delete(cnpgDB.Annotations, annotationRetainedFrom)
				adopted = append(adopted, dbSpec.Name)
			}
			if cnpgDB.CreationTimestamp.IsZero() || reAdopted {
				return controllerutil.SetControllerReference(postgresDB, cnpgDB, scheme)
			}
			return nil
		})
		if err != nil {
			return adopted, fmt.Errorf("reconciling CNPG Database %s: %w", cnpgDBName, err)
		}
		if reAdopted {
			logger.Info("CNPG Database re-adopted", "name", cnpgDBName)
		}
	}
	return adopted, nil
}

func verifyDatabasesReady(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase) ([]string, error) {
	var notReady []string
	for _, dbSpec := range postgresDB.Spec.Databases {
		cnpgDBName := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)
		cnpgDB := &cnpgv1.Database{}
		if err := c.Get(ctx, types.NamespacedName{Name: cnpgDBName, Namespace: postgresDB.Namespace}, cnpgDB); err != nil {
			if errors.IsNotFound(err) {
				notReady = append(notReady, dbSpec.Name)
				continue
			}
			return nil, fmt.Errorf("getting CNPG Database %s: %w", cnpgDBName, err)
		}
		if cnpgDB.Status.Applied == nil || !*cnpgDB.Status.Applied {
			notReady = append(notReady, dbSpec.Name)
		}
	}
	return notReady, nil
}

func persistStatus(ctx context.Context, c client.Client, metrics ports.Recorder, db *enterprisev4.PostgresDatabase, conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
	applyStatus(db, conditionType, conditionStatus, reason, message, phase)
	metrics.IncStatusTransition(ports.ControllerDatabase, string(conditionType), string(conditionStatus), string(reason))
	return c.Status().Update(ctx, db)
}

func applyStatus(db *enterprisev4.PostgresDatabase, conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) {
	meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
		Type:               string(conditionType),
		Status:             conditionStatus,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: db.Generation,
	})
	p := string(phase)
	db.Status.Phase = &p
	db.Status.ObservedGeneration = &db.Generation
}

func buildDeletionPlan(databases []enterprisev4.DatabaseDefinition) deletionPlan {
	var plan deletionPlan
	for _, db := range databases {
		if db.DeletionPolicy == deletionPolicyRetain {
			plan.retained = append(plan.retained, db)
		} else {
			plan.deleted = append(plan.deleted, db)
		}
	}
	return plan
}

func handleDeletion(ctx context.Context, rc *ReconcileContext, postgresDB *enterprisev4.PostgresDatabase) error {
	logger := log.FromContext(ctx)
	c := rc.Client
	plan := buildDeletionPlan(postgresDB.Spec.Databases)
	if err := orphanRetainedResources(ctx, c, postgresDB, plan.retained); err != nil {
		return err
	}
	if err := deleteRemovedResources(ctx, c, postgresDB, plan.deleted); err != nil {
		return err
	}
	if err := cleanupManagedRoles(ctx, c, postgresDB, plan); err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(postgresDB, postgresDatabaseFinalizerName)
	if err := c.Update(ctx, postgresDB); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("removing finalizer: %w", err)
	}
	rc.emitNormal(postgresDB, EventCleanupComplete, fmt.Sprintf("Cleanup complete (%d retained, %d deleted)", len(plan.retained), len(plan.deleted)))
	logger.Info("Cleanup completed", "retained", len(plan.retained), "deleted", len(plan.deleted))
	return nil
}

func orphanRetainedResources(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, retained []enterprisev4.DatabaseDefinition) error {
	if err := orphanCNPGDatabases(ctx, c, postgresDB, retained); err != nil {
		return err
	}
	if err := orphanConfigMaps(ctx, c, postgresDB, retained); err != nil {
		return err
	}
	return orphanSecrets(ctx, c, postgresDB, retained)
}

func deleteRemovedResources(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, deleted []enterprisev4.DatabaseDefinition) error {
	if err := deleteCNPGDatabases(ctx, c, postgresDB, deleted); err != nil {
		return err
	}
	if err := deleteConfigMaps(ctx, c, postgresDB, deleted); err != nil {
		return err
	}
	return deleteSecrets(ctx, c, postgresDB, deleted)
}

func cleanupManagedRoles(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, plan deletionPlan) error {
	logger := log.FromContext(ctx)
	if len(plan.deleted) == 0 {
		return nil
	}
	cluster := &enterprisev4.PostgresCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("getting PostgresCluster for role cleanup: %w", err)
		}
		logger.Info("PostgresCluster already deleted, skipping managed roles cleanup")
		return nil
	}
	fieldManager := fieldManagerName(postgresDB.Name)
	retainedRoles := buildDesiredRoles(postgresDB.Name, plan.retained)
	rolesToRemove := buildRolesToRemove(plan.deleted)
	allRoles := append(retainedRoles, rolesToRemove...)
	if err := patchManagedRoles(ctx, c, fieldManager, cluster, allRoles); err != nil {
		return err
	}
	return nil
}

func orphanCNPGDatabases(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range databases {
		name := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)
		db := &cnpgv1.Database{}
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: postgresDB.Namespace}, db); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting CNPG Database %s for orphaning: %w", name, err)
		}
		if db.Annotations[annotationRetainedFrom] == postgresDB.Name {
			continue
		}
		stripOwnerReference(db, postgresDB.UID)
		if db.Annotations == nil {
			db.Annotations = make(map[string]string)
		}
		db.Annotations[annotationRetainedFrom] = postgresDB.Name
		if err := c.Update(ctx, db); err != nil {
			return fmt.Errorf("orphaning CNPG Database %s: %w", name, err)
		}
		logger.Info("CNPG Database orphaned", "name", name)
	}
	return nil
}

func orphanConfigMaps(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range databases {
		name := configMapName(postgresDB.Name, dbSpec.Name)
		cm := &corev1.ConfigMap{}
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: postgresDB.Namespace}, cm); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting ConfigMap %s for orphaning: %w", name, err)
		}
		if cm.Annotations[annotationRetainedFrom] == postgresDB.Name {
			continue
		}
		stripOwnerReference(cm, postgresDB.UID)
		if cm.Annotations == nil {
			cm.Annotations = make(map[string]string)
		}
		cm.Annotations[annotationRetainedFrom] = postgresDB.Name
		if err := c.Update(ctx, cm); err != nil {
			return fmt.Errorf("orphaning ConfigMap %s: %w", name, err)
		}
		logger.Info("ConfigMap orphaned", "name", name)
	}
	return nil
}

func orphanSecrets(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range databases {
		for _, role := range []string{secretRoleAdmin, secretRoleRW} {
			name := roleSecretName(postgresDB.Name, dbSpec.Name, role)
			secret := &corev1.Secret{}
			if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: postgresDB.Namespace}, secret); err != nil {
				if errors.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("getting Secret %s for orphaning: %w", name, err)
			}
			if secret.Annotations[annotationRetainedFrom] == postgresDB.Name {
				continue
			}
			stripOwnerReference(secret, postgresDB.UID)
			if secret.Annotations == nil {
				secret.Annotations = make(map[string]string)
			}
			secret.Annotations[annotationRetainedFrom] = postgresDB.Name
			if err := c.Update(ctx, secret); err != nil {
				return fmt.Errorf("orphaning Secret %s: %w", name, err)
			}
			logger.Info("Secret orphaned", "name", name)
		}
	}
	return nil
}

func deleteCNPGDatabases(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range databases {
		name := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)
		db := &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgresDB.Namespace}}
		if err := c.Delete(ctx, db); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("deleting CNPG Database %s: %w", name, err)
		}
		logger.Info("CNPG Database deleted", "name", name)
	}
	return nil
}

func deleteConfigMaps(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range databases {
		name := configMapName(postgresDB.Name, dbSpec.Name)
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgresDB.Namespace}}
		if err := c.Delete(ctx, cm); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("deleting ConfigMap %s: %w", name, err)
		}
		logger.Info("ConfigMap deleted", "name", name)
	}
	return nil
}

func deleteSecrets(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range databases {
		for _, role := range []string{secretRoleAdmin, secretRoleRW} {
			name := roleSecretName(postgresDB.Name, dbSpec.Name, role)
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgresDB.Namespace}}
			if err := c.Delete(ctx, secret); err != nil {
				if errors.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("deleting Secret %s: %w", name, err)
			}
			logger.Info("Secret deleted", "name", name)
		}
	}
	return nil
}

// buildRolesToRemove produces Exists:false entries for the given databases so CNPG drops their roles.
func buildRolesToRemove(databases []enterprisev4.DatabaseDefinition) []enterprisev4.ManagedRole {
	roles := make([]enterprisev4.ManagedRole, 0, len(databases)*2)
	for _, dbSpec := range databases {
		roles = append(roles,
			enterprisev4.ManagedRole{Name: adminRoleName(dbSpec.Name), Exists: false},
			enterprisev4.ManagedRole{Name: rwRoleName(dbSpec.Name), Exists: false},
		)
	}
	return roles
}

// absentRolesByName produces Exists:false entries from a list of raw role names.
// Used by the normal reconcile path where names come from SSA field manager parsing.
func absentRolesByName(names []string) []enterprisev4.ManagedRole {
	roles := make([]enterprisev4.ManagedRole, 0, len(names))
	for _, name := range names {
		roles = append(roles, enterprisev4.ManagedRole{Name: name, Exists: false})
	}
	return roles
}

// findAddedRoleNames returns role names from the desired list that are missing
// from the cluster spec or currently marked absent.
func findAddedRoleNames(cluster *enterprisev4.PostgresCluster, desired []enterprisev4.ManagedRole) []string {
	current := make(map[string]bool, len(cluster.Spec.ManagedRoles))
	for _, r := range cluster.Spec.ManagedRoles {
		current[r.Name] = r.Exists
	}
	var toAdd []string
	for _, r := range desired {
		exists, found := current[r.Name]
		if !found || !exists {
			toAdd = append(toAdd, r.Name)
		}
	}
	return toAdd
}

// findRemovedRoleNames returns role names currently owned by this field manager
// in the cluster spec that are absent from the desired list.
func findRemovedRoleNames(cluster *enterprisev4.PostgresCluster, manager string, desired []enterprisev4.ManagedRole) []string {
	desiredSet := make(map[string]struct{}, len(desired))
	for _, r := range desired {
		desiredSet[r.Name] = struct{}{}
	}
	owners := managedRoleOwners(cluster.ManagedFields)
	var toRemove []string
	for name, owner := range owners {
		if owner == manager {
			if _, ok := desiredSet[name]; !ok {
				toRemove = append(toRemove, name)
			}
		}
	}
	return toRemove
}

// buildDesiredRoles builds the full set of roles that should be present for the given databases.
// This is the input to findAddedRoleNames and findRemovedRoleNames.
func buildDesiredRoles(postgresDBName string, databases []enterprisev4.DatabaseDefinition) []enterprisev4.ManagedRole {
	roles := make([]enterprisev4.ManagedRole, 0, len(databases)*2)
	for _, dbSpec := range databases {
		roles = append(roles,
			enterprisev4.ManagedRole{
				Name:   adminRoleName(dbSpec.Name),
				Exists: true,
				PasswordSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: roleSecretName(postgresDBName, dbSpec.Name, secretRoleAdmin)},
					Key: secretKeyPassword},
			},
			enterprisev4.ManagedRole{
				Name:   rwRoleName(dbSpec.Name),
				Exists: true,
				PasswordSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: roleSecretName(postgresDBName, dbSpec.Name, secretRoleRW)},
					Key: secretKeyPassword},
			},
		)
	}
	return roles
}

func buildManagedRolesPatch(cluster *enterprisev4.PostgresCluster, roles []enterprisev4.ManagedRole, scheme *runtime.Scheme) (*unstructured.Unstructured, error) {
	gvk, err := apiutil.GVKForObject(cluster, scheme)
	if err != nil {
		return nil, fmt.Errorf("getting GVK for Cluster: %w", err)
	}
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gvk.GroupVersion().String(),
			"kind":       gvk.Kind,
			"metadata":   map[string]any{"name": cluster.Name, "namespace": cluster.Namespace},
			"spec":       map[string]any{"managedRoles": roles},
		},
	}, nil
}

func stripOwnerReference(obj metav1.Object, ownerUID types.UID) {
	refs := obj.GetOwnerReferences()
	filtered := make([]metav1.OwnerReference, 0, len(refs))
	for _, ref := range refs {
		if ref.UID != ownerUID {
			filtered = append(filtered, ref)
		}
	}
	obj.SetOwnerReferences(filtered)
}

func adoptResource(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, obj client.Object) error {
	annotations := obj.GetAnnotations()
	delete(annotations, annotationRetainedFrom)
	obj.SetAnnotations(annotations)
	if err := controllerutil.SetControllerReference(postgresDB, obj, scheme); err != nil {
		return err
	}
	return c.Update(ctx, obj)
}

func secretMissingPolicyForDB(dbName string, existingDBs map[string]struct{}) secretMissingPolicy {
	if _, exists := existingDBs[dbName]; exists {
		return reportSecretDriftIfMissing
	}
	return createSecretIfMissing
}

func reconcileRoleSecrets(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, existingDatabases map[string]struct{}) error {
	for _, dbSpec := range postgresDB.Spec.Databases {
		missingPolicy := secretMissingPolicyForDB(dbSpec.Name, existingDatabases)
		if err := reconcileRoleSecret(ctx, c, scheme, postgresDB, adminRoleName(dbSpec.Name), roleSecretName(postgresDB.Name, dbSpec.Name, secretRoleAdmin), missingPolicy); err != nil {
			return err
		}
		if err := reconcileRoleSecret(ctx, c, scheme, postgresDB, rwRoleName(dbSpec.Name), roleSecretName(postgresDB.Name, dbSpec.Name, secretRoleRW), missingPolicy); err != nil {
			return err
		}
	}
	return nil
}

func reconcileRoleSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, roleName, secretName string, missingPolicy secretMissingPolicy) error {
	if missingPolicy == reportSecretDriftIfMissing {
		return ensureProvisionedSecret(ctx, c, scheme, postgresDB, roleName, secretName)
	}
	return ensureSecret(ctx, c, scheme, postgresDB, roleName, secretName)
}

func ensureSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, roleName, secretName string) error {
	secret, err := getSecret(ctx, c, postgresDB.Namespace, secretName)
	if err != nil {
		return err
	}
	if secret == nil {
		return createRoleSecret(ctx, c, scheme, postgresDB, roleName, secretName)
	}
	return reconcileExistingSecret(ctx, c, scheme, postgresDB, secretName, secret)
}

func ensureProvisionedSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, roleName, secretName string) error {
	secret, err := getSecret(ctx, c, postgresDB.Namespace, secretName)
	if err != nil {
		return err
	}
	if secret == nil {
		return &secretReconcileError{
			message: fmt.Sprintf("Managed Secret %s is missing for previously provisioned role %s", secretName, roleName),
			reason:  reasonSecretsDriftDetected,
		}
	}
	return reconcileExistingSecret(ctx, c, scheme, postgresDB, secretName, secret)
}

// reconcileExistingSecret only reconciles ownership — it never rewrites secret data.
// Passwords must not be regenerated for existing credentials; CNPG and consumers hold live references.
func reconcileExistingSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, secretName string, secret *corev1.Secret) error {
	logger := log.FromContext(ctx)
	switch {
	case secret.Annotations[annotationRetainedFrom] == postgresDB.Name:
		if err := adoptResource(ctx, c, scheme, postgresDB, secret); err != nil {
			return err
		}
		logger.Info("Secret re-adopted", "name", secretName)
		return nil
	case metav1.IsControlledBy(secret, postgresDB):
		return nil
	case metav1.GetControllerOf(secret) == nil:
		if err := adoptResource(ctx, c, scheme, postgresDB, secret); err != nil {
			return err
		}
		logger.Info("Secret adopted", "name", secretName)
		return nil
	default:
		owner := metav1.GetControllerOf(secret)
		return &secretReconcileError{
			message: fmt.Sprintf("Managed Secret %s is controlled by %s %s", secretName, owner.Kind, owner.Name),
			reason:  reasonSecretsDriftDetected,
		}
	}
}

func getSecret(ctx context.Context, c client.Client, namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret)
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return secret, nil
}

func createRoleSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, roleName, secretName string) error {
	pw, err := generatePassword()
	if err != nil {
		return err
	}
	secret := buildPasswordSecret(postgresDB, secretName, roleName, pw)
	if err := controllerutil.SetControllerReference(postgresDB, secret, scheme); err != nil {
		return fmt.Errorf("setting owner reference on Secret %s: %w", secretName, err)
	}
	if err := c.Create(ctx, secret); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	log.FromContext(ctx).Info("Role secret created", "name", secretName)
	return nil
}

func buildPasswordSecret(postgresDB *enterprisev4.PostgresDatabase, secretName, roleName, pw string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: postgresDB.Namespace,
			Labels:    map[string]string{labelManagedBy: "splunk-operator", labelCNPGReload: "true"},
		},
		Data: map[string][]byte{"username": []byte(roleName), secretKeyPassword: []byte(pw)},
	}
}

func buildCNPGDatabaseSpec(clusterName string, dbSpec enterprisev4.DatabaseDefinition) cnpgv1.DatabaseSpec {
	reclaimPolicy := cnpgv1.DatabaseReclaimDelete
	if dbSpec.DeletionPolicy == deletionPolicyRetain {
		reclaimPolicy = cnpgv1.DatabaseReclaimRetain
	}
	return cnpgv1.DatabaseSpec{
		Name:          dbSpec.Name,
		Owner:         adminRoleName(dbSpec.Name),
		ClusterRef:    corev1.LocalObjectReference{Name: clusterName},
		ReclaimPolicy: reclaimPolicy,
	}
}

func reconcileRoleConfigMaps(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, endpoints clusterEndpoints) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range postgresDB.Spec.Databases {
		cmName := configMapName(postgresDB.Name, dbSpec.Name)
		reAdopted := false
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: postgresDB.Namespace,
				Labels:    map[string]string{labelManagedBy: "splunk-operator"},
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
			cm.Data = buildDatabaseConfigMapBody(dbSpec.Name, endpoints)
			reAdopted = cm.Annotations[annotationRetainedFrom] == postgresDB.Name
			if reAdopted {
				delete(cm.Annotations, annotationRetainedFrom)
			}
			if !metav1.IsControlledBy(cm, postgresDB) {
				return controllerutil.SetControllerReference(postgresDB, cm, scheme)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("reconciling ConfigMap %s: %w", cmName, err)
		}
		if reAdopted {
			logger.Info("ConfigMap re-adopted", "name", cmName)
		}
	}
	return nil
}

func buildDatabaseConfigMapBody(dbName string, endpoints clusterEndpoints) map[string]string {
	data := map[string]string{
		"dbname":     dbName,
		"port":       postgresPort,
		"rw-host":    endpoints.RWHost,
		"ro-host":    endpoints.ROHost,
		"admin-user": adminRoleName(dbName),
		"rw-user":    rwRoleName(dbName),
	}
	if endpoints.PoolerRWHost != "" {
		data["pooler-rw-host"] = endpoints.PoolerRWHost
	}
	if endpoints.PoolerROHost != "" {
		data["pooler-ro-host"] = endpoints.PoolerROHost
	}
	return data
}

func resolveClusterEndpoints(cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, namespace string) clusterEndpoints {
	// FQDN so consumers in other namespaces can resolve without extra config.
	endpoints := clusterEndpoints{
		RWHost: fmt.Sprintf("%s.%s.svc.cluster.local", cnpgCluster.Status.WriteService, namespace),
		ROHost: fmt.Sprintf("%s.%s.svc.cluster.local", cnpgCluster.Status.ReadService, namespace),
	}
	if cluster.Status.ConnectionPoolerStatus != nil && cluster.Status.ConnectionPoolerStatus.Enabled {
		endpoints.PoolerRWHost = fmt.Sprintf("%s-pooler-%s.%s.svc.cluster.local", cnpgCluster.Name, readWriteEndpoint, namespace)
		endpoints.PoolerROHost = fmt.Sprintf("%s-pooler-%s.%s.svc.cluster.local", cnpgCluster.Name, readOnlyEndpoint, namespace)
	}
	return endpoints
}

func populateDatabaseStatus(postgresDB *enterprisev4.PostgresDatabase) []enterprisev4.DatabaseInfo {
	databases := make([]enterprisev4.DatabaseInfo, 0, len(postgresDB.Spec.Databases))
	for _, dbSpec := range postgresDB.Spec.Databases {
		databases = append(databases, enterprisev4.DatabaseInfo{
			Name:               dbSpec.Name,
			Ready:              true,
			AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: roleSecretName(postgresDB.Name, dbSpec.Name, secretRoleAdmin)}, Key: secretKeyPassword},
			RWUserSecretRef:    &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: roleSecretName(postgresDB.Name, dbSpec.Name, secretRoleRW)}, Key: secretKeyPassword},
			ConfigMapRef:       &corev1.LocalObjectReference{Name: configMapName(postgresDB.Name, dbSpec.Name)},
		})
	}
	return databases
}

func hasNewDatabases(postgresDB *enterprisev4.PostgresDatabase) bool {
	existing := make(map[string]bool, len(postgresDB.Status.Databases))
	for _, dbInfo := range postgresDB.Status.Databases {
		existing[dbInfo.Name] = true
	}
	for _, dbSpec := range postgresDB.Spec.Databases {
		if !existing[dbSpec.Name] {
			return true
		}
	}
	return false
}

// Naming helpers — single source of truth shared by creation and status wiring.
func fieldManagerName(postgresDBName string) string { return fieldManagerPrefix + postgresDBName }
func adminRoleName(dbName string) string            { return dbName + "_admin" }
func rwRoleName(dbName string) string               { return dbName + "_rw" }
func cnpgDatabaseName(postgresDBName, dbName string) string {
	return fmt.Sprintf("%s-%s", postgresDBName, dbName)
}
func roleSecretName(postgresDBName, dbName, role string) string {
	return fmt.Sprintf("%s-%s-%s", postgresDBName, dbName, role)
}
func configMapName(postgresDBName, dbName string) string {
	return fmt.Sprintf("%s-%s-config", postgresDBName, dbName)
}

// generatePassword uses crypto/rand (via sethvargo/go-password) — predictable passwords
// are unacceptable for credentials that protect live database access.
func generatePassword() (string, error) {
	return password.Generate(passwordLength, passwordDigits, passwordSymbols, false, true)
}
