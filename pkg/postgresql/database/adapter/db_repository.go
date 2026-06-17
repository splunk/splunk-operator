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
// Package adapter contains driven adapters for the PostgresDatabase domain.
// Each adapter implements a port defined in core/ports.go.
package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	dbcore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	_ dbcore.DBRepo     = (*pgDBRepository)(nil)
	_ ports.RoleSweeper = (*pgDBRepository)(nil)
)

const (
	superUsername    = "postgres"
	postgresPort     = "5432"
	dbConnectTimeout = 10 * time.Second

	pgCodeClassInvalidAuthorizationSpecification = "28"
	pgCodeInsufficientPrivilege                  = "42501"
)

var pgxConnectConfig = pgx.ConnectConfig

type dbConn interface {
	begin(ctx context.Context) (grantTx, error)
	close(ctx context.Context) error
	query(ctx context.Context, sql string, args ...any) (pgRows, error)
	exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type pgRows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

type grantTx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Commit(ctx context.Context) error
}

type pgxDBConn struct {
	conn *pgx.Conn
}

func (c pgxDBConn) begin(ctx context.Context) (grantTx, error) {
	return c.conn.Begin(ctx)
}

func (c pgxDBConn) close(ctx context.Context) error {
	return c.conn.Close(ctx)
}

func (c pgxDBConn) query(ctx context.Context, sql string, args ...any) (pgRows, error) {
	return c.conn.Query(ctx, sql, args...)
}

func (c pgxDBConn) exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return c.conn.Exec(ctx, sql, args...)
}

// pgDBRepository is the pgx-backed adapter for the core.DBRepo port.
// It owns the full connection lifecycle: open on construction, close on ExecGrants return.
type pgDBRepository struct {
	conn dbConn
}

// ExecGrants applies all privilege grants needed for the RW role on a single database.
// GRANT ON ALL TABLES/SEQUENCES covers existing objects; ALTER DEFAULT PRIVILEGES covers
// future ones created by the admin role (e.g. via migrations).
func (r *pgDBRepository) ExecGrants(ctx context.Context, dbName string) error {
	defer r.conn.close(context.Background())

	adminRole := dbName + "_admin"
	rwRole := dbName + "_rw"

	tx, err := r.conn.begin(ctx)
	if err != nil {
		if isTerminalPostgresError(err) {
			return fmt.Errorf("%w: beginning transaction: %w", dbcore.ErrTerminal, err)
		}
		return fmt.Errorf("beginning transaction: %w", err)
	}

	// SQL identifiers cannot be parameterised; dbName is quoted and escaped defensively.
	// Role names are derived from dbName so carry the same safety guarantee.
	quotedDB := `"` + strings.ReplaceAll(dbName, `"`, `""`) + `"`
	stmts := []string{
		fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", quotedDB, rwRole),
		fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", rwRole),
		fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %s", rwRole),
		fmt.Sprintf("GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %s", rwRole),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s", adminRole, rwRole),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %s", adminRole, rwRole),
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			if isTerminalPostgresError(err) {
				return fmt.Errorf("%w: executing grant %q: %w", dbcore.ErrTerminal, stmt, err)
			}
			return fmt.Errorf("executing grant %q: %w", stmt, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		if isTerminalPostgresError(err) {
			return fmt.Errorf("%w: committing grants: %w", dbcore.ErrTerminal, err)
		}
		return fmt.Errorf("committing grants: %w", err)
	}
	return nil
}

// sweepRolesQuery selects all login roles that should have login disabled after recovery.
// Excluded by explicit name:
//   - postgres — superuser managed by CNPG
//   - streaming_replica — replication agent managed by CNPG
//   - cnpg_pooler_pgbouncer — PgBouncer authentication role managed by CNPG pooler controller
//   - pg\_% (escaped) — PostgreSQL built-in system roles (pg_monitor, pg_read_all_data, etc.)
//
// User-created replication roles are intentionally NOT excluded — they carry restored password
// hashes and must be swept just like any other application role.
const sweepRolesQuery = `
SELECT rolname FROM pg_roles
WHERE rolcanlogin = true
  AND rolname NOT IN ('postgres', 'streaming_replica', 'cnpg_pooler_pgbouncer')
  AND rolname NOT LIKE 'pg\_%' ESCAPE '\'`

// SweepUnmanagedRolesAfterRestore disables login for all non-system login roles after recovery.
// All roles are disabled — including managed ones. The ManagedRoles reconciler runs
// immediately after and re-enables managed roles with fresh credentials.
// CNPG-owned roles (streaming_replica, cnpg_pooler_pgbouncer) are preserved.
func (r *pgDBRepository) SweepUnmanagedRolesAfterRestore(ctx context.Context) error {
	// Close on its own background context: if ctx is already cancelled/timed-out (the very
	// situations where cleanup matters most), passing it here would abort the close itself.
	defer r.conn.close(context.Background())

	rows, err := r.conn.query(ctx, sweepRolesQuery)
	if err != nil {
		return fmt.Errorf("querying pg_roles: %w", err)
	}

	var toDisable []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("scanning role name: %w", err)
		}
		toDisable = append(toDisable, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterating pg_roles: %w", err)
	}
	// Release the rows before opening the transaction — pgx allows only one active
	// operation per connection at a time.
	rows.Close()

	// Disabling the roles in a single transaction keeps the sweep atomic: a failure on any
	// role rolls back the whole batch rather than leaving the cluster half-swept (some roles
	// disabled, some still carrying restored credentials).
	tx, err := r.conn.begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning sweep transaction: %w", err)
	}

	for _, name := range toDisable {
		// PASSWORD NULL is intentional: it wipes the password hash restored from the source
		// cluster, so a stale credential can never authenticate again even if the role is later
		// re-enabled with LOGIN. NOLOGIN alone would leave that hash in pg_authid. The operator
		// re-provisions managed roles with fresh secrets immediately after.
		// Identifiers cannot be parameterised — name comes from pg_roles, not user input.
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER ROLE %s NOLOGIN PASSWORD NULL", pgx.Identifier{name}.Sanitize())); err != nil {
			return fmt.Errorf("disabling role %s: %w", name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing sweep: %w", err)
	}
	return nil
}

// openRepository opens a direct superuser connection and wraps it in the pgx adapter.
// Terminal connect failures (bad credentials, insufficient privilege) are wrapped with
// dbcore.ErrTerminal so callers can stop retrying.
func openRepository(ctx context.Context, host, dbName, password string) (*pgDBRepository, error) {
	cfg, err := pgx.ParseConfig(fmt.Sprintf(
		"postgres://%s@%s:%s/%s?sslmode=require&connect_timeout=%d",
		superUsername, host, postgresPort, dbName,
		int(dbConnectTimeout.Seconds()),
	))
	if err != nil {
		return nil, fmt.Errorf("parsing connection config for %s/%s: %w", host, dbName, err)
	}
	cfg.Password = password

	conn, err := pgxConnectConfig(ctx, cfg)
	if err != nil {
		if isTerminalPostgresError(err) {
			return nil, fmt.Errorf("%w: connecting to %s/%s: %w", dbcore.ErrTerminal, host, dbName, err)
		}
		return nil, fmt.Errorf("connecting to %s/%s: %w", host, dbName, err)
	}
	return &pgDBRepository{conn: pgxDBConn{conn: conn}}, nil
}

// NewDBRepository opens a direct superuser connection for database grant reconciliation.
func NewDBRepository(ctx context.Context, host, dbName, password string) (dbcore.DBRepo, error) {
	return openRepository(ctx, host, dbName, password)
}

// NewRoleSweeper opens a direct superuser connection for post-restore credential sweeping.
// It translates the adapter's terminal-connect signal onto the port sentinel so the cluster
// domain surfaces Failed without importing database/core.
func NewRoleSweeper(ctx context.Context, host, dbName, password string) (ports.RoleSweeper, error) {
	repo, err := openRepository(ctx, host, dbName, password)
	if err != nil {
		if errors.Is(err, dbcore.ErrTerminal) {
			return nil, fmt.Errorf("%w: %w", ports.ErrSweeperConnectTerminal, err)
		}
		return nil, err
	}
	return repo, nil
}

func isTerminalPostgresError(err error) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return isTerminalPGCode(pgErr.Code)
	}
	return false
}

func isTerminalPGCode(code string) bool {
	switch {
	case strings.HasPrefix(code, pgCodeClassInvalidAuthorizationSpecification):
		return true
	case code == pgCodeInsufficientPrivilege:
		return true
	default:
		return false
	}
}
