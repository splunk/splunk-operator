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
	"context"
	"fmt"

	dbcnpg "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/cnpg"
	dbtypes "github.com/splunk/splunk-operator/pkg/postgresql/database/types"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConnectionEndpointResolver reads and translates CNPG service facts without
// exposing CNPG API objects to connection-metadata policy.
type ConnectionEndpointResolver struct {
	reader client.Reader
}

// NewConnectionEndpointResolver returns a CNPG-backed endpoint resolver.
func NewConnectionEndpointResolver(reader client.Reader) *ConnectionEndpointResolver {
	return &ConnectionEndpointResolver{reader: reader}
}

func (r *ConnectionEndpointResolver) Resolve(ctx context.Context, request dbtypes.ConnectionEndpointRequest) (pgconninfo.Endpoints, error) {
	cluster, err := dbcnpg.GetCluster(ctx, r.reader, client.ObjectKey{
		Namespace: request.Namespace,
		Name:      request.ProviderResourceName,
	})
	if err != nil {
		return pgconninfo.Endpoints{}, fmt.Errorf("%w: reading CNPG Cluster %s/%s: %w", dbtypes.ErrConnectionEndpointProviderRead, request.Namespace, request.ProviderResourceName, err)
	}
	return pgcnpg.ResolveConnectionEndpoints(
		cluster.Name,
		cluster.Namespace,
		cluster.Status.WriteService,
		cluster.Status.ReadService,
		cluster.Status.ReadyInstances,
		pgcnpg.PoolerAvailability{
			Enabled: request.PoolerEnabled,
			RWReady: request.PoolerRWReady,
			ROReady: request.PoolerROReady,
		},
	)
}
