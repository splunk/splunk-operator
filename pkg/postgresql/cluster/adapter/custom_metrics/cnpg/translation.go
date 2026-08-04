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
	"fmt"
	"strings"

	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

const managedQueryPrefix = "splunk_operator_"

func RenderIdentity(q monitoring.ResolvedQuery, targetDB *string) monitoring.RenderedQueryIdentity {
	scope := "cluster:"
	publicName := publicQueryName(q.Name, targetDB)
	if targetDB != nil {
		scope = "database:" + *targetDB + ":"
	}
	return monitoring.RenderedQueryIdentity{
		QueryKey:       managedQueryPrefix + scope + q.Name,
		MetricFamilies: []string{"cnpg_" + publicName + "_" + q.Value},
	}
}

func toEntries(cfg monitoring.AggregatedConfig) (map[string]cnpginfra.MetricEntry, error) {
	entries := make(map[string]cnpginfra.MetricEntry, len(cfg.ClusterQueries)+len(cfg.DatabaseQueries))
	add := func(key string, entry cnpginfra.MetricEntry) error {
		if _, exists := entries[key]; exists {
			return fmt.Errorf("CNPG custom-query key %q is produced by more than one metric scope", key)
		}
		entries[key] = entry
		return nil
	}
	for i := range cfg.ClusterQueries {
		q := cfg.ClusterQueries[i]
		identity := RenderIdentity(q, nil)
		if err := add(identity.QueryKey, toEntry(q, nil)); err != nil {
			return nil, err
		}
	}
	for dbName, queries := range cfg.DatabaseQueries {
		db := dbName
		for i := range queries {
			q := queries[i]
			identity := RenderIdentity(q, &db)
			if err := add(identity.QueryKey, toEntry(q, &db)); err != nil {
				return nil, err
			}
		}
	}
	return entries, nil
}

func toEntry(q monitoring.ResolvedQuery, targetDB *string) cnpginfra.MetricEntry {
	metrics := []map[string]cnpginfra.MetricSpec{
		{q.Value: {Usage: strings.ToUpper(string(q.Type)), Description: q.Help}},
	}
	for _, label := range q.Labels {
		metrics = append(metrics, map[string]cnpginfra.MetricSpec{label: {Usage: "LABEL"}})
	}
	var targetDBs []string
	if targetDB != nil {
		targetDBs = []string{*targetDB}
	}
	return cnpginfra.MetricEntry{
		Name:            publicQueryName(q.Name, targetDB),
		Query:           projectedQuery(q),
		Metrics:         metrics,
		TargetDatabases: targetDBs,
	}
}

func publicQueryName(queryName string, targetDB *string) string {
	if targetDB == nil {
		return managedQueryPrefix + "cluster_" + queryName
	}
	return managedQueryPrefix + "database_" + *targetDB + "_" + queryName
}

func projectedQuery(q monitoring.ResolvedQuery) string {
	columns := make([]string, 0, 1+len(q.Labels))
	columns = append(columns, quoteIdentifier(q.Value))
	for _, label := range q.Labels {
		columns = append(columns, quoteIdentifier(label))
	}
	sql := strings.TrimSpace(q.SQL)
	return fmt.Sprintf(
		"SELECT %s\nFROM (\n%s\n) AS splunk_operator_custom_metrics",
		strings.Join(columns, ", "),
		sql,
	)
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
