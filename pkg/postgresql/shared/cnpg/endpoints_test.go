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

	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveConnectionEndpoints(t *testing.T) {
	tests := []struct {
		name             string
		clusterName      string
		namespace        string
		writeServiceName string
		readServiceName  string
		poolerEnabled    bool
		want             pgconninfo.Endpoints
		wantError        string
	}{
		{
			name:            "requires write service name",
			clusterName:     "tenant",
			namespace:       "default",
			readServiceName: "tenant-ro",
			poolerEnabled:   true,
			wantError:       "write service name is required",
		},
		{
			name:             "requires read service name",
			clusterName:      "tenant",
			namespace:        "default",
			writeServiceName: "tenant-rw",
			wantError:        "read service name is required",
		},
		{
			name:             "uses explicit service names",
			clusterName:      "tenant",
			namespace:        "default",
			writeServiceName: "custom-rw",
			readServiceName:  "custom-ro",
			want: pgconninfo.Endpoints{
				RWHost: "custom-rw.default.svc.cluster.local",
				ROHost: "custom-ro.default.svc.cluster.local",
				RHost:  "tenant-r.default.svc.cluster.local",
			},
		},
		{
			name:             "includes pooler endpoints when enabled",
			clusterName:      "tenant",
			namespace:        "default",
			writeServiceName: "tenant-rw",
			readServiceName:  "tenant-ro",
			poolerEnabled:    true,
			want: pgconninfo.Endpoints{
				RWHost:       "tenant-rw.default.svc.cluster.local",
				ROHost:       "tenant-ro.default.svc.cluster.local",
				RHost:        "tenant-r.default.svc.cluster.local",
				PoolerRWHost: "tenant-pooler-rw.default.svc.cluster.local",
				PoolerROHost: "tenant-pooler-ro.default.svc.cluster.local",
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			got, err := ResolveConnectionEndpoints(tst.clusterName, tst.namespace, tst.writeServiceName, tst.readServiceName, tst.poolerEnabled)

			if tst.wantError == "" {
				require.NoError(t, err)
				assert.Equal(t, tst.want, got)
				return
			}

			require.Error(t, err)
			assert.ErrorContains(t, err, tst.wantError)
		})
	}
}
