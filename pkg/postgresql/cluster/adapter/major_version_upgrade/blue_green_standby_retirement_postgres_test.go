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
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This opt-in test owns a resource-limited Docker container with two PostgreSQL
// instances. It never reads kubeconfig or connects to a deployed operator.
func TestCommittedSourceRetirementAfterPrimaryDemotion(t *testing.T) {
	image := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_HA_IMAGE")
	if image == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_HA_IMAGE to a local PostgreSQL 17+ image; the test creates its own disposable container")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	f := newStandbyRetirementFixture(t, ctx, image)
	sql, blue, status, id, _ := retainedSourceSQLFixture(t)
	const database = "retirement"
	status.DatabaseReplication = []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{DatabaseName: database}}
	require.NoError(t, sql.client.Delete(ctx, &corev1.Pod{Name: "orders-blue-3", Namespace: blue.Namespace}))
	blue.Status.CurrentPrimary = "orders-blue-1" // Intentionally stale after demotion.
	primary := f.connect(t, ctx, f.primary, "postgres", "postgres", "")
	var version string
	require.NoError(t, primary.QueryRow(ctx, "SELECT version()").Scan(&version))
	t.Log(version)
	_, err := primary.Exec(ctx, "CREATE DATABASE "+database)
	require.NoError(t, err)
	primary = f.connect(t, ctx, f.primary, database, "postgres", "")
	conn := livePreflightSQLConn{Conn: primary}
	require.NoError(t, prepareControlRole(ctx, conn, id))
	require.NoError(t, (&postgresSwitchoverSQL{}).installControlDatabase(ctx, conn, database, id, false))
	publisher := replicationRoleName(id.AttemptID)
	_, err = primary.Exec(ctx, "CREATE ROLE "+pginfra.QuoteIdentifier(publisher)+" LOGIN REPLICATION")
	require.NoError(t, err)
	for _, publication := range []string{publicationName(id.LogicalName, database), "unrelated_publication"} {
		_, err = primary.Exec(ctx, "CREATE PUBLICATION "+pginfra.QuoteIdentifier(publication)+" FOR ALL TABLES")
		require.NoError(t, err)
	}
	slot := replicationSlotName(id.LogicalName, database)
	f.startStandby(t, ctx)
	for _, name := range []string{slot, "unrelated_slot"} {
		_, err = primary.Exec(ctx, `SELECT pg_create_logical_replication_slot($1,'pgoutput',false,false,true)`, name)
		require.NoError(t, err)
	}
	standby := f.connect(t, ctx, f.standby, database, "postgres", "")
	require.Eventually(t, func() bool {
		_, _ = primary.Exec(ctx, "SELECT pg_log_standby_snapshot()")
		var synced bool
		err := standby.QueryRow(ctx, "SELECT synced AND NOT temporary FROM pg_replication_slots WHERE slot_name=$1", slot).Scan(&synced)
		return err == nil && synced
	}, 30*time.Second, 100*time.Millisecond, "the initial standby must have a persistent synchronized slot")
	t.Run("synchronized slots remain managed by PostgreSQL", func(t *testing.T) {
		_, err := primary.Exec(ctx, "ALTER ROLE "+pginfra.QuoteIdentifier(publisher)+" NOLOGIN")
		require.NoError(t, err)
		waitForPublisherLogin(t, ctx, standby, publisher, false)
		control := f.connect(t, ctx, f.standby, database, id.Role, id.Password)
		var removed, stillSynced bool
		require.NoError(t, queryOne(ctx, livePreflightSQLConn{Conn: control}, &removed, controlHelperCallSQL(controlRetireStandbySlotFunction)))
		assert.False(t, removed)
		require.NoError(t, standby.QueryRow(ctx, "SELECT synced FROM pg_replication_slots WHERE slot_name=$1", slot).Scan(&stillSynced))
		assert.True(t, stillSynced)
		_, err = primary.Exec(ctx, "ALTER ROLE "+pginfra.QuoteIdentifier(publisher)+" LOGIN")
		require.NoError(t, err)
		waitForPublisherLogin(t, ctx, standby, publisher, true)
	})
	t.Run("older preparation still verifies its control overlay", func(t *testing.T) {
		_, err := primary.Exec(ctx, "DROP FUNCTION "+pginfra.QuoteIdentifier(candidateGuardSchema)+"."+pginfra.QuoteIdentifier(controlRetireStandbySlotFunction)+"()")
		require.NoError(t, err)
		control := f.connect(t, ctx, f.standby, database, id.Role, id.Password)
		restricted := livePreflightSQLConn{Conn: control}
		require.Eventually(t, func() bool {
			var exists bool
			err := queryOne(ctx, restricted, &exists, controlGuardFunctionExistsSQL, candidateGuardSchema+"."+controlRetireStandbySlotFunction+"()")
			return err == nil && !exists
		}, 10*time.Second, 50*time.Millisecond)
		require.NoError(t, verifyControlDatabase(ctx, restricted, id, database))
		// This is fixture preparation, not an in-flight helper migration.
		require.NoError(t, installCutoverHelper(ctx, conn, retireStandbySlotHelper(id, database), id))
		require.Eventually(t, func() bool {
			return verifyControlHelper(ctx, restricted, retireStandbySlotHelper(id, database), id) == nil
		}, 10*time.Second, 50*time.Millisecond)
	})
	_, err = primary.Exec(ctx, "CHECKPOINT")
	require.NoError(t, err)
	f.demotePrimary(t, ctx)
	retired := f.connect(t, ctx, f.primary, database, "postgres", "")
	var recovery, active, synced, failover, temporary bool
	require.NoError(t, retired.QueryRow(ctx, `SELECT pg_is_in_recovery(),active,synced,failover,temporary FROM pg_replication_slots WHERE slot_name=$1`, slot).Scan(&recovery, &active, &synced, &failover, &temporary))
	require.True(t, recovery)
	require.False(t, active)
	require.False(t, synced, "reproduce the former primary's local slot, not a synchronized slot")
	require.True(t, failover)
	require.False(t, temporary)
	t.Logf("demoted source: recovery=%t active=%t synced=%t failover=%t temporary=%t", recovery, active, synced, failover, temporary)
	control := f.connect(t, ctx, f.primary, database, id.Role, id.Password)
	restricted := livePreflightSQLConn{Conn: control}
	var removed bool
	require.NoError(t, queryOne(ctx, restricted, &removed, controlHelperCallSQL(controlRetireStandbySlotFunction)))
	assert.False(t, removed, "the publisher must be retired before removing a local standby slot")
	current := f.connect(t, ctx, f.standby, database, id.Role, id.Password)
	require.NoError(t, queryOne(ctx, livePreflightSQLConn{Conn: current}, &removed, controlHelperCallSQL(controlRetireReplicationFunction)))
	require.True(t, removed)
	waitForPublisherLogin(t, ctx, retired, publisher, false)
	t.Run("active local slot is preserved", func(t *testing.T) {
		config := f.primary.Copy()
		config.Database = database
		config.RuntimeParams["replication"] = "database"
		stream, err := pgconn.ConnectConfig(ctx, &config.Config)
		require.NoError(t, err)
		t.Cleanup(func() { _ = stream.Close(context.WithoutCancel(ctx)) })
		stream.Frontend().SendQuery(&pgproto3.Query{String: "START_REPLICATION SLOT " + pginfra.QuoteIdentifier(slot) + " LOGICAL 0/0 (proto_version '1',publication_names 'unrelated_publication')"})
		require.NoError(t, stream.Frontend().Flush())
		receiveCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		message, err := stream.ReceiveMessage(receiveCtx)
		require.NoError(t, err)
		require.IsType(t, &pgproto3.CopyBothResponse{}, message)
		require.NoError(t, queryOne(ctx, restricted, &removed, controlHelperCallSQL(controlRetireStandbySlotFunction)))
		assert.False(t, removed)
		var active bool
		require.NoError(t, retired.QueryRow(ctx, "SELECT active FROM pg_replication_slots WHERE slot_name=$1", slot).Scan(&active))
		assert.True(t, active, "the helper must neither drop the slot nor terminate its consumer")
		require.NoError(t, stream.Close(ctx))
		require.Eventually(t, func() bool {
			err := retired.QueryRow(ctx, "SELECT active FROM pg_replication_slots WHERE slot_name=$1", slot).Scan(&active)
			return err == nil && !active
		}, 10*time.Second, 50*time.Millisecond)
	})
	sql.connect = func(ctx context.Context, host, database, user, password, _ string, _ []byte) (pginfra.Conn, error) {
		config := f.primary.Copy()
		if host == "10.0.0.2" {
			config = f.standby.Copy()
		}
		config.Database, config.User, config.Password = database, user, password
		raw, err := pgx.ConnectConfig(ctx, config)
		if err != nil {
			return nil, err
		}
		return livePreflightSQLConn{Conn: raw}, nil
	}
	access, err := newBlueGreenAccess(sql.client, sql.client, client.ObjectKeyFromObject(sql.owner), sql.owner, sql)
	require.NoError(t, err)
	completion := &postgresCompletionSQL{switchover: sql, access: access}
	require.EventuallyWithT(t, func(check *assert.CollectT) {
		done, err := completion.RemoveSourceReplication(ctx, blue, status)
		assert.NoError(check, err)
		assert.True(check, done)
	}, 10*time.Second, 100*time.Millisecond, "retirement must converge after primary demotion")
	for _, config := range []*pgx.ConnConfig{f.primary, f.standby} {
		admin := f.connect(t, ctx, config, database, "postgres", "")
		var absent, foreignPresent bool
		require.NoError(t, admin.QueryRow(ctx, "SELECT NOT EXISTS(SELECT FROM pg_replication_slots WHERE slot_name=$1),EXISTS(SELECT FROM pg_replication_slots WHERE slot_name='unrelated_slot')", slot).Scan(&absent, &foreignPresent))
		assert.True(t, absent)
		assert.True(t, foreignPresent, "retirement must preserve unrelated slots on both instances")
	}
	for range 2 {
		done, err := completion.RemoveSourceReplication(ctx, blue, status)
		require.NoError(t, err)
		assert.True(t, done, "retirement is idempotent")
	}
	t.Run("same-name physical slot is rejected", func(t *testing.T) {
		_, err := retired.Exec(ctx, "SELECT pg_create_physical_replication_slot($1)", slot)
		require.NoError(t, err)
		_, err = completion.RemoveSourceReplication(ctx, blue, status)
		require.ErrorContains(t, err, "replication slot identity changed")
		var physical bool
		require.NoError(t, retired.QueryRow(ctx, "SELECT slot_type='physical' FROM pg_replication_slots WHERE slot_name=$1", slot).Scan(&physical))
		assert.True(t, physical)
	})
	_, err = restricted.Exec(ctx, "SELECT pg_drop_replication_slot('unrelated_slot')")
	require.Error(t, err, "the control role must not gain general slot administration")
	require.ErrorContains(t, queryOne(ctx, livePreflightSQLConn{Conn: retired}, &removed, controlHelperCallSQL(controlRetireStandbySlotFunction)), "invalid cutover identity")
}

func waitForPublisherLogin(t *testing.T, ctx context.Context, conn *pgx.Conn, role string, login bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		var observed bool
		err := conn.QueryRow(ctx, "SELECT rolcanlogin FROM pg_roles WHERE rolname=$1", role).Scan(&observed)
		return err == nil && observed == login
	}, 10*time.Second, 50*time.Millisecond)
}

type standbyRetirementFixture struct {
	id               string
	primary, standby *pgx.ConnConfig
}

func newStandbyRetirementFixture(t *testing.T, ctx context.Context, image string) *standbyRetirementFixture {
	t.Helper()
	f := &standbyRetirementFixture{}
	args := []string{"run", "--detach", "--pull=never", "--label", "splunk-operator.test=standby-retirement", "--cpus=0.75", "--memory=512m", "--pids-limit=128", "--publish", "127.0.0.1::5432", "--publish", "127.0.0.1::5433", "--entrypoint", "sleep", image, "infinity"}
	f.id = strings.TrimSpace(f.docker(t, ctx, args...))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		if t.Failed() {
			out, _ := exec.CommandContext(cleanup, "docker", "exec", f.id, "sh", "-c", "tail -n 60 /tmp/a.log /tmp/b.log").CombinedOutput()
			t.Logf("isolated PostgreSQL logs:\n%s", out)
		}
		f.docker(t, cleanup, "rm", "--force", "--volumes", f.id)
	})
	for port, target := range map[string]**pgx.ConnConfig{"5432": &f.primary, "5433": &f.standby} {
		address := strings.TrimSpace(f.docker(t, ctx, "port", f.id, port+"/tcp"))
		host, p, err := net.SplitHostPort(address)
		require.NoError(t, err)
		*target, err = pgx.ParseConfig("host=" + host + " port=" + p + " user=postgres dbname=postgres sslmode=disable connect_timeout=5")
		require.NoError(t, err)
	}
	f.shell(t, ctx, `initdb -D /tmp/a --auth=trust --data-checksums >/tmp/initdb.log
echo 'host all all all trust' >> /tmp/a/pg_hba.conf
cat >> /tmp/a/postgresql.conf <<'CONF'
listen_addresses='*'
port=5432
wal_level=logical
max_replication_slots=16
max_wal_senders=8
hot_standby_feedback=on
sync_replication_slots=on
shared_buffers='16MB'
wal_keep_size='64MB'
CONF
pg_ctl -D /tmp/a -l /tmp/a.log -w start`)
	t.Logf("isolated HA fixture: container=%s image=%s", f.id, image)
	return f
}

func (f *standbyRetirementFixture) docker(t *testing.T, ctx context.Context, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	require.NoError(t, err, "Docker fixture operation: %s", out)
	return string(out)
}

func (f *standbyRetirementFixture) shell(t *testing.T, ctx context.Context, script string) {
	t.Helper()
	f.docker(t, ctx, "exec", "--user", "postgres", f.id, "sh", "-ceu", script)
}

func (f *standbyRetirementFixture) connect(t *testing.T, ctx context.Context, base *pgx.ConnConfig, database, user, password string) *pgx.Conn {
	t.Helper()
	config := base.Copy()
	config.Database, config.User, config.Password = database, user, password
	conn, err := pgx.ConnectConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(context.WithoutCancel(ctx)) })
	return conn
}

func (f *standbyRetirementFixture) startStandby(t *testing.T, ctx context.Context) {
	t.Helper()
	f.shell(t, ctx, `pg_basebackup -h 127.0.0.1 -p 5432 -U postgres -D /tmp/b -R -X stream -c fast -C -S node_b
cat >> /tmp/b/postgresql.auto.conf <<'CONF'
port=5433
primary_conninfo='host=127.0.0.1 port=5432 user=postgres dbname=postgres application_name=node_b'
CONF
pg_ctl -D /tmp/b -l /tmp/b.log -w start`)
}

func (f *standbyRetirementFixture) demotePrimary(t *testing.T, ctx context.Context) {
	t.Helper()
	f.shell(t, ctx, `pg_ctl -D /tmp/a -m fast -w stop
pg_ctl -D /tmp/b -w promote
psql -h 127.0.0.1 -p 5433 -U postgres -d postgres -c "SELECT pg_create_physical_replication_slot('node_a');" >/dev/null
pg_rewind --target-pgdata=/tmp/a --source-server='host=127.0.0.1 port=5433 user=postgres dbname=postgres'
touch /tmp/a/standby.signal
cat >> /tmp/a/postgresql.auto.conf <<'CONF'
port=5432
primary_slot_name='node_a'
primary_conninfo='host=127.0.0.1 port=5433 user=postgres dbname=postgres application_name=node_a'
CONF
pg_ctl -D /tmp/a -l /tmp/a.log -w start`)
}
