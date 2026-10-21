/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package majorupgradeadapter

import (
	"context"
	"errors"
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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDeclaredRoleNamesIncludesAttemptReplicationRole(t *testing.T) {
	roles := declaredRoleNames([]declaredDatabase{{name: "orders"}}, "attempt-1")
	assert.Contains(t, roles, replicationRoleName("attempt-1"))
	assert.Contains(t, roles, "orders_admin")
	assert.Contains(t, roles, "orders_rw")
}

func TestSourceCapacityReservesPhysicalAndTableSyncResources(t *testing.T) {
	conn := &preflightSQLTestConn{results: [][]any{{true}, {"orders"}}}
	err := checkSourceCapacityAndDatabases(t.Context(), conn, []declaredDatabase{{name: "orders"}}, 2)
	require.NoError(t, err)
	require.NotEmpty(t, conn.queries)
	query := conn.queries[0]
	assert.Contains(t, query, "slot_type = 'physical'")
	assert.Contains(t, query, "pg_stat_replication")
	assert.Contains(t, query, "max_sync_workers_per_subscription")
	assert.Contains(t, query, "max_active_replication_origins")
	assert.Contains(t, query, "physical_slots + logical_slots")
	assert.Contains(t, query, "physical_senders + logical_slots")
	assert.Contains(t, query, "logical_workers > $1::int", "at least one table-sync worker must remain")
}

func TestValidateSourceRolesUsesReservedRolePolicy(t *testing.T) {
	allowed := map[string]struct{}{"orders_rw": {}}
	require.NoError(t, validateSourceRoles([]string{"postgres", "streaming_replica", "cnpg_pooler_pgbouncer", "cnpg_metrics_exporter", "pg_monitor", "orders_rw"}, allowed))
	require.ErrorIs(t, validateSourceRoles([]string{"unmanaged_role"}, allowed), mvutypes.ErrUpgradeFlowFailed)
}

func TestFingerprintQueriesCoverTheFreezeContract(t *testing.T) {
	assert.Contains(t, schemaFingerprintSQL, "pg_attribute")
	assert.Contains(t, schemaFingerprintSQL, "pg_constraint")
	assert.Contains(t, schemaFingerprintSQL, "con.contype <> 'n'", "PG18's NOT NULL catalog rows are already covered by attnotnull")
	assert.Contains(t, schemaFingerprintSQL, "pg_get_functiondef")
	assert.Contains(t, schemaFingerprintSQL, "pg_policy")
	assert.Contains(t, schemaFingerprintSQL, "pol.polcmd")
	assert.Contains(t, schemaFingerprintSQL, "pol.polpermissive")
	assert.Contains(t, schemaFingerprintSQL, "unnest(pol.polroles)")
	assert.Contains(t, schemaFingerprintSQL, "pg_get_userbyid(roleid)")
	assert.Contains(t, schemaFingerprintSQL, "sha256(convert_to(definition, 'UTF8'))")
	assert.Contains(t, accessFingerprintSQL, "sha256(convert_to(definition, 'UTF8'))")
	assert.NotContains(t, schemaFingerprintSQL, "md5(")
	assert.NotContains(t, accessFingerprintSQL, "md5(")
	assert.Contains(t, schemaFingerprintSQL, "relreplident")
	assert.Contains(t, schemaFingerprintSQL, "pg_trigger")
	assert.Contains(t, schemaFingerprintSQL, "pg_enum")
	assert.Contains(t, schemaFingerprintSQL, "pg_rewrite")
	assert.Contains(t, accessFingerprintSQL, "pg_auth_members")
	assert.Contains(t, accessFingerprintSQL, "pg_default_acl")
	assert.Contains(t, accessFingerprintSQL, "relation-acl")
	assert.Contains(t, accessFingerprintSQL, "column-acl")
	assert.Contains(t, accessFingerprintSQL, "attacl")
	assert.Contains(t, accessFingerprintSQL, "aclexplode")
	assert.NotContains(t, schemaFingerprintSQL, "nspacl", "schema privileges belong to the normalized access fingerprint")
	assert.Contains(t, schemaFingerprintSQL, "pg_get_userbyid(nspowner)")
	assert.Contains(t, schemaFingerprintSQL, "pg_get_userbyid(c.relowner)")
	assert.Contains(t, schemaFingerprintSQL, "pg_inherits")
	assert.Contains(t, schemaFingerprintSQL, "relpartbound")
	assert.Contains(t, schemaFingerprintSQL, "pg_get_userbyid(p.proowner)")
	assert.Contains(t, schemaFingerprintSQL, "format_type(seq.seqtypid, NULL)")
	assert.Contains(t, accessFingerprintSQL, "pg_get_userbyid(datdba)")
	assert.Contains(t, accessFingerprintSQL, "pg_get_userbyid(acl.grantor)", "blue and green catalog OIDs differ")
	assert.Contains(t, accessFingerprintSQL, "CASE WHEN acl.grantee = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(acl.grantee) END")
	assert.Contains(t, accessFingerprintSQL, "role.rolname, member.rolname")
	assert.Contains(t, accessFingerprintSQL, "pg_get_userbyid(defaclrole)")
	assert.Contains(t, accessFingerprintSQL, "string_agg(item::text, ',' ORDER BY item::text)")
	assert.Contains(t, accessFingerprintSQL, "AS definition", "the outer aggregate must reference a named column")
	assert.Contains(t, accessFingerprintSQL, "cnpg_pooler_pgbouncer")
}

func TestAttemptPublicationAcceptsOnlyTheAttemptOwnedPublication(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	owner := &platformv1alpha1.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: types.UID("owner")}}
	controller := true
	publicationName := publicationName("orders", "orders")
	publication := &cnpgv1.Publication{
		ObjectMeta: metav1.ObjectMeta{
			Name: publicationName, Namespace: "default",
			Labels:          map[string]string{blueGreenAttemptLabel: "attempt-1"},
			OwnerReferences: []metav1.OwnerReference{{UID: owner.UID, Name: owner.Name, Controller: &controller}},
		},
		Spec: cnpgv1.PublicationSpec{
			ClusterRef:    corev1.LocalObjectReference{Name: "source"},
			Name:          publicationName,
			DBName:        "orders",
			Target:        cnpgv1.PublicationTarget{AllTables: true},
			ReclaimPolicy: cnpgv1.PublicationReclaimDelete,
		},
	}
	preflight := &postgresSourcePreflight{
		client:      fake.NewClientBuilder().WithScheme(scheme).WithObjects(publication).Build(),
		logicalName: "orders",
		owner:       owner,
	}
	source := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default"}}

	name, err := preflight.attemptPublication(t.Context(), source, "orders", "attempt-1")
	require.NoError(t, err)
	assert.Equal(t, publication.Name, name)
	changed := publication.DeepCopy()
	changed.Spec.Parameters = map[string]string{"publish": "insert"}
	require.False(t, publicationMatchesAttempt(changed, owner, source.Name, "orders", "attempt-1"))

	_, err = preflight.attemptPublication(t.Context(), source, "orders", "different-attempt")
	require.ErrorIs(t, err, mvutypes.ErrUpgradeFlowFailed)
}

func TestSourcePreflightInspectsInventoryWithoutMutation(t *testing.T) {
	tests := []struct {
		name      string
		change    func(*preflightSQLTestConn, *preflightSQLTestConn, *declaredDatabase)
		wantError string
	}{
		{name: "healthy source"},
		{name: "insufficient capacity", change: func(source, _ *preflightSQLTestConn, _ *declaredDatabase) { source.results[0] = []any{false} }, wantError: "cannot support this attempt"},
		{name: "unmanaged database", change: func(source, _ *preflightSQLTestConn, _ *declaredDatabase) {
			source.results[1] = []any{"orders", "stray"}
		}, wantError: "not managed"},
		{name: "missing managed database", change: func(source, _ *preflightSQLTestConn, _ *declaredDatabase) { source.results[1] = nil }, wantError: "does not exist"},
		{name: "unmanaged slot", change: func(source, _ *preflightSQLTestConn, _ *declaredDatabase) {
			source.results[3] = []any{"logical-slot:stray"}
		}, wantError: "unmanaged logical-slot"},
		{name: "subscription", change: func(source, _ *preflightSQLTestConn, _ *declaredDatabase) { source.results[4] = []any{"stray"} }, wantError: "unmanaged subscriptions"},
		{name: "maintenance object", change: func(source, _ *preflightSQLTestConn, _ *declaredDatabase) {
			source.results[5] = []any{"maintenance-object:public.stray"}
		}, wantError: "unsupported maintenance-object"},
		{name: "unsupported relation", change: func(_, database *preflightSQLTestConn, _ *declaredDatabase) { database.results[0] = []any{true} }, wantError: "contains large objects"},
		{name: "foreign object", change: func(_, database *preflightSQLTestConn, _ *declaredDatabase) {
			database.results[2] = []any{"foreign-server:stray"}
		}, wantError: "unsupported foreign-server"},
		{name: "unmanaged publication", change: func(_, database *preflightSQLTestConn, _ *declaredDatabase) {
			database.results[3] = []any{"publication:stray"}
		}, wantError: "unmanaged publication"},
		{name: "undeclared extension", change: func(_, database *preflightSQLTestConn, _ *declaredDatabase) {
			database.results[4] = []any{"plpgsql", "postgis"}
		}, wantError: "not declared"},
		{name: "background worker extension", change: func(_, database *preflightSQLTestConn, declaration *declaredDatabase) {
			database.results[4] = []any{"plpgsql", "pg_cron"}
			declaration.definition.Extensions = []string{"pg_cron"}
		}, wantError: "requires background-worker"},
		{name: "reserved candidate guard schema", change: func(_, database *preflightSQLTestConn, _ *declaredDatabase) {
			database.results[5] = []any{true}
		}, wantError: "reserved candidate-guard schema"},
		{name: "unmanaged role", change: func(source, _ *preflightSQLTestConn, _ *declaredDatabase) {
			source.results[2] = []any{"postgres", "stray"}
		}, wantError: "not declared"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceConn := &preflightSQLTestConn{results: [][]any{{true}, {"orders"}, {"postgres"}, {}, {}, {}}}
			databaseConn := &preflightSQLTestConn{results: [][]any{{false}, {}, {}, {}, {"plpgsql"}, {false}, {"schema-hash"}, {"access-hash"}}}
			declaration := declaredDatabase{name: "orders"}
			if tt.change != nil {
				tt.change(sourceConn, databaseConn, &declaration)
			}
			preflight, source := newSourcePreflightForTest(t, sourceConn, databaseConn)
			checks, err := preflight.Inspect(t.Context(), source, []declaredDatabase{declaration}, "attempt-1")
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				assert.Empty(t, checks)
			} else {
				require.NoError(t, err)
				require.Equal(t, []databasePreflight{{name: "orders", schemaFingerprint: "schema-hash", accessFingerprint: "access-hash"}}, checks)
			}
			assert.Empty(t, sourceConn.execs)
			assert.Empty(t, databaseConn.execs)
		})
	}
}

func TestEnsureReplicationRoleGrantsDeclaredDatabaseAccess(t *testing.T) {
	sourceConn := &preflightSQLTestConn{}
	databaseConn := &preflightSQLTestConn{results: [][]any{{"public", "analytics"}}}
	preflight, source := newSourcePreflightForTest(t, sourceConn, databaseConn)
	require.NoError(t, preflight.EnsureReplicationRole(t.Context(), source, []declaredDatabase{{name: "orders"}}, "replica", "secret"))
	assert.Equal(t, []string{
		`CREATE ROLE "replica" LOGIN REPLICATION PASSWORD E'secret'`,
		`ALTER ROLE "replica" LOGIN REPLICATION PASSWORD E'secret'`,
		`GRANT CONNECT ON DATABASE "orders" TO "replica"`,
	}, sourceConn.execs)
	assert.Equal(t, []string{
		`GRANT USAGE ON SCHEMA "public" TO "replica"`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA "public" TO "replica"`,
		`GRANT USAGE ON SCHEMA "analytics" TO "replica"`,
		`GRANT SELECT ON ALL TABLES IN SCHEMA "analytics" TO "replica"`,
	}, databaseConn.execs)
}

func TestEnsureReplicationRoleRetriesExistingRole(t *testing.T) {
	conn := &preflightSQLTestConn{execErrors: []error{&pgconn.PgError{Code: "42710"}}}
	require.NoError(t, ensureReplicationRole(t.Context(), conn, "replica", "secret"))
	assert.Len(t, conn.execs, 2)
	assert.True(t, isDuplicateRole(&pgconn.PgError{Code: "42710"}))
	assert.False(t, isDuplicateRole(&pgconn.PgError{Code: "42501"}))
}

func TestSourcePreflightReportsCatalogReadFailures(t *testing.T) {
	tests := []struct {
		name       string
		sourceRead int
		dbRead     int
		context    string
	}{
		{name: "capacity", sourceRead: 1, context: "checking source logical-replication capacity"},
		{name: "database inventory", sourceRead: 2, context: "listing source databases"},
		{name: "roles", sourceRead: 3, context: "listing source roles"},
		{name: "cluster objects", sourceRead: 4, context: "listing source cluster-scoped replication objects"},
		{name: "subscriptions", sourceRead: 5, context: "listing source subscriptions"},
		{name: "maintenance objects", sourceRead: 6, context: "listing maintenance-database objects"},
		{name: "relation inventory", dbRead: 1, context: "checking logical-replication inventory"},
		{name: "foreign objects", dbRead: 3, context: "listing foreign objects"},
		{name: "publications", dbRead: 4, context: "listing publication and subscription objects"},
		{name: "extensions", dbRead: 5, context: "listing extensions"},
		{name: "reserved candidate guard schema", dbRead: 6, context: "checking reserved candidate-guard schema"},
		{name: "schema fingerprint", dbRead: 7, context: "fingerprinting source schema"},
		{name: "access fingerprint", dbRead: 8, context: "fingerprinting source access policy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceConn := &preflightSQLTestConn{results: [][]any{{true}, {"orders"}, {"postgres"}, {}, {}, {}}}
			databaseConn := &preflightSQLTestConn{results: [][]any{{false}, {}, {}, {}, {"plpgsql"}, {false}, {"schema"}, {"access"}}}
			if tt.sourceRead != 0 {
				sourceConn.queryErrors = make([]error, tt.sourceRead)
				sourceConn.queryErrors[tt.sourceRead-1] = errors.New("catalog read failed")
			} else {
				databaseConn.queryErrors = make([]error, tt.dbRead)
				databaseConn.queryErrors[tt.dbRead-1] = errors.New("catalog read failed")
			}
			preflight, source := newSourcePreflightForTest(t, sourceConn, databaseConn)
			_, err := preflight.Inspect(t.Context(), source, []declaredDatabase{{name: "orders"}}, "attempt-1")
			require.ErrorContains(t, err, tt.context)
			require.ErrorContains(t, err, "catalog read failed")
		})
	}
}

func TestEnsureReplicationRoleStopsOnGrantFailure(t *testing.T) {
	tests := []struct {
		name         string
		sourceExec   int
		databaseExec int
		want         string
	}{
		{name: "create role", sourceExec: 1, want: "creating attempt replication role"},
		{name: "alter role", sourceExec: 2, want: "updating attempt replication role"},
		{name: "database grant", sourceExec: 3, want: "granting replication role database access"},
		{name: "schema grant", databaseExec: 1, want: "granting replication role schema access"},
		{name: "table grant", databaseExec: 2, want: "granting replication role publication access"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceConn := &preflightSQLTestConn{}
			databaseConn := &preflightSQLTestConn{results: [][]any{{"public"}}}
			if tt.sourceExec != 0 {
				sourceConn.execErrors = make([]error, tt.sourceExec)
				sourceConn.execErrors[tt.sourceExec-1] = errors.New("permission denied")
			} else {
				databaseConn.execErrors = make([]error, tt.databaseExec)
				databaseConn.execErrors[tt.databaseExec-1] = errors.New("permission denied")
			}
			preflight, source := newSourcePreflightForTest(t, sourceConn, databaseConn)
			err := preflight.EnsureReplicationRole(t.Context(), source, []declaredDatabase{{name: "orders"}}, "replica", "secret")
			require.ErrorContains(t, err, tt.want)
			require.ErrorContains(t, err, "permission denied")
		})
	}
}

func TestConnectSourceRequiresUsableSuperuserCredential(t *testing.T) {
	preflight, source := newSourcePreflightForTest(t, &preflightSQLTestConn{}, &preflightSQLTestConn{})
	key := types.NamespacedName{Name: source.GetSuperuserSecretName(), Namespace: source.Namespace}
	secret := &corev1.Secret{}
	require.NoError(t, preflight.client.Get(t.Context(), key, secret))
	require.NoError(t, preflight.client.Delete(t.Context(), secret))
	_, err := preflight.connectSource(t.Context(), source, "orders")
	require.ErrorContains(t, err, "reading source superuser Secret")

	secret.ResourceVersion = ""
	secret.Data = nil
	require.NoError(t, preflight.client.Create(t.Context(), secret))
	_, err = preflight.connectSource(t.Context(), source, "orders")
	require.ErrorContains(t, err, "has no password")

	secret.Data = map[string][]byte{"password": []byte("secret")}
	require.NoError(t, preflight.client.Update(t.Context(), secret))
	caKey := types.NamespacedName{Name: source.Status.Certificates.ServerCASecret, Namespace: source.Namespace}
	serverCA := &corev1.Secret{}
	require.NoError(t, preflight.client.Get(t.Context(), caKey, serverCA))
	require.NoError(t, preflight.client.Delete(t.Context(), serverCA))
	_, err = preflight.connectSource(t.Context(), source, "orders")
	require.ErrorContains(t, err, "reading PostgreSQL server CA Secret")
	serverCA.ResourceVersion = ""
	serverCA.Data = nil
	require.NoError(t, preflight.client.Create(t.Context(), serverCA))
	_, err = preflight.connectSource(t.Context(), source, "orders")
	require.ErrorContains(t, err, "has no ca.crt")
	serverCA.Data = map[string][]byte{"ca.crt": []byte("test-ca")}
	require.NoError(t, preflight.client.Update(t.Context(), serverCA))
	preflight.connect = func(context.Context, string, string, string, string, []byte) (pginfra.Conn, error) {
		return nil, errors.New("connection refused")
	}
	_, err = preflight.connectSource(t.Context(), source, "orders")
	require.ErrorContains(t, err, "connecting to source database")
	require.ErrorContains(t, err, "connection refused")
}

func newSourcePreflightForTest(t *testing.T, sourceConn, databaseConn pginfra.Conn) (*postgresSourcePreflight, *cnpgv1.Cluster) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "source-superuser", Namespace: "default"}, Data: map[string][]byte{"password": []byte("test-password")}}
	serverCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "source-server-ca", Namespace: "default"}, Data: map[string][]byte{"ca.crt": []byte("test-ca")}}
	preflight := &postgresSourcePreflight{
		client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret, serverCA).Build(),
		connect: func(_ context.Context, _, database, _, _ string, _ []byte) (pginfra.Conn, error) {
			if database == "postgres" {
				return sourceConn, nil
			}
			return databaseConn, nil
		},
		logicalName: "source",
	}
	source := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "default"}, Spec: cnpgv1.ClusterSpec{SuperuserSecret: &cnpgv1.LocalObjectReference{Name: secret.Name}}, Status: cnpgv1.ClusterStatus{Certificates: cnpgv1.CertificatesStatus{CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerCASecret: serverCA.Name}}}}
	return preflight, source
}
