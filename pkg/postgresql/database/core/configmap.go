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
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	ConfigMapKeyDatabaseName = "DATABASE_NAME"
	ConfigMapKeyAdminUser    = "ADMIN_USER_NAME"
	ConfigMapKeyRWUser       = "RW_USER_NAME"
)

func withDatabaseIdentity(dbName string) pgconninfo.Option {
	return withDatabaseIdentityAndRoles(dbName, ports.DatabaseRoleNames{
		Admin: adminRoleName(dbName),
		RW:    rwRoleName(dbName),
	})
}

func withDatabaseIdentityAndRoles(dbName string, roles ports.DatabaseRoleNames) pgconninfo.Option {
	return func(builder *pgconninfo.Builder) {
		builder.SetRequired(ConfigMapKeyDatabaseName, dbName)
		builder.SetRequired(ConfigMapKeyAdminUser, roles.Admin)
		builder.SetRequired(ConfigMapKeyRWUser, roles.RW)
	}
}

func buildDatabaseConfigMapData(dbName string, endpoints clusterEndpoints) (map[string]string, []string, error) {
	return pgconninfo.BuildConfigMapData(endpoints, withDatabaseIdentity(dbName))
}

func buildDatabaseConfigMapDataForDatabase(dbSpec platformv1alpha1.DatabaseDefinition, endpoints clusterEndpoints) (map[string]string, []string, error) {
	return pgconninfo.BuildConfigMapData(endpoints, withDatabaseIdentityAndRoles(dbSpec.Name, EffectiveRoleNames(dbSpec)))
}

// resolveClusterEndpoints derives the database access endpoints from the cluster
// status, mapping the pooler reconciliation gates onto PoolerAvailability.
func resolveClusterEndpoints(cluster *platformv1alpha1.PostgresCluster, cnpgCluster *cnpgv1.Cluster, namespace string) (clusterEndpoints, error) {
	var pooler pgcnpg.PoolerAvailability
	if poolerStatus := cluster.Status.ConnectionPoolerStatus; poolerStatus != nil && poolerStatus.Enabled {
		pooler = pgcnpg.PoolerAvailability{
			Enabled: true,
			RWReady: poolerStatus.ReadWriteEnabled,
			ROReady: poolerStatus.ReadOnlyEnabled,
		}
	}
	return pgcnpg.ResolveConnectionEndpoints(
		cnpgCluster.Name,
		namespace,
		cnpgCluster.Status.WriteService,
		cnpgCluster.Status.ReadService,
		cnpgCluster.Status.ReadyInstances,
		pooler,
	)
}
