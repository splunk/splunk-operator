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

package monitoring

import (
	"fmt"
	"time"
)

// QuerySource preserves provenance for diagnostics and collision precedence.
type QuerySource struct {
	ConfigMapName string
	ConfigMapKey  string
	Namespace     string
	Contributor   *ContributorIdentity
}

type ResolvedQuery struct {
	PlatformQuery
	Source QuerySource
	// TargetDatabase is nil for cluster-wide queries.
	TargetDatabase *string
}

// ClusterQuerySet is one atomic cluster-scoped source package.
type ClusterQuerySet struct {
	Queries []ResolvedQuery
}

type AggregatedConfig struct {
	ClusterQueries  []ResolvedQuery
	DatabaseQueries map[string][]ResolvedQuery
}

type RenderedQueryIdentity struct {
	QueryKey       string
	MetricFamilies []string
}

type QueryIdentityRenderer func(query ResolvedQuery, targetDatabase *string) RenderedQueryIdentity

type CollisionKind string

const (
	CollisionLogicalName  CollisionKind = "LogicalName"
	CollisionQueryKey     CollisionKind = "QueryKey"
	CollisionMetricFamily CollisionKind = "MetricFamily"
)

// CollisionKey identifies either a logical metric scope or a rendered provider identity.
type CollisionKey struct {
	Kind           CollisionKind
	MetricName     string
	TargetDatabase *string
	RenderedName   string
}

type CollisionError struct {
	Key CollisionKey
	// First wins according to caller ordering.
	First             QuerySource
	Second            QuerySource
	FirstMetricName   string
	FirstValueColumn  string
	SecondMetricName  string
	SecondValueColumn string
}

func (e *CollisionError) Error() string {
	if e.Key.RenderedName != "" {
		return fmt.Sprintf(
			"rendered %s collision for %q: metric %q value %q in %s/%s:%s conflicts with metric %q value %q in %s/%s:%s",
			e.Key.Kind, e.Key.RenderedName,
			e.FirstMetricName, e.FirstValueColumn,
			e.First.Namespace, e.First.ConfigMapName, e.First.ConfigMapKey,
			e.SecondMetricName, e.SecondValueColumn,
			e.Second.Namespace, e.Second.ConfigMapName, e.Second.ConfigMapKey,
		)
	}
	scope := "cluster-wide"
	if e.Key.TargetDatabase != nil {
		scope = "database " + *e.Key.TargetDatabase
	}
	return fmt.Sprintf(
		"metric name collision for %q (%s): defined in %s/%s:%s and %s/%s:%s",
		e.Key.MetricName, scope,
		e.First.Namespace, e.First.ConfigMapName, e.First.ConfigMapKey,
		e.Second.Namespace, e.Second.ConfigMapName, e.Second.ConfigMapKey,
	)
}

// DatabaseQuerySet is one atomic database-scoped source package.
type DatabaseQuerySet struct {
	DatabaseName string
	// CreationTimestamp controls deterministic first-wins ordering.
	CreationTimestamp time.Time
	Contributor       *ContributorIdentity
	Queries           []ResolvedQuery
}
