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
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	bluegreen "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/use_case/blue_green"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type completionSQLFake struct {
	stopped, archived, accessible bool
	teardownErr                   error
	sourceCalls, greenCalls       int
}

func (p *completionSQLFake) SourceStopped(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return p.stopped, nil
}
func (p *completionSQLFake) VerifyRecovery(context.Context, *cnpgv1.Cluster, *cnpgv1.Backup, string) (bool, error) {
	return p.archived, nil
}
func (p *completionSQLFake) RemoveGreenReplication(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	p.greenCalls++
	return p.teardownErr
}
func (p *completionSQLFake) RemoveSourceReplication(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	p.sourceCalls++
	return p.teardownErr == nil, p.teardownErr
}
func (p *completionSQLFake) GreenAccessible(context.Context, *cnpgv1.Cluster, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return p.accessible, nil
}

// completionFixture persists a real handoff and fakes SQL observations.
func completionFixture(t *testing.T) (*cutoverFixture, *BlueGreenCompletion, *completionSQLFake) {
	t.Helper()
	f := newCutoverFixture(t)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	parent := new(platformv1alpha1.PostgresCluster)
	require.NoError(t, f.runtime.reader.Get(t.Context(), f.runtime.key, parent))
	f.runtime.owner = parent
	f.mutate(func(parent *platformv1alpha1.PostgresCluster, _ *platformv1alpha1.PostgresBlueGreenUpgradeStatus) {
		parent.Status.PostgresMajorUpgradeStatus[0].BackupNames = &platformv1alpha1.UpgradeBackupNames{PreUpgrade: new("source-anchor")}
	})
	status := f.status()
	for _, side := range []environmentSide{sideBlue, sideGreen} {
		ref := status.Blue
		if side == sideGreen {
			ref = status.Green
		}
		cluster := &cnpgv1.Cluster{}
		require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: parent.Namespace, Name: ref.Ref.Name}, cluster))
		cluster.Spec.Managed = &cnpgv1.ManagedConfiguration{}
		cluster.Spec.PostgresConfiguration.PgHBA = []string{}
		if side == sideBlue {
			cluster.Spec.PostgresConfiguration.PgHBA = fencedHBA(status, []string{})
		}
		cluster.Status.Instances, cluster.Status.ReadyInstances, cluster.Status.CurrentPrimary = 1, 1, cluster.Name+"-1"
		cluster.Status.Phase = cnpgv1.PhaseHealthy
		require.NoError(t, f.runtime.client.Update(t.Context(), cluster))
	}
	for _, name := range []string{replicationSecretName(parent.Name, status.AttemptID), controlSecretName(parent.Name, status.AttemptID)} {
		secret := &corev1.Secret{}
		err := f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: parent.Namespace, Name: name}, secret)
		if err != nil {
			secret = &corev1.Secret{Name: name, Namespace: parent.Namespace, UID: types.UID("owned-" + name)}
		}
		secret.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(parent, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}
		secret.Labels = map[string]string{blueGreenAttemptLabel: status.AttemptID}
		if err != nil {
			require.NoError(t, f.runtime.client.Create(t.Context(), secret))
		} else {
			require.NoError(t, f.runtime.client.Update(t.Context(), secret))
		}
	}
	teardown, err := NewBlueGreenReplicationTeardown(f.runtime.client, f.runtime.reader, f.runtime.key, parent)
	require.NoError(t, err)
	teardown.sql = &teardownSQLFake{}
	sql := &completionSQLFake{stopped: true, accessible: true}
	c := &BlueGreenCompletion{switchover: f.runtime, teardown: teardown, sql: sql, config: &clustercore.MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{Backup: &platformv1alpha1.BackupConfig{Enabled: new(true), Schedule: new("0 2 * * *")}},
		CNPG: &platformv1alpha1.CNPGConfig{Backup: &platformv1alpha1.CNPGBackupConfig{VolumeSnapshot: &platformv1alpha1.CNPGVolumeSnapshotConfig{}}},
	}}
	return f, c, sql
}

// completionStep saves one pass through the concurrency-checked state store.
func completionStep(t *testing.T, f *cutoverFixture, c *BlueGreenCompletion) bluegreen.Outcome {
	t.Helper()
	parent := new(platformv1alpha1.PostgresCluster)
	require.NoError(t, f.runtime.reader.Get(t.Context(), f.runtime.key, parent))
	intent := mvutypes.Intent{Strategy: mvutypes.MajorUpgradeFlowBlueGreen, SourcePgVersion: "17", TargetPgVersion: "18", AttemptID: "attempt", Generation: parent.Generation, State: parent.Status.PostgresMajorUpgradeStatus}
	out, err := c.AdvanceCompletion(t.Context(), intent, f.status())
	require.NoError(t, err)
	store := NewMajorUpgradeStateStore(newTestClusterStateStore(t, f.runtime.client, f.runtime.key))
	_, err = store.SaveMajorUpgradeProgress(t.Context(), intent, mvutypes.Progress{Report: out.Report, BlueGreen: out.BlueGreen})
	require.NoError(t, err)
	return out
}

func waitForTargetBackup(t *testing.T, f *cutoverFixture, c *BlueGreenCompletion) *cnpgv1.Backup {
	t.Helper()
	for range 12 {
		completionStep(t, f, c)
		backups := &cnpgv1.BackupList{}
		require.NoError(t, f.runtime.reader.List(t.Context(), backups))
		if len(backups.Items) > 0 {
			require.Len(t, backups.Items, 1)
			return backups.Items[0].DeepCopy()
		}
	}
	t.Fatal("target backup was not created")
	return nil
}

func completeTargetBackup(t *testing.T, f *cutoverFixture, backup *cnpgv1.Backup) {
	t.Helper()
	backup.Status.Phase = cnpgv1.BackupPhaseCompleted
	backup.Status.StartedAt = new(metav1.NewTime(f.status().CommitReceipt.CommittedAt.Add(time.Second)))
	backup.Status.StoppedAt = new(metav1.NewTime(f.status().CommitReceipt.CommittedAt.Add(2 * time.Second)))
	backup.Status.EndWal = "000000010000000000000001"
	require.NoError(t, f.runtime.client.Update(t.Context(), backup))
}

func TestCompletionRequiresBackupAndWALBeforeTeardown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		phase   cnpgv1.BackupPhase
		message string
	}{
		{"failed backup", cnpgv1.BackupPhaseFailed, "failed"},
		{"completed backup awaiting WAL", cnpgv1.BackupPhaseCompleted, "WAL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, c, sql := completionFixture(t)
			backup := waitForTargetBackup(t, f, c)
			assert.Equal(t, "green", backup.Spec.Cluster.Name)
			assert.Equal(t, "attempt", backup.Labels[blueGreenAttemptLabel])
			completeTargetBackup(t, f, backup)
			backup.Status.Phase = tc.phase
			require.NoError(t, f.runtime.client.Update(t.Context(), backup))
			out := completionStep(t, f, c)
			assert.Equal(t, string(mvutypes.PostUpgradeBackup), out.Report.Phase)
			assert.Contains(t, out.Report.Message, tc.message)
			assert.Nil(t, f.status().Recoverability)
			assert.Zero(t, sql.sourceCalls)
			assert.Zero(t, sql.greenCalls)
			assert.NotContains(t, f.calls, "restore-blue")
			assert.NotNil(t, f.status().CommitReceipt)
			if tc.phase == cnpgv1.BackupPhaseFailed {
				return
			}
			sql.archived = true
			out = completionStep(t, f, c)
			require.Equal(t, string(mvutypes.Recoverable), out.Report.Phase, out.Report.Message)
			require.True(t, mvutypes.BlueGreenRecoverable(f.status()))
			parent := new(platformv1alpha1.PostgresCluster)
			require.NoError(t, f.runtime.client.Get(t.Context(), f.runtime.key, parent))
			assert.Equal(t, backup.Name, *parent.Status.PostgresMajorUpgradeStatus[0].BackupNames.PostUpgrade)
			assert.Equal(t, "source-anchor", *parent.Status.PostgresMajorUpgradeStatus[0].BackupNames.PreUpgrade)
			assert.Zero(t, sql.sourceCalls, "Recoverable must be persisted before teardown")
		})
	}
}

func TestCompletionRecoversPartialTeardownAndCredentialRemoval(t *testing.T) {
	f, c, sql := completionFixture(t)
	backup := waitForTargetBackup(t, f, c)
	completeTargetBackup(t, f, backup)
	sql.archived = true
	completionStep(t, f, c)
	sql.teardownErr = errors.New("temporary SQL outage")
	out := completionStep(t, f, c)
	assert.Equal(t, string(mvutypes.Recoverable), out.Report.Phase)
	assert.Nil(t, f.status().Completion.ReplicationRemovedAt)
	sql.teardownErr = nil
	completionStep(t, f, c)
	require.NotNil(t, f.status().Completion.ReplicationRemovedAt)
	completionStep(t, f, c) // declare password retirement
	blue := &cnpgv1.Cluster{}
	require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: f.runtime.key.Namespace, Name: "blue"}, blue))
	blue.Status.ManagedRolesStatus.ByStatus = map[cnpgv1.RoleStatus][]string{cnpgv1.RoleStatusReconciled: {cutoverRoleName("attempt"), replicationRoleName("attempt")}}
	blue.Status.ManagedRolesStatus.PasswordStatus = map[string]cnpgv1.PasswordState{cutoverRoleName("attempt"): {TransactionID: 1}, replicationRoleName("attempt"): {TransactionID: 2}}
	require.NoError(t, f.runtime.client.Update(t.Context(), blue))
	completionStep(t, f, c)
	// Simulate a crash after Secret deletion but before checkpoint persistence.
	_, err := c.teardown.deleteSecrets(t.Context(), "attempt")
	require.NoError(t, err)
	c = &BlueGreenCompletion{switchover: f.runtime, teardown: c.teardown, config: c.config, sql: sql}
	for range 4 {
		out = completionStep(t, f, c)
		if out.Report.Phase == string(mvutypes.Completed) {
			break
		}
	}
	assert.Equal(t, string(mvutypes.Completed), out.Report.Phase)
	assert.NotNil(t, f.status().Completion.CredentialsRemovedAt)
	assert.NotContains(t, f.calls, "restore-blue")
	require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: f.runtime.key.Namespace, Name: "blue"}, blue))
	assert.True(t, retiredAccessMatches(blue, f.status()))
	assert.Equal(t, "enabled", blue.Annotations[cnpgutils.SkipWalArchiving])
	assert.NotNil(t, f.status().CommitReceipt)
}

func TestBackupHandoffNeverEnablesGreenBeforeSourceShutdownProof(t *testing.T) {
	f, c, sql := completionFixture(t)
	sql.stopped = false
	for range 4 {
		completionStep(t, f, c)
	}
	green := &cnpgv1.Cluster{}
	require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: f.runtime.key.Namespace, Name: "green"}, green))
	assert.Nil(t, green.Spec.Backup)
	assert.Nil(t, f.status().Completion.SourceStoppedAt)
	sql.stopped = true
	waitForTargetBackup(t, f, c)
	require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKeyFromObject(green), green))
	assert.NotNil(t, green.Spec.Backup)
	assert.NotNil(t, f.status().Completion.SourceStoppedAt)
}

func TestCompletionRejectsRetainedSourceFenceDrift(t *testing.T) {
	for name, change := range map[string]func(*cnpgv1.RoleConfiguration){
		"login enabled":     func(role *cnpgv1.RoleConfiguration) { role.Login = true },
		"password retained": func(role *cnpgv1.RoleConfiguration) { role.DisablePassword = false },
		"privilege added":   func(role *cnpgv1.RoleConfiguration) { role.Superuser = true },
		"membership added":  func(role *cnpgv1.RoleConfiguration) { role.InRoles = []string{"postgres"} },
	} {
		t.Run(name, func(t *testing.T) {
			f, c, sql := completionFixture(t)
			waitForTargetBackup(t, f, c)
			blue := &cnpgv1.Cluster{}
			require.NoError(t, f.runtime.client.Get(t.Context(), client.ObjectKey{Namespace: f.runtime.key.Namespace, Name: "blue"}, blue))
			role := cnpgv1.RoleConfiguration{Name: cutoverRoleName("attempt"), Ensure: cnpgv1.EnsurePresent, DisablePassword: true}
			change(&role)
			blue.Spec.Managed.Roles = append(blue.Spec.Managed.Roles, role)
			require.NoError(t, f.runtime.client.Update(t.Context(), blue))
			out := completionStep(t, f, c)
			assert.Contains(t, out.Report.Message, "fence changed")
			assert.Nil(t, f.status().Recoverability)
			assert.Zero(t, sql.sourceCalls)
			assert.NotContains(t, f.calls, "restore-blue")
		})
	}
}

func TestCompletionBackupRetryPreservesFailedRequestAndPinsRecoveryAnchor(t *testing.T) {
	f, c, sql := completionFixture(t)
	backup := waitForTargetBackup(t, f, c)
	backup.Status.Phase = cnpgv1.BackupPhaseFailed
	require.NoError(t, f.runtime.client.Update(t.Context(), backup))
	firstName := backup.Name
	parent := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, f.runtime.client.Get(t.Context(), f.runtime.key, parent))
	parent.Annotations = map[string]string{mvutypes.AnnotationMajorUpgradeRetryAt: f.status().CommitReceipt.CommittedAt.Add(time.Minute).UTC().Format(time.RFC3339Nano)}
	require.NoError(t, f.runtime.client.Update(t.Context(), parent))
	completionStep(t, f, c)
	backups := &cnpgv1.BackupList{}
	require.NoError(t, f.runtime.reader.List(t.Context(), backups))
	require.Len(t, backups.Items, 2)
	for i := range backups.Items {
		if backups.Items[i].Name != firstName {
			backup = backups.Items[i].DeepCopy()
		}
	}
	require.NotEqual(t, firstName, backup.Name)
	completeTargetBackup(t, f, backup)
	sql.archived = true
	completionStep(t, f, c)
	require.Equal(t, backup.Name, *f.status().Recoverability.TargetBackupName)
	require.NoError(t, f.runtime.client.Get(t.Context(), f.runtime.key, parent))
	parent.Annotations[mvutypes.AnnotationMajorUpgradeRetryAt] = f.status().CommitReceipt.CommittedAt.Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)
	require.NoError(t, f.runtime.client.Update(t.Context(), parent))
	completionStep(t, f, c)
	require.NoError(t, f.runtime.reader.List(t.Context(), backups))
	assert.Len(t, backups.Items, 2, "a verified recovery anchor cannot be replaced by a retry")
}

type failingBackupHandoff struct{ calls int }

func (h *failingBackupHandoff) TransferBackupOwnership(context.Context, mvutypes.Intent, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	h.calls++
	return false, errors.New("archive backend unavailable")
}

func TestCommittedBackupFailureDoesNotStarveGreenAvailability(t *testing.T) {
	f := newCutoverFixture(t)
	handoff := &failingBackupHandoff{}
	f.runtime.WithBackupHandoff(handoff)
	f.until(func(bg *platformv1alpha1.PostgresBlueGreenUpgradeStatus) bool {
		return bg.Switchover.EndpointPublicationEventRef != nil
	})
	assert.Positive(t, handoff.calls)
	assert.NotNil(t, f.status().Switchover.TargetAvailableAt)
	assert.NotContains(t, f.calls, "restore-blue")
}

func TestCompletionUsesClassBarmanBackendAndBothSchedules(t *testing.T) {
	f, c, _ := completionFixture(t)
	c.config.CNPG.Backup.BarmanObjectStore = &platformv1alpha1.CNPGBarmanObjectStoreConfig{DestinationPath: "s3://fixture/green"}
	backup := waitForTargetBackup(t, f, c)
	require.Equal(t, cnpgv1.BackupMethodPlugin, backup.Spec.Method)
	require.NotNil(t, backup.Spec.PluginConfiguration)
	_, plugin, _ := clustercore.EffectiveBackupProvider(c.config)
	assert.Equal(t, plugin, backup.Spec.PluginConfiguration.Name)
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(clustercore.ObjectStoreGVK)
	require.NoError(t, f.runtime.reader.Get(t.Context(), client.ObjectKey{Namespace: f.runtime.key.Namespace, Name: "green-object-store"}, object))
	require.True(t, metav1.IsControlledBy(object, f.runtime.owner))
	// Accept provider defaults that preserve the configured policy.
	require.NoError(t, unstructured.SetNestedField(object.Object, "gzip", "spec", "configuration", "wal", "compression"))
	require.NoError(t, f.runtime.client.Update(t.Context(), object))
	completionStep(t, f, c)
	schedules := &cnpgv1.ScheduledBackupList{}
	require.NoError(t, f.runtime.reader.List(t.Context(), schedules))
	require.Len(t, schedules.Items, 2)
	for _, schedule := range schedules.Items {
		assert.Equal(t, "green", schedule.Spec.Cluster.Name)
		assert.True(t, metav1.IsControlledBy(&schedule, f.runtime.owner))
	}
}
