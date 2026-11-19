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

func TestSubscriptionRetirementSQLAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable superuser database")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	defer admin.Close(context.WithoutCancel(t.Context()))
	suffix := time.Now().UnixNano()
	database := fmt.Sprintf("bg_retire_%d", suffix)
	role := fmt.Sprintf("bg_retire_role_%d", suffix)
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pginfra.QuoteIdentifier(database))
	require.NoError(t, err)
	_, err = admin.Exec(t.Context(), "CREATE ROLE "+pginfra.QuoteIdentifier(role))
	require.NoError(t, err)
	defer func() {
		_, _ = admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+pginfra.QuoteIdentifier(database)+" WITH (FORCE)")
		_, _ = admin.Exec(context.WithoutCancel(t.Context()), "DROP ROLE "+pginfra.QuoteIdentifier(role))
	}()
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.Database = database
	raw, err := pgx.ConnectConfig(t.Context(), config)
	require.NoError(t, err)
	defer raw.Close(context.WithoutCancel(t.Context()))
	conn := livePreflightSQLConn{Conn: raw}
	id := cutoverIdentity{Role: role, AttemptID: "attempt", LogicalName: "logical"}
	_, err = raw.Exec(t.Context(), "CREATE SCHEMA "+pginfra.QuoteIdentifier(candidateGuardSchema)+" AUTHORIZATION postgres")
	require.NoError(t, err)
	helper := retireSubscriptionHelper(id, database)
	require.NoError(t, installCutoverHelper(t.Context(), conn, helper, id))
	_, err = raw.Exec(t.Context(), "GRANT USAGE ON SCHEMA "+pginfra.QuoteIdentifier(candidateGuardSchema)+" TO "+pginfra.QuoteIdentifier(role))
	require.NoError(t, err)
	sub := subscriptionName(id.LogicalName, database)
	pub := publicationName(id.LogicalName, database)
	slot := replicationSlotName(id.LogicalName, database)
	statement := "CREATE SUBSCRIPTION " + pginfra.QuoteIdentifier(sub) + " CONNECTION 'host=127.0.0.1 port=1 user=nobody dbname=postgres connect_timeout=1' PUBLICATION " + pginfra.QuoteIdentifier(pub) + " WITH (connect=false,create_slot=false,enabled=false,slot_name=" + quoteSQLStringLiteral(slot) + ")"
	_, err = raw.Exec(t.Context(), statement)
	require.NoError(t, err)
	_, err = raw.Exec(t.Context(), "ALTER SUBSCRIPTION "+pginfra.QuoteIdentifier(sub)+" ENABLE")
	require.NoError(t, err)
	for _, test := range []struct {
		name, assignment, restore string
	}{
		{"owner", "subowner=(SELECT oid FROM pg_roles WHERE rolname=" + quoteSQLStringLiteral(role) + ")", "subowner=(SELECT oid FROM pg_roles WHERE rolname='postgres')"},
		{"slot", "subslotname='another_slot'", "subslotname=" + quoteSQLStringLiteral(slot)},
		{"publication", "subpublications=ARRAY['another_publication']::text[]", "subpublications=ARRAY[" + quoteSQLStringLiteral(pub) + "]::text[]"},
	} {
		t.Run("rejects mismatched "+test.name, func(t *testing.T) {
			_, err := raw.Exec(t.Context(), "UPDATE pg_subscription SET "+test.assignment+" WHERE subname=$1", sub)
			require.NoError(t, err)
			_, err = raw.Exec(t.Context(), "SET SESSION AUTHORIZATION "+pginfra.QuoteIdentifier(role))
			require.NoError(t, err)
			defer func() {
				_, err := raw.Exec(context.WithoutCancel(t.Context()), "RESET SESSION AUTHORIZATION")
				require.NoError(t, err)
				_, err = raw.Exec(context.WithoutCancel(t.Context()), "UPDATE pg_subscription SET "+test.restore+" WHERE subname=$1", sub)
				require.NoError(t, err)
			}()
			var done bool
			require.ErrorContains(t, queryOne(t.Context(), conn, &done, controlHelperCallSQL(helper.name)), "subscription identity changed")
			var enabled bool
			require.NoError(t, raw.QueryRow(t.Context(), "SELECT subenabled FROM pg_subscription WHERE subname=$1", sub).Scan(&enabled))
			assert.True(t, enabled, "a rejected identity must remain present and enabled")
		})
	}
	// The privileged function accepts only its recorded session identity.
	var done bool
	require.Error(t, queryOne(t.Context(), conn, &done, controlHelperCallSQL(helper.name)))
	_, err = raw.Exec(t.Context(), "SET SESSION AUTHORIZATION "+pginfra.QuoteIdentifier(role))
	require.NoError(t, err)
	require.Error(t, func() error {
		_, err := raw.Exec(t.Context(), "ALTER SUBSCRIPTION "+pginfra.QuoteIdentifier(sub)+" DISABLE")
		return err
	}())
	for range 20 {
		require.NoError(t, queryOne(t.Context(), conn, &done, controlHelperCallSQL(helper.name)))
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, done, "staged disable, worker stop, detach, and drop must converge")
	require.NoError(t, queryOne(t.Context(), conn, &done, controlHelperCallSQL(helper.name)))
	assert.True(t, done, "an absent subscription is idempotent")
	_, err = raw.Exec(t.Context(), "RESET SESSION AUTHORIZATION")
	require.NoError(t, err)
	var remaining int
	require.NoError(t, raw.QueryRow(t.Context(), "SELECT count(*) FROM pg_subscription WHERE subname=$1", sub).Scan(&remaining))
	assert.Zero(t, remaining)
}
