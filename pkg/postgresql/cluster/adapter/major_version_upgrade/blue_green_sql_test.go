/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package majorupgradeadapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureReplicationRoleQuotesPassword(t *testing.T) {
	conn := &preflightSQLTestConn{}
	require.NoError(t, ensureReplicationRole(t.Context(), conn, `role"name`, `pa'ss\word`))
	require.Len(t, conn.execs, 2)
	assert.Equal(t, `CREATE ROLE "role""name" LOGIN REPLICATION PASSWORD E'pa''ss\\word'`, conn.execs[0])
	assert.Equal(t, `ALTER ROLE "role""name" LOGIN REPLICATION PASSWORD E'pa''ss\\word'`, conn.execs[1])
}

func TestCheckUnloggedTablesReadsActualRows(t *testing.T) {
	table := `"Odd Schema"."odd table"`
	conn := &preflightSQLTestConn{results: [][]any{{table}, {true}}}
	err := checkUnloggedTables(t.Context(), conn, "orders")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.ErrorContains(t, err, table)
	require.Len(t, conn.queries, 2)
	assert.Contains(t, conn.queries[0], "format('%I.%I'")
	assert.Equal(t, "SELECT EXISTS (SELECT 1 FROM "+table+")", conn.queries[1])

	conn = &preflightSQLTestConn{results: [][]any{{table}, {false}}}
	require.NoError(t, checkUnloggedTables(t.Context(), conn, "orders"))
}

// Set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable superuser database to
// exercise the exact role and unlogged-table statements against PostgreSQL.
func TestBlueGreenSQLAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable PostgreSQL superuser database")
	}
	pgconn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	defer pgconn.Close(t.Context())
	conn := livePreflightSQLConn{Conn: pgconn}

	suffix := time.Now().UnixNano()
	role := fmt.Sprintf("bg_sql_test_%d", suffix)
	defer func() { _, _ = pgconn.Exec(t.Context(), "DROP ROLE IF EXISTS "+pginfra.QuoteIdentifier(role)) }()
	require.NoError(t, ensureReplicationRole(t.Context(), conn, role, `pa'ss\word`))
	require.NoError(t, ensureReplicationRole(t.Context(), conn, role, `new'pa\ss`))
	var login, replication bool
	require.NoError(t, pgconn.QueryRow(t.Context(), "SELECT rolcanlogin, rolreplication FROM pg_roles WHERE rolname = $1", role).Scan(&login, &replication))
	assert.True(t, login)
	assert.True(t, replication)
	_, err = pgconn.Exec(t.Context(), "ALTER ROLE "+pginfra.QuoteIdentifier(role)+" NOLOGIN")
	require.NoError(t, err)
	roles, err := queryStrings(t.Context(), conn, sourceRolesSQL)
	require.NoError(t, err)
	assert.Contains(t, roles, role, "a NOLOGIN role must remain in the inventory")
	_, err = queryStrings(t.Context(), conn, foreignObjectsSQL)
	require.NoError(t, err, "foreign-object inventory must use real PostgreSQL view columns")

	database := fmt.Sprintf("bg_sql_db_%d", suffix)
	_, err = pgconn.Exec(t.Context(), "CREATE DATABASE "+pginfra.QuoteIdentifier(database))
	require.NoError(t, err)
	defer func() { _, _ = pgconn.Exec(t.Context(), "DROP DATABASE IF EXISTS "+pginfra.QuoteIdentifier(database)) }()
	_, err = pgconn.Exec(t.Context(), "ALTER DATABASE "+pginfra.QuoteIdentifier(database)+" WITH ALLOW_CONNECTIONS false")
	require.NoError(t, err)
	names, err := queryStrings(t.Context(), conn, sourceDatabasesSQL)
	require.NoError(t, err)
	assert.Contains(t, names, database, "a connection-disabled database must remain in the inventory")

	table := fmt.Sprintf("aa_bg_sql_test_%d", suffix)
	qualifiedTable := "public." + pginfra.QuoteIdentifier(table)
	_, err = pgconn.Exec(t.Context(), "CREATE UNLOGGED TABLE "+qualifiedTable+" (id integer PRIMARY KEY)")
	require.NoError(t, err)
	defer func() { _, _ = pgconn.Exec(t.Context(), "DROP TABLE IF EXISTS "+qualifiedTable) }()
	_, err = pgconn.Exec(t.Context(), "ANALYZE "+qualifiedTable)
	require.NoError(t, err)
	_, err = pgconn.Exec(t.Context(), "INSERT INTO "+qualifiedTable+" VALUES (1)")
	require.NoError(t, err)
	var reltuples float32
	require.NoError(t, pgconn.QueryRow(t.Context(), "SELECT reltuples FROM pg_class WHERE oid = $1::regclass", qualifiedTable).Scan(&reltuples))
	assert.Zero(t, reltuples, "test must reproduce stale planner statistics")
	err = checkUnloggedTables(t.Context(), conn, "orders")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.ErrorContains(t, err, table)

	rlsTable := "public." + pginfra.QuoteIdentifier(fmt.Sprintf("bg_rls_test_%d", suffix))
	_, err = pgconn.Exec(t.Context(), "CREATE TABLE "+rlsTable+" (id integer PRIMARY KEY)")
	require.NoError(t, err)
	defer func() { _, _ = pgconn.Exec(t.Context(), "DROP TABLE IF EXISTS "+rlsTable) }()
	_, err = pgconn.Exec(t.Context(), "ALTER TABLE "+rlsTable+" ENABLE ROW LEVEL SECURITY")
	require.NoError(t, err)
	var unsupported bool
	require.NoError(t, queryOne(t.Context(), conn, &unsupported, unsupportedDatabaseObjectsSQL))
	assert.True(t, unsupported, "RLS-enabled tables must fail logical-replication preflight")
}

// Set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable PostgreSQL 17+ superuser
// database. This uses the real grant path and catalog queries across retries.
func TestReplicationGrantsDoNotChangeSourceFingerprints(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable PostgreSQL 17+ superuser database")
	}
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	admin, err := pgx.ConnectConfig(t.Context(), config)
	require.NoError(t, err)
	defer admin.Close(t.Context())
	var serverVersion int
	require.NoError(t, admin.QueryRow(t.Context(), "SELECT current_setting('server_version_num')::int").Scan(&serverVersion))
	if serverVersion < 170000 {
		t.Skip("source preflight requires PostgreSQL 17 or newer")
	}

	suffix := time.Now().UnixNano()
	database := fmt.Sprintf("bg_acl_%d", suffix)
	role := fmt.Sprintf("bg_replica_%d", suffix)
	quotedDatabase := pginfra.QuoteIdentifier(database)
	quotedRole := pginfra.QuoteIdentifier(role)
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quotedDatabase)
	require.NoError(t, err)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quotedDatabase); err != nil {
			t.Errorf("dropping SQL test database: %v", err)
		}
		if _, err := admin.Exec(cleanupCtx, "DROP ROLE IF EXISTS "+quotedRole); err != nil {
			t.Errorf("dropping SQL test role: %v", err)
		}
	}()

	connectDatabase := func(ctx context.Context, databaseName string) (livePreflightSQLConn, error) {
		cfg := config.Copy()
		cfg.Database = databaseName
		conn, err := pgx.ConnectConfig(ctx, cfg)
		return livePreflightSQLConn{Conn: conn}, err
	}
	dbConn, err := connectDatabase(t.Context(), database)
	require.NoError(t, err)
	_, err = dbConn.Exec(t.Context(), "CREATE TABLE public.orders (id integer PRIMARY KEY)")
	require.NoError(t, err)
	require.NoError(t, dbConn.Close(t.Context()))

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "source-superuser", Namespace: "default"}, Data: map[string][]byte{"password": []byte("unused-by-test-connector")}}
	source := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default"}, Spec: cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: secret.Name}}}
	preflight := &postgresSourcePreflight{
		client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build(),
		connect: func(ctx context.Context, _, databaseName, _ string) (pginfra.Conn, error) {
			return connectDatabase(ctx, databaseName)
		},
	}
	fingerprints := func() (string, string) {
		conn, err := connectDatabase(t.Context(), database)
		require.NoError(t, err)
		defer conn.Close(t.Context())
		var schema, access string
		require.NoError(t, queryOne(t.Context(), conn, &schema, schemaFingerprintSQL))
		require.NoError(t, queryOne(t.Context(), conn, &access, accessFingerprintSQL, role))
		return schema, access
	}
	baselineSchema, baselineAccess := fingerprints()
	for range 2 {
		require.NoError(t, preflight.EnsureReplicationRole(t.Context(), source, []declaredDatabase{{name: database}}, role, "test-password"))
		schema, access := fingerprints()
		assert.Equal(t, baselineSchema, schema, "attempt-owned grants must not change the schema baseline")
		assert.Equal(t, baselineAccess, access, "attempt-owned grants must not change the access baseline")
	}

	conn, err := connectDatabase(t.Context(), database)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), "GRANT UPDATE ON public.orders TO "+quotedRole)
	require.NoError(t, err)
	_, changedAccess := fingerprints()
	assert.NotEqual(t, baselineAccess, changedAccess, "unexpected grants to the attempt role must remain visible")
	_, err = conn.Exec(t.Context(), "REVOKE UPDATE ON public.orders FROM "+quotedRole)
	require.NoError(t, err)
	_, restoredAccess := fingerprints()
	assert.Equal(t, baselineAccess, restoredAccess)
	_, err = admin.Exec(t.Context(), "ALTER ROLE "+quotedRole+" SUPERUSER")
	require.NoError(t, err)
	_, changedAccess = fingerprints()
	assert.NotEqual(t, baselineAccess, changedAccess, "unsafe changes to the attempt role must remain visible")
	_, err = admin.Exec(t.Context(), "ALTER ROLE "+quotedRole+" NOSUPERUSER")
	require.NoError(t, err)
	_, restoredAccess = fingerprints()
	assert.Equal(t, baselineAccess, restoredAccess)

	_, err = conn.Exec(t.Context(), "GRANT UPDATE (id) ON public.orders TO PUBLIC")
	require.NoError(t, err)
	columnSchema, columnAccess := fingerprints()
	assert.Equal(t, baselineSchema, columnSchema, "column privileges do not change the schema fingerprint")
	assert.NotEqual(t, baselineAccess, columnAccess, "column-level grants must change the access fingerprint")
	_, err = conn.Exec(t.Context(), "REVOKE UPDATE (id) ON public.orders FROM PUBLIC")
	require.NoError(t, err)
	_, restoredAccess = fingerprints()
	assert.Equal(t, baselineAccess, restoredAccess, "revoking the column grant must restore the access baseline")

	_, err = conn.Exec(t.Context(), "GRANT UPDATE ON public.orders TO PUBLIC")
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), "ALTER TABLE public.orders ADD COLUMN note text")
	require.NoError(t, err)
	require.NoError(t, conn.Close(t.Context()))
	changedSchema, changedAccess := fingerprints()
	assert.NotEqual(t, baselineSchema, changedSchema, "unrelated schema changes must remain visible")
	assert.NotEqual(t, baselineAccess, changedAccess, "unrelated privilege changes must remain visible")
}

type preflightSQLTestConn struct {
	results     [][]any
	queries     []string
	execs       []string
	queryErrors []error
	execErrors  []error
}

func (c *preflightSQLTestConn) Begin(context.Context) (pginfra.Tx, error) {
	return nil, errors.New("not used")
}

func (c *preflightSQLTestConn) Close(context.Context) error { return nil }

func (c *preflightSQLTestConn) Query(_ context.Context, sql string, _ ...any) (pginfra.Rows, error) {
	c.queries = append(c.queries, sql)
	if len(c.queryErrors) != 0 {
		err := c.queryErrors[0]
		c.queryErrors = c.queryErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if len(c.results) == 0 {
		return nil, errors.New("unexpected query")
	}
	rows := &preflightSQLTestRows{values: c.results[0]}
	c.results = c.results[1:]
	return rows, nil
}

func (c *preflightSQLTestConn) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if len(args) != 0 {
		return pgconn.CommandTag{}, errors.New("role statements must not use bind parameters")
	}
	c.execs = append(c.execs, sql)
	if len(c.execErrors) != 0 {
		err := c.execErrors[0]
		c.execErrors = c.execErrors[1:]
		return pgconn.CommandTag{}, err
	}
	return pgconn.CommandTag{}, nil
}

type preflightSQLTestRows struct {
	values []any
	index  int
}

func (r *preflightSQLTestRows) Next() bool {
	if r.index >= len(r.values) {
		return false
	}
	r.index++
	return true
}

func (r *preflightSQLTestRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("expected one destination")
	}
	switch value := r.values[r.index-1].(type) {
	case string:
		*dest[0].(*string) = value
	case bool:
		*dest[0].(*bool) = value
	default:
		return fmt.Errorf("unexpected value type %T", value)
	}
	return nil
}

func (r *preflightSQLTestRows) Close()     {}
func (r *preflightSQLTestRows) Err() error { return nil }

type livePreflightSQLConn struct{ *pgx.Conn }

func (c livePreflightSQLConn) Begin(ctx context.Context) (pginfra.Tx, error) {
	return c.Conn.Begin(ctx)
}

func (c livePreflightSQLConn) Query(ctx context.Context, sql string, args ...any) (pginfra.Rows, error) {
	return c.Conn.Query(ctx, sql, args...)
}

var _ pginfra.Conn = (*preflightSQLTestConn)(nil)
var _ pginfra.Conn = livePreflightSQLConn{}
