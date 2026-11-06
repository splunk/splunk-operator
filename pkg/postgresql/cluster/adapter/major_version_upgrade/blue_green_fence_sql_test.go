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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCutoverDrainRequeriesAfterTermination(t *testing.T) {
	id := cutoverIdentity{Role: "control", AttemptID: "attempt-1"}
	for _, remaining := range []bool{true, false} {
		t.Run(fmt.Sprintf("sessions remain %t", remaining), func(t *testing.T) {
			var terminated bool
			conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
				if strings.Contains(sql, `SELECT "splunk_operator_bluegreen"."cutover_terminate_sessions"()`) {
					terminated = true
					return [][]any{{true}}, nil
				}
				if strings.Contains(sql, "SELECT NOT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity WHERE") {
					assert.True(t, terminated, "termination acknowledgment must not be treated as proof of absence")
					assert.Contains(t, sql, "pg_prepared_xacts")
					return [][]any{{!remaining}}, nil
				}
				return [][]any{{true}}, nil
			}}
			drained, err := drainCutoverInstance(t.Context(), conn, id, true)
			require.NoError(t, err)
			assert.Equal(t, !remaining, drained)
		})
	}
}

func TestCutoverDrainReadOnlyPassDoesNotTerminateSessions(t *testing.T) {
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		assert.NotContains(t, sql, `SELECT "splunk_operator_bluegreen"."cutover_terminate_sessions"()`)
		return [][]any{{true}}, nil
	}}
	drained, err := drainCutoverInstance(t.Context(), conn, cutoverIdentity{Role: "control", AttemptID: "attempt-1"}, false)
	require.NoError(t, err)
	assert.True(t, drained)
	assert.Empty(t, conn.execs)
}
