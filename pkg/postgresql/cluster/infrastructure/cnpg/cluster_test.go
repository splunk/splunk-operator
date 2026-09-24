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
