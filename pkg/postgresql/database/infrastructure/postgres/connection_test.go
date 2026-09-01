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

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnect(t *testing.T) {
	original := connectConfig
	t.Cleanup(func() { connectConfig = original })

	t.Run("builds the expected superuser connection config", func(t *testing.T) {
		var gotConfig *pgx.ConnConfig
		connectConfig = func(_ context.Context, cfg *pgx.ConnConfig) (*pgx.Conn, error) {
			gotConfig = cfg
			return nil, nil
		}

		conn, err := Connect(context.Background(), "postgres.example.com", "appdb", "secret")

		require.NoError(t, err)
		require.NotNil(t, conn)
		require.NotNil(t, gotConfig)
		assert.Equal(t, SuperUsername, gotConfig.User)
		assert.Equal(t, "postgres.example.com", gotConfig.Host)
		assert.Equal(t, uint16(5432), gotConfig.Port)
		assert.Equal(t, "appdb", gotConfig.Database)
		assert.Equal(t, "secret", gotConfig.Password)
	})

	t.Run("returns the dial error unwrapped", func(t *testing.T) {
		dialErr := errors.New("connection reset")
		connectConfig = func(_ context.Context, _ *pgx.ConnConfig) (*pgx.Conn, error) {
			return nil, dialErr
		}

		conn, err := Connect(context.Background(), "postgres.example.com", "appdb", "secret")

		require.ErrorIs(t, err, dialErr)
		assert.Nil(t, conn)
	})
}

func TestQuoteIdentifier(t *testing.T) {
	assert.Equal(t, `"appdb"`, QuoteIdentifier("appdb"))
	assert.Equal(t, `"app""db"`, QuoteIdentifier(`app"db`))
}

func TestIsTerminalError(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantTerminal bool
	}{
		{name: "nil", err: nil, wantTerminal: false},
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
			assert.Equal(t, tst.wantTerminal, IsTerminalError(tst.err))
		})
	}
}
