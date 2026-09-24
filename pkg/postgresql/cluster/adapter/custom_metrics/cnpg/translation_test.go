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
package cnpgmonitoring

import (
	"testing"

	custommetrics "github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/custom_metrics"
	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToEntries_WireFormat(t *testing.T) {
	cfg := monitoring.AggregatedConfig{
		ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("pg_conns")},
	}
	entries, err := toEntries(cfg)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	e := entries["splunk_operator_cluster:pg_conns"]
	assert.Equal(t, "splunk_operator_cluster_pg_conns", e.Name)
	assert.Equal(t, `SELECT "value", "state"
FROM (
SELECT count(*) AS value FROM pg_stat_activity
) AS splunk_operator_custom_metrics`, e.Query)
	assert.Empty(t, e.TargetDatabases)
	require.Len(t, e.Metrics, 2)
	assert.Equal(t, cnpginfra.MetricSpec{Usage: "GAUGE", Description: "help for pg_conns"}, e.Metrics[0]["value"])
	assert.Equal(t, cnpginfra.MetricSpec{Usage: "LABEL"}, e.Metrics[1]["state"])
}

func TestToEntries_DatabaseScopedAddsTargetDatabases(t *testing.T) {
	dbq := gaugeQuery("pg_scoped")
	dbq.TargetDatabase = func() *string { s := "team_a_db"; return &s }()
	cfg := monitoring.AggregatedConfig{
		DatabaseQueries: map[string][]monitoring.ResolvedQuery{"team_a_db": {dbq}},
	}
	entries, err := toEntries(cfg)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	e := entries["splunk_operator_database:team_a_db:pg_scoped"]
	assert.Equal(t, "splunk_operator_database_team_a_db_pg_scoped", e.Name)
	assert.Equal(t, []string{"team_a_db"}, e.TargetDatabases)
}

func TestToEntries_QualifiesCrossScopeKeys(t *testing.T) {
	cluster := gaugeQuery("orders:waiting")
	database := gaugeQuery("waiting")
	database.TargetDatabase = func() *string { s := "orders"; return &s }()

	entries, err := toEntries(monitoring.AggregatedConfig{
		ClusterQueries:  []monitoring.ResolvedQuery{cluster},
		DatabaseQueries: map[string][]monitoring.ResolvedQuery{"orders": {database}},
	})

	require.NoError(t, err)
	assert.Contains(t, entries, "splunk_operator_cluster:orders:waiting")
	assert.Contains(t, entries, "splunk_operator_database:orders:waiting")
}

func TestRenderIdentity_UsesManagedPublicMetricFamily(t *testing.T) {
	query := gaugeQuery("foo_bar")
	query.Value = "baz"

	cluster := RenderIdentity(query, nil)
	assert.Equal(t, "splunk_operator_cluster:foo_bar", cluster.QueryKey)
	assert.Equal(t, []string{"cnpg_splunk_operator_cluster_foo_bar_baz"}, cluster.MetricFamilies)

	databaseName := "orders"
	database := RenderIdentity(query, &databaseName)
	assert.Equal(t, "splunk_operator_database:orders:foo_bar", database.QueryKey)
	assert.Equal(t, []string{"cnpg_splunk_operator_database_orders_foo_bar_baz"}, database.MetricFamilies)
}

func TestRenderIdentity_IsolatesUserQueriesFromCNPGBuiltIns(t *testing.T) {
	for _, name := range []string{"backends", "pg_database", "pg_stat_database"} {
		query := gaugeQuery(name)

		identity := RenderIdentity(query, nil)

		require.Len(t, identity.MetricFamilies, 1)
		assert.Equal(t, "cnpg_splunk_operator_cluster_"+name+"_value", identity.MetricFamilies[0])
		assert.NotEqual(t, "cnpg_"+name+"_value", identity.MetricFamilies[0])
	}
}

func TestToEntry_ProjectsLabelsInDeclaredOrder(t *testing.T) {
	query := gaugeQuery("waiting_orders")
	query.SQL = "SELECT status, region, count(*) AS value FROM orders GROUP BY status, region"
	query.Labels = []string{"region", "status"}

	entry := toEntry(query, nil)

	assert.Equal(t, `SELECT "value", "region", "status"
FROM (
SELECT status, region, count(*) AS value FROM orders GROUP BY status, region
) AS splunk_operator_custom_metrics`, entry.Query)
	require.Len(t, entry.Metrics, 3)
	assert.Contains(t, entry.Metrics[1], "region")
	assert.Contains(t, entry.Metrics[2], "status")
}

func TestToEntries_ProjectsNormalizedTerminalSemicolonBeforeComment(t *testing.T) {
	queries, err := custommetrics.NewParser().Parse([]byte(`
commented:
  type: gauge
  help: h
  query: SELECT count(*) AS value FROM t; -- why
  value: value
`), monitoring.QuerySource{Namespace: "ns", ConfigMapName: "metrics", ConfigMapKey: "queries.yaml"}, nil)
	require.NoError(t, err)

	entries, err := toEntries(monitoring.AggregatedConfig{ClusterQueries: queries})
	require.NoError(t, err)

	assert.Equal(t, `SELECT "value"
FROM (
SELECT count(*) AS value FROM t -- why
) AS splunk_operator_custom_metrics`, entries["splunk_operator_cluster:commented"].Query)
}

func TestToEntries_Deterministic(t *testing.T) {
	cfg := monitoring.AggregatedConfig{
		ClusterQueries: []monitoring.ResolvedQuery{gaugeQuery("b_metric"), gaugeQuery("a_metric"), gaugeQuery("c_metric")},
	}
	first, err := toEntries(cfg)
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		again, err := toEntries(cfg)
		require.NoError(t, err)
		assert.Equal(t, first, again)
	}
}

func TestToEntries_EmptyConfigYieldsEmptyMap(t *testing.T) {
	entries, err := toEntries(monitoring.AggregatedConfig{})
	require.NoError(t, err)
	assert.Empty(t, entries)
}
