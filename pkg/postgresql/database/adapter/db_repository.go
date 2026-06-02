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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

// NewDBRepository opens a direct superuser connection, bypassing any pooler.
// PgBouncer in transaction mode blocks DDL; password is set on the config
// struct to avoid URL-encoding issues with special characters.
func NewDBRepository(ctx context.Context, host, dbName, password string) (dbcore.DBRepo, error) {
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
