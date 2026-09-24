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
	"testing"

	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildClusterConfigMapData(t *testing.T) {
	t.Run("builds shared and cluster-specific keys", func(t *testing.T) {
		data, required, err := buildClusterConfigMapData(
			pgconninfo.Endpoints{
				RWHost:       "cluster-rw.default.svc.cluster.local",
				ROHost:       "cluster-ro.default.svc.cluster.local",
				RHost:        "cluster-r.default.svc.cluster.local",
				PoolerRWHost: "cluster-pooler-rw.default.svc.cluster.local",
				PoolerROHost: "cluster-pooler-ro.default.svc.cluster.local",
			},
			"postgres",
			"cluster-secret",
			"server-ca-ref",
		)

		require.NoError(t, err)
		assert.Equal(t, "postgres", data[configMapKeySuperUserName])
		assert.Equal(t, "cluster-secret", data[configMapKeySuperUserSecretRef])
		assert.Equal(t, "server-ca-ref", data[configMapKeyServerCASecretRef])
		assert.ElementsMatch(t, []string{
			pgconninfo.KeyClusterRWEndpoint,
			pgconninfo.KeyClusterROEndpoint,
			pgconninfo.KeyClusterREndpoint,
			pgconninfo.KeyDefaultClusterPort,
			configMapKeySuperUserName,
			configMapKeySuperUserSecretRef,
		}, required)
	})

	t.Run("fails when required superuser values are missing", func(t *testing.T) {
		_, _, err := buildClusterConfigMapData(
			pgconninfo.Endpoints{
				RWHost: "cluster-rw.default.svc.cluster.local",
				ROHost: "cluster-ro.default.svc.cluster.local",
				RHost:  "cluster-r.default.svc.cluster.local",
			},
			"",
			"cluster-secret",
			"",
		)

		require.Error(t, err)
		assert.ErrorContains(t, err, "required key SUPER_USER_NAME is empty")
	})
}

func TestRequiredClusterConfigMapKeys(t *testing.T) {
	assert.ElementsMatch(t, []string{
		pgconninfo.KeyClusterRWEndpoint,
		pgconninfo.KeyClusterROEndpoint,
		pgconninfo.KeyClusterREndpoint,
		pgconninfo.KeyDefaultClusterPort,
		configMapKeySuperUserName,
		configMapKeySuperUserSecretRef,
	}, requiredClusterConfigMapKeys())
}
