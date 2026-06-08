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
	contracts := &reconcileContracts{}
	if len(cnpg) > 0 {
		contracts.CNPGCluster = cnpg[0]
	}
	return newBackupModel(c, scheme, events, updater, cluster, cfg, contracts)
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

	t.Run("deletes existing scheduled backup when disabled", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(false, "")
		existingSB := &cnpgv1.ScheduledBackup{ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingSB).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert
		require.NoError(t, err)
		getErr := c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, &cnpgv1.ScheduledBackup{})
		assert.True(t, apierrors.IsNotFound(getErr))
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

	t.Run("creates scheduled backup", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		emitter := &captureBackupEmitter{}
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		model := newTestBackupModel(c, scheme, emitter, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert
		require.NoError(t, err)
		sb := &cnpgv1.ScheduledBackup{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, sb))
		assert.Equal(t, "0 0 2 * * *", sb.Spec.Schedule)
		assert.Equal(t, cnpgv1.BackupMethodVolumeSnapshot, sb.Spec.Method)
		assert.Equal(t, cnpgv1.BackupTargetStandby, sb.Spec.Target)
		assert.Equal(t, "c1", sb.Spec.Cluster.Name)
		assert.Contains(t, emitter.normals[0], EventScheduledBackupCreated)
	})

	t.Run("updates existing scheduled backup", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "30 3 * * *")
		existingSB := &cnpgv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"},
			Spec: cnpgv1.ScheduledBackupSpec{
				Schedule: "0 0 2 * * *",
				Cluster:  cnpgv1.LocalObjectReference{Name: "c1"},
				Method:   cnpgv1.BackupMethodVolumeSnapshot,
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingSB).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert
		require.NoError(t, err)
		sb := &cnpgv1.ScheduledBackup{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, sb))
		assert.Equal(t, "0 30 3 * * *", sb.Spec.Schedule)
	})

	t.Run("uses target from cnpg config", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		cfg.CNPG.Backup.Target = ptr.To("primary")
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, cnpg)

		// Act
		err := model.Reconcile(context.Background())

		// Assert
		require.NoError(t, err)
		sb := &cnpgv1.ScheduledBackup{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "c1-backup", Namespace: "ns1"}, sb))
		assert.Equal(t, cnpgv1.BackupTargetPrimary, sb.Spec.Target)
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
		assert.Equal(t, reasonBackupVolumeSnapshotMissing, health.Reason)
	})
}

func TestBackupModel_Reconcile_CreateError(t *testing.T) {
	// Arrange
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfig(true, "0 2 * * *")
	errClient := createErrorClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		err:    apierrors.NewServiceUnavailable("unavailable"),
		matcher: func(obj client.Object) bool {
			_, ok := obj.(*cnpgv1.ScheduledBackup)
			return ok
		},
	}
	emitter := &captureBackupEmitter{}
	model := newTestBackupModel(errClient, scheme, emitter, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

	// Act
	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.Error(t, err)
	assert.Equal(t, pgcConstants.Failed, health.State)
	assert.Len(t, emitter.warnings, 1)
}

func TestBackupModel_Reconcile_DeleteError(t *testing.T) {
	// Arrange
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfig(false, "")
	errClient := getErrorClient{
		Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
		err:    apierrors.NewForbidden(schema.GroupResource{Resource: "scheduledbackups"}, "c1-backup", nil),
		matcher: func(obj client.Object) bool {
			_, ok := obj.(*cnpgv1.ScheduledBackup)
			return ok
		},
	}
	model := newTestBackupModel(errClient, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

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
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		sb := &cnpgv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"},
			Spec: cnpgv1.ScheduledBackupSpec{
				Schedule: "0 0 2 * * *",
				Cluster:  cnpgv1.LocalObjectReference{Name: "c1"},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, sb).WithStatusSubresource(cluster).Build()
		emitter := &captureBackupEmitter{}
		model := newTestBackupModel(c, scheme, emitter, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

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
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		// Act
		health, err := model.Observe(context.Background(), nil)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Pending, health.State)
		assert.Equal(t, reasonScheduledBackupCreated, health.Reason)
	})

	t.Run("get error returns failed", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		errClient := getErrorClient{
			Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
			err:    apierrors.NewServiceUnavailable("down"),
			matcher: func(obj client.Object) bool {
				_, ok := obj.(*cnpgv1.ScheduledBackup)
				return ok
			},
		}
		model := newTestBackupModel(errClient, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		// Act
		health, err := model.Observe(context.Background(), nil)

		// Assert
		require.Error(t, err)
		assert.Equal(t, pgcConstants.Failed, health.State)
	})

	t.Run("populates schedule times from ScheduledBackup status", func(t *testing.T) {
		// Arrange
		cluster := newTestCluster("c1", "ns1")
		cfg := newTestMergedConfig(true, "0 2 * * *")
		now := metav1.Now()
		next := metav1.NewTime(now.Add(24 * 60 * 60 * 1e9))
		sb := &cnpgv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"},
			Spec: cnpgv1.ScheduledBackupSpec{
				Schedule: "0 0 2 * * *",
				Cluster:  cnpgv1.LocalObjectReference{Name: "c1"},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, sb).WithStatusSubresource(sb).Build()
		sb.Status = cnpgv1.ScheduledBackupStatus{LastScheduleTime: &now, NextScheduleTime: &next}
		require.NoError(t, c.Status().Update(context.Background(), sb))
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

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

		sb := &cnpgv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, sb).WithStatusSubresource(cluster, sb).Build()
		require.NoError(t, c.Status().Update(ctx, cluster))

		// New timestamps — only change this reconcile.
		now2 := metav1.Now()
		next2 := metav1.NewTime(now2.Add(24 * time.Hour))
		sb.Status = cnpgv1.ScheduledBackupStatus{LastScheduleTime: &now2, NextScheduleTime: &next2}
		require.NoError(t, c.Status().Update(ctx, sb))

		updater := func(before *enterprisev4.PostgresClusterStatus, health componentHealth) error {
			return setStatusFromHealth(ctx, c, nil, cluster, before, health)
		}
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, updater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

		health, err := model.Observe(ctx, nil)

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
		sb := &cnpgv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"},
			Spec:       cnpgv1.ScheduledBackupSpec{Schedule: "0 0 2 * * *", Cluster: cnpgv1.LocalObjectReference{Name: "c1"}},
		}
		ctx := context.Background()
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, sb).WithStatusSubresource(cluster, sb).Build()
		sb.Status = cnpgv1.ScheduledBackupStatus{LastScheduleTime: &now, NextScheduleTime: &next}
		require.NoError(t, c.Status().Update(ctx, sb))

		updater := func(before *enterprisev4.PostgresClusterStatus, health componentHealth) error {
			return setStatusFromHealth(ctx, c, nil, cluster, before, health)
		}
		model := newTestBackupModel(c, scheme, noopBackupEmitter{}, updater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

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
		normalized := normalizeCNPGClusterSpec(spec, nil)

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
		normalized := normalizeCNPGClusterSpec(spec, nil)

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
		normalized := normalizeCNPGClusterSpec(spec, nil)

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
		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "secret", false)

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
		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "secret", false)

		// Assert
		assert.Nil(t, spec.Backup)
	})

	t.Run("no backup section when cnpg backup nil", func(t *testing.T) {
		// Arrange
		cfg := newTestMergedConfig(true, "0 2 * * *")
		cfg.CNPG.Backup = nil

		// Act
		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "secret", false)

		// Assert
		assert.Nil(t, spec.Backup)
	})
}

func TestBackupModel_Reconcile_RepairsOrphanedOwnerRef(t *testing.T) {
	t.Parallel()

	// Arrange: ScheduledBackup exists but has no owner reference — simulates a resource
	// that was created outside the controller or lost its owner ref.
	scheme := newTestScheme()
	cluster := newTestCluster("c1", "ns1")
	cfg := newTestMergedConfig(true, "0 2 * * *")
	orphanedSB := &cnpgv1.ScheduledBackup{
		ObjectMeta: metav1.ObjectMeta{Name: "c1-backup", Namespace: "ns1"},
		Spec: cnpgv1.ScheduledBackupSpec{
			Schedule: "0 0 2 * * *",
			Cluster:  cnpgv1.LocalObjectReference{Name: "c1"},
			Method:   cnpgv1.BackupMethodVolumeSnapshot,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(orphanedSB).Build()
	model := newTestBackupModel(c, scheme, noopBackupEmitter{}, noopHealthUpdater, cluster, cfg, newTestCNPGCluster("c1", "ns1"))

	// Act
	err := model.Reconcile(context.Background())

	// Assert
	require.NoError(t, err)
	adopted := &cnpgv1.ScheduledBackup{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "c1-backup", Namespace: "ns1"}, adopted))
	require.Len(t, adopted.OwnerReferences, 1, "owner reference must be set after repair")
	assert.Equal(t, cluster.Name, adopted.OwnerReferences[0].Name)
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
