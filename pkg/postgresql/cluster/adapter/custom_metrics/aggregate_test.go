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

package custom_metrics

import (
	"testing"
	"time"

	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAggregate_ClusterAndDatabases(t *testing.T) {
	cluster := []mtypes.ResolvedQuery{clusterQ("pg_cluster", "alpha")}
	sets := []mtypes.DatabaseQuerySet{
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(100, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_a", "beta", "db-a")}},
		{DatabaseName: "db-b", CreationTimestamp: time.Unix(200, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_b", "gamma", "db-b")}},
	}
	cfg := NewAggregator().Aggregate(cluster, sets)
	require.Len(t, cfg.ClusterQueries, 1)
	assert.Equal(t, "pg_cluster", cfg.ClusterQueries[0].Name)
	require.Len(t, cfg.DatabaseQueries, 2)
	assert.Equal(t, "pg_a", cfg.DatabaseQueries["db-a"][0].Name)
	assert.Equal(t, "pg_b", cfg.DatabaseQueries["db-b"][0].Name)
}

func TestAggregate_Empty(t *testing.T) {
	cfg := NewAggregator().Aggregate(nil, nil)
	assert.Nil(t, cfg.ClusterQueries)
	assert.Nil(t, cfg.DatabaseQueries)
}

func TestAggregate_SkipsEmptyDatabaseSets(t *testing.T) {
	sets := []mtypes.DatabaseQuerySet{
		{DatabaseName: "db-empty", CreationTimestamp: time.Unix(100, 0)},
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(200, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_a", "beta", "db-a")}},
	}
	cfg := NewAggregator().Aggregate(nil, sets)
	require.Len(t, cfg.DatabaseQueries, 1)
	_, hasEmpty := cfg.DatabaseQueries["db-empty"]
	assert.False(t, hasEmpty)
}
