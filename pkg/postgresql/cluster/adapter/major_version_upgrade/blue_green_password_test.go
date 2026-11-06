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
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradePasswordEncodingRestoresSessionAndPreservesFailures(t *testing.T) {
	readErr := errors.New("reading encoding failed")
	setErr := errors.New("setting encoding failed")
	prepareErr := errors.New("preparing role failed")
	restoreErr := errors.New("restoring encoding failed")
	for _, test := range []struct {
		name                        string
		encoding                    string
		readErr, setErr, prepareErr error
		restoreErr                  error
		prepareCalled               bool
		execs                       int
	}{
		{name: "MD5 restored after success", encoding: "md5", prepareCalled: true, execs: 2},
		{name: "MD5 restored after preparation failure", encoding: "md5", prepareErr: prepareErr, prepareCalled: true, execs: 2},
		{name: "both errors preserved", encoding: "md5", prepareErr: prepareErr, restoreErr: restoreErr, prepareCalled: true, execs: 2},
		{name: "restoration failure blocks success", encoding: "md5", restoreErr: restoreErr, prepareCalled: true, execs: 2},
		{name: "read failure blocks preparation", readErr: readErr},
		{name: "setting failure blocks preparation", encoding: "md5", setErr: setErr, execs: 1},
		{name: "SCRAM needs no setting change", encoding: scramSHA256, prepareCalled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn := &cutoverSQLTestConn{preflightSQLTestConn: preflightSQLTestConn{execErrors: []error{test.setErr, test.restoreErr}}, query: func(sql string, _ []any) ([][]any, error) {
				require.Equal(t, "SHOW password_encryption", sql)
				return [][]any{{test.encoding}}, test.readErr
			}}
			called := false
			err := withSCRAMPasswordEncoding(t.Context(), conn, func() error {
				called = true
				if test.encoding == "md5" {
					require.Equal(t, []string{"SET password_encryption = E'scram-sha-256'"}, conn.execs)
				}
				return test.prepareErr
			})
			assert.Equal(t, test.prepareCalled, called)
			require.Len(t, conn.execs, test.execs)
			if test.execs == 2 {
				assert.Equal(t, "SET password_encryption = E'md5'", conn.execs[1])
			}
			for _, expected := range []error{test.readErr, test.setErr, test.prepareErr, test.restoreErr} {
				if expected != nil {
					assert.ErrorIs(t, err, expected)
				}
			}
			if test.readErr == nil && test.setErr == nil && test.prepareErr == nil && test.restoreErr == nil {
				require.NoError(t, err)
			}
		})
	}
}

func TestPrepareControlRolePreservesSafetyCheckBeforePasswordRefresh(t *testing.T) {
	for _, safe := range []bool{false, true} {
		t.Run(fmt.Sprint(safe), func(t *testing.T) {
			conn := &preflightSQLTestConn{results: [][]any{{safe}}, execErrors: []error{&pgconn.PgError{Code: "42710"}}}
			id := cutoverIdentity{Role: `control"role`, Password: `pa'ss\word`, AttemptID: "attempt-1"}
			err := prepareControlRole(t.Context(), conn, id)
			if !safe {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
				require.Len(t, conn.execs, 1, "an unsafe existing role must not receive new credentials or grants")
				return
			}
			require.NoError(t, err)
			require.Len(t, conn.execs, 3)
			assert.Equal(t, `CREATE ROLE "control""role" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD E'pa''ss\\word'`, conn.execs[0])
			assert.Equal(t, `ALTER ROLE "control""role" PASSWORD E'pa''ss\\word'`, conn.execs[1])
			assert.Equal(t, `GRANT pg_read_all_stats TO "control""role"`, conn.execs[2])
		})
	}
}

// This DSN must point to a disposable superuser database whose TCP HBA rules
// require SCRAM. The test sets MD5 only on its own preparation connection.
func TestUpgradePasswordsAgainstSCRAMPostgres(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_SCRAM_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_SCRAM_DSN to a disposable PostgreSQL server with SCRAM-only TCP authentication")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	require.NotEmpty(t, config.Host)
	require.NotContains(t, config.Host, "/", "this test must authenticate over TCP")
	admin, err := pgx.ConnectConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	var onlySCRAM bool
	require.NoError(t, admin.QueryRow(t.Context(), `SELECT COALESCE(bool_and(auth_method = 'scram-sha-256'), false) FROM pg_hba_file_rules WHERE type LIKE 'host%'`).Scan(&onlySCRAM))
	require.True(t, onlySCRAM, "the disposable server must require SCRAM for TCP connections")
	_, err = admin.Exec(t.Context(), "SET password_encryption = 'md5'")
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		prepare func(context.Context, pginfra.Conn, string, string) error
	}{
		{"control", func(ctx context.Context, conn pginfra.Conn, role, password string) error {
			return prepareControlRole(ctx, conn, cutoverIdentity{Role: role, Password: password, AttemptID: "scram-test"})
		}},
		{"replication", ensureReplicationRole},
	} {
		t.Run(test.name, func(t *testing.T) {
			role := fmt.Sprintf("bg_scram_%s_%d", test.name, time.Now().UnixNano())
			quotedRole := pginfra.QuoteIdentifier(role)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_, cleanupErr := admin.Exec(ctx, "DROP ROLE IF EXISTS "+quotedRole)
				assert.NoError(t, cleanupErr)
			})
			for _, password := range []string{`pa'ss\word`, `new'pa\ss`} {
				require.NoError(t, test.prepare(t.Context(), livePreflightSQLConn{admin}, role, password))
				var storedPassword, encoding string
				require.NoError(t, admin.QueryRow(t.Context(), "SELECT rolpassword FROM pg_authid WHERE rolname = $1", role).Scan(&storedPassword))
				require.True(t, strings.HasPrefix(storedPassword, "SCRAM-SHA-256$"), "upgrade credentials must use SCRAM even when the preparation connection defaults to MD5")
				require.NoError(t, admin.QueryRow(t.Context(), "SHOW password_encryption").Scan(&encoding))
				assert.Equal(t, "md5", encoding, "preparation must not change the session encoding")
				cfg := config.Copy()
				cfg.User, cfg.Password = role, password+"-wrong"
				unexpected, err := pgx.ConnectConfig(t.Context(), cfg)
				if unexpected != nil {
					_ = unexpected.Close(t.Context())
				}
				require.Error(t, err, "a fresh wrong-password probe must fail; trust authentication cannot prove this regression")
				cfg.Password = password
				conn, err := pgx.ConnectConfig(t.Context(), cfg)
				require.NoError(t, err, "the prepared identity must authenticate through a SCRAM HBA rule")
				require.NoError(t, conn.Close(t.Context()))
			}
		})
	}
}
