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
	"reflect"
	"slices"

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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	secretRoleAdmin = "admin"
	secretRoleRW    = "rw"

	// Password generation — no symbols for PostgreSQL connection string compatibility.
	passwordLength  = 32
	passwordDigits  = 8
	passwordSymbols = 0

	// Label keys used on managed secrets.
	labelManagedBy  = "app.kubernetes.io/managed-by"
	labelCNPGReload = "cnpg.io/reload"

	// postgresPort is the standard PostgreSQL port used in all connection strings.
	postgresPort = "5432"
)

// PostgresDatabaseReconciler reconciles a PostgresDatabase object
type PostgresDatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresdatabases/finalizers,verbs=update
//+kubebuilder:rbac:groups=enterprise.splunk.com,resources=postgresclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;patch
//+kubebuilder:rbac:groups=postgresql.cnpg.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update

func (r *PostgresDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PostgresDatabase", "name", req.Name, "namespace", req.Namespace)

	postgresDB := &enterprisev4.PostgresDatabase{}
	if err := r.Get(ctx, req.NamespacedName, postgresDB); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("PostgresDatabase resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get PostgresDatabase", "name", req.Name)
		return ctrl.Result{}, err
	}
	logger.Info("PostgresDatabase CR Fetched successfully", "generation", postgresDB.Generation)

	// Closure captures postgresDB so call sites don't repeat it on every status update.
	updateStatus := func(conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
		return r.updateStatus(ctx, postgresDB, conditionType, conditionStatus, reason, message, phase)
	}

	// Handle finalizer: cleanup on deletion, register on creation
	if postgresDB.GetDeletionTimestamp() != nil {
		if err := r.handleDeletion(ctx, postgresDB); err != nil {
			logger.Error(err, "Cleanup failed for PostgresDatabase")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}
	if !controllerutil.ContainsFinalizer(postgresDB, postgresDatabaseFinalizerName) {
		controllerutil.AddFinalizer(postgresDB, postgresDatabaseFinalizerName)
		if err := r.Update(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to add finalizer to PostgresDatabase")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// ObservedGeneration is only written when all phases complete successfully,
	// so equality means nothing changed and there is no pending work.
	if postgresDB.Status.ObservedGeneration == postgresDB.Generation {
		logger.Info("Spec unchanged and all phases complete, skipping")
		return ctrl.Result{}, nil
	}
	logger.Info("Changes to resource detected, reconciling...")

	// Phase: ClusterValidation
	var cluster *enterprisev4.PostgresCluster
	var clusterStatus clusterReadyStatus
	var err error

	cluster, clusterStatus, err = r.ensureClusterReady(ctx, postgresDB)
	if err != nil {
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterInfoFetchFailed, "Can't reach Cluster CR due to transient errors", pendingDBPhase); statusErr != nil {
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

	// We need the CNPG Cluster directly because PostgresCluster status does not yet
	// surface managed role reconciliation state — tracked as a future abstraction improvement.
	cnpgCluster := &cnpgv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.ProvisionerRef.Name,
		Namespace: cluster.Status.ProvisionerRef.Namespace,
	}, cnpgCluster); err != nil {
		logger.Error(err, "Failed to fetch CNPG Cluster")
		return ctrl.Result{}, err
	}

	// Phase: CredentialProvisioning — secrets must exist before roles are patched,
	// CNPG rejects a PasswordSecretRef pointing at a missing secret.
	if err := r.reconcileUserSecrets(ctx, postgresDB); err != nil {
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

	// Phase: ConnectionMetadata — ConfigMaps carry connection info consumers need as soon as
	// databases are ready, so they are created alongside secrets before any role or database work begins.
	endpoints := resolveClusterEndpoints(cluster, cnpgCluster, postgresDB.Namespace)
	if err := r.reconcileUserConfigMaps(ctx, postgresDB, endpoints); err != nil {
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
	actualUsersInSpec := getUsersInClusterSpec(cluster)
	var missingUsersFromSpec []string
	for _, user := range desiredUsers {
		if !slices.Contains(actualUsersInSpec, user) {
			missingUsersFromSpec = append(missingUsersFromSpec, user)
		}
	}

	if len(missingUsersFromSpec) > 0 {
		logger.Info("User spec changed, patching CNPG Cluster", "missing", missingUsersFromSpec)
		if err := r.patchManagedRoles(ctx, postgresDB, cluster); err != nil {
			logger.Error(err, "Failed to patch users in CNPG Cluster")
			return ctrl.Result{}, err
		}
		// Spec updated, requeue to check status
		if err := updateStatus(usersReady, metav1.ConditionFalse, reasonWaitingForCNPG, fmt.Sprintf("Waiting for %d users to be reconciled", len(desiredUsers)), provisioningDBPhase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	notReadyUsers, err := r.verifyRolesReady(ctx, desiredUsers, cnpgCluster)
	if err != nil {
		if statusErr := updateStatus(usersReady, metav1.ConditionFalse, reasonUsersCreationFailed, fmt.Sprintf("User creation failed: %v", err), failedDBPhase); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	if len(notReadyUsers) > 0 {
		if err := updateStatus(usersReady, metav1.ConditionFalse, reasonWaitingForCNPG, fmt.Sprintf("Waiting for users to be reconciled: %v", notReadyUsers), provisioningDBPhase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	// All users present in spec and reconciled in status
	if err := updateStatus(usersReady, metav1.ConditionTrue, reasonUsersAvailable, fmt.Sprintf("All %d users in PostgreSQL", len(desiredUsers)), provisioningDBPhase); err != nil {
		return ctrl.Result{}, err
	}

	// Phase: DatabaseProvisioning
	if err := r.reconcileCNPGDatabases(ctx, postgresDB, cluster); err != nil {
		logger.Error(err, "Failed to reconcile CNPG Databases")
		return ctrl.Result{}, err
	}

	notReadyDatabases, err := r.verifyDatabasesReady(ctx, postgresDB)
	if err != nil {
		logger.Error(err, "Failed to verify database status")
		return ctrl.Result{}, err
	}

	if len(notReadyDatabases) > 0 {
		if err := updateStatus(databasesReady, metav1.ConditionFalse, reasonWaitingForCNPG,
			fmt.Sprintf("Waiting for databases to be ready: %v", notReadyDatabases), provisioningDBPhase); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	databasesInfo := populateDatabaseStatus(postgresDB)
	postgresDB.Status.Databases = databasesInfo
	postgresDB.Status.ObservedGeneration = postgresDB.Generation
	if err := updateStatus(databasesReady, metav1.ConditionTrue, reasonDatabasesAvailable, fmt.Sprintf("All %d databases ready", len(postgresDB.Spec.Databases)), readyDBPhase); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("All phases complete")
	return ctrl.Result{}, nil
}

// ensureClusterReady checks if the referenced PostgresCluster exists and is ready
// Returns: cluster (if found), status, error (API errors only)
func (r *PostgresDatabaseReconciler) ensureClusterReady(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
) (*enterprisev4.PostgresCluster, clusterReadyStatus, error) {
	logger := log.FromContext(ctx)

	cluster := &enterprisev4.PostgresCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		if errors.IsNotFound(err) {
			return nil, ClusterNotFound, nil
		}
		logger.Error(err, "Failed to fetch Cluster", "name", postgresDB.Spec.ClusterRef.Name)
		return nil, ClusterNotReady, err
	}

	if cluster.Status.Phase != string(ClusterReady) {
		logger.Info("Cluster not ready", "status", cluster.Status.Phase)
		return cluster, ClusterNotReady, nil
	}

	if cluster.Status.ProvisionerRef == nil {
		logger.Info("Cluster has no ProvisionerRef yet", "cluster", cluster.Name)
		return cluster, ClusterNoProvisionerRef, nil
	}

	return cluster, ClusterReady, nil
}

// getDesiredUsers builds the list of users we want to create for this PostgresDatabase
func getDesiredUsers(postgresDB *enterprisev4.PostgresDatabase) []string {
	users := make([]string, 0, len(postgresDB.Spec.Databases)*2)
	for _, dbSpec := range postgresDB.Spec.Databases {
		users = append(users, adminRoleName(dbSpec.Name), rwRoleName(dbSpec.Name))
	}
	return users
}

// getUsersInClusterSpec checks our PostgresCluster CR rather than the CNPG Cluster
// because the database controller owns PostgresCluster.spec.managedRoles via SSA —
// CNPG may have roles from other sources that we must not treat as our own.
// Name-only comparison is sufficient: PasswordSecretRef is always set in the same
// reconcile that creates the role, so a role present by name already carries the correct ref.
func getUsersInClusterSpec(cluster *enterprisev4.PostgresCluster) []string {
	users := make([]string, 0, len(cluster.Spec.ManagedRoles))
	for _, role := range cluster.Spec.ManagedRoles {
		users = append(users, role.Name)
	}
	return users
}

// patchManagedRoles patches PostgresCluster.spec.managedRoles via SSA using an unstructured patch.
// Using unstructured avoids the zero-value problem: typed Go structs serialize required fields
// (e.g. spec.class) as "" even when unset, causing SSA to claim ownership and conflict.
// An unstructured map contains ONLY the keys we explicitly set — nothing else leaks.
// PostgresCluster controller will then diff and reconcile these roles to CNPG Cluster.
func (r *PostgresDatabaseReconciler) patchManagedRoles(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
	cluster *enterprisev4.PostgresCluster,
) error {
	logger := log.FromContext(ctx)

	// Build roles — name, ensure, and PasswordSecretRef pointing to the pre-created secrets.
	// Secrets are guaranteed to exist at this point because Phase 2a (reconcileUserSecrets)
	// runs before patchManagedRoles in the reconciliation loop.
	allRoles := make([]enterprisev4.ManagedRole, 0, len(postgresDB.Spec.Databases)*2)
	for _, dbSpec := range postgresDB.Spec.Databases {
		allRoles = append(allRoles,
			enterprisev4.ManagedRole{
				Name:              adminRoleName(dbSpec.Name),
				Ensure:            "present",
				PasswordSecretRef: &corev1.LocalObjectReference{Name: userSecretName(postgresDB.Name, dbSpec.Name, secretRoleAdmin)},
			},
			enterprisev4.ManagedRole{
				Name:              rwRoleName(dbSpec.Name),
				Ensure:            "present",
				PasswordSecretRef: &corev1.LocalObjectReference{Name: userSecretName(postgresDB.Name, dbSpec.Name, secretRoleRW)},
			})
	}

	// Construct a minimal unstructured patch — only spec.managedRoles is present.
	// No other spec fields (class, storage, instances...) are included, so SSA
	// will only claim ownership of the roles we explicitly list.
	rolePatch := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": cluster.APIVersion,
			"kind":       cluster.Kind,
			"metadata": map[string]any{
				"name":      cluster.Name,
				"namespace": cluster.Namespace,
			},
			"spec": map[string]any{
				"managedRoles": allRoles,
			},
		},
	}

	fieldManager := fmt.Sprintf("postgresdatabase-%s", postgresDB.Name)
	if err := r.Patch(ctx, rolePatch, client.Apply, client.FieldOwner(fieldManager)); err != nil {
		logger.Error(err, "Failed to add users to PostgresCluster", "postgresDatabase", postgresDB.Name)
		return fmt.Errorf("failed to patch managed roles for PostgresDatabase %s: %w", postgresDB.Name, err)
	}
	logger.Info("Users added to PostgresCluster via SSA", "postgresDatabase", postgresDB.Name, "postgresCluster", cluster.Name, "roleCount", len(allRoles))

	return nil
}

// verifyRolesReady checks if CNPG has finished creating the users.
func (r *PostgresDatabaseReconciler) verifyRolesReady(
	ctx context.Context,
	expectedUsers []string,
	cnpgCluster *cnpgv1.Cluster,
) ([]string, error) {
	logger := log.FromContext(ctx)

	if cnpgCluster.Status.ManagedRolesStatus.CannotReconcile != nil {
		for _, userName := range expectedUsers {
			if errs, exists := cnpgCluster.Status.ManagedRolesStatus.CannotReconcile[userName]; exists {
				logger.Error(nil, "User reconciliation failed permanently", "user", userName, "errors", errs)
				return nil, fmt.Errorf("user %s reconciliation failed: %v", userName, errs)
			}
		}
	}

	reconciledUsers := cnpgCluster.Status.ManagedRolesStatus.ByStatus[cnpgv1.RoleStatusReconciled]
	var notReady []string
	for _, userName := range expectedUsers {
		if !slices.Contains(reconciledUsers, userName) {
			notReady = append(notReady, userName)
		}
	}

	if len(notReady) > 0 {
		logger.Info("Users not reconciled yet", "pending", notReady)
	} else {
		logger.Info("All users reconciled")
	}
	return notReady, nil
}

// reconcileCNPGDatabases creates or updates CNPG Database CRs for each database in the spec.
func (r *PostgresDatabaseReconciler) reconcileCNPGDatabases(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
	cluster *enterprisev4.PostgresCluster,
) error {
	logger := log.FromContext(ctx)

	for _, dbSpec := range postgresDB.Spec.Databases {
		logger.Info("Processing database", "database", dbSpec.Name)

		cnpgDBName := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)

		// reclaimPolicy controls whether CNPG physically drops the PostgreSQL database
		// when the CR is deleted — a destructive and irreversible operation.
		reclaimPolicy := cnpgv1.DatabaseReclaimDelete
		if dbSpec.DeletionPolicy == deletionPolicyRetain {
			reclaimPolicy = cnpgv1.DatabaseReclaimRetain
		}

		cnpgDB := &cnpgv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cnpgDBName,
				Namespace: postgresDB.Namespace,
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cnpgDB, func() error {

			spec := cnpgv1.DatabaseSpec{
				Name:  dbSpec.Name,
				Owner: adminRoleName(dbSpec.Name),
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Status.ProvisionerRef.Name,
				},
				ReclaimPolicy: reclaimPolicy,
			}
			cnpgDB.Spec = spec

			if cnpgDB.CreationTimestamp.IsZero() {
				if err := controllerutil.SetControllerReference(postgresDB, cnpgDB, r.Scheme); err != nil {
					logger.Error(err, "Failed to set owner reference")
					return err
				}
			}
			return nil
		})
		if err != nil {
			logger.Error(err, "Failed to create CNPG Database", "name", cnpgDBName)
			return fmt.Errorf("failed to create CNPG Database %s: %w", cnpgDBName, err)
		}
		logger.Info("CNPG Database created/updated successfully", "database", dbSpec.Name)
	}
	return nil
}

// verifyDatabasesReady checks if CNPG has finished provisioning the databases.
// All databases are checked before returning so the caller gets a complete picture,
// consistent with verifyRolesReady.
func (r *PostgresDatabaseReconciler) verifyDatabasesReady(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
) ([]string, error) {
	logger := log.FromContext(ctx)

	var notReady []string
	for _, dbSpec := range postgresDB.Spec.Databases {
		cnpgDBName := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)

		cnpgDB := &cnpgv1.Database{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      cnpgDBName,
			Namespace: postgresDB.Namespace,
		}, cnpgDB); err != nil {
			logger.Error(err, "Failed to get CNPG Database status", "database", dbSpec.Name)
			return nil, fmt.Errorf("failed to get CNPG Database %s: %w", cnpgDBName, err)
		}

		if cnpgDB.Status.Applied == nil || !*cnpgDB.Status.Applied {
			notReady = append(notReady, dbSpec.Name)
		}
	}
	return notReady, nil
}

// updateStatus sets a condition and phase on the PostgresDatabase status in a single write —
// callers should not call r.Status().Update() directly.
func (r *PostgresDatabaseReconciler) updateStatus(
	ctx context.Context,
	db *enterprisev4.PostgresDatabase,
	conditionType conditionTypes,
	conditionStatus metav1.ConditionStatus,
	reason conditionReasons,
	message string,
	phase reconcileDBPhases,
) error {
	meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
		Type:               string(conditionType),
		Status:             conditionStatus,
		Reason:             string(reason),
		Message:            message,
		ObservedGeneration: db.Generation,
	})
	db.Status.Phase = string(phase)
	return r.Status().Update(ctx, db)
}

// handleDeletion runs cleanup when a PostgresDatabase is being deleted:
// deletes all CNPG Database CRs, releases managed role ownership, and removes the finalizer.
// The actual PostgreSQL databases and roles survive based on CNPG's databaseReclaimPolicy
// (set during creation from DeletionPolicy: "retain" keeps the PG database, "delete" drops it).
func (r *PostgresDatabaseReconciler) handleDeletion(ctx context.Context, postgresDB *enterprisev4.PostgresDatabase) error {
	logger := log.FromContext(ctx)

	if err := r.removeDatabases(ctx, postgresDB); err != nil {
		return err
	}

	if err := r.removeUsersFromCluster(ctx, postgresDB); err != nil {
		return err
	}

	controllerutil.RemoveFinalizer(postgresDB, postgresDatabaseFinalizerName)
	if err := r.Update(ctx, postgresDB); err != nil {
		return fmt.Errorf("failed to remove finalizer: %w", err)
	}

	logger.Info("Cleanup complete for PostgresDatabase", "name", postgresDB.Name)
	return nil
}

// removeUsersFromCluster releases ownership of all managed roles by patching with an empty list.
// The actual PostgreSQL roles are not dropped — they become unmanaged by CNPG.
func (r *PostgresDatabaseReconciler) removeUsersFromCluster(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
) error {
	logger := log.FromContext(ctx)

	cluster := &enterprisev4.PostgresCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("PostgresCluster already deleted, skipping user cleanup")
			return nil
		}
		return fmt.Errorf("failed to get PostgresCluster for user cleanup: %w", err)
	}

	rolePatch := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": cluster.APIVersion,
			"kind":       cluster.Kind,
			"metadata": map[string]any{
				"name":      cluster.Name,
				"namespace": cluster.Namespace,
			},
			"spec": map[string]any{
				"managedRoles": []any{},
			},
		},
	}

	fieldManager := fmt.Sprintf("postgresdatabase-%s", postgresDB.Name)
	if err := r.Patch(ctx, rolePatch, client.Apply, client.FieldOwner(fieldManager)); err != nil {
		return fmt.Errorf("failed to release user ownership from PostgresCluster: %w", err)
	}

	logger.Info("Released managed role ownership from PostgresCluster", "postgresDatabase", postgresDB.Name, "postgresCluster", cluster.Name)
	return nil
}

// removeDatabases deletes all CNPG Database CRs owned by this PostgresDatabase.
// The actual PostgreSQL databases are retained or deleted based on the databaseReclaimPolicy
// set on each CNPG Database CR during creation.
func (r *PostgresDatabaseReconciler) removeDatabases(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
) error {
	logger := log.FromContext(ctx)

	cnpgDBList := &cnpgv1.DatabaseList{}
	if err := r.List(ctx, cnpgDBList,
		client.InNamespace(postgresDB.Namespace),
		client.MatchingFields{".metadata.controller": postgresDB.Name},
	); err != nil {
		return fmt.Errorf("failed to list CNPG Databases for cleanup: %w", err)
	}

	for _, db := range cnpgDBList.Items {
		logger.Info("Deleting CNPG Database CR", "name", db.Name, "reclaimPolicy", db.Spec.ReclaimPolicy)
		if err := r.Delete(ctx, &db); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete CNPG Database %s: %w", db.Name, err)
		}
	}

	logger.Info("All CNPG Database CRs deleted", "count", len(cnpgDBList.Items))
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Index CNPG Databases by controller owner so getDatabasesInCNPGSpec can filter by owner name without a full list scan.
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&cnpgv1.Database{},
		".metadata.controller",
		func(obj client.Object) []string {
			owner := metav1.GetControllerOf(obj)
			if owner == nil {
				return nil
			}
			if owner.APIVersion != enterprisev4.GroupVersion.String() || owner.Kind != "PostgresDatabase" {
				return nil
			}
			return []string{owner.Name}
		},
	); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresDatabase{}, builder.WithPredicates(
			predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.Funcs{
					UpdateFunc: func(e event.UpdateEvent) bool {
						return !reflect.DeepEqual(
							e.ObjectOld.GetFinalizers(),
							e.ObjectNew.GetFinalizers(),
						)
					},
				},
			),
		)).
		Owns(&cnpgv1.Database{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Named("postgresdatabase").
		Complete(r)
}

// userSecretName gives both secret creation and status wiring a single source of truth
// for naming — eliminating any risk of the two sides drifting out of sync.
func userSecretName(postgresDBName, dbName, role string) string {
	return fmt.Sprintf("%s-%s-%s", postgresDBName, dbName, role)
}

func adminRoleName(dbName string) string { return dbName + "_admin" }
func rwRoleName(dbName string) string    { return dbName + "_rw" }
func cnpgDatabaseName(postgresDBName, dbName string) string {
	return fmt.Sprintf("%s-%s", postgresDBName, dbName)
}

// generatePassword uses crypto/rand (via sethvargo/go-password) rather than math/rand
// because these credentials protect live database access — predictability is unacceptable.
func generatePassword() (string, error) {
	return password.Generate(passwordLength, passwordDigits, passwordSymbols, false, true)
}

// reconcileUserSecrets checks existence before creating — intentionally not using
// CreateOrUpdate because secrets must never be updated after creation. Rotating a password
// here would break live connections before the application has a chance to pick up the change.
func (r *PostgresDatabaseReconciler) reconcileUserSecrets(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
) error {
	logger := log.FromContext(ctx)

	for _, dbSpec := range postgresDB.Spec.Databases {
		adminSecretName := userSecretName(postgresDB.Name, dbSpec.Name, secretRoleAdmin)
		rwSecretName := userSecretName(postgresDB.Name, dbSpec.Name, secretRoleRW)

		adminExists, err := r.secretExists(ctx, postgresDB.Namespace, adminSecretName)
		if err != nil {
			return err
		}
		rwExists, err := r.secretExists(ctx, postgresDB.Namespace, rwSecretName)
		if err != nil {
			return err
		}

		if !adminExists {
			logger.Info("Creating missing admin user secrets for database", "database", dbSpec.Name)
			if err := r.createUserSecret(ctx, postgresDB, adminRoleName(dbSpec.Name), adminSecretName); err != nil {
				return fmt.Errorf("failed to create secrets for database %s: %w", dbSpec.Name, err)
			}
		}
		if !rwExists {
			logger.Info("Creating missing rw user secrets for database", "database", dbSpec.Name)
			if err := r.createUserSecret(ctx, postgresDB, rwRoleName(dbSpec.Name), rwSecretName); err != nil {
				return fmt.Errorf("failed to create secrets for database %s: %w", dbSpec.Name, err)
			}
		}
	}
	return nil
}

// secretExists treats non-NotFound errors as real failures — a transient API error
// must not cause a spurious Create attempt.
func (r *PostgresDatabaseReconciler) secretExists(ctx context.Context, namespace, name string) (bool, error) {
	logger := log.FromContext(ctx)

	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, secret)
	if errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		logger.Error(err, "Failed to check secret existence", "secret", name)
		return false, err
	}
	return true, nil
}

// createUserSecret creates a single password secret for a database user.
// AlreadyExists is treated as success — safe to retry after a partial failure
// on a prior run without conflicting with the already-created secret.
// TODO: Secret cleanup will be added to the finalizers to respect retain state
func (r *PostgresDatabaseReconciler) createUserSecret(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
	roleName string,
	secretName string,
) error {
	logger := log.FromContext(ctx)

	password, err := generatePassword()
	if err != nil {
		logger.Error(err, "Failed to generate password", "secret", secretName)
		return err
	}

	secret := buildPasswordSecret(postgresDB, secretName, roleName, password)
	if err := r.Create(ctx, secret); err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		logger.Error(err, "Failed to create secret", "secret", secretName)
		return err
	}
	return nil
}

// buildPasswordSecret constructs the Secret object but intentionally omits an ownerReference.
// Secrets are referenced by ManagedRole entries in the PostgresCluster CR, which outlives the
// PostgresDatabase — cascade deletion would leave dangling PasswordSecretRef pointers.
// Both "username" and "password" keys are required by CNPG.
func buildPasswordSecret(postgresDB *enterprisev4.PostgresDatabase, secretName, roleName, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: postgresDB.Namespace,
			Labels: map[string]string{
				labelManagedBy:  "splunk-operator",
				labelCNPGReload: "true",
			},
		},
		Data: map[string][]byte{
			"username": []byte(roleName),
			"password": []byte(password),
		},
	}
}

// configMapName mirrors userSecretName() so creation and status wiring share one source of truth.
func configMapName(postgresDBName, dbName string) string {
	return fmt.Sprintf("%s-%s-config", postgresDBName, dbName)
}

// clusterEndpoints holds fully-resolved connection hostnames for a cluster.
// PoolerRWHost and PoolerROHost are empty when connection pooling is disabled.
type clusterEndpoints struct {
	RWHost       string
	ROHost       string
	PoolerRWHost string
	PoolerROHost string
}

func resolveClusterEndpoints(cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, namespace string) clusterEndpoints {
	// FQDN so consumers in other namespaces can resolve without extra config.
	endpoints := clusterEndpoints{
		RWHost: fmt.Sprintf("%s.%s.svc.cluster.local", cnpgCluster.Status.WriteService, namespace),
		ROHost: fmt.Sprintf("%s.%s.svc.cluster.local", cnpgCluster.Status.ReadService, namespace),
	}
	// Pooler service names follow the pattern set by postgrescluster_controller: {cnpgClusterName}-pooler-{rw|ro}.
	if cluster.Status.ConnectionPoolerStatus != nil && cluster.Status.ConnectionPoolerStatus.Enabled {
		endpoints.PoolerRWHost = fmt.Sprintf("%s-pooler-%s.%s.svc.cluster.local", cnpgCluster.Name, readWriteEndpoint, namespace)
		endpoints.PoolerROHost = fmt.Sprintf("%s-pooler-%s.%s.svc.cluster.local", cnpgCluster.Name, readOnlyEndpoint, namespace)
	}
	return endpoints
}

// buildDatabaseConfigMapBody is a pure function — no API calls, no decisions about which
// endpoints exist. All that is resolved upstream and encoded in endpoints before this is called.
func buildDatabaseConfigMapBody(
	dbName string,
	endpoints clusterEndpoints,
) map[string]string {
	data := map[string]string{
		"dbname":     dbName,
		"port":       postgresPort,
		"rw-host":    endpoints.RWHost,
		"ro-host":    endpoints.ROHost,
		"admin-user": adminRoleName(dbName),
		"rw-user":    rwRoleName(dbName),
	}
	// Pooler keys are only written when pooling is active
	if endpoints.PoolerRWHost != "" {
		data["pooler-rw-host"] = endpoints.PoolerRWHost
	}
	if endpoints.PoolerROHost != "" {
		data["pooler-ro-host"] = endpoints.PoolerROHost
	}
	return data
}

// reconcileUserConfigMaps mirrors reconcileUserSecrets: checks per-database,
// creates only what is absent. Endpoints are resolved by the caller so this function
// has a single responsibility: iteration and existence-gated creation.
func (r *PostgresDatabaseReconciler) reconcileUserConfigMaps(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
	endpoints clusterEndpoints,
) error {
	logger := log.FromContext(ctx)

	for _, dbSpec := range postgresDB.Spec.Databases {
		cmName := configMapName(postgresDB.Name, dbSpec.Name)

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: postgresDB.Namespace,
				Labels: map[string]string{
					labelManagedBy: "splunk-operator",
				},
			},
		}

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
			cm.Data = buildDatabaseConfigMapBody(dbSpec.Name, endpoints)

			if cm.CreationTimestamp.IsZero() {
				if err := controllerutil.SetControllerReference(postgresDB, cm, r.Scheme); err != nil {
					logger.Error(err, "Failed to set owner reference on ConfigMap", "configmap", cm.Name)
					return err
				}
			}
			return nil
		})
		if err != nil {
			logger.Error(err, "failed to create or update database configmap", "db", postgresDB.Name, "configmap", cmName)
			return fmt.Errorf("failed to create or update database configmap %s: %w", cmName, err)
		}
	}
	return nil
}

// populateDatabaseStatus derives all secret ref names via userSecretName() — the same function
// used during creation — so status refs are always consistent with actual secret names.
// Recomputing from spec rather than reading live Secret names keeps this side-effect free.
func populateDatabaseStatus(postgresDB *enterprisev4.PostgresDatabase) []enterprisev4.DatabaseInfo {
	databases := make([]enterprisev4.DatabaseInfo, 0, len(postgresDB.Spec.Databases))
	for _, dbSpec := range postgresDB.Spec.Databases {
		databases = append(databases, enterprisev4.DatabaseInfo{
			Name:  dbSpec.Name,
			Ready: true,
			AdminUserSecretRef: &corev1.LocalObjectReference{
				Name: userSecretName(postgresDB.Name, dbSpec.Name, secretRoleAdmin),
			},
			RWUserSecretRef: &corev1.LocalObjectReference{
				Name: userSecretName(postgresDB.Name, dbSpec.Name, secretRoleRW),
			},
			ConfigMapRef: &corev1.LocalObjectReference{
				Name: configMapName(postgresDB.Name, dbSpec.Name),
			},
		})
	}
	return databases
}
