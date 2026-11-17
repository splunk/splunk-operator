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
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// teardownSQLDatabase simulates one PostgreSQL instance.
type teardownSQLDatabase struct {
	slots           map[string]bool // name -> active
	roles           map[string]bool // name -> owns objects
	execs           []string
	connected       []string
	closeErr        error
	closed          int
	connectErrors   map[string]error
	publications    map[string]bool
	foreignConsumer bool
	syncSlots       map[string]string
	sessions        int64
	sessionsLinger  bool
	stoppedRoles    []string
}

type teardownSQLTestConn struct {
	db       *teardownSQLDatabase
	database string
}

func (c *teardownSQLTestConn) Begin(context.Context) (pginfra.Tx, error) {
	return nil, fmt.Errorf("unexpected transaction")
}
func (c *teardownSQLTestConn) Close(context.Context) error { c.db.closed++; return c.db.closeErr }

func (c *teardownSQLTestConn) Query(_ context.Context, sql string, args ...any) (pginfra.Rows, error) {
	switch sql {
	case teardownConsumerCountSQL:
		return &cutoverSQLTestRows{values: [][]any{{c.db.sessions}}}, nil
	case teardownSyncSlotsSQL:
		var rows [][]any
		for name, database := range c.db.syncSlots {
			if slices.Contains(args[0].([]string), database) {
				if _, ok := c.db.slots[name]; ok {
					rows = append(rows, []any{name})
				}
			}
		}
		return &cutoverSQLTestRows{values: rows}, nil
	case teardownDatabaseExistsSQL:
		return &cutoverSQLTestRows{values: [][]any{{c.db.connectErrors[args[0].(string)] == nil}}}, nil
	case teardownForeignConsumerSQL:
		return &cutoverSQLTestRows{values: [][]any{{c.db.foreignConsumer}}}, nil
	case teardownStopConsumersSQL:
		c.db.stoppedRoles = append(c.db.stoppedRoles, args[0].(string))
		if !c.db.sessionsLinger {
			c.db.sessions = 0
		}
		return &cutoverSQLTestRows{values: [][]any{{true}}}, nil
	case teardownInactiveSlotsSQL:
		var rows [][]any
		for _, name := range args[0].([]string) {
			if active, ok := c.db.slots[name]; ok && !active {
				rows = append(rows, []any{name})
			}
		}
		return &cutoverSQLTestRows{values: rows}, nil
	case teardownSlotCountSQL:
		var count int64
		for _, name := range args[0].([]string) {
			if _, ok := c.db.slots[name]; ok {
				count++
			}
		}
		return &cutoverSQLTestRows{values: [][]any{{count}}}, nil
	case teardownRoleSQL:
		_, exists := c.db.roles[args[0].(string)]
		return &cutoverSQLTestRows{values: [][]any{{exists}}}, nil
	case teardownRoleOwnsSQL:
		return &cutoverSQLTestRows{values: [][]any{{c.db.roles[args[0].(string)]}}}, nil
	}
	return nil, fmt.Errorf("unexpected query %q", sql)
}

func (c *teardownSQLTestConn) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	c.db.execs = append(c.db.execs, c.database+": "+sql)
	switch {
	case strings.HasPrefix(sql, "SELECT pg_catalog.pg_drop_replication_slot"):
		delete(c.db.slots, args[0].(string))
	case strings.HasPrefix(sql, "DROP ROLE "):
		for role := range c.db.roles {
			if sql == "DROP ROLE "+pginfra.QuoteIdentifier(role) {
				delete(c.db.roles, role)
			}
		}
	case strings.HasPrefix(sql, "DROP PUBLICATION IF EXISTS "):
		for name := range c.db.publications {
			if sql == "DROP PUBLICATION IF EXISTS "+pginfra.QuoteIdentifier(name) {
				delete(c.db.publications, name)
			}
		}
	}
	return pgconn.CommandTag{}, nil
}

type teardownSQLHarness struct {
	sql       *postgresTeardownSQL
	primary   *teardownSQLDatabase
	standby   *teardownSQLDatabase
	source    *cnpgv1.Cluster
	slotNames []string
}

func newTeardownSQLHarness(t *testing.T, standbyReady bool) *teardownSQLHarness {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	source := &cnpgv1.Cluster{Name: "blue", Namespace: "default", UID: "blue-uid"}
	source.Status.CurrentPrimary = "blue-1"
	source.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-1", "blue-2"}}
	ready := corev1.ConditionFalse
	if standbyReady {
		ready = corev1.ConditionTrue
	}
	standby := &corev1.Pod{Name: "blue-2", Namespace: "default", UID: "blue-2-uid", Labels: map[string]string{"cnpg.io/cluster": "blue"},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(source, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))},
		Status:          corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(standby).Build()
	h := &teardownSQLHarness{source: source, slotNames: []string{"orders_slot"},
		primary: &teardownSQLDatabase{slots: map[string]bool{}, roles: map[string]bool{}},
		standby: &teardownSQLDatabase{slots: map[string]bool{}, roles: map[string]bool{}}}
	h.sql = &postgresTeardownSQL{client: c, connect: func(_ context.Context, _ *cnpgv1.Cluster, host, database string) (pginfra.Conn, error) {
		db := h.primary
		if host == "10.0.0.2" {
			db = h.standby
		}
		db.connected = append(db.connected, database)
		if err := db.connectErrors[database]; err != nil {
			return nil, err
		}
		return &teardownSQLTestConn{db: db, database: database}, nil
	}}
	return h
}

func TestTeardownSQLDropsOnlyInactivePrimarySlots(t *testing.T) {
	h := newTeardownSQLHarness(t, true)
	h.primary.slots["orders_slot"] = true

	gone, err := h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: h.slotNames})
	require.NoError(t, err)
	assert.False(t, gone)
	assert.Empty(t, h.primary.execs, "an active slot still has a consumer and is never dropped")

	h.primary.slots["orders_slot"] = false
	h.standby.slots["orders_slot"] = false
	gone, err = h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: h.slotNames})
	require.NoError(t, err)
	assert.False(t, gone, "a synchronized standby copy is awaited")
	assert.Equal(t, []string{"postgres: SELECT pg_catalog.pg_drop_replication_slot($1)"}, h.primary.execs)
	assert.Empty(t, h.standby.execs, "standby copies are left to slot synchronization")

	delete(h.standby.slots, "orders_slot")
	gone, err = h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: h.slotNames})
	require.NoError(t, err)
	assert.True(t, gone)
}

func TestTeardownSQLSkipsStandbysThatAreNotReady(t *testing.T) {
	h := newTeardownSQLHarness(t, false)
	h.standby.slots["orders_slot"] = false
	gone, err := h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: h.slotNames})
	require.NoError(t, err)
	assert.True(t, gone)
	assert.Empty(t, h.standby.connected)
}

func TestTeardownSQLRejectsForeignStandby(t *testing.T) {
	h := newTeardownSQLHarness(t, true)
	pod := &corev1.Pod{}
	require.NoError(t, h.sql.client.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "blue-2"}, pod))
	pod.OwnerReferences = nil
	require.NoError(t, h.sql.client.Update(t.Context(), pod))
	_, err := h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: h.slotNames})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestTeardownSQLDropsRolesAndControlSchema(t *testing.T) {
	h := newTeardownSQLHarness(t, true)
	h.primary.roles["sok_bg_replication"] = false
	h.primary.roles["sok_bg_control"] = false

	require.NoError(t, h.sql.DropAttemptRoles(t.Context(), h.source, []string{"orders", "postgres"}, []string{"sok_bg_replication", "sok_bg_control"}))
	assert.Empty(t, h.primary.roles)
	assert.Equal(t, []string{"orders", "postgres", "postgres"}, h.primary.connected, "every database is revoked once before roles are dropped")
	schema := pginfra.QuoteIdentifier(candidateGuardSchema)
	for _, database := range []string{"orders", "postgres"} {
		assert.Contains(t, h.primary.execs, database+": DROP SCHEMA IF EXISTS "+schema+" RESTRICT")
		assert.Contains(t, h.primary.execs, database+": DROP OWNED BY "+pginfra.QuoteIdentifier("sok_bg_control"))
		for _, helper := range cutoverHelpers(cutoverIdentity{}, "") {
			assert.Contains(t, h.primary.execs, database+": DROP FUNCTION IF EXISTS "+schema+"."+pginfra.QuoteIdentifier(helper.name)+"()")
		}
	}
	last := h.primary.execs[len(h.primary.execs)-2:]
	assert.Equal(t, []string{"postgres: DROP ROLE " + pginfra.QuoteIdentifier("sok_bg_replication"), "postgres: DROP ROLE " + pginfra.QuoteIdentifier("sok_bg_control")}, last)

	h.primary.execs = nil
	require.NoError(t, h.sql.DropAttemptRoles(t.Context(), h.source, []string{"orders"}, []string{"sok_bg_control"}), "a completed teardown is idempotent")
	for _, exec := range h.primary.execs {
		assert.NotContains(t, exec, "DROP OWNED", "absent roles need no revocation")
		assert.NotContains(t, exec, "DROP ROLE")
	}
}

func TestTeardownSQLReportsConnectionCloseErrors(t *testing.T) {
	h := newTeardownSQLHarness(t, true)
	h.primary.closeErr = errors.New("close failed")
	_, err := h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: h.slotNames})
	require.ErrorContains(t, err, "close failed")
	require.ErrorContains(t, h.sql.DropAttemptRoles(t.Context(), h.source, nil, nil), "close failed")
}

func TestTeardownSQLChecksOwnedStandbysMissingFromStatus(t *testing.T) {
	h := newTeardownSQLHarness(t, true)
	h.source.Status.InstancesStatus = map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"blue-1"}}
	h.standby.slots["orders_slot"] = false
	gone, err := h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: h.slotNames})
	require.NoError(t, err)
	assert.False(t, gone, "an owned standby that status no longer lists is still checked")
}

func TestTeardownSQLRefusesToDropObjectsOwnedByAttemptRole(t *testing.T) {
	h := newTeardownSQLHarness(t, true)
	h.primary.roles["sok_bg_control"] = true
	err := h.sql.DropAttemptRoles(t.Context(), h.source, nil, []string{"sok_bg_control"})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	for _, exec := range h.primary.execs {
		assert.NotContains(t, exec, "DROP OWNED")
		assert.NotContains(t, exec, "DROP ROLE")
	}
	assert.Contains(t, h.primary.roles, "sok_bg_control")
}

func TestTeardownSQLMissingDatabaseDoesNotBlockPublicationOrCredentialCleanup(t *testing.T) {
	h := newTeardownSQLHarness(t, false)
	h.primary.connectErrors = map[string]error{"gone": &pgconn.PgError{Code: "3D000"}}
	name := publicationName("orders", "surviving")
	h.primary.publications = map[string]bool{name: true}
	h.primary.roles["attempt_role"] = false
	require.NoError(t, h.sql.RemovePublications(t.Context(), h.source, "orders", []string{"gone", "surviving"}))
	assert.Empty(t, h.primary.publications, "a failed creation status write cannot hide remaining SQL")
	require.NoError(t, h.sql.DropAttemptRoles(t.Context(), h.source, []string{"gone", "surviving"}, []string{"attempt_role"}))
	assert.Empty(t, h.primary.roles)
	assert.Contains(t, h.primary.connected, "surviving")
}

func TestTeardownSQLPreservesAmbiguousDatabaseErrors(t *testing.T) {
	absent := &pgconn.PgError{Code: "3D000"}
	for name, failure := range map[string]error{
		"authentication": &pgconn.PgError{Code: "28P01"},
		"network":        errors.New("connection reset"),
		"joined":         errors.Join(absent, &pgconn.PgError{Code: "28P01"}),
	} {
		t.Run(name, func(t *testing.T) {
			h := newTeardownSQLHarness(t, false)
			h.primary.connectErrors = map[string]error{"gone": failure}
			h.primary.roles["attempt_role"] = false
			require.ErrorIs(t, h.sql.RemovePublications(t.Context(), h.source, "orders", []string{"gone"}), failure)
			require.ErrorIs(t, h.sql.DropAttemptRoles(t.Context(), h.source, []string{"gone"}, []string{"attempt_role"}), failure)
			assert.Contains(t, h.primary.roles, "attempt_role")
		})
	}
}

func TestTeardownSQLStopsOnlyAttemptConsumers(t *testing.T) {
	h := newTeardownSQLHarness(t, false)
	h.primary.roles["attempt_role"] = false
	require.NoError(t, h.sql.StopReplication(t.Context(), h.source, "attempt_role", h.slotNames))
	assert.Contains(t, h.primary.execs, `postgres: ALTER ROLE "attempt_role" NOLOGIN`)
	h.primary.foreignConsumer = true
	require.ErrorIs(t, h.sql.StopReplication(t.Context(), h.source, "attempt_role", h.slotNames), mvutypes.ErrUpgradeFlowFailed)
}

func TestTeardownSQLPinsInstanceBeforeExecutingSQL(t *testing.T) {
	for _, replacement := range []string{"none", "cluster", "pod"} {
		t.Run(replacement, func(t *testing.T) {
			f := newTeardownFixture(t)
			source := &cnpgv1.Cluster{Name: "blue", Namespace: "default"}
			require.True(t, f.exists(source))
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "blue-1", Namespace: "default", UID: "blue-pod", Labels: map[string]string{"cnpg.io/cluster": "blue"}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(source, cnpgv1.SchemeGroupVersion.WithKind("Cluster"))}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.1"}}
			require.NoError(t, f.client.Create(t.Context(), pod))
			db := &teardownSQLDatabase{}
			conn, err := connectTeardownInstance(t.Context(), f.client, f.parent, source, "blue-rw.default.svc.cluster.local", "postgres", func(_ context.Context, _ *cnpgv1.Cluster, host, _ string) (pginfra.Conn, error) {
				assert.Equal(t, "10.0.0.1", host, "connect directly to the recorded environment's instance")
				var replaced client.Object
				switch replacement {
				case "cluster":
					replaced = source.DeepCopy()
				case "pod":
					replaced = pod.DeepCopy()
				}
				if replaced != nil {
					require.NoError(t, f.client.Delete(t.Context(), replaced))
					replaced.SetUID("replacement")
					replaced.SetResourceVersion("")
					require.NoError(t, f.client.Create(t.Context(), replaced))
				}
				return &teardownSQLTestConn{db: db}, nil
			})
			if replacement == "none" {
				require.NoError(t, err)
				require.NotNil(t, conn)
				require.NoError(t, conn.Close(t.Context()))
			} else {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
				assert.Nil(t, conn)
				assert.Equal(t, 1, db.closed)
			}
			assert.Empty(t, db.execs, "identity is checked before any SQL can run")
		})
	}
}

// This uses the existing explicitly disposable PostgreSQL contract; it never
// connects to a Kubernetes environment or a customer's database.
func TestTeardownSQLAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SPLUNK_OPERATOR_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set SPLUNK_OPERATOR_TEST_POSTGRES_DSN to a disposable PostgreSQL superuser database")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close(context.WithoutCancel(t.Context())) })
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	database, gone, role, logical := "teardown_"+suffix, "gone_"+suffix, "role_"+suffix, "parent_"+suffix
	slot, publication := replicationSlotName(logical, database), publicationName(logical, database)
	var systemID string
	require.NoError(t, admin.QueryRow(t.Context(), "SELECT system_identifier::text FROM pg_catalog.pg_control_system()").Scan(&systemID))
	var syncSlot, foreignSlot string
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+pginfra.QuoteIdentifier(database))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_, _ = admin.Exec(ctx, "SELECT pg_catalog.pg_drop_replication_slot(slot_name) FROM pg_catalog.pg_replication_slots WHERE slot_name=ANY($1::text[]) AND NOT active", []string{slot, syncSlot, foreignSlot})
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pginfra.QuoteIdentifier(database)+" WITH (FORCE)")
		_, _ = admin.Exec(ctx, "DROP ROLE IF EXISTS "+pginfra.QuoteIdentifier(role))
	})
	var subscriberOID string
	require.NoError(t, admin.QueryRow(t.Context(), "SELECT oid::text FROM pg_catalog.pg_database WHERE datname=$1", database).Scan(&subscriberOID))
	syncSlot, foreignSlot = "pg_"+subscriberOID+"_sync_84_"+systemID, "pg_"+subscriberOID+"_sync_85_1"
	_, err = admin.Exec(t.Context(), "CREATE ROLE "+pginfra.QuoteIdentifier(role)+" LOGIN REPLICATION")
	require.NoError(t, err)
	config, err := pgx.ParseConfig(dsn)
	require.NoError(t, err)
	config.Database = database
	blue, err := pgx.ConnectConfig(t.Context(), config)
	require.NoError(t, err)
	defer blue.Close(context.WithoutCancel(t.Context()))
	_, err = blue.Exec(t.Context(), "CREATE PUBLICATION "+pginfra.QuoteIdentifier(publication)+" FOR ALL TABLES")
	require.NoError(t, err)
	for _, name := range []string{slot, syncSlot, foreignSlot} {
		_, err = blue.Exec(t.Context(), "SELECT pg_catalog.pg_create_logical_replication_slot($1,'pgoutput')", name)
		require.NoError(t, err)
	}
	replicationConfig := config.Copy()
	replicationConfig.User = role
	replicationConfig.RuntimeParams["replication"] = "database"
	consumer, err := pgconn.ConnectConfig(t.Context(), &replicationConfig.Config)
	require.NoError(t, err)
	defer consumer.Close(context.WithoutCancel(t.Context()))
	consumer.Frontend().Send(&pgproto3.Query{String: "START_REPLICATION SLOT " + pginfra.QuoteIdentifier(syncSlot) + " LOGICAL 0/0 (proto_version '1', publication_names '" + publication + "')"})
	require.NoError(t, consumer.Frontend().Flush())
	message, err := consumer.ReceiveMessage(t.Context())
	require.NoError(t, err)
	require.IsType(t, &pgproto3.CopyBothResponse{}, message)
	h := newTeardownSQLHarness(t, false)
	h.sql.connect = func(ctx context.Context, _ *cnpgv1.Cluster, _, db string) (pginfra.Conn, error) {
		cfg := config.Copy()
		cfg.Database = db
		conn, err := pgx.ConnectConfig(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return livePreflightSQLConn{conn}, nil
	}
	var stopErr error
	require.Eventually(t, func() bool {
		stopErr = h.sql.StopReplication(t.Context(), h.source, role, []string{slot})
		return !errors.Is(stopErr, mvutypes.ErrUpgradeFlowPending)
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, stopErr)
	require.Eventually(t, func() bool {
		var active bool
		err := blue.QueryRow(t.Context(), "SELECT active FROM pg_catalog.pg_replication_slots WHERE slot_name=$1", syncSlot).Scan(&active)
		return err == nil && !active
	}, 5*time.Second, 10*time.Millisecond)
	// A positive missing-database error must not strand later publications or roles.
	require.NoError(t, h.sql.RemovePublications(t.Context(), h.source, logical, []string{gone, database}))
	var count int
	require.NoError(t, blue.QueryRow(t.Context(), "SELECT count(*) FROM pg_catalog.pg_publication WHERE pubname=$1", publication).Scan(&count))
	assert.Zero(t, count)
	removed, err := h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: []string{slot}, databases: []string{database}, systemID: systemID})
	require.NoError(t, err)
	assert.True(t, removed)
	require.NoError(t, blue.QueryRow(t.Context(), "SELECT count(*) FROM pg_catalog.pg_replication_slots WHERE slot_name=ANY($1::text[])", []string{slot, syncSlot}).Scan(&count))
	assert.Zero(t, count, "interrupted-copy synchronization slots must be reclaimed")
	require.NoError(t, blue.QueryRow(t.Context(), "SELECT count(*) FROM pg_catalog.pg_replication_slots WHERE slot_name=$1", foreignSlot).Scan(&count))
	assert.Equal(t, 1, count, "another subscriber's synchronization slot must be preserved")
	require.NoError(t, h.sql.DropAttemptRoles(t.Context(), h.source, []string{gone, database}, []string{role}))
	require.NoError(t, admin.QueryRow(t.Context(), "SELECT count(*) FROM pg_catalog.pg_roles WHERE rolname=$1", role).Scan(&count))
	assert.Zero(t, count)
}

func TestTeardownSQLIncludesOnlyOwnedSynchronizationSlots(t *testing.T) {
	h := newTeardownSQLHarness(t, true)
	inactive, active, foreign, otherDB, physical := "pg_10_sync_20_111", "pg_10_sync_21_111", "pg_10_sync_20_222", "pg_10_sync_22_111", "pg_10_sync_23_111"
	h.primary.slots = map[string]bool{"orders_slot": false, inactive: false, active: true, foreign: false, otherDB: false, physical: false}
	h.primary.syncSlots = map[string]string{inactive: "orders", active: "orders", foreign: "orders", otherDB: "other"}
	inventory := teardownSlotInventory{names: h.slotNames, databases: []string{"orders"}, systemID: "111"}
	gone, err := h.sql.RemoveSourceSlots(t.Context(), h.source, inventory)
	require.NoError(t, err)
	assert.False(t, gone, "an active initial-copy slot must keep teardown pending")
	assert.NotContains(t, h.primary.slots, inactive)
	for _, name := range []string{active, foreign, otherDB, physical} {
		assert.Contains(t, h.primary.slots, name)
	}
	h.primary.slots[active] = false
	h.standby.slots[inactive] = false
	h.standby.syncSlots = map[string]string{inactive: "orders"}
	gone, err = h.sql.RemoveSourceSlots(t.Context(), h.source, inventory)
	require.NoError(t, err)
	assert.False(t, gone, "discover synchronized copies even when the primary slot is already gone")
	assert.Contains(t, h.standby.slots, inactive, "standby copies must never be dropped directly")
	assert.Empty(t, h.standby.execs)
	delete(h.standby.slots, inactive)
	gone, err = h.sql.RemoveSourceSlots(t.Context(), h.source, inventory)
	require.NoError(t, err)
	assert.True(t, gone)
	for _, name := range []string{foreign, otherDB, physical} {
		assert.Contains(t, h.primary.slots, name)
	}
}

func TestTeardownSQLRefusesUnidentifiedSynchronizationSlots(t *testing.T) {
	h := newTeardownSQLHarness(t, false)
	name := "pg_10_sync_20_111"
	h.primary.slots[name] = false
	h.primary.syncSlots = map[string]string{name: "orders"}
	gone, err := h.sql.RemoveSourceSlots(t.Context(), h.source, teardownSlotInventory{names: h.slotNames, databases: []string{"orders"}})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.False(t, gone)
	assert.Contains(t, h.primary.slots, name)
	assert.Empty(t, h.primary.execs)
}

func TestTeardownSQLWaitsForCopyWorkerSessionsToExit(t *testing.T) {
	h := newTeardownSQLHarness(t, false)
	h.primary.sessions = 1
	h.primary.sessionsLinger = true
	require.ErrorIs(t, h.sql.StopReplication(t.Context(), h.source, "attempt_role", h.slotNames), mvutypes.ErrUpgradeFlowPending)
	assert.Equal(t, []string{"attempt_role"}, h.primary.stoppedRoles)
	h.primary.sessionsLinger = false
	require.NoError(t, h.sql.StopReplication(t.Context(), h.source, "attempt_role", h.slotNames))
}
