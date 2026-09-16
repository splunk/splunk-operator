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
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestConnectionEndpointResolver(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *cnpgv1.Cluster
		request  dbtypes.ConnectionEndpointRequest
		want     pgconninfo.Endpoints
		wantErr  string
		notFound bool
	}{
		{
			name:    "direct endpoints",
			cluster: cnpgClusterWithEndpoints(2, "primary-rw", "primary-ro"),
			request: dbtypes.ConnectionEndpointRequest{ProviderResourceName: "primary", Namespace: "dbs"},
			want: pgconninfo.Endpoints{
				RWHost: "primary-rw.dbs.svc.cluster.local", ROHost: "primary-ro.dbs.svc.cluster.local", RHost: "primary-r.dbs.svc.cluster.local",
			},
		},
		{
			name:    "pooler sides follow readiness",
			cluster: cnpgClusterWithEndpoints(2, "primary-rw", "primary-ro"),
			request: dbtypes.ConnectionEndpointRequest{
				ProviderResourceName: "primary", Namespace: "dbs",
				PoolerEnabled: true, PoolerRWReady: true,
			},
			want: pgconninfo.Endpoints{
				RWHost: "primary-rw.dbs.svc.cluster.local", ROHost: "primary-ro.dbs.svc.cluster.local", RHost: "primary-r.dbs.svc.cluster.local",
				PoolerEnabled: true, PoolerRWHost: "primary-pooler-rw.dbs.svc.cluster.local",
			},
		},
		{
			name:    "read only pooler can become ready independently",
			cluster: cnpgClusterWithEndpoints(2, "primary-rw", "primary-ro"),
			request: dbtypes.ConnectionEndpointRequest{
				ProviderResourceName: "primary", Namespace: "dbs",
				PoolerEnabled: true, PoolerROReady: true,
			},
			want: pgconninfo.Endpoints{
				RWHost: "primary-rw.dbs.svc.cluster.local", ROHost: "primary-ro.dbs.svc.cluster.local", RHost: "primary-r.dbs.svc.cluster.local",
				PoolerEnabled: true, PoolerROHost: "primary-pooler-ro.dbs.svc.cluster.local",
			},
		},
		{
			name:    "both pooler endpoints follow ready status",
			cluster: cnpgClusterWithEndpoints(2, "primary-rw", "primary-ro"),
			request: dbtypes.ConnectionEndpointRequest{
				ProviderResourceName: "primary", Namespace: "dbs",
				PoolerEnabled: true, PoolerRWReady: true, PoolerROReady: true,
			},
			want: pgconninfo.Endpoints{
				RWHost: "primary-rw.dbs.svc.cluster.local", ROHost: "primary-ro.dbs.svc.cluster.local", RHost: "primary-r.dbs.svc.cluster.local",
				PoolerEnabled: true,
				PoolerRWHost:  "primary-pooler-rw.dbs.svc.cluster.local",
				PoolerROHost:  "primary-pooler-ro.dbs.svc.cluster.local",
			},
		},
		{
			name:    "single instance suppresses read only endpoints",
			cluster: cnpgClusterWithEndpoints(1, "primary-rw", "primary-ro"),
			request: dbtypes.ConnectionEndpointRequest{
				ProviderResourceName: "primary", Namespace: "dbs",
				PoolerEnabled: true, PoolerRWReady: true, PoolerROReady: true,
			},
			want: pgconninfo.Endpoints{
				RWHost: "primary-rw.dbs.svc.cluster.local", RHost: "primary-r.dbs.svc.cluster.local", ROUnavailable: true,
				PoolerEnabled: true, PoolerRWHost: "primary-pooler-rw.dbs.svc.cluster.local",
			},
		},
		{
			name:    "missing write service",
			cluster: cnpgClusterWithEndpoints(2, "", "primary-ro"),
			request: dbtypes.ConnectionEndpointRequest{ProviderResourceName: "primary", Namespace: "dbs"},
			wantErr: "write service name is required",
		},
		{
			name:     "provider read failure",
			request:  dbtypes.ConnectionEndpointRequest{ProviderResourceName: "missing", Namespace: "dbs"},
			wantErr:  "reading CNPG Cluster dbs/missing",
			notFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, cnpgv1.AddToScheme(scheme))
			objects := []client.Object{}
			if tt.cluster != nil {
				objects = append(objects, tt.cluster)
			}
			resolver := NewConnectionEndpointResolver(fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build())
			got, err := resolver.Resolve(t.Context(), tt.request)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				assert.Equal(t, tt.notFound, apierrors.IsNotFound(err))
				if tt.notFound {
					assert.ErrorIs(t, err, dbtypes.ErrConnectionEndpointProviderRead)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func cnpgClusterWithEndpoints(readyInstances int, writeService, readService string) *cnpgv1.Cluster {
	return &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"},
		Status: cnpgv1.ClusterStatus{
			ReadyInstances: readyInstances,
			WriteService:   writeService,
			ReadService:    readService,
		},
	}
}
