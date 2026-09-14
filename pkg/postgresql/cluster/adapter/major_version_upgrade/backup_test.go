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

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	backuptypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/backup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeBackupBackend is a controllable stand-in for onDemandBackupClient.
type fakeBackupBackend struct {
	backupNowErr     error
	backupNowCalls   int
	foundAfterCreate bool
	getResult        backuptypes.BackupResult
	getFound         bool
	getErr           error
	capturedReq      backuptypes.BackupRequest
}

func (f *fakeBackupBackend) BackupNow(_ context.Context, _ client.Object, req backuptypes.BackupRequest) (bool, error) {
	f.backupNowCalls++
	f.capturedReq = req
	return true, f.backupNowErr
}

func (f *fakeBackupBackend) GetBackup(_ context.Context, _ client.Object, _, _ string) (backuptypes.BackupResult, bool, error) {
	if f.foundAfterCreate && f.backupNowCalls == 0 {
		return backuptypes.BackupResult{}, false, f.getErr
	}
	return f.getResult, f.getFound, f.getErr
}

func testBackupIntent() mvutypes.Intent {
	return mvutypes.Intent{
		SourcePgVersion: "17",
		TargetPgVersion: "18",
		Strategy:        mvutypes.MajorUpgradeFlowPgUpgrade,
	}
}

func newBackupTestAdapter(t *testing.T, method backuptypes.BackupMethod, pluginName string, backend onDemandBackupClient) (*RollbackCapabilityAdapter, client.ObjectKey) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	owner := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg-demo", Namespace: "default"},
	}
	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: owner.Namespace},
		Status: cnpgv1.ClusterStatus{
			Phase:           cnpgv1.PhaseHealthy,
			Instances:       1,
			ReadyInstances:  1,
			CurrentPrimary:  "pg-demo-1",
			TargetPrimary:   "pg-demo-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"pg-demo-1"}},
		},
	}
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, cluster).Build()
	key := types.NamespacedName{Name: owner.Name, Namespace: owner.Namespace}

	adapter := &RollbackCapabilityAdapter{
		client:     k8sClient,
		key:        key,
		backend:    backend,
		method:     method,
		pluginName: pluginName,
	}
	return adapter, key
}

func TestCreateBackupWaitsForCNPGHealthyTargetStatus(t *testing.T) {
	backend := &fakeBackupBackend{}
	adapter, key := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)

	cluster := &cnpgv1.Cluster{}
	require.NoError(t, adapter.client.Get(t.Context(), key, cluster))
	cluster.Status.InstancesStatus = nil
	require.NoError(t, adapter.client.Update(t.Context(), cluster))

	info, err := adapter.CreateBackup(t.Context(), testBackupIntent(), mvutypes.PostUpgradeBackupName)
	require.ErrorIs(t, err, mvutypes.ErrRollbackCapabilityNotReady)
	assert.Contains(t, err.Error(), `target primary "pg-demo-1"`)
	assert.Nil(t, info)
	assert.Zero(t, backend.backupNowCalls, "must not create a CNPG Backup before its target is healthy in cluster status")

	cluster.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {cluster.Status.TargetPrimary}}
	require.NoError(t, adapter.client.Update(t.Context(), cluster))
	backend.foundAfterCreate = true
	backend.getFound = true
	backend.getResult = backuptypes.BackupResult{Done: true}

	info, err = adapter.CreateBackup(t.Context(), testBackupIntent(), mvutypes.PostUpgradeBackupName)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, 1, backend.backupNowCalls)
}

func TestCreateBackupObservesExistingBackupWhileCNPGIsUpgrading(t *testing.T) {
	backend := &fakeBackupBackend{getFound: true, getResult: backuptypes.BackupResult{Done: true}}
	adapter, key := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)

	cluster := &cnpgv1.Cluster{}
	require.NoError(t, adapter.client.Get(t.Context(), key, cluster))
	cluster.Status.Phase = cnpgv1.PhaseMajorUpgrade
	cluster.Status.InstancesStatus = nil
	require.NoError(t, adapter.client.Update(t.Context(), cluster))

	info, err := adapter.CreateBackup(t.Context(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Zero(t, backend.backupNowCalls, "an existing backup must not be recreated")
}

func TestCreateBackupReturnsMissingWhenNotDone(t *testing.T) {
	backend := &fakeBackupBackend{getFound: true, getResult: backuptypes.BackupResult{Done: false}}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)

	info, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.ErrorIs(t, err, mvutypes.ErrBackupStatusMissing)
	assert.Nil(t, info)
}

func TestCreateBackupReturnsMissingWhenNotFound(t *testing.T) {
	backend := &fakeBackupBackend{getFound: false}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)

	info, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.ErrorIs(t, err, mvutypes.ErrBackupStatusMissing)
	assert.Nil(t, info)
}

func TestCreateBackupWrapsBackupFailedError(t *testing.T) {
	backend := &fakeBackupBackend{
		getFound:  true,
		getResult: backuptypes.BackupResult{Done: false, Failed: true, Error: "disk full"},
	}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)

	info, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.Contains(t, err.Error(), "disk full")
	assert.Nil(t, info)
}

func TestCreateBackupReturnsDoneWithVolumeSnapshotStatus(t *testing.T) {
	backend := &fakeBackupBackend{getFound: true, getResult: backuptypes.BackupResult{Done: true}}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)

	info, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, info.BackupStatus)
	assert.NotNil(t, info.BackupStatus.VolumeSnapshot, "VolumeSnapshot status should be set for VolumeSnapshot method")
}

func TestCreateBackupReturnsDoneWithoutVolumeSnapshotStatusForPluginMethod(t *testing.T) {
	backend := &fakeBackupBackend{getFound: true, getResult: backuptypes.BackupResult{Done: true}}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodPlugin, "barman-cloud.cloudnative-pg.io", backend)

	info, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, info.BackupStatus)
	assert.Nil(t, info.BackupStatus.VolumeSnapshot, "VolumeSnapshot status must not be set for Plugin method")
	require.NotNil(t, info.BackupStatus.ObjectStore, "ObjectStore status must be set for Plugin method")
	assert.True(t, info.BackupStatus.ObjectStore.Enabled)
}

func TestCreateBackupPassesMethodAndPluginToBackend(t *testing.T) {
	backend := &fakeBackupBackend{foundAfterCreate: true, getFound: true, getResult: backuptypes.BackupResult{Done: true}}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodPlugin, "barman-cloud.cloudnative-pg.io", backend)

	_, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.NoError(t, err)
	assert.Equal(t, backuptypes.BackupMethodPlugin, backend.capturedReq.Method)
	assert.Equal(t, "barman-cloud.cloudnative-pg.io", backend.capturedReq.PluginName)
}

func TestCreateBackupProducesDeterministicName(t *testing.T) {
	backend := &fakeBackupBackend{foundAfterCreate: true, getFound: true, getResult: backuptypes.BackupResult{Done: true}}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)

	info, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.NoError(t, err)
	// Name is "{owner.Name}-{generated suffix}" — deterministic across reconciles.
	expectedName := "pg-demo-" + mvutypes.PreUpgradeBackupName(testBackupIntent())
	assert.Equal(t, expectedName, info.BackupName)
	assert.Equal(t, expectedName, backend.capturedReq.Name)
}

func TestCreateBackupPreAndPostNamesAreDifferent(t *testing.T) {
	backend := &fakeBackupBackend{getFound: true, getResult: backuptypes.BackupResult{Done: true}}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)
	intent := testBackupIntent()

	infoA, err := adapter.CreateBackup(context.Background(), intent, mvutypes.PreUpgradeBackupName)
	require.NoError(t, err)

	infoB, err := adapter.CreateBackup(context.Background(), intent, mvutypes.PostUpgradeBackupName)
	require.NoError(t, err)

	assert.NotEqual(t, infoA.BackupName, infoB.BackupName, "pre- and post-upgrade backup names must differ")
}

func TestCreateBackupPropagatesOwnerFetchError(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))

	// No objects registered — Get will return NotFound.
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	adapter := &RollbackCapabilityAdapter{
		client:  k8sClient,
		key:     types.NamespacedName{Name: "missing", Namespace: "default"},
		backend: &fakeBackupBackend{},
		method:  backuptypes.BackupMethodVolumeSnapshot,
	}

	_, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching PostgresCluster")
}

func TestCreateBackupPropagatesBackupNowError(t *testing.T) {
	backupNowErr := errors.New("backend unavailable")
	backend := &fakeBackupBackend{backupNowErr: backupNowErr}
	adapter, _ := newBackupTestAdapter(t, backuptypes.BackupMethodVolumeSnapshot, "", backend)

	_, err := adapter.CreateBackup(context.Background(), testBackupIntent(), mvutypes.PreUpgradeBackupName)
	require.Error(t, err)
	assert.ErrorContains(t, err, "backend unavailable")
}
