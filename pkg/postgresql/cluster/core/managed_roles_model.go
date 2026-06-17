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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
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

	if err := reconcileManagedRoles(ctx, m.client, m.cluster, m.contracts.CNPGCluster); err != nil {
		return newReconcileFailure(reasonManagedRolesFailed, err)
	}
	return nil
}

// needsCredentialSweep gates the sweep to run exactly once per restore: only for
// backup-bootstrapped clusters, and only until completion is recorded in status.
func (m *managedRolesModel) needsCredentialSweep() bool {
	if m.cluster.Spec.BootstrapFrom == nil || m.cluster.Spec.BootstrapFrom.VolumeSnapshot == nil {
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
	pw := string(m.contracts.Secret.Data[secretKeyPassword])
	rwHost := fmt.Sprintf("%s-rw.%s", m.contracts.CNPGCluster.Name, m.cluster.Namespace)

	repo, err := m.newRoleSweeper(ctx, rwHost, defaultDatabaseName, pw)
	if err != nil {
		if errors.Is(err, ports.ErrSweeperConnectTerminal) {
			return fmt.Errorf("%w: %w", errSweepTerminal, err)
		}
		return fmt.Errorf("%w: %w", errSweepConnect, err)
	}

	if err := repo.SweepUnmanagedRolesAfterRestore(ctx); err != nil {
		return fmt.Errorf("%w: %w", errSweepTerminal, err)
	}
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
	// retrying — surface Failed with the wrapped cause.
	if errors.Is(reconcileErr, errSweepTerminal) {
		m.events.emitWarning(m.cluster, EventUnmanagedRolesSweepFailed, fmt.Sprintf("failed to sweep unmanaged roles for PostgresCluster %s — check operator logs", m.cluster.Name))
		return newFailedHealth(managedRolesReady, reasonManagedRolesFailed, fmt.Sprintf("Failed to sweep unmanaged roles: %v", reconcileErr)), reconcileErr
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

	syncManagedRolesStatusFromCNPG(m.cluster, m.contracts.CNPGCluster)
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
	snapshotName := m.cluster.Spec.BootstrapFrom.VolumeSnapshot.Storage
	m.cluster.Status.Restore = &enterprisev4.RestoreStatus{
		Source:          enterprisev4.RestoreSourceStatus{VolumeSnapshot: &snapshotName},
		CredentialSweep: enterprisev4.RestoreCredentialSweepStatus{Completed: true},
	}
	m.events.emitNormal(m.cluster, EventUnmanagedRolesSweepDone, fmt.Sprintf("unmanaged login roles disabled for PostgresCluster %s", m.cluster.Name))
	return newProvisioningHealth(managedRolesReady, reasonManagedRolesPending, "Credential sweep completed, waiting for managed roles to be re-enabled")
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

// TODO: Ports as access to cnpg originated info to decouple.
func syncManagedRolesStatusFromCNPG(cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) {
	if cluster == nil || cnpgCluster == nil {
		return
	}

	expectedRoles := make([]string, 0, len(cluster.Spec.ManagedRoles))
	for _, role := range cluster.Spec.ManagedRoles {
		expectedRoles = append(expectedRoles, role.Name)
	}

	cnpgStatus := cnpgCluster.Status.ManagedRolesStatus
	reconciled := append([]string(nil), cnpgStatus.ByStatus[cnpgv1.RoleStatusReconciled]...)
	pending := append([]string(nil), cnpgStatus.ByStatus[cnpgv1.RoleStatusPendingReconciliation]...)

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
		if len(errs) == 0 {
			failed[roleName] = "role cannot be reconciled"
			continue
		}
		failed[roleName] = strings.Join(errs, "; ")
	}

	for _, roleName := range expectedRoles {
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
	if len(failed) == 0 {
		failed = nil
	}

	cluster.Status.ManagedRolesStatus = &enterprisev4.ManagedRolesStatus{
		Reconciled: reconciled,
		Pending:    pending,
		Failed:     failed,
	}
}

// reconcileManagedRoles synchronizes ManagedRoles from PostgresCluster spec to CNPG Cluster managed.roles.
func reconcileManagedRoles(ctx context.Context, c client.Client, cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster) error {
	logger := logging.FromContext(ctx).With("func", "reconcileManagedRoles")

	if len(cluster.Spec.ManagedRoles) == 0 {
		logger.InfoContext(ctx, "no managed roles to reconcile")
		return nil
	}

	desiredRoles := make([]cnpgv1.RoleConfiguration, 0, len(cluster.Spec.ManagedRoles))
	for _, role := range cluster.Spec.ManagedRoles {
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
