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
	stderrors "errors"
	"fmt"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/sethvargo/go-password/password"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	dbmetrics "github.com/splunk/splunk-operator/pkg/postgresql/database/core/custom_metrics"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NewDBRepoFunc constructs a DBRepo adapter for the given host and database.
// Injected by the controller so the core never imports the pgx adapter directly.
type NewDBRepoFunc func(ctx context.Context, host, dbName, password string) (DBRepo, error)

// secretReconcileError is the single typed, terminal failure raised while
// reconciling externally managed or provisioned role secrets — covering both
// "absent" (reasonExternalSecretMissing) and "present but invalid"/"drift". It
// carries the conditionReason so the handler branches on reason rather than on a
// distinct type; message holds only user-facing context.
type secretReconcileError struct {
	message string
	reason  conditionReasons
}

func (e secretReconcileError) Error() string {
	return e.message
}

// Unwraps and searches for reasonExternalSecretMissing in the err chain
func chooseSecretError(err error) *secretReconcileError {
	leaves := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		leaves = joined.Unwrap()
	}

	var first *secretReconcileError
	for _, leaf := range leaves {
		var se secretReconcileError
		if !stderrors.As(leaf, &se) {
			continue
		}
		if se.reason == reasonExternalSecretMissing {
			return &se
		}
		if first == nil {
			first = &se
		}
	}
	return first
}

type secretMissingPolicy int

const (
	createSecretIfMissing secretMissingPolicy = iota
	reportSecretDriftIfMissing
)

func requeueOnConflict(ctx context.Context, err error, category reconcileConflictCategory, action string) (ctrl.Result, error, bool) {
	if !errors.IsConflict(err) {
		return ctrl.Result{}, err, false
	}

	// Keep the category stable so future metrics or events can aggregate conflict sources.
	logging.FromContext(ctx).InfoContext(ctx,
		"conflict during PostgresDatabase reconciliation, will requeue",
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
	logger := logging.FromContext(ctx).With("func", "PostgresDatabaseService", "postgresDatabase", postgresDB.Name)
	ctx = logging.WithLogger(ctx, logger)
	logger.DebugContext(ctx, "reconciling PostgresDatabase")
	wasReady := postgresDB.Status.Phase != nil && *postgresDB.Status.Phase == string(readyDBPhase)

	updateStatus := func(conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
		return persistStatus(ctx, c, rc.Metrics, postgresDB, wasReady, conditionType, conditionStatus, reason, message, phase)
	}

	// Finalizer: cleanup on deletion, register on creation.
	if postgresDB.GetDeletionTimestamp() != nil {
		if err := handleDeletion(ctx, rc, postgresDB); err != nil {
			if stderrors.Is(err, errRoleCleanupPending) {
				return ctrl.Result{RequeueAfter: retryDelay}, nil
			}
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDeletion, "handling deletion"); ok {
				return result, conflictErr
			}
			rc.emitWarning(postgresDB, EventCleanupFailed, fmt.Sprintf("cleanup failed for PostgresDatabase %s — check operator logs", postgresDB.Name))
			return ctrl.Result{}, fmt.Errorf("failed to clean up PostgresDatabase: %w", err)
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
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		logger.InfoContext(ctx, "finalizer added successfully")
		return ctrl.Result{}, nil
	}

	_, err := persistCustomMetricsPublication(ctx, c, postgresDB)
	if err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictCustomMetricsStatus, "publishing custom metrics participation"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, fmt.Errorf("publishing custom metrics participation: %w", err)
	}

	currentReconcileFailure := hasCurrentReconcileFailure(postgresDB)

	// A terminal failure only blocks the generation that observed it. A stale
	// marker is kept until recovery succeeds because earlier status updates
	// persist the whole status object and may return before the failed phase.
	retryAfterStaleReconcileFailure := postgresDB.Status.ReconcileFailureType != "" && !currentReconcileFailure
	if currentReconcileFailure {
		return ctrl.Result{}, nil
	}
	previouslyProvisionedDatabases := existingDatabaseStatus(postgresDB)
	if retryAfterStaleReconcileFailure {
		// During stale terminal recovery the phase is still Failed, but status.databases
		// records previously provisioned databases whose credentials must not be regenerated.
		previouslyProvisionedDatabases = make(map[string]struct{}, len(postgresDB.Status.Databases))
		for _, database := range postgresDB.Status.Databases {
			previouslyProvisionedDatabases[database.Name] = struct{}{}
		}
	}

	// Phase: ClusterValidation
	cluster, err := fetchCluster(ctx, c, postgresDB)
	if err != nil {
		if errors.IsNotFound(err) {
			rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, clusterReady, EventClusterNotFound, fmt.Sprintf("PostgresCluster %s not found", postgresDB.Spec.ClusterRef.Name))
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
			logger.ErrorContext(ctx, "failed to persist cluster status", "error", statusErr)
		}
		return ctrl.Result{}, err
	}
	clusterStatus := getClusterReadyStatus(cluster)
	logger.DebugContext(ctx, "cluster validation completed", "clusterRef", postgresDB.Spec.ClusterRef.Name, "status", clusterStatus)

	switch clusterStatus {
	case ClusterNotReady, ClusterNoProvisionerRef:
		eventReason := EventClusterNotReady
		eventMessage := fmt.Sprintf("referenced PostgresCluster %s is not ready yet", postgresDB.Spec.ClusterRef.Name)
		conditionReason := reasonClusterProvisioning
		conditionMessage := "Cluster is not in ready state yet"
		clusterCondition := meta.FindStatusCondition(postgresDB.Status.Conditions, string(clusterReady))
		reportRecovery := wasReady || (clusterCondition != nil && clusterCondition.Reason == string(reasonClusterRecovery))
		if reportRecovery && isClusterInRecovery(cluster) {
			eventReason = EventWaitingForClusterRecovery
			eventMessage = fmt.Sprintf("referenced PostgresCluster %s is recovering", postgresDB.Spec.ClusterRef.Name)
			conditionReason = reasonClusterRecovery
			conditionMessage = "Cluster is recovering; waiting for it to become ready"
		}
		rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, clusterReady, eventReason, eventMessage)
		if err := updateStatus(clusterReady, metav1.ConditionFalse, conditionReason, conditionMessage, pendingDBPhase); err != nil {
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

	cnpgCluster := &cnpgv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      cluster.Status.ProvisionerRef.Name,
		Namespace: cluster.Status.ProvisionerRef.Namespace,
	}, cnpgCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to fetch CNPG Cluster %s: %w", cluster.Status.ProvisionerRef.Name, err)
	}

	// Phase: CredentialProvisioning — secrets must exist before roles are patched.
	// CNPG rejects a PasswordSecretRef pointing at a missing secret.
	if err := reconcileRoleSecrets(ctx, c, rc.Scheme, postgresDB, previouslyProvisionedDatabases); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictSecretsReconcile, "reconciling user secrets"); ok {
			return result, conflictErr
		}

		// Both "missing" and "invalid/drift" share one typed carrier now; branch
		// on the embedded reason. chooseSecretError prefers a missing-reason leaf
		// across the joined admin/RW errors so a missing secret (hard failure)
		// still wins over a merely invalid one, regardless of ordering.
		if secretErr := chooseSecretError(err); secretErr != nil {
			if secretErr.reason == reasonExternalSecretMissing {
				rc.emitWarning(postgresDB, EventRoleSecretsFailed, fmt.Sprintf("external secret(s) are missing for PostgresDatabase %s — check operator logs", postgresDB.Name))
				if statusErr := updateStatus(secretsReady, metav1.ConditionFalse, reasonExternalSecretMissing,
					fmt.Sprintf("external secret(s) are missing: %v", err), failedDBPhase); statusErr != nil {
					if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictSecretsStatus, "persisting secret failure status"); ok {
						return result, conflictErr
					}
					logger.ErrorContext(ctx, "failed to persist secrets status", "error", statusErr)
					return ctrl.Result{}, statusErr
				}
				// missing external secret == terminal
				// recovery is driven by the external-Secret watch predicate.
				return ctrl.Result{}, reconcile.TerminalError(err)
			}

			// Use err.Error() (not secretErr.message) so a combined admin+RW
			// failure surfaces both causes rather than just the first match.
			rc.emitWarning(postgresDB, EventRolesSecretsDriftDetected, err.Error())
			if statusErr := updateStatus(secretsReady, metav1.ConditionFalse, secretErr.reason,
				err.Error(), provisioningDBPhase); statusErr != nil {
				if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictSecretsStatus, "persisting secret drift status"); ok {
					return result, conflictErr
				}
				logger.ErrorContext(ctx, "failed to persist secret drift status", "error", statusErr)
			}
			return ctrl.Result{RequeueAfter: retryDelay}, nil
		}

		rc.emitWarning(postgresDB, EventRoleSecretsFailed, fmt.Sprintf("failed to reconcile user secrets for PostgresDatabase %s — check operator logs", postgresDB.Name))
		if statusErr := updateStatus(secretsReady, metav1.ConditionFalse, reasonSecretsCreationFailed,
			fmt.Sprintf("Failed to reconcile user secrets: %v", err), provisioningDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictSecretsStatus, "persisting secret failure status"); ok {
				return result, conflictErr
			}
			logger.ErrorContext(ctx, "failed to persist secrets status", "error", statusErr)
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
	// Publish credential-ready roles for cluster-side role reconciliation.
	if err := persistDatabaseInfos(ctx, c, postgresDB, false, rolesExist); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictSecretsStatus, "publishing credential-ready role status"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, err
	}

	// Phase: ConnectionMetadata — ConfigMaps carry connection info consumers need as soon
	// as databases are ready, so they are created alongside secrets.
	endpoints, err := resolveClusterEndpoints(cluster, cnpgCluster, postgresDB.Namespace)
	if err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictConfigMapsReconcile, "resolving configmap endpoints"); ok {
			return result, conflictErr
		}
		rc.emitWarning(postgresDB, EventAccessConfigFailed, fmt.Sprintf("failed to resolve ConfigMap endpoints for PostgresDatabase %s — check operator logs", postgresDB.Name))
		if statusErr := updateStatus(configMapsReady, metav1.ConditionFalse, reasonConfigMapsCreationFailed,
			fmt.Sprintf("Failed to resolve ConfigMap endpoints: %v", err), provisioningDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictConfigMapsStatus, "persisting configmaps endpoint failure status"); ok {
				return result, conflictErr
			}
			logger.ErrorContext(ctx, "failed to persist configmaps endpoint failure status", "error", statusErr)
		}
		return ctrl.Result{}, err
	}
	if err := reconcileRoleConfigMaps(ctx, c, rc.Scheme, postgresDB, endpoints); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictConfigMapsReconcile, "reconciling configmaps"); ok {
			return result, conflictErr
		}
		rc.emitWarning(postgresDB, EventAccessConfigFailed, fmt.Sprintf("failed to reconcile ConfigMaps for PostgresDatabase %s — check operator logs", postgresDB.Name))
		if statusErr := updateStatus(configMapsReady, metav1.ConditionFalse, reasonConfigMapsCreationFailed,
			fmt.Sprintf("Failed to reconcile ConfigMaps: %v", err), provisioningDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictConfigMapsStatus, "persisting configmaps failure status"); ok {
				return result, conflictErr
			}
			logger.ErrorContext(ctx, "failed to persist configmaps status", "error", statusErr)
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

	switch gate := evaluateRoleGate(postgresDB, cluster.Status.ManagedRolesStatus); gate.State {
	case roleGateConflict:
		conflictMsg := fmt.Sprintf("Role conflict in PostgresDatabase %s: %s", postgresDB.Name, gate.Message)
		rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, rolesReady, EventRoleConflict, conflictMsg)
		if statusErr := updateStatus(rolesReady, metav1.ConditionFalse, reasonRoleConflict, conflictMsg, failedDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictRoleConflictStatus, "persisting role conflict status"); ok {
				return result, conflictErr
			}
			logger.ErrorContext(ctx, "failed to persist role conflict status", "error", statusErr)
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	case roleGateFailed:
		failedMsg := fmt.Sprintf("Role reconciliation failed for PostgresDatabase %s: %s", postgresDB.Name, gate.Message)
		rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, rolesReady, EventRoleReconcileFailed, failedMsg)
		if statusErr := updateStatus(rolesReady, metav1.ConditionFalse, reasonRoleReconcileFailed, failedMsg, failedDBPhase); statusErr != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictRolesStatus, "persisting role reconcile failed status"); ok {
				return result, conflictErr
			}
			logger.ErrorContext(ctx, "failed to persist role reconcile failed status", "error", statusErr)
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	case roleGatePending:
		if err := updateStatus(rolesReady, metav1.ConditionFalse, reasonWaitingForCNPG, gate.Message, provisioningDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRolesStatus, "persisting roles pending status"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	}
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, rolesReady, EventRolesReady, fmt.Sprintf("Roles reconciled: %d active", len(getDesiredRoles(postgresDB))))
	if err := updateStatus(rolesReady, metav1.ConditionTrue, reasonRolesAvailable,
		fmt.Sprintf("Roles reconciled: %d active", len(getDesiredRoles(postgresDB))), provisioningDBPhase); err != nil {
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
		rc.emitWarning(postgresDB, EventDatabasesReconcileFailed, fmt.Sprintf("failed to reconcile databases for PostgresDatabase %s — check operator logs", postgresDB.Name))
		if statusErr := updateStatus(databasesReady, metav1.ConditionFalse, reasonDatabaseReconcileFailed,
			fmt.Sprintf("Failed to reconcile databases: %v", err), failedDBPhase); statusErr != nil {
			logger.ErrorContext(ctx, "failed to persist databases status", "error", statusErr)
		}
		return ctrl.Result{}, err
	}
	if len(adopted) > 0 {
		rc.emitNormal(postgresDB, EventResourcesAdopted, fmt.Sprintf("Adopted retained databases: %v", adopted))
	}

	notReadyDBs, err := verifyDatabasesReady(ctx, c, postgresDB)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to verify database readiness: %w", err)
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
		fmt.Sprintf("All %d databases ready", len(postgresDB.Spec.Databases)), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting databases ready status"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, err
	}

	// Phase: RWRolePrivileges
	// Skipped when no new databases are detected — ALTER DEFAULT PRIVILEGES covers tables
	// added by migrations on existing databases. Re-runs for all databases when a new one
	// is added, or when a spec change leaves a stale terminal failure to recover.
	databaseCount := len(postgresDB.Spec.Databases)
	privilegesMsg := fmt.Sprintf("RW role privileges already current for all %d databases", databaseCount)
	if hasNewDatabases(postgresDB) || retryAfterStaleReconcileFailure {
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
			if failureType, ok := terminalFailureType(err); ok {
				upsertFailureState(postgresDB, failureType)
				logger.ErrorContext(ctx, "RW role privileges grant failed terminally", "error", err)
				msg := "Failed to grant RW role privileges. Manual intervention required: " +
					"fix the PostgresDatabase spec or referenced configuration, then redeploy with a spec change."
				eventMsg := fmt.Sprintf("Failed to grant RW role privileges for PostgresDatabase %s. Manual intervention required: "+
					"fix the PostgresDatabase spec or referenced configuration, then redeploy with a spec change. "+
					"Check operator logs for details.", postgresDB.Name)
				rc.emitWarning(postgresDB, EventPrivilegesGrantFailed, eventMsg)
				if statusErr := updateStatus(privilegesReady, metav1.ConditionFalse, reasonPrivilegesTerminalFailure,
					msg, failedDBPhase); statusErr != nil {
					wrappedStatusErr := fmt.Errorf("failed to persist terminal privileges status: %w", statusErr)
					if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictPrivilegesStatus,
						"persisting terminal privileges status"); ok {
						return result, conflictErr
					}
					return ctrl.Result{}, stderrors.Join(err, wrappedStatusErr)
				}
				return ctrl.Result{}, nil
			}

			msg := fmt.Sprintf(
				"Failed to grant RW role privileges: %v. Will retry automatically.", err,
			)
			eventMsg := fmt.Sprintf("failed to grant RW role privileges for PostgresDatabase %s — check operator logs", postgresDB.Name)
			rc.emitWarning(postgresDB, EventPrivilegesGrantFailed, eventMsg)
			if statusErr := updateStatus(privilegesReady, metav1.ConditionFalse, reasonPrivilegesGrantFailed,
				msg, provisioningDBPhase); statusErr != nil {
				wrappedStatusErr := fmt.Errorf("failed to persist privileges status: %w", statusErr)
				if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictPrivilegesStatus,
					"persisting privileges failure status"); ok {
					return result, conflictErr
				}
				return ctrl.Result{}, stderrors.Join(err, wrappedStatusErr)
			}
			return ctrl.Result{}, err
		}
		if retryAfterStaleReconcileFailure {
			postgresDB.Status.ReconcileFailureType = ""
		}
		privilegesMsg = fmt.Sprintf("RW role privileges granted for all %d databases", databaseCount)
		rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, privilegesReady, EventPrivilegesReady, privilegesMsg)
	}
	applyStatus(postgresDB, privilegesReady, metav1.ConditionTrue, reasonPrivilegesGranted, privilegesMsg, readyDBPhase)
	completedReadinessCycle := postgresDB.Status.LastTransitionTime != nil
	var lastTransitionTime time.Time
	if completedReadinessCycle {
		lastTransitionTime = postgresDB.Status.LastTransitionTime.Time
		postgresDB.Status.LastTransitionTime = nil
	}

	metricsOutcome, err := reconcileCustomMetricsGate(ctx, rc, postgresDB, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling custom-metrics acknowledgement: %w", err)
	}
	switch metricsOutcome.State {
	case dbmetrics.GateFailed:
		rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, customMetricsReady, EventCustomMetricsFailed, metricsOutcome.Message)
		if err := persistCustomMetricsStatus(ctx, rc, postgresDB, metricsOutcome, metav1.ConditionFalse, failedDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictCustomMetricsStatus, "persisting custom metrics failure"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	case dbmetrics.GatePending:
		if err := persistCustomMetricsStatus(ctx, rc, postgresDB, metricsOutcome, metav1.ConditionUnknown, provisioningDBPhase); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictCustomMetricsStatus, "persisting custom metrics pending status"); ok {
				return result, conflictErr
			}
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil
	default:
		rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, customMetricsReady, EventCustomMetricsReady, metricsOutcome.Message)
		applyCustomMetricsStatus(rc, postgresDB, metricsOutcome, metav1.ConditionTrue, readyDBPhase)
	}

	if !wasReady {
		rc.emitNormal(postgresDB, EventPostgresDatabaseReady, fmt.Sprintf("PostgresDatabase %s is ready", postgresDB.Name))
	}
	postgresDB.Status.Databases = populateDatabaseStatus(postgresDB, true, rolesExist)
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation

	if err := c.Status().Update(ctx, postgresDB); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictFinalStatus, "persisting final status"); ok {
			return result, conflictErr
		}
		return ctrl.Result{}, fmt.Errorf("failed to persist final status: %w", err)
	}
	if completedReadinessCycle && rc.Metrics != nil {
		rc.Metrics.ObserveProvisioningDuration(
			ports.ControllerDatabase,
			time.Since(lastTransitionTime).Seconds(),
		)
	}

	logger.DebugContext(ctx, "PostgresDatabase reconciliation completed")
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
	logger := logging.FromContext(ctx)
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
		logger.InfoContext(ctx, "RW role privileges granted", "database", dbName, "rwRole", rwRoleName(dbName))
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

func isClusterInRecovery(cluster *enterprisev4.PostgresCluster) bool {
	cond := meta.FindStatusCondition(cluster.Status.Conditions, string(clusterReady))
	if cond == nil {
		return false
	}
	return cond.Reason == string(cnpgReasonRecovery) || cond.Reason == string(cnpgReasonFailingOver)
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
	existing := make(map[string]struct{}, len(postgresDB.Status.Databases))
	for _, database := range postgresDB.Status.Databases {
		existing[database.Name] = struct{}{}
	}
	return existing
}

type roleGateState string

const (
	roleGateProceed  roleGateState = "Proceed"
	roleGatePending  roleGateState = "Pending"
	roleGateConflict roleGateState = "Conflict"
	roleGateFailed   roleGateState = "Failed"
)

type roleGateDecision struct {
	State   roleGateState
	Message string
}

func evaluateRoleGate(postgresDB *enterprisev4.PostgresDatabase, status *enterprisev4.ManagedRolesStatus) roleGateDecision {
	if status == nil {
		return roleGateDecision{State: roleGatePending, Message: "Waiting for cluster to publish managed role status"}
	}
	self := enterprisev4.RoleOwnerReference{Name: postgresDB.Name, UID: string(postgresDB.UID)}
	roleSet := make(map[string]struct{}, len(getDesiredRoles(postgresDB)))
	for _, role := range getDesiredRoles(postgresDB) {
		roleSet[role] = struct{}{}
	}
	for _, conflict := range status.Conflicts {
		if _, wanted := roleSet[conflict.Role]; wanted && sameRoleOwner(conflict.AttemptedBy, self) {
			return roleGateDecision{State: roleGateConflict, Message: fmt.Sprintf("role %s is already claimed", conflict.Role)}
		}
	}
	for role := range roleSet {
		if reason, failed := status.Failed[role]; failed {
			return roleGateDecision{State: roleGateFailed, Message: fmt.Sprintf("role %s failed to reconcile: %s", role, reason)}
		}
	}
	reconciled := make(map[string]struct{}, len(status.Reconciled))
	for _, role := range status.Reconciled {
		reconciled[role] = struct{}{}
	}
	for role := range roleSet {
		owner, owned := status.RoleOwners[role]
		if !owned || !sameRoleOwner(owner, self) {
			return roleGateDecision{State: roleGatePending, Message: fmt.Sprintf("Waiting for role %s to be owned by this PostgresDatabase", role)}
		}
		if _, ok := reconciled[role]; !ok {
			return roleGateDecision{State: roleGatePending, Message: fmt.Sprintf("Waiting for role %s to be reconciled", role)}
		}
	}
	return roleGateDecision{State: roleGateProceed, Message: "Roles are reconciled and owned by this PostgresDatabase"}
}

func sameRoleOwner(a, b enterprisev4.RoleOwnerReference) bool {
	return a.Name == b.Name && a.UID == b.UID
}

func reconcileCNPGDatabases(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, cluster *enterprisev4.PostgresCluster) ([]string, error) {
	logger := logging.FromContext(ctx)
	var adopted []string
	for _, dbSpec := range postgresDB.Spec.Databases {
		cnpgDBName := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)
		reAdopted := false
		cnpgDB := &cnpgv1.Database{
			ObjectMeta: metav1.ObjectMeta{Name: cnpgDBName, Namespace: postgresDB.Namespace},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, c, cnpgDB, func() error {
			cnpgDB.Spec = buildCNPGDatabaseSpec(cluster.Status.ProvisionerRef.Name, dbSpec, reconcileExtensions(dbSpec.Extensions, cnpgDB.Spec.Extensions))
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
			logger.InfoContext(ctx, "CNPG Database re-adopted", "name", cnpgDBName)
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

func persistStatus(ctx context.Context, c client.Client, metrics ports.Recorder, db *enterprisev4.PostgresDatabase, wasReadyAtReconcileStart bool, conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases,
) error {
	before := db.Status.DeepCopy()
	applyStatus(db, conditionType, conditionStatus, reason, message, phase)
	beginReadinessCycle(db, before, wasReadyAtReconcileStart, conditionStatus, phase)
	if equality.Semantic.DeepEqual(*before, db.Status) {
		return nil
	}
	if metrics != nil {
		metrics.IncStatusTransition(ports.ControllerDatabase, string(conditionType), string(conditionStatus), string(reason))
	}
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

// beginReadinessCycle persists the start of a single time-to-Ready cycle. Initial
// provisioning starts at creation. Later cycles start for a new generation or a
// real readiness blocker. Routine successful Provisioning updates are written
// on every reconcile and do not start a cycle.
func beginReadinessCycle(db *enterprisev4.PostgresDatabase, before *enterprisev4.PostgresDatabaseStatus, wasReadyAtReconcileStart bool, conditionStatus metav1.ConditionStatus, phase reconcileDBPhases) {
	if phase == readyDBPhase || db.Status.LastTransitionTime != nil {
		return
	}

	if before.Phase == nil && before.ObservedGeneration == nil {
		lastTransitionTime := db.CreationTimestamp
		db.Status.LastTransitionTime = &lastTransitionTime
		return
	}

	wasReady := before.Phase != nil && *before.Phase == string(readyDBPhase)
	generationChanged := before.ObservedGeneration != nil && *before.ObservedGeneration != db.Generation
	readinessBlocked := conditionStatus == metav1.ConditionFalse || phase == pendingDBPhase || phase == failedDBPhase
	if generationChanged || ((wasReady || wasReadyAtReconcileStart) && readinessBlocked) {
		lastTransitionTime := metav1.Now()
		db.Status.LastTransitionTime = &lastTransitionTime
	}
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
	logger := logging.FromContext(ctx)
	c := rc.Client
	plan := buildDeletionPlan(postgresDB.Spec.Databases)
	if err := orphanRetainedResources(ctx, c, postgresDB, plan.retained); err != nil {
		return err
	}
	if err := deleteRemovedResources(ctx, c, postgresDB, plan.deleted); err != nil {
		return err
	}
	if err := cleanupManagedRoles(ctx, rc, postgresDB, plan); err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(postgresDB, postgresDatabaseFinalizerName)
	if err := c.Update(ctx, postgresDB); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("removing finalizer: %w", err)
	}
	rc.emitNormal(postgresDB, EventCleanupComplete, fmt.Sprintf("cleanup complete for PostgresDatabase %s (%d retained, %d deleted)", postgresDB.Name, len(plan.retained), len(plan.deleted)))
	logger.InfoContext(ctx, "cleanup completed", "retained", len(plan.retained), "deleted", len(plan.deleted))
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

// cleanupManagedRoles publishes drop intent and retains the finalizer until the cluster stops owning deleted roles.
func cleanupManagedRoles(ctx context.Context, rc *ReconcileContext, postgresDB *enterprisev4.PostgresDatabase, plan deletionPlan) error {
	c := rc.Client
	logger := logging.FromContext(ctx)
	if len(plan.deleted) == 0 {
		postgresDB.Status.Databases = nil
		postgresDB.Status.ObservedGeneration = &postgresDB.Generation
		return c.Status().Update(ctx, postgresDB)
	}
	cluster := &enterprisev4.PostgresCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("getting PostgresCluster for role cleanup: %w", err)
		}
		logger.InfoContext(ctx, "PostgresCluster already deleted, skipping managed roles cleanup")
		return nil
	}
	if cluster.GetDeletionTimestamp() != nil {
		logger.InfoContext(ctx, "PostgresCluster is deleting, skipping managed roles cleanup")
		return nil
	}

	stillOwned := rolesStillOwnedBySelf(postgresDB, cluster.Status.ManagedRolesStatus, plan.deleted)
	priorRolesCond := meta.FindStatusCondition(postgresDB.Status.Conditions, string(rolesReady))

	postgresDB.Status.Databases = populateDatabaseStatusForDefinitions(postgresDB, plan.deleted, false, rolesAbsent)
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation
	if stillOwned {
		reason := reasonRoleCleanupWaiting
		message := "Waiting for PostgresCluster to drop managed roles"
		if roleCleanupTimedOut(postgresDB) {
			reason = reasonRoleCleanupBlocked
			message = fmt.Sprintf("Managed role cleanup is still pending after %s; retaining finalizer to avoid leaking roles", roleCleanupTimeout)
			logger.WarnContext(ctx, "managed role cleanup timed out; retaining finalizer", "timeout", roleCleanupTimeout.String())
		}
		deleting := string(deletingDBPhase)
		postgresDB.Status.Phase = &deleting
		meta.SetStatusCondition(&postgresDB.Status.Conditions, metav1.Condition{
			Type:               string(rolesReady),
			Status:             metav1.ConditionFalse,
			Reason:             string(reason),
			Message:            message,
			ObservedGeneration: postgresDB.Generation,
		})
		if priorRolesCond == nil || priorRolesCond.Reason != string(reason) {
			rc.emitWarning(postgresDB, EventRoleCleanupBlocked, fmt.Sprintf("PostgresDatabase %s deletion is %s", postgresDB.Name, message))
		}
	}
	if err := c.Status().Update(ctx, postgresDB); err != nil {
		return err
	}
	if stillOwned {
		return errRoleCleanupPending
	}
	return nil
}

func orphanCNPGDatabases(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := logging.FromContext(ctx)
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
		logger.InfoContext(ctx, "CNPG Database orphaned", "name", name)
	}
	return nil
}

func orphanConfigMaps(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := logging.FromContext(ctx)
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
		logger.InfoContext(ctx, "ConfigMap orphaned", "name", name)
	}
	return nil
}

func orphanSecrets(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := logging.FromContext(ctx)
	for _, dbSpec := range databases {
		// if external secret is configured, skip
		if dbSpec.PasswordConfig != nil {
			continue
		}
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
			logger.InfoContext(ctx, "secret orphaned", "name", name)
		}
	}
	return nil
}

func deleteCNPGDatabases(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := logging.FromContext(ctx)
	for _, dbSpec := range databases {
		name := cnpgDatabaseName(postgresDB.Name, dbSpec.Name)
		db := &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgresDB.Namespace}}
		if err := c.Delete(ctx, db); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("deleting CNPG Database %s: %w", name, err)
		}
		logger.InfoContext(ctx, "CNPG Database deleted", "name", name)
	}
	return nil
}

func deleteConfigMaps(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := logging.FromContext(ctx)
	for _, dbSpec := range databases {
		name := configMapName(postgresDB.Name, dbSpec.Name)
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgresDB.Namespace}}
		if err := c.Delete(ctx, cm); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("deleting ConfigMap %s: %w", name, err)
		}
		logger.InfoContext(ctx, "ConfigMap deleted", "name", name)
	}
	return nil
}

func deleteSecrets(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, databases []enterprisev4.DatabaseDefinition) error {
	logger := logging.FromContext(ctx)
	for _, dbSpec := range databases {
		// Do not delete externally managed Secrets; they may have other consumers.
		if dbSpec.PasswordConfig != nil {
			continue
		}
		for _, role := range []string{secretRoleAdmin, secretRoleRW} {
			name := roleSecretName(postgresDB.Name, dbSpec.Name, role)
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgresDB.Namespace}}
			if err := c.Delete(ctx, secret); err != nil {
				if errors.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("deleting Secret %s: %w", name, err)
			}
			logger.InfoContext(ctx, "secret deleted", "name", name)
		}
	}
	return nil
}

func roleCleanupTimedOut(postgresDB *enterprisev4.PostgresDatabase) bool {
	return postgresDB.DeletionTimestamp != nil && time.Since(postgresDB.DeletionTimestamp.Time) > roleCleanupTimeout
}

func rolesStillOwnedBySelf(postgresDB *enterprisev4.PostgresDatabase, status *enterprisev4.ManagedRolesStatus, databases []enterprisev4.DatabaseDefinition) bool {
	if status == nil {
		return true
	}
	self := enterprisev4.RoleOwnerReference{Name: postgresDB.Name, UID: string(postgresDB.UID)}
	for _, dbSpec := range databases {
		for _, role := range []string{adminRoleName(dbSpec.Name), rwRoleName(dbSpec.Name)} {
			if owner, ok := status.RoleOwners[role]; ok && sameRoleOwner(owner, self) {
				return true
			}
		}
	}
	return false
}

func resolveSecretNames(postgresDBName string, dbSpec enterprisev4.DatabaseDefinition) (adminSecretName string, rwSecretName string) {
	rwSecretName = ""
	adminSecretName = ""

	if dbSpec.PasswordConfig != nil {
		rwSecretName = dbSpec.PasswordConfig.ExternalRWSecretRef.Name
		adminSecretName = dbSpec.PasswordConfig.ExternalAdminSecretRef.Name
	} else {
		rwSecretName = roleSecretName(postgresDBName, dbSpec.Name, secretRoleRW)
		adminSecretName = roleSecretName(postgresDBName, dbSpec.Name, secretRoleAdmin)
	}
	return adminSecretName, rwSecretName
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
	if annotations := obj.GetAnnotations(); annotations != nil {
		delete(annotations, annotationRetainedFrom)
		obj.SetAnnotations(annotations)
	}
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
		adminSecretName, rwSecretName := resolveSecretNames(postgresDB.Name, dbSpec)

		adminErr := reconcileRoleSecret(ctx, c,
			scheme, postgresDB, adminRoleName(dbSpec.Name),
			adminSecretName,
			missingPolicy, dbSpec)
		rwErr := reconcileRoleSecret(ctx, c,
			scheme, postgresDB, rwRoleName(dbSpec.Name),
			rwSecretName,
			missingPolicy, dbSpec)
		if err := stderrors.Join(adminErr, rwErr); err != nil {
			return err
		}
	}
	return nil
}

func reconcileRoleSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, roleName, secretName string, missingPolicy secretMissingPolicy, dbSpec enterprisev4.DatabaseDefinition) error {
	if dbSpec.PasswordConfig != nil {
		return ensureExternalSecret(ctx, c, postgresDB, secretName)
	} else {
		if missingPolicy == reportSecretDriftIfMissing {
			return ensureProvisionedSecret(ctx, c, scheme, postgresDB, roleName, secretName)
		}
		return ensureSecret(ctx, c, scheme, postgresDB, roleName, secretName)
	}
}

func ensureExternalSecret(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, secretName string) error {
	// generic safety for this codeblock, as strict safety + verbose information
	// is meant to be provided by kubebuilder validation (which cant be tested here)
	if secretName == "" {
		return secretReconcileError{
			message: "validate external secret refs, empty ref name occured",
			reason:  reasonExternalSecretInvalid,
		}
	}

	secret, err := getSecret(ctx, c, postgresDB.Namespace, secretName)
	if secret == nil && err == nil {
		return secretReconcileError{
			message: fmt.Sprintf("external secret \"%s\" is missing", secretName),
			reason:  reasonExternalSecretMissing,
		}
	}
	if err != nil {
		return err
	}

	return ValidateExternalDatabaseSecret(secret, secretName)
}

func ValidateExternalDatabaseSecret(secret *corev1.Secret, secretName string) error {
	if secret.Data == nil {
		return secretReconcileError{
			message: fmt.Sprintf("external secret \"%s\" is missing data", secretName),
			reason:  reasonExternalSecretMissingData,
		}
	}

	if secret.Data[secretKeyPassword] == nil ||
		secret.Data[secretKeyUsername] == nil ||
		len(secret.Data[secretKeyPassword]) == 0 ||
		len(secret.Data[secretKeyUsername]) == 0 {
		return secretReconcileError{
			message: fmt.Sprintf("external secret \"%s\" is missing required keys", secretName),
			reason:  reasonExternalSecretMissingKeys,
		}
	}

	if secret.Labels[labelCNPGReload] != "true" {
		return secretReconcileError{
			message: fmt.Sprintf("external secret %q is missing the %s=\"true\" label", secretName, labelCNPGReload),
			reason:  reasonExternalSecretMissingLabel,
		}
	}

	return nil
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
		return secretReconcileError{
			message: fmt.Sprintf("Managed Secret %s is missing for previously provisioned role %s", secretName, roleName),
			reason:  reasonSecretsDriftDetected,
		}
	}
	return reconcileExistingSecret(ctx, c, scheme, postgresDB, secretName, secret)
}

// reconcileExistingSecret only reconciles ownership — it never rewrites secret data.
// Passwords must not be regenerated for existing credentials; CNPG and consumers hold live references.
func reconcileExistingSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, secretName string, secret *corev1.Secret) error {
	logger := logging.FromContext(ctx)
	switch {
	case secret.Annotations[annotationRetainedFrom] == postgresDB.Name:
		if err := adoptResource(ctx, c, scheme, postgresDB, secret); err != nil {
			return err
		}
		logger.InfoContext(ctx, "secret re-adopted", "name", secretName)
		return nil
	case metav1.IsControlledBy(secret, postgresDB):
		return nil
	case metav1.GetControllerOf(secret) == nil:
		if err := adoptResource(ctx, c, scheme, postgresDB, secret); err != nil {
			return err
		}
		logger.InfoContext(ctx, "secret adopted", "name", secretName)
		return nil
	default:
		owner := metav1.GetControllerOf(secret)
		return secretReconcileError{
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
	logging.FromContext(ctx).InfoContext(ctx, "role secret created", "name", secretName)
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

func buildCNPGDatabaseSpec(clusterName string, dbSpec enterprisev4.DatabaseDefinition, extensions []cnpgv1.ExtensionSpec) cnpgv1.DatabaseSpec {
	reclaimPolicy := cnpgv1.DatabaseReclaimDelete
	if dbSpec.DeletionPolicy == deletionPolicyRetain {
		reclaimPolicy = cnpgv1.DatabaseReclaimRetain
	}
	return cnpgv1.DatabaseSpec{
		Name:          dbSpec.Name,
		Owner:         adminRoleName(dbSpec.Name),
		ClusterRef:    corev1.LocalObjectReference{Name: clusterName},
		ReclaimPolicy: reclaimPolicy,
		Extensions:    extensions,
	}
}

// reconcileExtensions produces the final extension list for a CNPG Database spec.
// Desired extensions are marked present; extensions previously declared but now removed
// are carried forward as absent so CNPG issues DROP EXTENSION.
func reconcileExtensions(desired []string, existing []cnpgv1.ExtensionSpec) []cnpgv1.ExtensionSpec {
	desiredSet := make(map[string]struct{}, len(desired))
	result := make([]cnpgv1.ExtensionSpec, 0, len(desired))
	for _, name := range desired {
		desiredSet[name] = struct{}{}
		result = append(result, cnpgv1.ExtensionSpec{
			DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: name, Ensure: cnpgv1.EnsurePresent},
		})
	}
	for _, ext := range existing {
		if _, ok := desiredSet[ext.Name]; !ok {
			result = append(result, cnpgv1.ExtensionSpec{
				DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: ext.Name, Ensure: cnpgv1.EnsureAbsent},
			})
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func reconcileRoleConfigMaps(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *enterprisev4.PostgresDatabase, endpoints clusterEndpoints) error {
	logger := logging.FromContext(ctx)
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
			data, _, err := buildDatabaseConfigMapData(dbSpec.Name, endpoints)
			if err != nil {
				return fmt.Errorf("building ConfigMap data for database %s: %w", dbSpec.Name, err)
			}
			cm.Data = data
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
			logger.InfoContext(ctx, "ConfigMap re-adopted", "name", cmName)
		}
	}
	return nil
}

func persistDatabaseInfos(ctx context.Context, c client.Client, postgresDB *enterprisev4.PostgresDatabase, ready bool, exists bool) error {
	before := postgresDB.Status.DeepCopy()
	postgresDB.Status.Databases = populateDatabaseStatus(postgresDB, ready, exists)
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation
	if equality.Semantic.DeepEqual(*before, postgresDB.Status) {
		return nil
	}
	return c.Status().Update(ctx, postgresDB)
}

func populateDatabaseStatus(postgresDB *enterprisev4.PostgresDatabase, flags ...bool) []enterprisev4.DatabaseInfo {
	ready := true
	exists := true
	includeRoles := false
	if len(flags) > 0 {
		ready = flags[0]
		includeRoles = true
	}
	if len(flags) > 1 {
		exists = flags[1]
	}
	return populateDatabaseStatusForDefinitions(postgresDB, postgresDB.Spec.Databases, ready, exists, includeRoles)
}

func populateDatabaseStatusForDefinitions(postgresDB *enterprisev4.PostgresDatabase, definitions []enterprisev4.DatabaseDefinition, ready bool, exists bool, includeRoles ...bool) []enterprisev4.DatabaseInfo {
	publishRoles := true
	if len(includeRoles) > 0 {
		publishRoles = includeRoles[0]
	}
	existingReady := make(map[string]bool, len(postgresDB.Status.Databases))
	for _, existing := range postgresDB.Status.Databases {
		if existing.Ready || len(existing.Roles) == 0 {
			existingReady[existing.Name] = true
		}
	}
	databases := make([]enterprisev4.DatabaseInfo, 0, len(definitions))
	for _, dbSpec := range definitions {
		adminSecretName, rwSecretName := resolveSecretNames(postgresDB.Name, dbSpec)
		info := enterprisev4.DatabaseInfo{
			Name:               dbSpec.Name,
			Ready:              ready || (exists && existingReady[dbSpec.Name]),
			AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: adminSecretName}, Key: secretKeyPassword},
			RWUserSecretRef:    &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: rwSecretName}, Key: secretKeyPassword},
			ConfigMapRef:       &corev1.LocalObjectReference{Name: configMapName(postgresDB.Name, dbSpec.Name)},
		}
		if publishRoles {
			info.Roles = []enterprisev4.DatabaseRoleInfo{
				{Name: adminRoleName(dbSpec.Name), SecretRef: &corev1.LocalObjectReference{Name: adminSecretName}, Exists: exists},
				{Name: rwRoleName(dbSpec.Name), SecretRef: &corev1.LocalObjectReference{Name: rwSecretName}, Exists: exists},
			}
		}
		databases = append(databases, info)
	}
	return databases
}

func hasNewDatabases(postgresDB *enterprisev4.PostgresDatabase) bool {
	existing := make(map[string]bool, len(postgresDB.Status.Databases))
	for _, dbInfo := range postgresDB.Status.Databases {
		// Entries with no role status are treated as provisioned database status.
		if dbInfo.Ready || len(dbInfo.Roles) == 0 {
			existing[dbInfo.Name] = true
		}
	}
	for _, dbSpec := range postgresDB.Spec.Databases {
		if !existing[dbSpec.Name] {
			return true
		}
	}
	return false
}

// Naming helpers — single source of truth shared by creation and status wiring.
func adminRoleName(dbName string) string { return dbName + "_admin" }
func rwRoleName(dbName string) string    { return dbName + "_rw" }
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

func upsertFailureState(db *enterprisev4.PostgresDatabase, failureType string) {
	db.Status.ReconcileFailureType = failureType
}

func terminalFailureType(err error) (string, bool) {
	if stderrors.Is(err, ErrTerminal) {
		return reconcileFailurePrivileges, true
	}
	return "", false
}

func hasCurrentReconcileFailure(db *enterprisev4.PostgresDatabase) bool {
	if db.Status.Phase == nil || *db.Status.Phase != string(failedDBPhase) {
		return false
	}
	if db.Status.ObservedGeneration == nil || *db.Status.ObservedGeneration != db.Generation {
		return false
	}

	switch db.Status.ReconcileFailureType {
	case reconcileFailurePrivileges:
		condition := meta.FindStatusCondition(db.Status.Conditions, string(privilegesReady))
		return condition != nil &&
			condition.Status == metav1.ConditionFalse &&
			condition.Reason == string(reasonPrivilegesTerminalFailure) &&
			condition.ObservedGeneration == db.Generation
	default:
		return false
	}
}
