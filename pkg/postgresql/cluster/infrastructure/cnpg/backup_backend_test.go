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
package cnpg

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/backuptypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// barmanCloudPluginName mirrors the plugin name used by the cluster model; the
// adapter tests only need a stable non-empty plugin name to assert wiring.
const barmanCloudPluginName = "barman-cloud.cloudnative-pg.io"

type noopBackupEmitter struct{}

func (noopBackupEmitter) emitNormal(_ client.Object, _, _ string)  {}
func (noopBackupEmitter) emitWarning(_ client.Object, _, _ string) {}

type captureBackupEmitter struct {
	normals  []string
	warnings []string
}

func (c *captureBackupEmitter) emitNormal(_ client.Object, reason, message string) {
	c.normals = append(c.normals, reason+":"+message)
}

func (c *captureBackupEmitter) emitWarning(_ client.Object, reason, message string) {
	c.warnings = append(c.warnings, reason+":"+message)
}

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	enterprisev4.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)
	corev1.AddToScheme(scheme)
	return scheme
}

func newTestCluster(name, ns string) *enterprisev4.PostgresCluster {
	return &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			UID:       "test-uid-123",
		},
	}
}

// ownedByCluster returns an OwnerReferences slice marking the object as controlled by the
// given PostgresCluster, matching what ctrl.SetControllerReference writes at runtime. Used to
// seed ScheduledBackups that the controller is allowed to garbage-collect.
func ownedByCluster(cluster *enterprisev4.PostgresCluster) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion: enterprisev4.GroupVersion.String(),
		Kind:       "PostgresCluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
		Controller: ptr.To(true),
	}}
}

func volumeSnapshotScheduleSpec() backuptypes.ScheduleSpec {
	return backuptypes.ScheduleSpec{
		Name:            "c1-backup",
		Namespace:       "ns1",
		CNPGClusterName: "c1",
		Schedule:        "0 2 * * *",
		Target:          "prefer-standby",
		Method:          backuptypes.BackupMethodVolumeSnapshot,
	}
}

func TestCNPGBackupBackend_EnsureScheduled_Creates(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	emitter := &captureBackupEmitter{}
	backend := newBackupBackend(c, scheme, emitter)

	err := backend.EnsureScheduled(context.Background(), cluster, volumeSnapshotScheduleSpec())
	require.NoError(t, err)

	sb := &cnpgv1.ScheduledBackup{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, sb))
	assert.Equal(t, "0 0 2 * * *", sb.Spec.Schedule)
	assert.Equal(t, cnpgv1.BackupMethodVolumeSnapshot, sb.Spec.Method)
	assert.Equal(t, cnpgv1.BackupTarget("prefer-standby"), sb.Spec.Target)
	assert.Equal(t, "c1", sb.Spec.Cluster.Name)
	assert.Nil(t, sb.Spec.PluginConfiguration)
	require.Len(t, emitter.normals, 1)
	assert.Contains(t, emitter.normals[0], core.EventScheduledBackupCreated)
	// owner reference is set to the cluster
	require.Len(t, sb.OwnerReferences, 1)
	assert.Equal(t, cluster.UID, sb.OwnerReferences[0].UID)
}

func TestCNPGBackupBackend_EnsureScheduled_PluginMethod(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	spec := volumeSnapshotScheduleSpec()
	spec.Name = "c1-backup-objectstore"
	spec.Method = backuptypes.BackupMethodPlugin
	spec.PluginName = barmanCloudPluginName

	require.NoError(t, backend.EnsureScheduled(context.Background(), cluster, spec))

	sb := &cnpgv1.ScheduledBackup{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-backup-objectstore", Namespace: "ns1"}, sb))
	assert.Equal(t, cnpgv1.BackupMethodPlugin, sb.Spec.Method)
	require.NotNil(t, sb.Spec.PluginConfiguration)
	assert.Equal(t, barmanCloudPluginName, sb.Spec.PluginConfiguration.Name)
}

func TestCNPGBackupBackend_EnsureScheduled_UpdatesSpec(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	existing := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule: "0 0 1 * * *",
			Cluster:  cnpgv1.LocalObjectReference{Name: "c1"},
			Method:   cnpgv1.BackupMethodVolumeSnapshot,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	require.NoError(t, backend.EnsureScheduled(context.Background(), cluster, volumeSnapshotScheduleSpec()))

	sb := &cnpgv1.ScheduledBackup{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, sb))
	assert.Equal(t, "0 0 2 * * *", sb.Spec.Schedule)
}

func TestCNPGBackupBackend_EnsureScheduled_NoOpWhenAlreadyCorrect(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	// Pre-existing object already matches the desired spec (including BackupOwnerReference).
	existing := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule:             "0 0 2 * * *",
			Cluster:              cnpgv1.LocalObjectReference{Name: "c1"},
			Method:               cnpgv1.BackupMethodVolumeSnapshot,
			Target:               cnpgv1.BackupTarget("prefer-standby"),
			BackupOwnerReference: "cluster",
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})
	require.NoError(t, backend.EnsureScheduled(context.Background(), cluster, volumeSnapshotScheduleSpec()))

	sb := &cnpgv1.ScheduledBackup{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, sb))
	// Spec must not have drifted.
	assert.Equal(t, "0 0 2 * * *", sb.Spec.Schedule)
	assert.Equal(t, cnpgv1.BackupMethodVolumeSnapshot, sb.Spec.Method)
}

func TestCNPGBackupBackend_EnsureScheduled_ForeignOwnerIsError(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	// A different cluster with a distinct UID — newTestCluster always returns the same UID so override it.
	otherCluster := newTestCluster("c2", "ns1")
	otherCluster.UID = "other-uid-456"
	// A ScheduledBackup with the same name but controlled by a different owner.
	foreign := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1", OwnerReferences: ownedByCluster(otherCluster)},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(foreign).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	err := backend.EnsureScheduled(context.Background(), cluster, volumeSnapshotScheduleSpec())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not controlled by this owner")
}

func TestCNPGBackupBackend_DeleteScheduled_OwnedIsDeleted(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	existing := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	emitter := &captureBackupEmitter{}
	backend := newBackupBackend(c, scheme, emitter)

	require.NoError(t, backend.DeleteScheduled(context.Background(), cluster, "c1-backup", "ns1"))

	getErr := c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, &cnpgv1.ScheduledBackup{})
	assert.True(t, apierrors.IsNotFound(getErr))
	require.Len(t, emitter.normals, 1)
	assert.Contains(t, emitter.normals[0], core.EventScheduledBackupDeleted)
}

func TestCNPGBackupBackend_DeleteScheduled_ForeignIsPreserved(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	// A ScheduledBackup of the same name but not controlled by this cluster.
	foreign := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(foreign).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	require.NoError(t, backend.DeleteScheduled(context.Background(), cluster, "c1-backup", "ns1"))

	// Still present — not ours to delete.
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, &cnpgv1.ScheduledBackup{}))
}

func TestCNPGBackupBackend_DeleteScheduled_AbsentIsNoOp(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	require.NoError(t, backend.DeleteScheduled(context.Background(), cluster, "missing", "ns1"))
}

func TestCNPGBackupBackend_GetSchedule(t *testing.T) {
	scheme := newTestScheme()
	now := metav1.Now()
	existing := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"},
		Status:     cnpgv1.ScheduledBackupStatus{LastScheduleTime: &now},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	t.Run("present", func(t *testing.T) {
		res, err := backend.GetSchedule(context.Background(), "c1-backup", "ns1")
		require.NoError(t, err)
		assert.True(t, res.Exists)
		require.NotNil(t, res.LastScheduleTime)
		assert.Equal(t, now.Unix(), res.LastScheduleTime.Unix())
	})

	t.Run("absent", func(t *testing.T) {
		res, err := backend.GetSchedule(context.Background(), "missing", "ns1")
		require.NoError(t, err)
		assert.False(t, res.Exists)
	})
}

func volumeSnapshotBackupRequest() backuptypes.BackupRequest {
	return backuptypes.BackupRequest{
		Name:            "c1-ondemand",
		Namespace:       "ns1",
		CNPGClusterName: "c1",
		Target:          "prefer-standby",
		Method:          backuptypes.BackupMethodVolumeSnapshot,
	}
}

func TestCNPGBackupBackend_BackupNow_Creates(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	emitter := &captureBackupEmitter{}
	backend := newBackupBackend(c, scheme, emitter)

	require.NoError(t, backend.BackupNow(context.Background(), cluster, volumeSnapshotBackupRequest()))

	backup := &cnpgv1.Backup{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-ondemand", Namespace: "ns1"}, backup))
	assert.Equal(t, cnpgv1.BackupMethodVolumeSnapshot, backup.Spec.Method)
	assert.Equal(t, cnpgv1.BackupTarget("prefer-standby"), backup.Spec.Target)
	assert.Equal(t, "c1", backup.Spec.Cluster.Name)
	assert.Nil(t, backup.Spec.PluginConfiguration)
	require.Len(t, emitter.normals, 1)
	assert.Contains(t, emitter.normals[0], core.EventOnDemandBackupCreated)
	require.Len(t, backup.OwnerReferences, 1)
	assert.Equal(t, cluster.UID, backup.OwnerReferences[0].UID)
}

func TestCNPGBackupBackend_BackupNow_PluginMethod(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	req := volumeSnapshotBackupRequest()
	req.Name = "c1-ondemand-objectstore"
	req.Method = backuptypes.BackupMethodPlugin
	req.PluginName = barmanCloudPluginName

	require.NoError(t, backend.BackupNow(context.Background(), cluster, req))

	backup := &cnpgv1.Backup{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-ondemand-objectstore", Namespace: "ns1"}, backup))
	assert.Equal(t, cnpgv1.BackupMethodPlugin, backup.Spec.Method)
	require.NotNil(t, backup.Spec.PluginConfiguration)
	assert.Equal(t, barmanCloudPluginName, backup.Spec.PluginConfiguration.Name)
}

func TestCNPGBackupBackend_BackupNow_IdempotentOnName(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	// A Backup with this name already exists and is owned by this cluster —
	// BackupNow must not recreate or mutate it (CNPG Backup spec is immutable).
	existing := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "c1-ondemand",
			Namespace:       "ns1",
			OwnerReferences: ownedByCluster(cluster),
		},
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{Name: "c1"},
			Method:  cnpgv1.BackupMethodVolumeSnapshot,
			Target:  cnpgv1.BackupTarget("primary"),
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	emitter := &captureBackupEmitter{}
	backend := newBackupBackend(c, scheme, emitter)

	require.NoError(t, backend.BackupNow(context.Background(), cluster, volumeSnapshotBackupRequest()))

	backup := &cnpgv1.Backup{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-ondemand", Namespace: "ns1"}, backup))
	// Original target preserved, not overwritten with the request's value.
	assert.Equal(t, cnpgv1.BackupTarget("primary"), backup.Spec.Target)
	assert.Empty(t, emitter.normals)
}

func TestCNPGBackupBackend_BackupNow_ForeignCollisionIsError(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	// A Backup with the same name exists but is not controlled by this cluster.
	foreign := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-ondemand", Namespace: "ns1"},
		Spec:       cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: "c1"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(foreign).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	err := backend.BackupNow(context.Background(), cluster, volumeSnapshotBackupRequest())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not controlled by this owner")
}

func TestCNPGBackupBackend_GetBackup(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	started := metav1.Now()
	existing := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-ondemand", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
		Spec: cnpgv1.BackupSpec{
			Cluster: cnpgv1.LocalObjectReference{Name: "c1"},
			Method:  cnpgv1.BackupMethodVolumeSnapshot,
		},
		Status: cnpgv1.BackupStatus{
			Phase:     cnpgv1.BackupPhaseCompleted,
			BackupID:  "backup-123",
			StartedAt: &started,
			BackupSnapshotStatus: cnpgv1.BackupSnapshotStatus{
				Elements: []cnpgv1.BackupSnapshotElementStatus{{Name: "snap-1"}, {Name: "snap-2"}},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	t.Run("present", func(t *testing.T) {
		res, found, err := backend.GetBackup(context.Background(), cluster, "c1-ondemand", "ns1")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, "c1-ondemand", res.Name)
		assert.Equal(t, "c1", res.CNPGClusterName)
		assert.Equal(t, backuptypes.BackupMethodVolumeSnapshot, res.Method)
		assert.True(t, res.Done)
		assert.False(t, res.Failed)
		assert.Equal(t, "backup-123", res.BackupID)
		require.NotNil(t, res.StartedAt)
		assert.Equal(t, started.Unix(), res.StartedAt.Unix())
		assert.Equal(t, []string{"snap-1", "snap-2"}, res.SnapshotNames)
	})

	t.Run("absent", func(t *testing.T) {
		_, found, err := backend.GetBackup(context.Background(), cluster, "missing", "ns1")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("foreign owner treated as absent", func(t *testing.T) {
		// Use a distinct UID — newTestCluster always returns the same UID so override it.
		otherCluster := newTestCluster("c2", "ns1")
		otherCluster.UID = "other-uid-456"
		_, found, err := backend.GetBackup(context.Background(), otherCluster, "c1-ondemand", "ns1")
		require.NoError(t, err)
		assert.False(t, found)
	})
}

func TestCNPGBackupBackend_GetBackup_FailedPhase(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	failed := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-failed", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
		Spec:       cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: "c1"}, Method: cnpgv1.BackupMethodVolumeSnapshot},
		Status: cnpgv1.BackupStatus{
			Phase: cnpgv1.BackupPhaseFailed,
			Error: "disk full",
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(failed).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	res, found, err := backend.GetBackup(context.Background(), cluster, "c1-failed", "ns1")
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, res.Failed)
	assert.False(t, res.Done)
	assert.Equal(t, "disk full", res.Error)
}

func TestCNPGBackupBackend_ListBackups(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	older := metav1.Unix(1000, 0)
	newer := metav1.Unix(2000, 0)
	c1Old := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-old", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
		Spec:       cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: "c1"}},
		Status:     cnpgv1.BackupStatus{Phase: cnpgv1.BackupPhaseCompleted, StartedAt: &older},
	}
	c1New := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-new", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
		Spec:       cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: "c1"}},
		Status:     cnpgv1.BackupStatus{Phase: cnpgv1.BackupPhaseRunning, StartedAt: &newer},
	}
	// A backup for a different cluster in the same namespace must be excluded.
	otherCluster := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c2-backup", Namespace: "ns1"},
		Spec:       cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: "c2"}},
	}
	// A backup for the same cluster but owned by a different controller must be excluded.
	foreignOwner := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-foreign", Namespace: "ns1"},
		Spec:       cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: "c1"}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(c1Old, c1New, otherCluster, foreignOwner).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	results, err := backend.ListBackups(context.Background(), cluster, "c1", "ns1")
	require.NoError(t, err)
	require.Len(t, results, 2)
	// Most recent first.
	assert.Equal(t, "c1-new", results[0].Name)
	assert.Equal(t, "c1-old", results[1].Name)
}

func TestCNPGBackupBackend_ListBackups_NilStartedAtSortStable(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	// Two pending backups with no StartedAt — order must be deterministic (by name).
	pending1 := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-pending-a", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
		Spec:       cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: "c1"}},
		Status:     cnpgv1.BackupStatus{Phase: cnpgv1.BackupPhasePending},
	}
	pending2 := &cnpgv1.Backup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-pending-b", Namespace: "ns1", OwnerReferences: ownedByCluster(cluster)},
		Spec:       cnpgv1.BackupSpec{Cluster: cnpgv1.LocalObjectReference{Name: "c1"}},
		Status:     cnpgv1.BackupStatus{Phase: cnpgv1.BackupPhasePending},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pending1, pending2).Build()
	backend := newBackupBackend(c, scheme, noopBackupEmitter{})

	results, err := backend.ListBackups(context.Background(), cluster, "c1", "ns1")
	require.NoError(t, err)
	require.Len(t, results, 2)
	// Both have nil StartedAt (epoch 0); secondary tiebreaker is name ascending.
	assert.Equal(t, "c1-pending-a", results[0].Name)
	assert.Equal(t, "c1-pending-b", results[1].Name)
}

// TestNewBackupBackend_FromRecorder exercises the exported constructor used by
// out-of-package consumers (e.g. the controller and major-upgrade adapters): a
// backend built from a plain EventRecorder must reconcile and emit through it.
func TestNewBackupBackend_FromRecorder(t *testing.T) {
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	recorder := record.NewFakeRecorder(10)

	backend := NewBackupBackend(c, scheme, recorder)
	require.NoError(t, backend.BackupNow(context.Background(), cluster, volumeSnapshotBackupRequest()))

	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-ondemand", Namespace: "ns1"}, &cnpgv1.Backup{}))
	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, core.EventOnDemandBackupCreated)
	default:
		t.Fatal("expected an OnDemandBackupCreated event to be recorded")
	}
}
