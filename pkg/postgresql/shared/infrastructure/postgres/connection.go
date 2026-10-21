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

// Package postgres provides shared pgx connection, SQL quoting, and error
// classification primitives used by the cluster and database adapters.
package postgres

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// SuperUsername is the CNPG-managed superuser role used for direct connections.
	SuperUsername = "postgres"
	// Port is the standard PostgreSQL connection port.
	Port = "5432"
	// ConnectTimeout bounds how long opening a superuser connection may take.
	ConnectTimeout = 10 * time.Second

	pgCodeClassInvalidAuthorizationSpecification = "28"
	pgCodeInsufficientPrivilege                  = "42501"
)

// ErrTxAlreadyClosed is returned by Tx.Rollback when the transaction was already
// committed or rolled back. Callers use it to distinguish an expected no-op
// rollback-after-commit from a genuine rollback failure.
var ErrTxAlreadyClosed = pgx.ErrTxClosed

// Conn is a single superuser connection to a PostgreSQL database.
type Conn interface {
	Begin(ctx context.Context) (Tx, error)
	Close(ctx context.Context) error
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Rows is a cursor over query results.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

// Tx is an in-flight transaction. pgx.Tx already implements this signature-for-signature,
// so Conn.Begin below returns it directly with no separate wrapper type.
type Tx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type pgxConn struct {
	conn *pgx.Conn
}

func (c pgxConn) Begin(ctx context.Context) (Tx, error) {
	return c.conn.Begin(ctx)
}

func (c pgxConn) Close(ctx context.Context) error {
	return c.conn.Close(ctx)
}

func (c pgxConn) Query(ctx context.Context, sql string, args ...any) (Rows, error) {
	return c.conn.Query(ctx, sql, args...)
}

func (c pgxConn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return c.conn.Exec(ctx, sql, args...)
}

// connectConfig is a seam for tests to stub the pgx dial without a live server.
var connectConfig = pgx.ConnectConfig

// Connect opens a direct superuser connection to dbName on host.
func Connect(ctx context.Context, host, dbName, password string) (Conn, error) {
	return connect(ctx, host, dbName, password, nil)
}

// ConnectVerified opens a superuser connection authenticated by the managed
// server CA and serverName. This permits direct pod connections while checking
// the cluster's shared Service identity; blue/green control operations must
// not send the superuser password to an unauthenticated peer.
func ConnectVerified(ctx context.Context, host, dbName, password, serverName string, serverCA []byte) (Conn, error) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(serverCA) {
		return nil, fmt.Errorf("server CA is not valid PEM certificate data")
	}
	if serverName == "" {
		return nil, fmt.Errorf("verified connection server name is required")
	}
	return connect(ctx, host, dbName, password, &tls.Config{RootCAs: roots, ServerName: serverName, MinVersion: tls.VersionTLS12})
}

func connect(ctx context.Context, host, dbName, password string, verifiedTLS *tls.Config) (Conn, error) {
	cfg, err := pgx.ParseConfig(fmt.Sprintf(
		"postgres://%s@%s:%s/%s?sslmode=require&connect_timeout=%d",
		SuperUsername, host, Port, dbName,
		int(ConnectTimeout.Seconds()),
	))
	if err != nil {
		return nil, fmt.Errorf("parsing connection config for %s/%s: %w", host, dbName, err)
	}
	cfg.Password = password
	if verifiedTLS != nil {
		cfg.TLSConfig = verifiedTLS
		cfg.Fallbacks = nil
	}

	conn, err := connectConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return pgxConn{conn: conn}, nil
}

// QuoteIdentifier safely quotes a single SQL identifier (database or role name).
// SQL identifiers cannot be parameterised, so every caller-supplied identifier —
// including custom role names — must be quoted defensively before use in a
// statement.
func QuoteIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

// IsTerminalError reports whether err is a PostgreSQL error class that will not
// resolve by retrying the same operation (bad credentials, insufficient privilege).
func IsTerminalError(err error) bool {
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
