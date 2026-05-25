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
	"fmt"
	"strings"
	"time"

	dbcore "github.com/splunk/splunk-operator/pkg/postgresql/database/core"

	"github.com/jackc/pgx/v5"
)

const (
	superUsername    = "postgres"
	postgresPort     = "5432"
	dbConnectTimeout = 10 * time.Second
)

// pgDBRepository is the pgx-backed adapter for the core.DBRepo port.
// It owns the full connection lifecycle: open on construction, close on ExecGrants return.
type pgDBRepository struct {
	conn *pgx.Conn
}

// ExecGrants applies all privilege grants needed for the RW role on a single database.
// GRANT ON ALL TABLES/SEQUENCES covers existing objects; ALTER DEFAULT PRIVILEGES covers
// future ones created by the admin role (e.g. via migrations).
func (r *pgDBRepository) ExecGrants(ctx context.Context, dbName string) error {
	defer r.conn.Close(context.Background())

	adminRole := dbName + "_admin"
	rwRole := dbName + "_rw"

	tx, err := r.conn.Begin(ctx)
	if err != nil {
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
			return fmt.Errorf("executing grant %q: %w", stmt, err)
		}
	}

	return tx.Commit(ctx)
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

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s/%s: %w", host, dbName, err)
	}
	return &pgDBRepository{conn: conn}, nil
}
