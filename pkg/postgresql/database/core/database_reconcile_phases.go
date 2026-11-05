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
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	dbmetrics "github.com/splunk/splunk-operator/pkg/postgresql/database/core/custom_metrics"
	dbclusterinfo "github.com/splunk/splunk-operator/pkg/postgresql/database/ports/clusterinfo"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// databaseReconciliation holds the observation shared by the ordered phases
// of a single reconcile. Each phase returns stop=true when it has handled the
// resource, including waits and terminal conditions.
type databaseReconciliation struct {
	rc                                    *ReconcileContext
	db                                    *platformv1alpha1.PostgresDatabase
	newDBRepo                             ports.NewDBRepoFunc
	wasReady                              bool
	retryAfterStaleReconcileFailure       bool
	previouslyProvisionedDatabases        map[string]struct{}
	clusterFacts                          dbclusterinfo.ResolvedClusterFacts
	providerClusterName                   string
	databaseEnvironmentName               string
	cnpgCluster                           *cnpgv1.Cluster
	endpoints                             clusterEndpoints
	provisioningResult                    databaseProvisioningResult
	awaitingPrivilegeBootstrap            bool
	finalizeClosedDatabaseAfterPrivileges bool
	privilegesMsg                         string
	databasesToGrant                      []platformv1alpha1.DatabaseDefinition
	lastTransitionTime                    time.Time
	completedReadinessCycle               bool
}

func (r *databaseReconciliation) updateStatus(ctx context.Context, conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
	return persistStatus(ctx, r.rc.Client, r.rc.Metrics, r.db, r.wasReady, conditionType, conditionStatus, reason, message, phase)
}

func (r *databaseReconciliation) initialize(ctx context.Context) (ctrl.Result, error, bool) {
	postgresDB := r.db
	c := r.rc.Client
	logger := logging.FromContext(ctx)
	// Finalizer: cleanup on deletion, register on creation.
	if postgresDB.GetDeletionTimestamp() != nil {
		return r.deleteDatabase(ctx)
	}

	finalizerAdded := false
	// Add finalizer if not present.
	if !controllerutil.ContainsFinalizer(postgresDB, postgresDatabaseFinalizerName) {
		controllerutil.AddFinalizer(postgresDB, postgresDatabaseFinalizerName)
		if err := c.Update(ctx, postgresDB); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictFinalizer, "adding finalizer"); ok {
				return result, conflictErr, true
			}
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err), true
		}
		logger.InfoContext(ctx, "finalizer added successfully")
		finalizerAdded = true
	}

	// A status-only update does not re-enqueue this resource, so initialize the
	// observable phase and explicitly schedule the normal reconciliation path.
	initialized, err := initializePendingPhase(ctx, c, postgresDB)
	if err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictInitialPhase, "initializing Pending phase"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, fmt.Errorf("failed to initialize PostgresDatabase phase: %w", err), true
	}
	if initialized {
		logger.InfoContext(ctx, "initialized PostgresDatabase phase", "phase", pendingDBPhase)
		return ctrl.Result{Requeue: true}, nil, true
	}
	if finalizerAdded {
		return ctrl.Result{}, nil, true
	}

	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) reconcileEndpoints(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	// Endpoint intents must run even while the parent is Configuring or this
	// database has an unrelated terminal reconcile failure. In particular, the
	// ordinary Ready gate and ConfigMap writer must never publish green merely
	// because provisionerRef has switched.
	if handled, result, err := reconcileEndpointProtocol(ctx, rc, postgresDB); handled {
		return result, err, true
	}

	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) publishMetricsParticipation(ctx context.Context) (ctrl.Result, error, bool) {
	postgresDB := r.db
	c := r.rc.Client
	_, err := persistCustomMetricsPublication(ctx, c, postgresDB)
	if err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictCustomMetricsStatus, "publishing custom metrics participation"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, fmt.Errorf("publishing custom metrics participation: %w", err), true
	}

	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) recoverTerminalFailure(ctx context.Context) (ctrl.Result, error, bool) {
	postgresDB := r.db
	currentReconcileFailure := hasCurrentReconcileFailure(postgresDB)

	// A terminal failure only blocks the generation that observed it. A stale
	// marker is kept until recovery succeeds because earlier status updates
	// persist the whole status object and may return before the failed phase.
	retryAfterStaleReconcileFailure := postgresDB.Status.ReconcileFailureType != "" && !currentReconcileFailure
	if currentReconcileFailure {
		return r.restoreClosedDatabaseState(ctx)
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

	r.retryAfterStaleReconcileFailure = retryAfterStaleReconcileFailure
	r.previouslyProvisionedDatabases = previouslyProvisionedDatabases
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) observeProvider(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	wasReady := r.wasReady
	updateStatus := func(conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
		return r.updateStatus(ctx, conditionType, conditionStatus, reason, message, phase)
	}
	// Phase: ClusterValidation
	clusterFacts, result, err, stop := observeClusterReadiness(ctx, rc, postgresDB, wasReady, updateStatus)
	if stop {
		return result, err, true
	}
	if clusterFacts.Cluster == nil {
		return ctrl.Result{}, fmt.Errorf("cluster readiness gate converged without cluster identity facts"), true
	}
	authoritativeEnvironment := clusterFacts.Cluster.Authoritative
	providerClusterName := authoritativeEnvironment.Identity.Name
	providerClusterNamespace := authoritativeEnvironment.Identity.Namespace
	providerClusterUID := authoritativeEnvironment.Identity.UID
	databaseEnvironmentName := cnpgDatabaseEnvironmentName(authoritativeEnvironment)

	cnpgCluster := &cnpgv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      providerClusterName,
		Namespace: providerClusterNamespace,
	}, cnpgCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to fetch CNPG Cluster %s: %w", providerClusterName, err), true
	}
	if providerClusterUID != "" && cnpgCluster.UID != providerClusterUID {
		return ctrl.Result{}, fmt.Errorf("CNPG Cluster %s/%s UID %q does not match resolved provider identity UID %q", providerClusterNamespace, providerClusterName, cnpgCluster.UID, providerClusterUID), true
	}

	r.clusterFacts = clusterFacts
	r.providerClusterName = providerClusterName
	r.databaseEnvironmentName = databaseEnvironmentName
	r.cnpgCluster = cnpgCluster
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) reconcileCredentials(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	databaseEnvironmentName := r.databaseEnvironmentName
	previouslyProvisionedDatabases := r.previouslyProvisionedDatabases
	// Phase: CredentialProvisioning — secrets must exist before roles are patched.
	// CNPG rejects a PasswordSecretRef pointing at a missing secret.
	if err := reconcileRoleSecrets(ctx, c, rc.Scheme, postgresDB, previouslyProvisionedDatabases); err != nil {
		return r.handleCredentialError(ctx, err)
	}
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, secretsReady, EventSecretsReady, fmt.Sprintf("All secrets provisioned for %d databases", len(postgresDB.Spec.Databases)))
	if err := r.updateStatus(ctx, secretsReady, metav1.ConditionTrue, reasonSecretsCreated,
		fmt.Sprintf("All secrets provisioned for %d databases", len(postgresDB.Spec.Databases)), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictSecretsStatus, "persisting secrets ready status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	// Publish credential-ready roles for cluster-side role reconciliation.
	if err := persistDatabaseInfos(ctx, c, postgresDB, false, rolesExist, databaseEnvironmentName); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictSecretsStatus, "publishing credential-ready role status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}

	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) reconcileConnectionMetadata(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	clusterFacts := r.clusterFacts
	cnpgCluster := r.cnpgCluster
	// Phase: ConnectionMetadata — ConfigMaps carry connection info consumers need as soon
	// as databases are ready, so they are created alongside secrets.
	endpoints, err := resolveClusterEndpoints(clusterFacts.ConnectionPoolerStatus, cnpgCluster, postgresDB.Namespace)
	if err != nil {
		return r.handleEndpointResolutionFailure(ctx, err)
	}
	if err := reconcileRoleConfigMaps(ctx, c, rc.Scheme, postgresDB, endpoints); err != nil {
		return r.handleConfigMapFailure(ctx, err)
	}
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, configMapsReady, EventConfigMapsReady, fmt.Sprintf("All ConfigMaps provisioned for %d databases", len(postgresDB.Spec.Databases)))
	if err := r.updateStatus(ctx, configMapsReady, metav1.ConditionTrue, reasonConfigMapsCreated,
		fmt.Sprintf("All ConfigMaps provisioned for %d databases", len(postgresDB.Spec.Databases)), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictConfigMapsStatus, "persisting configmaps ready status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}

	r.endpoints = endpoints
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) observeRoles(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	clusterFacts := r.clusterFacts
	switch gate := evaluateRoleGate(postgresDB, clusterFacts.ManagedRolesStatus); gate.State {
	case roleGateConflict:
		return r.waitForRoleConflict(ctx, gate)
	case roleGateFailed:
		return r.waitForRoleFailed(ctx, gate)
	case roleGatePending:
		return r.waitForRolePending(ctx, gate)
	}
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, rolesReady, EventRolesReady, fmt.Sprintf("Roles reconciled: %d active", len(getDesiredRoles(postgresDB))))
	if err := r.updateStatus(ctx, rolesReady, metav1.ConditionTrue, reasonRolesAvailable,
		fmt.Sprintf("Roles reconciled: %d active", len(getDesiredRoles(postgresDB))), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRolesStatus, "persisting roles ready status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}

	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) reconcileDatabases(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	providerClusterName := r.providerClusterName
	databaseEnvironmentName := r.databaseEnvironmentName
	// Phase: DatabaseProvisioning
	provisioningResult, err := reconcileDatabaseProvisioning(ctx, rc.DatabaseProvisioner, postgresDB, providerClusterName, databaseEnvironmentName)
	if err != nil {
		return r.handleDatabaseProvisioningError(ctx, err)
	}
	if len(provisioningResult.adopted) > 0 {
		rc.emitNormal(postgresDB, EventResourcesAdopted, fmt.Sprintf("Adopted retained databases: %v", provisioningResult.adopted))
	}

	if len(provisioningResult.notReady) > 0 {
		return r.waitForDatabases(ctx, provisioningResult)
	}
	awaitingPrivilegeBootstrap := len(provisioningResult.bootstrap) > 0
	finalizeClosedDatabaseAfterPrivileges := requiresClosedDatabaseFinalization(provisioningResult.bootstrap)
	if result, err, stop := r.observeDatabaseBootstrap(ctx, provisioningResult, awaitingPrivilegeBootstrap, finalizeClosedDatabaseAfterPrivileges); stop {
		return result, err, true
	}

	r.provisioningResult = provisioningResult
	r.awaitingPrivilegeBootstrap = awaitingPrivilegeBootstrap
	r.finalizeClosedDatabaseAfterPrivileges = finalizeClosedDatabaseAfterPrivileges
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) reconcilePrivileges(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	newDBRepo := r.newDBRepo
	c := r.rc.Client
	endpoints := r.endpoints
	clusterFacts := r.clusterFacts
	retryAfterStaleReconcileFailure := r.retryAfterStaleReconcileFailure
	provisioningResult := r.provisioningResult
	// Phase: RWRolePrivileges
	// Skipped when no new databases are detected — ALTER DEFAULT PRIVILEGES covers tables
	// added by migrations on existing databases. Bootstrap grants target only new databases;
	// stale terminal recovery retains the previous all-database retry when no database is new.
	databaseCount := len(postgresDB.Spec.Databases)
	privilegesMsg := fmt.Sprintf("RW role privileges already current for all %d databases", databaseCount)
	databasesToGrant := provisioningResult.bootstrap
	if len(databasesToGrant) > 0 || retryAfterStaleReconcileFailure {
		if len(databasesToGrant) == 0 {
			databasesToGrant = postgresDB.Spec.Databases
		}
		// Use the readiness snapshot of the cluster-owned SecretKeySelector (name + key).
		// The cluster created this reference when it provisioned the secret, which avoids
		// depending on CNPG's spec field and makes the key explicit.
		if clusterFacts.SuperUserSecretRef == nil {
			return ctrl.Result{}, fmt.Errorf("postgresCluster %s has no superuser secret ref in status", clusterFacts.Name), true
		}
		superSecretRef := clusterFacts.SuperUserSecretRef
		superSecret := &corev1.Secret{}
		if err := c.Get(ctx, types.NamespacedName{
			Name:      superSecretRef.Name,
			Namespace: postgresDB.Namespace,
		}, superSecret); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to fetch superuser secret %s: %w", superSecretRef.Name, err), true
		}
		pw, ok := superSecret.Data[superSecretRef.Key]
		if !ok {
			return ctrl.Result{}, fmt.Errorf("superuser secret %s missing %q key", superSecretRef.Name, superSecretRef.Key), true
		}

		privilegeTargets := privilegeTargetsForDatabases(databasesToGrant)

		if err := reconcileRWRolePrivileges(ctx, endpoints.RWHost, string(pw), privilegeTargets, newDBRepo); err != nil {
			return r.handlePrivilegesError(ctx, err)
		}
		if retryAfterStaleReconcileFailure {
			postgresDB.Status.ReconcileFailureType = ""
		}
		privilegesMsg = fmt.Sprintf("RW role privileges granted, count: %d", len(privilegeTargets))
		rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, privilegesReady, EventPrivilegesReady, privilegesMsg)
	}
	r.privilegesMsg = privilegesMsg
	r.databasesToGrant = databasesToGrant
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) completeBootstrap(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	awaitingPrivilegeBootstrap := r.awaitingPrivilegeBootstrap
	databasesToGrant := r.databasesToGrant
	databaseEnvironmentName := r.databaseEnvironmentName
	privilegesMsg := r.privilegesMsg
	finalizeClosedDatabaseAfterPrivileges := r.finalizeClosedDatabaseAfterPrivileges
	provisioningResult := r.provisioningResult
	applyStatus(postgresDB, privilegesReady, metav1.ConditionTrue, reasonPrivilegesGranted, privilegesMsg, readyDBPhase)
	if finalizeClosedDatabaseAfterPrivileges {
		if err := persistDatabaseBootstrapCompletion(
			ctx, c, postgresDB, databasesToGrant, provisioningResult.databaseUIDs, databaseEnvironmentName,
		); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictPrivilegesStatus, "persisting database privilege bootstrap completion"); ok {
				return result, conflictErr, true
			}
			return ctrl.Result{}, fmt.Errorf("persisting database privilege bootstrap completion: %w", err), true
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil, true
	}
	recordDatabaseBootstrapCompletion(postgresDB, databasesToGrant, provisioningResult.databaseUIDs, databaseEnvironmentName)
	if awaitingPrivilegeBootstrap {
		message := fmt.Sprintf(msgFmtDatabasesReady, len(postgresDB.Spec.Databases))
		rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, databasesReady, EventDatabasesReady, message)
		applyStatus(postgresDB, databasesReady, metav1.ConditionTrue, reasonDatabasesAvailable, message, provisioningDBPhase)
	}
	lastTransitionTime, completedReadinessCycle := completeReadinessCycle(postgresDB)

	r.lastTransitionTime = lastTransitionTime
	r.completedReadinessCycle = completedReadinessCycle
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) observeMetrics(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	databaseEnvironmentName := r.databaseEnvironmentName
	clusterFacts := r.clusterFacts
	metricsOutcome, err := reconcileCustomMetricsGate(ctx, rc, postgresDB, clusterFacts.CustomMetricsStatus)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling custom-metrics acknowledgement: %w", err), true
	}
	switch metricsOutcome.State {
	case dbmetrics.GateFailed:
		rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, customMetricsReady, EventCustomMetricsFailed, metricsOutcome.Message)
		if err := persistCustomMetricsStatus(ctx, rc, postgresDB, metricsOutcome, metav1.ConditionFalse, failedDBPhase, databaseEnvironmentName); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictCustomMetricsStatus, "persisting custom metrics failure"); ok {
				return result, conflictErr, true
			}
			return ctrl.Result{}, err, true
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil, true
	case dbmetrics.GatePending:
		if err := persistCustomMetricsStatus(ctx, rc, postgresDB, metricsOutcome, metav1.ConditionUnknown, provisioningDBPhase, databaseEnvironmentName); err != nil {
			if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictCustomMetricsStatus, "persisting custom metrics pending status"); ok {
				return result, conflictErr, true
			}
			return ctrl.Result{}, err, true
		}
		return ctrl.Result{RequeueAfter: retryDelay}, nil, true
	default:
		rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, customMetricsReady, EventCustomMetricsReady, metricsOutcome.Message)
		applyCustomMetricsStatus(rc, postgresDB, metricsOutcome, metav1.ConditionTrue, readyDBPhase, databaseEnvironmentName)
	}

	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) persistReady(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	logger := logging.FromContext(ctx)
	wasReady := r.wasReady
	completedReadinessCycle := r.completedReadinessCycle
	databaseEnvironmentName := r.databaseEnvironmentName
	lastTransitionTime := r.lastTransitionTime
	if !wasReady {
		rc.emitNormal(postgresDB, EventPostgresDatabaseReady, fmt.Sprintf("PostgresDatabase %s is ready", postgresDB.Name))
	}
	postgresDB.Status.Databases = populateDatabaseStatusForEnvironment(postgresDB, databaseEnvironmentName, true, rolesExist)
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation

	if err := c.Status().Update(ctx, postgresDB); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictFinalStatus, "persisting final status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, fmt.Errorf("failed to persist final status: %w", err), true
	}
	if completedReadinessCycle && rc.Metrics != nil {
		rc.Metrics.ObserveProvisioningDuration(
			ports.ControllerDatabase,
			time.Since(lastTransitionTime).Seconds(),
		)
	}

	logger.DebugContext(ctx, "PostgresDatabase reconciliation completed")
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) deleteDatabase(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	if err := handleDeletion(ctx, rc, postgresDB); err != nil {
		if stderrors.Is(err, errRoleCleanupPending) {
			return ctrl.Result{RequeueAfter: retryDelay}, nil, true
		}
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDeletion, "handling deletion"); ok {
			return result, conflictErr, true
		}
		rc.emitWarning(postgresDB, EventCleanupFailed, fmt.Sprintf("cleanup failed for PostgresDatabase %s — check operator logs", postgresDB.Name))
		return ctrl.Result{}, fmt.Errorf("failed to clean up PostgresDatabase: %w", err), true
	}
	return ctrl.Result{}, nil, true
}

func (r *databaseReconciliation) restoreClosedDatabaseState(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	if !requiresClosedDatabaseFinalization(postgresDB.Spec.Databases) {
		return ctrl.Result{}, nil, true
	}
	clusterCard, err := readReferencedClusterCard(ctx, rc, postgresDB)
	if stderrors.Is(err, dbclusterinfo.ErrClusterNotFound) {
		return ctrl.Result{RequeueAfter: clusterNotFoundRetryDelay}, nil, true
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("fetching cluster while restoring requested closed database state: %w", err), true
	}
	authoritative := clusterCard.Authoritative
	if err := validateAuthoritativeProviderUID(ctx, c, authoritative); err != nil {
		return ctrl.Result{}, fmt.Errorf("validating provider while restoring requested closed database state: %w", err), true
	}
	closed, err := reconcileRequestedClosedState(ctx, rc.DatabaseProvisioner, postgresDB, authoritative.Identity.Name, cnpgDatabaseEnvironmentName(authoritative))
	if err != nil {
		return ctrl.Result{}, err, true
	}
	if !closed {
		return ctrl.Result{RequeueAfter: retryDelay}, nil, true
	}
	return ctrl.Result{}, nil, true
}

func (r *databaseReconciliation) handleCredentialError(ctx context.Context, err error) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	logger := logging.FromContext(ctx)
	if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictSecretsReconcile, "reconciling user secrets"); ok {
		return result, conflictErr, true
	}

	// Both "missing" and "invalid/drift" share one typed carrier now; branch
	// on the embedded reason. chooseSecretError prefers a missing-reason leaf
	// across the joined admin/RW errors so a missing secret (hard failure)
	// still wins over a merely invalid one, regardless of ordering.
	if secretErr := chooseSecretError(err); secretErr != nil {
		return r.handleTypedCredentialError(ctx, secretErr, err)
	}

	rc.emitWarning(postgresDB, EventRoleSecretsFailed, fmt.Sprintf("failed to reconcile user secrets for PostgresDatabase %s — check operator logs", postgresDB.Name))
	if statusErr := r.updateStatus(ctx, secretsReady, metav1.ConditionFalse, reasonSecretsCreationFailed,
		fmt.Sprintf("Failed to reconcile user secrets: %v", err), provisioningDBPhase); statusErr != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictSecretsStatus, "persisting secret failure status"); ok {
			return result, conflictErr, true
		}
		logger.ErrorContext(ctx, "failed to persist secrets status", "error", statusErr)
	}
	return ctrl.Result{}, err, true
}

func (r *databaseReconciliation) handleTypedCredentialError(ctx context.Context, secretErr *secretReconcileError, err error) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	logger := logging.FromContext(ctx)
	if secretErr.reason == reasonExternalSecretMissing {
		return r.handleMissingExternalSecret(ctx, err)
	}

	// Use err.Error() (not secretErr.message) so a combined admin+RW
	// failure surfaces both causes rather than just the first match.
	if isManagedSecretDriftReason(secretErr.reason) {
		rc.emitWarnOnConditionReasonTransition(
			postgresDB,
			postgresDB.Status.Conditions,
			secretsReady,
			secretErr.reason,
			EventRolesSecretsDriftDetected,
			err.Error(),
		)
	} else {
		rc.emitWarning(postgresDB, EventRolesSecretsDriftDetected, err.Error())
	}
	if statusErr := r.updateStatus(ctx, secretsReady, metav1.ConditionFalse, secretErr.reason,
		err.Error(), provisioningDBPhase); statusErr != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictSecretsStatus, "persisting secret drift status"); ok {
			return result, conflictErr, true
		}
		logger.ErrorContext(ctx, "failed to persist secret drift status", "error", statusErr)
	}
	return ctrl.Result{RequeueAfter: retryDelay}, nil, true
}

func (r *databaseReconciliation) handleMissingExternalSecret(ctx context.Context, err error) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	logger := logging.FromContext(ctx)
	rc.emitWarning(postgresDB, EventRoleSecretsFailed, fmt.Sprintf("external secret(s) are missing for PostgresDatabase %s — check operator logs", postgresDB.Name))
	if statusErr := r.updateStatus(ctx, secretsReady, metav1.ConditionFalse, reasonExternalSecretMissing,
		fmt.Sprintf("external secret(s) are missing: %v", err), failedDBPhase); statusErr != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictSecretsStatus, "persisting secret failure status"); ok {
			return result, conflictErr, true
		}
		logger.ErrorContext(ctx, "failed to persist secrets status", "error", statusErr)
		return ctrl.Result{}, statusErr, true
	}
	// missing external secret == terminal
	// recovery is driven by the external-Secret watch predicate.
	return ctrl.Result{}, reconcile.TerminalError(err), true
}

func (r *databaseReconciliation) handleEndpointResolutionFailure(ctx context.Context, err error) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	logger := logging.FromContext(ctx)
	if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictConfigMapsReconcile, "resolving configmap endpoints"); ok {
		return result, conflictErr, true
	}
	rc.emitWarning(postgresDB, EventAccessConfigFailed, fmt.Sprintf("failed to resolve ConfigMap endpoints for PostgresDatabase %s — check operator logs", postgresDB.Name))
	if statusErr := r.updateStatus(ctx, configMapsReady, metav1.ConditionFalse, reasonConfigMapsCreationFailed,
		fmt.Sprintf("Failed to resolve ConfigMap endpoints: %v", err), provisioningDBPhase); statusErr != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictConfigMapsStatus, "persisting configmaps endpoint failure status"); ok {
			return result, conflictErr, true
		}
		logger.ErrorContext(ctx, "failed to persist configmaps endpoint failure status", "error", statusErr)
	}
	return ctrl.Result{}, err, true
}

func (r *databaseReconciliation) handleConfigMapFailure(ctx context.Context, err error) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	logger := logging.FromContext(ctx)
	if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictConfigMapsReconcile, "reconciling configmaps"); ok {
		return result, conflictErr, true
	}
	rc.emitWarning(postgresDB, EventAccessConfigFailed, fmt.Sprintf("failed to reconcile ConfigMaps for PostgresDatabase %s — check operator logs", postgresDB.Name))
	if statusErr := r.updateStatus(ctx, configMapsReady, metav1.ConditionFalse, reasonConfigMapsCreationFailed,
		fmt.Sprintf("Failed to reconcile ConfigMaps: %v", err), provisioningDBPhase); statusErr != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictConfigMapsStatus, "persisting configmaps failure status"); ok {
			return result, conflictErr, true
		}
		logger.ErrorContext(ctx, "failed to persist configmaps status", "error", statusErr)
	}
	return ctrl.Result{}, err, true
}

func (r *databaseReconciliation) waitForRoleConflict(ctx context.Context, gate roleGateDecision) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	logger := logging.FromContext(ctx)
	conflictMsg := fmt.Sprintf(msgFmtRoleConflict, postgresDB.Name, gate.Message)
	rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, rolesReady, EventRoleConflict, conflictMsg)
	if statusErr := r.updateStatus(ctx, rolesReady, metav1.ConditionFalse, reasonRoleConflict, conflictMsg, failedDBPhase); statusErr != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictRoleConflictStatus, "persisting role conflict status"); ok {
			return result, conflictErr, true
		}
		logger.ErrorContext(ctx, "failed to persist role conflict status", "error", statusErr)
		return ctrl.Result{}, statusErr, true
	}
	if err := persistDatabaseMessages(ctx, c, postgresDB, roleGateReasons(postgresDB, gate)); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRoleConflictStatus, "persisting role conflict status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	return ctrl.Result{RequeueAfter: retryDelay}, nil, true
}

func (r *databaseReconciliation) waitForRoleFailed(ctx context.Context, gate roleGateDecision) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	logger := logging.FromContext(ctx)
	failedMsg := fmt.Sprintf(msgFmtRoleReconcileFailed, postgresDB.Name, gate.Message)
	rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, rolesReady, EventRoleReconcileFailed, failedMsg)
	if statusErr := r.updateStatus(ctx, rolesReady, metav1.ConditionFalse, reasonRoleReconcileFailed, failedMsg, failedDBPhase); statusErr != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictRolesStatus, "persisting role reconcile failed status"); ok {
			return result, conflictErr, true
		}
		logger.ErrorContext(ctx, "failed to persist role reconcile failed status", "error", statusErr)
		return ctrl.Result{}, statusErr, true
	}
	if err := persistDatabaseMessages(ctx, c, postgresDB, roleGateReasons(postgresDB, gate)); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRolesStatus, "persisting role reconcile failed status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	return ctrl.Result{RequeueAfter: retryDelay}, nil, true
}

func (r *databaseReconciliation) waitForRolePending(ctx context.Context, gate roleGateDecision) (ctrl.Result, error, bool) {
	postgresDB := r.db
	c := r.rc.Client
	if err := r.updateStatus(ctx, rolesReady, metav1.ConditionFalse, reasonWaitingForCNPG, gate.Message, provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRolesStatus, "persisting roles pending status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	if err := persistDatabaseMessages(ctx, c, postgresDB, roleGateReasons(postgresDB, gate)); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictRolesStatus, "persisting roles pending status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	return ctrl.Result{RequeueAfter: retryDelay}, nil, true
}

func (r *databaseReconciliation) handleDatabaseProvisioningError(ctx context.Context, err error) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	logger := logging.FromContext(ctx)
	if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictCNPGDatabasesReconcile, "reconciling CNPG databases"); ok {
		return result, conflictErr, true
	}
	rc.emitWarning(postgresDB, EventDatabasesReconcileFailed, fmt.Sprintf("failed to reconcile databases for PostgresDatabase %s — check operator logs", postgresDB.Name))
	if statusErr := r.updateStatus(ctx, databasesReady, metav1.ConditionFalse, reasonDatabaseReconcileFailed,
		fmt.Sprintf("Failed to reconcile databases: %v", err), failedDBPhase); statusErr != nil {
		logger.ErrorContext(ctx, "failed to persist databases status", "error", statusErr)
	}
	return ctrl.Result{}, err, true
}

func (r *databaseReconciliation) waitForDatabases(ctx context.Context, provisioningResult databaseProvisioningResult) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	rc.emitOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, databasesReady, EventDatabaseReconciliationStarted, fmt.Sprintf("Reconciling %d databases, waiting for readiness", len(postgresDB.Spec.Databases)))
	if err := r.updateStatus(ctx, databasesReady, metav1.ConditionFalse, reasonWaitingForCNPG,
		fmt.Sprintf("Waiting for databases to be ready: %v", provisioningResult.notReady), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting databases pending status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	// Persisted separately: the condition message is stable across requeues, so a
	// change confined to a per-database message would be invisible to updateStatus.
	if err := persistDatabaseMessages(ctx, c, postgresDB, provisioningResult.reasons); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting databases pending status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	return ctrl.Result{RequeueAfter: retryDelay}, nil, true
}

func (r *databaseReconciliation) waitForDatabaseBootstrap(ctx context.Context, provisioningResult databaseProvisioningResult, finalizeClosedDatabaseAfterPrivileges bool) (ctrl.Result, error, bool) {
	postgresDB := r.db
	c := r.rc.Client
	message := "Waiting for initial application privileges"
	if finalizeClosedDatabaseAfterPrivileges {
		message += " before applying the final database connection policy"
	}
	if err := r.updateStatus(ctx, databasesReady, metav1.ConditionFalse, reasonWaitingForCNPG, message, provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting database bootstrap status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	if err := persistDatabaseMessages(ctx, c, postgresDB, bootstrapDatabaseReasons(provisioningResult.bootstrap)); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting database bootstrap status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) markDatabasesReady(ctx context.Context) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	c := r.rc.Client
	rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, databasesReady, EventDatabasesReady, fmt.Sprintf(msgFmtDatabasesReady, len(postgresDB.Spec.Databases)))
	if err := r.updateStatus(ctx, databasesReady, metav1.ConditionTrue, reasonDatabasesAvailable,
		fmt.Sprintf(msgFmtDatabasesReady, len(postgresDB.Spec.Databases)), provisioningDBPhase); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting databases ready status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	// Clear now-stale per-database messages before a later phase can fail and return early.
	if err := persistDatabaseMessages(ctx, c, postgresDB, nil); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictDatabasesStatus, "persisting databases ready status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, err, true
	}
	return ctrl.Result{}, nil, false
}

func (r *databaseReconciliation) observeDatabaseBootstrap(ctx context.Context, provisioningResult databaseProvisioningResult, awaiting, finalizeClosed bool) (ctrl.Result, error, bool) {
	if awaiting {
		return r.waitForDatabaseBootstrap(ctx, provisioningResult, finalizeClosed)
	}
	return r.markDatabasesReady(ctx)
}

func (r *databaseReconciliation) handlePrivilegesError(ctx context.Context, err error) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	logger := logging.FromContext(ctx)
	if failureType, ok := terminalFailureType(err); ok {
		upsertFailureState(postgresDB, failureType)
		logger.ErrorContext(ctx, "RW role privileges grant failed terminally", "error_category", "terminal")
		return r.handleTerminalPrivilegesError(ctx, err)
	}

	msg := "Failed to grant RW role privileges. Will retry automatically; check operator logs for details."
	eventMsg := fmt.Sprintf("failed to grant RW role privileges for PostgresDatabase %s — check operator logs", postgresDB.Name)
	rc.emitWarning(postgresDB, EventPrivilegesGrantFailed, eventMsg)
	if statusErr := r.updateStatus(ctx, privilegesReady, metav1.ConditionFalse, reasonPrivilegesGrantFailed,
		msg, provisioningDBPhase); statusErr != nil {
		wrappedStatusErr := fmt.Errorf("failed to persist privileges status: %w", statusErr)
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictPrivilegesStatus,
			"persisting privileges failure status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, stderrors.Join(err, wrappedStatusErr), true
	}
	return ctrl.Result{}, err, true
}

func (r *databaseReconciliation) handleTerminalPrivilegesError(ctx context.Context, err error) (ctrl.Result, error, bool) {
	rc := r.rc
	postgresDB := r.db
	providerClusterName := r.providerClusterName
	databaseEnvironmentName := r.databaseEnvironmentName
	msg := "Failed to grant RW role privileges. Manual intervention required: " +
		"fix the PostgresDatabase spec or referenced configuration, then redeploy with a spec change."
	eventMsg := fmt.Sprintf("Failed to grant RW role privileges for PostgresDatabase %s. Manual intervention required: "+
		"fix the PostgresDatabase spec or referenced configuration, then redeploy with a spec change. "+
		"Check operator logs for details.", postgresDB.Name)
	rc.emitWarning(postgresDB, EventPrivilegesGrantFailed, eventMsg)
	if statusErr := r.updateStatus(ctx, privilegesReady, metav1.ConditionFalse, reasonPrivilegesTerminalFailure,
		msg, failedDBPhase); statusErr != nil {
		wrappedStatusErr := fmt.Errorf("failed to persist terminal privileges status: %w", statusErr)
		if result, conflictErr, ok := requeueOnConflict(ctx, statusErr, conflictPrivilegesStatus,
			"persisting terminal privileges status"); ok {
			return result, conflictErr, true
		}
		return ctrl.Result{}, stderrors.Join(err, wrappedStatusErr), true
	}
	closed, closeErr := reconcileRequestedClosedState(ctx, rc.DatabaseProvisioner, postgresDB, providerClusterName, databaseEnvironmentName)
	if closeErr != nil {
		return ctrl.Result{}, closeErr, true
	}
	if !closed {
		return ctrl.Result{RequeueAfter: retryDelay}, nil, true
	}
	return ctrl.Result{}, nil, true
}

func privilegeTargetsForDatabases(databasesToGrant []platformv1alpha1.DatabaseDefinition) []databasePrivilegeTarget {
	privilegeTargets := make([]databasePrivilegeTarget, 0, len(databasesToGrant))
	for _, dbSpec := range databasesToGrant {
		privilegeTargets = append(privilegeTargets, databasePrivilegeTarget{
			Database: dbSpec.Name,
			Roles:    EffectiveRoleNames(dbSpec),
		})
	}

	return privilegeTargets
}
