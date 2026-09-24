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

// Package custom_metrics owns provider-neutral custom-metrics policy.
package custom_metrics

import (
	"context"

	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

// DataRepository reads committed database intent and mutable query sources.
type DataRepository interface {
	ListDatabaseContributions(ctx context.Context, namespace, clusterName string) (monitoring.DatabaseContributionSnapshot, error)
	FetchConfigMap(ctx context.Context, namespace, configMapName, dataKey string) ([]byte, error)
}

type Parser interface {
	Parse(raw []byte, source monitoring.QuerySource, targetDB *string) ([]monitoring.ResolvedQuery, error)
}

// Collider applies first-wins precedence and rejects a colliding package in full.
type Collider interface {
	DetectCollisions(clusterQuerySets []monitoring.ClusterQuerySet, dbQuerySets []monitoring.DatabaseQuerySet) (acceptedCluster []monitoring.ResolvedQuery, acceptedDB []monitoring.DatabaseQuerySet, collisions []monitoring.CollisionError)
}

type Aggregator interface {
	Aggregate(clusterQueries []monitoring.ResolvedQuery, acceptedDBSets []monitoring.DatabaseQuerySet) monitoring.AggregatedConfig
}
