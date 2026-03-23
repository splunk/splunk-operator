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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// NewDBRepoFunc constructs a DBRepo adapter for the given host and database.
// Injected by the controller so the core never imports the pgx adapter directly.
type NewDBRepoFunc func(ctx context.Context, host, dbName, password string) (DBRepo, error)

// PostgresDatabaseService is the application service entry point called by the primary adapter (reconciler).
// newDBRepo is injected to keep the core free of pgx imports.
func PostgresDatabaseService(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	postgresDB *enterprisev4.PostgresDatabase,
	newDBRepo NewDBRepoFunc,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PostgresDatabase", "name", postgresDB.Name, "namespace", postgresDB.Namespace)

	updateStatus := func(conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
		return setStatus(ctx, c, postgresDB, conditionType, conditionStatus, reason, message, phase)
	}

	// Finalizer: cleanup on deletion, register on creation.
	if postgresDB.GetDeletionTimestamp() != nil {
		if err := handleDeletion(ctx, c, postgresDB); err != nil {
			logger.Error(err, "Cleanup failed for PostgresDatabase")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if !controllerutil.ContainsFinalizer(postgresDB, postgresDatabaseFinalizerName) {
		controllerutil.AddFinalizer(postgresDB, postgresDatabaseFinalizerName)
		if err := c.Update(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to add finalizer to PostgresDatabase")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// ObservedGeneration equality means all phases completed on the current spec — nothing to do.
	if postgresDB.Status.ObservedGeneration != nil && *postgresDB.Status.ObservedGeneration == postgresDB.Generation {
		logger.Info("Spec unchanged and all phases complete, skipping")
		return ctrl.Result{}, nil
	}

	// Phase: ClusterValidation
	cluster, clusterStatus, err := ensureClusterReady(ctx, c, postgresDB)
	if err != nil {
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterInfoFetchFailed,
			"Can't reach Cluster CR due to transient errors", pendingDBPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	logger.Info("Cluster validation done", "clusterName", postgresDB.Spec.ClusterRef.Name, "status", clusterStatus)

	switch clusterStatus {
	case ClusterNotFound:
		if err := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterNotFound, "Cluster CR not found", pendingDBPhase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: clusterNotFoundRetryDelay}, nil

	case ClusterNotReady, ClusterNoProvisionerRef:
		if err := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterProvisioning, "Cluster is not in ready state yet", pendingDBPhase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil

	case ClusterReady:
		if err := updateStatus(clusterReady, metav1.ConditionTrue, reasonClusterAvailable, "Cluster is operational", provisioningDBPhase); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Phase: RoleConflictCheck — verify no other SSA field manager already owns our roles.
	roleConflicts := getRoleConflicts(postgresDB, cluster)
	if len(roleConflicts) > 0 {
		conflictMsg := fmt.Sprintf("Role conflict: %s. "+
			"If you deleted a previous PostgresDatabase, recreate it with the original name to re-adopt the orphaned resources.",
			strings.Join(roleConflicts, ", "))
		logger.Error(nil, conflictMsg)
		if statusErr := updateStatus(rolesReady, metav1.ConditionFalse, reasonRoleConflict, conflictMsg, failedDBPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, nil
	}

	// We need the CNPG Cluster directly because PostgresCluster status does not yet
	// surface managed role reconciliation state.
	cnpgCluster := &cnpgv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.ProvisionerRef.Name,
		Namespace: cluster.Status.ProvisionerRef.Namespace,
	}, cnpgCluster); err != nil {
		logger.Error(err, "Failed to fetch CNPG Cluster")
		return ctrl.Result{}, err
	}

	// Phase: CredentialProvisioning — secrets must exist before roles are patched.
	// CNPG rejects a PasswordSecretRef pointing at a missing secret.
	if err := reconcileUserSecrets(ctx, c, scheme, postgresDB); err != nil {
		if statusErr := updateStatus(secretsReady, metav1.ConditionFalse, reasonSecretsCreationFailed,
			fmt.Sprintf("Failed to reconcile user secrets: %v", err), provisioningDBPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	if err := updateStatus(secretsReady, metav1.ConditionTrue, reasonSecretsCreated,
		fmt.Sprintf("All secrets provisioned for %d databases", len(postgresDB.Spec.Databases)), provisioningDBPhase); err != nil {
		return ctrl.Result{}, err
	}

	// Phase: ConnectionMetadata — ConfigMaps carry connection info consumers need as soon
	// as databases are ready, so they are created alongside secrets.
	endpoints := resolveClusterEndpoints(cluster, cnpgCluster, postgresDB.Namespace)
	if err := reconcileRoleConfigMaps(ctx, c, scheme, postgresDB, endpoints); err != nil {
		if statusErr := updateStatus(configMapsReady, metav1.ConditionFalse, reasonConfigMapsCreationFailed,
			fmt.Sprintf("Failed to reconcile ConfigMaps: %v", err), provisioningDBPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	if err := updateStatus(configMapsReady, metav1.ConditionTrue, reasonConfigMapsCreated,
		fmt.Sprintf("All ConfigMaps provisioned for %d databases", len(postgresDB.Spec.Databases)), provisioningDBPhase); err != nil {
		return ctrl.Result{}, err
	}

	// Phase: RoleProvisioning
	desiredUsers := getDesiredUsers(postgresDB)
	actualRoles := getUsersInClusterSpec(cluster)
	var missing []string
	for _, role := range desiredUsers {
		if !slices.Contains(actualRoles, role) {
			missing = append(missing, role)
		}
	}

	if len(missing) > 0 {
		logger.Info("User spec changed, patching CNPG Cluster", "missing", missing)
		if err := patchManagedRoles(ctx, c, postgresDB, cluster); err != nil {
			logger.Error(err, "Failed to patch users in CNPG Cluster")
			return ctrl.Result{}, err
		}
		if err := updateStatus(rolesReady, metav1.ConditionFalse, reasonWaitingForCNPG,
			fmt.Sprintf("Waiting for %d roles to be reconciled", len(desiredUsers)), provisioningDBPhase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	notReadyRoles, err := verifyRolesReady(ctx, desiredUsers, cnpgCluster)
	if err != nil {
		if statusErr := updateStatus(rolesReady, metav1.ConditionFalse, reasonUsersCreationFailed,
			fmt.Sprintf("Role creation failed: %v", err), failedDBPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}
	if len(notReadyRoles) > 0 {
		if err := updateStatus(rolesReady, metav1.ConditionFalse, reasonWaitingForCNPG,
			fmt.Sprintf("Waiting for roles to be reconciled: %v", notReadyRoles), provisioningDBPhase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	if err := updateStatus(rolesReady, metav1.ConditionTrue, reasonUsersAvailable,
		fmt.Sprintf("All %d users in PostgreSQL", len(desiredUsers)), provisioningDBPhase); err != nil {
		return ctrl.Result{}, err
	}

	// Phase: DatabaseProvisioning
	if err := reconcileCNPGDatabases(ctx, c, scheme, postgresDB, cluster); err != nil {
		logger.Error(err, "Failed to reconcile CNPG Databases")
		return ctrl.Result{}, err
	}

	notReadyDBs, err := verifyDatabasesReady(ctx, c, postgresDB)
	if err != nil {
		logger.Error(err, "Failed to verify database status")
		return ctrl.Result{}, err
	}
	if len(notReadyDBs) > 0 {
		if err := updateStatus(databasesReady, metav1.ConditionFalse, reasonWaitingForCNPG,
			fmt.Sprintf("Waiting for databases to be ready: %v", notReadyDBs), provisioningDBPhase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	if err := updateStatus(databasesReady, metav1.ConditionTrue, reasonDatabasesAvailable,
		fmt.Sprintf("All %d databases ready", len(postgresDB.Spec.Databases)), readyDBPhase); err != nil {
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
			return ctrl.Result{}, fmt.Errorf("PostgresCluster %s has no superuser secret ref in status", cluster.Name)
		}
		superSecretRef := cluster.Status.Resources.SuperUserSecretRef
		superSecret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{
			Name:      superSecretRef.Name,
			Namespace: postgresDB.Namespace,
		}, superSecret); err != nil {
			return ctrl.Result{}, fmt.Errorf("fetching superuser secret %s: %w", superSecretRef.Name, err)
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
			if statusErr := updateStatus(privilegesReady, metav1.ConditionFalse, reasonPrivilegesGrantFailed,
				fmt.Sprintf("Failed to grant RW role privileges: %v", err), provisioningDBPhase); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		if err := updateStatus(privilegesReady, metav1.ConditionTrue, reasonPrivilegesGranted,
			fmt.Sprintf("RW role privileges granted for all %d databases", len(postgresDB.Spec.Databases)), readyDBPhase); err != nil {
			return ctrl.Result{}, err
		}
	}

	postgresDB.Status.Databases = populateDatabaseStatus(postgresDB)
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation

	logger.Info("All phases complete")
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
			logger.Error(err, "Failed to connect to database", "database", dbName)
			errs = append(errs, fmt.Errorf("database %s: %w", dbName, err))
			continue
		}
		if err := repo.ExecGrants(ctx, dbName); err != nil {
			logger.Error(err, "Failed to grant RW role privileges", "database", dbName)
			errs = append(errs, fmt.Errorf("database %s: %w", dbName, err))
			continue
		}
		logger.Info("RW role privileges granted", "database", dbName, "rwRole", rwRoleName(dbName))
	}
	return stderrors.Join(errs...)
}

func ensureClusterReady(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase) (*enterprisev4.PostgresCluster, clusterReadyStatus, error) {
	logger := log.FromContext(ctx)
	cluster := &enterprisev4.PostgresCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		if errors.IsNotFound(err) {
			return nil, ClusterNotFound, nil
		}
		logger.Error(err, "Failed to fetch Cluster", "name", postgresDB.Spec.ClusterRef.Name)
		return nil, ClusterNotReady, err
	}
	if cluster.Status.Phase == nil || *cluster.Status.Phase != string(ClusterReady) {
		return cluster, ClusterNotReady, nil
	}
	if cluster.Status.ProvisionerRef == nil {
		return cluster, ClusterNoProvisionerRef, nil
	}
	return cluster, ClusterReady, nil
}

func getDesiredUsers(postgresDB *enterprisev4.PostgresDatabase) []string {
	users := make([]string, 0, len(postgresDB.Spec.Databases)*2)
	for _, dbSpec := range postgresDB.Spec.Databases {
		users = append(users, adminRoleName(dbSpec.Name), rwRoleName(dbSpec.Name))
	}
	return users
}

func getUsersInClusterSpec(cluster *enterprisev4.PostgresCluster) []string {
	users := make([]string, 0, len(cluster.Spec.ManagedRoles))
	for _, role := range cluster.Spec.ManagedRoles {
		users = append(users, role.Name)
	}
	return users
}

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

func patchManagedRoles(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, cluster *enterprisev4.PostgresCluster) error {
	logger := log.FromContext(ctx)
	allRoles := make([]enterprisev4.ManagedRole, 0, len(postgresDB.Spec.Databases)*2)
	for _, dbSpec := range postgresDB.Spec.Databases {
		allRoles = append(allRoles,
			enterprisev4.ManagedRole{
				Name:   adminRoleName(dbSpec.Name),
				Exists: true,
				PasswordSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: roleSecretName(postgresDB.Name, dbSpec.Name, secretRoleAdmin)},
					Key: secretKeyPassword},
			},
			enterprisev4.ManagedRole{
				Name:   rwRoleName(dbSpec.Name),
				Exists: true,
				PasswordSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: roleSecretName(postgresDB.Name, dbSpec.Name, secretRoleRW)},
					Key: secretKeyPassword},
			})
	}
	rolePatch := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": cluster.APIVersion,
			"kind":       cluster.Kind,
			"metadata":   map[string]any{"name": cluster.Name, "namespace": cluster.Namespace},
			"spec":       map[string]any{"managedRoles": allRoles},
		},
	}
	fieldManager := fieldManagerName(postgresDB.Name)
	if err := c.Patch(ctx, rolePatch, client.Apply, client.FieldOwner(fieldManager)); err != nil {
		logger.Error(err, "Failed to add users to PostgresCluster", "postgresDatabase", postgresDB.Name)
		return fmt.Errorf("patching managed roles for PostgresDatabase %s: %w", postgresDB.Name, err)
	}
	logger.Info("Users added to PostgresCluster via SSA", "postgresDatabase", postgresDB.Name, "roleCount", len(allRoles))
	return nil
}

func verifyRolesReady(ctx context.Context, expectedUsers []string, cnpgCluster *cnpgv1.Cluster) ([]string, error) {
	logger := log.FromContext(ctx)
	if cnpgCluster.Status.ManagedRolesStatus.CannotReconcile != nil {
		for _, userName := range expectedUsers {
			if errs, exists := cnpgCluster.Status.ManagedRolesStatus.CannotReconcile[userName]; exists {
				return nil, fmt.Errorf("user %s reconciliation failed: %v", userName, errs)
			}
		}
	}
	reconciled := cnpgCluster.Status.ManagedRolesStatus.ByStatus[cnpgv1.RoleStatusReconciled]
	var notReady []string
	for _, userName := range expectedUsers {
		if !slices.Contains(reconciled, userName) {
			notReady = append(notReady, userName)
		}
	}
	if len(notReady) > 0 {
		logger.Info("Users not reconciled yet", "pending", notReady)
	}
	return notReady, nil
}

func reconcileCNPGDatabases(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, cluster *enterprisev4.PostgresCluster) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range postgresDB.Spec.Databases {
		cnpgDBName := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)
		reclaimPolicy := cnpgv1.DatabaseReclaimDelete
		if dbSpec.DeletionPolicy == deletionPolicyRetain {
			reclaimPolicy = cnpgv1.DatabaseReclaimRetain
		}
		cnpgDB := &cnpgv1.Database{
			ObjectMeta: metav1.ObjectMeta{Name: cnpgDBName, Namespace: postgresDB.Namespace},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, c, cnpgDB, func() error {
			cnpgDB.Spec = cnpgv1.DatabaseSpec{
				Name:          dbSpec.Name,
				Owner:         adminRoleName(dbSpec.Name),
				ClusterRef:    corev1.LocalObjectReference{Name: cluster.Status.ProvisionerRef.Name},
				ReclaimPolicy: reclaimPolicy,
			}
			reAdopting := cnpgDB.Annotations[annotationRetainedFrom] == postgresDB.Name
			if reAdopting {
				logger.Info("Re-adopting orphaned CNPG Database", "name", cnpgDBName)
				delete(cnpgDB.Annotations, annotationRetainedFrom)
			}
			if cnpgDB.CreationTimestamp.IsZero() || reAdopting {
				return controllerutil.SetControllerReference(postgresDB, cnpgDB, scheme)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("reconciling CNPG Database %s: %w", cnpgDBName, err)
		}
	}
	return nil
}

func verifyDatabasesReady(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase) ([]string, error) {
	var notReady []string
	for _, dbSpec := range postgresDB.Spec.Databases {
		cnpgDBName := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)
		cnpgDB := &cnpgv1.Database{}
		if err := c.Get(ctx, types.NamespacedName{Name: cnpgDBName, Namespace: postgresDB.Namespace}, cnpgDB); err != nil {
			return nil, fmt.Errorf("getting CNPG Database %s: %w", cnpgDBName, err)
		}
		if cnpgDB.Status.Applied == nil || !*cnpgDB.Status.Applied {
			notReady = append(notReady, dbSpec.Name)
		}
	}
	return notReady, nil
}

func setStatus(ctx context.Context, c client.Client, db *enterprisev4.PostgresDatabase, conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
	meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
		Type:               string(conditionType),
		Status:             conditionStatus,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: db.Generation,
	})
	p := string(phase)
	db.Status.Phase = &p
	return c.Status().Update(ctx, db)
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

func handleDeletion(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase) error {
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
	log.FromContext(ctx).Info("Cleanup complete", "name", postgresDB.Name, "retained", len(plan.retained), "deleted", len(plan.deleted))
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
	if len(plan.deleted) == 0 {
		return nil
	}
	cluster := &enterprisev4.PostgresCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("getting PostgresCluster for role cleanup: %w", err)
		}
		log.FromContext(ctx).Info("PostgresCluster already deleted, skipping role cleanup")
		return nil
	}
	return patchManagedRolesOnDeletion(ctx, c, postgresDB, cluster, plan.retained)
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
		logger.Info("Orphaned CNPG Database CR", "name", name)
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
		logger.Info("Orphaned ConfigMap", "name", name)
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
			logger.Info("Orphaned Secret", "name", name)
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
		logger.Info("Deleted CNPG Database CR", "name", name)
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
		logger.Info("Deleted ConfigMap", "name", name)
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
			logger.Info("Deleted Secret", "name", name)
		}
	}
	return nil
}

func buildRetainedRoles(postgresDBName string, retained []enterprisev4.DatabaseDefinition) []enterprisev4.ManagedRole {
	roles := make([]enterprisev4.ManagedRole, 0, len(retained)*2)
	for _, dbSpec := range retained {
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

func patchManagedRolesOnDeletion(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, cluster *enterprisev4.PostgresCluster, retained []enterprisev4.DatabaseDefinition) error {
	roles := buildRetainedRoles(postgresDB.Name, retained)
	rolePatch := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": cluster.APIVersion,
			"kind":       cluster.Kind,
			"metadata":   map[string]any{"name": cluster.Name, "namespace": cluster.Namespace},
			"spec":       map[string]any{"managedRoles": roles},
		},
	}
	if err := c.Patch(ctx, rolePatch, client.Apply, client.FieldOwner(fieldManagerName(postgresDB.Name))); err != nil {
		return fmt.Errorf("patching managed roles on deletion: %w", err)
	}
	log.FromContext(ctx).Info("Patched managed roles on deletion", "postgresDatabase", postgresDB.Name, "retainedRoles", len(roles))
	return nil
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

func reconcileUserSecrets(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase) error {
	for _, dbSpec := range postgresDB.Spec.Databases {
		if err := ensureSecret(ctx, c, scheme, postgresDB, adminRoleName(dbSpec.Name), roleSecretName(postgresDB.Name, dbSpec.Name, secretRoleAdmin)); err != nil {
			return err
		}
		if err := ensureSecret(ctx, c, scheme, postgresDB, rwRoleName(dbSpec.Name), roleSecretName(postgresDB.Name, dbSpec.Name, secretRoleRW)); err != nil {
			return err
		}
	}
	return nil
}

func ensureSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, roleName, secretName string) error {
	secret, err := getSecret(ctx, c, postgresDB.Namespace, secretName)
	if err != nil {
		return err
	}
	logger := log.FromContext(ctx)
	switch {
	case secret == nil:
		logger.Info("Creating missing user secret", "name", secretName)
		return createUserSecret(ctx, c, scheme, postgresDB, roleName, secretName)
	case secret.Annotations[annotationRetainedFrom] == postgresDB.Name:
		logger.Info("Re-adopting orphaned secret", "name", secretName)
		return adoptResource(ctx, c, scheme, postgresDB, secret)
	}
	return nil
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

func createUserSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, roleName, secretName string) error {
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

func reconcileRoleConfigMaps(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, endpoints clusterEndpoints) error {
	logger := log.FromContext(ctx)
	for _, dbSpec := range postgresDB.Spec.Databases {
		cmName := configMapName(postgresDB.Name, dbSpec.Name)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: postgresDB.Namespace,
				Labels:    map[string]string{labelManagedBy: "splunk-operator"},
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, c, cm, func() error {
			cm.Data = buildDatabaseConfigMapBody(dbSpec.Name, endpoints)
			reAdopting := cm.Annotations[annotationRetainedFrom] == postgresDB.Name
			if reAdopting {
				logger.Info("Re-adopting orphaned ConfigMap", "name", cmName)
				delete(cm.Annotations, annotationRetainedFrom)
			}
			if cm.CreationTimestamp.IsZero() || reAdopting {
				return controllerutil.SetControllerReference(postgresDB, cm, scheme)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("reconciling ConfigMap %s: %w", cmName, err)
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
