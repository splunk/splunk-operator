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
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Use the existing explicitly disposable superuser DSN contract. Real role
// connections and catalog checks are essential: SQL mocks cannot prove that an
// application caller reaches the intended rejection before permission checks.
func TestCandidateDMLGuardAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable PostgreSQL superuser database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close(context.WithoutCancel(t.Context())) })
	suffix := time.Now().UnixNano()
	database := fmt.Sprintf("bg_guard_%d", suffix)
	application := fmt.Sprintf("bg_guard_app_%d", suffix)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pginfra.QuoteIdentifier(database))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer stop()
		_, err := admin.Exec(cleanup, "DROP DATABASE IF EXISTS "+pginfra.QuoteIdentifier(database)+" WITH (FORCE)")
		assert.NoError(t, err)
		_, err = admin.Exec(cleanup, "DROP ROLE IF EXISTS "+pginfra.QuoteIdentifier(application))
		assert.NoError(t, err)
	})
	_, err = admin.Exec(ctx, "CREATE ROLE "+pginfra.QuoteIdentifier(application)+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD 'test-guard-password'")
	require.NoError(t, err)
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.Database = database
	bootstrap, err := pgx.ConnectConfig(ctx, config)
	require.NoError(t, err)
	defer bootstrap.Close(ctx)
	_, err = bootstrap.Exec(ctx, `CREATE TABLE public.orders (id integer PRIMARY KEY, payload text NOT NULL);
INSERT INTO public.orders VALUES (1, 'seed-one'), (2, 'seed-two')`)
	require.NoError(t, err)
	_, err = bootstrap.Exec(ctx, "ALTER TABLE public.orders OWNER TO "+pginfra.QuoteIdentifier(application))
	require.NoError(t, err)
	conn := livePreflightSQLConn{Conn: bootstrap}
	require.NoError(t, installCandidateDMLGuards(ctx, conn))
	applicationConfig := config.Copy()
	applicationConfig.User, applicationConfig.Password = application, "test-guard-password"
	app, err := pgx.ConnectConfig(ctx, applicationConfig)
	require.NoError(t, err)
	defer app.Close(ctx)

	const message = "ordinary writes are disabled while this blue/green candidate is synchronizing"
	assertPGError := func(t *testing.T, err error, code, message string) {
		t.Helper()
		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		require.True(t, ok, "expected a PostgreSQL rejection, got %v", err)
		assert.Equal(t, code, pgErr.Code)
		assert.Equal(t, message, pgErr.Message)
	}
	assertContents := func(t *testing.T) {
		t.Helper()
		var contents string
		require.NoError(t, app.QueryRow(ctx, "SELECT string_agg(id::text || '=' || payload, ',' ORDER BY id) FROM public.orders").Scan(&contents))
		assert.Equal(t, "1=seed-one,2=seed-two", contents)
	}
	guardDefinition := func(t *testing.T, conn pginfra.Conn) string {
		t.Helper()
		var definition string
		require.NoError(t, queryOne(ctx, conn, &definition, "SELECT oid::text || ':' || pg_catalog.pg_get_functiondef(oid) FROM pg_catalog.pg_proc WHERE oid = $1::regprocedure", candidateGuardSchema+"."+candidateGuardFunction+"()"))
		return definition
	}
	rollback := func(t *testing.T, tx pgx.Tx) {
		t.Helper()
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer stop()
		assert.NoError(t, tx.Rollback(cleanup))
	}

	before := guardDefinition(t, conn)
	t.Run("read-only verification", func(t *testing.T) {
		tx, err := bootstrap.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		require.NoError(t, err)
		defer rollback(t, tx)
		for range 2 {
			require.NoError(t, verifyCandidateDMLGuards(ctx, liveCandidateGuardTx{Tx: tx}))
		}
	})
	assert.Equal(t, before, guardDefinition(t, conn), "verification must preserve the exact installed guard and identity")
	for _, statement := range []string{
		"INSERT INTO public.orders VALUES (3, 'forbidden')",
		"UPDATE public.orders SET payload='forbidden' WHERE id=1",
		"DELETE FROM public.orders WHERE id=1",
		"TRUNCATE public.orders",
		"UPDATE public.orders SET payload='forbidden' WHERE id=99",
		"DELETE FROM public.orders WHERE id=99",
	} {
		t.Run(statement, func(t *testing.T) {
			_, err := app.Exec(ctx, statement)
			assertPGError(t, err, "P0001", message)
			assertContents(t)
		})
	}
	var canExecute bool
	require.NoError(t, app.QueryRow(ctx, "SELECT pg_catalog.has_function_privilege('pg_catalog.pg_replication_origin_session_is_setup()', 'EXECUTE')").Scan(&canExecute))
	assert.False(t, canExecute, "the fix must not grant access to the built-in")
	_, err = app.Exec(ctx, "SELECT pg_catalog.pg_replication_origin_session_is_setup()")
	assertPGError(t, err, "42501", "permission denied for function pg_replication_origin_session_is_setup")
	_, err = bootstrap.Exec(ctx, "INSERT INTO public.orders VALUES (3, 'forbidden')")
	assertPGError(t, err, "P0001", message)
	assertContents(t)
	functionName := pginfra.QuoteIdentifier(candidateGuardSchema) + "." + pginfra.QuoteIdentifier(candidateGuardFunction) + "()"
	for _, test := range []struct{ name, change string }{
		{"security definer", "ALTER FUNCTION " + functionName + " SECURITY DEFINER"},
		{"function configuration", "ALTER FUNCTION " + functionName + " SET search_path=pg_catalog"},
		{"disabled trigger", "ALTER TABLE public.orders DISABLE TRIGGER " + pginfra.QuoteIdentifier(candidateGuardTrigger)},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := bootstrap.Begin(ctx)
			require.NoError(t, err)
			defer rollback(t, tx)
			txConn := liveCandidateGuardTx{Tx: tx}
			require.NoError(t, verifyCandidateDMLGuards(ctx, txConn))
			_, err = tx.Exec(ctx, test.change)
			require.NoError(t, err)
			require.ErrorIs(t, verifyCandidateDMLGuards(ctx, txConn), mvutypes.ErrCandidateInvalid)
		})
		require.NoError(t, verifyCandidateDMLGuards(ctx, conn), "rollback must restore the baseline before the next case")
	}
	for _, test := range []struct{ name, body string }{
		{"permissive body", "BEGIN RETURN NULL; END;"},
		{"modified body", candidateGuardBody + "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := bootstrap.Begin(ctx)
			require.NoError(t, err)
			defer rollback(t, tx)
			txConn := liveCandidateGuardTx{Tx: tx}
			require.NoError(t, verifyCandidateDMLGuards(ctx, txConn))
			_, err = tx.Exec(ctx, fmt.Sprintf("CREATE OR REPLACE FUNCTION %s.%s() RETURNS trigger LANGUAGE plpgsql AS $guard$%s$guard$", pginfra.QuoteIdentifier(candidateGuardSchema), pginfra.QuoteIdentifier(candidateGuardFunction), test.body))
			require.NoError(t, err)
			before := guardDefinition(t, txConn)
			require.ErrorIs(t, verifyCandidateDMLGuards(ctx, txConn), mvutypes.ErrCandidateInvalid)
			assert.Equal(t, before, guardDefinition(t, txConn), "an unknown body must be rejected, never repaired")
		})
		require.NoError(t, verifyCandidateDMLGuards(ctx, conn), "rollback must restore the baseline before the next case")
	}

	// A trusted origin session still takes the existing bypass. Ordinary
	// sessions remain denied after that session state is reset.
	origin := fmt.Sprintf("bg_guard_origin_%d", suffix)
	_, err = bootstrap.Exec(ctx, "SELECT pg_catalog.pg_replication_origin_create($1)", origin)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer stop()
		// Deferred session connections close before cleanup runs, releasing
		// any origin session state even when the test context was cancelled.
		_, err := admin.Exec(cleanup, "SELECT pg_catalog.pg_replication_origin_drop($1)", origin)
		assert.NoError(t, err)
	})
	_, err = bootstrap.Exec(ctx, "SELECT pg_catalog.pg_replication_origin_session_setup($1)", origin)
	require.NoError(t, err)
	_, bypassErr := bootstrap.Exec(ctx, "INSERT INTO public.orders VALUES (3, 'replicated')")
	_, resetErr := bootstrap.Exec(ctx, "SELECT pg_catalog.pg_replication_origin_session_reset()")
	require.NoError(t, resetErr)
	require.NoError(t, bypassErr)
	var payload string
	require.NoError(t, app.QueryRow(ctx, "SELECT payload FROM public.orders WHERE id=3").Scan(&payload))
	assert.Equal(t, "replicated", payload)
	_, err = app.Exec(ctx, "DELETE FROM public.orders WHERE id=3")
	assertPGError(t, err, "P0001", message)

	_, err = bootstrap.Exec(ctx, "DROP TRIGGER "+pginfra.QuoteIdentifier(candidateGuardTrigger)+" ON public.orders; DROP FUNCTION "+functionName)
	require.NoError(t, err)
	require.ErrorIs(t, verifyCandidateDMLGuards(ctx, conn), mvutypes.ErrCandidateInvalid)
	_, err = app.Exec(ctx, "INSERT INTO public.orders VALUES (4, 'after-removal')")
	require.NoError(t, err)
	require.NoError(t, app.QueryRow(ctx, "SELECT payload FROM public.orders WHERE id=4").Scan(&payload))
	assert.Equal(t, "after-removal", payload)
}

// liveCandidateGuardTx keeps every verifier query on the transaction under test.
type liveCandidateGuardTx struct{ pgx.Tx }

func (c liveCandidateGuardTx) Begin(ctx context.Context) (pginfra.Tx, error) {
	return c.Tx.Begin(ctx)
}

func (c liveCandidateGuardTx) Query(ctx context.Context, sql string, args ...any) (pginfra.Rows, error) {
	return c.Tx.Query(ctx, sql, args...)
}

func (c liveCandidateGuardTx) Close(ctx context.Context) error {
	return c.Tx.Rollback(ctx)
}
