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

// Package custom_metrics parses, validates, collides, and aggregates query packages.
package custom_metrics

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"sigs.k8s.io/yaml"
)

type Parser struct{}

var prometheusIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func NewParser() Parser { return Parser{} }

type rawQuery struct {
	Type   mtypes.MetricType `json:"type"`
	Help   string            `json:"help"`
	Query  string            `json:"query"`
	Value  string            `json:"value"`
	Labels []string          `json:"labels,omitempty"`
}

// Parse reports every validation issue with source context.
func (Parser) Parse(raw []byte, source mtypes.QuerySource, targetDB *string) ([]mtypes.ResolvedQuery, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, fmt.Errorf("%w: %s/%s/%s: query package is empty",
			mtypes.ErrInvalidQueryDefinition, source.Namespace, source.ConfigMapName, source.ConfigMapKey)
	}
	entries := map[string]rawQuery{}
	if err := yaml.UnmarshalStrict(raw, &entries); err != nil {
		return nil, fmt.Errorf("%w: %s/%s/%s: %v",
			mtypes.ErrInvalidQueryDefinition, source.Namespace, source.ConfigMapName, source.ConfigMapKey, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s/%s/%s: query package defines no queries",
			mtypes.ErrInvalidQueryDefinition, source.Namespace, source.ConfigMapName, source.ConfigMapKey)
	}

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	resolved := make([]mtypes.ResolvedQuery, 0, len(entries))
	var validationIssues []string
	for _, name := range names {
		entry := entries[name]
		issues := validateRawQuery(name, entry)
		normalizedSQL, sqlErr := normalizeSingleSQLStatement(entry.Query)
		if sqlErr != nil {
			issues = append(issues, fmt.Sprintf("metric %q query %s", name, sqlErr))
		}
		if len(issues) > 0 {
			validationIssues = append(validationIssues, issues...)
			continue
		}
		resolved = append(resolved, mtypes.ResolvedQuery{
			PlatformQuery: mtypes.PlatformQuery{
				Name:   name,
				Type:   entry.Type,
				Help:   entry.Help,
				SQL:    normalizedSQL,
				Value:  entry.Value,
				Labels: entry.Labels,
			},
			Source:         source,
			TargetDatabase: targetDB,
		})
	}
	if len(validationIssues) > 0 {
		return nil, fmt.Errorf("%w: %s/%s/%s: %s",
			mtypes.ErrInvalidQueryDefinition,
			source.Namespace,
			source.ConfigMapName,
			source.ConfigMapKey,
			strings.Join(validationIssues, "; "),
		)
	}
	return resolved, nil
}

func validateRawQuery(name string, q rawQuery) []string {
	var issues []string
	required := []struct {
		field string
		value string
	}{
		{field: "metric name", value: name},
		{field: "type", value: string(q.Type)},
		{field: "help", value: q.Help},
		{field: "query", value: q.Query},
		{field: "value", value: q.Value},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			issues = append(issues, fmt.Sprintf("metric %q missing required field %q", name, field.field))
		}
	}
	if name != "" && strings.Contains(name, ":") {
		issues = append(issues, fmt.Sprintf("metric name %q contains reserved character ':'", name))
	} else if name != "" && !prometheusIdentifier.MatchString(name) {
		issues = append(issues, fmt.Sprintf(
			"metric %q has invalid metric name %q: must match [a-zA-Z_][a-zA-Z0-9_]*",
			name, name,
		))
	}
	if q.Value != "" && !prometheusIdentifier.MatchString(q.Value) {
		issues = append(issues, fmt.Sprintf(
			"metric %q has invalid value column %q: must match [a-zA-Z_][a-zA-Z0-9_]*",
			name, q.Value,
		))
	}
	labels := make(map[string]struct{}, len(q.Labels))
	for i, label := range q.Labels {
		if strings.TrimSpace(label) == "" {
			issues = append(issues, fmt.Sprintf("metric %q contains an empty label at index %d", name, i))
			continue
		}
		if q.Value != "" && label == q.Value {
			issues = append(issues, fmt.Sprintf("metric %q uses value column %q as a label at index %d", name, label, i))
		}
		if !prometheusIdentifier.MatchString(label) {
			issues = append(issues, fmt.Sprintf(
				"metric %q has invalid label %q at index %d: must match [a-zA-Z_][a-zA-Z0-9_]*",
				name, label, i,
			))
		} else if strings.HasPrefix(label, "__") {
			issues = append(issues, fmt.Sprintf(
				"metric %q uses reserved label %q at index %d: labels beginning with '__' are not allowed",
				name, label, i,
			))
		}
		if _, exists := labels[label]; exists {
			issues = append(issues, fmt.Sprintf("metric %q contains duplicate label %q at index %d", name, label, i))
		}
		labels[label] = struct{}{}
	}
	switch q.Type {
	case mtypes.MetricTypeGauge, mtypes.MetricTypeCounter:
	case "":
	default:
		issues = append(issues, fmt.Sprintf("metric %q has unsupported type %q (want gauge or counter)", name, q.Type))
	}
	return issues
}
