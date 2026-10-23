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
	"strings"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/jackc/pgx/v5/pgconn"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	pginfra "github.com/splunk/splunk-operator/pkg/postgresql/shared/infrastructure/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCandidateBaselineReusesReadOnlyPreflightInventory(t *testing.T) {
	source, verifier, preflight := sourceInventoryTestFixture(t, []string{subscriptionName("orders", "orders") + "/orders"}, true)
	databases := []declaredDatabase{{name: "orders"}}

	_, err := preflight.Inspect(t.Context(), source, databases, "attempt-1")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.Contains(t, err.Error(), "logical-slot")

	err = verifier.VerifySourceBaseline(t.Context(), source, databases, &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1"})
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.Contains(t, err.Error(), "large objects", "candidate verification must rerun the unsupported-object preflight check")
}

func TestCandidateBaselineAcceptsUnchangedPreparedSource(t *testing.T) {
	source, verifier, _ := sourceInventoryTestFixture(t, []string{subscriptionName("orders", "orders") + "/orders"}, false)
	schema, access := "schema-fingerprint", "access-fingerprint"
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt-1",
		DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{
			DatabaseName: "orders", SourceSchemaFingerprint: &schema, SourceAccessPolicyFingerprint: &access,
		}},
	}
	require.NoError(t, verifier.VerifySourceBaseline(t.Context(), source, []declaredDatabase{{name: "orders"}}, status))
}

func TestCandidateBaselineAcceptsItsPreparedPublication(t *testing.T) {
	source, verifier, _ := sourceInventoryTestFixture(t, nil, false)
	owner := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: source.Namespace, UID: "parent-uid"}}
	verifier.owner = owner
	name := publicationName(owner.Name, "orders")
	publication := &cnpgv1.Publication{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: source.Namespace, Labels: map[string]string{blueGreenAttemptLabel: "attempt-1"}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))}},
		Spec:       desiredPublicationSpec(source.Name, "orders", name),
	}
	require.NoError(t, verifier.client.Create(t.Context(), publication))
	verifier.connect = func(_ context.Context, _, database, _, _ string, _ []byte) (pginfra.Conn, error) {
		return sourceInventoryConn{database: database, publication: name}, nil
	}
	schema, access := "schema-fingerprint", "access-fingerprint"
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{
		DatabaseName: "orders", SourceSchemaFingerprint: &schema, SourceAccessPolicyFingerprint: &access,
	}}}
	require.NoError(t, verifier.VerifySourceBaseline(t.Context(), source, []declaredDatabase{{name: "orders"}}, status))
	verifier.connect = func(_ context.Context, _, database, _, _ string, _ []byte) (pginfra.Conn, error) {
		return sourceInventoryConn{database: database, publication: name, publicationIncomplete: true}, nil
	}
	require.ErrorIs(t, verifier.VerifySourceBaseline(t.Context(), source, []declaredDatabase{{name: "orders"}}, status), mvutypes.ErrUpgradeFlowFailed)
}

func TestCandidateReadyRejectsUnexpectedSourceLogicalSlot(t *testing.T) {
	for _, test := range []struct {
		name  string
		slots []string
		ready bool
		fail  bool
	}{
		{name: "only attempt slot", slots: []string{subscriptionName("orders", "orders") + "/orders"}, ready: true},
		{name: "table copy slot still closing", slots: []string{subscriptionName("orders", "orders") + "/orders", "pg_10_sync_11_12/orders"}},
		{name: "unexpected slot", slots: []string{subscriptionName("orders", "orders") + "/orders", "other/orders"}, fail: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, verifier, _ := sourceInventoryTestFixture(t, test.slots, false)
			ready, err := verifier.verifySourceSlotInventory(t.Context(), source, []declaredDatabase{{name: "orders"}})
			if test.fail {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.ready, ready)
		})
	}
}

func TestCandidateReadyRequiresHealthySourceFailoverTopology(t *testing.T) {
	source := &cnpgv1.Cluster{
		Spec: cnpgv1.ClusterSpec{Instances: 3},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy, Instances: 3, ReadyInstances: 3, CurrentPrimary: "source-1",
			InstancesStatus: map[cnpgv1.PodStatus][]string{cnpgv1.PodHealthy: {"source-1", "source-2", "source-3"}},
		},
	}
	assert.True(t, sourceReadyForFailover(source))
	source.Status.ReadyInstances = 1
	source.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"source-1"}
	assert.False(t, sourceReadyForFailover(source), "a healthy primary alone is not a failover-safe source")
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{
		DatabaseName: "orders", SlotHealthy: new(true), ReplicaSlots: []platformv1alpha1.BlueGreenReplicaSlotStatus{{ReplicaName: "source-1"}},
	}}}
	verifier := &postgresCandidateVerifier{client: fake.NewClientBuilder().Build(), connect: func(context.Context, string, string, string, string, []byte) (pginfra.Conn, error) {
		t.Fatal("degraded source must not begin SQL replication verification")
		return nil, nil
	}}
	ready, err := verifier.VerifyReplication(t.Context(), source, &cnpgv1.Cluster{}, nil, status)
	require.NoError(t, err)
	assert.False(t, ready)
	assert.Equal(t, new(false), status.DatabaseReplication[0].SlotHealthy)
	assert.Empty(t, status.DatabaseReplication[0].ReplicaSlots)
	source.Status.ReadyInstances = 3
	assert.False(t, sourceReadyForFailover(source), "a stale ready count does not prove replica health")
	source.Status.InstancesStatus[cnpgv1.PodHealthy] = []string{"source-2", "source-3", "source-4"}
	assert.False(t, sourceReadyForFailover(source), "the current primary must be in the healthy set")
}

func TestCandidateSamplesBlueWALPosition(t *testing.T) {
	source, verifier, _ := sourceInventoryTestFixture(t, nil, false)
	position, err := verifier.sourceWALPosition(t.Context(), source)
	require.NoError(t, err)
	assert.Equal(t, "0/16B6C50", position)
}

func TestDirectBluePodConnectionVerifiesBlueServiceName(t *testing.T) {
	source, verifier, _ := sourceInventoryTestFixture(t, nil, false)
	host := "source-1.source-r.default.svc.cluster.local"
	verifier.connect = func(_ context.Context, gotHost, database, password, serverName string, serverCA []byte) (pginfra.Conn, error) {
		assert.Equal(t, host, gotHost)
		assert.Equal(t, "orders", database)
		assert.Equal(t, "test-password", password)
		assert.Equal(t, "source-rw.default.svc.cluster.local", serverName)
		assert.Equal(t, []byte("test-ca"), serverCA)
		return sourceInventoryConn{database: database}, nil
	}
	conn, err := verifier.connectCluster(t.Context(), source, host, "orders")
	require.NoError(t, err)
	require.NoError(t, conn.Close(t.Context()))
}

func TestLogicalSlotRequiresFailoverSafeReplicaState(t *testing.T) {
	primary := logicalSlotObservation{active: true, confirmed: true, failover: true, positionCovered: true}
	assert.True(t, logicalSlotReady(primary, true))
	assert.False(t, logicalSlotReady(primary, false), "a primary slot is not a synchronized standby slot")
	standby := logicalSlotObservation{confirmed: true, failover: true, synced: true, positionCovered: true}
	assert.True(t, logicalSlotReady(standby, false))
	standby.synced = false
	assert.False(t, logicalSlotReady(standby, false))
	standby.synced = true
	standby.positionCovered = false
	assert.False(t, logicalSlotReady(standby, false))
	standby.positionCovered = true
	standby.temporary = true
	assert.False(t, logicalSlotReady(standby, false))
	primary.failover = false
	assert.False(t, logicalSlotReady(primary, true))
	primary.failover = true
	primary.positionCovered = false
	assert.False(t, logicalSlotReady(primary, true), "a confirmed but lagging slot is not caught up")

	conn := &preflightSQLTestConn{results: [][]any{{[]any{false, true, "", true, true, false, true}}}}
	state, err := logicalSlotState(t.Context(), conn, "orders-slot", "0/16B6C50")
	require.NoError(t, err)
	assert.True(t, logicalSlotReady(state, false))
	require.Len(t, conn.queries, 1)
	assert.Contains(t, conn.queries[0], "pg_last_wal_replay_lsn()")
	assert.Contains(t, conn.queries[0], "failover, synced, temporary")
	assert.Contains(t, conn.queries[0], "confirmed_flush_lsn >= $2::pg_lsn")

	missing := &preflightSQLTestConn{results: [][]any{{}}}
	state, err = logicalSlotState(t.Context(), missing, "missing-slot", "0/16B6C50")
	require.NoError(t, err)
	assert.Equal(t, "missing", state.invalidation)
	assert.False(t, logicalSlotReady(state, false))

	lateError := &preflightSQLTestConn{
		results:   [][]any{{[]any{false, true, "", true, true, false, true}}},
		rowErrors: []error{fmt.Errorf("slot query failed after its row")},
	}
	_, err = logicalSlotState(t.Context(), lateError, "orders-slot", "0/16B6C50")
	require.ErrorContains(t, err, "slot query failed after its row")
}

func TestSubscriptionStateRequiresRunningCaughtUpApplyWorkerAndEveryTableCopied(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	target := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "default"}, Spec: cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "green-superuser"}}, Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: "green-server-ca"}}}}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "green-superuser", Namespace: "default"}, Data: map[string][]byte{replicationSecretPasswordKey: []byte("password")}}
	serverCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "green-server-ca", Namespace: "default"}, Data: map[string][]byte{"ca.crt": []byte("test-ca")}}
	verifier := &postgresCandidateVerifier{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, serverCA).Build(), logicalName: "orders"}
	for _, test := range []struct {
		name         string
		applyWorker  bool
		caughtUp     bool
		tablesCopied bool
	}{
		{name: "worker stopped", caughtUp: true, tablesCopied: true},
		{name: "table still copying", applyWorker: true, caughtUp: true},
		{name: "apply is behind", applyWorker: true, tablesCopied: true},
		{name: "fully ready", applyWorker: true, caughtUp: true, tablesCopied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			verifier.connect = func(context.Context, string, string, string, string, []byte) (pginfra.Conn, error) {
				return subscriptionStateConn{applyWorker: test.applyWorker, caughtUp: test.caughtUp, tablesCopied: test.tablesCopied, t: t}, nil
			}
			applyWorker, tablesCopied, err := verifier.subscriptionState(t.Context(), target, "orders", "0/16B6C50")
			require.NoError(t, err)
			assert.Equal(t, test.applyWorker && test.caughtUp, applyWorker)
			assert.Equal(t, test.tablesCopied, tablesCopied)
		})
	}
}

type subscriptionStateConn struct {
	applyWorker  bool
	caughtUp     bool
	tablesCopied bool
	t            *testing.T
}

func (subscriptionStateConn) Begin(context.Context) (pginfra.Tx, error) { panic("not used") }
func (subscriptionStateConn) Close(context.Context) error               { return nil }
func (c subscriptionStateConn) Query(_ context.Context, sql string, _ ...any) (pginfra.Rows, error) {
	switch {
	case strings.Contains(sql, "pg_stat_subscription"):
		assert.Contains(c.t, sql, "worker.pid IS NOT NULL")
		assert.Contains(c.t, sql, "sub.subfailover")
		assert.Contains(c.t, sql, "worker.latest_end_lsn >= $2::pg_lsn")
		return &sourceInventoryRows{values: []any{c.applyWorker && c.caughtUp}}, nil
	case strings.Contains(sql, "pg_subscription_rel"):
		assert.Contains(c.t, sql, "rel.srrelid = c.oid")
		assert.Contains(c.t, sql, "rel.srsubstate = 'r'")
		return &sourceInventoryRows{values: []any{c.tablesCopied}}, nil
	default:
		return nil, fmt.Errorf("unexpected subscription query: %s", sql)
	}
}
func (subscriptionStateConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("not used")
}

func sourceInventoryTestFixture(t *testing.T, slots []string, unsupported bool) (*cnpgv1.Cluster, *postgresCandidateVerifier, *postgresSourcePreflight) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	source := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default"},
		Spec:       cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "source-superuser"}},
		Status:     cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: "source-server-ca"}}},
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "source-superuser", Namespace: "default"}, Data: map[string][]byte{"password": []byte("test-password")}}
	serverCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "source-server-ca", Namespace: "default"}, Data: map[string][]byte{"ca.crt": []byte("test-ca")}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, serverCA).Build()
	connect := func(_ context.Context, _, database, _, _ string, _ []byte) (pginfra.Conn, error) {
		return sourceInventoryConn{database: database, slots: slots, unsupported: unsupported}, nil
	}
	return source, &postgresCandidateVerifier{client: client, connect: connect, logicalName: "orders"},
		&postgresSourcePreflight{client: client, connect: connect, logicalName: "orders"}
}

type sourceInventoryConn struct {
	database              string
	slots                 []string
	unsupported           bool
	publication           string
	publicationIncomplete bool
}

func (sourceInventoryConn) Begin(context.Context) (pginfra.Tx, error) { panic("read-only inspection") }
func (sourceInventoryConn) Close(context.Context) error               { return nil }
func (sourceInventoryConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("read-only inspection")
}
func (c sourceInventoryConn) Query(_ context.Context, sql string, args ...any) (pginfra.Rows, error) {
	switch {
	case strings.Contains(sql, "current_setting('max_replication_slots')"):
		return &sourceInventoryRows{values: []any{true}}, nil
	case strings.Contains(sql, "SELECT datname FROM pg_database"):
		return &sourceInventoryRows{values: []any{"orders"}}, nil
	case strings.Contains(sql, "SELECT rolname FROM pg_roles"):
		return &sourceInventoryRows{values: []any{"postgres"}}, nil
	case strings.Contains(sql, "SELECT 'tablespace:'"):
		if args[0] == true && len(c.slots) != 0 {
			return &sourceInventoryRows{values: []any{"logical-slot:" + strings.SplitN(c.slots[0], "/", 2)[0]}}, nil
		}
		return &sourceInventoryRows{}, nil
	case strings.Contains(sql, "SELECT 'maintenance-object:'"):
		return &sourceInventoryRows{}, nil
	case strings.Contains(sql, "SELECT subname FROM pg_subscription"):
		return &sourceInventoryRows{}, nil
	case strings.Contains(sql, "SELECT EXISTS (SELECT 1 FROM pg_namespace"):
		return &sourceInventoryRows{values: []any{false}}, nil
	case strings.Contains(sql, "pg_largeobject_metadata"):
		return &sourceInventoryRows{values: []any{c.unsupported}}, nil
	case strings.Contains(sql, "SELECT format('%I.%I', n.nspname, c.relname)"):
		return &sourceInventoryRows{}, nil
	case strings.Contains(sql, "SELECT 'foreign-server:'"):
		return &sourceInventoryRows{}, nil
	case strings.Contains(sql, "SELECT 'publication:'"):
		if c.publication != "" {
			return &sourceInventoryRows{values: []any{"publication:" + c.publication}}, nil
		}
		return &sourceInventoryRows{}, nil
	case strings.Contains(sql, "puballtables AND pubinsert"):
		return &sourceInventoryRows{values: []any{!c.publicationIncomplete}}, nil
	case strings.Contains(sql, "SELECT extname FROM pg_extension"):
		return &sourceInventoryRows{values: []any{"plpgsql"}}, nil
	case strings.Contains(sql, "format('extension:%I:%s'"):
		return &sourceInventoryRows{values: []any{"schema-fingerprint"}}, nil
	case strings.Contains(sql, "format('database:%I:%s'"):
		return &sourceInventoryRows{values: []any{"access-fingerprint"}}, nil
	case strings.Contains(sql, "SELECT slot_name || '/' || database"):
		values := make([]any, len(c.slots))
		for i, slot := range c.slots {
			values[i] = slot
		}
		return &sourceInventoryRows{values: values}, nil
	case strings.Contains(sql, "pg_current_wal_lsn()::text"):
		return &sourceInventoryRows{values: []any{"0/16B6C50"}}, nil
	default:
		return nil, fmt.Errorf("unexpected %s query: %s", c.database, sql)
	}
}

type sourceInventoryRows struct {
	values []any
	next   int
}

func (r *sourceInventoryRows) Next() bool {
	if r.next >= len(r.values) {
		return false
	}
	r.next++
	return true
}
func (r *sourceInventoryRows) Scan(dest ...any) error {
	switch value := r.values[r.next-1].(type) {
	case bool:
		*dest[0].(*bool) = value
	case string:
		*dest[0].(*string) = value
	default:
		return fmt.Errorf("unexpected result type %T", value)
	}
	return nil
}
func (*sourceInventoryRows) Close()     {}
func (*sourceInventoryRows) Err() error { return nil }

func TestCandidateGuardInstalledFailsClosedOnAlteredGuard(t *testing.T) {
	for _, test := range []struct {
		state     string
		installed bool
		failed    bool
	}{
		{state: "missing"},
		{state: "valid", installed: true},
		{state: "invalid", failed: true},
	} {
		t.Run(test.state, func(t *testing.T) {
			installed, err := candidateGuardInstalled(t.Context(), guardStateConn{state: test.state}, "public", "orders")
			if test.failed {
				require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.installed, installed)
		})
	}
}

func TestCandidateGuardInstalledRequiresUnconditionalTrigger(t *testing.T) {
	var query string
	installed, err := candidateGuardInstalled(t.Context(), guardStateConn{state: "valid", query: &query}, "public", "orders")
	require.NoError(t, err)
	assert.True(t, installed)
	assert.Contains(t, query, "tgqual IS NULL", "a WHEN clause can prevent the guard from firing")
	assert.Contains(t, query, "pg_catalog.cardinality(tgattr) = 0", "column-specific UPDATE guards do not cover every write")
	assert.Contains(t, query, "tgnargs = 0")
}

func TestCandidateGuardsAreInstalledOnceThenOnlyVerified(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	target := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "default"},
		Spec:       cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "green-superuser"}},
		Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
			ServerCASecret: "green-server-ca",
		}}},
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "green-superuser", Namespace: "default"}, Data: map[string][]byte{"password": []byte("test-password")}}
	serverCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "green-server-ca", Namespace: "default"}, Data: map[string][]byte{"ca.crt": []byte("test-ca")}}
	schema, access := "schema-fingerprint", "access-fingerprint"
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{{
		DatabaseName: "orders", SourceSchemaFingerprint: &schema, SourceAccessPolicyFingerprint: &access,
	}}}
	verifier := &postgresCandidateVerifier{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, serverCA).Build()}
	var conn *preflightSQLTestConn
	verifier.connect = func(context.Context, string, string, string, string, []byte) (pginfra.Conn, error) { return conn, nil }
	databases := []declaredDatabase{{name: "orders"}}
	unpersisted := status.DeepCopy()
	conn = &preflightSQLTestConn{results: [][]any{
		{schema}, {access}, {false}, {[]any{"public", "orders"}},
		{true}, {[]any{"public", "orders"}}, {"valid"},
	}}
	require.NoError(t, verifier.VerifyAndGuard(t.Context(), target, databases, status))
	assert.Equal(t, new(true), status.DatabaseReplication[0].SchemaImported)
	assert.Len(t, conn.execs, 3, "first pass creates the schema, function, and table guard")
	assert.Equal(t, 1, conn.commits)
	assert.Zero(t, conn.rollbacks)
	assert.Contains(t, conn.execs[1], "pg_catalog.pg_replication_origin_session_is_setup()")

	conn = &preflightSQLTestConn{results: [][]any{
		{schema}, {access}, {true}, {true}, {[]any{"public", "orders"}}, {"valid"},
	}}
	require.NoError(t, verifier.VerifyAndGuard(t.Context(), target, databases, unpersisted))
	assert.Empty(t, conn.execs, "an unsaved receipt must adopt intact guards without replacing them")
	assert.Equal(t, new(true), unpersisted.DatabaseReplication[0].SchemaImported)

	conn = &preflightSQLTestConn{results: [][]any{
		{schema}, {access}, {true}, {true}, {[]any{"public", "orders"}}, {"missing"},
	}}
	err := verifier.VerifyAndGuard(t.Context(), target, databases, statusWithoutGuardReceipt(status))
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.Empty(t, conn.execs, "partial guards without a receipt must invalidate the candidate")

	conn = &preflightSQLTestConn{results: [][]any{
		{schema}, {access}, {false}, {[]any{"public", "orders"}},
	}, execErrors: []error{nil, nil, fmt.Errorf("trigger creation failed")}}
	err = verifier.VerifyAndGuard(t.Context(), target, databases, statusWithoutGuardReceipt(status))
	require.Error(t, err)
	assert.Equal(t, 1, conn.rollbacks, "failed installation must roll back all guard DDL")
	assert.Zero(t, conn.commits)

	conn = &preflightSQLTestConn{results: [][]any{
		{schema}, {access}, {true}, {[]any{"public", "orders"}}, {"valid"},
	}}
	require.NoError(t, verifier.VerifyAndGuard(t.Context(), target, databases, status))
	assert.Empty(t, conn.execs, "readiness retries must not replace installed guards")

	for _, test := range []struct {
		name    string
		results [][]any
	}{
		{name: "function changed", results: [][]any{{schema}, {access}, {false}}},
		{name: "trigger missing", results: [][]any{{schema}, {access}, {true}, {[]any{"public", "orders"}}, {"missing"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			conn = &preflightSQLTestConn{results: test.results}
			err := verifier.VerifyAndGuard(t.Context(), target, databases, status)
			require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
			assert.Empty(t, conn.execs, "lost protection must invalidate, not repair, a prepared candidate")
		})
	}
}

func TestCandidateGuardReceiptSurvivesLaterDatabaseFailure(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	target := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "green", Namespace: "default"},
		Spec:       cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: "green-superuser"}},
		Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
			ServerCASecret: "green-server-ca",
		}}},
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "green-superuser", Namespace: "default"}, Data: map[string][]byte{"password": []byte("test-password")}}
	serverCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "green-server-ca", Namespace: "default"}, Data: map[string][]byte{"ca.crt": []byte("test-ca")}}
	schema, access := "schema-fingerprint", "access-fingerprint"
	status := &platformv1alpha1.PostgresBlueGreenUpgradeStatus{AttemptID: "attempt-1", DatabaseReplication: []platformv1alpha1.BlueGreenDatabaseReplicationStatus{
		{DatabaseName: "orders", SourceSchemaFingerprint: &schema, SourceAccessPolicyFingerprint: &access},
		{DatabaseName: "inventory", SourceSchemaFingerprint: &schema, SourceAccessPolicyFingerprint: &access},
	}}
	first := &preflightSQLTestConn{results: [][]any{
		{schema}, {access}, {false}, {[]any{"public", "orders"}},
		{true}, {[]any{"public", "orders"}}, {"valid"},
	}}
	second := &preflightSQLTestConn{results: [][]any{{"changed-schema"}, {access}}}
	verifier := &postgresCandidateVerifier{client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, serverCA).Build()}
	verifier.connect = func(_ context.Context, _ string, database string, _ string, _ string, _ []byte) (pginfra.Conn, error) {
		if database == "orders" {
			return first, nil
		}
		return second, nil
	}
	err := verifier.VerifyAndGuard(t.Context(), target, []declaredDatabase{{name: "orders"}, {name: "inventory"}}, status)
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
	assert.Equal(t, new(true), status.DatabaseReplication[0].SchemaImported)
	assert.Nil(t, status.DatabaseReplication[1].SchemaImported)
}

func statusWithoutGuardReceipt(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) *platformv1alpha1.PostgresBlueGreenUpgradeStatus {
	copy := status.DeepCopy()
	copy.DatabaseReplication[0].SchemaImported = nil
	copy.DatabaseReplication[0].TargetSchemaFingerprint = nil
	copy.DatabaseReplication[0].TargetAccessPolicyFingerprint = nil
	return copy
}

type guardStateConn struct {
	state string
	query *string
}

func (guardStateConn) Begin(context.Context) (pginfra.Tx, error) { panic("not used") }
func (guardStateConn) Close(context.Context) error               { return nil }
func (c guardStateConn) Query(_ context.Context, query string, _ ...any) (pginfra.Rows, error) {
	if c.query != nil {
		*c.query = query
	}
	return &guardStateRows{state: c.state}, nil
}
func (guardStateConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("not used")
}

type guardStateRows struct {
	state string
	read  bool
}

func (r *guardStateRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}
func (r *guardStateRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.state
	return nil
}
func (*guardStateRows) Close()     {}
func (*guardStateRows) Err() error { return nil }
