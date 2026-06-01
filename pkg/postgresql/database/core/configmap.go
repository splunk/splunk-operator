package core

import (
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	ConfigMapKeyDatabaseName = "DATABASE_NAME"
	ConfigMapKeyAdminUser    = "ADMIN_USER_NAME"
	ConfigMapKeyRWUser       = "RW_USER_NAME"
)

func withDatabaseIdentity(dbName string) pgconninfo.Option {
	return func(builder *pgconninfo.Builder) {
		builder.SetRequired(ConfigMapKeyDatabaseName, dbName)
		builder.SetRequired(ConfigMapKeyAdminUser, adminRoleName(dbName))
		builder.SetRequired(ConfigMapKeyRWUser, rwRoleName(dbName))
	}
}

func buildDatabaseConfigMapData(dbName string, endpoints clusterEndpoints) (map[string]string, []string, error) {
	return pgconninfo.BuildConfigMapData(endpoints, withDatabaseIdentity(dbName))
}

func resolveClusterEndpoints(cluster *enterprisev4.PostgresCluster, cnpgCluster *cnpgv1.Cluster, namespace string) (clusterEndpoints, error) {
	return pgcnpg.ResolveConnectionEndpoints(
		cnpgCluster.Name,
		namespace,
		cnpgCluster.Status.WriteService,
		cnpgCluster.Status.ReadService,
		cluster.Status.ConnectionPoolerStatus != nil && cluster.Status.ConnectionPoolerStatus.Enabled,
	)
}
