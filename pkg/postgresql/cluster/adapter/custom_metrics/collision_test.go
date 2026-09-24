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
	"k8s.io/utils/ptr"
)

func src(name string) mtypes.QuerySource {
	return mtypes.QuerySource{ConfigMapName: name, ConfigMapKey: "queries.yaml", Namespace: "demo"}
}

func clusterQ(metric, source string) mtypes.ResolvedQuery {
	return mtypes.ResolvedQuery{PlatformQuery: mtypes.PlatformQuery{Name: metric}, Source: src(source)}
}

func dbQ(metric, source, db string) mtypes.ResolvedQuery {
	return mtypes.ResolvedQuery{PlatformQuery: mtypes.PlatformQuery{Name: metric}, Source: src(source), TargetDatabase: ptr.To(db)}
}

func clusterSet(queries ...mtypes.ResolvedQuery) mtypes.ClusterQuerySet {
	return mtypes.ClusterQuerySet{Queries: queries}
}

func TestDetectCollisions_ClusterVsCluster(t *testing.T) {
	cluster := []mtypes.ClusterQuerySet{
		clusterSet(clusterQ("pg_slow", "alpha")),
		clusterSet(clusterQ("pg_slow", "beta")),
	}
	acceptedCluster, acceptedDB, collisions := NewCollider().DetectCollisions(cluster, nil)
	require.Len(t, acceptedCluster, 1)
	assert.Equal(t, "alpha", acceptedCluster[0].Source.ConfigMapName)
	assert.Empty(t, acceptedDB)
	require.Len(t, collisions, 1)
	assert.Nil(t, collisions[0].Key.TargetDatabase)
	assert.Equal(t, "alpha", collisions[0].First.ConfigMapName)
	assert.Equal(t, "beta", collisions[0].Second.ConfigMapName)
}

func TestDetectCollisions_SameDatabase(t *testing.T) {
	sets := []mtypes.DatabaseQuerySet{
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(100, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_slow", "alpha", "db-a")}},
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(200, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_slow", "beta", "db-a")}},
	}
	acceptedCluster, acceptedDB, collisions := NewCollider().DetectCollisions(nil, sets)
	assert.Empty(t, acceptedCluster)
	require.Len(t, acceptedDB, 1)
	assert.Equal(t, "alpha", acceptedDB[0].Queries[0].Source.ConfigMapName)
	require.Len(t, collisions, 1)
	require.NotNil(t, collisions[0].Key.TargetDatabase)
	assert.Equal(t, "db-a", *collisions[0].Key.TargetDatabase)
}

func TestDetectCollisions_ClusterAndDatabaseSameNameOK(t *testing.T) {
	cluster := []mtypes.ClusterQuerySet{clusterSet(clusterQ("pg_slow", "alpha"))}
	sets := []mtypes.DatabaseQuerySet{
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(100, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_slow", "beta", "db-a")}},
	}
	acceptedCluster, acceptedDB, collisions := NewCollider().DetectCollisions(cluster, sets)
	assert.Len(t, acceptedCluster, 1)
	assert.Len(t, acceptedDB, 1)
	assert.Empty(t, collisions, "cluster-wide and db-scoped share a name but produce distinct series")
}

func TestDetectCollisions_ClusterLosingPackageDroppedWhole(t *testing.T) {
	sets := []mtypes.ClusterQuerySet{
		clusterSet(clusterQ("pg_slow", "alpha")),
		clusterSet(
			clusterQ("pg_slow", "beta"),
			clusterQ("pg_unique", "beta"),
		),
	}
	acceptedCluster, acceptedDB, collisions := NewCollider().DetectCollisions(sets, nil)
	assert.Empty(t, acceptedDB)
	require.Len(t, acceptedCluster, 1)
	assert.Equal(t, "alpha", acceptedCluster[0].Source.ConfigMapName)
	require.Len(t, collisions, 1)
	assert.Equal(t, "beta", collisions[0].Second.ConfigMapName)
}

func TestDetectCollisions_ReportsEveryConflictInRejectedPackage(t *testing.T) {
	sets := []mtypes.ClusterQuerySet{
		clusterSet(clusterQ("pg_slow", "alpha"), clusterQ("pg_busy", "alpha")),
		clusterSet(clusterQ("pg_slow", "beta"), clusterQ("pg_busy", "beta")),
	}
	acceptedCluster, _, collisions := NewCollider().DetectCollisions(sets, nil)
	require.Len(t, acceptedCluster, 2)
	require.Len(t, collisions, 2)
	assert.Equal(t, "pg_slow", collisions[0].Key.MetricName)
	assert.Equal(t, "pg_busy", collisions[1].Key.MetricName)
}

func TestDetectCollisions_DifferentDatabasesSameNameOK(t *testing.T) {
	sets := []mtypes.DatabaseQuerySet{
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(100, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_slow", "alpha", "db-a")}},
		{DatabaseName: "db-b", CreationTimestamp: time.Unix(200, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_slow", "beta", "db-b")}},
	}
	acceptedCluster, acceptedDB, collisions := NewCollider().DetectCollisions(nil, sets)
	assert.Empty(t, acceptedCluster)
	assert.Len(t, acceptedDB, 2)
	assert.Empty(t, collisions)
}

func TestDetectCollisions_LosingSetDroppedWhole(t *testing.T) {
	sets := []mtypes.DatabaseQuerySet{
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(100, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_slow", "alpha", "db-a")}},
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(200, 0), Queries: []mtypes.ResolvedQuery{
			dbQ("pg_slow", "beta", "db-a"),
			dbQ("pg_unique", "beta", "db-a"),
		}},
	}
	acceptedCluster, acceptedDB, collisions := NewCollider().DetectCollisions(nil, sets)
	assert.Empty(t, acceptedCluster)
	require.Len(t, acceptedDB, 1)
	assert.Len(t, acceptedDB[0].Queries, 1)
	require.Len(t, collisions, 1)
}

func TestDetectCollisions_TrustsCallerOrdering(t *testing.T) {
	sets := []mtypes.DatabaseQuerySet{
		{DatabaseName: "db-a", CreationTimestamp: time.Unix(100, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_x", "alpha", "db-a")}},
		{DatabaseName: "db-b", CreationTimestamp: time.Unix(100, 0), Queries: []mtypes.ResolvedQuery{dbQ("pg_x", "beta", "db-b")}},
	}
	acceptedCluster, acceptedDB, collisions := NewCollider().DetectCollisions(nil, sets)
	assert.Empty(t, acceptedCluster)
	assert.Len(t, acceptedDB, 2, "different databases never collide regardless of order")
	assert.Empty(t, collisions)
}

func TestDetectCollisions_RenderedFamilyWithinPackageRejectsWholePackage(t *testing.T) {
	first := clusterQ("foo_bar", "alpha")
	first.Value = "baz"
	second := clusterQ("foo", "alpha")
	second.Value = "bar_baz"
	render := func(q mtypes.ResolvedQuery, _ *string) mtypes.RenderedQueryIdentity {
		return mtypes.RenderedQueryIdentity{
			QueryKey:       "managed:" + q.Name,
			MetricFamilies: []string{"cnpg_" + q.Name + "_" + q.Value},
		}
	}

	acceptedCluster, acceptedDB, collisions := NewCollider(render).DetectCollisions(
		[]mtypes.ClusterQuerySet{clusterSet(first, second)},
		nil,
	)

	assert.Empty(t, acceptedCluster)
	assert.Empty(t, acceptedDB)
	require.Len(t, collisions, 1)
	assert.Equal(t, mtypes.CollisionMetricFamily, collisions[0].Key.Kind)
	assert.Equal(t, "cnpg_foo_bar_baz", collisions[0].Key.RenderedName)
	assert.Contains(t, collisions[0].Error(), `metric "foo_bar" value "baz"`)
	assert.Contains(t, collisions[0].Error(), `metric "foo" value "bar_baz"`)
}

func TestDetectCollisions_RenderedFamilyRejectsLaterPackage(t *testing.T) {
	first := clusterQ("foo_bar", "alpha")
	first.Value = "baz"
	second := clusterQ("foo", "beta")
	second.Value = "bar_baz"
	render := func(q mtypes.ResolvedQuery, _ *string) mtypes.RenderedQueryIdentity {
		return mtypes.RenderedQueryIdentity{
			QueryKey:       "managed:" + q.Name,
			MetricFamilies: []string{"cnpg_" + q.Name + "_" + q.Value},
		}
	}

	acceptedCluster, _, collisions := NewCollider(render).DetectCollisions(
		[]mtypes.ClusterQuerySet{clusterSet(first), clusterSet(second)},
		nil,
	)

	require.Len(t, acceptedCluster, 1)
	assert.Equal(t, "foo_bar", acceptedCluster[0].Name)
	require.Len(t, collisions, 1)
	assert.Equal(t, "beta", collisions[0].Second.ConfigMapName)
}
