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
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/backuptypes"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type noopBackupEmitter struct{}

func (noopBackupEmitter) emitNormal(_ client.Object, _, _ string)                         {}
func (noopBackupEmitter) emitWarning(_ client.Object, _, _ string)                        {}
func (noopBackupEmitter) emitBackupReadyTransition(_ client.Object, _ []metav1.Condition) {}

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

func (c *captureBackupEmitter) emitBackupReadyTransition(_ client.Object, _ []metav1.Condition) {
	c.normals = append(c.normals, EventBackupConfigured+":Backup configuration is ready")
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

func newTestMergedConfig(backupEnabled bool, schedule string) *MergedConfig {
	instances := int32(3)
	version := "18"
	storage := resource.MustParse("50Gi")
	cfg := &MergedConfig{
		Spec: &enterprisev4.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storage,
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
			Resources:        &corev1.ResourceRequirements{},
		},
		CNPG: &enterprisev4.CNPGConfig{
			PrimaryUpdateMethod: ptr.To("restart"),
			Backup: &enterprisev4.CNPGBackupConfig{
				VolumeSnapshot: &enterprisev4.CNPGVolumeSnapshotConfig{
					ClassName: ptr.To("csi-snapclass"),
					Online:    ptr.To(true),
				},
				Target: ptr.To("prefer-standby"),
			},
		},
	}
	if backupEnabled {
		cfg.Spec.Backup = &enterprisev4.BackupConfig{
			Enabled:  ptr.To(true),
			Schedule: ptr.To(schedule),
		}
	}
	return cfg
}

func noopHealthUpdater(_ *enterprisev4.PostgresClusterStatus, _ componentHealth) error { return nil }

// newTestBackupModel creates a backupModel with contracts.CNPGCluster set to cnpg (may be nil to test contracts-not-ready path).
func newTestBackupModel(c client.Client, scheme *runtime.Scheme, events backupEmitter, updater healthStatusUpdater, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpg ...*cnpgv1.Cluster) *backupModel {
	return newTestBackupModelWithBackend(noopBackupBackend{}, events, updater, cluster, cfg, cnpg...)
}

// newTestBackupModelWithBackend wires a backupModel to an explicit BackupBackend so the
// model can be unit-tested at its port boundary. The CNPG-specific translation and
// ownership guards live in the infrastructure/cnpg adapter and are tested there; here we
// verify that the model drives the port with the correct engine-agnostic spec/requests and
// maps the observed ScheduleResult back into cluster status.
func newTestBackupModelWithBackend(backend BackupBackend, events backupEmitter, updater healthStatusUpdater, cluster *enterprisev4.PostgresCluster, cfg *MergedConfig, cnpg ...*cnpgv1.Cluster) *backupModel {
	contracts := &reconcileContracts{}
	if len(cnpg) > 0 {
		contracts.CNPGCluster = cnpg[0]
	}
	return newBackupModel(backend, events, updater, cluster, cfg, contracts)
}

// deletedSchedule records one DeleteScheduled call.
type deletedSchedule struct{ name, namespace string }

// spyBackupBackend is an in-package test double for BackupBackend. It records the calls the
// model makes and returns canned observations, so backupModel can be exercised without any
// CNPG dependency (which would form an import cycle with infrastructure/cnpg).
type spyBackupBackend struct {
	ensured []backuptypes.ScheduleSpec
	deleted []deletedSchedule
	backups []backuptypes.BackupRequest

	// schedules maps ScheduledBackup name -> the observation GetSchedule returns for it.
	// Absent names report ScheduleResult{Exists: false}.
	schedules map[string]backuptypes.ScheduleResult

	ensureErr   error
	deleteErr   error
	scheduleErr error
}

func (s *spyBackupBackend) EnsureScheduled(_ context.Context, _ client.Object, spec backuptypes.ScheduleSpec) error {
	s.ensured = append(s.ensured, spec)
	return s.ensureErr
}

func (s *spyBackupBackend) DeleteScheduled(_ context.Context, _ client.Object, name, namespace string) error {
	s.deleted = append(s.deleted, deletedSchedule{name: name, namespace: namespace})
	return s.deleteErr
}

func (s *spyBackupBackend) GetSchedule(_ context.Context, name, _ string) (backuptypes.ScheduleResult, error) {
	if s.scheduleErr != nil {
		return backuptypes.ScheduleResult{}, s.scheduleErr
	}
	if r, ok := s.schedules[name]; ok {
		return r, nil
	}
	return backuptypes.ScheduleResult{Exists: false}, nil
}

func (s *spyBackupBackend) BackupNow(_ context.Context, _ client.Object, req backuptypes.BackupRequest) error {
	s.backups = append(s.backups, req)
	return nil
}

func (s *spyBackupBackend) GetBackup(_ context.Context, _ client.Object, _, _ string) (backuptypes.BackupResult, bool, error) {
	return backuptypes.BackupResult{}, false, nil
}

func (s *spyBackupBackend) ListBackups(_ context.Context, _ client.Object, _, _ string) ([]backuptypes.BackupResult, error) {
	return nil, nil
}

// ensuredByName returns the most recent ScheduleSpec ensured under name, if any.
func (s *spyBackupBackend) ensuredByName(name string) (backuptypes.ScheduleSpec, bool) {
	for i := len(s.ensured) - 1; i >= 0; i-- {
		if s.ensured[i].Name == name {
			return s.ensured[i], true
		}
	}
	return backuptypes.ScheduleSpec{}, false
}

// deletedNames returns the names passed to DeleteScheduled, in call order.
func (s *spyBackupBackend) deletedNames() []string {
	names := make([]string, 0, len(s.deleted))
	for _, d := range s.deleted {
		names = append(names, d.name)
	}
	return names
}

// newTestCNPGCluster returns a minimal CNPG cluster for seeding contracts in backup model tests.
func newTestCNPGCluster(name, ns string) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
}

// --- Reconcile: disabled ---

func TestBackupModel_Reconcile_Disabled(t *testing.T) {
	scheme := newTestScheme()
	cnpg := newTestCNPGCluster("c1", "ns1")

	t.Run("clears BackupStatus when previously set and backup is disabled", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cluster.Status.BackupStatus = &enterprisev4.BackupStatus{
			VolumeSnapshot: &enterprisev4.VolumeSnapshotBackupStatus{Enabled: true},
		}
		cfg := newTestMergedConfig(false, "")
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act — status is cleared in Observe, not Reconcile
		reconcileErr := model.Reconcile(context.Background())
		_, err := model.Observe(context.Background(), reconcileErr)

		// Assert
		require.NoError(t, err)
		assert.Nil(t, cluster.Status.BackupStatus)
	})

	t.Run("deletes every owned scheduled backup when disabled", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(false, "")
		backend := &spyBackupBackend{}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert — model asks the backend to delete both deterministic names; the ownership
		// guard that keeps foreign objects safe lives in (and is tested by) the adapter.
		require.NoError(t, err)
		assert.Empty(t, backend.ensured)
		assert.ElementsMatch(t, []string{"c1-backup", "c1-backup-objectstore"}, backend.deletedNames())
	})

	t.Run("no-op when disabled and no scheduled backup exists", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(false, "")
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert
		require.NoError(t, err)
		assert.Nil(t, cluster.Status.BackupStatus)
	})
}

// --- Reconcile: enabled ---

func TestBackupModel_Reconcile_Enabled(t *testing.T) {
	scheme := newTestScheme()
	cnpg := newTestCNPGCluster("c1", "ns1")

	t.Run("ensures scheduled backup with the volume-snapshot spec", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		backend := &spyBackupBackend{}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert — model passes the engine-agnostic spec; cron six-field conversion, CNPG method
		// mapping and object creation are the adapter's job (tested in infrastructure/cnpg).
		require.NoError(t, err)
		spec, ok := backend.ensuredByName("c1-backup")
		require.True(t, ok, "volume-snapshot ScheduledBackup must be ensured")
		assert.Equal(t, "0 2 * * *", spec.Schedule)
		assert.Equal(t, backuptypes.BackupMethodVolumeSnapshot, spec.Method)
		assert.Equal(t, "prefer-standby", spec.Target)
		assert.Equal(t, "c1", spec.CNPGClusterName)
		assert.Empty(t, spec.PluginName)
	})

	t.Run("ensure is called on every reconcile (idempotent create-or-update)", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "30 3 * * *")
		backend := &spyBackupBackend{}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert — the model always drives EnsureScheduled with the desired schedule; whether the
		// adapter creates or updates the underlying object is the adapter's concern.
		require.NoError(t, err)
		spec, ok := backend.ensuredByName("c1-backup")
		require.True(t, ok)
		assert.Equal(t, "30 3 * * *", spec.Schedule)
	})

	t.Run("uses target from cnpg config", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		cfg.CNPG.Backup.Target = ptr.To("primary")
		backend := &spyBackupBackend{}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert
		require.NoError(t, err)
		spec, ok := backend.ensuredByName("c1-backup")
		require.True(t, ok)
		assert.Equal(t, "primary", spec.Target)
	})

	t.Run("returns reconcileFailure when volumeSnapshot not configured", func(t *testing.T) {
		// Arrange — backup enabled in spec but CNPG VolumeSnapshot config absent
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		cfg.CNPG.Backup.VolumeSnapshot = nil
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		reconcileErr := model.Reconcile(context.Background())
		health, err := model.Observe(context.Background(), reconcileErr)

		// Assert
		require.Error(t, err)
		assert.Equal(t, pgcConstants.Failed, health.State)
		assert.Equal(t, reasonBackupProviderMissing, health.Reason)
	})
}

func TestBackupModel_Reconcile_CreateError(t *testing.T) {
	// Arrange — the backend fails to ensure the ScheduledBackup.
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfig(true, "0 2 * * *")
	backend := &spyBackupBackend{ensureErr: apierrors.NewServiceUnavailable("unavailable")}
	emitter := &captureBackupEmitter{}
	model := newTestBackupModelWithBackend(backend, emitter, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

	// Act
	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert — the model turns a backend ensure error into a failed health + warning event.
	require.Error(t, err)
	assert.Equal(t, pgcConstants.Failed, health.State)
	assert.Len(t, emitter.warnings, 1)
}

func TestBackupModel_Reconcile_DeleteError(t *testing.T) {
	// Arrange — backup disabled, and the backend fails to delete a ScheduledBackup.
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfig(false, "")
	backend := &spyBackupBackend{deleteErr: apierrors.NewForbidden(schema.GroupResource{Resource: "scheduledbackups"}, "c1-backup", nil)}
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

	// Act
	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.Error(t, err)
	assert.Equal(t, pgcConstants.Failed, health.State)
}

// --- Observe ---

func TestBackupModel_Observe_Disabled(t *testing.T) {
	// Arrange
	scheme := newTestScheme()
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfig(false, "")
	model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg)

	// Act
	health, err := model.Observe(context.Background(), nil)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Ready, health.State)
	assert.Equal(t, reasonBackupDisabled, health.Reason)
}

func TestBackupModel_Observe_Enabled(t *testing.T) {
	scheme := newTestScheme()

	t.Run("ready when scheduled backup exists", func(t *testing.T) {
		// Arrange — backend reports the ScheduledBackup exists.
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		backend := &spyBackupBackend{schedules: map[string]backuptypes.ScheduleResult{
			"c1-backup": {Exists: true},
		}}
		emitter := &captureBackupEmitter{}
		model := newTestBackupModelWithBackend(backend, emitter, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		// Act
		health, err := model.Observe(context.Background(), nil)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Ready, health.State)
		assert.Equal(t, reasonBackupConfigured, health.Reason)
		require.NotNil(t, cluster.Status.BackupStatus)
		require.NotNil(t, cluster.Status.BackupStatus.VolumeSnapshot)
		assert.True(t, cluster.Status.BackupStatus.VolumeSnapshot.Enabled)
		assert.Contains(t, emitter.normals[0], EventBackupConfigured)
	})

	t.Run("pending when scheduled backup not found", func(t *testing.T) {
		// Arrange — backend reports the ScheduledBackup does not exist yet.
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		backend := &spyBackupBackend{}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		// Act
		health, err := model.Observe(context.Background(), nil)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Pending, health.State)
		assert.Equal(t, reasonScheduledBackupCreated, health.Reason)
	})

	t.Run("get error returns failed", func(t *testing.T) {
		// Arrange — backend GetSchedule fails.
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		backend := &spyBackupBackend{scheduleErr: apierrors.NewServiceUnavailable("down")}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		// Act
		health, err := model.Observe(context.Background(), nil)

		// Assert
		require.Error(t, err)
		assert.Equal(t, pgcConstants.Failed, health.State)
	})

	t.Run("populates schedule times from ScheduleResult", func(t *testing.T) {
		// Arrange — backend surfaces last/next schedule times.
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		now := metav1.Now()
		next := metav1.NewTime(now.Add(24 * 60 * 60 * 1e9))
		backend := &spyBackupBackend{schedules: map[string]backuptypes.ScheduleResult{
			"c1-backup": {Exists: true, LastScheduleTime: &now, NextScheduleTime: &next},
		}}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		// Act
		health, err := model.Observe(context.Background(), nil)

		// Assert — schedule times are copied into in-memory cluster status; writeComponentStatus persists them
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Ready, health.State)
		require.NotNil(t, cluster.Status.BackupStatus)
		require.NotNil(t, cluster.Status.BackupStatus.VolumeSnapshot)
		assert.WithinDuration(t, now.Time, cluster.Status.BackupStatus.VolumeSnapshot.LastScheduleTime.Time, time.Second)
		assert.WithinDuration(t, next.Time, cluster.Status.BackupStatus.VolumeSnapshot.NextScheduleTime.Time, time.Second)
	})

	t.Run("BackupStatus timestamps persisted when condition is unchanged (steady-state)", func(t *testing.T) {
		// This test exercises the exact bug that the before-snapshot fix addresses:
		// if cluster.Status.BackupStatus is mutated before setStatus takes its
		// before-snapshot, a timestamps-only change is invisible to the DeepEqual
		// guard and silently dropped when condition/generation are unchanged.
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		ctx := context.Background()

		// Pre-populate the condition as already Ready so it won't change this cycle.
		now1 := metav1.NewTime(metav1.Now().Add(-time.Hour))
		next1 := metav1.NewTime(now1.Add(23 * time.Hour))
		cluster.Status.Conditions = []metav1.Condition{{
			Type:   string(backupReady),
			Status: metav1.ConditionTrue,
			Reason: string(reasonBackupConfigured),
		}}
		cluster.Status.BackupStatus = &enterprisev4.BackupStatus{
			VolumeSnapshot: &enterprisev4.VolumeSnapshotBackupStatus{
				Enabled: true, LastScheduleTime: &now1, NextScheduleTime: &next1,
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
		require.NoError(t, c.Status().Update(ctx, cluster))

		// New timestamps — only change this reconcile. The backend reports them via ScheduleResult.
		now2 := metav1.Now()
		next2 := metav1.NewTime(now2.Add(24 * time.Hour))
		backend := &spyBackupBackend{schedules: map[string]backuptypes.ScheduleResult{
			"c1-backup": {Exists: true, LastScheduleTime: &now2, NextScheduleTime: &next2},
		}}

		updater := func(before *enterprisev4.PostgresClusterStatus, health componentHealth) error {
			return setStatusFromHealth(ctx, c, nil, cluster, before, health)
		}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, updater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		health, err := model.Observe(ctx, nil)

		// Assert — schedule times are copied into in-memory cluster status; writeComponentStatus persists them
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Ready, health.State)
		persisted := &enterprisev4.PostgresCluster{}
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "c1", Namespace: "ns1"}, persisted))
		require.NotNil(t, persisted.Status.BackupStatus)
		require.NotNil(t, persisted.Status.BackupStatus.VolumeSnapshot)
		assert.WithinDuration(t, now2.Time, persisted.Status.BackupStatus.VolumeSnapshot.LastScheduleTime.Time, time.Second,
			"updated LastScheduleTime must be persisted even when condition is unchanged")
		assert.WithinDuration(t, next2.Time, persisted.Status.BackupStatus.VolumeSnapshot.NextScheduleTime.Time, time.Second,
			"updated NextScheduleTime must be persisted even when condition is unchanged")
	})

	t.Run("BackupStatus persisted to API server via writeComponentStatus", func(t *testing.T) {
		// Arrange — wire a real healthStatusUpdater backed by the fake client so that
		// writeComponentStatus (deferred in Observe) actually calls c.Status().Update.
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		now := metav1.Now()
		next := metav1.NewTime(now.Add(24 * time.Hour))
		ctx := context.Background()
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).WithStatusSubresource(cluster).Build()
		backend := &spyBackupBackend{schedules: map[string]backuptypes.ScheduleResult{
			"c1-backup": {Exists: true, LastScheduleTime: &now, NextScheduleTime: &next},
		}}

		updater := func(before *enterprisev4.PostgresClusterStatus, health componentHealth) error {
			return setStatusFromHealth(ctx, c, nil, cluster, before, health)
		}
		model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, updater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		// Act
		health, err := model.Observe(ctx, nil)

		// Assert — BackupStatus must be readable from the API server after Observe
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Ready, health.State)
		persisted := &enterprisev4.PostgresCluster{}
		require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "c1", Namespace: "ns1"}, persisted))
		require.NotNil(t, persisted.Status.BackupStatus)
		require.NotNil(t, persisted.Status.BackupStatus.VolumeSnapshot)
		assert.True(t, persisted.Status.BackupStatus.VolumeSnapshot.Enabled)
		assert.WithinDuration(t, now.Time, persisted.Status.BackupStatus.VolumeSnapshot.LastScheduleTime.Time, time.Second)
		assert.WithinDuration(t, next.Time, persisted.Status.BackupStatus.VolumeSnapshot.NextScheduleTime.Time, time.Second)
	})
}

// --- getMergedConfig backup merge ---

func TestGetMergedConfig_BackupMerge(t *testing.T) {
	classInstances := int32(1)
	classVersion := "17"
	classStorage := resource.MustParse("50Gi")

	t.Run("cluster inherits class backup when cluster has no backup", func(t *testing.T) {
		// Arrange
		class := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "standard"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       &classInstances,
					PostgresVersion: &classVersion,
					Storage:         &classStorage,
					Backup: &enterprisev4.BackupConfig{
						Enabled:  ptr.To(true),
						Schedule: ptr.To("0 2 * * *"),
					},
				},
			},
		}
		cluster := &enterprisev4.PostgresCluster{Spec: enterprisev4.PostgresClusterSpec{}}

		// Act
		cfg := GetMergedConfig(class, cluster)

		// Assert
		require.Empty(t, ValidateMergedConfig(cfg, class.Name))
		require.NotNil(t, cfg.Spec.Backup)
		assert.True(t, *cfg.Spec.Backup.Enabled)
		assert.Equal(t, "0 2 * * *", *cfg.Spec.Backup.Schedule)
	})

	t.Run("cluster overrides schedule but inherits enabled from class", func(t *testing.T) {
		// Arrange
		class := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "standard"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       &classInstances,
					PostgresVersion: &classVersion,
					Storage:         &classStorage,
					Backup: &enterprisev4.BackupConfig{
						Enabled:  ptr.To(true),
						Schedule: ptr.To("0 2 * * *"),
					},
				},
			},
		}
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{
				Backup: &enterprisev4.BackupConfig{Schedule: ptr.To("30 3 * * *")},
			},
		}

		// Act
		cfg := GetMergedConfig(class, cluster)

		// Assert
		require.Empty(t, ValidateMergedConfig(cfg, class.Name))
		require.NotNil(t, cfg.Spec.Backup)
		assert.True(t, *cfg.Spec.Backup.Enabled)
		assert.Equal(t, "30 3 * * *", *cfg.Spec.Backup.Schedule)
	})

	t.Run("cluster overrides enabled but inherits schedule from class", func(t *testing.T) {
		// Arrange
		class := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "standard"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       &classInstances,
					PostgresVersion: &classVersion,
					Storage:         &classStorage,
					Backup: &enterprisev4.BackupConfig{
						Enabled:  ptr.To(true),
						Schedule: ptr.To("0 2 * * *"),
					},
				},
			},
		}
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{
				Backup: &enterprisev4.BackupConfig{Enabled: ptr.To(false)},
			},
		}

		// Act
		cfg := GetMergedConfig(class, cluster)

		// Assert
		require.Empty(t, ValidateMergedConfig(cfg, class.Name))
		require.NotNil(t, cfg.Spec.Backup)
		assert.False(t, *cfg.Spec.Backup.Enabled)
		assert.Equal(t, "0 2 * * *", *cfg.Spec.Backup.Schedule)
	})

	t.Run("error when enabled true but no schedule after merge", func(t *testing.T) {
		// Arrange
		class := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "standard"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       &classInstances,
					PostgresVersion: &classVersion,
					Storage:         &classStorage,
				},
			},
		}
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{
				Backup: &enterprisev4.BackupConfig{Enabled: ptr.To(true)},
			},
		}

		// Act
		cfg := GetMergedConfig(class, cluster)
		errs := ValidateMergedConfig(cfg, class.Name)

		// Assert
		require.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Error(), "backup.schedule is required")
	})

	t.Run("no error when backup nil on both", func(t *testing.T) {
		// Arrange
		class := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "standard"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       &classInstances,
					PostgresVersion: &classVersion,
					Storage:         &classStorage,
				},
			},
		}
		cluster := &enterprisev4.PostgresCluster{Spec: enterprisev4.PostgresClusterSpec{}}

		// Act
		cfg := GetMergedConfig(class, cluster)

		// Assert
		require.Empty(t, ValidateMergedConfig(cfg, class.Name))
		assert.Nil(t, cfg.Spec.Backup)
	})
}

// --- normalizeCNPGClusterSpec backup ---

func TestNormalizeCNPGClusterSpec_Backup(t *testing.T) {
	t.Run("nil backup yields nil normalized backup", func(t *testing.T) {
		// Arrange
		spec := cnpgv1.ClusterSpec{ImageName: "img:18", Instances: 1}

		// Act
		normalized := normalizeCNPGClusterSpec(spec)

		// Assert
		assert.Nil(t, normalized.Backup)
	})

	t.Run("backup fields fully normalized", func(t *testing.T) {
		// Arrange
		spec := cnpgv1.ClusterSpec{
			ImageName: "img:18",
			Instances: 1,
			Backup: &cnpgv1.BackupConfiguration{
				Target: cnpgv1.BackupTargetStandby,
				VolumeSnapshot: &cnpgv1.VolumeSnapshotConfiguration{
					ClassName:              "csi-snapclass",
					WalClassName:           "csi-wal-snapclass",
					SnapshotOwnerReference: cnpgv1.SnapshotOwnerReference("cluster"),
					Online:                 ptr.To(true),
					Labels:                 map[string]string{"env": "prod"},
					Annotations:            map[string]string{"backup.io/tier": "gold"},
				},
			},
		}

		// Act
		normalized := normalizeCNPGClusterSpec(spec)

		// Assert
		require.NotNil(t, normalized.Backup)
		assert.Equal(t, "prefer-standby", normalized.Backup.Target)
		assert.Equal(t, "csi-snapclass", normalized.Backup.VolumeSnapshotClass)
		assert.Equal(t, "csi-wal-snapclass", normalized.Backup.WalClassName)
		assert.Equal(t, "cluster", normalized.Backup.SnapshotOwnerReference)
		assert.Equal(t, ptr.To(true), normalized.Backup.Online)
		assert.Equal(t, map[string]string{"env": "prod"}, normalized.Backup.Labels)
		assert.Equal(t, map[string]string{"backup.io/tier": "gold"}, normalized.Backup.Annotations)
	})

	t.Run("backup without volume snapshot", func(t *testing.T) {
		// Arrange
		spec := cnpgv1.ClusterSpec{
			ImageName: "img:18",
			Instances: 1,
			Backup:    &cnpgv1.BackupConfiguration{Target: cnpgv1.BackupTargetPrimary},
		}

		// Act
		normalized := normalizeCNPGClusterSpec(spec)

		// Assert
		require.NotNil(t, normalized.Backup)
		assert.Equal(t, "primary", normalized.Backup.Target)
		assert.Empty(t, normalized.Backup.VolumeSnapshotClass)
	})
}

// --- buildCNPGBackupConfiguration ---

func TestBuildCNPGBackupConfiguration(t *testing.T) {
	t.Run("builds complete config", func(t *testing.T) {
		// Arrange
		cfg := &MergedConfig{
			CNPG: &enterprisev4.CNPGConfig{
				Backup: &enterprisev4.CNPGBackupConfig{
					Target: ptr.To("prefer-standby"),
					VolumeSnapshot: &enterprisev4.CNPGVolumeSnapshotConfig{
						ClassName:              ptr.To("csi-snap"),
						WalClassName:           ptr.To("csi-wal"),
						SnapshotOwnerReference: ptr.To("cluster"),
						Online:                 ptr.To(true),
						Labels:                 map[string]string{"team": "platform"},
						Annotations:            map[string]string{"cost-center": "infra"},
					},
				},
			},
		}

		// Act
		result := buildCNPGBackupConfiguration(cfg)

		// Assert
		require.NotNil(t, result)
		assert.Equal(t, cnpgv1.BackupTargetStandby, result.Target)
		require.NotNil(t, result.VolumeSnapshot)
		assert.Equal(t, "csi-snap", result.VolumeSnapshot.ClassName)
		assert.Equal(t, "csi-wal", result.VolumeSnapshot.WalClassName)
		assert.Equal(t, cnpgv1.SnapshotOwnerReference("cluster"), result.VolumeSnapshot.SnapshotOwnerReference)
		assert.Equal(t, ptr.To(true), result.VolumeSnapshot.Online)
		assert.Equal(t, map[string]string{"team": "platform"}, result.VolumeSnapshot.Labels)
		assert.Equal(t, map[string]string{"cost-center": "infra"}, result.VolumeSnapshot.Annotations)
	})

	t.Run("nil target omits target", func(t *testing.T) {
		// Arrange
		cfg := &MergedConfig{
			CNPG: &enterprisev4.CNPGConfig{
				Backup: &enterprisev4.CNPGBackupConfig{
					VolumeSnapshot: &enterprisev4.CNPGVolumeSnapshotConfig{ClassName: ptr.To("snap")},
				},
			},
		}

		// Act
		result := buildCNPGBackupConfiguration(cfg)

		// Assert
		assert.Equal(t, cnpgv1.BackupTarget(""), result.Target)
		require.NotNil(t, result.VolumeSnapshot)
		assert.Equal(t, "snap", result.VolumeSnapshot.ClassName)
	})
}

// --- Helper functions ---

func TestScheduledBackupName(t *testing.T) {
	assert.Equal(t, "my-cluster-backup", scheduledBackupName("my-cluster"))
	assert.Equal(t, "x-backup", scheduledBackupName("x"))
}

func TestToSixFieldCron(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"0 2 * * *", "0 0 2 * * *"},
		{"*/15 * * * *", "0 */15 * * * *"},
		{"30 3 1 * 0", "0 30 3 1 * 0"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, toSixFieldCron(tt.input))
		})
	}
}

// --- buildCNPGClusterSpec with backup ---

func TestBuildCNPGClusterSpec_Backup(t *testing.T) {
	t.Run("includes backup when enabled", func(t *testing.T) {
		// Arrange
		cfg := newTestMergedConfig(true, "0 2 * * *")

		// Act
		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "secret", false)

		// Assert
		require.NotNil(t, spec.Backup)
		assert.Equal(t, cnpgv1.BackupTargetStandby, spec.Backup.Target)
		require.NotNil(t, spec.Backup.VolumeSnapshot)
		assert.Equal(t, "csi-snapclass", spec.Backup.VolumeSnapshot.ClassName)
	})

	t.Run("no backup section when disabled", func(t *testing.T) {
		// Arrange
		cfg := newTestMergedConfig(false, "")

		// Act
		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "secret", false)

		// Assert
		assert.Nil(t, spec.Backup)
	})

	t.Run("no backup section when cnpg backup nil", func(t *testing.T) {
		// Arrange
		cfg := newTestMergedConfig(true, "0 2 * * *")
		cfg.CNPG.Backup = nil

		// Act
		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "secret", false)

		// Assert
		assert.Nil(t, spec.Backup)
	})
}

func TestBackupModel_Reconcile_EnsurePassesOwner(t *testing.T) {
	t.Parallel()

	// The model always drives EnsureScheduled with the PostgresCluster as owner; adopting an
	// orphaned ScheduledBackup and repairing its owner reference is the adapter's responsibility
	// (covered by infrastructure/cnpg TestCNPGBackupBackend_EnsureScheduled_* tests).
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfig(true, "0 2 * * *")
	backend := &spyBackupBackend{}
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

	// Act
	err := model.Reconcile(context.Background())

	// Assert
	require.NoError(t, err)
	_, ok := backend.ensuredByName("c1-backup")
	require.True(t, ok, "the volume-snapshot ScheduledBackup must be ensured so the adapter can repair ownership")
}

// --- contracts ---

func TestBackupModel_ContractsNotReady(t *testing.T) {
	scheme := newTestScheme()

	t.Run("CheckContracts returns errContractsNotReady when CNPGCluster is nil", func(t *testing.T) {
		// Arrange — no cnpg argument → contracts.CNPGCluster == nil
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg)

		// Act
		err := model.CheckContracts()

		//Assert
		assert.EqualError(t, err, errContractsNotReady.Error())
	})

	t.Run("Observe returns Pending when CNPGCluster contract is missing", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg)

		// Act — simulate runComponents: CheckContracts error is passed as reconcileErr, Reconcile is skipped
		contractErr := model.CheckContracts()
		health, err := model.Observe(context.Background(), contractErr)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Pending, health.State)
		assert.Equal(t, reasonUpstreamNotReady, health.Reason)
	})
}

// --- Barman object store backup ---

// newTestMergedConfigBarman builds a barman-object-store-only backup config (no volume snapshot),
// so single-provider barman behaviour can be asserted in isolation. Use
// newTestMergedConfigDualProvider for the both-providers case.
func newTestMergedConfigBarman(schedule string) *MergedConfig {
	cfg := newTestMergedConfig(true, schedule)
	cfg.CNPG.Backup.VolumeSnapshot = nil
	cfg.CNPG.Backup.BarmanObjectStore = newTestBarmanObjectStoreConfig()
	return cfg
}

func newTestBarmanObjectStoreConfig() *enterprisev4.CNPGBarmanObjectStoreConfig {
	return &enterprisev4.CNPGBarmanObjectStoreConfig{
		DestinationPath: "s3://test-bucket/clusters/",
		S3Credentials: enterprisev4.CNPGBarmanS3Credentials{
			AccessKeyId:     corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "s3-creds"}, Key: "accessKeyId"},
			SecretAccessKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "s3-creds"}, Key: "secretAccessKey"},
		},
	}
}

// newTestMergedConfigDualProvider configures both volume snapshot and barman object store on an
// enabled backup.
func newTestMergedConfigDualProvider(schedule string) *MergedConfig {
	cfg := newTestMergedConfig(true, schedule)
	cfg.CNPG.Backup.BarmanObjectStore = newTestBarmanObjectStoreConfig()
	return cfg
}

func TestBackupModel_Barman_EnsuresPluginScheduledBackup(t *testing.T) {
	cluster := newTestCluster("c1", "ns1")
	cnpg := newTestCNPGCluster("c1", "ns1")
	cfg := newTestMergedConfigBarman("0 2 * * *")
	backend := &spyBackupBackend{}
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

	require.NoError(t, model.Reconcile(context.Background()))

	// Barman-only config ensures the object-store ScheduledBackup with the plugin method...
	spec, ok := backend.ensuredByName("c1-backup-objectstore")
	require.True(t, ok, "object-store ScheduledBackup must be ensured")
	assert.Equal(t, backuptypes.BackupMethodPlugin, spec.Method)
	assert.Equal(t, "barman-cloud.cloudnative-pg.io", spec.PluginName)

	// ...and must not ensure the volume-snapshot ScheduledBackup.
	_, vsOK := backend.ensuredByName("c1-backup")
	assert.False(t, vsOK, "no volume-snapshot ScheduledBackup expected for barman-only config")
}

func TestBackupModel_Barman_PopulatesObjectStoreStatus(t *testing.T) {
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfigBarman("0 2 * * *")
	now := metav1.Now()
	next := metav1.NewTime(now.Add(3600 * 1e9))
	backend := &spyBackupBackend{schedules: map[string]backuptypes.ScheduleResult{
		"c1-backup-objectstore": {Exists: true, LastScheduleTime: &now, NextScheduleTime: &next},
	}}
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

	health, err := model.Observe(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Ready, health.State)
	require.NotNil(t, cluster.Status.BackupStatus)
	require.NotNil(t, cluster.Status.BackupStatus.ObjectStore)
	assert.True(t, cluster.Status.BackupStatus.ObjectStore.Enabled)
	assert.Nil(t, cluster.Status.BackupStatus.VolumeSnapshot)
}

// --- Dual provider (volume snapshot + barman object store) ---

func TestBackupModel_DualProvider_EnsuresBothScheduledBackups(t *testing.T) {
	cluster := newTestCluster("c1", "ns1")
	cnpg := newTestCNPGCluster("c1", "ns1")
	cfg := newTestMergedConfigDualProvider("0 2 * * *")
	backend := &spyBackupBackend{}
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

	require.NoError(t, model.Reconcile(context.Background()))

	vs, ok := backend.ensuredByName("c1-backup")
	require.True(t, ok)
	assert.Equal(t, backuptypes.BackupMethodVolumeSnapshot, vs.Method)
	assert.Empty(t, vs.PluginName)

	os, ok := backend.ensuredByName("c1-backup-objectstore")
	require.True(t, ok)
	assert.Equal(t, backuptypes.BackupMethodPlugin, os.Method)
	assert.Equal(t, "barman-cloud.cloudnative-pg.io", os.PluginName)
}

func TestBackupModel_DualProvider_PopulatesBothStatuses(t *testing.T) {
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfigDualProvider("0 2 * * *")
	backend := &spyBackupBackend{schedules: map[string]backuptypes.ScheduleResult{
		"c1-backup":             {Exists: true},
		"c1-backup-objectstore": {Exists: true},
	}}
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

	health, err := model.Observe(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Ready, health.State)
	require.NotNil(t, cluster.Status.BackupStatus)
	require.NotNil(t, cluster.Status.BackupStatus.VolumeSnapshot)
	assert.True(t, cluster.Status.BackupStatus.VolumeSnapshot.Enabled)
	require.NotNil(t, cluster.Status.BackupStatus.ObjectStore)
	assert.True(t, cluster.Status.BackupStatus.ObjectStore.Enabled)
}

func TestBackupModel_DualProvider_PendingUntilBothScheduledBackupsExist(t *testing.T) {
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfigDualProvider("0 2 * * *")
	// Only the volume-snapshot ScheduledBackup exists yet.
	backend := &spyBackupBackend{schedules: map[string]backuptypes.ScheduleResult{
		"c1-backup": {Exists: true},
	}}
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

	health, err := model.Observe(context.Background(), nil)

	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Pending, health.State)
}

func TestBackupModel_DualProvider_GCsObjectStoreBackupWhenProviderRemoved(t *testing.T) {
	cluster := newTestCluster("c1", "ns1")
	cnpg := newTestCNPGCluster("c1", "ns1")
	backend := &spyBackupBackend{}

	// New config keeps only the volume-snapshot provider; the object-store provider was removed.
	cfg := newTestMergedConfig(true, "0 2 * * *")
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

	require.NoError(t, model.Reconcile(context.Background()))

	// The volume-snapshot provider is ensured; the stale object-store name is asked to be deleted.
	// The adapter's ownership guard (tested there) protects foreign objects with the same name.
	_, ensured := backend.ensuredByName("c1-backup")
	assert.True(t, ensured, "volume-snapshot ScheduledBackup must be ensured")
	assert.Contains(t, backend.deletedNames(), "c1-backup-objectstore",
		"object-store ScheduledBackup must be garbage-collected when barman provider is removed")
	assert.NotContains(t, backend.deletedNames(), "c1-backup",
		"active volume-snapshot ScheduledBackup must not be deleted")
}

func TestBackupModel_Reconcile_DeletesAllNamesWhenDisabled(t *testing.T) {
	cluster := newTestCluster("c1", "ns1")
	cnpg := newTestCNPGCluster("c1", "ns1")
	backend := &spyBackupBackend{}

	// Backup disabled — Reconcile asks the backend to delete every deterministic name. The
	// adapter's ownership guard (tested in infrastructure/cnpg) ensures foreign objects sharing
	// a name are never actually deleted.
	cfg := newTestMergedConfig(false, "")
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

	require.NoError(t, model.Reconcile(context.Background()))

	assert.Empty(t, backend.ensured)
	assert.ElementsMatch(t, []string{"c1-backup", "c1-backup-objectstore"}, backend.deletedNames())
}

func TestBuildCNPGClusterSpec_BarmanDisabledOmitsPlugin(t *testing.T) {
	cfg := newTestMergedConfigBarman("0 2 * * *")
	cfg.Spec.Backup.Enabled = ptr.To(false)

	spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "c1", "secret", false)

	assert.Nil(t, spec.Backup)
	assert.Empty(t, spec.Plugins)
}

func TestBuildCNPGClusterSpec_PreservesForeignPlugins(t *testing.T) {
	t.Run("keeps unmanaged plugins and appends barman entry", func(t *testing.T) {
		cfg := newTestMergedConfigBarman("0 2 * * *")
		live := cnpgv1.ClusterSpec{
			Plugins: []cnpgv1.PluginConfiguration{
				{Name: "some-other.plugin.io"},
			},
		}

		spec := buildCNPGClusterSpec(live, cfg, "c1", "secret", false)

		names := make([]string, 0, len(spec.Plugins))
		for _, p := range spec.Plugins {
			names = append(names, p.Name)
		}
		assert.Contains(t, names, "some-other.plugin.io", "foreign plugin must be preserved")
		assert.Contains(t, names, "barman-cloud.cloudnative-pg.io", "barman plugin must be present")
		assert.Len(t, spec.Plugins, 2)
	})

	t.Run("does not duplicate barman plugin across reconciles", func(t *testing.T) {
		cfg := newTestMergedConfigBarman("0 2 * * *")
		live := cnpgv1.ClusterSpec{
			Plugins: []cnpgv1.PluginConfiguration{
				{Name: "some-other.plugin.io"},
				{Name: "barman-cloud.cloudnative-pg.io", Parameters: map[string]string{"stale": "true"}},
			},
		}

		spec := buildCNPGClusterSpec(live, cfg, "c1", "secret", false)

		var barmanCount int
		for _, p := range spec.Plugins {
			if p.Name == "barman-cloud.cloudnative-pg.io" {
				barmanCount++
				assert.NotContains(t, p.Parameters, "stale", "stale barman entry must be replaced, not kept")
			}
		}
		assert.Equal(t, 1, barmanCount, "barman plugin must appear exactly once")
		assert.Len(t, spec.Plugins, 2)
	})

	t.Run("drops stale barman plugin when backups disabled", func(t *testing.T) {
		cfg := newTestMergedConfigBarman("0 2 * * *")
		cfg.Spec.Backup.Enabled = ptr.To(false)
		live := cnpgv1.ClusterSpec{
			Plugins: []cnpgv1.PluginConfiguration{
				{Name: "some-other.plugin.io"},
				{Name: "barman-cloud.cloudnative-pg.io"},
			},
		}

		spec := buildCNPGClusterSpec(live, cfg, "c1", "secret", false)

		require.Len(t, spec.Plugins, 1)
		assert.Equal(t, "some-other.plugin.io", spec.Plugins[0].Name)
	})
}

func TestBackupModel_NilTarget_UsesDefault(t *testing.T) {
	// Target is *string with kubebuilder default but may be nil when constructed programmatically.
	cluster := newTestCluster("c1", "ns1")
	cnpg := newTestCNPGCluster("c1", "ns1")
	cfg := newTestMergedConfig(true, "0 2 * * *")
	cfg.CNPG.Backup.Target = nil // explicitly nil — must not panic
	backend := &spyBackupBackend{}
	model := newTestBackupModelWithBackend(backend, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

	require.NotPanics(t, func() {
		_ = model.Reconcile(context.Background())
	})
	require.NoError(t, model.Reconcile(context.Background()))

	spec, ok := backend.ensuredByName("c1-backup")
	require.True(t, ok)
	assert.Equal(t, "prefer-standby", spec.Target, "nil target must default to prefer-standby")
}
