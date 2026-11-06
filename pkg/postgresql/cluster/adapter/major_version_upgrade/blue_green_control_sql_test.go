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
	"strings"
	"testing"

	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCutoverControlBootstrapConstrainsPrivilegedHelpers(t *testing.T) {
	id := cutoverIdentity{Role: "sok_bgc_attempt", Password: "password", AttemptID: "attempt-1"}
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		if strings.Contains(sql, "SELECT pg_catalog.to_regprocedure") {
			return [][]any{{false}}, nil
		}
		if strings.Contains(sql, "SELECT ns.nspname,c.relname,c.relkind='S'") {
			return [][]any{{`strange"schema`, `seq';drop role postgres;`, true}}, nil
		}
		return [][]any{{true}}, nil
	}}
	boundary := &postgresSwitchoverSQL{}
	require.NoError(t, boundary.installControlDatabase(t.Context(), conn, `db"name`, id, true))
	assert.Equal(t, 3, conn.commits)
	assert.Zero(t, conn.rollbacks)
	all := strings.Join(conn.execs, "\n")
	assert.Contains(t, all, "SECURITY DEFINER SET search_path = pg_catalog")
	assert.Contains(t, all, "REVOKE ALL ON FUNCTION")
	assert.Contains(t, all, "FROM PUBLIC")
	assert.NotContains(t, all, "GRANT pg_signal_backend")
	assert.NotContains(t, all, "GRANT ALL")
	assert.Contains(t, all, `GRANT CONNECT ON DATABASE "db""name" TO "sok_bgc_attempt"`)
	assert.Contains(t, all, `GRANT SELECT, UPDATE ON SEQUENCE "strange""schema"."seq';drop role postgres;" TO "sok_bgc_attempt"`)
	assert.Contains(t, all, "GRANT SELECT (oid,subname,subenabled,subfailover,subslotname,subdbid)")
	assert.NotContains(t, all, "subconninfo")
	for _, helper := range cutoverHelpers(id) {
		assert.Contains(t, helper.body, "session_user <> E'sok_bgc_attempt'")
		assert.NotContains(t, helper.body, "EXECUTE $1")
	}
	assert.Contains(t, cutoverHelpers(id)[0].body, "client_addr IS NOT NULL OR usename <> 'postgres'")
	assert.Contains(t, cutoverHelpers(id)[0].body, "backend_type = 'client backend'")
	assert.Contains(t, cutoverHelpers(id)[2].body, "LIMIT 4")
}

func TestCutoverControlBootstrapRefusesToReplaceAnAlteredHelper(t *testing.T) {
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		if strings.Contains(sql, "SELECT pg_catalog.to_regprocedure") {
			return [][]any{{true}}, nil
		}
		return [][]any{{false}}, nil
	}}
	err := (&postgresSwitchoverSQL{}).installControlDatabase(t.Context(), conn, "orders", cutoverIdentity{Role: "control", AttemptID: "attempt-1"}, true)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.NotContains(t, strings.Join(conn.execs, "\n"), "CREATE FUNCTION")
	assert.NotContains(t, strings.Join(conn.execs, "\n"), "CREATE OR REPLACE")
}

func TestCutoverSourceSequenceGrantsAreReadOnly(t *testing.T) {
	conn := &cutoverSQLTestConn{query: func(sql string, args []any) ([][]any, error) {
		if strings.Contains(sql, "SELECT pg_catalog.to_regprocedure") {
			return [][]any{{false}}, nil
		}
		if strings.Contains(sql, "SELECT ns.nspname,c.relname,c.relkind='S'") {
			return [][]any{{"public", "ids", true}}, nil
		}
		return [][]any{{true}}, nil
	}}
	require.NoError(t, (&postgresSwitchoverSQL{}).installControlDatabase(t.Context(), conn, "orders", cutoverIdentity{Role: "control", AttemptID: "attempt-1"}, false))
	sql := strings.Join(conn.execs, "\n")
	assert.Contains(t, sql, `GRANT SELECT ON SEQUENCE "public"."ids" TO "control"`)
	assert.NotContains(t, sql, "GRANT SELECT, UPDATE ON SEQUENCE")
}
