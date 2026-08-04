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

type Aggregator struct{}

func NewAggregator() Aggregator { return Aggregator{} }

func (Aggregator) Aggregate(clusterQueries []mtypes.ResolvedQuery, acceptedDBSets []mtypes.DatabaseQuerySet) mtypes.AggregatedConfig {
	cfg := mtypes.AggregatedConfig{}
	if len(clusterQueries) > 0 {
		cfg.ClusterQueries = append(cfg.ClusterQueries, clusterQueries...)
	}
	for _, set := range acceptedDBSets {
		if len(set.Queries) == 0 {
			continue
		}
		if cfg.DatabaseQueries == nil {
			cfg.DatabaseQueries = map[string][]mtypes.ResolvedQuery{}
		}
		cfg.DatabaseQueries[set.DatabaseName] = append(cfg.DatabaseQueries[set.DatabaseName], set.Queries...)
	}
	return cfg
}
