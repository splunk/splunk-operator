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
	"slices"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/sethvargo/go-password/password"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/logging"
	dbclusterreadiness "github.com/splunk/splunk-operator/pkg/postgresql/database/core/components/clusterreadiness"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
	dbclusterinfo "github.com/splunk/splunk-operator/pkg/postgresql/database/ports/clusterinfo"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	identitytypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/identity"
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
)

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

// chooseSecretError unwraps joined role-secret errors and returns the most
// actionable typed error for condition/event handling.
func chooseSecretError(err error) *secretReconcileError {
	leaves := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		leaves = joined.Unwrap()
	}

	var first *secretReconcileError
	var managedMissing *secretReconcileError
	for _, leaf := range leaves {
		se, ok := stderrors.AsType[secretReconcileError](leaf)
		if !ok {
			continue
		}
		if se.reason == reasonExternalSecretMissing {
			return &se
		}
		if se.reason == reasonManagedSecretMissing && managedMissing == nil {
			managedMissing = &se
		}
		if first == nil {
			first = &se
		}
	}
	if managedMissing != nil {
		return managedMissing
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
	postgresDB *platformv1alpha1.PostgresDatabase,
	newDBRepo ports.NewDBRepoFunc,
) (ctrl.Result, error) {
	logger := logging.FromContext(ctx).With("func", "PostgresDatabaseService", "postgresDatabase", postgresDB.Name)
	ctx = logging.WithLogger(ctx, logger)
	logger.DebugContext(ctx, "reconciling PostgresDatabase")
	r := &databaseReconciliation{
		rc: rc, db: postgresDB, newDBRepo: newDBRepo,
		wasReady: postgresDB.Status.Phase != nil && *postgresDB.Status.Phase == string(readyDBPhase),
	}
	for _, phase := range []func(context.Context) (ctrl.Result, error, bool){
		r.initialize,
		r.reconcileEndpoints,
		r.publishMetricsParticipation,
		r.recoverTerminalFailure,
		r.observeProvider,
		r.reconcileCredentials,
		r.reconcileConnectionMetadata,
		r.observeRoles,
		r.reconcileDatabases,
		r.reconcilePrivileges,
		r.completeBootstrap,
		r.observeMetrics,
		r.persistReady,
	} {
		if result, err, stop := phase(ctx); stop {
			return result, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileRWRolePrivileges calls the DBRepo port for each database.
// Errors are collected so all databases are attempted before returning.
func reconcileRWRolePrivileges(
	ctx context.Context,
	rwHost, superPassword string,
	privilegeTargets []databasePrivilegeTarget,
	newDBRepo ports.NewDBRepoFunc,
) error {
	var errs []error
	logger := logging.FromContext(ctx)
	for _, privilegeTarget := range privilegeTargets {
		dbName := privilegeTarget.Database
		started := time.Now()
		repo, err := newDBRepo(ctx, rwHost, dbName, superPassword)
		if err != nil {
			errorCategory := privilegeLogRetryable
			if stderrors.Is(err, ErrTerminal) {
				errorCategory = privilegeLogTerminal
			}
			logger.ErrorContext(ctx, "PostgreSQL privilege reconciliation failed",
				"host", rwHost,
				"database", dbName,
				"duration", time.Since(started),
				"outcome", privilegeLogOutcomeFailure,
				"failure_stage", privilegeLogStageConnect,
				"error_category", errorCategory,
			)
			errs = append(errs, safePrivilegeOperationError("connecting", dbName, err))
			continue
		}
		if err := repo.AssignRequiredPermissionsToRole(ctx, dbName, privilegeTarget.Roles); err != nil {
			errorCategory := privilegeLogRetryable
			if stderrors.Is(err, ErrTerminal) {
				errorCategory = privilegeLogTerminal
			}
			logger.ErrorContext(ctx, "PostgreSQL privilege reconciliation failed",
				"host", rwHost,
				"database", dbName,
				"duration", time.Since(started),
				"outcome", privilegeLogOutcomeFailure,
				"failure_stage", privilegeLogStageGrant,
				"error_category", errorCategory,
			)
			errs = append(errs, safePrivilegeOperationError("granting RW privileges", dbName, err))
			continue
		}
		logger.InfoContext(ctx, "PostgreSQL privilege reconciliation completed",
			"host", rwHost,
			"database", dbName,
			"duration", time.Since(started),
			"outcome", privilegeLogOutcomeSuccess,
		)
	}
	return stderrors.Join(errs...)
}

// safePrivilegeOperationError prevents adapter error text from reaching controller logs or
// status while preserving the terminal marker that controls retry behavior.
func safePrivilegeOperationError(operation, dbName string, err error) error {
	if stderrors.Is(err, ErrTerminal) {
		return fmt.Errorf("%w: %s on database %s failed", ErrTerminal, operation, dbName)
	}
	return fmt.Errorf("%s on database %s failed", operation, dbName)
}

func fetchCluster(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase) (*platformv1alpha1.PostgresCluster, error) {
	cluster := &platformv1alpha1.PostgresCluster{}
	if err := c.Get(ctx, types.NamespacedName{Name: postgresDB.Spec.ClusterRef.Name, Namespace: postgresDB.Namespace}, cluster); err != nil {
		return nil, err
	}
	return cluster, nil
}

func readReferencedClusterCard(ctx context.Context, rc *ReconcileContext, postgresDB *platformv1alpha1.PostgresDatabase) (*identitytypes.ClusterCard, error) {
	if rc.ClusterReader == nil {
		return nil, dbclusterinfo.ErrClusterReaderNotConfigured
	}
	facts, err := rc.ClusterReader.Read(ctx, postgresDB.Namespace, postgresDB.Spec.ClusterRef.Name)
	if err != nil {
		return nil, err
	}
	if facts.Cluster == nil {
		return nil, fmt.Errorf("cluster reader returned no cluster identity")
	}
	return facts.Cluster, nil
}

func validateAuthoritativeProviderUID(ctx context.Context, c client.Reader, authoritative identitytypes.Environment) error {
	if authoritative.Identity.UID == "" {
		return nil
	}

	provider := &cnpgv1.Cluster{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      authoritative.Identity.Name,
		Namespace: authoritative.Identity.Namespace,
	}, provider); err != nil {
		return fmt.Errorf("fetching CNPG Cluster %s/%s: %w", authoritative.Identity.Namespace, authoritative.Identity.Name, err)
	}
	if provider.UID != authoritative.Identity.UID {
		return fmt.Errorf("CNPG Cluster %s/%s UID %q does not match resolved provider identity UID %q", authoritative.Identity.Namespace, authoritative.Identity.Name, provider.UID, authoritative.Identity.UID)
	}
	return nil
}

// observeClusterReadiness applies the pure cluster readiness decision at the
// facade boundary, where condition transitions, events, and status persistence
// remain owned by PostgresDatabase reconciliation.
func observeClusterReadiness(
	ctx context.Context,
	rc *ReconcileContext,
	postgresDB *platformv1alpha1.PostgresDatabase,
	wasReady bool,
	updateStatus func(conditionTypes, metav1.ConditionStatus, conditionReasons, string, reconcileDBPhases) error,
) (dbclusterinfo.ResolvedClusterFacts, ctrl.Result, error, bool) {
	previous := meta.FindStatusCondition(postgresDB.Status.Conditions, string(clusterReady))
	previousReason := ""
	if previous != nil {
		previousReason = previous.Reason
	}

	decision := dbclusterreadiness.New(rc.ClusterReader).Observe(ctx, dbclusterreadiness.Input{
		Namespace:                  postgresDB.Namespace,
		Name:                       postgresDB.Spec.ClusterRef.Name,
		WasReady:                   wasReady,
		PreviousClusterReadyReason: previousReason,
	})
	outcome := decision.Outcome
	if err := outcome.Validate("cluster readiness"); err != nil {
		return decision.Facts, ctrl.Result{}, err, true
	}
	if outcome.StatusAction() != reconciliationtypes.StatusPersistAndContinue && outcome.StatusAction() != reconciliationtypes.StatusPersistAndStop {
		return decision.Facts, ctrl.Result{}, fmt.Errorf("cluster readiness outcome %q does not persist status", outcome.Mode()), true
	}
	logger := logging.FromContext(ctx)
	logger.DebugContext(ctx, "cluster validation completed", "clusterRef", postgresDB.Spec.ClusterRef.Name, "outcome", outcome.Mode(), "reason", outcome.Reason())

	switch outcome.Reason() {
	case string(reasonClusterNotFound):
		rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, clusterReady,
			EventClusterNotFound, fmt.Sprintf("PostgresCluster %s not found", postgresDB.Spec.ClusterRef.Name))
	case string(reasonClusterRecovery):
		rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, clusterReady,
			EventWaitingForClusterRecovery, fmt.Sprintf("referenced PostgresCluster %s is recovering", postgresDB.Spec.ClusterRef.Name))
	case string(reasonClusterProvisioning):
		rc.emitWarnOnceBeforeWait(postgresDB, postgresDB.Status.Conditions, clusterReady,
			EventClusterNotReady, fmt.Sprintf("referenced PostgresCluster %s is not ready yet", postgresDB.Spec.ClusterRef.Name))
	case string(reasonClusterAvailable):
		rc.emitOnConditionTransition(postgresDB, postgresDB.Status.Conditions, clusterReady,
			EventClusterValidated, "Referenced PostgresCluster is ready")
	}

	if err := updateStatus(
		conditionTypes(outcome.Condition()),
		outcome.ConditionStatus(),
		conditionReasons(outcome.Reason()),
		outcome.Message(),
		reconcileDBPhases(outcome.Phase()),
	); err != nil {
		if result, conflictErr, ok := requeueOnConflict(ctx, err, conflictClusterStatus, "persisting cluster readiness status"); ok {
			return decision.Facts, result, conflictErr, true
		}
		if outcome.Mode() == reconciliationtypes.ModeRetryableRequeue {
			logger.ErrorContext(ctx, "failed to persist cluster status", "error", err)
			return decision.Facts, ctrl.Result{}, outcome.Err(), true
		}
		return decision.Facts, ctrl.Result{}, err, true
	}

	switch outcome.Mode() {
	case reconciliationtypes.ModeConverged:
		return decision.Facts, ctrl.Result{}, nil, false
	case reconciliationtypes.ModeWaiting:
		return decision.Facts, outcome.Result(), nil, true
	case reconciliationtypes.ModeRetryableRequeue:
		return decision.Facts, ctrl.Result{}, outcome.Err(), true
	default:
		return decision.Facts, ctrl.Result{}, fmt.Errorf("unexpected cluster readiness outcome %q", outcome.Mode()), true
	}
}

func getDesiredRoles(postgresDB *platformv1alpha1.PostgresDatabase) []string {
	users := make([]string, 0, len(postgresDB.Spec.Databases)*2)
	for _, dbSpec := range postgresDB.Spec.Databases {
		roles := EffectiveRoleNames(dbSpec)
		users = append(users, roles.Admin, roles.RW)
	}
	return users
}

func existingDatabaseStatus(postgresDB *platformv1alpha1.PostgresDatabase) map[string]struct{} {
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
	// Role is the specific managed role that drove a non-Proceed decision, when one
	// is implicated. Empty for cluster-level waits with no single offending role.
	Role string
}

func evaluateRoleGate(postgresDB *platformv1alpha1.PostgresDatabase, status *platformv1alpha1.ManagedRolesStatus) roleGateDecision {
	if status == nil {
		return roleGateDecision{State: roleGatePending, Message: "Waiting for cluster to publish managed role status"}
	}
	self := platformv1alpha1.RoleOwnerReference{Name: postgresDB.Name, UID: string(postgresDB.UID)}
	// Iterate desiredRoles in order so a multi-role failure blames the same role
	// (and therefore the same database) on every reconcile; map iteration would
	// pick an arbitrary offender and flap the published messages.
	desiredRoles := getDesiredRoles(postgresDB)
	roleSet := make(map[string]struct{}, len(desiredRoles))
	for _, role := range desiredRoles {
		roleSet[role] = struct{}{}
	}
	for _, conflict := range status.Conflicts {
		if _, wanted := roleSet[conflict.Role]; wanted && sameRoleOwner(conflict.AttemptedBy, self) {
			return roleGateDecision{State: roleGateConflict, Message: fmt.Sprintf("role %s is already claimed", conflict.Role), Role: conflict.Role}
		}
	}
	for _, role := range desiredRoles {
		if reason, failed := status.Failed[role]; failed {
			return roleGateDecision{State: roleGateFailed, Message: fmt.Sprintf("role %s failed to reconcile: %s", role, reason), Role: role}
		}
	}
	reconciled := make(map[string]struct{}, len(status.Reconciled))
	for _, role := range status.Reconciled {
		reconciled[role] = struct{}{}
	}
	for _, role := range desiredRoles {
		owner, owned := status.RoleOwners[role]
		if !owned || !sameRoleOwner(owner, self) {
			return roleGateDecision{State: roleGatePending, Message: fmt.Sprintf("Waiting for role %s to be owned by this PostgresDatabase", role), Role: role}
		}
		if _, ok := reconciled[role]; !ok {
			return roleGateDecision{State: roleGatePending, Message: fmt.Sprintf("Waiting for role %s to be reconciled", role), Role: role}
		}
	}
	return roleGateDecision{State: roleGateProceed, Message: "Roles are reconciled and owned by this PostgresDatabase"}
}

func sameRoleOwner(a, b platformv1alpha1.RoleOwnerReference) bool {
	return a.Name == b.Name && a.UID == b.UID
}

func persistStatus(ctx context.Context, c client.Client, metrics ports.Recorder, db *platformv1alpha1.PostgresDatabase, wasReadyAtReconcileStart bool, conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases,
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

// initializePendingPhase records that a newly observed PostgresDatabase has
// entered reconciliation without claiming that its desired generation is ready.
func initializePendingPhase(ctx context.Context, c client.Client, db *platformv1alpha1.PostgresDatabase) (bool, error) {
	if db.Status.Phase != nil {
		return false, nil
	}
	before := db.Status.DeepCopy()
	pending := string(pendingDBPhase)
	db.Status.Phase = &pending
	beginReadinessCycle(db, before, false, metav1.ConditionFalse, pendingDBPhase)
	// No sub-condition triggered this write, so pass ConditionTrue to have the reason
	// and message synthesized from the phase rather than forwarded from a caller.
	applyReadyCondition(db, pendingDBPhase, metav1.ConditionTrue, "", "")
	if err := c.Status().Update(ctx, db); err != nil {
		return false, err
	}
	return true, nil
}

func completeReadinessCycle(db *platformv1alpha1.PostgresDatabase) (time.Time, bool) {
	if db.Status.LastTransitionTime == nil {
		return time.Time{}, false
	}
	lastTransitionTime := db.Status.LastTransitionTime.Time
	db.Status.LastTransitionTime = nil
	return lastTransitionTime, true
}

func applyStatus(db *platformv1alpha1.PostgresDatabase, conditionType conditionTypes, conditionStatus metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) {
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
	applyReadyCondition(db, phase, conditionStatus, reason, message)
}

// applyReadyCondition derives the aggregate Ready condition from the phase this
// write is transitioning to. Naively forwarding the triggering call's reason/message
// would self-contradict for the sub-checks that report ConditionTrue while the phase
// is still non-ready (e.g. clusterReady=True at provisioningDBPhase), so those are
// synthesized from the phase instead of forwarded.
func applyReadyCondition(db *platformv1alpha1.PostgresDatabase, phase reconcileDBPhases, status metav1.ConditionStatus, reason conditionReasons, message string) {
	readyStatus, readyReason, readyMessage := metav1.ConditionFalse, reason, message
	switch {
	case phase == readyDBPhase:
		readyStatus, readyReason, readyMessage = metav1.ConditionTrue, reasonDatabaseReady, "All PostgresDatabase checks passed"
	case status != metav1.ConditionFalse:
		readyReason, readyMessage = conditionReasons(phase), fmt.Sprintf("PostgresDatabase is in phase %s", phase)
	}
	meta.SetStatusCondition(&db.Status.Conditions, metav1.Condition{
		Type:               string(readyCondition),
		Status:             readyStatus,
		Reason:             string(readyReason),
		Message:            readyMessage,
		ObservedGeneration: db.Generation,
	})
}

// beginReadinessCycle persists the start of a single time-to-Ready cycle. Initial
// provisioning starts at creation. Later cycles start for a new generation or a
// real readiness blocker. Routine successful Provisioning updates are written
// on every reconcile and do not start a cycle.
func beginReadinessCycle(db *platformv1alpha1.PostgresDatabase, before *platformv1alpha1.PostgresDatabaseStatus, wasReadyAtReconcileStart bool, conditionStatus metav1.ConditionStatus, phase reconcileDBPhases) {
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

func buildDeletionPlan(databases []platformv1alpha1.DatabaseDefinition) deletionPlan {
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

func handleDeletion(ctx context.Context, rc *ReconcileContext, postgresDB *platformv1alpha1.PostgresDatabase) error {
	logger := logging.FromContext(ctx)
	c := rc.Client
	cluster, err := fetchCluster(ctx, c, postgresDB)
	if errors.IsNotFound(err) {
		cluster = nil
	} else if err != nil {
		return fmt.Errorf("getting PostgresCluster for database cleanup: %w", err)
	}
	var clusterCard *identitytypes.ClusterCard
	if cluster != nil {
		clusterCard, err = readReferencedClusterCard(ctx, rc, postgresDB)
		if err != nil {
			return fmt.Errorf("resolving PostgresCluster identity for database cleanup: %w", err)
		}
	}
	environmentNames := databaseEnvironmentNames(clusterCard)
	plan := buildDeletionPlan(postgresDB.Spec.Databases)
	if err := orphanRetainedResources(ctx, c, postgresDB, plan.retained, environmentNames...); err != nil {
		return err
	}
	if err := deleteRemovedResources(ctx, c, postgresDB, plan.deleted, environmentNames...); err != nil {
		return err
	}
	if err := cleanupManagedRoles(ctx, rc, postgresDB, cluster, plan); err != nil {
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

func orphanRetainedResources(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, retained []platformv1alpha1.DatabaseDefinition, environmentNames ...string) error {
	if err := orphanCNPGDatabases(ctx, c, postgresDB, retained, environmentNames...); err != nil {
		return err
	}
	if err := orphanConfigMaps(ctx, c, postgresDB, retained); err != nil {
		return err
	}
	return orphanSecrets(ctx, c, postgresDB, retained)
}

func deleteRemovedResources(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, deleted []platformv1alpha1.DatabaseDefinition, environmentNames ...string) error {
	if err := deleteCNPGDatabases(ctx, c, postgresDB, deleted, environmentNames...); err != nil {
		return err
	}
	if err := deleteConfigMaps(ctx, c, postgresDB, deleted); err != nil {
		return err
	}
	return deleteSecrets(ctx, c, postgresDB, deleted)
}

// cleanupManagedRoles publishes drop intent and retains the finalizer until the cluster stops owning deleted roles.
func cleanupManagedRoles(ctx context.Context, rc *ReconcileContext, postgresDB *platformv1alpha1.PostgresDatabase, cluster *platformv1alpha1.PostgresCluster, plan deletionPlan) error {
	c := rc.Client
	logger := logging.FromContext(ctx)
	if len(plan.deleted) == 0 {
		postgresDB.Status.Databases = nil
		postgresDB.Status.ObservedGeneration = &postgresDB.Generation
		return c.Status().Update(ctx, postgresDB)
	}
	if cluster == nil {
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
		applyStatus(postgresDB, rolesReady, metav1.ConditionFalse, reason, message, deletingDBPhase)
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

func orphanCNPGDatabases(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, databases []platformv1alpha1.DatabaseDefinition, environmentNames ...string) error {
	logger := logging.FromContext(ctx)
	owned, err := ownedCNPGDatabases(ctx, c, postgresDB, databases)
	if err != nil {
		return err
	}
	for _, db := range owned {
		if db.Annotations[annotationRetainedFrom] == postgresDB.Name {
			continue
		}
		stripOwnerReference(db, postgresDB.UID)
		if db.Annotations == nil {
			db.Annotations = make(map[string]string)
		}
		db.Annotations[annotationRetainedFrom] = postgresDB.Name
		if err := c.Update(ctx, db); err != nil {
			return fmt.Errorf("orphaning CNPG Database %s: %w", db.Name, err)
		}
		logger.InfoContext(ctx, "CNPG Database orphaned", "name", db.Name)
	}
	for _, dbSpec := range databases {
		for _, name := range cnpgDatabaseNamesForCleanup(postgresDB, dbSpec.Name, environmentNames...) {
			db := &cnpgv1.Database{}
			if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: postgresDB.Namespace}, db); err != nil {
				if errors.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("getting CNPG Database %s for orphaning: %w", name, err)
			}
			if !metav1.IsControlledBy(db, postgresDB) {
				continue
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
	}
	return nil
}

func orphanConfigMaps(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, databases []platformv1alpha1.DatabaseDefinition) error {
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

func orphanSecrets(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, databases []platformv1alpha1.DatabaseDefinition) error {
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

func deleteCNPGDatabases(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, databases []platformv1alpha1.DatabaseDefinition, environmentNames ...string) error {
	logger := logging.FromContext(ctx)
	owned, err := ownedCNPGDatabases(ctx, c, postgresDB, databases)
	if err != nil {
		return err
	}
	for _, db := range owned {
		if err := c.Delete(ctx, db); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("deleting CNPG Database %s: %w", db.Name, err)
		}
		logger.InfoContext(ctx, "CNPG Database deleted", "name", db.Name)
	}
	for _, dbSpec := range databases {
		for _, name := range cnpgDatabaseNamesForCleanup(postgresDB, dbSpec.Name, environmentNames...) {
			db := &cnpgv1.Database{}
			if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: postgresDB.Namespace}, db); err != nil {
				if errors.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("getting CNPG Database %s for deletion: %w", name, err)
			}
			if !metav1.IsControlledBy(db, postgresDB) {
				continue
			}
			if err := c.Delete(ctx, db); err != nil && !errors.IsNotFound(err) {
				return fmt.Errorf("deleting CNPG Database %s: %w", name, err)
			}
			logger.InfoContext(ctx, "CNPG Database deleted", "name", name)
		}
	}
	return nil
}

func ownedCNPGDatabases(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, databases []platformv1alpha1.DatabaseDefinition) ([]*cnpgv1.Database, error) {
	databaseNames := make(map[string]struct{}, len(databases))
	for _, database := range databases {
		databaseNames[database.Name] = struct{}{}
	}
	var list cnpgv1.DatabaseList
	if err := c.List(ctx, &list, client.InNamespace(postgresDB.Namespace)); err != nil {
		return nil, fmt.Errorf("listing CNPG Databases for cleanup: %w", err)
	}
	owned := make([]*cnpgv1.Database, 0)
	for i := range list.Items {
		database := &list.Items[i]
		if _, found := databaseNames[database.Spec.Name]; found && metav1.IsControlledBy(database, postgresDB) {
			owned = append(owned, database)
		}
	}
	return owned, nil
}

func deleteConfigMaps(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, databases []platformv1alpha1.DatabaseDefinition) error {
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

func deleteSecrets(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, databases []platformv1alpha1.DatabaseDefinition) error {
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

func roleCleanupTimedOut(postgresDB *platformv1alpha1.PostgresDatabase) bool {
	return postgresDB.DeletionTimestamp != nil && time.Since(postgresDB.DeletionTimestamp.Time) > roleCleanupTimeout
}

func rolesStillOwnedBySelf(postgresDB *platformv1alpha1.PostgresDatabase, status *platformv1alpha1.ManagedRolesStatus, databases []platformv1alpha1.DatabaseDefinition) bool {
	if status == nil {
		return true
	}
	self := platformv1alpha1.RoleOwnerReference{Name: postgresDB.Name, UID: string(postgresDB.UID)}
	for _, dbSpec := range databases {
		roles := EffectiveRoleNames(dbSpec)
		for _, role := range []string{roles.Admin, roles.RW} {
			if owner, ok := status.RoleOwners[role]; ok && sameRoleOwner(owner, self) {
				return true
			}
		}
	}
	return false
}

// ResolveSecretNames returns the credential Secret names for a database. Names are stable
// regardless of adminRoleName/rwRoleName overrides.
func ResolveSecretNames(postgresDBName string, dbSpec platformv1alpha1.DatabaseDefinition) (adminSecretName string, rwSecretName string) {
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

func adoptResource(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *platformv1alpha1.PostgresDatabase, obj client.Object) error {
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

func reconcileRoleSecrets(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *platformv1alpha1.PostgresDatabase, existingDatabases map[string]struct{}) error {
	for _, dbSpec := range postgresDB.Spec.Databases {
		missingPolicy := secretMissingPolicyForDB(dbSpec.Name, existingDatabases)
		adminSecretName, rwSecretName := ResolveSecretNames(postgresDB.Name, dbSpec)
		roles := EffectiveRoleNames(dbSpec)

		adminErr := reconcileRoleSecret(ctx, c,
			scheme, postgresDB, roles.Admin,
			adminSecretName,
			missingPolicy, dbSpec)
		rwErr := reconcileRoleSecret(ctx, c,
			scheme, postgresDB, roles.RW,
			rwSecretName,
			missingPolicy, dbSpec)
		if err := stderrors.Join(adminErr, rwErr); err != nil {
			return err
		}
	}
	return nil
}

func reconcileRoleSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *platformv1alpha1.PostgresDatabase, roleName, secretName string, missingPolicy secretMissingPolicy, dbSpec platformv1alpha1.DatabaseDefinition) error {
	if dbSpec.PasswordConfig != nil {
		return ensureExternalSecret(ctx, c, postgresDB, secretName, roleName)
	} else {
		if missingPolicy == reportSecretDriftIfMissing {
			return ensureProvisionedSecret(ctx, c, scheme, postgresDB, roleName, secretName)
		}
		return ensureSecret(ctx, c, scheme, postgresDB, roleName, secretName)
	}
}

func ensureExternalSecret(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, secretName, roleName string) error {
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

	return ValidateExternalDatabaseSecret(secret, secretName, roleName)
}

func ValidateExternalDatabaseSecret(secret *corev1.Secret, secretName, expectedRoleName string) error {
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

	if string(secret.Data[secretKeyUsername]) != expectedRoleName {
		return secretReconcileError{
			message: fmt.Sprintf("external secret %q username does not match PostgreSQL role %q", secretName, expectedRoleName),
			reason:  reasonExternalSecretInvalid,
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

func ensureSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *platformv1alpha1.PostgresDatabase, roleName, secretName string) error {
	secret, err := getSecret(ctx, c, postgresDB.Namespace, secretName)
	if err != nil {
		return err
	}
	if secret == nil {
		return createRoleSecret(ctx, c, scheme, postgresDB, roleName, secretName)
	}
	return reconcileExistingSecret(ctx, c, scheme, postgresDB, secretName, secret)
}

func ensureProvisionedSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *platformv1alpha1.PostgresDatabase, roleName, secretName string) error {
	secret, err := getSecret(ctx, c, postgresDB.Namespace, secretName)
	if err != nil {
		return err
	}
	if secret == nil {
		return secretReconcileError{
			message: fmt.Sprintf("Managed Secret %s is missing for previously provisioned role %s; restore the Secret with the original credential data", secretName, roleName),
			reason:  reasonManagedSecretMissing,
		}
	}
	return reconcileExistingSecret(ctx, c, scheme, postgresDB, secretName, secret)
}

func isManagedSecretDriftReason(reason conditionReasons) bool {
	return reason == reasonManagedSecretMissing || reason == reasonManagedSecretOwnershipConflict
}

// reconcileExistingSecret only reconciles ownership — it never rewrites secret data.
// Passwords must not be regenerated for existing credentials; CNPG and consumers hold live references.
func reconcileExistingSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *platformv1alpha1.PostgresDatabase, secretName string, secret *corev1.Secret) error {
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
			message: fmt.Sprintf("Managed Secret %s is controlled by %s %s; remove the conflicting owner or restore operator ownership", secretName, owner.Kind, owner.Name),
			reason:  reasonManagedSecretOwnershipConflict,
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

func createRoleSecret(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *platformv1alpha1.PostgresDatabase, roleName, secretName string) error {
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

func buildPasswordSecret(postgresDB *platformv1alpha1.PostgresDatabase, secretName, roleName, pw string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: postgresDB.Namespace,
			Labels:    map[string]string{labelManagedBy: "splunk-operator", labelCNPGReload: "true"},
		},
		Data: map[string][]byte{"username": []byte(roleName), secretKeyPassword: []byte(pw)},
	}
}

func reconcileRoleConfigMaps(ctx context.Context, c client.Client, scheme *runtime.Scheme, postgresDB *platformv1alpha1.PostgresDatabase, endpoints clusterEndpoints) error {
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
			data, _, err := buildDatabaseConfigMapDataForDatabase(dbSpec, endpoints)
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

func persistDatabaseInfos(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, ready bool, exists bool, providerClusterName string) error {
	before := postgresDB.Status.DeepCopy()
	postgresDB.Status.Databases = populateDatabaseStatusForEnvironment(postgresDB, providerClusterName, ready, exists)
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation
	if equality.Semantic.DeepEqual(*before, postgresDB.Status) {
		return nil
	}
	return c.Status().Update(ctx, postgresDB)
}

// persistDatabaseMessages overlays per-database readiness onto existing status entries: a
// database is ready exactly when the reasons map holds no failure for it, and carries that
// reason as its message otherwise. Roles and the bootstrap identity are never touched, so
// reporting a failure here cannot re-run the privileges phase.
func persistDatabaseMessages(ctx context.Context, c client.Client, postgresDB *platformv1alpha1.PostgresDatabase, reasons map[string]string) error {
	before := postgresDB.Status.DeepCopy()
	for i := range postgresDB.Status.Databases {
		reason, hasReason := reasons[postgresDB.Status.Databases[i].Name]
		postgresDB.Status.Databases[i].Message = reason
		postgresDB.Status.Databases[i].Ready = !hasReason
	}
	postgresDB.Status.ObservedGeneration = &postgresDB.Generation
	if equality.Semantic.DeepEqual(*before, postgresDB.Status) {
		return nil
	}
	return c.Status().Update(ctx, postgresDB)
}

func populateDatabaseStatus(postgresDB *platformv1alpha1.PostgresDatabase, flags ...bool) []platformv1alpha1.DatabaseInfo {
	return populateDatabaseStatusForEnvironment(postgresDB, "", flags...)
}

func populateDatabaseStatusForEnvironment(postgresDB *platformv1alpha1.PostgresDatabase, providerClusterName string, flags ...bool) []platformv1alpha1.DatabaseInfo {
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
	statuses := populateDatabaseStatusForDefinitionsForEnvironment(postgresDB, postgresDB.Spec.Databases, ready, exists, includeRoles, providerClusterName)
	if includeRoles {
		statuses = append(statuses, removedDatabaseTombstones(postgresDB)...)
	}
	return statuses
}

// removedDatabaseTombstones re-publishes an Exists:false tombstone for any database that
// was previously reported in status but has since been removed from Spec.Databases while
// the PostgresDatabase itself is not being deleted (e.g. a spec edit, not a CR deletion).
// Without this, the role simply disappears from status with no drop signal, and
// computeDesiredRoles (pkg/postgresql/cluster/core/managed_roles_model.go) has no explicit
// drop to observe, so it keeps treating the role as still owned forever. This mirrors the
// tombstoning cleanupManagedRoles performs for a full CR deletion.
func removedDatabaseTombstones(postgresDB *platformv1alpha1.PostgresDatabase) []platformv1alpha1.DatabaseInfo {
	inSpec := make(map[string]struct{}, len(postgresDB.Spec.Databases))
	for _, dbSpec := range postgresDB.Spec.Databases {
		inSpec[dbSpec.Name] = struct{}{}
	}
	var tombstones []platformv1alpha1.DatabaseInfo
	for _, existing := range postgresDB.Status.Databases {
		if _, ok := inSpec[existing.Name]; ok {
			continue
		}
		if len(existing.Roles) == 0 {
			continue
		}
		tombstone := platformv1alpha1.DatabaseInfo{Name: existing.Name}
		for _, role := range existing.Roles {
			tombstone.Roles = append(tombstone.Roles, platformv1alpha1.DatabaseRoleInfo{
				Name:      role.Name,
				SecretRef: role.SecretRef,
				Exists:    false,
			})
		}
		tombstones = append(tombstones, tombstone)
	}
	return tombstones
}

func populateDatabaseStatusForDefinitions(postgresDB *platformv1alpha1.PostgresDatabase, definitions []platformv1alpha1.DatabaseDefinition, ready bool, exists bool, includeRoles ...bool) []platformv1alpha1.DatabaseInfo {
	publishRoles := true
	if len(includeRoles) > 0 {
		publishRoles = includeRoles[0]
	}
	return populateDatabaseStatusForDefinitionsForEnvironment(postgresDB, definitions, ready, exists, publishRoles, "")
}

func populateDatabaseStatusForDefinitionsForEnvironment(
	postgresDB *platformv1alpha1.PostgresDatabase,
	definitions []platformv1alpha1.DatabaseDefinition,
	ready bool,
	exists bool,
	publishRoles bool,
	providerClusterName string,
) []platformv1alpha1.DatabaseInfo {
	existingProvisioned := make(map[string]bool, len(postgresDB.Status.Databases))
	existingMessage := make(map[string]string, len(postgresDB.Status.Databases))
	existingUID := make(map[string]types.UID, len(postgresDB.Status.Databases))
	for _, existing := range postgresDB.Status.Databases {
		if databaseProvisioned(existing) {
			existingProvisioned[existing.Name] = true
		}
		existingMessage[existing.Name] = existing.Message
		existingUID[existing.Name] = existing.DatabaseUID
	}
	databases := make([]platformv1alpha1.DatabaseInfo, 0, len(definitions))
	for _, dbSpec := range definitions {
		adminSecretName, rwSecretName := ResolveSecretNames(postgresDB.Name, dbSpec)
		roles := EffectiveRoleNames(dbSpec)
		info := platformv1alpha1.DatabaseInfo{
			Name:               dbSpec.Name,
			Ready:              ready,
			AdminUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: adminSecretName}, Key: secretKeyPassword},
			RWUserSecretRef:    &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: rwSecretName}, Key: secretKeyPassword},
			ConfigMapRef:       &corev1.LocalObjectReference{Name: configMapName(postgresDB.Name, dbSpec.Name)},
		}
		// DatabaseRef is the sticky bootstrap marker. Ready alone is transient and must not
		// decide whether one-time privileges need to run again.
		if ready || (exists && existingProvisioned[dbSpec.Name]) {
			info.DatabaseRef = &corev1.LocalObjectReference{Name: cnpgDatabaseResourceName(postgresDB, dbSpec.Name, providerClusterName)}
			info.DatabaseUID = existingUID[dbSpec.Name]
		}
		// Preserve any existing not-ready message; persistDatabaseMessages owns setting and clearing it.
		if !info.Ready {
			info.Message = existingMessage[dbSpec.Name]
		}
		if publishRoles {
			info.Roles = []platformv1alpha1.DatabaseRoleInfo{
				{Name: roles.Admin, SecretRef: &corev1.LocalObjectReference{Name: adminSecretName}, Exists: exists},
				{Name: roles.RW, SecretRef: &corev1.LocalObjectReference{Name: rwSecretName}, Exists: exists},
			}
		}
		databases = append(databases, info)
	}
	return databases
}

// databaseProvisioned reports whether a status entry completed its initial bootstrap.
// DatabaseRef stays present across later not-ready provider changes.
// Entries with no role status are legacy credential-only rows and count as provisioned.
func databaseProvisioned(dbInfo platformv1alpha1.DatabaseInfo) bool {
	return dbInfo.DatabaseRef != nil || len(dbInfo.Roles) == 0
}

// roleGateReasons maps each spec database to a not-ready message for a role-gate
// failure. The database owning the offending role gets the specific gate message;
// the rest are reported as blocked by it, since the gate fails the CR as a whole.
func roleGateReasons(postgresDB *platformv1alpha1.PostgresDatabase, gate roleGateDecision) map[string]string {
	blamed := databaseForRole(postgresDB, gate.Role)
	reasons := make(map[string]string, len(postgresDB.Spec.Databases))
	for _, dbSpec := range postgresDB.Spec.Databases {
		if dbSpec.Name == blamed {
			reasons[dbSpec.Name] = gate.Message
		} else if blamed != "" {
			reasons[dbSpec.Name] = fmt.Sprintf("blocked by role gate on database %q", blamed)
		} else {
			reasons[dbSpec.Name] = gate.Message
		}
	}
	return reasons
}

// databaseForRole resolves a managed role name back to its spec database, or "" if none matches.
func databaseForRole(postgresDB *platformv1alpha1.PostgresDatabase, role string) string {
	for _, dbSpec := range postgresDB.Spec.Databases {
		roles := EffectiveRoleNames(dbSpec)
		if role == roles.Admin || role == roles.RW {
			return dbSpec.Name
		}
	}
	return ""
}

// Naming helpers — single source of truth shared by creation and status wiring.
func adminRoleName(dbName string) string { return dbName + "_admin" }
func rwRoleName(dbName string) string    { return dbName + "_rw" }
func cnpgDatabaseName(postgresDBName, dbName string) string {
	return fmt.Sprintf("%s-%s", postgresDBName, dbName)
}

func cnpgDatabaseResourceName(postgresDB *platformv1alpha1.PostgresDatabase, dbName, providerClusterName string) string {
	if providerClusterName == "" || providerClusterName == postgresDB.Spec.ClusterRef.Name {
		return cnpgDatabaseName(postgresDB.Name, dbName)
	}
	return fmt.Sprintf("%s-%s-%s", postgresDB.Name, providerClusterName, dbName)
}

func cnpgDatabaseEnvironmentName(environment identitytypes.Environment) string {
	if environment.Scope != identitytypes.NamingScopeEnvironment {
		return ""
	}
	return environment.Identity.Name
}

func cnpgDatabaseNameFromStatus(postgresDB *platformv1alpha1.PostgresDatabase, databaseName string) string {
	for _, database := range postgresDB.Status.Databases {
		if database.Name == databaseName && database.DatabaseRef != nil && database.DatabaseRef.Name != "" {
			return database.DatabaseRef.Name
		}
	}
	return cnpgDatabaseName(postgresDB.Name, databaseName)
}

func cnpgDatabaseNamesForCleanup(postgresDB *platformv1alpha1.PostgresDatabase, databaseName string, environmentNames ...string) []string {
	names := make([]string, 0, len(environmentNames)+1)
	seen := make(map[string]struct{}, len(environmentNames)+1)
	add := func(name string) {
		if name == "" {
			return
		}
		if _, found := seen[name]; found {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	add(cnpgDatabaseNameFromStatus(postgresDB, databaseName))
	for _, environmentName := range environmentNames {
		add(cnpgDatabaseResourceName(postgresDB, databaseName, environmentName))
	}
	return names
}

func databaseEnvironmentNames(cluster *identitytypes.ClusterCard) []string {
	if cluster == nil {
		return nil
	}
	names := make([]string, 0, len(cluster.Managed))
	for _, environment := range cluster.Managed {
		name := cnpgDatabaseEnvironmentName(environment)
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
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

func upsertFailureState(db *platformv1alpha1.PostgresDatabase, failureType string) {
	db.Status.ReconcileFailureType = failureType
}

func terminalFailureType(err error) (string, bool) {
	if stderrors.Is(err, ErrTerminal) {
		return reconcileFailurePrivileges, true
	}
	return "", false
}

func hasCurrentReconcileFailure(db *platformv1alpha1.PostgresDatabase) bool {
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
