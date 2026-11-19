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

package majorupgradeadapter

import (
	"context"
	"errors"
	"fmt"
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// candidateRejectAll is the one-way fence rule for a cancelled green. It
// precedes every other rule, so only local CNPG management access remains.
const candidateRejectAll = "host all all all reject"

type candidateFenceProbe interface {
	// VerifyCandidateFence reports whether every addressable instance rejects
	// each role because of HBA or LOGIN.
	VerifyCandidateFence(ctx context.Context, green *cnpgv1.Cluster, attemptID string, roles []string) (bool, error)
}

// BlueGreenCancellation implements the pre-commit cancellation port from the
// switchover's restoration, the replication teardown, and a one-way green
// fence.
type BlueGreenCancellation struct {
	*BlueGreenReplicationTeardown
	client        client.Client
	switchover    *BlueGreenSwitchover
	probe         candidateFenceProbe
	subscriberSQL subscriptionRetirementSQL
}

var _ bluegreen.Canceller = (*BlueGreenCancellation)(nil)

func NewBlueGreenCancellation(c client.Client, reader client.Reader, key types.NamespacedName, owner *platformv1alpha1.PostgresCluster, switchover *BlueGreenSwitchover) (*BlueGreenCancellation, error) {
	if switchover == nil {
		return nil, errors.New("blue/green cancellation requires the switchover runtime")
	}
	teardown, err := NewBlueGreenReplicationTeardown(c, reader, key, owner)
	if err != nil {
		return nil, err
	}
	probe, err := newPostgresSwitchoverSQL(liveReadClient{Client: c, reader: reader}, key.Name, owner)
	if err != nil {
		return nil, fmt.Errorf("construct candidate fence probe: %w", err)
	}
	result := &BlueGreenCancellation{BlueGreenReplicationTeardown: teardown, client: c, switchover: switchover, probe: probe}
	result.subscriberSQL = probe
	return result, nil
}

func (c *BlueGreenCancellation) CommitObserved(ctx context.Context, attemptID string) (bool, error) {
	_, status, err := c.switchover.readAttempt(ctx, attemptID)
	if err != nil {
		return false, err
	}
	// No endpoint commit can be authorized before green is recorded.
	if status.Green == nil && status.CommitReceipt == nil {
		return false, nil
	}
	return c.switchover.CommitObserved(ctx, attemptID)
}

func (c *BlueGreenCancellation) RestoreSource(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if changed, err := c.RecoverGreen(ctx, status); err != nil || changed {
		// Persist the discovered identity before restoring blue or deleting
		// replication resources. A lost status write repeats discovery.
		return false, err
	}
	return c.switchover.RestoreForCancellation(ctx, intent.AttemptID, status)
}

// FenceCandidate disables every managed green login and rejects all remote
// access. The fence is one-way: a cancelled green is never reopened.
func (c *BlueGreenCancellation) FenceCandidate(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if changed, err := c.RecoverGreen(ctx, status); err != nil || changed {
		return false, err
	}
	green, err := c.AttemptGreen(ctx, status)
	if err != nil {
		return false, err
	}
	if green == nil {
		return c.handBackUnpreparedSource(ctx, status)
	}
	if !candidateFenced(green) {
		original := green.DeepCopy()
		fenceCandidate(green)
		if err := c.client.Patch(ctx, green, client.MergeFromWithOptions(original, client.MergeFromWithOptimisticLock{})); err != nil {
			return false, fmt.Errorf("fencing retained green: %w", err)
		}
		return false, nil
	}
	if pending := green.Status.ManagedRolesStatus.ByStatus[cnpgv1.RoleStatusPendingReconciliation]; len(pending) > 0 {
		return false, nil
	}
	return c.probe.VerifyCandidateFence(ctx, green, status.AttemptID, probedRoles(green))
}

// An early Scheduled attempt has no Blue reference and authorized no source
// artifacts. Save the authoritative source identity only after teardown, so
// subsequent passes do not mistake this handback for prepared SQL inventory.
// Ordinary reconciliation retains its exact UID/version checks after cancel.
func (c *BlueGreenCancellation) handBackUnpreparedSource(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	if status.Blue != nil {
		return true, nil
	}
	var cancellation *platformv1alpha1.BlueGreenCancellationStatus
	if status.Gates != nil {
		cancellation = status.Gates.Cancellation
	}
	if status.SourcePreparation != nil || status.SourceConfigurationFingerprint != nil || len(status.CredentialFingerprints) != 0 ||
		len(status.DatabaseReplication) != 0 || status.Green != nil || status.Switchover != nil ||
		cancellation == nil || cancellation.SourceAccessRestoredAt == nil || cancellation.ReplicationRemovedAt == nil || cancellation.CredentialsRemovedAt == nil {
		return false, fmt.Errorf("%w: missing source identity after preparation or before teardown confirmation", mvutypes.ErrUpgradeFlowFailed)
	}
	parent, _, err := c.switchover.readAttempt(ctx, status.AttemptID)
	if err != nil {
		return false, err
	}
	ref := parent.Status.ProvisionerRef
	if ref == nil || ref.Name == "" || ref.UID == "" {
		return false, fmt.Errorf("%w: early cancellation requires an authoritative source identity", mvutypes.ErrUpgradeFlowFailed)
	}
	blue, err := c.environment(ctx, &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: *ref})
	if err != nil {
		return false, err
	}
	entry := *mvutypes.LatestAttempt(parent.Status.PostgresMajorUpgradeStatus)
	if blue == nil || entry.SourcePgVersion == nil || blue.Status.PGDataImageInfo == nil || blue.Status.PGDataImageInfo.MajorVersion <= 0 ||
		!samePostgresMajor(*entry.SourcePgVersion, fmt.Sprintf("%d", blue.Status.PGDataImageInfo.MajorVersion)) {
		return false, fmt.Errorf("%w: authoritative source no longer matches the cancelled attempt's source major", mvutypes.ErrUpgradeFlowFailed)
	}
	status.Blue = &platformv1alpha1.BlueGreenEnvironmentStatus{Ref: corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Namespace: blue.Namespace, Name: blue.Name, UID: blue.UID}}
	return false, nil // Persist the exact handback identity before completion.
}

func candidateFenced(green *cnpgv1.Cluster) bool {
	hba := green.Spec.PostgresConfiguration.PgHBA
	return len(hba) > 0 && hba[0] == candidateRejectAll &&
		!slices.ContainsFunc(managedRoles(green), func(role cnpgv1.RoleConfiguration) bool { return role.Login })
}

func fenceCandidate(green *cnpgv1.Cluster) {
	for i := range managedRoles(green) {
		green.Spec.Managed.Roles[i].Login = false
	}
	if hba := green.Spec.PostgresConfiguration.PgHBA; len(hba) == 0 || hba[0] != candidateRejectAll {
		green.Spec.PostgresConfiguration.PgHBA = append([]string{candidateRejectAll}, hba...)
	}
}

// probedRoles includes the superuser, which the fence also rejects remotely,
// and every managed role that has a credential to try.
func probedRoles(green *cnpgv1.Cluster) []string {
	roles := []string{"postgres"}
	for _, role := range managedRoles(green) {
		if role.PasswordSecret != nil {
			roles = append(roles, role.Name)
		}
	}
	return roles
}
