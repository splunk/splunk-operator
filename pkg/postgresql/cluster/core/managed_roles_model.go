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
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type managedRolesModel struct {
	client         client.Client
	events         eventEmitter
	updateStatus   healthStatusUpdater
	contracts      *reconcileContracts
	cluster        *enterprisev4.PostgresCluster
	newRoleSweeper ports.NewRoleSweeperFunc

	desiredRoles []managedRole
	roleOwners   map[string]enterprisev4.RoleOwnerReference
	conflicts    []enterprisev4.RoleConflict
}

func newManagedRolesModel(c client.Client, _ *runtime.Scheme, events eventEmitter, updateStatus healthStatusUpdater, cluster *enterprisev4.PostgresCluster, contracts *reconcileContracts, newRoleSweeper ports.NewRoleSweeperFunc) *managedRolesModel {
	return &managedRolesModel{client: c, events: events, updateStatus: updateStatus, contracts: contracts, cluster: cluster, newRoleSweeper: newRoleSweeper}
}

func (m *managedRolesModel) Name() string { return pgcConstants.ComponentManagedRoles }
func (m *managedRolesModel) Requires() []contractKey {
	return []contractKey{contractCNPGCluster, contractSecret}
}
func (m *managedRolesModel) Provides() []contractKey { return nil }

func (m *managedRolesModel) CheckContracts() error {
	if !checkContractsFromRequirements(m.Requires(), m.contracts) {
		return errContractsNotReady
	}
	return nil
}

func (m *managedRolesModel) Reconcile(ctx context.Context) error {
	// On a restore the snapshot carries the source cluster's roles with stale credentials,
	// so sweep them before patching managed roles into the CNPG Cluster.
	if m.needsCredentialSweep() {
		return m.runCredentialSweep(ctx)
	}

	databases, err := listPostgresDatabasesForCluster(ctx, m.client, m.cluster)
	if err != nil {
		return err
	}
	currentOwners := map[string]enterprisev4.RoleOwnerReference{}
	if m.cluster.Status.ManagedRolesStatus != nil {
		for role, owner := range m.cluster.Status.ManagedRolesStatus.RoleOwners {
			currentOwners[role] = owner
		}
	}
	currentRoles := currentManagedRolesFromCNPG(m.contracts.CNPGCluster)
	decision := computeDesiredRoles(databases, currentOwners, currentRoles, reconciledAbsentRoles(m.contracts.CNPGCluster, currentRoles))
	m.desiredRoles = decision.Roles
	m.roleOwners = decision.RoleOwners
	m.conflicts = decision.Conflicts

	if len(databases) == 0 && len(currentOwners) > 0 {
		// Empty observations may come from an unsynced cache; keep ownership until a non-empty scan can compute drops.
		logging.FromContext(ctx).InfoContext(ctx,
			"no PostgresDatabases observed for cluster; retaining existing managed-role owners to avoid wiping ownership",
			"retainedRoleOwners", len(currentOwners))
		m.roleOwners = currentOwners
		m.desiredRoles = retainCurrentOwnedRoles(currentOwners, currentRoles)
	}
	if hasLegacyDatabaseStatus(databases) && len(currentOwners) == 0 && len(m.desiredRoles) == 0 && len(currentRoles) > 0 {
		logging.FromContext(ctx).InfoContext(ctx,
			"PostgresDatabase status without role intent observed; preserving current managed roles",
			"preservedRoleCount", len(currentRoles))
		m.desiredRoles = retainAllCurrentRoles(currentRoles)
	}

	if err := reconcileManagedRoles(ctx, m.client, m.desiredRoles, m.contracts.CNPGCluster); err != nil {
		return newReconcileFailure(reasonManagedRolesFailed, err)
	}
	return nil
}

// needsCredentialSweep gates the sweep to run exactly once per restore: only for
// backup-bootstrapped clusters (any recovery source — volume snapshot or object storage), and only
// until completion is recorded in status. The sweep is source-agnostic: any recovered cluster can
// carry unmanaged roles with stale password hashes, so it must run for objectStorage restores too.
func (m *managedRolesModel) needsCredentialSweep() bool {
	b := m.cluster.Spec.BootstrapFrom
	if b == nil || (b.VolumeSnapshot == nil && b.ObjectStorage == nil) {
		return false
	}
	return m.cluster.Status.Restore == nil || !m.cluster.Status.Restore.CredentialSweep.Completed
}

// runCredentialSweep disables all non-system login roles on the restored cluster. It performs
// only the DB write and returns a typed error; events, health, and status are built in Observe.
//
//   - transient connect failure → errSweepConnect: the restored DB may still be initialising,
//     so the pass requeues and retries. No status is written, so the sweep runs again next pass.
//   - terminal connect failure → errSweepTerminal: the sweeper reported ErrSweeperConnectTerminal
//     (bad credentials, insufficient privilege), which retrying will not fix — surfaced as Failed.
//   - exec failure → errSweepTerminal: the sweep connected but a role could not be disabled, which
//     retrying the same query will not fix — surfaced as Failed.
//   - success → nil. Observe detects the completed sweep (needsCredentialSweep still true because
//     status is not yet written) and records status.Restore, then requeues to re-enable roles.
func (m *managedRolesModel) runCredentialSweep(ctx context.Context) error {
	started := time.Now()
	pw := string(m.contracts.Secret.Data[secretKeyPassword])
	rwHost := fmt.Sprintf("%s-rw.%s", m.contracts.CNPGCluster.Name, m.cluster.Namespace)
	logger := logging.FromContext(ctx)

	repo, err := m.newRoleSweeper(ctx, rwHost, defaultDatabaseName, pw)
	if err != nil {
		if errors.Is(err, ports.ErrSweeperConnectTerminal) {
			logger.ErrorContext(ctx, "PostgreSQL post-restore credential sweep failed",
				"host", rwHost,
				"database", defaultDatabaseName,
				"duration", time.Since(started),
				"outcome", credentialSweepLogOutcomeFailure,
				"failure_stage", credentialSweepLogStageConnect,
				"error_category", credentialSweepLogTerminal,
			)
			return errSweepTerminal
		}
		logger.ErrorContext(ctx, "PostgreSQL post-restore credential sweep failed",
			"host", rwHost,
			"database", defaultDatabaseName,
			"duration", time.Since(started),
			"outcome", credentialSweepLogOutcomeFailure,
			"failure_stage", credentialSweepLogStageConnect,
			"error_category", credentialSweepLogRetryable,
		)
		return errSweepConnect
	}

	rolesSwept, err := repo.SweepUnmanagedRolesAfterRestore(ctx)
	if err != nil {
		logger.ErrorContext(ctx, "PostgreSQL post-restore credential sweep failed",
			"host", rwHost,
			"database", defaultDatabaseName,
			"duration", time.Since(started),
			"outcome", credentialSweepLogOutcomeFailure,
			"failure_stage", credentialSweepLogStageSweep,
			"error_category", credentialSweepLogTerminal,
		)
		return errSweepTerminal
	}
	logger.InfoContext(ctx, "PostgreSQL post-restore credential sweep completed",
		"host", rwHost,
		"database", defaultDatabaseName,
		"duration", time.Since(started),
		"outcome", credentialSweepLogOutcomeSuccess,
		"roles_swept", rolesSwept,
	)
	return nil
}

func (m *managedRolesModel) Observe(_ context.Context, reconcileErr error) (componentHealth, error) {
	before := m.cluster.Status.DeepCopy()
	health, err := m.computeHealth(reconcileErr)
	statusErr := writeComponentStatus(m.updateStatus, before, health)
	return health, errors.Join(err, statusErr)
}

func (m *managedRolesModel) computeHealth(reconcileErr error) (componentHealth, error) {
	// A transient sweep connect failure is not a cluster failure: the restored DB may still be
	// initialising. Requeue so the sweep retries; no status is written. The warning is emitted
	// only on the transition into this state, not on every requeue.
	if errors.Is(reconcileErr, errSweepConnect) {
		h := newProvisioningHealth(managedRolesReady, reasonManagedRolesPending, "Waiting to connect for credential sweep")
		if !m.isManagedRolesConditionState(metav1.ConditionFalse, reasonManagedRolesPending, h.Message) {
			m.events.emitWarning(m.cluster, EventUnmanagedRolesSweepFailed, fmt.Sprintf("failed to connect for credential sweep for PostgresCluster %s — check operator logs", m.cluster.Name))
		}
		return h, nil
	}
	// A terminal sweep failure (terminal connect or failed role disable) is not recoverable by
	// retrying — surface Failed without exposing the driver cause.
	if errors.Is(reconcileErr, errSweepTerminal) {
		m.events.emitWarning(m.cluster, EventUnmanagedRolesSweepFailed, fmt.Sprintf("failed to sweep unmanaged roles for PostgresCluster %s — check operator logs", m.cluster.Name))
		return newFailedHealth(managedRolesReady, reasonManagedRolesFailed, "Failed to sweep unmanaged roles; check operator logs"), reconcileErr
	}

	if errors.Is(reconcileErr, errDatabaseListUnavailable) {
		return newPendingHealth(managedRolesReady, reasonManagedRolesPending, "Waiting to list PostgresDatabases for managed-role computation"), nil
	}
	if h, err, ok := classifyReconcileErr(reconcileErr, managedRolesReady, m.events, m.cluster, EventManagedRolesFailed, "managed roles"); ok {
		return h, err
	}

	// A nil error while a sweep was still needed means the sweep just succeeded this pass.
	// Record status.Restore (persisted by writeComponentStatus) and requeue so the next pass
	// re-enables managed roles. Checked before managed-roles observation because the sweep pass
	// intentionally skips the CNPG role patch.
	if m.needsCredentialSweep() {
		return m.observeCredentialSweepDone(), nil
	}

	syncManagedRolesStatusFromCNPG(m.cluster, m.contracts.CNPGCluster, m.desiredRoles, m.roleOwners, m.conflicts)
	status := m.cluster.Status.ManagedRolesStatus
	if status == nil {
		return newPendingHealth(managedRolesReady, reasonManagedRolesPending, "Managed roles status not published yet"), nil
	}

	if len(status.Failed) > 0 {
		h := newFailedHealth(managedRolesReady, reasonManagedRolesFailed, fmt.Sprintf("Managed roles reconciliation failed for %d role(s)", len(status.Failed)))
		m.emitManagedRolesConvergeFailure(h.Message)
		return h, fmt.Errorf("managed roles have failed entries")
	}

	if len(status.Pending) > 0 {
		return newPendingHealth(managedRolesReady, reasonManagedRolesPending, fmt.Sprintf("Managed roles pending for %d role(s)", len(status.Pending))), nil
	}

	h := newReadyHealth(managedRolesReady, reasonManagedRolesReady, "Managed roles are reconciled")
	if !meta.IsStatusConditionTrue(m.cluster.Status.Conditions, string(managedRolesReady)) {
		m.events.emitNormal(m.cluster, EventManagedRolesReady, fmt.Sprintf("managed roles reconciled for PostgresCluster %s", m.cluster.Name))
	}
	return h, nil
}

// observeCredentialSweepDone records the restore status for a sweep that just completed and
// requeues so the next pass re-enables managed roles. The status write is what flips
// needsCredentialSweep to false, so the sweep runs exactly once.
func (m *managedRolesModel) observeCredentialSweepDone() componentHealth {
	m.cluster.Status.Restore = &enterprisev4.RestoreStatus{
		Source:          restoreSourceStatus(m.cluster.Spec.BootstrapFrom),
		CredentialSweep: enterprisev4.RestoreCredentialSweepStatus{Completed: true},
	}
	m.events.emitNormal(m.cluster, EventUnmanagedRolesSweepDone, fmt.Sprintf("unmanaged login roles disabled for PostgresCluster %s", m.cluster.Name))
	return newProvisioningHealth(managedRolesReady, reasonManagedRolesPending, "Credential sweep completed, waiting for managed roles to be re-enabled")
}

// restoreSourceStatus builds the observed restore source from the (source-agnostic) bootstrapFrom
// intent, echoing whichever source was used and the PITR target if any. Safe for either source type.
func restoreSourceStatus(b *enterprisev4.BootstrapFrom) enterprisev4.RestoreSourceStatus {
	source := enterprisev4.RestoreSourceStatus{}
	if b == nil {
		return source
	}
	if b.VolumeSnapshot != nil {
		name := b.VolumeSnapshot.Storage
		source.VolumeSnapshot = &name
	}
	if b.ObjectStorage != nil {
		name := b.ObjectStorage.ServerName
		source.ObjectStorage = &name
	}
	source.RequestedRecoveryTarget = recoveryTargetStatus(b.RecoveryTarget)
	return source
}

// recoveryTargetStatus builds the structured echo of a requested recovery target for status display,
// mirroring the spec RecoveryTarget shape so consumers need not parse a formatted string. It reflects
// what the restore was asked to recover to, derived from the desired spec (not observed from the
// provider). Returns nil when no target is set (recovery to latest available WAL).
func recoveryTargetStatus(rt *enterprisev4.RecoveryTarget) *enterprisev4.RecoveryTargetStatus {
	if rt == nil {
		return nil
	}
	status := &enterprisev4.RecoveryTargetStatus{
		Type:  rt.Type,
		Value: rt.Value,
	}
	if rt.Exclusive != nil {
		exclusive := *rt.Exclusive
		status.Exclusive = &exclusive
	}
	return status
}

func (m *managedRolesModel) emitManagedRolesConvergeFailure(message string) {
	if m.isManagedRolesConditionState(metav1.ConditionFalse, reasonManagedRolesFailed, message) {
		return
	}
	m.events.emitWarning(m.cluster, EventManagedRolesFailed, message)
}

// isManagedRolesConditionState reports whether the current managedRolesReady condition already
// matches the given status/reason/message. It gates one-shot warning events so a state that
// persists across requeues is announced once, not on every pass.
func (m *managedRolesModel) isManagedRolesConditionState(status metav1.ConditionStatus, reason conditionReasons, message string) bool {
	cond := meta.FindStatusCondition(m.cluster.Status.Conditions, string(managedRolesReady))
	return cond != nil &&
		cond.Status == status &&
		cond.Reason == string(reason) &&
		cond.Message == message
}

var errDatabaseListUnavailable = errors.New("unable to list PostgresDatabases for managed-role computation")

func listPostgresDatabasesForCluster(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster) ([]enterprisev4.PostgresDatabase, error) {
	var list enterprisev4.PostgresDatabaseList
	if err := c.List(ctx, &list,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{enterprisev4.PostgresDatabaseClusterRefNameField: cluster.Name},
	); err != nil {
		logging.FromContext(ctx).WarnContext(ctx,
			"indexed PostgresDatabase list failed, falling back to namespace list",
			"index", enterprisev4.PostgresDatabaseClusterRefNameField, "error", err)
		if fallbackErr := c.List(ctx, &list, client.InNamespace(cluster.Namespace)); fallbackErr != nil {
			return nil, fmt.Errorf("%w (%s/%s): indexed=%v fallback=%v", errDatabaseListUnavailable, cluster.Namespace, cluster.Name, err, fallbackErr)
		}
	}
	items := make([]enterprisev4.PostgresDatabase, 0, len(list.Items))
	for _, db := range list.Items {
		if db.Spec.ClusterRef.Name == cluster.Name {
			items = append(items, db)
		}
	}
	return items, nil
}

type desiredRolesDecision struct {
	Roles      []managedRole
	Conflicts  []enterprisev4.RoleConflict
	RoleOwners map[string]enterprisev4.RoleOwnerReference
}

type roleClaim struct {
	Owner enterprisev4.RoleOwnerReference
	Role  enterprisev4.DatabaseRoleInfo
}

func computeDesiredRoles(databases []enterprisev4.PostgresDatabase, ownerMap map[string]enterprisev4.RoleOwnerReference, currentRoles map[string]managedRole, absentReconciled map[string]struct{}) desiredRolesDecision {
	dbByOwner := make(map[enterprisev4.RoleOwnerReference]enterprisev4.PostgresDatabase, len(databases))
	claims := map[string][]roleClaim{}
	explicitDrops := map[string]map[enterprisev4.RoleOwnerReference]struct{}{}

	for _, db := range databases {
		owner := enterprisev4.RoleOwnerReference{Name: db.Name, UID: string(db.UID)}
		dbByOwner[owner] = db
		for _, info := range db.Status.Databases {
			for _, role := range info.Roles {
				if role.Name == "" {
					continue
				}
				if !role.Exists {
					if explicitDrops[role.Name] == nil {
						explicitDrops[role.Name] = map[enterprisev4.RoleOwnerReference]struct{}{}
					}
					explicitDrops[role.Name][owner] = struct{}{}
					continue
				}
				if role.SecretRef == nil || role.SecretRef.Name == "" {
					continue
				}
				claims[role.Name] = append(claims[role.Name], roleClaim{Owner: owner, Role: role})
			}
		}
	}

	newOwners := make(map[string]enterprisev4.RoleOwnerReference, len(ownerMap)+len(claims))
	rolesByName := map[string]managedRole{}
	var conflicts []enterprisev4.RoleConflict

	for roleName, owner := range ownerMap {
		if _, exists := dbByOwner[owner]; !exists {
			continue
		}
		if dropOwners, drop := explicitDrops[roleName]; drop {
			if _, ok := dropOwners[owner]; ok {
				if _, reconciled := absentReconciled[roleName]; reconciled {
					continue
				}
				newOwners[roleName] = owner
				rolesByName[roleName] = managedRole{Name: roleName, Exists: false}
				continue
			}
		}
		newOwners[roleName] = owner
		if role, ok := currentRoles[roleName]; ok {
			rolesByName[roleName] = role
		}
	}

	for roleName, roleClaims := range claims {
		if incumbent, hasIncumbent := newOwners[roleName]; hasIncumbent {
			var incumbentClaim *roleClaim
			for i := range roleClaims {
				claim := roleClaims[i]
				if sameOwner(claim.Owner, incumbent) {
					incumbentClaim = &claim
					continue
				}
				claimedBy := incumbent
				conflicts = append(conflicts, enterprisev4.RoleConflict{Role: roleName, ClaimedBy: &claimedBy, AttemptedBy: claim.Owner})
			}
			if incumbentClaim != nil {
				rolesByName[roleName] = managedRoleFromClaim(*incumbentClaim)
			}
			continue
		}

		uniqueClaims := collapseClaims(roleClaims)
		if len(uniqueClaims) == 1 {
			claim := uniqueClaims[0]
			newOwners[roleName] = claim.Owner
			rolesByName[roleName] = managedRoleFromClaim(claim)
			continue
		}
		for _, claim := range uniqueClaims {
			conflicts = append(conflicts, enterprisev4.RoleConflict{Role: roleName, AttemptedBy: claim.Owner})
		}
	}

	roles := make([]managedRole, 0, len(rolesByName))
	for _, role := range rolesByName {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Role != conflicts[j].Role {
			return conflicts[i].Role < conflicts[j].Role
		}
		return conflicts[i].AttemptedBy.Name < conflicts[j].AttemptedBy.Name
	})
	if len(newOwners) == 0 {
		newOwners = nil
	}
	return desiredRolesDecision{Roles: roles, Conflicts: conflicts, RoleOwners: newOwners}
}

func collapseClaims(claims []roleClaim) []roleClaim {
	seen := map[enterprisev4.RoleOwnerReference]struct{}{}
	out := make([]roleClaim, 0, len(claims))
	for _, claim := range claims {
		if _, ok := seen[claim.Owner]; ok {
			continue
		}
		seen[claim.Owner] = struct{}{}
		out = append(out, claim)
	}
	return out
}

func managedRoleFromClaim(claim roleClaim) managedRole {
	return managedRole{
		Name:   claim.Role.Name,
		Exists: true,
		PasswordSecretRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: claim.Role.SecretRef.Name},
			Key:                  "password",
		},
	}
}

func sameOwner(a, b enterprisev4.RoleOwnerReference) bool {
	return a.Name == b.Name && a.UID == b.UID
}

func hasLegacyDatabaseStatus(databases []enterprisev4.PostgresDatabase) bool {
	for _, db := range databases {
		for _, info := range db.Status.Databases {
			if len(info.Roles) == 0 {
				return true
			}
		}
	}
	return false
}

func retainCurrentOwnedRoles(owners map[string]enterprisev4.RoleOwnerReference, currentRoles map[string]managedRole) []managedRole {
	roles := make([]managedRole, 0, len(owners))
	for roleName := range owners {
		if role, ok := currentRoles[roleName]; ok {
			roles = append(roles, role)
		}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles
}

func retainAllCurrentRoles(currentRoles map[string]managedRole) []managedRole {
	roles := make([]managedRole, 0, len(currentRoles))
	for _, role := range currentRoles {
		roles = append(roles, role)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Name < roles[j].Name })
	return roles
}

func reconciledAbsentRoles(cnpgCluster *cnpgv1.Cluster, currentRoles map[string]managedRole) map[string]struct{} {
	reconciled := map[string]struct{}{}
	if cnpgCluster == nil {
		return reconciled
	}
	for _, roleName := range cnpgCluster.Status.ManagedRolesStatus.ByStatus[cnpgv1.RoleStatusReconciled] {
		if role, ok := currentRoles[roleName]; ok && !role.Exists {
			reconciled[roleName] = struct{}{}
		}
	}
	return reconciled
}

func currentManagedRolesFromCNPG(cnpgCluster *cnpgv1.Cluster) map[string]managedRole {
	roles := map[string]managedRole{}
	if cnpgCluster == nil || cnpgCluster.Spec.Managed == nil {
		return roles
	}
	for _, role := range cnpgCluster.Spec.Managed.Roles {
		managed := managedRole{Name: role.Name, Exists: role.Ensure != cnpgv1.EnsureAbsent}
		if role.PasswordSecret != nil {
			managed.PasswordSecretRef = &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: role.PasswordSecret.Name},
				Key:                  "password",
			}
		}
		roles[role.Name] = managed
	}
	return roles
}

func syncManagedRolesStatusFromCNPG(cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, expected []managedRole, owners map[string]enterprisev4.RoleOwnerReference, conflicts []enterprisev4.RoleConflict) {
	if cluster == nil || cnpgCluster == nil {
		return
	}

	expectedSet := make(map[string]struct{}, len(expected))
	for _, role := range expected {
		expectedSet[role.Name] = struct{}{}
	}

	cnpgStatus := cnpgCluster.Status.ManagedRolesStatus
	reconciled := filterExpected(cnpgStatus.ByStatus[cnpgv1.RoleStatusReconciled], expectedSet)
	pending := filterExpected(cnpgStatus.ByStatus[cnpgv1.RoleStatusPendingReconciliation], expectedSet)

	reconciledSet := make(map[string]struct{}, len(reconciled))
	for _, roleName := range reconciled {
		reconciledSet[roleName] = struct{}{}
	}
	pendingSet := make(map[string]struct{}, len(pending))
	for _, roleName := range pending {
		pendingSet[roleName] = struct{}{}
	}

	failed := make(map[string]string, len(cnpgStatus.CannotReconcile))
	for roleName, errs := range cnpgStatus.CannotReconcile {
		if _, expected := expectedSet[roleName]; !expected {
			continue
		}
		if len(errs) == 0 {
			failed[roleName] = "role cannot be reconciled"
			continue
		}
		failed[roleName] = strings.Join(errs, "; ")
	}

	for roleName := range expectedSet {
		if _, ok := reconciledSet[roleName]; ok {
			continue
		}
		if _, ok := failed[roleName]; ok {
			continue
		}
		if _, ok := pendingSet[roleName]; ok {
			continue
		}
		pending = append(pending, roleName)
	}

	sort.Strings(reconciled)
	sort.Strings(pending)
	if len(reconciled) == 0 {
		reconciled = nil
	}
	if len(pending) == 0 {
		pending = nil
	}
	if len(failed) == 0 {
		failed = nil
	}
	if len(conflicts) == 0 {
		conflicts = nil
	}
	if len(owners) == 0 {
		owners = nil
	}

	cluster.Status.ManagedRolesStatus = &enterprisev4.ManagedRolesStatus{
		Reconciled: reconciled,
		Pending:    pending,
		Failed:     failed,
		RoleOwners: owners,
		Conflicts:  conflicts,
	}
}

func filterExpected(values []string, expected map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := expected[value]; ok {
			out = append(out, value)
		}
	}
	return out
}

// reconcileManagedRoles synchronizes computed ManagedRoles to CNPG Cluster managed.roles.
func reconcileManagedRoles(ctx context.Context, c client.Client, roles []managedRole, cnpgCluster *cnpgv1.Cluster) error {
	logger := logging.FromContext(ctx).With("func", "reconcileManagedRoles")

	desiredRoles := make([]cnpgv1.RoleConfiguration, 0, len(roles))
	for _, role := range roles {
		r := cnpgv1.RoleConfiguration{
			Name:   role.Name,
			Ensure: cnpgv1.EnsureAbsent,
		}
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
		logger.InfoContext(ctx, "CNPG Cluster roles already match desired state, no update needed")
		return nil
	}

	logger.InfoContext(ctx, "CNPG Cluster roles drift detected, update started",
		"currentCount", len(currentRoles), "desiredCount", len(desiredRoles))

	originalCluster := cnpgCluster.DeepCopy()
	if cnpgCluster.Spec.Managed == nil {
		cnpgCluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	cnpgCluster.Spec.Managed.Roles = desiredRoles

	if err := c.Patch(ctx, cnpgCluster, client.MergeFrom(originalCluster)); err != nil {
		return fmt.Errorf("patching CNPG Cluster managed roles: %w", err)
	}
	logger.InfoContext(ctx, "CNPG Cluster managed roles updated", "roleCount", len(desiredRoles))
	return nil
}
