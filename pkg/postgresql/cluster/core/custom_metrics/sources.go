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
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

type sourceRef struct {
	configMapName string
	dataKey       string
	namespace     string
	targetDB      *string
	creationTime  time.Time
	contributor   *mtypes.ContributorIdentity
}

func (r sourceRef) key() string {
	return fmt.Sprintf("%s/%s/%s", r.namespace, r.configMapName, r.dataKey)
}

func (r sourceRef) querySource() mtypes.QuerySource {
	return mtypes.QuerySource{
		ConfigMapName: r.configMapName,
		ConfigMapKey:  r.dataKey,
		Namespace:     r.namespace,
		Contributor:   r.contributor,
	}
}

// ok=false distinguishes watch-recoverable input failure from retryable errors.
func (r *run) resolveSource(ctx context.Context, ref sourceRef) ([]mtypes.ResolvedQuery, bool, error) {
	key := ref.key()
	source := ref.querySource()

	raw, err := r.m.sources.FetchConfigMap(ctx, ref.namespace, ref.configMapName, ref.dataKey)
	if err != nil {
		if !errors.Is(err, mtypes.ErrSourceNotFound) {
			return nil, false, err
		}
		r.markInvalid(InvalidConfigMapNotFound, key)
		r.markContributorInvalid(ref.contributor, "CustomMetricsConfigMapNotFound", key)
		r.events = append(r.events, Event{
			Kind:    EventConfigMapNotFound,
			Message: fmt.Sprintf("custom-metrics source %s not found; previous complete configuration remains active", key),
		})
		return nil, false, nil
	}

	queries, perr := r.m.parser.Parse(raw, source, ref.targetDB)
	if perr != nil {
		detail := strings.TrimPrefix(perr.Error(), mtypes.ErrInvalidQueryDefinition.Error()+": ")
		r.markInvalid(InvalidQuery, detail)
		r.markContributorInvalid(ref.contributor, "InvalidQueryDefinition", detail)
		r.events = append(r.events, Event{
			Kind:    EventInvalidQuery,
			Message: fmt.Sprintf("invalid custom-metrics query definition: %s; previous complete configuration remains active", joinDiagnostics([]string{detail})),
		})
		return nil, false, nil
	}
	return queries, true, nil
}

// Cluster packages precede committed database packages.
func (m *Model) gatherSourceRefs(ctx context.Context, cluster *enterprisev4.PostgresCluster) ([]sourceRef, mtypes.DatabaseContributionSnapshot, error) {
	var refs []sourceRef

	if cluster.Spec.Monitoring != nil {
		for _, q := range cluster.Spec.Monitoring.CustomQueriesConfigMap {
			refs = append(refs, sourceRef{
				configMapName: q.Name,
				dataKey:       q.Key,
				namespace:     cluster.Namespace,
				creationTime:  cluster.CreationTimestamp.Time,
			})
		}
	}

	snapshot, err := m.sources.ListDatabaseContributions(ctx, cluster.Namespace, cluster.Name)
	if err != nil {
		return nil, mtypes.DatabaseContributionSnapshot{}, err
	}
	for i := range snapshot.Contributions {
		contribution := &snapshot.Contributions[i]
		if !contribution.Exists {
			continue
		}
		dbName := contribution.Identity.DatabaseName
		identity := contribution.Identity
		for _, selector := range contribution.Selectors {
			refs = append(refs, sourceRef{
				configMapName: selector.ConfigMapName,
				dataKey:       selector.ConfigMapKey,
				namespace:     contribution.Identity.Namespace,
				targetDB:      &dbName,
				creationTime:  contribution.CreationTimestamp,
				contributor:   &identity,
			})
		}
	}
	return refs, snapshot, nil
}

// Older resources win; contributor identity breaks timestamp ties. Stable sort
// preserves selector declaration order within one contribution.
func sortDatabaseQuerySets(sets []mtypes.DatabaseQuerySet) {
	sort.SliceStable(sets, func(i, j int) bool {
		a, b := sets[i], sets[j]
		if !a.CreationTimestamp.Equal(b.CreationTimestamp) {
			return a.CreationTimestamp.Before(b.CreationTimestamp)
		}
		if a.DatabaseName != b.DatabaseName {
			return a.DatabaseName < b.DatabaseName
		}
		an, auid := contributorSortIdentity(a.Contributor)
		bn, buid := contributorSortIdentity(b.Contributor)
		if an != bn {
			return an < bn
		}
		return auid < buid
	})
}

func contributorSortIdentity(identity *mtypes.ContributorIdentity) (string, string) {
	if identity == nil {
		return "", ""
	}
	return identity.PostgresDatabaseName, identity.PostgresDatabaseUID
}
