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
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCutoverSQLCancellationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable PostgreSQL superuser database")
	}
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		_ = conn.Close(cleanup)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = withCutoverConn(ctx, livePreflightSQLConn{Conn: conn}, func(conn pginfra.Conn) error {
		var complete bool
		return queryCutoverBoolean(ctx, conn, &complete, "SELECT true FROM pg_catalog.pg_sleep(30)")
	})
	require.ErrorIs(t, err, context.DeadlineExceeded, "an established connection must honor the operation deadline")
	assert.Less(t, time.Since(started), 5*time.Second)
	assert.True(t, conn.IsClosed(), "the timed-out connection must not survive its recovery attempt")
	// Cancellation of one attempt must not poison a fresh attempt.
	retry, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	var complete bool
	require.NoError(t, withCutoverConn(t.Context(), livePreflightSQLConn{Conn: retry}, func(conn pginfra.Conn) error {
		return queryCutoverBoolean(t.Context(), conn, &complete, "SELECT true")
	}))
	assert.True(t, complete)
}

func TestCutoverDrainWaitsForInitializingBackendAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable PostgreSQL superuser database")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	suffix := time.Now().UnixNano()
	database := fmt.Sprintf("bg_startup_%d", suffix)
	application := fmt.Sprintf("bg_application_%d", suffix)
	id := cutoverIdentity{Role: fmt.Sprintf("bg_control_%d", suffix), Password: "test-control-password", AttemptID: fmt.Sprintf("startup-%d", suffix)}
	observerRole := replicationRoleName(id.AttemptID)
	_, err = admin.Exec(ctx, "CREATE DATABASE "+pginfra.QuoteIdentifier(database))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer stop()
		conn, err := pgx.Connect(cleanup, dsn)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(cleanup)
		_, err = conn.Exec(cleanup, "DROP DATABASE IF EXISTS "+pginfra.QuoteIdentifier(database)+" WITH (FORCE)")
		assert.NoError(t, err)
		for _, role := range []string{application, id.Role, observerRole} {
			_, err = conn.Exec(cleanup, "DROP ROLE IF EXISTS "+pginfra.QuoteIdentifier(role))
			assert.NoError(t, err)
		}
	})
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.Database = database
	bootstrap, err := pgx.ConnectConfig(ctx, config)
	require.NoError(t, err)
	defer bootstrap.Close(ctx)
	_, err = bootstrap.Exec(ctx, "CREATE ROLE "+pginfra.QuoteIdentifier(application)+" LOGIN PASSWORD 'test-application-password'; CREATE TABLE public.orders (id integer); GRANT INSERT ON public.orders TO "+pginfra.QuoteIdentifier(application))
	require.NoError(t, err)
	require.NoError(t, prepareControlRole(ctx, livePreflightSQLConn{Conn: bootstrap}, id))
	require.NoError(t, (&postgresSwitchoverSQL{}).installControlDatabase(ctx, livePreflightSQLConn{Conn: bootstrap}, database, id, false))
	// Keep test administration outside the application-session predicate. This
	// privileged observer exists only in the explicitly disposable test server.
	_, err = bootstrap.Exec(ctx, "CREATE ROLE "+pginfra.QuoteIdentifier(observerRole)+" LOGIN SUPERUSER PASSWORD 'test-observer-password'")
	require.NoError(t, err)
	observerConfig := config.Copy()
	observerConfig.User, observerConfig.Password = observerRole, "test-observer-password"
	observer, err := pgx.ConnectConfig(ctx, observerConfig)
	require.NoError(t, err)
	defer observer.Close(ctx)
	controlConfig := config.Copy()
	controlConfig.User, controlConfig.Password = id.Role, id.Password
	control, err := pgx.ConnectConfig(ctx, controlConfig)
	require.NoError(t, err)
	defer control.Close(ctx)
	require.NoError(t, bootstrap.Close(ctx))
	require.NoError(t, admin.Close(ctx))

	// InitPostgres takes this database object lock after checking LOGIN and
	// holds it through post_auth_delay, before registering activity on PG17.
	// Observe that actual startup state rather than guessing when to fence.
	applicationConfig := config.Copy()
	applicationConfig.User, applicationConfig.Password = application, "test-application-password"
	applicationConfig.RuntimeParams["post_auth_delay"] = "5"
	type connected struct {
		conn *pgx.Conn
		err  error
	}
	result := make(chan connected, 1)
	go func() {
		conn, err := pgx.ConnectConfig(ctx, applicationConfig)
		result <- connected{conn, err}
	}()
	var initializingPID int32
	require.Eventually(t, func() bool {
		return observer.QueryRow(ctx, `SELECT pid FROM pg_catalog.pg_locks WHERE locktype='object' AND classid='pg_catalog.pg_database'::regclass AND objid=(SELECT oid FROM pg_catalog.pg_database WHERE datname=current_database()) AND mode='RowExclusiveLock' AND granted`).Scan(&initializingPID) == nil
	}, 5*time.Second, 10*time.Millisecond)
	var version int
	require.NoError(t, observer.QueryRow(ctx, "SELECT current_setting('server_version_num')::integer").Scan(&version))
	var visible bool
	require.NoError(t, observer.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_stat_activity WHERE pid=$1)", initializingPID).Scan(&visible))
	if version < 180000 {
		require.False(t, visible, "the regression must exercise the PostgreSQL 17 activity-registration gap")
	}
	var startupLock bool
	require.NoError(t, observer.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_locks WHERE pid=$1 AND locktype='virtualxid' AND mode='ExclusiveLock' AND granted)", initializingPID).Scan(&startupLock))
	require.True(t, startupLock, "an admitted initializing connection must be visible to the source barrier")
	t.Logf("PostgreSQL %d: initializing backend %d visible in activity=%t, virtualxid lock=%t", version, initializingPID, visible, startupLock)
	_, err = observer.Exec(ctx, "ALTER ROLE "+pginfra.QuoteIdentifier(application)+" NOLOGIN")
	require.NoError(t, err)
	_, err = pgx.ConnectConfig(ctx, applicationConfig)
	require.Error(t, err, "fresh connections must already be fenced")
	require.True(t, intentionalFenceDenial(err), "a timeout or another connection failure is not proof of the fence")
	for _, terminate := range []bool{false, true} {
		drained, err := drainCutoverInstance(ctx, livePreflightSQLConn{Conn: control}, id, terminate)
		require.NoError(t, err)
		assert.False(t, drained, "an initializing backend must block the final source position, even after termination of visible sessions")
	}
	select {
	case early := <-result:
		if early.conn != nil {
			_ = early.conn.Close(ctx)
		}
		t.Fatalf("startup completed before the barrier assertions; the required ordering was not exercised: %v", early.err)
	default:
	}
	var sourcePosition string
	require.NoError(t, observer.QueryRow(ctx, "SELECT pg_catalog.pg_current_wal_lsn()::text").Scan(&sourcePosition))
	var started connected
	// Force startup to finish between lock capture and activity observation.
	// Sampling activity first and locks second can miss this exact handoff.
	handoff := startupHandoffConn{Conn: livePreflightSQLConn{Conn: control}, handoff: func() {
		select {
		case started = <-result:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		require.NoError(t, started.err)
		_, err = started.conn.Exec(ctx, "INSERT INTO public.orders VALUES (1)")
		require.NoError(t, err, "NOLOGIN does not revoke a connection that already passed its startup login check")
	}}
	drained, err := drainCutoverInstance(ctx, handoff, id, false)
	require.NoError(t, err)
	require.NotNil(t, started.conn, "the startup transition must occur between the production barrier's observations")
	defer started.conn.Close(ctx)
	assert.False(t, drained, "a backend that finishes startup between observations must remain a blocker")
	var later bool
	require.NoError(t, observer.QueryRow(ctx, "SELECT pg_catalog.pg_current_wal_lsn() > $1::pg_lsn", sourcePosition).Scan(&later))
	require.True(t, later, "accepting the earlier source position would omit the late committed write")
	require.Eventually(t, func() bool {
		drained, err := drainCutoverInstance(ctx, livePreflightSQLConn{Conn: control}, id, true)
		return err == nil && drained
	}, 5*time.Second, 10*time.Millisecond)
}

type startupHandoffConn struct {
	pginfra.Conn
	handoff func()
}

func (c startupHandoffConn) Query(ctx context.Context, sql string, args ...any) (pginfra.Rows, error) {
	rows, err := c.Conn.Query(ctx, sql, args...)
	if err == nil && sql == cutoverStartupPIDsSQL {
		return startupHandoffRows{Rows: rows, handoff: c.handoff}, nil
	}
	return rows, err
}

type startupHandoffRows struct {
	pginfra.Rows
	handoff func()
}

func (r startupHandoffRows) Close() {
	r.Rows.Close()
	r.handoff()
}
