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
package connectioninfo

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceFQDN(t *testing.T) {
	t.Run("builds service fqdn", func(t *testing.T) {
		got, err := ServiceFQDN("cluster-rw", "default")

		require.NoError(t, err)
		assert.Equal(t, "cluster-rw.default.svc.cluster.local", got)
	})

	t.Run("rejects empty service name", func(t *testing.T) {
		_, err := ServiceFQDN("", "default")

		require.Error(t, err)
		assert.ErrorContains(t, err, "service name is required")
	})

	t.Run("rejects empty namespace", func(t *testing.T) {
		_, err := ServiceFQDN("cluster-rw", "")

		require.Error(t, err)
		assert.ErrorContains(t, err, "namespace is required")
	})
}

func TestEndpointsValidate(t *testing.T) {
	tests := []struct {
		name      string
		endpoints Endpoints
		wantError string
	}{
		{
			name: "valid direct endpoints",
			endpoints: Endpoints{
				RWHost: "cluster-rw.default.svc.cluster.local",
				ROHost: "cluster-ro.default.svc.cluster.local",
				RHost:  "cluster-r.default.svc.cluster.local",
			},
		},
		{
			name: "valid pooler endpoints",
			endpoints: Endpoints{
				RWHost:       "cluster-rw.default.svc.cluster.local",
				ROHost:       "cluster-ro.default.svc.cluster.local",
				RHost:        "cluster-r.default.svc.cluster.local",
				PoolerRWHost: "cluster-pooler-rw.default.svc.cluster.local",
				PoolerROHost: "cluster-pooler-ro.default.svc.cluster.local",
			},
		},
		{
			name: "missing rw host",
			endpoints: Endpoints{
				ROHost: "cluster-ro.default.svc.cluster.local",
				RHost:  "cluster-r.default.svc.cluster.local",
			},
			wantError: "RWHost is required",
		},
		{
			name: "missing ro host",
			endpoints: Endpoints{
				RWHost: "cluster-rw.default.svc.cluster.local",
				RHost:  "cluster-r.default.svc.cluster.local",
			},
			wantError: "ROHost is required",
		},
		{
			name: "missing r host",
			endpoints: Endpoints{
				RWHost: "cluster-rw.default.svc.cluster.local",
				ROHost: "cluster-ro.default.svc.cluster.local",
			},
			wantError: "RHost is required",
		},
		{
			name: "unpaired pooler host",
			endpoints: Endpoints{
				RWHost:       "cluster-rw.default.svc.cluster.local",
				ROHost:       "cluster-ro.default.svc.cluster.local",
				RHost:        "cluster-r.default.svc.cluster.local",
				PoolerRWHost: "cluster-pooler-rw.default.svc.cluster.local",
			},
			wantError: "pooler endpoints must both be set or both be empty",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			err := tst.endpoints.Validate()

			if tst.wantError == "" {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			assert.ErrorContains(t, err, tst.wantError)
		})
	}
}

func TestBuildConfigMapData(t *testing.T) {
	t.Run("uses shared schema keys", func(t *testing.T) {
		got, required, err := BuildConfigMapData(
			Endpoints{
				RWHost:       "cluster-rw.default.svc.cluster.local",
				ROHost:       "cluster-ro.default.svc.cluster.local",
				RHost:        "cluster-r.default.svc.cluster.local",
				PoolerRWHost: "cluster-pooler-rw.default.svc.cluster.local",
				PoolerROHost: "cluster-pooler-ro.default.svc.cluster.local",
			},
		)

		require.NoError(t, err)
		assert.ElementsMatch(t, []string{
			KeyPoolerROEndpoint,
			KeyPoolerRWEndpoint,
			KeyClusterROEndpoint,
			KeyClusterRWEndpoint,
			KeyClusterREndpoint,
			KeyDefaultClusterPort,
		}, slices.Collect(maps.Keys(got)))
		assert.ElementsMatch(t, RequiredKeys(), required)
	})

	t.Run("adds required keys from local options", func(t *testing.T) {
		got, required, err := BuildConfigMapData(
			Endpoints{
				RWHost: "cluster-rw.default.svc.cluster.local",
				ROHost: "cluster-ro.default.svc.cluster.local",
				RHost:  "cluster-r.default.svc.cluster.local",
			},
			func(builder *Builder) {
				builder.SetRequired("DATABASE_NAME", "payments")
			},
		)

		require.NoError(t, err)
		assert.Equal(t, "payments", got["DATABASE_NAME"])
		assert.ElementsMatch(t, append(RequiredKeys(), "DATABASE_NAME"), required)
	})

	t.Run("fails when required option value is empty", func(t *testing.T) {
		_, _, err := BuildConfigMapData(
			Endpoints{
				RWHost: "cluster-rw.default.svc.cluster.local",
				ROHost: "cluster-ro.default.svc.cluster.local",
				RHost:  "cluster-r.default.svc.cluster.local",
			},
			func(builder *Builder) {
				builder.SetRequired("DATABASE_NAME", "")
			},
		)

		require.Error(t, err)
		assert.ErrorContains(t, err, "required key DATABASE_NAME is empty")
	})

	t.Run("fails when endpoints are incomplete", func(t *testing.T) {
		_, _, err := BuildConfigMapData(Endpoints{})

		require.Error(t, err)
		assert.ErrorContains(t, err, "RWHost is required")
	})
}

func TestRequiredKeys(t *testing.T) {
	assert.ElementsMatch(t, []string{
		KeyClusterRWEndpoint,
		KeyClusterROEndpoint,
		KeyClusterREndpoint,
		KeyDefaultClusterPort,
	}, RequiredKeys())
}
