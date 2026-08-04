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
	"errors"
	"strings"
	"testing"

	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func testSource() mtypes.QuerySource {
	return mtypes.QuerySource{ConfigMapName: "alpha", ConfigMapKey: "queries.yaml", Namespace: "demo"}
}

func TestParse_ValidQueries(t *testing.T) {
	raw := []byte(`
pg_active_conns_by_state:
  type: gauge
  help: "Active connections by wait event state"
  query: "SELECT count(*) AS value, state FROM pg_stat_activity GROUP BY state"
  value: value
  labels:
    - state
pg_txn_total:
  type: counter
  help: "Total transactions"
  query: "SELECT xact_commit AS value FROM pg_stat_database"
  value: value
`)
	got, err := NewParser().Parse(raw, testSource(), nil)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "pg_active_conns_by_state", got[0].Name)
	assert.Equal(t, mtypes.MetricTypeGauge, got[0].Type)
	assert.Equal(t, []string{"state"}, got[0].Labels)
	assert.Equal(t, testSource(), got[0].Source)
	assert.Nil(t, got[0].TargetDatabase)

	assert.Equal(t, "pg_txn_total", got[1].Name)
	assert.Equal(t, mtypes.MetricTypeCounter, got[1].Type)
	assert.Empty(t, got[1].Labels)
}

func TestParse_TargetDatabaseTagged(t *testing.T) {
	raw := []byte(`
pg_scoped:
  type: gauge
  help: "scoped"
  query: "SELECT 1 AS value"
  value: value
`)
	got, err := NewParser().Parse(raw, testSource(), ptr.To("team-a-db"))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].TargetDatabase)
	assert.Equal(t, "team-a-db", *got[0].TargetDatabase)
}

func TestParse_EmptyDocument(t *testing.T) {
	for _, raw := range [][]byte{nil, {}, []byte("   \n"), []byte("{}"), []byte("null")} {
		got, err := NewParser().Parse(raw, testSource(), nil)
		assert.Nil(t, got)
		assert.ErrorIs(t, err, mtypes.ErrInvalidQueryDefinition, "empty query package must be rejected")
	}
}

func TestParse_MissingRequiredField(t *testing.T) {
	cases := map[string]string{
		"missing type":  `pg_x: {help: h, query: q, value: value}`,
		"missing help":  `pg_x: {type: gauge, query: q, value: value}`,
		"missing query": `pg_x: {type: gauge, help: h, value: value}`,
		"missing value": `pg_x: {type: gauge, help: h, query: q}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewParser().Parse([]byte(raw), testSource(), nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, mtypes.ErrInvalidQueryDefinition)
		})
	}
}

func TestParse_ReportsAllValidationIssuesDeterministically(t *testing.T) {
	raw := []byte(`
zeta:
  type: histogram
  help: ""
  query: ""
  value: value
  labels: ["", value, state, state]
alpha:
  help: ""
  query: ""
`)

	_, err := NewParser().Parse(raw, testSource(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, mtypes.ErrInvalidQueryDefinition)

	message := err.Error()
	expectedInOrder := []string{
		`metric "alpha" missing required field "type"`,
		`metric "alpha" missing required field "help"`,
		`metric "alpha" missing required field "query"`,
		`metric "alpha" missing required field "value"`,
		`metric "zeta" missing required field "help"`,
		`metric "zeta" missing required field "query"`,
		`metric "zeta" contains an empty label at index 0`,
		`metric "zeta" uses value column "value" as a label at index 1`,
		`metric "zeta" contains duplicate label "state" at index 3`,
		`metric "zeta" has unsupported type "histogram"`,
	}
	position := -1
	for _, issue := range expectedInOrder {
		next := strings.Index(message, issue)
		require.Greater(t, next, position, "issue %q must be present in deterministic order: %s", issue, message)
		position = next
	}
}

func TestParse_UnsupportedType(t *testing.T) {
	raw := []byte(`pg_x: {type: histogram, help: h, query: q, value: value}`)
	_, err := NewParser().Parse(raw, testSource(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, mtypes.ErrInvalidQueryDefinition)
	assert.Contains(t, err.Error(), "histogram")
}

func TestParse_RejectsAmbiguousNamesAndLabels(t *testing.T) {
	tests := map[string]struct {
		raw     string
		message string
	}{
		"reserved query separator": {
			raw:     `orders:waiting: {type: gauge, help: h, query: q, value: value}`,
			message: "reserved character ':'",
		},
		"empty label": {
			raw:     `pg_x: {type: gauge, help: h, query: q, value: value, labels: [""]}`,
			message: "empty label",
		},
		"duplicate label": {
			raw:     `pg_x: {type: gauge, help: h, query: q, value: value, labels: [state, state]}`,
			message: `duplicate label "state"`,
		},
		"value reused as label": {
			raw:     `pg_x: {type: gauge, help: h, query: q, value: value, labels: [value]}`,
			message: `uses value column "value" as a label`,
		},
		"hyphen in metric name": {
			raw:     `pg-x: {type: gauge, help: h, query: q, value: value}`,
			message: `invalid metric name "pg-x"`,
		},
		"dot in metric name": {
			raw:     `pg.x: {type: gauge, help: h, query: q, value: value}`,
			message: `invalid metric name "pg.x"`,
		},
		"leading digit in metric name": {
			raw:     `2fast: {type: gauge, help: h, query: q, value: value}`,
			message: `invalid metric name "2fast"`,
		},
		"non-ASCII metric name": {
			raw:     `połączenia: {type: gauge, help: h, query: q, value: value}`,
			message: `invalid metric name "połączenia"`,
		},
		"invalid value column": {
			raw:     `pg_x: {type: gauge, help: h, query: q, value: value.count}`,
			message: `invalid value column "value.count"`,
		},
		"invalid label": {
			raw:     `pg_x: {type: gauge, help: h, query: q, value: value, labels: [region-code]}`,
			message: `invalid label "region-code"`,
		},
		"reserved Prometheus label": {
			raw:     `pg_x: {type: gauge, help: h, query: q, value: value, labels: [__name__]}`,
			message: `uses reserved label "__name__"`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewParser().Parse([]byte(tt.raw), testSource(), nil)
			require.Error(t, err)
			assert.ErrorIs(t, err, mtypes.ErrInvalidQueryDefinition)
			assert.Contains(t, err.Error(), tt.message)
		})
	}
}

func TestParse_AcceptsLegacySafeIdentifiers(t *testing.T) {
	raw := []byte(`pg_connections_2: {type: gauge, help: h, query: q, value: value_2, labels: [_region, state2]}`)

	queries, err := NewParser().Parse(raw, testSource(), nil)

	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "pg_connections_2", queries[0].Name)
	assert.Equal(t, "value_2", queries[0].Value)
	assert.Equal(t, []string{"_region", "state2"}, queries[0].Labels)
}

func TestParse_NormalizesTerminalStatementTerminatorBeforeComment(t *testing.T) {
	raw := []byte(`
pg_x:
  type: gauge
  help: h
  query: SELECT 1 AS value; -- why
  value: value
`)

	queries, err := NewParser().Parse(raw, testSource(), nil)

	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "SELECT 1 AS value -- why", queries[0].SQL)
}

func TestParse_RejectsNonTerminalStatementTerminators(t *testing.T) {
	raw := []byte(`
pg_x:
  type: gauge
  help: h
  query: |
    SELECT 1 AS value) AS first;
    DELETE FROM audit_queue;
    SELECT * FROM (SELECT 2 AS value; -- finish
  value: value
`)

	queries, err := NewParser().Parse(raw, testSource(), nil)

	assert.Nil(t, queries)
	require.Error(t, err)
	assert.ErrorIs(t, err, mtypes.ErrInvalidQueryDefinition)
	assert.Contains(t, err.Error(), "demo/alpha/queries.yaml")
	assert.Contains(t, err.Error(), "non-terminal statement terminator")
	assert.Contains(t, err.Error(), "exactly one SQL statement is allowed")
}

func TestParse_MalformedYAML(t *testing.T) {
	raw := []byte("pg_x: [this is: not valid")
	_, err := NewParser().Parse(raw, testSource(), nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, mtypes.ErrInvalidQueryDefinition))
}

func TestParse_StrictYAML(t *testing.T) {
	tests := map[string]string{
		"duplicate query name": `
pg_x:
  type: gauge
  help: first
  query: SELECT 1 AS value
  value: value
pg_x:
  type: gauge
  help: second
  query: SELECT 2 AS value
  value: value
`,
		"duplicate query field": `
pg_x:
  type: gauge
  help: first
  help: second
  query: SELECT 1 AS value
  value: value
`,
		"unknown query field": `
pg_x:
  type: gauge
  help: h
  qurey: SELECT 1 AS value
  query: SELECT 1 AS value
  value: value
`,
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewParser().Parse([]byte(raw), testSource(), nil)

			require.Error(t, err)
			assert.ErrorIs(t, err, mtypes.ErrInvalidQueryDefinition)
			assert.Contains(t, err.Error(), "demo/alpha/queries.yaml")
		})
	}
}
