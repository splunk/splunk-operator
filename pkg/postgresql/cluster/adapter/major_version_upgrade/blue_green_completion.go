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
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationtypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// completionSQL verifies database state beyond Kubernetes status.
type completionSQL interface {
	SourceStopped(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	TargetArchivingReady(context.Context, *cnpgv1.Cluster, string) (bool, error)
	VerifyRecovery(context.Context, *cnpgv1.Cluster, *cnpgv1.Backup, string) (bool, error)
	RemoveGreenReplication(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error
	RemoveSourceReplication(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	GreenAccessible(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
}

// BlueGreenCompletion makes committed green recoverable and retires replication.
type BlueGreenCompletion struct {
	switchover *BlueGreenSwitchover
	teardown   *BlueGreenReplicationTeardown
	config     *clustercore.MergedConfig
	sql        completionSQL
}

// NewBlueGreenCompletion wires committed recovery.
func NewBlueGreenCompletion(switchover *BlueGreenSwitchover, teardown *BlueGreenReplicationTeardown, config *clustercore.MergedConfig) (*BlueGreenCompletion, error) {
	if switchover == nil || teardown == nil {
		return nil, fmt.Errorf("completion requires switchover and teardown adapters")
	}
	sql, ok := switchover.sql.(*postgresSwitchoverSQL)
	if !ok {
		return nil, fmt.Errorf("completion requires the PostgreSQL cutover SQL adapter")
	}
	live := liveReadClient{Client: switchover.client, reader: switchover.reader}
	return &BlueGreenCompletion{switchover: switchover, teardown: teardown, config: config,
		sql: &postgresCompletionSQL{switchover: sql, access: switchover.access, teardown: newPostgresTeardownSQL(live, switchover.key.Name, switchover.owner)}}, nil
}

// AdvanceCompletion advances one checkpoint while keeping green authoritative.
func (c *BlueGreenCompletion) AdvanceCompletion(ctx context.Context, intent mvutypes.Intent, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bluegreen.Outcome, error) {
	ctx, cancel := context.WithTimeout(ctx, cutoverRecoveryTimeout)
	defer cancel()
	s := c.switchover
	parent, status, err := s.readAttempt(ctx, intent.AttemptID)
	if err != nil {
		return bluegreen.Outcome{}, err
	}
	if mvutypes.CutoverStageOfStatus(status) != mvutypes.CutoverHandedOff {
		return c.pendingCompletion(status, "Waiting for durable green availability and endpoint publication.", nil)
	}
	if err := s.endpoints.RecoverCanonical(ctx, s.key, intent.AttemptID); err != nil {
		return c.pendingCompletion(status, "Repairing committed green routing.", err)
	}
	published, err := s.endpoints.ReconcilePublicationStatus(ctx, s.key, intent.AttemptID)
	if err != nil || !published {
		return c.pendingCompletion(status, "Waiting for every database endpoint to publish green.", err)
	}
	green, err := s.environment(ctx, status.Green)
	if err != nil {
		return c.pendingCompletion(status, "Checking green identity.", err)
	}
	if !cnpginfra.ClusterReady(green) {
		return c.pendingCompletion(status, "Waiting for healthy green workloads.", nil)
	}
	handedOff, err := c.TransferBackupOwnership(ctx, intent, status)
	if errors.Is(err, mvutypes.ErrUpgradeFlowPending) {
		return c.pendingCompletion(status, err.Error(), nil)
	}
	if err != nil || !handedOff {
		return c.pendingCompletion(status, "Transferring backup and WAL production to green.", err)
	}
	backup, err := c.verifiedTargetBackup(ctx, parent, intent, status, green)
	if err != nil || backup == nil {
		return c.pendingCompletion(status, "Waiting for the target backup and its WAL archive baseline.", err)
	}
	if !mvutypes.BlueGreenRecoverable(status) {
		now := metav1.Now()
		status.Recoverability = &platformv1alpha1.BlueGreenRecoverabilityStatus{TargetBackupName: new(backup.Name), BackupVerifiedAt: &now, WALArchiveBaselineVerifiedAt: &now}
		return c.pendingCompletion(status, "The target-major backup and WAL archive baseline are verified.", nil)
	}
	return c.advanceTeardown(ctx, status, green)
}

// advanceTeardown persists each cleanup checkpoint before starting the next.
func (c *BlueGreenCompletion) advanceTeardown(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, green *cnpgv1.Cluster) (bluegreen.Outcome, error) {
	if status.Completion.ReplicationRemovedAt == nil {
		removed, err := c.removeReplication(ctx, status, green)
		if err != nil || !removed {
			return c.pendingCompletion(status, "Removing committed replication artifacts.", err)
		}
		status.Completion.ReplicationRemovedAt = new(metav1.Now())
		return c.pendingCompletion(status, "Replication teardown is verified.", nil)
	}
	if status.Completion.SourceCredentialsRetiredAt == nil {
		retired, err := c.retireSourceCredentials(ctx, status)
		if err != nil || !retired {
			return c.pendingCompletion(status, "Retiring source replication and control credentials behind its fence.", err)
		}
		status.Completion.SourceCredentialsRetiredAt = new(metav1.Now())
		return c.pendingCompletion(status, "Source credential retirement is verified.", nil)
	}
	if status.Completion.CredentialsRemovedAt == nil {
		if err := c.teardown.sql.DropAttemptRoles(ctx, green, c.teardown.databaseNames(status), []string{replicationRoleName(status.AttemptID), cutoverRoleName(status.AttemptID)}); err != nil {
			return c.pendingCompletion(status, "Removing target temporary credentials.", err)
		}
		gone, err := c.teardown.deleteSecrets(ctx, status.AttemptID)
		if err != nil || !gone {
			return c.pendingCompletion(status, "Removing attempt credential Secrets.", err)
		}
		status.Completion.CredentialsRemovedAt = new(metav1.Now())
		return c.pendingCompletion(status, "Attempt credentials are removed; verifying completion.", nil)
	}
	retired, err := c.retireSourceCredentials(ctx, status)
	if err != nil || !retired {
		return c.pendingCompletion(status, "Verifying the retained source fence.", err)
	}
	open, err := c.sql.GreenAccessible(ctx, green, status)
	if err != nil || !open {
		return c.pendingCompletion(status, "Verifying ordinary green read/write access.", err)
	}
	return bluegreen.Outcome{BlueGreen: status, Report: reconciliationtypes.Report{Name: mvutypes.UseCaseName, Phase: string(mvutypes.Completed), Reason: "BlueGreenFinalized", Message: "Green is available and recoverable; replication and temporary credentials are removed. Fenced blue is retained until cleanup."}}, nil
}

// pendingCompletion reports retries without losing recovery evidence.
func (c *BlueGreenCompletion) pendingCompletion(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, message string, cause error) (bluegreen.Outcome, error) {
	out := c.switchover.pending(status, message)
	out.Report.Phase = string(mvutypes.PostUpgradeBackup)
	if mvutypes.BlueGreenRecoverable(status) {
		out.Report.Phase = string(mvutypes.Recoverable)
	}
	if cause != nil {
		out.Report.Reason = mvutypes.ReasonPostUpgradeBackupNotReady
		out.Report.Message = fmt.Sprintf("%s: %v", message, cause)
	}
	return out, nil
}

// removeReplication removes owned replication resources and their SQL objects.
func (c *BlueGreenCompletion) removeReplication(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, green *cnpgv1.Cluster) (bool, error) {
	t := c.teardown
	for _, objects := range []client.ObjectList{&cnpgv1.SubscriptionList{}, &cnpgv1.PublicationList{}} {
		gone, err := t.deleteOwned(ctx, objects, status, func(object client.Object) error {
			if subscription, ok := object.(*cnpgv1.Subscription); ok {
				return quiesceRetainedSubscription(ctx, t.client, subscription)
			}
			return t.releaseRetainedFinalizer(ctx, object, cnpgutils.PublicationFinalizerName)
		})
		if err != nil || !gone {
			return false, err
		}
	}
	if err := c.sql.RemoveGreenReplication(ctx, green, status); err != nil {
		return false, err
	}
	blue, err := c.fencedSource(ctx, status)
	if err != nil {
		return false, err
	}
	if blue != nil {
		fenced, err := c.switchover.access.FenceBlue(ctx, blue, status)
		if err != nil || !fenced {
			return false, errors.Join(err, fmt.Errorf("retained blue fence is not verified"))
		}
		gone, err := c.sql.RemoveSourceReplication(ctx, blue, status)
		if err != nil || !gone {
			return false, err
		}
	}
	return t.removeReplicationSource(ctx, green)
}

// fencedSource verifies retained blue's identity and application fence.
func (c *BlueGreenCompletion) fencedSource(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (*cnpgv1.Cluster, error) {
	blue, err := c.switchover.retiredSource(ctx, status)
	if err != nil || blue == nil {
		return blue, err
	}
	if !retiredAccessMatches(blue, status) {
		return nil, fmt.Errorf("retained blue application fence changed")
	}
	return blue, nil
}

// retiredAccessMatches checks blue's fence, allowing disabled attempt roles.
func retiredAccessMatches(blue *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	copy := blue.DeepCopy()
	if copy.Spec.Managed != nil {
		copy.Spec.Managed.Roles = slices.DeleteFunc(copy.Spec.Managed.Roles, func(role cnpgv1.RoleConfiguration) bool {
			return isRetiredMaintenanceRole(role, status.AttemptID)
		})
	}
	return accessMatches(copy, status, sideBlue, fenceApplied)
}

// isRetiredMaintenanceRole matches this attempt's disabled, unprivileged roles.
func isRetiredMaintenanceRole(role cnpgv1.RoleConfiguration, attemptID string) bool {
	attemptRole := role.Name == cutoverRoleName(attemptID) || role.Name == replicationRoleName(attemptID)
	disabled := role.Ensure == cnpgv1.EnsurePresent && !role.Login && role.DisablePassword && role.PasswordSecret == nil
	unprivileged := !role.Superuser && !role.CreateDB && !role.CreateRole &&
		!role.Replication && !role.BypassRLS && len(role.InRoles) == 0
	return attemptRole && disabled && unprivileged
}

// retireSourceCredentials disables attempt passwords through CNPG behind blue's fence.
func (c *BlueGreenCompletion) retireSourceCredentials(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	blue, err := c.fencedSource(ctx, status)
	if err != nil || blue == nil {
		return err == nil, err
	}
	before := blue.DeepCopy()
	if blue.Spec.Managed == nil {
		blue.Spec.Managed = &cnpgv1.ManagedConfiguration{}
	}
	for _, name := range []string{cutoverRoleName(status.AttemptID), replicationRoleName(status.AttemptID)} {
		if !slices.ContainsFunc(blue.Spec.Managed.Roles, func(role cnpgv1.RoleConfiguration) bool { return role.Name == name }) {
			blue.Spec.Managed.Roles = append(blue.Spec.Managed.Roles, cnpgv1.RoleConfiguration{Name: name, Ensure: cnpgv1.EnsurePresent, Login: false, DisablePassword: true, ConnectionLimit: -1})
		}
	}
	if !reflect.DeepEqual(before.Spec.Managed, blue.Spec.Managed) {
		return false, c.switchover.client.Patch(ctx, blue, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	return sourceRolesRetired(blue, status), nil
}

// sourceRolesRetired requires CNPG proof that attempt roles and passwords are disabled.
func sourceRolesRetired(blue *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
	roles := blue.Status.ManagedRolesStatus
	for _, name := range []string{cutoverRoleName(status.AttemptID), replicationRoleName(status.AttemptID)} {
		if !slices.Contains(roles.ByStatus[cnpgv1.RoleStatusReconciled], name) || len(roles.CannotReconcile[name]) != 0 {
			return false
		}
		password := roles.PasswordStatus[name]
		if password.TransactionID == 0 || password.SecretResourceVersion != "" {
			return false
		}
	}
	return true
}

// TransferBackupOwnership stops blue producers before enabling green.
// New checkpoints return false until the caller persists them.
func (c *BlueGreenCompletion) TransferBackupOwnership(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	s := c.switchover
	if !mvutypes.CutoverStageOfStatus(status).Committed() {
		return false, fmt.Errorf("backup handoff requires a durable green commit")
	}
	if status.Completion == nil {
		status.Completion = &platformv1alpha1.BlueGreenCompletionStatus{StartedAt: new(metav1.Now())}
		return false, nil
	}
	if status.Completion.StartedAt == nil {
		return false, fmt.Errorf("backup handoff has no durable start time")
	}
	if _, _, configured := clustercore.EffectiveBackupProvider(c.config); !configured {
		return false, mvutypes.ErrBackupProviderMissing
	}
	green, err := s.environment(ctx, status.Green)
	if err != nil {
		return false, err
	}
	blue, err := c.fencedSource(ctx, status)
	if err != nil {
		return false, err
	}
	if blue != nil {
		if status.Completion.SourceStoppedAt != nil && !sourceProtectionDisabled(blue) {
			return false, fmt.Errorf("retired source backup or archive production was re-enabled")
		}
		stopped, err := c.stopSourceBackups(ctx, blue, status)
		if err != nil || !stopped {
			return false, err
		}
	}
	if status.Completion.SourceStoppedAt == nil {
		status.Completion.SourceStoppedAt = new(metav1.Now())
		return false, nil
	}
	return c.enableGreenBackups(ctx, green, status.AttemptID)
}

// sourceProtectionDisabled checks that blue's backup and archive settings stay off.
func sourceProtectionDisabled(blue *cnpgv1.Cluster) bool {
	return blue.Annotations[cnpgutils.SkipWalArchiving] == "enabled" && blue.Spec.Backup == nil &&
		!slices.ContainsFunc(blue.Spec.Plugins, isWALArchiver)
}

func isWALArchiver(plugin cnpgv1.PluginConfiguration) bool {
	return plugin.IsWALArchiver != nil && *plugin.IsWALArchiver
}

// stopSourceBackups drains backups and disables archiving, retaining recovery anchors.
func (c *BlueGreenCompletion) stopSourceBackups(ctx context.Context, blue *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	s := c.switchover
	schedules := &cnpgv1.ScheduledBackupList{}
	if err := s.reader.List(ctx, schedules, client.InNamespace(blue.Namespace)); err != nil {
		return false, err
	}
	for i := range schedules.Items {
		schedule := &schedules.Items[i]
		if schedule.Spec.Cluster.Name != blue.Name {
			continue
		}
		if !metav1.IsControlledBy(schedule, s.owner) {
			return false, fmt.Errorf("source backup schedule %q is not owned by this cluster", schedule.Name)
		}
		return false, c.teardown.deleteExact(ctx, schedule)
	}
	backups := &cnpgv1.BackupList{}
	if err := s.reader.List(ctx, backups, client.InNamespace(blue.Namespace)); err != nil {
		return false, err
	}
	for i := range backups.Items {
		backup := &backups.Items[i]
		if backup.Spec.Cluster.Name != blue.Name || backup.Status.Phase == cnpgv1.BackupPhaseCompleted || backup.Status.Phase == cnpgv1.BackupPhaseFailed {
			continue
		}
		if !metav1.IsControlledBy(backup, blue) && !metav1.IsControlledBy(backup, s.owner) {
			return false, fmt.Errorf("unowned source backup %q prevents producer shutdown", backup.Name)
		}
		if backup.Status.Phase == cnpgv1.BackupPhasePending && backup.Status.StartedAt == nil {
			return false, c.teardown.deleteExact(ctx, backup)
		}
		return false, nil
	}
	before := blue.DeepCopy()
	if blue.Annotations == nil {
		blue.Annotations = make(map[string]string)
	}
	blue.Annotations[cnpgutils.SkipWalArchiving] = "enabled"
	blue.Annotations[cnpgutils.ClusterRestartAnnotationName] = status.Completion.StartedAt.UTC().Format(time.RFC3339Nano)
	blue.Spec.Backup = nil
	blue.Spec.Plugins = slices.DeleteFunc(blue.Spec.Plugins, isWALArchiver)
	// Restart fenced blue automatically to stop its archive producer.
	blue.Spec.PrimaryUpdateStrategy = cnpgv1.PrimaryUpdateStrategyUnsupervised
	if !reflect.DeepEqual(before.Spec, blue.Spec) || !reflect.DeepEqual(before.Annotations, blue.Annotations) {
		return false, s.client.Patch(ctx, blue, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	if status.Completion.SourceStoppedAt != nil {
		return true, nil
	}
	return c.sql.SourceStopped(ctx, blue, status)
}

// enableGreenBackups applies the class backup policy to green.
func (c *BlueGreenCompletion) enableGreenBackups(ctx context.Context, green *cnpgv1.Cluster, attemptID string) (bool, error) {
	s := c.switchover
	backup, plugins := clustercore.BackupConfigurationForEnvironment(c.config, green.Name)
	_, pluginName, _ := clustercore.EffectiveBackupProvider(c.config)
	if len(plugins) > 0 {
		ready, err := ensureAttemptObjectStore(ctx, liveReadClient{Client: s.client, reader: s.reader}, s.owner, green.Name, attemptID, c.config.CNPG.Backup.BarmanObjectStore)
		if err != nil || !ready {
			return false, err
		}
		active := slices.ContainsFunc(green.Spec.Plugins, func(plugin cnpgv1.PluginConfiguration) bool {
			return plugin.Name == pluginName && isWALArchiver(plugin)
		})
		if !active {
			// Older candidates may have no sidecars. Roll them out while the
			// plugin is not an archiver, then activate only after every Pod is ready.
			before := green.DeepCopy()
			green.Spec.Plugins = slices.DeleteFunc(green.Spec.Plugins, func(plugin cnpgv1.PluginConfiguration) bool { return plugin.Name == pluginName })
			green.Spec.Plugins = append(green.Spec.Plugins, stagedBackupPlugins(c.config, green.Name)...)
			if !reflect.DeepEqual(before.Spec, green.Spec) {
				return false, s.client.Patch(ctx, green, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
			}
			if _, err := c.targetBackupInstances(ctx, green); err != nil {
				return false, err
			}
		}
	}
	before := green.DeepCopy()
	green.Spec.Backup = backup
	green.Spec.Plugins = slices.DeleteFunc(green.Spec.Plugins, func(plugin cnpgv1.PluginConfiguration) bool { return plugin.Name == pluginName })
	green.Spec.Plugins = append(green.Spec.Plugins, plugins...)
	if !reflect.DeepEqual(before.Spec, green.Spec) {
		return false, s.client.Patch(ctx, green, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	if len(plugins) > 0 {
		if err := c.targetBackupReady(ctx, green); err != nil {
			return false, err
		}
	}
	backend := cnpginfra.NewBackupBackend(liveReadClient{Client: s.client, reader: s.reader}, s.client.Scheme())
	for _, provider := range clustercore.BackupSchedulesForEnvironment(c.config, green.Name, green.Namespace) {
		_, err := backend.EnsureScheduled(ctx, s.owner, provider)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// verifiedTargetBackup verifies backup and WAL without replacing a saved anchor.
func (c *BlueGreenCompletion) verifiedTargetBackup(ctx context.Context, parent *platformv1alpha1.PostgresCluster, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, green *cnpgv1.Cluster) (*cnpgv1.Backup, error) {
	if !mvutypes.BlueGreenRecoverable(status) {
		var err error
		intent.RetryRequestedAt, err = retryRequestedAt(parent.Annotations)
		if err != nil {
			return nil, err
		}
	}
	backup, err := c.targetBackup(ctx, intent, status, green)
	if err != nil || backup == nil {
		return nil, err
	}
	verified, err := c.sql.VerifyRecovery(ctx, green, backup, intent.TargetPgVersion)
	if err != nil || !verified {
		return nil, err
	}
	return backup, nil
}

// targetBackup creates or observes this attempt's backup on green.
func (c *BlueGreenCompletion) targetBackup(ctx context.Context, intent mvutypes.Intent, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, green *cnpgv1.Cluster) (*cnpgv1.Backup, error) {
	s := c.switchover
	_, plugin, configured := clustercore.EffectiveBackupProvider(c.config)
	if !configured {
		return nil, mvutypes.ErrBackupProviderMissing
	}
	identity := string(s.owner.UID) + "/" + intent.AttemptID
	if intent.RetryRequestedAt != nil && intent.RetryRequestedAt.After(status.CommitReceipt.CommittedAt.Time) {
		identity += "/" + intent.RetryRequestedAt.UTC().Format(time.RFC3339Nano)
	}
	digest := sha256.Sum256([]byte(identity))
	name := fmt.Sprintf("%s-bg-backup-%x", s.key.Name[:min(len(s.key.Name), 40)], digest[:6])
	if mvutypes.BlueGreenRecoverable(status) {
		name = *status.Recoverability.TargetBackupName
	}
	cnpgMethod := cnpgv1.BackupMethodVolumeSnapshot
	if plugin != "" {
		cnpgMethod = cnpgv1.BackupMethodPlugin
	}
	backup := &cnpgv1.Backup{}
	err := s.reader.Get(ctx, client.ObjectKey{Namespace: s.key.Namespace, Name: name}, backup)
	if apierrors.IsNotFound(err) {
		backup = &cnpgv1.Backup{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.key.Namespace,
			Labels: map[string]string{blueGreenAttemptLabel: intent.AttemptID}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(s.owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}},
			Spec: cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: green.Name}, Target: cnpgv1.BackupTargetPrimary, Method: cnpgMethod}}
		if plugin != "" {
			backup.Spec.PluginConfiguration = &cnpgv1.BackupPluginConfiguration{Name: plugin}
		}
		return nil, s.client.Create(ctx, backup)
	}
	if err != nil {
		return nil, err
	}
	if !backupBelongsToAttempt(backup, s.owner, intent.AttemptID) || backup.Spec.Cluster.Name != green.Name || !backupUsesBackend(backup, cnpgMethod, plugin) {
		return nil, fmt.Errorf("workflow backup identity or backend differs from the committed attempt")
	}
	if backup.Status.Phase == cnpgv1.BackupPhaseFailed {
		return nil, fmt.Errorf("target-major backup %q failed; correct its backend and set a newer post-commit %s timestamp to request a fresh backup", name, mvutypes.AnnotationMajorUpgradeRetryAt)
	}
	if backup.Status.Phase != cnpgv1.BackupPhaseCompleted {
		return nil, nil
	}
	if backup.Status.StartedAt == nil || backup.Status.StoppedAt == nil || backup.Status.StartedAt.Before(status.CommitReceipt.CommittedAt) {
		return nil, fmt.Errorf("target backup lacks post-commit completion evidence")
	}
	if r := status.Recoverability; r != nil && r.TargetBackupName != nil && *r.TargetBackupName != backup.Name {
		return nil, fmt.Errorf("target recovery anchor changed")
	}
	return backup, nil
}

// backupBelongsToAttempt checks ownership, attempt label, and deletion state.
func backupBelongsToAttempt(backup *cnpgv1.Backup, owner *platformv1alpha1.PostgresCluster, attemptID string) bool {
	return backup.DeletionTimestamp == nil && metav1.IsControlledBy(backup, owner) && backup.Labels[blueGreenAttemptLabel] == attemptID
}

// backupUsesBackend checks the backup method and plugin.
func backupUsesBackend(backup *cnpgv1.Backup, method cnpgv1.BackupMethod, plugin string) bool {
	configured := ptr.Deref(backup.Spec.PluginConfiguration, cnpgv1.BackupPluginConfiguration{})
	return backup.Spec.Method == method && (plugin == "" || configured.Name == plugin)
}
