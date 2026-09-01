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
// Each adapter implements a port defined in pkg/postgresql/shared/ports,
// built on top of the pgx-backed primitives in
// pkg/postgresql/database/infrastructure/postgres.
package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/splunk/splunk-operator/pkg/logging"
	"github.com/splunk/splunk-operator/pkg/postgresql/database/infrastructure/postgres"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
)

var (
	_ ports.DBRepo      = (*pgDBRepository)(nil)
	_ ports.RoleSweeper = (*pgDBRepository)(nil)
)

const (
	dbCloseTimeout = 10 * time.Second
)

// connectToPostgres is a seam for tests to stub the infra connection dial.
var connectToPostgres = postgres.Connect

// pgDBRepository is the DBRepo/RoleSweeper adapter. It owns the full connection
// lifecycle: open on construction, close on return from the calling method.
type pgDBRepository struct {
	conn postgres.Conn
}

// closeConnection closes a repository-owned connection with a fresh bounded context.
// Cleanup failures are logged but never replace the primary operation result.
func (r *pgDBRepository) closeConnection(logCtx context.Context) {
	closeCtx, cancel := context.WithTimeout(context.Background(), dbCloseTimeout)
	defer cancel()

	if err := r.conn.Close(closeCtx); err != nil {
		logging.FromContext(logCtx).WarnContext(logCtx,
			"PostgreSQL connection close failed",
			"error_category", "connection_close_failed",
		)
	}
}

// rollbackTransaction explicitly aborts a transaction after Begin with a fresh,
// bounded context. ErrTxAlreadyClosed is the expected result when Commit already
// succeeded; other cleanup failures are logged without masking the primary
// operation result.
func rollbackTransaction(logCtx context.Context, tx postgres.Tx) {
	rollbackCtx, cancel := context.WithTimeout(context.Background(), dbCloseTimeout)
	defer cancel()

	if err := tx.Rollback(rollbackCtx); err != nil && !errors.Is(err, postgres.ErrTxAlreadyClosed) {
		logging.FromContext(logCtx).WarnContext(logCtx,
			"PostgreSQL transaction rollback failed",
			"error_category", "transaction_rollback_failed",
		)
	}
}

// safePostgresOperationError keeps driver errors inside the adapter. Driver error text can include
// connection details, so callers receive only a safe operation message while terminal failures
// retain the existing sentinel used for reconciliation classification.
func safePostgresOperationError(operation string, err error) error {
	if postgres.IsTerminalError(err) {
		return fmt.Errorf("%w: PostgreSQL %s failed", ports.ErrDBRepoTerminal, operation)
	}
	return fmt.Errorf("PostgreSQL %s failed", operation)
}

// AssignRequiredPermissionsToRole applies all privilege grants needed for the RW role on a
// single database. GRANT ON ALL TABLES/SEQUENCES covers existing objects; ALTER DEFAULT
// PRIVILEGES covers future ones created by the admin role (e.g. via migrations).
func (r *pgDBRepository) AssignRequiredPermissionsToRole(ctx context.Context, dbName string, roles ports.DatabaseRoleNames) error {
	defer r.closeConnection(ctx)

	tx, err := r.conn.Begin(ctx)
	if err != nil {
		return safePostgresOperationError("grant transaction begin", err)
	}
	defer rollbackTransaction(ctx, tx)

	// SQL identifiers cannot be parameterised. Quote every caller-supplied
	// identifier defensively, including custom role names.
	quotedDB := postgres.QuoteIdentifier(dbName)
	quotedAdminRole := postgres.QuoteIdentifier(roles.Admin)
	quotedRWRole := postgres.QuoteIdentifier(roles.RW)
	stmts := []string{
		fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", quotedDB, quotedRWRole),
		fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", quotedRWRole),
		fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %s", quotedRWRole),
		fmt.Sprintf("GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %s", quotedRWRole),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s", quotedAdminRole, quotedRWRole),
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %s", quotedAdminRole, quotedRWRole),
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return safePostgresOperationError("grant execution", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return safePostgresOperationError("grant transaction commit", err)
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
func (r *pgDBRepository) SweepUnmanagedRolesAfterRestore(ctx context.Context) (int, error) {
	defer r.closeConnection(ctx)

	rows, err := r.conn.Query(ctx, sweepRolesQuery)
	if err != nil {
		return 0, safePostgresOperationError("role sweep query", err)
	}

	var toDisable []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return 0, safePostgresOperationError("role sweep query", err)
		}
		toDisable = append(toDisable, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, safePostgresOperationError("role sweep query", err)
	}
	// Release the rows before opening the transaction — pgx allows only one active
	// operation per connection at a time.
	rows.Close()

	// Disabling the roles in a single transaction keeps the sweep atomic: a failure on any
	// role rolls back the whole batch rather than leaving the cluster half-swept (some roles
	// disabled, some still carrying restored credentials).
	tx, err := r.conn.Begin(ctx)
	if err != nil {
		return 0, safePostgresOperationError("role sweep transaction begin", err)
	}
	defer rollbackTransaction(ctx, tx)

	for _, name := range toDisable {
		// PASSWORD NULL is intentional: it wipes the password hash restored from the source
		// cluster, so a stale credential can never authenticate again even if the role is later
		// re-enabled with LOGIN. NOLOGIN alone would leave that hash in pg_authid. The operator
		// re-provisions managed roles with fresh secrets immediately after.
		// Identifiers cannot be parameterised — name comes from pg_roles, not user input.
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER ROLE %s NOLOGIN PASSWORD NULL", postgres.QuoteIdentifier(name))); err != nil {
			return 0, safePostgresOperationError("role sweep execution", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, safePostgresOperationError("role sweep transaction commit", err)
	}
	return len(toDisable), nil
}

// openRepository opens a direct superuser connection and wraps it in the pgx adapter.
// Terminal connect failures (bad credentials, insufficient privilege) are wrapped with
// ports.ErrDBRepoTerminal so callers can stop retrying.
func openRepository(ctx context.Context, host, dbName, password string) (*pgDBRepository, error) {
	conn, err := connectToPostgres(ctx, host, dbName, password)
	if err != nil {
		return nil, safePostgresOperationError("connection", err)
	}
	return &pgDBRepository{conn: conn}, nil
}

// NewDBRepository opens a direct superuser connection for database grant reconciliation.
func NewDBRepository(ctx context.Context, host, dbName, password string) (ports.DBRepo, error) {
	return openRepository(ctx, host, dbName, password)
}

// NewRoleSweeper opens a direct superuser connection for post-restore credential sweeping.
// It translates the adapter's terminal-connect signal onto the port sentinel so the cluster
// domain surfaces Failed without importing database/core.
func NewRoleSweeper(ctx context.Context, host, dbName, password string) (ports.RoleSweeper, error) {
	repo, err := openRepository(ctx, host, dbName, password)
	if err != nil {
		if errors.Is(err, ports.ErrDBRepoTerminal) {
			return nil, fmt.Errorf("%w: %w", ports.ErrSweeperConnectTerminal, err)
		}
		return nil, err
	}
	return repo, nil
}
