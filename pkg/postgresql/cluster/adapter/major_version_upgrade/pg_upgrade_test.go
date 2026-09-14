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
	"errors"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPgUpgradeDriverStartUpgradePatchesCNPGImage(t *testing.T) {
	ctx := t.Context()
	k8sClient, key := newPgUpgradeTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName: "ghcr.io/cloudnative-pg/postgresql:17",
		},
	})

	err := NewPgUpgradeDriver(k8sClient, key, "18").ApplyTargetImage(ctx)
	require.NoError(t, err)

	cluster := &cnpgv1.Cluster{}
	require.NoError(t, k8sClient.Get(ctx, key, cluster))
	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18", cluster.Spec.ImageName)
}

func TestPgUpgradeDriverStartUpgradePatchesCNPGImagePullSecrets(t *testing.T) {
	ctx := t.Context()
	k8sClient, key := newPgUpgradeTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName: "ghcr.io/cloudnative-pg/postgresql:18",
		},
	})

	err := NewPgUpgradeDriver(k8sClient, key, "18").
		WithImagePullSecrets([]cnpgv1.LocalObjectReference{{Name: "target-registry-creds"}}).
		ApplyTargetImage(ctx)
	require.NoError(t, err)

	cluster := &cnpgv1.Cluster{}
	require.NoError(t, k8sClient.Get(ctx, key, cluster))
	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18", cluster.Spec.ImageName)
	assert.Equal(t, []cnpgv1.LocalObjectReference{{Name: "target-registry-creds"}}, cluster.Spec.ImagePullSecrets)
}

func TestPgUpgradeDriverUpgradeCompleteWaitsForCNPGMajorUpgrade(t *testing.T) {
	ctx := t.Context()
	k8sClient, key := newPgUpgradeTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName: "ghcr.io/cloudnative-pg/postgresql:18",
		},
		Status: cnpgv1.ClusterStatus{
			Phase:          cnpgv1.PhaseMajorUpgrade,
			Instances:      3,
			ReadyInstances: 2,
			CurrentPrimary: "pg1-1",
		},
	})

	complete, err := NewPgUpgradeDriver(k8sClient, key, "18").UpgradeComplete(ctx)
	require.NoError(t, err)
	assert.False(t, complete)
}

func TestPgUpgradeDriverUpgradeCompleteRequiresCNPGConversionEvidence(t *testing.T) {
	tests := []struct {
		name              string
		pgDataMajor       int
		conversionPending bool
		want              bool
	}{
		{name: "old PGDATA despite matching desired image", pgDataMajor: 17},
		{name: "conversion marker remains", pgDataMajor: 18, conversionPending: true},
		{name: "target PGDATA and conversion complete", pgDataMajor: 18, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8sClient, key := newPgUpgradeTestClient(t, healthyUpgradeCluster(tt.pgDataMajor, tt.conversionPending))
			complete, err := NewPgUpgradeDriver(k8sClient, key, "18").UpgradeComplete(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.want, complete)
		})
	}
}

func TestPgUpgradeDriverUpgradeCompleteDoesNotWaitForRebuildingReplica(t *testing.T) {
	cluster := healthyUpgradeCluster(18, false)
	cluster.Status.Instances = 3
	cluster.Status.ReadyInstances = 2
	cluster.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{
		cnpgv1.PodHealthy: {cluster.Status.CurrentPrimary, "pg1-2"},
	}
	k8sClient, key := newPgUpgradeTestClient(t, cluster)

	complete, err := NewPgUpgradeDriver(k8sClient, key, "18").UpgradeComplete(t.Context())
	require.NoError(t, err)
	assert.True(t, complete)
}

func TestPgUpgradeDriverVerifyUpgradeRejectsWrongImage(t *testing.T) {
	ctx := t.Context()
	k8sClient, key := newPgUpgradeTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName: "ghcr.io/cloudnative-pg/postgresql:17",
		},
		Status: cnpgv1.ClusterStatus{
			Phase:          cnpgv1.PhaseHealthy,
			Instances:      3,
			ReadyInstances: 3,
			CurrentPrimary: "pg1-1",
		},
	})

	verified, err := NewPgUpgradeDriver(k8sClient, key, "18").VerifyUpgrade(ctx)
	require.Error(t, err)
	assert.False(t, verified)
	assert.Contains(t, err.Error(), "does not match target image")
}

func TestPgUpgradeDriverVerifyUpgradeRejectsOldPGData(t *testing.T) {
	k8sClient, key := newPgUpgradeTestClient(t, healthyUpgradeCluster(17, false))

	verified, err := NewPgUpgradeDriver(k8sClient, key, "18").VerifyUpgrade(t.Context())
	require.Error(t, err)
	assert.False(t, verified)
	assert.Contains(t, err.Error(), "targetMajor=18 observedPGDataMajor=17")
}

func TestPgUpgradeDriverVerifyUpgradeWaitsForTransientClusterHealth(t *testing.T) {
	cluster := healthyUpgradeCluster(18, false)
	cluster.Status.InstancesStatus = nil
	k8sClient, key := newPgUpgradeTestClient(t, cluster)

	verified, err := NewPgUpgradeDriver(k8sClient, key, "18").VerifyUpgrade(t.Context())
	require.NoError(t, err)
	assert.False(t, verified)
}

func TestPgUpgradeDriverVerifyUpgradeDoesNotWaitForRebuildingReplica(t *testing.T) {
	cluster := healthyUpgradeCluster(18, false)
	cluster.Status.Instances = 3
	cluster.Status.ReadyInstances = 2
	cluster.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{
		cnpgv1.PodHealthy: {cluster.Status.CurrentPrimary, "pg1-2"},
	}
	k8sClient, key := newPgUpgradeTestClient(t, cluster)

	verified, err := NewPgUpgradeDriver(k8sClient, key, "18").VerifyUpgrade(t.Context())
	require.NoError(t, err)
	assert.True(t, verified)
}

func TestPgUpgradeDriverReturnsBlockingCNPGPhaseError(t *testing.T) {
	ctx := t.Context()
	k8sClient, key := newPgUpgradeTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName: "ghcr.io/cloudnative-pg/postgresql:18",
		},
		Status: cnpgv1.ClusterStatus{
			Phase:       cnpgv1.PhaseWaitingForUser,
			PhaseReason: "primary cannot be restarted",
		},
	})

	complete, err := NewPgUpgradeDriver(k8sClient, key, "18").UpgradeComplete(ctx)
	assert.False(t, complete)
	require.Error(t, err)
	assert.True(t, errors.Is(err, mvutypes.ErrUpgradeFlowFailed))
	assert.Contains(t, err.Error(), "requires user action")
}

func TestPgUpgradeDriverUsesInjectedImageResolver(t *testing.T) {
	ctx := t.Context()
	k8sClient, key := newPgUpgradeTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
		},
		Spec: cnpgv1.ClusterSpec{
			ImageName: "old",
		},
	})

	err := NewPgUpgradeDriver(k8sClient, key, "18").
		WithImageForVersion(func(version string) (string, error) {
			return "registry.example/postgres:" + version, nil
		}).
		ApplyTargetImage(ctx)
	require.NoError(t, err)

	cluster := &cnpgv1.Cluster{}
	require.NoError(t, k8sClient.Get(ctx, key, cluster))
	assert.Equal(t, "registry.example/postgres:18", cluster.Spec.ImageName)
}

func TestPgUpgradeDriverApplyTargetImageTerminatesOnEmptyVersion(t *testing.T) {
	ctx := t.Context()
	k8sClient, key := newPgUpgradeTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	})

	err := NewPgUpgradeDriver(k8sClient, key, "").ApplyTargetImage(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, mvutypes.ErrUpgradeFlowFailed))
}

func TestPgUpgradeDriverApplyTargetImageTerminatesOnUnresolvableImage(t *testing.T) {
	ctx := t.Context()
	k8sClient, key := newPgUpgradeTestClient(t, &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	})

	err := NewPgUpgradeDriver(k8sClient, key, "99").
		WithImageForVersion(func(string) (string, error) { return "", nil }).
		ApplyTargetImage(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, mvutypes.ErrUpgradeFlowFailed))
}

func healthyUpgradeCluster(pgDataMajor int, conversionPending bool) *cnpgv1.Cluster {
	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec:       cnpgv1.ClusterSpec{ImageName: "ghcr.io/cloudnative-pg/postgresql:18"},
		Status: cnpgv1.ClusterStatus{
			Phase:           cnpgv1.PhaseHealthy,
			Instances:       1,
			ReadyInstances:  1,
			CurrentPrimary:  "pg1-1",
			TargetPrimary:   "pg1-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"pg1-1"}},
			PGDataImageInfo: &cnpgv1.ImageInfo{MajorVersion: pgDataMajor},
		},
	}
	if conversionPending {
		cluster.Status.TargetPGDataImageInfo = &cnpgv1.ImageInfo{MajorVersion: 18}
	}
	return cluster
}

func newPgUpgradeTestClient(t *testing.T, cluster *cnpgv1.Cluster) (client.Client, client.ObjectKey) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	key := client.ObjectKeyFromObject(cluster)
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&cnpgv1.Cluster{}).
		WithObjects(cluster).
		Build(), key
}
