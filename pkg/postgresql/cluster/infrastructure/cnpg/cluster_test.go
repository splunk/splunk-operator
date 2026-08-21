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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresImageNameFormatsVersion(t *testing.T) {
	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18", PostgresImageName("18"))
	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18.1", PostgresImageName("18.1"))
}

func TestClusterReadyRequiresHealthyReadyInstancesAndPrimary(t *testing.T) {
	cluster := &cnpgv1.Cluster{
		Status: cnpgv1.ClusterStatus{
			Phase:          cnpgv1.PhaseHealthy,
			Instances:      3,
			ReadyInstances: 3,
			CurrentPrimary: "pg1-1",
		},
	}

	assert.True(t, ClusterReady(cluster))
}

func TestPrimaryReadyDoesNotRequireEveryReplica(t *testing.T) {
	cluster := &cnpgv1.Cluster{
		Status: cnpgv1.ClusterStatus{
			Phase:           cnpgv1.PhaseHealthy,
			Instances:       3,
			ReadyInstances:  2,
			CurrentPrimary:  "pg1-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"pg1-1", "pg1-2"}},
		},
	}

	assert.True(t, PrimaryReady(cluster))
	cluster.Status.InstancesStatus = nil
	assert.False(t, PrimaryReady(cluster))
}

func TestBackupTargetReadinessRequiresPublishedHealthyPrimaryFallback(t *testing.T) {
	err := BackupTargetReadiness(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster is missing")

	cluster := &cnpgv1.Cluster{
		Status: cnpgv1.ClusterStatus{
			Phase:           cnpgv1.PhaseHealthy,
			Instances:       1,
			ReadyInstances:  1,
			CurrentPrimary:  "pg1-1",
			TargetPrimary:   "pg1-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"pg1-1"}},
		},
	}

	targetPrimary := cluster.Status.TargetPrimary
	cluster.Status.TargetPrimary = ""
	err = BackupTargetReadiness(cluster)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target primary is not published")
	cluster.Status.TargetPrimary = targetPrimary

	require.NoError(t, BackupTargetReadiness(cluster))

	cluster.Status.Instances = 3
	cluster.Status.ReadyInstances = 2
	cluster.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{
		cnpgv1.PodHealthy: {"pg1-1", "pg1-2"},
	}
	require.NoError(t, BackupTargetReadiness(cluster), "a rebuilding replica must not block the usable backup target")

	cluster.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{
		cnpgv1.PodHealthy: {"pg1-2"},
	}
	err = BackupTargetReadiness(cluster)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `target primary "pg1-1"`)
	assert.Contains(t, err.Error(), "pg1-2")
}

func TestClusterBlockingErrorReturnsUserActionError(t *testing.T) {
	err := ClusterBlockingError(&cnpgv1.Cluster{
		Status: cnpgv1.ClusterStatus{
			Phase:       cnpgv1.PhaseWaitingForUser,
			PhaseReason: "primary cannot restart",
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires user action")
}
