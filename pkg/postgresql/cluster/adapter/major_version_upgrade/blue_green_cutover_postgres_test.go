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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Use only the same explicitly disposable database contract as the source SQL
// tests. A private database and unique roles keep test objects isolated.
func TestCutoverSQLAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable PostgreSQL superuser database")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close(context.WithoutCancel(t.Context())) })
	suffix := time.Now().UnixNano()
	database := fmt.Sprintf("bg_cutover_%d", suffix)
	id := cutoverIdentity{Role: fmt.Sprintf("bg_control_%d", suffix), Password: `pa'ss\word`, AttemptID: fmt.Sprintf("attempt-%d", suffix)}
	application := fmt.Sprintf("bg_application_%d", suffix)
	publisher := replicationRoleName(id.AttemptID)
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pginfra.QuoteIdentifier(database))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup := context.WithoutCancel(t.Context())
		_, _ = admin.Exec(cleanup, "DROP DATABASE IF EXISTS "+pginfra.QuoteIdentifier(database)+" WITH (FORCE)")
		_, _ = admin.Exec(cleanup, "DROP ROLE IF EXISTS "+pginfra.QuoteIdentifier(id.Role))
		_, _ = admin.Exec(cleanup, "DROP ROLE IF EXISTS "+pginfra.QuoteIdentifier(publisher))
		_, _ = admin.Exec(cleanup, "DROP ROLE IF EXISTS "+pginfra.QuoteIdentifier(application))
	})
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.Database = database
	bootstrap, err := pgx.ConnectConfig(t.Context(), config)
	require.NoError(t, err)
	defer bootstrap.Close(t.Context())
	conn := livePreflightSQLConn{Conn: bootstrap}
	require.NoError(t, proveCutoverWritable(t.Context(), conn))
	var probeAbsent bool
	require.NoError(t, queryOne(t.Context(), conn, &probeAbsent, `SELECT pg_catalog.to_regclass('pg_temp.splunk_operator_cutover_probe') IS NULL`))
	assert.True(t, probeAbsent, "the target write probe must leave no persistent object")
	_, err = bootstrap.Exec(t.Context(), "CREATE ROLE "+pginfra.QuoteIdentifier(application)+" LOGIN")
	require.NoError(t, err)
	_, err = bootstrap.Exec(t.Context(), `CREATE TABLE public.orders (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY); CREATE UNLOGGED TABLE public.empty_unlogged (id integer)`)
	require.NoError(t, err)
	var baseline string
	require.NoError(t, queryOne(t.Context(), conn, &baseline, accessFingerprintSQL, replicationRoleName(id.AttemptID)))
	baselineDefinitions, err := queryStrings(t.Context(), conn, cutoverFingerprintDefinitionsSQL(accessFingerprintSQL), replicationRoleName(id.AttemptID))
	require.NoError(t, err)
	require.NoError(t, installCandidateDMLGuards(t.Context(), conn))
	_, err = bootstrap.Exec(t.Context(), "CREATE ROLE "+pginfra.QuoteIdentifier(id.Role)+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD "+quoteSQLStringLiteral(id.Password))
	require.NoError(t, err)
	_, err = bootstrap.Exec(t.Context(), "GRANT pg_read_all_stats TO "+pginfra.QuoteIdentifier(id.Role))
	require.NoError(t, err)
	require.NoError(t, (&postgresSwitchoverSQL{}).installControlDatabase(t.Context(), conn, database, id, true))
	_, err = bootstrap.Exec(t.Context(), "ALTER ROLE "+pginfra.QuoteIdentifier(application)+" NOLOGIN")
	require.NoError(t, err)
	controlConfig := config.Copy()
	controlConfig.User, controlConfig.Password = id.Role, id.Password
	control, err := pgx.ConnectConfig(t.Context(), controlConfig)
	require.NoError(t, err)
	defer control.Close(t.Context())
	restricted := livePreflightSQLConn{Conn: control}
	var finalGreenLSN string
	require.NoError(t, queryOne(t.Context(), restricted, &finalGreenLSN, targetWALPositionSQL))
	var durable bool
	require.NoError(t, queryOne(t.Context(), restricted, &durable, targetPrimaryDurabilitySQL, finalGreenLSN))
	assert.True(t, durable, "the restricted identity can prove locally flushed target WAL")
	require.NoError(t, queryOne(t.Context(), restricted, &durable, targetStandbyDurabilitySQL, finalGreenLSN))
	assert.False(t, durable, "a promoted target must not satisfy a stale standby proof")
	verifyCutoverPublicationSQLAgainstPostgres(t, bootstrap, restricted, database)
	require.NoError(t, verifyControlDatabase(t.Context(), restricted, id))
	require.NoError(t, verifyCandidateDMLGuards(t.Context(), restricted))
	var fenced bool
	require.NoError(t, queryOne(t.Context(), restricted, &fenced, `SELECT splunk_operator_bluegreen.cutover_fence_file_valid()`))
	assert.False(t, fenced, "the unfenced disposable server must not be mistaken for a loaded access fence")
	var normalized string
	require.NoError(t, queryOne(t.Context(), restricted, &normalized, cutoverAccessFingerprintSQL(sideBlue), replicationRoleName(id.AttemptID), id.Role, `[{"roleName":"`+application+`","login":true}]`))
	normalizedDefinitions, err := queryStrings(t.Context(), restricted, cutoverFingerprintDefinitionsSQL(cutoverAccessFingerprintSQL(sideBlue)), replicationRoleName(id.AttemptID), id.Role, `[{"roleName":"`+application+`","login":true}]`)
	require.NoError(t, err)
	assert.Equal(t, baselineDefinitions, normalizedDefinitions)
	assert.Equal(t, baseline, normalized, "only the verified control grants and captured login overlay may disappear")
	// CNPG precreates the publisher only as an inert ACL grantee on green.
	// The same restricted identity must prove its attributes and application
	// ACLs without permission to inspect pg_authid or credential columns.
	quotedPublisher := pginfra.QuoteIdentifier(publisher)
	resetPublisher := "ALTER ROLE " + quotedPublisher + " NOLOGIN NOREPLICATION NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS INHERIT CONNECTION LIMIT -1 PASSWORD NULL"
	_, err = bootstrap.Exec(t.Context(), "CREATE ROLE "+quotedPublisher+" NOLOGIN NOREPLICATION")
	require.NoError(t, err)
	greenFingerprint := func() string {
		t.Helper()
		var observed string
		require.NoError(t, queryOne(t.Context(), restricted, &observed, cutoverAccessFingerprintSQL(sideGreen), publisher, id.Role, `[{"roleName":"`+application+`","login":true}]`))
		return observed
	}
	assert.Equal(t, baseline, greenFingerprint(), "the inert green role must preserve the exact application fingerprint")
	for _, grant := range []struct{ apply, revoke string }{
		{"GRANT CONNECT ON DATABASE " + pginfra.QuoteIdentifier(database) + " TO " + quotedPublisher, "REVOKE CONNECT ON DATABASE " + pginfra.QuoteIdentifier(database) + " FROM " + quotedPublisher},
		{"GRANT USAGE ON SCHEMA public TO " + quotedPublisher, "REVOKE USAGE ON SCHEMA public FROM " + quotedPublisher},
		{"GRANT SELECT ON public.orders TO " + quotedPublisher, "REVOKE SELECT ON public.orders FROM " + quotedPublisher},
	} {
		_, err = bootstrap.Exec(t.Context(), grant.apply)
		require.NoError(t, err)
		assert.Equal(t, baseline, greenFingerprint(), "pg_restore must retain allowed publisher ACL references: "+grant.apply)
		_, err = bootstrap.Exec(t.Context(), grant.revoke)
		require.NoError(t, err)
	}
	for _, option := range []string{"LOGIN", "REPLICATION", "SUPERUSER", "CREATEDB", "CREATEROLE", "BYPASSRLS", "NOINHERIT", "CONNECTION LIMIT 1"} {
		t.Run("green rejects "+option, func(t *testing.T) {
			_, err := bootstrap.Exec(t.Context(), resetPublisher)
			require.NoError(t, err)
			_, err = bootstrap.Exec(t.Context(), "ALTER ROLE "+quotedPublisher+" "+option)
			require.NoError(t, err)
			assert.NotEqual(t, baseline, greenFingerprint())
		})
	}
	_, err = bootstrap.Exec(t.Context(), resetPublisher)
	require.NoError(t, err)
	for _, grant := range []struct{ apply, revoke string }{
		{"GRANT " + pginfra.QuoteIdentifier(application) + " TO " + quotedPublisher, "REVOKE " + pginfra.QuoteIdentifier(application) + " FROM " + quotedPublisher},
		{"GRANT UPDATE ON public.orders TO " + quotedPublisher, "REVOKE UPDATE ON public.orders FROM " + quotedPublisher},
		{"GRANT SELECT ON public.orders TO " + quotedPublisher + " WITH GRANT OPTION", "REVOKE SELECT ON public.orders FROM " + quotedPublisher},
		{"GRANT SELECT ON public.orders TO " + pginfra.QuoteIdentifier(application), "REVOKE SELECT ON public.orders FROM " + pginfra.QuoteIdentifier(application)},
	} {
		_, err = bootstrap.Exec(t.Context(), grant.apply)
		require.NoError(t, err)
		assert.NotEqual(t, baseline, greenFingerprint(), grant.apply)
		_, err = bootstrap.Exec(t.Context(), grant.revoke)
		require.NoError(t, err)
		assert.Equal(t, baseline, greenFingerprint(), "restoring the exact allowed grants must restore the fingerprint")
	}
	_, err = bootstrap.Exec(t.Context(), "DROP ROLE "+quotedPublisher)
	require.NoError(t, err)
	assert.NotEqual(t, baseline, greenFingerprint(), "an absent shadow cannot satisfy the green policy")
	_, err = control.Exec(t.Context(), "ALTER ROLE "+pginfra.QuoteIdentifier(id.Role)+" SUPERUSER")
	require.Error(t, err)
	_, err = control.Exec(t.Context(), "SELECT subconninfo FROM pg_catalog.pg_subscription")
	require.Error(t, err, "control must not read replication credentials")
	var removed bool
	require.NoError(t, queryOne(t.Context(), restricted, &removed, `SELECT splunk_operator_bluegreen.cutover_remove_guards()`))
	assert.True(t, removed)
	sequences, err := readCutoverSequences(t.Context(), restricted)
	require.NoError(t, err)
	require.Len(t, sequences, 1)
	sequences[0].value, sequences[0].called = 456, false
	require.NoError(t, syncCutoverSequences(t.Context(), restricted, sequences))
	actual, err := readCutoverSequences(t.Context(), restricted)
	require.NoError(t, err)
	assert.Equal(t, sequences, actual)
	_, err = bootstrap.Exec(t.Context(), "GRANT SELECT (subconninfo) ON pg_catalog.pg_subscription TO "+pginfra.QuoteIdentifier(id.Role))
	require.NoError(t, err)
	require.ErrorIs(t, verifyControlDatabase(t.Context(), restricted, id), mvutypes.ErrUpgradeFlowFailed, "unrequested catalog-column access must invalidate control proof")
	_, err = bootstrap.Exec(t.Context(), "REVOKE SELECT (subconninfo) ON pg_catalog.pg_subscription FROM "+pginfra.QuoteIdentifier(id.Role))
	require.NoError(t, err)
	require.NoError(t, verifyControlDatabase(t.Context(), restricted, id))
	_, err = bootstrap.Exec(t.Context(), "GRANT SELECT ON public.orders TO "+pginfra.QuoteIdentifier(id.Role))
	require.NoError(t, err)
	require.ErrorIs(t, verifyControlDatabase(t.Context(), restricted, id), mvutypes.ErrUpgradeFlowFailed, "unexpected grants must be rejected before normalization")
}

func verifyCutoverPublicationSQLAgainstPostgres(t *testing.T, admin *pgx.Conn, restricted pginfra.Conn, database string) {
	t.Helper()
	name := publicationName("cutover-test", database)
	quoted := pginfra.QuoteIdentifier(name)
	check := func(want bool) {
		t.Helper()
		var valid bool
		require.NoError(t, queryOne(t.Context(), restricted, &valid, cutoverPublicationInventorySQL, name))
		assert.Equal(t, want, valid)
	}
	check(false)
	_, err := admin.Exec(t.Context(), "CREATE PUBLICATION "+quoted+" FOR ALL TABLES")
	require.NoError(t, err)
	_, err = admin.Exec(t.Context(), "ALTER PUBLICATION "+quoted+" OWNER TO postgres")
	require.NoError(t, err)
	check(true)
	for _, option := range []string{"publish_via_partition_root=true", "publish='insert'"} {
		_, err = admin.Exec(t.Context(), "ALTER PUBLICATION "+quoted+" SET ("+option+")")
		require.NoError(t, err)
		check(false)
		_, err = admin.Exec(t.Context(), "ALTER PUBLICATION "+quoted+" SET (publish_via_partition_root=false, publish='insert, update, delete, truncate')")
		require.NoError(t, err)
		check(true)
	}
	_, err = admin.Exec(t.Context(), "CREATE PUBLICATION unexpected_cutover_publication FOR ALL TABLES")
	require.NoError(t, err)
	check(false)
	_, err = admin.Exec(t.Context(), "DROP PUBLICATION unexpected_cutover_publication")
	require.NoError(t, err)
	check(true)
	_, err = admin.Exec(t.Context(), "DROP PUBLICATION "+quoted)
	require.NoError(t, err)
	check(false)
}

func cutoverFingerprintDefinitionsSQL(sql string) string {
	start := strings.Index(sql, "SELECT encode(sha256")
	from := start + strings.Index(sql[start:], "\nFROM (")
	return sql[:start] + "SELECT definition" + sql[from:] + " ORDER BY definition"
}
