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
	"slices"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
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

func (r *PostgresDatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PostgresDatabase", "name", req.Name, "namespace", req.Namespace)

	// Fetch the PostgresDatabase CR
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

	// Create closure for status updates
	updateStatus := func(conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcilePhases) error {
		return r.updateStatus(ctx, postgresDB, conditionType, conditionStatus, reason, message, phase)
	}

	// Finalizer logic: clean up managed users and databases from CNPG when PostgresDatabase is deleted
	if !postgresDB.ObjectMeta.DeletionTimestamp.IsZero() {
		// Remove CNPG Database CRs owned by this PostgresDatabase
		if err := r.removeDatabases(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to delete CNPG Databases during cleanup")
			if statusErr := updateStatus(databasesReady, metav1.ConditionFalse, reasonDatabasesCleanupFailed, fmt.Sprintf("Failed to delete databases during cleanup: %v", err), failed); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		// Remove managed users from PostgresCluster via SSA (release field ownership)
		if err := r.removeUsersFromCluster(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to remove users during cleanup")
			if statusErr := updateStatus(usersReady, metav1.ConditionFalse, reasonUsersCleanupFailed, fmt.Sprintf("Failed to remove users during cleanup: %v", err), failed); statusErr != nil {
				logger.Error(statusErr, "Failed to update status")
			}
			return ctrl.Result{}, err
		}
		// Remove finalizer after cleanup
		controllerutil.RemoveFinalizer(postgresDB, postgresDatabaseFinalizerName)
		if err := r.Update(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to remove finalizer from PostgresDatabase")
			return ctrl.Result{}, err
		}
		logger.Info("Cleanup complete for PostgresDatabase", "name", postgresDB.Name)
		return ctrl.Result{}, nil
	}
	// Object is not being deleted — ensure finalizer is registered
	if !controllerutil.ContainsFinalizer(postgresDB, postgresDatabaseFinalizerName) {
		controllerutil.AddFinalizer(postgresDB, postgresDatabaseFinalizerName)
		if err := r.Update(ctx, postgresDB); err != nil {
			logger.Error(err, "Failed to add finalizer to PostgresDatabase")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // requeue via watch
	}

	// skip if spec unchanged and all work complete
	if postgresDB.Status.ObservedGeneration == postgresDB.Generation {
		logger.Info("Spec unchanged and all phases complete, skipping")
		return ctrl.Result{}, nil
	}
	logger.Info("Changes to resource detected, reconciling...")

	// Phase 1: Verify cluster exists and is ready
	var cluster *enterprisev4.PostgresCluster
	var clusterStatus clusterReadyStatus
	var err error

	cluster, clusterStatus, err = r.ensureClusterReady(ctx, postgresDB)
	if err != nil {
		if statusErr := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterInfoFetchFailed, "Can't reach Cluster CR due to transient errors", pending); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	logger.Info("Cluster validation done", "clusterName", cluster.Name, "status", clusterStatus)
	switch clusterStatus {
	case ClusterNotFound:
		if err := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterNotFound, "Cluster CR not found", pending); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil

	case ClusterNotReady, ClusterNoProvisionerRef:
		if err := updateStatus(clusterReady, metav1.ConditionFalse, reasonClusterProvisioning, "Cluster is not in ready state yet", pending); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil

	case ClusterReady:
		if err := updateStatus(clusterReady, metav1.ConditionTrue, reasonClusterAvailable, "Cluster is operational", provisioning); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Fetch CNPG Cluster to check both spec and status of users and databases
	cnpgCluster := &cnpgv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.ProvisionerRef.Name,
		Namespace: cluster.Status.ProvisionerRef.Namespace,
	}, cnpgCluster); err != nil {
		logger.Error(err, "Failed to fetch CNPG Cluster")
		return ctrl.Result{}, err
	}

	// Phase 2: Verify users exist and are reconciled in CNPG

	// Get desired users from our spec
	desiredUsers := getDesiredUsers(postgresDB)

	// Check if CNPG spec needs updating
	actualUsersInSpec := getUsersInCNPGSpec(cnpgCluster)
	missingUsersFromSpec := []string{}
	for _, user := range desiredUsers {
		if !slices.Contains(actualUsersInSpec, user) {
			missingUsersFromSpec = append(missingUsersFromSpec, user)
		}
	}

	if len(missingUsersFromSpec) > 0 {
		logger.Info("User spec changed, patching CNPG Cluster", "adding", missingUsersFromSpec)
		if err := r.createUsers(ctx, postgresDB, cluster); err != nil {
			logger.Error(err, "Failed to patch users in CNPG Cluster")
			return ctrl.Result{}, err
		}
		// Spec updated, requeue to check status
		if err := updateStatus(usersReady, metav1.ConditionFalse, reasonWaitingForCNPG, fmt.Sprintf("Waiting for %d users to be reconciled", len(desiredUsers)), provisioning); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	// Spec is correct, verify status - are users reconciled?
	allUsersReady, notReadyUsers, err := r.verifyUsersReady(ctx, postgresDB, cluster)
	if err != nil {
		if statusErr := updateStatus(usersReady, metav1.ConditionFalse, reasonUsersCreationFailed, fmt.Sprintf("User creation failed: %v", err), failed); statusErr != nil {
			logger.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	if !allUsersReady {
		// Users in spec but not yet reconciled by CNPG - wait
		logger.Info("Users in CNPG spec but not reconciled yet", "pending", notReadyUsers)
		if err := updateStatus(usersReady, metav1.ConditionFalse, reasonWaitingForCNPG, fmt.Sprintf("Waiting for %d users to be reconciled", len(notReadyUsers)), provisioning); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	// All users present in spec and reconciled in status
	if err := updateStatus(usersReady, metav1.ConditionTrue, reasonUsersAvailable, fmt.Sprintf("All %d users in PostgreSQL", len(desiredUsers)), provisioning); err != nil {
		return ctrl.Result{}, err
	}

	// Phase 3: Verify databases exist and are ready in CNPG

	// Get desired users from our spec
	desiredDatabases := getDesiredDatabases(postgresDB)

	// Check if CNPG spec needs updating
	actualDatabasesInSpec, err := r.getDatabasesInCNPGSpec(ctx, postgresDB)
	if err != nil {
		logger.Error(err, "Failed to fetch CNPG Databases")
		return ctrl.Result{}, err
	}
	missingDatabaseFromSpec := []string{}

	for _, database := range desiredDatabases {
		if !slices.Contains(actualDatabasesInSpec, database) {
			missingDatabaseFromSpec = append(missingDatabaseFromSpec, database)
		}
	}

	if len(missingDatabaseFromSpec) > 0 {
		logger.Info("Database spec changed, patching CNPG Cluster", "adding", missingDatabaseFromSpec)
		if err := r.createDatabases(ctx, postgresDB, cluster); err != nil {
			logger.Error(err, "Failed to patch database in CNPG Cluster")
			return ctrl.Result{}, err
		}
		// Spec updated, requeue to check status
		if err := updateStatus(databasesReady, metav1.ConditionFalse, reasonWaitingForCNPG, fmt.Sprintf("Waiting for %d database to be reconciled", len(desiredDatabases)), provisioning); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	allDatabasesReady, err := r.verifyDatabasesReady(ctx, postgresDB)
	if err != nil {
		logger.Error(err, "Failed to verify database status")
		return ctrl.Result{}, err
	}

	if !allDatabasesReady {
		logger.Info("Databases in CNPG spec but not reconciled yet")
		// Databases in spec but not yet reconciled by CNPG - wait
		if err := updateStatus(databasesReady, metav1.ConditionFalse, reasonWaitingForCNPG, "Waiting for databases to be ready", provisioning); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}

	// All phases complete - mark as reconciled
	postgresDB.Status.ObservedGeneration = postgresDB.Generation
	if err := updateStatus(databasesReady, metav1.ConditionTrue, reasonDatabasesAvailable, fmt.Sprintf("All %d databases ready", len(postgresDB.Spec.Databases)), ready); err != nil {
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

	// Fetch the PostgresCluster CR
	cluster := &enterprisev4.PostgresCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		if errors.IsNotFound(err) {
			return nil, ClusterNotFound, nil
		}
		logger.Error(err, "Failed to fetch Cluster", "name", postgresDB.Spec.ClusterRef.Name)
		return nil, ClusterNotReady, err
	}

	// Check if cluster is ready
	if cluster.Status.Phase != string(ClusterReady) {
		logger.Info("Cluster not ready", "status", cluster.Status.Phase)
		return cluster, ClusterNotReady, nil
	}

	// Validate ProvisionerRef exists
	if cluster.Status.ProvisionerRef == nil {
		logger.Info("Cluster has no ProvisionerRef yet", "cluster", cluster.Name)
		return cluster, ClusterNoProvisionerRef, nil
	}

	return cluster, ClusterReady, nil
}

// getDesiredUsers builds the list of users we want to create for this PostgresDatabase
func getDesiredUsers(postgresDB *enterprisev4.PostgresDatabase) []string {
	users := []string{}
	for _, dbSpec := range postgresDB.Spec.Databases {
		users = append(users,
			fmt.Sprintf("%s_admin", dbSpec.Name),
			fmt.Sprintf("%s_rw", dbSpec.Name),
		)
	}
	return users
}

// getUsersInCNPGSpec extracts the list of managed role names from CNPG Cluster spec
func getUsersInCNPGSpec(cnpgCluster *cnpgv1.Cluster) []string {
	if cnpgCluster.Spec.Managed == nil || cnpgCluster.Spec.Managed.Roles == nil {
		return []string{}
	}

	users := []string{}
	for _, role := range cnpgCluster.Spec.Managed.Roles {
		users = append(users, role.Name)
	}
	return users
}

// getDesiredUsers builds the list of users we want to create for this PostgresDatabase
func getDesiredDatabases(postgresDB *enterprisev4.PostgresDatabase) []string {
	databases := []string{}
	for _, dbSpec := range postgresDB.Spec.Databases {
		databases = append(databases, dbSpec.Name)
	}
	return databases
}

// getUsersInCNPGSpec extracts the list of managed role names from CNPG Cluster spec
func (r *PostgresDatabaseReconciler) getDatabasesInCNPGSpec(ctx context.Context, postgresDB *enterprisev4.PostgresDatabase) ([]string, error) {
	// find all databases where we have reference from cluster
	var dbList []string
	cnpgDBList := &cnpgv1.DatabaseList{}
	if err := r.List(ctx, cnpgDBList,
		client.InNamespace(postgresDB.Namespace),
		client.MatchingFields{".metadata.controller": postgresDB.Name},
	); err != nil {
		return nil, err
	}
	for _, db := range cnpgDBList.Items {
		// format of the db is fmt.Sprintf("%s-%s", postgresDB.Name, dbSpec.Name) so we need to remove postgresDatabase prefix first
		dbList = append(dbList, strings.TrimPrefix(db.Name, fmt.Sprintf("%s-", postgresDB.Name)))
	}
	return dbList, nil
}

// createUsers patches PostgresCluster with managed roles via SSA
// PostgresCluster controller will then reconcile these roles to CNPG Cluster
// Returns: error (patch failure only)
func (r *PostgresDatabaseReconciler) createUsers(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
	cluster *enterprisev4.PostgresCluster,
) error {
	logger := log.FromContext(ctx)

	// Build list of roles for this PostgresDatabase
	allRoles := []enterprisev4.ManagedRole{}
	for _, dbSpec := range postgresDB.Spec.Databases {
		dbAdminUser := fmt.Sprintf("%s_admin", dbSpec.Name)
		dbRWUser := fmt.Sprintf("%s_rw", dbSpec.Name)
		allRoles = append(allRoles,
			enterprisev4.ManagedRole{
				Name:   dbAdminUser,
				Ensure: "present",
			},
			enterprisev4.ManagedRole{
				Name:   dbRWUser,
				Ensure: "present",
			})
	}

	// Build unstructured SSA patch with only managedRoles — avoids Go zero-value fields
	// (like spec.class) leaking into the patch and causing SSA ownership conflicts.
	roles := make([]interface{}, len(allRoles))
	for i, role := range allRoles {
		roles[i] = map[string]interface{}{
			"name":   role.Name,
			"ensure": role.Ensure,
		}
	}

	rolePatch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "enterprise.splunk.com/v4",
			"kind":       "PostgresCluster",
			"metadata": map[string]interface{}{
				"name":      cluster.Name,
				"namespace": cluster.Namespace,
			},
			"spec": map[string]interface{}{
				"managedRoles": roles,
			},
		},
	}

	fieldManager := fmt.Sprintf("postgresdatabase-%s", postgresDB.Name)
	if err := r.Patch(ctx, rolePatch, client.Apply, client.FieldOwner(fieldManager)); err != nil {
		logger.Error(err, "Failed to add users to PostgresCluster", "postgresDatabase", postgresDB.Name)
		return err
	}
	logger.Info("Users added to PostgresCluster via SSA", "postgresDatabase", postgresDB.Name, "postgresCluster", cluster.Name, "roleCount", len(allRoles))

	return nil
}

// verifyUsersReady checks if CNPG has finished creating the users
func (r *PostgresDatabaseReconciler) verifyUsersReady(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
	cluster *enterprisev4.PostgresCluster,
) (bool, []string, error) {
	logger := log.FromContext(ctx)

	// Fetch CNPG Cluster to check user status
	// ABSTRACTION BREAK: We fetch CNPG Cluster directly for now, migrate to postggres cluster CR once we can fetch status from there
	cnpgCluster := &cnpgv1.Cluster{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.ProvisionerRef.Name,
		Namespace: cluster.Status.ProvisionerRef.Namespace,
	}, cnpgCluster); err != nil {
		logger.Error(err, "Failed to fetch CNPG Cluster status")
		return false, nil, err
	}

	expectedUsers := getDesiredUsers(postgresDB)

	if cnpgCluster.Status.ManagedRolesStatus.CannotReconcile != nil {
		for _, userName := range expectedUsers {
			if errs, exists := cnpgCluster.Status.ManagedRolesStatus.CannotReconcile[userName]; exists {
				logger.Error(nil, "User reconciliation failed permanently", "user", userName, "errors", errs)
				return false, nil, fmt.Errorf("user %s reconciliation failed: %v", userName, errs)
			}
		}
	}

	// Check if all users are reconciled
	reconciledUsers := cnpgCluster.Status.ManagedRolesStatus.ByStatus[cnpgv1.RoleStatusReconciled]
	notReady := []string{}
	for _, userName := range expectedUsers {
		if !slices.Contains(reconciledUsers, userName) {
			notReady = append(notReady, userName)
		}
	}

	// Return readiness status
	if len(notReady) > 0 {
		logger.Info("Users not reconciled yet", "pending", notReady)
		return false, notReady, nil
	}

	logger.Info("All users reconciled")
	return true, nil, nil
}

// createDatabases creates CNPG Database CRs
func (r *PostgresDatabaseReconciler) createDatabases(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
	cluster *enterprisev4.PostgresCluster,
) error {
	logger := log.FromContext(ctx)

	// Create databases
	for _, dbSpec := range postgresDB.Spec.Databases {
		logger.Info("Processing database", "database", dbSpec.Name)

		cnpgDBName := fmt.Sprintf("%s-%s", postgresDB.Name, dbSpec.Name)

		// Create CNPG Database CR
		cnpgDB := &cnpgv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cnpgDBName,
				Namespace: postgresDB.Namespace,
			},
			Spec: cnpgv1.DatabaseSpec{
				Name:  dbSpec.Name,
				Owner: fmt.Sprintf("%s_admin", dbSpec.Name),
				ClusterRef: corev1.LocalObjectReference{
					Name: cluster.Status.ProvisionerRef.Name,
				},
			},
		}

		// Set owner reference for cascade deletion
		if err := controllerutil.SetControllerReference(postgresDB, cnpgDB, r.Scheme); err != nil {
			logger.Error(err, "Failed to set owner reference")
			return err
		}
		// Create or update database
		if err := r.Create(ctx, cnpgDB); err != nil {
			if errors.IsAlreadyExists(err) {
				logger.Info("Database already exists, updating", "database", dbSpec.Name)

				// Fetch existing and update
				currentDB := &cnpgv1.Database{}
				if err := r.Get(ctx, types.NamespacedName{
					Name:      cnpgDBName,
					Namespace: postgresDB.Namespace,
				}, currentDB); err != nil {
					logger.Error(err, "Failed to get existing CNPG Database", "name", cnpgDBName)
					return err
				}

				// Update spec
				currentDB.Spec = cnpgDB.Spec
				if err := r.Update(ctx, currentDB); err != nil {
					logger.Error(err, "Failed to update CNPG Database", "name", cnpgDBName)
					return err
				}
			} else {
				// Real error (not AlreadyExists)
				logger.Error(err, "Failed to create CNPG Database", "name", cnpgDBName)
				return err
			}
		}

		logger.Info("CNPG Database created/updated successfully", "database", dbSpec.Name)
	}

	return nil
}

// verifyDatabasesReady checks if CNPG has finished provisioning the databases
func (r *PostgresDatabaseReconciler) verifyDatabasesReady(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
) (bool, error) {
	logger := log.FromContext(ctx)

	for _, dbSpec := range postgresDB.Spec.Databases {
		cnpgDBName := fmt.Sprintf("%s-%s", postgresDB.Name, dbSpec.Name)

		// Fetch the CNPG Database to check its status
		cnpgDB := &cnpgv1.Database{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      cnpgDBName,
			Namespace: postgresDB.Namespace,
		}, cnpgDB); err != nil {
			logger.Error(err, "Failed to get CNPG Database status", "database", dbSpec.Name)
			return false, err
		}

		// Check if CNPG reports this database as ready
		if cnpgDB.Status.Applied == nil || !*cnpgDB.Status.Applied {
			logger.Info("Database not ready yet", "database", dbSpec.Name)
			return false, nil
		}
	}

	logger.Info("All databases provisioned")
	return true, nil
}

func (r *PostgresDatabaseReconciler) updateStatus(
	ctx context.Context,
	db *enterprisev4.PostgresDatabase,
	conditionType conditionTypes,
	conditionStatus metav1.ConditionStatus,
	reason conditionReasons,
	message string,
	phase reconcilePhases,
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

// removeUsersFromCluster removes managed roles from the PostgresCluster by patching with an empty roles list
// using the same SSA field manager, which releases ownership of the roles this PostgresDatabase previously managed.
func (r *PostgresDatabaseReconciler) removeUsersFromCluster(
	ctx context.Context,
	postgresDB *enterprisev4.PostgresDatabase,
) error {
	logger := log.FromContext(ctx)

	// Find the referenced PostgresCluster
	cluster := &enterprisev4.PostgresCluster{}
	if err := r.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("PostgresCluster already deleted, skipping user cleanup")
			return nil
		}
		return fmt.Errorf("failed to get PostgresCluster for user cleanup: %w", err)
	}

	// Patch with empty managedRoles using unstructured to avoid Go zero-value fields
	// leaking into the patch. Using the same field manager releases ownership of roles
	// that this PostgresDatabase previously managed.
	rolePatch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "enterprise.splunk.com/v4",
			"kind":       "PostgresCluster",
			"metadata": map[string]interface{}{
				"name":      cluster.Name,
				"namespace": cluster.Namespace,
			},
			"spec": map[string]interface{}{
				"managedRoles": []interface{}{},
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

	for i := range cnpgDBList.Items {
		db := &cnpgDBList.Items[i]
		logger.Info("Deleting CNPG Database", "name", db.Name)
		if err := r.Delete(ctx, db); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete CNPG Database %s: %w", db.Name, err)
		}
	}

	logger.Info("All CNPG Databases deleted", "count", len(cnpgDBList.Items))
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresDatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Index CNPG Databases by their PostgresDatabase controller owner name
	// Allows efficient lookup of all databases owned by a specific PostgresDatabase CR
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&cnpgv1.Database{},
		".metadata.controller",
		func(obj client.Object) []string {
			// Get the owner of cnpg database object
			owner := metav1.GetControllerOf(obj)
			if owner == nil {
				return nil
			}
			// Do not index if owner is not us
			if owner.APIVersion != enterprisev4.GroupVersion.String() || owner.Kind != "PostgresDatabase" {
				return nil
			}
			return []string{owner.Name}
		},
	); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&enterprisev4.PostgresDatabase{}).
		Owns(&cnpgv1.Database{}).
		Named("postgresdatabase").
		Complete(r)
}
