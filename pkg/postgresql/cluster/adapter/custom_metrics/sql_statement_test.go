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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSingleSQLStatement(t *testing.T) {
	tests := []struct {
		name            string
		sql             string
		want            string
		wantErrContains string
	}{
		{name: "no terminator", sql: "SELECT 1 AS value", want: "SELECT 1 AS value"},
		{name: "terminal terminator", sql: "SELECT 1 AS value;", want: "SELECT 1 AS value"},
		{name: "line comment", sql: "SELECT 1 AS value; -- why", want: "SELECT 1 AS value -- why"},
		{name: "line comment on next line", sql: "SELECT 1 AS value;\n-- why", want: "SELECT 1 AS value\n-- why"},
		{name: "block comment", sql: "SELECT 1 AS value; /* why */", want: "SELECT 1 AS value /* why */"},
		{name: "nested block comment", sql: "SELECT 1 AS value; /* outer /* nested */ outer */", want: "SELECT 1 AS value /* outer /* nested */ outer */"},
		{name: "comment before terminator", sql: "SELECT 1 AS value /* why */;", want: "SELECT 1 AS value /* why */"},
		{name: "semicolon in string", sql: "SELECT ';' AS label, 1 AS value", want: "SELECT ';' AS label, 1 AS value"},
		{name: "comment marker in string", sql: "SELECT ';-- not a comment' AS label, 1 AS value", want: "SELECT ';-- not a comment' AS label, 1 AS value"},
		{name: "semicolon in escaped string", sql: `SELECT E'it\'s;' AS label, 1 AS value; -- why`, want: `SELECT E'it\'s;' AS label, 1 AS value -- why`},
		{name: "semicolon in quoted identifier", sql: `SELECT "value;name" AS value; -- why`, want: `SELECT "value;name" AS value -- why`},
		{name: "semicolon in dollar quote", sql: "SELECT $$;$$ AS label, 1 AS value; -- why", want: "SELECT $$;$$ AS label, 1 AS value -- why"},
		{name: "comment marker in tagged dollar quote", sql: "SELECT $body$; -- not a comment$body$ AS label, 1 AS value; -- why", want: "SELECT $body$; -- not a comment$body$ AS label, 1 AS value -- why"},
		{name: "semicolon in line comment", sql: "SELECT 1 AS value -- why;", want: "SELECT 1 AS value -- why;"},
		{name: "multiple statements", sql: "SELECT 1 AS value; SELECT 2 AS value; -- why", wantErrContains: "non-terminal statement terminator"},
		{name: "wrapper breakout", sql: "SELECT 1 AS value) AS first; DELETE FROM audit_queue; SELECT * FROM (SELECT 2 AS value; -- finish", wantErrContains: "non-terminal statement terminator"},
		{name: "wrapper breakout before unterminated block comment", sql: "SELECT 1 AS value) AS splunk_operator_custom_metrics; SELECT pg_sleep(10); /*", wantErrContains: "unterminated block comment"},
		{name: "unterminated nested block comment", sql: "SELECT 1 AS value /* outer /* nested */", wantErrContains: "unterminated block comment"},
		{name: "unterminated string", sql: "SELECT 'value", wantErrContains: "unterminated quoted value"},
		{name: "unterminated quoted identifier", sql: `SELECT "value`, wantErrContains: "unterminated quoted value"},
		{name: "unterminated dollar quote", sql: "SELECT $body$value", wantErrContains: "unterminated dollar-quoted value"},
		{name: "recursive query", sql: "WITH RECURSIVE t(n) AS (SELECT 1) SELECT n AS value FROM t; -- why", want: "WITH RECURSIVE t(n) AS (SELECT 1) SELECT n AS value FROM t -- why"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSingleSQLStatement(tt.sql)
			if tt.wantErrContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContains)
				assert.Contains(t, err.Error(), "exactly one SQL statement is allowed")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
