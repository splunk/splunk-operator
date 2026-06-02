package adapter

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	dbcore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDBConn struct {
	tx        *fakeGrantTx
	beginErr  error
	closeErr  error
	closed    bool
	beginCall int
}

func (c *fakeDBConn) begin(ctx context.Context) (grantTx, error) {
	c.beginCall++
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	return c.tx, nil
}

func (c *fakeDBConn) close(ctx context.Context) error {
	c.closed = true
	return c.closeErr
}

type fakeGrantTx struct {
	execErrAt int
	execErr   error
	commitErr error
	stmts     []string
	committed bool
}

func (tx *fakeGrantTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tx.stmts = append(tx.stmts, sql)
	if tx.execErrAt == len(tx.stmts) {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.CommandTag{}, nil
}

func (tx *fakeGrantTx) Commit(ctx context.Context) error {
	tx.committed = true
	return tx.commitErr
}

func TestPGDBRepositoryExecGrantsSuccess(t *testing.T) {
	tx := &fakeGrantTx{}
	conn := &fakeDBConn{tx: tx}
	repo := &pgDBRepository{conn: conn}

	err := repo.ExecGrants(context.Background(), "appdb")

	require.NoError(t, err)
	assert.True(t, conn.closed)
	assert.Equal(t, 1, conn.beginCall)
	assert.True(t, tx.committed)
	assert.Equal(t, []string{
		`GRANT CONNECT ON DATABASE "appdb" TO appdb_rw`,
		`GRANT USAGE ON SCHEMA public TO appdb_rw`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO appdb_rw`,
		`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO appdb_rw`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE appdb_admin IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO appdb_rw`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE appdb_admin IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO appdb_rw`,
	}, tx.stmts)
}

func TestPGDBRepositoryExecGrantsEscapesDatabaseName(t *testing.T) {
	tx := &fakeGrantTx{}
	conn := &fakeDBConn{tx: tx}
	repo := &pgDBRepository{conn: conn}

	err := repo.ExecGrants(context.Background(), `app"db`)

	require.NoError(t, err)
	require.NotEmpty(t, tx.stmts)
	assert.Equal(t, `GRANT CONNECT ON DATABASE "app""db" TO app"db_rw`, tx.stmts[0])
}

func TestPGDBRepositoryExecGrantsErrors(t *testing.T) {
	tests := []struct {
		name         string
		conn         *fakeDBConn
		wantTerminal bool
		wantContains string
	}{
		{
			name:         "terminal begin error",
			conn:         &fakeDBConn{beginErr: &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}},
			wantTerminal: true,
			wantContains: "beginning transaction",
		},
		{
			name:         "retryable begin error",
			conn:         &fakeDBConn{beginErr: &pgconn.PgError{Code: "08006", Message: "connection failure"}},
			wantTerminal: false,
			wantContains: "beginning transaction",
		},
		{
			name:         "terminal exec error",
			conn:         &fakeDBConn{tx: &fakeGrantTx{execErrAt: 1, execErr: &pgconn.PgError{Code: "42501", Message: "permission denied"}}},
			wantTerminal: true,
			wantContains: "executing grant",
		},
		{
			name:         "retryable exec error",
			conn:         &fakeDBConn{tx: &fakeGrantTx{execErrAt: 1, execErr: &pgconn.PgError{Code: "42601", Message: "syntax error"}}},
			wantTerminal: false,
			wantContains: "executing grant",
		},
		{
			name:         "terminal commit error",
			conn:         &fakeDBConn{tx: &fakeGrantTx{commitErr: &pgconn.PgError{Code: "28000", Message: "invalid authorization specification"}}},
			wantTerminal: true,
			wantContains: "committing grants",
		},
		{
			name:         "retryable commit error",
			conn:         &fakeDBConn{tx: &fakeGrantTx{commitErr: errors.New("connection reset")}},
			wantTerminal: false,
			wantContains: "committing grants",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			repo := &pgDBRepository{conn: tst.conn}

			err := repo.ExecGrants(context.Background(), "appdb")

			require.Error(t, err)
			assert.Equal(t, tst.wantTerminal, errors.Is(err, dbcore.ErrTerminal))
			assert.Contains(t, err.Error(), tst.wantContains)
			assert.True(t, tst.conn.closed)
		})
	}
}

func TestNewDBRepository(t *testing.T) {
	originalConnectConfig := pgxConnectConfig
	t.Cleanup(func() {
		pgxConnectConfig = originalConnectConfig
	})

	t.Run("returns repository with parsed config", func(t *testing.T) {
		var gotConfig *pgx.ConnConfig
		pgxConnectConfig = func(ctx context.Context, cfg *pgx.ConnConfig) (*pgx.Conn, error) {
			gotConfig = cfg
			return nil, nil
		}

		repo, err := NewDBRepository(context.Background(), "postgres.example.com", "appdb", "secret")

		require.NoError(t, err)
		require.NotNil(t, repo)
		require.NotNil(t, gotConfig)
		assert.Equal(t, superUsername, gotConfig.User)
		assert.Equal(t, "postgres.example.com", gotConfig.Host)
		assert.Equal(t, uint16(5432), gotConfig.Port)
		assert.Equal(t, "appdb", gotConfig.Database)
		assert.Equal(t, "secret", gotConfig.Password)
	})

	t.Run("wraps terminal connect error", func(t *testing.T) {
		connectErr := &pgconn.PgError{Code: "28P01", Message: "password authentication failed"}
		pgxConnectConfig = func(ctx context.Context, cfg *pgx.ConnConfig) (*pgx.Conn, error) {
			return nil, connectErr
		}

		repo, err := NewDBRepository(context.Background(), "postgres.example.com", "appdb", "secret")

		require.Error(t, err)
		assert.Nil(t, repo)
		assert.ErrorIs(t, err, dbcore.ErrTerminal)
		assert.ErrorIs(t, err, connectErr)
		assert.Contains(t, err.Error(), "connecting to postgres.example.com/appdb")
	})

	t.Run("returns retryable connect error", func(t *testing.T) {
		connectErr := errors.New("connection reset")
		pgxConnectConfig = func(ctx context.Context, cfg *pgx.ConnConfig) (*pgx.Conn, error) {
			return nil, connectErr
		}

		repo, err := NewDBRepository(context.Background(), "postgres.example.com", "appdb", "secret")

		require.Error(t, err)
		assert.Nil(t, repo)
		assert.NotErrorIs(t, err, dbcore.ErrTerminal)
		assert.ErrorIs(t, err, connectErr)
		assert.Contains(t, err.Error(), "connecting to postgres.example.com/appdb")
	})
}

func TestIsTerminalPostgresError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantTerminal bool
	}{
		{
			name:         "nil",
			err:          nil,
			wantTerminal: false,
		},
		{
			name:         "postgres auth failure",
			err:          &pgconn.PgError{Code: "28P01", Message: "password authentication failed"},
			wantTerminal: true,
		},
		{
			name:         "postgres invalid authorization",
			err:          &pgconn.PgError{Code: "28000", Message: "invalid authorization specification"},
			wantTerminal: true,
		},
		{
			name:         "postgres auth class error",
			err:          &pgconn.PgError{Code: "28XYZ", Message: "authorization failure"},
			wantTerminal: true,
		},
		{
			name:         "postgres insufficient privilege",
			err:          &pgconn.PgError{Code: "42501", Message: "permission denied"},
			wantTerminal: true,
		},
		{
			name:         "wrapped terminal postgres error",
			err:          fmt.Errorf("executing grant: %w", &pgconn.PgError{Code: "42501", Message: "permission denied"}),
			wantTerminal: true,
		},
		{
			name:         "postgres connection exception is retryable",
			err:          &pgconn.PgError{Code: "08006", Message: "connection failure"},
			wantTerminal: false,
		},
		{
			name:         "postgres syntax/access class error other than insufficient privilege is retryable",
			err:          &pgconn.PgError{Code: "42601", Message: "syntax error"},
			wantTerminal: false,
		},
		{
			name:         "postgres cannot connect now is retryable",
			err:          &pgconn.PgError{Code: "57P03", Message: "cannot connect now"},
			wantTerminal: false,
		},
		{
			name:         "plain auth text is retryable",
			err:          errors.New("password authentication failed"),
			wantTerminal: false,
		},
		{
			name:         "some other error is retryable",
			err:          errors.New("some other error"),
			wantTerminal: false,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			assert.Equal(t, tst.wantTerminal, isTerminalPostgresError(tst.err))
		})
	}
}
