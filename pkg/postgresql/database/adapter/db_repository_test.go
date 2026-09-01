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
package adapter

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/splunk/splunk-operator/pkg/logging"
	"github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/postgres"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	_ postgres.Conn = (*fakeConn)(nil)
	_ postgres.Tx   = (*fakeTx)(nil)
	_ postgres.Rows = (*fakeRows)(nil)
)

type fakeConn struct {
	tx               *fakeTx
	rows             *fakeRows
	queryErr         error
	beginErr         error
	closeErr         error
	closed           bool
	beginCall        int
	closeCtx         context.Context
	closeCtxErr      error
	closeDeadline    time.Time
	closeHasDeadline bool
}

func (c *fakeConn) Begin(_ context.Context) (postgres.Tx, error) {
	c.beginCall++
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return c.tx, nil
}

func (c *fakeConn) Close(ctx context.Context) error {
	c.closed = true
	c.closeCtx = ctx
	c.closeCtxErr = ctx.Err()
	c.closeDeadline, c.closeHasDeadline = ctx.Deadline()
	return c.closeErr
}

func (c *fakeConn) Query(_ context.Context, _ string, _ ...any) (postgres.Rows, error) {
	if c.queryErr != nil {
		return nil, c.queryErr
	}
	if c.rows == nil {
		return &fakeRows{}, nil
	}
	return c.rows, nil
}

func (c *fakeConn) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

type fakeRows struct {
	names   []string
	idx     int
	scanErr error
	errErr  error
	closed  bool
}

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.names) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	*(dest[0].(*string)) = r.names[r.idx-1]
	return nil
}

func (r *fakeRows) Close()     { r.closed = true }
func (r *fakeRows) Err() error { return r.errErr }

type fakeTx struct {
	execErrAt           int
	execErr             error
	commitErr           error
	rollbackErr         error
	onExec              func()
	stmts               []string
	committed           bool
	rolledBack          bool
	rollbackCalls       int
	rollbackCtx         context.Context
	rollbackCtxErr      error
	rollbackDeadline    time.Time
	rollbackHasDeadline bool
}

func (tx *fakeTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.stmts = append(tx.stmts, sql)
	if tx.onExec != nil {
		tx.onExec()
	}
	if tx.execErrAt == len(tx.stmts) {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.CommandTag{}, nil
}

func (tx *fakeTx) Commit(_ context.Context) error {
	tx.committed = true
	return tx.commitErr
}

func (tx *fakeTx) Rollback(ctx context.Context) error {
	tx.rollbackCalls++
	tx.rollbackCtx = ctx
	tx.rollbackCtxErr = ctx.Err()
	tx.rollbackDeadline, tx.rollbackHasDeadline = ctx.Deadline()
	if tx.rollbackErr != nil {
		return tx.rollbackErr
	}
	if tx.committed {
		return postgres.ErrTxAlreadyClosed
	}
	tx.rolledBack = true
	return nil
}

func TestPGDBRepositoryAssignRequiredPermissionsToRoleSuccess(t *testing.T) {
	tx := &fakeTx{}
	conn := &fakeConn{tx: tx}
	repo := &pgDBRepository{conn: conn}

	err := repo.AssignRequiredPermissionsToRole(context.Background(), "appdb", ports.DatabaseRoleNames{Admin: "appdb_admin", RW: "appdb_rw"})

	require.NoError(t, err)
	assert.True(t, conn.closed)
	assert.Equal(t, 1, conn.beginCall)
	assert.True(t, tx.committed)
	assert.Equal(t, 1, tx.rollbackCalls, "the post-commit rollback must safely close the transaction")
	assert.False(t, tx.rolledBack)
	assert.Equal(t, []string{
		`GRANT CONNECT ON DATABASE "appdb" TO "appdb_rw"`,
		`GRANT USAGE ON SCHEMA public TO "appdb_rw"`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO "appdb_rw"`,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO "appdb_rw"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "appdb_admin" IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "appdb_rw"`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE "appdb_admin" IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO "appdb_rw"`,
	}, tx.stmts)
}

func TestPGDBRepositoryAssignRequiredPermissionsToRoleUsesConfiguredRoleNames(t *testing.T) {
	tx := &fakeTx{}
	repo := &pgDBRepository{conn: &fakeConn{tx: tx}}

	require.NoError(t, repo.AssignRequiredPermissionsToRole(context.Background(), "appdb", ports.DatabaseRoleNames{Admin: "Tenant_Owner", RW: "Tenant_RW"}))

	assert.Equal(t, `GRANT CONNECT ON DATABASE "appdb" TO "Tenant_RW"`, tx.stmts[0])
	assert.Equal(t, `ALTER DEFAULT PRIVILEGES FOR ROLE "Tenant_Owner" IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "Tenant_RW"`, tx.stmts[4])
}

func TestPGDBRepositoryAssignRequiredPermissionsToRoleEscapesDatabaseName(t *testing.T) {
	tx := &fakeTx{}
	conn := &fakeConn{tx: tx}
	repo := &pgDBRepository{conn: conn}

	err := repo.AssignRequiredPermissionsToRole(context.Background(), `app"db`, ports.DatabaseRoleNames{Admin: `app"db_admin`, RW: `app"db_rw`})

	require.NoError(t, err)
	require.NotEmpty(t, tx.stmts)
	assert.Equal(t, `GRANT CONNECT ON DATABASE "app""db" TO "app""db_rw"`, tx.stmts[0])
}

func TestPGDBRepositoryAssignRequiredPermissionsToRoleErrors(t *testing.T) {
	tests := []struct {
		name         string
		conn         *fakeConn
		wantTerminal bool
		wantContains string
	}{
		{
			name:         "terminal begin error",
			conn:         &fakeConn{beginErr: &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}},
			wantTerminal: true,
			wantContains: "PostgreSQL grant transaction begin failed",
		},
		{
			name:         "retryable begin error",
			conn:         &fakeConn{beginErr: &pgconn.PgError{Code: "08006", Message: "connection failure"}},
			wantTerminal: false,
			wantContains: "PostgreSQL grant transaction begin failed",
		},
		{
			name:         "terminal exec error",
			conn:         &fakeConn{tx: &fakeTx{execErrAt: 1, execErr: &pgconn.PgError{Code: "42501", Message: "permission denied"}}},
			wantTerminal: true,
			wantContains: "PostgreSQL grant execution failed",
		},
		{
			name:         "retryable exec error",
			conn:         &fakeConn{tx: &fakeTx{execErrAt: 1, execErr: &pgconn.PgError{Code: "42601", Message: "syntax error"}}},
			wantTerminal: false,
			wantContains: "PostgreSQL grant execution failed",
		},
		{
			name:         "terminal commit error",
			conn:         &fakeConn{tx: &fakeTx{commitErr: &pgconn.PgError{Code: "28000", Message: "invalid authorization specification"}}},
			wantTerminal: true,
			wantContains: "PostgreSQL grant transaction commit failed",
		},
		{
			name:         "retryable commit error",
			conn:         &fakeConn{tx: &fakeTx{commitErr: errors.New("connection reset")}},
			wantTerminal: false,
			wantContains: "PostgreSQL grant transaction commit failed",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			repo := &pgDBRepository{conn: tst.conn}

			err := repo.AssignRequiredPermissionsToRole(context.Background(), "appdb", ports.DatabaseRoleNames{Admin: "appdb_admin", RW: "appdb_rw"})

			require.Error(t, err)
			assert.Equal(t, tst.wantTerminal, errors.Is(err, ports.ErrDBRepoTerminal))
			assert.Contains(t, err.Error(), tst.wantContains)
			assert.NotContains(t, err.Error(), "password authentication failed")
			assert.True(t, tst.conn.closed)
			if tst.conn.tx != nil {
				assert.Equal(t, 1, tst.conn.tx.rollbackCalls, "every post-begin failure must attempt rollback")
			}
		})
	}
}

func TestPGDBRepositoryAssignRequiredPermissionsToRoleExecFailureRollsBack(t *testing.T) {
	tx := &fakeTx{execErrAt: 2, execErr: errors.New("grant failed")}
	conn := &fakeConn{tx: tx}
	repo := &pgDBRepository{conn: conn}

	err := repo.AssignRequiredPermissionsToRole(context.Background(), "appdb", ports.DatabaseRoleNames{Admin: "appdb_admin", RW: "appdb_rw"})

	require.Error(t, err)
	assert.False(t, tx.committed)
	assert.True(t, tx.rolledBack)
	assert.Equal(t, 1, tx.rollbackCalls)
	assert.True(t, conn.closed)
}

func TestPGDBRepositoryAssignRequiredPermissionsToRoleRollsBackWithFreshBoundedContextAfterCancellation(t *testing.T) {
	operationCtx, cancel := context.WithCancel(context.Background())
	tx := &fakeTx{execErrAt: 1, execErr: errors.New("grant failed")}
	tx.onExec = cancel
	conn := &fakeConn{tx: tx}

	err := (&pgDBRepository{conn: conn}).AssignRequiredPermissionsToRole(operationCtx, "appdb", ports.DatabaseRoleNames{Admin: "appdb_admin", RW: "appdb_rw"})

	require.ErrorContains(t, err, "PostgreSQL grant execution failed")
	require.ErrorIs(t, operationCtx.Err(), context.Canceled)
	assert.True(t, tx.rolledBack)
	require.NotNil(t, tx.rollbackCtx)
	assert.NotEqual(t, operationCtx, tx.rollbackCtx)
	assert.NoError(t, tx.rollbackCtxErr, "rollback must not inherit the cancelled operation context")
	assert.True(t, tx.rollbackHasDeadline, "rollback context must be bounded")
}

func TestPGDBRepositoryClosesWithFreshBoundedContext(t *testing.T) {
	tests := []struct {
		name string
		run  func(*pgDBRepository, context.Context) error
		conn *fakeConn
	}{
		{
			name: "grant execution",
			conn: &fakeConn{tx: &fakeTx{}},
			run: func(repo *pgDBRepository, ctx context.Context) error {
				return repo.AssignRequiredPermissionsToRole(ctx, "appdb", ports.DatabaseRoleNames{Admin: "appdb_admin", RW: "appdb_rw"})
			},
		},
		{
			name: "post restore role sweeping",
			conn: &fakeConn{tx: &fakeTx{}, rows: &fakeRows{}},
			run: func(repo *pgDBRepository, ctx context.Context) error {
				_, err := repo.SweepUnmanagedRolesAfterRestore(ctx)
				return err
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			operationCtx, cancel := context.WithCancel(context.Background())
			cancel()
			started := time.Now()

			err := tst.run(&pgDBRepository{conn: tst.conn}, operationCtx)

			require.NoError(t, err)
			require.NotNil(t, tst.conn.closeCtx)
			assert.NotEqual(t, operationCtx, tst.conn.closeCtx)
			assert.NoError(t, tst.conn.closeCtxErr, "close must not inherit a cancelled operation context")
			require.True(t, tst.conn.closeHasDeadline, "close context must be bounded")
			assert.WithinDuration(t, started.Add(dbCloseTimeout), tst.conn.closeDeadline, time.Second)
		})
	}
}

func TestPGDBRepositoryCloseFailureIsLoggedWithoutMaskingPrimaryError(t *testing.T) {
	var logs bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
	conn := &fakeConn{
		beginErr: errors.New("primary operation failure"),
		closeErr: errors.New("close failure must not escape"),
	}

	err := (&pgDBRepository{conn: conn}).AssignRequiredPermissionsToRole(ctx, "appdb", ports.DatabaseRoleNames{Admin: "appdb_admin", RW: "appdb_rw"})

	require.Error(t, err)
	assert.ErrorContains(t, err, "PostgreSQL grant transaction begin failed")
	assert.NotContains(t, err.Error(), "close failure")
	assert.Contains(t, logs.String(), "PostgreSQL connection close failed")
	assert.Contains(t, logs.String(), "error_category=connection_close_failed")
	assert.NotContains(t, logs.String(), "close failure must not escape")
}

func TestPGDBRepositorySweepCloseFailureIsLoggedWithoutMaskingSuccess(t *testing.T) {
	var logs bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&logs, nil)))
	conn := &fakeConn{
		tx:       &fakeTx{},
		rows:     &fakeRows{},
		closeErr: errors.New("sweep close failure must not escape"),
	}

	rolesSwept, err := (&pgDBRepository{conn: conn}).SweepUnmanagedRolesAfterRestore(ctx)

	require.NoError(t, err)
	assert.Zero(t, rolesSwept)
	assert.True(t, conn.closed)
	assert.Contains(t, logs.String(), "PostgreSQL connection close failed")
	assert.Contains(t, logs.String(), "error_category=connection_close_failed")
	assert.NotContains(t, logs.String(), "sweep close failure must not escape")
}

func TestNewDBRepository(t *testing.T) {
	original := connectToPostgres
	t.Cleanup(func() { connectToPostgres = original })

	t.Run("returns repository on success", func(t *testing.T) {
		connectToPostgres = func(_ context.Context, host, dbName, password string) (postgres.Conn, error) {
			assert.Equal(t, "postgres.example.com", host)
			assert.Equal(t, "appdb", dbName)
			assert.Equal(t, "secret", password)
			return &fakeConn{}, nil
		}

		repo, err := NewDBRepository(context.Background(), "postgres.example.com", "appdb", "secret")

		require.NoError(t, err)
		require.NotNil(t, repo)
	})

	t.Run("wraps terminal connect error", func(t *testing.T) {
		connectErr := &pgconn.PgError{Code: "28P01", Message: "password authentication failed: supersecret"}
		connectToPostgres = func(_ context.Context, _, _, _ string) (postgres.Conn, error) {
			return nil, connectErr
		}

		repo, err := NewDBRepository(context.Background(), "postgres.example.com", "appdb", "secret")

		require.Error(t, err)
		assert.Nil(t, repo)
		assert.ErrorIs(t, err, ports.ErrDBRepoTerminal)
		assert.NotErrorIs(t, err, connectErr)
		assert.Contains(t, err.Error(), "PostgreSQL connection failed")
		assert.NotContains(t, err.Error(), "supersecret")
	})

	t.Run("returns retryable connect error", func(t *testing.T) {
		connectErr := errors.New("connection reset: supersecret")
		connectToPostgres = func(_ context.Context, _, _, _ string) (postgres.Conn, error) {
			return nil, connectErr
		}

		repo, err := NewDBRepository(context.Background(), "postgres.example.com", "appdb", "secret")

		require.Error(t, err)
		assert.Nil(t, repo)
		assert.NotErrorIs(t, err, ports.ErrDBRepoTerminal)
		assert.NotErrorIs(t, err, connectErr)
		assert.Contains(t, err.Error(), "PostgreSQL connection failed")
		assert.NotContains(t, err.Error(), "supersecret")
	})
}

func TestNewRoleSweeper(t *testing.T) {
	original := connectToPostgres
	t.Cleanup(func() { connectToPostgres = original })

	t.Run("returns sweeper", func(t *testing.T) {
		connectToPostgres = func(_ context.Context, _, _, _ string) (postgres.Conn, error) {
			return &fakeConn{}, nil
		}

		sweeper, err := NewRoleSweeper(context.Background(), "postgres.example.com", "appdb", "secret")

		require.NoError(t, err)
		require.NotNil(t, sweeper)
	})

	t.Run("wraps terminal connect error as port sentinel", func(t *testing.T) {
		connectErr := &pgconn.PgError{Code: "28P01", Message: "password authentication failed: supersecret"}
		connectToPostgres = func(_ context.Context, _, _, _ string) (postgres.Conn, error) {
			return nil, connectErr
		}

		sweeper, err := NewRoleSweeper(context.Background(), "postgres.example.com", "appdb", "secret")

		require.Error(t, err)
		assert.Nil(t, sweeper)
		assert.ErrorIs(t, err, ports.ErrSweeperConnectTerminal)
		assert.NotErrorIs(t, err, connectErr)
		assert.NotContains(t, err.Error(), "supersecret")
	})

	t.Run("returns retryable connect error unwrapped", func(t *testing.T) {
		connectErr := errors.New("connection reset: supersecret")
		connectToPostgres = func(_ context.Context, _, _, _ string) (postgres.Conn, error) {
			return nil, connectErr
		}

		sweeper, err := NewRoleSweeper(context.Background(), "postgres.example.com", "appdb", "secret")

		require.Error(t, err)
		assert.Nil(t, sweeper)
		assert.NotErrorIs(t, err, ports.ErrSweeperConnectTerminal)
		assert.NotErrorIs(t, err, connectErr)
		assert.NotContains(t, err.Error(), "supersecret")
	})
}

func TestSweepUnmanagedRolesAfterRestoreSuccess(t *testing.T) {
	tx := &fakeTx{}
	conn := &fakeConn{
		tx:   tx,
		rows: &fakeRows{names: []string{"app_user", "reporting"}},
	}
	repo := &pgDBRepository{conn: conn}

	rolesSwept, err := repo.SweepUnmanagedRolesAfterRestore(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 2, rolesSwept)
	assert.True(t, conn.closed)
	assert.True(t, conn.rows.closed, "rows must be closed before the transaction begins")
	assert.Equal(t, 1, conn.beginCall)
	assert.True(t, tx.committed)
	assert.Equal(t, []string{
		`ALTER ROLE "app_user" NOLOGIN PASSWORD NULL`,
		`ALTER ROLE "reporting" NOLOGIN PASSWORD NULL`,
	}, tx.stmts)
}

func TestSweepUnmanagedRolesAfterRestoreNoRoles(t *testing.T) {
	tx := &fakeTx{}
	conn := &fakeConn{tx: tx, rows: &fakeRows{}}
	repo := &pgDBRepository{conn: conn}

	rolesSwept, err := repo.SweepUnmanagedRolesAfterRestore(context.Background())

	require.NoError(t, err)
	assert.Zero(t, rolesSwept)
	assert.True(t, tx.committed)
	assert.Empty(t, tx.stmts)
}

func TestSweepUnmanagedRolesAfterRestoreErrors(t *testing.T) {
	t.Run("query error", func(t *testing.T) {
		conn := &fakeConn{queryErr: errors.New("boom")}
		repo := &pgDBRepository{conn: conn}

		_, err := repo.SweepUnmanagedRolesAfterRestore(context.Background())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "PostgreSQL role sweep query failed")
		assert.Equal(t, 0, conn.beginCall, "no transaction should start when the query fails")
		assert.True(t, conn.closed)
	})

	t.Run("exec error aborts the batch", func(t *testing.T) {
		tx := &fakeTx{execErrAt: 1, execErr: errors.New("permission denied")}
		conn := &fakeConn{tx: tx, rows: &fakeRows{names: []string{"app_user", "reporting"}}}
		repo := &pgDBRepository{conn: conn}

		_, err := repo.SweepUnmanagedRolesAfterRestore(context.Background())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "PostgreSQL role sweep execution failed")
		assert.False(t, tx.committed, "a failed role disable must not commit a half-swept batch")
		assert.True(t, tx.rolledBack, "a failed role disable must explicitly roll back the batch")
		assert.True(t, conn.closed)
	})

	t.Run("commit error", func(t *testing.T) {
		tx := &fakeTx{commitErr: errors.New("connection reset")}
		conn := &fakeConn{tx: tx, rows: &fakeRows{names: []string{"app_user"}}}
		repo := &pgDBRepository{conn: conn}

		_, err := repo.SweepUnmanagedRolesAfterRestore(context.Background())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "PostgreSQL role sweep transaction commit failed")
		assert.True(t, conn.closed)
	})
}
