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
	"context"
	"fmt"
	"strings"
	"testing"

	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/require"
)

func TestFinalSourceLSNIncludesAcknowledgedBufferedCommits(t *testing.T) {
	sql, source, _, id, _ := retainedSourceSQLFixture(t)
	// An asynchronously acknowledged transaction has inserted its commit WAL,
	// but the WAL writer has not yet reached it when session draining finishes.
	written, inserted := "0/100", "0/200"
	sql.connect = func(_ context.Context, _, _, role, _, _ string, _ []byte) (pginfra.Conn, error) {
		require.Equal(t, id.Role, role)
		return &cutoverSQLTestConn{query: func(query string, _ []any) ([][]any, error) {
			switch {
			case strings.Contains(query, "pg_current_wal_insert_lsn()"):
				return [][]any{{inserted}}, nil
			case strings.Contains(query, "pg_current_wal_lsn()"):
				return [][]any{{written}}, nil
			default:
				return nil, fmt.Errorf("unexpected source WAL query: %s", query)
			}
		}}, nil
	}
	final, err := sql.SourceLSN(t.Context(), source, id)
	require.NoError(t, err)
	require.Equal(t, inserted, final, "final parity must wait for the buffered acknowledged transaction, not only previously written WAL")
}
