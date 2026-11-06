/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package majorupgradeadapter

import (
	"fmt"
	"strings"
	"testing"

	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCutoverSequenceSynchronizationPreservesIsCalledAndQuotesIdentifiers(t *testing.T) {
	for _, called := range []bool{false, true} {
		t.Run(fmt.Sprintf("called %t", called), func(t *testing.T) {
			want := cutoverSequence{schema: `strange"schema`, name: `next_id');DROP TABLE users;--`, value: 123, called: called}
			var set bool
			conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
				if strings.Contains(sql, "SELECT n.nspname,c.relname") {
					return [][]any{{want.schema, want.name}}, nil
				}
				if strings.Contains(sql, "SELECT last_value,is_called FROM") {
					assert.Equal(t, `SELECT last_value,is_called FROM "strange""schema"."next_id');DROP TABLE users;--"`, sql)
					if set {
						return [][]any{{want.value, want.called}}, nil
					}
					return [][]any{{int64(1), false}}, nil
				}
				if strings.Contains(sql, "pg_catalog.setval") {
					assert.Equal(t, []any{`"strange""schema"."next_id');DROP TABLE users;--"`, want.value, want.called}, args)
					set = true
					return [][]any{{want.value}}, nil
				}
				return nil, fmt.Errorf("unexpected query %s", sql)
			}}
			require.NoError(t, syncCutoverSequences(t.Context(), conn, []cutoverSequence{want}))
			assert.True(t, set)
			assert.Empty(t, conn.execs)
		})
	}
}

func TestCutoverSequenceSynchronizationRejectsInventoryAndReadbackDrift(t *testing.T) {
	for _, test := range []struct {
		name           string
		inventory      [][]any
		readbackCalled bool
	}{
		{"missing sequence", nil, false},
		{"different sequence", [][]any{{"public", "other"}}, false},
		{"is_called changed", [][]any{{"public", "ids"}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
				if strings.Contains(sql, "SELECT n.nspname,c.relname") {
					return test.inventory, nil
				}
				if strings.Contains(sql, "SELECT last_value,is_called") {
					return [][]any{{int64(7), test.readbackCalled}}, nil
				}
				if strings.Contains(sql, "pg_catalog.setval") {
					return [][]any{{int64(7)}}, nil
				}
				return nil, fmt.Errorf("unexpected query")
			}}
			err := syncCutoverSequences(t.Context(), conn, []cutoverSequence{{schema: "public", name: "ids", value: 7, called: true}})
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
		})
	}
}
