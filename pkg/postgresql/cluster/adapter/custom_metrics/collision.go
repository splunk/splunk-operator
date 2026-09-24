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
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

type Collider struct {
	renderIdentity mtypes.QueryIdentityRenderer
}

func NewCollider(renderer ...mtypes.QueryIdentityRenderer) Collider {
	var render mtypes.QueryIdentityRenderer
	if len(renderer) > 0 {
		render = renderer[0]
	}
	return Collider{renderIdentity: render}
}

// scopeKey avoids pointer identity in CollisionKey map lookups.
type scopeKey struct {
	metricName  string
	database    string
	clusterWide bool
}

func clusterScopeKey(metricName string) scopeKey {
	return scopeKey{metricName: metricName, clusterWide: true}
}

func databaseScopeKey(metricName, database string) scopeKey {
	return scopeKey{metricName: metricName, database: database}
}

func (k scopeKey) collisionKey() mtypes.CollisionKey {
	if k.clusterWide {
		return mtypes.CollisionKey{Kind: mtypes.CollisionLogicalName, MetricName: k.metricName}
	}
	db := k.database
	return mtypes.CollisionKey{Kind: mtypes.CollisionLogicalName, MetricName: k.metricName, TargetDatabase: &db}
}

type renderedKey struct {
	kind mtypes.CollisionKind
	name string
}

type seenQuery struct {
	source mtypes.QuerySource
	query  mtypes.ResolvedQuery
}

// First source wins; any collision rejects the later package.
func (c Collider) DetectCollisions(
	clusterQuerySets []mtypes.ClusterQuerySet,
	dbQuerySets []mtypes.DatabaseQuerySet,
) (acceptedCluster []mtypes.ResolvedQuery, acceptedDB []mtypes.DatabaseQuerySet, collisions []mtypes.CollisionError) {
	seenLogical := map[scopeKey]seenQuery{}
	seenRendered := map[renderedKey]seenQuery{}

	for _, set := range clusterQuerySets {
		setCollisions := c.collisionsForSet(set.Queries, nil, func(q mtypes.ResolvedQuery) scopeKey {
			return clusterScopeKey(q.Name)
		}, seenLogical, seenRendered)
		if len(setCollisions) > 0 {
			collisions = append(collisions, setCollisions...)
			continue
		}
		for _, q := range set.Queries {
			recordQuery(q, nil, clusterScopeKey(q.Name), c.renderIdentity, seenLogical, seenRendered)
		}
		acceptedCluster = append(acceptedCluster, set.Queries...)
	}

	for _, set := range dbQuerySets {
		db := set.DatabaseName
		setCollisions := c.collisionsForSet(set.Queries, &db, func(q mtypes.ResolvedQuery) scopeKey {
			return databaseScopeKey(q.Name, set.DatabaseName)
		}, seenLogical, seenRendered)
		if len(setCollisions) > 0 {
			collisions = append(collisions, setCollisions...)
			continue
		}
		for _, q := range set.Queries {
			recordQuery(q, &db, databaseScopeKey(q.Name, set.DatabaseName), c.renderIdentity, seenLogical, seenRendered)
		}
		acceptedDB = append(acceptedDB, set)
	}
	return acceptedCluster, acceptedDB, collisions
}

func (c Collider) collisionsForSet(
	queries []mtypes.ResolvedQuery,
	targetDatabase *string,
	keyFor func(mtypes.ResolvedQuery) scopeKey,
	seenLogical map[scopeKey]seenQuery,
	seenRendered map[renderedKey]seenQuery,
) []mtypes.CollisionError {
	var collisions []mtypes.CollisionError
	localLogical := map[scopeKey]seenQuery{}
	localRendered := map[renderedKey]seenQuery{}
	for _, q := range queries {
		logical := keyFor(q)
		logicalCollision := false
		if prior, ok := firstSeen(logical, seenLogical, localLogical); ok {
			collisions = append(collisions, collisionError(logical.collisionKey(), prior, q))
			logicalCollision = true
		}

		if !logicalCollision {
			for _, rendered := range renderedKeys(c.renderIdentity, q, targetDatabase) {
				if prior, ok := firstRendered(rendered, seenRendered, localRendered); ok {
					key := mtypes.CollisionKey{Kind: rendered.kind, RenderedName: rendered.name}
					collisions = append(collisions, collisionError(key, prior, q))
				}
			}
		}

		current := seenQuery{source: q.Source, query: q}
		localLogical[logical] = current
		for _, rendered := range renderedKeys(c.renderIdentity, q, targetDatabase) {
			localRendered[rendered] = current
		}
	}
	return collisions
}

func recordQuery(
	q mtypes.ResolvedQuery,
	targetDatabase *string,
	logical scopeKey,
	renderer mtypes.QueryIdentityRenderer,
	seenLogical map[scopeKey]seenQuery,
	seenRendered map[renderedKey]seenQuery,
) {
	current := seenQuery{source: q.Source, query: q}
	seenLogical[logical] = current
	for _, rendered := range renderedKeys(renderer, q, targetDatabase) {
		seenRendered[rendered] = current
	}
}

func renderedKeys(renderer mtypes.QueryIdentityRenderer, q mtypes.ResolvedQuery, targetDatabase *string) []renderedKey {
	if renderer == nil {
		return nil
	}
	identity := renderer(q, targetDatabase)
	keys := make([]renderedKey, 0, 1+len(identity.MetricFamilies))
	if identity.QueryKey != "" {
		keys = append(keys, renderedKey{kind: mtypes.CollisionQueryKey, name: identity.QueryKey})
	}
	for _, family := range identity.MetricFamilies {
		if family != "" {
			keys = append(keys, renderedKey{kind: mtypes.CollisionMetricFamily, name: family})
		}
	}
	return keys
}

func firstSeen(key scopeKey, global, local map[scopeKey]seenQuery) (seenQuery, bool) {
	if prior, ok := global[key]; ok {
		return prior, true
	}
	prior, ok := local[key]
	return prior, ok
}

func firstRendered(key renderedKey, global, local map[renderedKey]seenQuery) (seenQuery, bool) {
	if prior, ok := global[key]; ok {
		return prior, true
	}
	prior, ok := local[key]
	return prior, ok
}

func collisionError(key mtypes.CollisionKey, prior seenQuery, current mtypes.ResolvedQuery) mtypes.CollisionError {
	return mtypes.CollisionError{
		Key:               key,
		First:             prior.source,
		Second:            current.Source,
		FirstMetricName:   prior.query.Name,
		FirstValueColumn:  prior.query.Value,
		SecondMetricName:  current.Name,
		SecondValueColumn: current.Value,
	}
}
