package core

// The following functions are intentionally not tested directly here.
// Their business logic is covered by narrower helper tests where practical,
// and the remaining behavior is mostly controller-runtime orchestration:
// - PostgresDatabaseService
// - patchManagedRoles
// - reconcileCNPGDatabases
// - handleDeletion
// - orphanRetainedResources
// - deleteRemovedResources
// - cleanupManagedRoles

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"unicode"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// managedRolesFieldsRaw is a helper to construct the raw managed fields JSON for testing parseRoleNames and related functions.
func managedRolesFieldsRaw(t *testing.T, keys ...string) []byte {
	t.Helper()

	managedRoles := make(map[string]any, len(keys))
	for _, key := range keys {
		managedRoles[key] = map[string]any{}
	}

	raw, err := json.Marshal(map[string]any{
		"f:spec": map[string]any{
			"f:managedRoles": managedRoles,
		},
	})
	require.NoError(t, err)

	return raw
}

type stubDBRepo struct {
	execErr error
	calls   []string
}

// ExecGrants is a stub implementation of the DBRepo interface that records calls and returns a predefined error.
func (r *stubDBRepo) ExecGrants(_ context.Context, dbName string) error {
	r.calls = append(r.calls, dbName)
	return r.execErr
}

// boolPtr is a helper to get a pointer to a bool value, used for testing conditions with pointer fields.
func boolPtr(v bool) *bool {
	return &v
}

// strPtr is a helper to get a pointer to a string value, used for testing pointer string fields.
func strPtr(s string) *string {
	return &s
}

func databaseNames(defs []enterprisev4.DatabaseDefinition) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}

func assertGeneratedPassword(t *testing.T, got string, wantLength, wantDigits int) {
	t.Helper()

	digitCount := 0
	for _, r := range got {
		if unicode.IsDigit(r) {
			digitCount++
			continue
		}

		assert.Truef(t, unicode.IsLetter(r), "password contains unsupported rune %q", r)
	}

	assert.Len(t, got, wantLength)
	assert.Equal(t, wantDigits, digitCount)
}

// testScheme constructs a runtime.Scheme with the necessary API types registered for testing.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(enterprisev4.AddToScheme(scheme))
	utilruntime.Must(cnpgv1.AddToScheme(scheme))

	return scheme
}

// testClient constructs a fake client with the given scheme and initial objects for testing.
func testClient(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) client.Client {
	t.Helper()

	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&enterprisev4.PostgresDatabase{}).
		WithObjects(objs...)

	return builder.Build()
}

func TestGetDesiredUsers(t *testing.T) {
	postgresDB := &enterprisev4.PostgresDatabase{
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{
				{Name: "main_db"},
				{Name: "secondary_db"},
			},
		},
	}
	want := []string{
		"main_db_admin",
		"main_db_rw",
		"secondary_db_admin",
		"secondary_db_rw",
	}

	got := getDesiredUsers(postgresDB)

	assert.Equal(t, want, got)
}

func TestGetUsersInClusterSpec(t *testing.T) {
	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			ManagedRoles: []enterprisev4.ManagedRole{
				{Name: "main_db_admin"},
				{Name: "main_db_rw"},
			},
		},
	}
	want := []string{"main_db_admin", "main_db_rw"}

	got := getUsersInClusterSpec(cluster)

	assert.Equal(t, want, got)
}

func TestParseRoleNames(t *testing.T) {
	validKey, err := json.Marshal(map[string]string{"name": "main_db_admin"})
	require.NoError(t, err)
	ignoredKey, err := json.Marshal(map[string]string{"other": "value"})
	require.NoError(t, err)

	tests := []struct {
		name string
		raw  []byte
		want []string
	}{
		{
			name: "extracts role names from managed roles fields",
			raw: managedRolesFieldsRaw(
				t,
				"k:"+string(validKey),
				"k:"+string(ignoredKey),
				"plain-key",
			),
			want: []string{"main_db_admin"},
		},
		{
			name: "returns nil on invalid json",
			raw:  []byte(`{"f:spec"`),
			want: nil,
		},
		{
			name: "returns empty when managed roles missing",
			raw:  []byte(`{"f:spec":{}}`),
			want: nil,
		},
		{
			name: "returns empty when spec field is missing entirely",
			raw:  []byte(`{"f:metadata":{}}`),
			want: nil,
		},
	}

	for _, tst := range tests {

		t.Run(tst.name, func(t *testing.T) {
			got := parseRoleNames(tst.raw)

			assert.ElementsMatch(t, tst.want, got)
		})
	}
}

func TestManagedRoleOwners(t *testing.T) {
	roleKey, err := json.Marshal(map[string]string{"name": "main_db_admin"})
	require.NoError(t, err)
	secondRoleKey, err := json.Marshal(map[string]string{"name": "main_db_rw"})
	require.NoError(t, err)

	managedFields := []metav1.ManagedFieldsEntry{
		{Manager: "ignored"},
		{
			Manager: "postgresdatabase-other",
			FieldsV1: &metav1.FieldsV1{
				Raw: managedRolesFieldsRaw(
					t,
					"k:"+string(roleKey),
					"k:"+string(secondRoleKey),
				),
			},
		},
		{
			Manager: "postgresdatabase-newer",
			FieldsV1: &metav1.FieldsV1{
				Raw: managedRolesFieldsRaw(t, "k:"+string(roleKey)),
			},
		},
	}
	want := map[string]string{
		"main_db_admin": "postgresdatabase-newer",
		"main_db_rw":    "postgresdatabase-other",
	}

	got := managedRoleOwners(managedFields)

	assert.Equal(t, want, got)
}

func TestGetRoleConflicts(t *testing.T) {
	roleKey, err := json.Marshal(map[string]string{"name": "main_db_admin"})
	require.NoError(t, err)
	sameOwnerKey, err := json.Marshal(map[string]string{"name": "main_db_rw"})
	require.NoError(t, err)
	unrelatedKey, err := json.Marshal(map[string]string{"name": "audit_admin"})
	require.NoError(t, err)

	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "primary"},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{{Name: "main_db"}},
		},
	}
	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager: "postgresdatabase-legacy",
					FieldsV1: &metav1.FieldsV1{
						Raw: managedRolesFieldsRaw(
							t,
							"k:"+string(roleKey),
							"k:"+string(unrelatedKey),
						),
					},
				},
				{
					Manager: fieldManagerName(postgresDB.Name),
					FieldsV1: &metav1.FieldsV1{
						Raw: managedRolesFieldsRaw(t, "k:"+string(sameOwnerKey)),
					},
				},
			},
		},
	}
	want := []string{"main_db_admin (owned by postgresdatabase-legacy)"}

	got := getRoleConflicts(postgresDB, cluster)

	assert.ElementsMatch(t, want, got)
}

func TestVerifyRolesReady(t *testing.T) {
	tests := []struct {
		name          string
		expectedUsers []string
		cluster       *cnpgv1.Cluster
		wantNotReady  []string
		wantErr       string
	}{
		{
			name:          "returns error when a role cannot reconcile",
			expectedUsers: []string{"main_db_admin", "main_db_rw"},
			cluster: &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					ManagedRolesStatus: cnpgv1.ManagedRoles{
						CannotReconcile: map[string][]string{
							"main_db_rw": {"reserved role"},
						},
					},
				},
			},
			wantErr: "user main_db_rw reconciliation failed: [reserved role]",
		},
		{
			name:          "returns missing roles that are not reconciled yet",
			expectedUsers: []string{"main_db_admin", "main_db_rw", "analytics_admin"},
			cluster: &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					ManagedRolesStatus: cnpgv1.ManagedRoles{
						ByStatus: map[cnpgv1.RoleStatus][]string{
							cnpgv1.RoleStatusReconciled: {"main_db_admin", "analytics_admin"},
						},
					},
				},
			},
			wantNotReady: []string{"main_db_rw"},
		},
		{
			name:          "returns pending reconciliation roles as not ready",
			expectedUsers: []string{"main_db_admin", "main_db_rw"},
			cluster: &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					ManagedRolesStatus: cnpgv1.ManagedRoles{
						ByStatus: map[cnpgv1.RoleStatus][]string{
							cnpgv1.RoleStatusReconciled:            {"main_db_admin"},
							cnpgv1.RoleStatusPendingReconciliation: {"main_db_rw"},
						},
					},
				},
			},
			wantNotReady: []string{"main_db_rw"},
		},
		{
			name:          "returns empty when all roles are reconciled",
			expectedUsers: []string{"main_db_admin"},
			cluster: &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					ManagedRolesStatus: cnpgv1.ManagedRoles{
						ByStatus: map[cnpgv1.RoleStatus][]string{
							cnpgv1.RoleStatusReconciled: {"main_db_admin"},
						},
					},
				},
			},
			wantNotReady: nil,
		},
	}

	for _, tst := range tests {

		t.Run(tst.name, func(t *testing.T) {
			gotNotReady, err := verifyRolesReady(context.Background(), tst.expectedUsers, tst.cluster)
			if tst.wantErr != "" {
				require.Error(t, err)
				assert.Equal(t, tst.wantErr, err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tst.wantNotReady, gotNotReady)
		})
	}
}

func TestReconcileRWRolePrivileges(t *testing.T) {
	tests := []struct {
		name            string
		dbNames         []string
		newRepoErrs     map[string]error
		execErrs        map[string]error
		wantRepoCalls   []string
		wantExecCalls   map[string][]string
		wantErrContains []string
	}{
		{
			name:          "returns nil when all databases succeed",
			dbNames:       []string{"payments", "analytics"},
			wantRepoCalls: []string{"payments", "analytics"},
			wantExecCalls: map[string][]string{
				"payments":  {"payments"},
				"analytics": {"analytics"},
			},
		},
		{
			name:          "continues after repo creation and exec errors",
			dbNames:       []string{"payments", "analytics", "audit"},
			newRepoErrs:   map[string]error{"payments": errors.New("connect failed")},
			execErrs:      map[string]error{"analytics": errors.New("grant failed")},
			wantRepoCalls: []string{"payments", "analytics", "audit"},
			wantExecCalls: map[string][]string{
				"analytics": {"analytics"},
				"audit":     {"audit"},
			},
			wantErrContains: []string{
				"database payments: connect failed",
				"database analytics: grant failed",
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			repos := make(map[string]*stubDBRepo, len(tst.dbNames))
			repoCalls := make([]string, 0, len(tst.dbNames))

			for _, dbName := range tst.dbNames {
				repos[dbName] = &stubDBRepo{execErr: tst.execErrs[dbName]}
			}

			newDBRepo := func(_ context.Context, host, dbName, password string) (DBRepo, error) {
				repoCalls = append(repoCalls, dbName)
				if err := tst.newRepoErrs[dbName]; err != nil {
					return nil, err
				}

				return repos[dbName], nil
			}

			err := reconcileRWRolePrivileges(context.Background(), "rw.example.internal", "supersecret", tst.dbNames, newDBRepo)

			assert.Equal(t, tst.wantRepoCalls, repoCalls)
			for dbName, wantCalls := range tst.wantExecCalls {
				assert.Equal(t, wantCalls, repos[dbName].calls)
			}

			if len(tst.wantErrContains) == 0 {
				assert.NoError(t, err)
				return
			}

			require.Error(t, err)
			for _, wantMsg := range tst.wantErrContains {
				assert.ErrorContains(t, err, wantMsg)
			}
		})
	}
}

func TestGetClusterReadyStatus(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *enterprisev4.PostgresCluster
		wantStatus clusterReadyStatus
	}{
		{
			name:       "returns not ready when phase is nil",
			cluster:    &enterprisev4.PostgresCluster{},
			wantStatus: ClusterNotReady,
		},
		{
			name: "returns not ready when phase is not ready",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					Phase: strPtr("Provisioning"),
				},
			},
			wantStatus: ClusterNotReady,
		},
		{
			name: "returns no provisioner ref when phase is ready but ref is missing",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					Phase: strPtr(string(ClusterReady)),
				},
			},
			wantStatus: ClusterNoProvisionerRef,
		},
		{
			name: "returns ready when phase and provisioner ref are present",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					Phase:          strPtr(string(ClusterReady)),
					ProvisionerRef: &corev1.ObjectReference{Name: "cnpg-primary", Namespace: "dbs"},
				},
			},
			wantStatus: ClusterReady,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			assert.Equal(t, tst.wantStatus, getClusterReadyStatus(tst.cluster))
		})
	}
}

// Uses a fake client because fetching the referenced Cluster depends on API reads.
func TestFetchCluster(t *testing.T) {
	scheme := testScheme(t)

	tests := []struct {
		name       string
		cluster    *enterprisev4.PostgresCluster
		wantName   string
		wantErr    string
		wantAbsent bool
	}{
		{
			name:       "returns not found when cluster is absent",
			wantAbsent: true,
		},
		{
			name: "returns referenced cluster when present",
			cluster: &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"},
			},
			wantName: "primary",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			postgresDB := &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "dbs"},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "primary"},
				},
			}

			var objs []client.Object
			if tst.cluster != nil {
				objs = append(objs, tst.cluster)
			}

			c := testClient(t, scheme, objs...)
			cluster, err := fetchCluster(context.Background(), c, postgresDB)

			if tst.wantAbsent {
				require.Error(t, err)
				assert.True(t, apierrors.IsNotFound(err))
				assert.Nil(t, cluster)
				return
			}

			if tst.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tst.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cluster)
			assert.Equal(t, tst.wantName, cluster.Name)
		})
	}

	t.Run("returns error on client failure", func(t *testing.T) {
		postgresDB := &enterprisev4.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "dbs"},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "primary"},
			},
		}
		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return errors.New("api unavailable")
				},
			}).
			Build()

		cluster, err := fetchCluster(context.Background(), c, postgresDB)

		require.Error(t, err)
		assert.Nil(t, cluster)
		assert.ErrorContains(t, err, "api unavailable")
	})
}

// Uses a fake client because the helper mutates status in-memory and persists it through the status subresource.
func TestSetStatus(t *testing.T) {
	scheme := testScheme(t)
	existing := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "primary",
			Namespace:  "dbs",
			Generation: 7,
		},
	}
	c := testClient(t, scheme, existing)
	postgresDB := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}, postgresDB))

	err := persistStatus(
		context.Background(),
		c,
		postgresDB,
		clusterReady,
		metav1.ConditionTrue,
		reasonClusterAvailable,
		"Cluster is operational",
		provisioningDBPhase,
	)

	require.NoError(t, err)
	require.NotNil(t, postgresDB.Status.Phase)
	assert.Equal(t, string(provisioningDBPhase), *postgresDB.Status.Phase)
	require.Len(t, postgresDB.Status.Conditions, 1)
	assert.Equal(t, string(clusterReady), postgresDB.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, postgresDB.Status.Conditions[0].Status)
	assert.Equal(t, string(reasonClusterAvailable), postgresDB.Status.Conditions[0].Reason)
	assert.Equal(t, "Cluster is operational", postgresDB.Status.Conditions[0].Message)
	assert.Equal(t, postgresDB.Generation, postgresDB.Status.Conditions[0].ObservedGeneration)

	got := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: postgresDB.Name, Namespace: postgresDB.Namespace}, got))
	require.NotNil(t, got.Status.Phase)
	assert.Equal(t, *postgresDB.Status.Phase, *got.Status.Phase)
	require.Len(t, got.Status.Conditions, 1)
	assert.Equal(t, postgresDB.Status.Conditions[0], got.Status.Conditions[0])
}

// Uses a fake client because readiness is determined from CNPG Database objects in the API.
func TestVerifyDatabasesReady(t *testing.T) {
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{
				{Name: "payments"},
				{Name: "analytics"},
			},
		},
	}

	tests := []struct {
		name         string
		objects      []client.Object
		wantNotReady []string
		wantErr      string
	}{
		{
			name: "returns empty when all databases are applied",
			objects: []client.Object{
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-payments", Namespace: "dbs"},
					Status:     cnpgv1.DatabaseStatus{Applied: boolPtr(true)},
				},
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-analytics", Namespace: "dbs"},
					Status:     cnpgv1.DatabaseStatus{Applied: boolPtr(true)},
				},
			},
			wantNotReady: nil,
		},
		{
			name: "returns names for databases that are not applied",
			objects: []client.Object{
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-payments", Namespace: "dbs"},
					Status:     cnpgv1.DatabaseStatus{Applied: boolPtr(false)},
				},
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-analytics", Namespace: "dbs"},
				},
			},
			wantNotReady: []string{"payments", "analytics"},
		},
		{
			name: "returns error when a database is missing",
			objects: []client.Object{
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-payments", Namespace: "dbs"},
					Status:     cnpgv1.DatabaseStatus{Applied: boolPtr(true)},
				},
			},
			wantErr: "getting CNPG Database primary-analytics",
		},
	}

	for _, tst := range tests {

		t.Run(tst.name, func(t *testing.T) {
			c := testClient(t, scheme, tst.objects...)

			got, err := verifyDatabasesReady(context.Background(), c, postgresDB)

			if tst.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tst.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tst.wantNotReady, got)
		})
	}
}

// Uses a fake client because the helper wraps Kubernetes get/not-found behavior.
func TestGetSecret(t *testing.T) {
	scheme := testScheme(t)

	t.Run("returns secret when found", func(t *testing.T) {
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "db-secret", Namespace: "dbs"},
			Data:       map[string][]byte{secretKeyPassword: []byte("value")},
		}
		c := testClient(t, scheme, existing)

		secret, err := getSecret(context.Background(), c, "dbs", "db-secret")

		require.NoError(t, err)
		require.NotNil(t, secret)
		assert.Equal(t, existing.Name, secret.Name)
		assert.Equal(t, "value", string(secret.Data[secretKeyPassword]))
	})

	t.Run("returns nil nil when secret is absent", func(t *testing.T) {
		c := testClient(t, scheme)

		secret, err := getSecret(context.Background(), c, "dbs", "missing")

		require.NoError(t, err)
		assert.Nil(t, secret)
	})
}

// Uses a fake client because adoption updates object metadata and persists it through the client.
func TestAdoptResource(t *testing.T) {
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev4.GroupVersion.String(),
			Kind:       "PostgresDatabase",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
			UID:       types.UID("postgresdb-uid"),
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "primary-payments-config",
			Namespace:   "dbs",
			Annotations: map[string]string{annotationRetainedFrom: "primary", "keep": "true"},
		},
	}
	c := testClient(t, scheme, postgresDB, configMap)

	err := adoptResource(context.Background(), c, scheme, postgresDB, configMap)

	require.NoError(t, err)

	updated := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, updated))
	assert.Equal(t, "true", updated.Annotations["keep"])
	_, exists := updated.Annotations[annotationRetainedFrom]
	assert.False(t, exists)
	require.Len(t, updated.OwnerReferences, 1)
	assert.Equal(t, postgresDB.UID, updated.OwnerReferences[0].UID)
}

// Uses a fake client because these helpers mutate existing API objects during orphaning.
func TestOrphanResourceHelpers(t *testing.T) {
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
			UID:       types.UID("postgresdb-uid"),
		},
	}
	databases := []enterprisev4.DatabaseDefinition{{Name: "payments"}}

	t.Run("orphanCNPGDatabases strips owner and adds retain annotation", func(t *testing.T) {
		db := &cnpgv1.Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-payments",
				Namespace: "dbs",
				OwnerReferences: []metav1.OwnerReference{
					{UID: postgresDB.UID, Name: postgresDB.Name},
					{UID: types.UID("other"), Name: "other"},
				},
			},
		}
		c := testClient(t, scheme, db)

		require.NoError(t, orphanCNPGDatabases(context.Background(), c, postgresDB, databases))

		updated := &cnpgv1.Database{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: db.Name, Namespace: db.Namespace}, updated))
		assert.Equal(t, postgresDB.Name, updated.Annotations[annotationRetainedFrom])
		require.Len(t, updated.OwnerReferences, 1)
		assert.Equal(t, types.UID("other"), updated.OwnerReferences[0].UID)
	})

	t.Run("orphanConfigMaps skips not found", func(t *testing.T) {
		c := testClient(t, scheme)
		require.NoError(t, orphanConfigMaps(context.Background(), c, postgresDB, databases))
	})

	t.Run("orphanSecrets skips already retained secret", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "primary-payments-admin",
				Namespace:   "dbs",
				Annotations: map[string]string{annotationRetainedFrom: postgresDB.Name},
			},
		}
		c := testClient(t, scheme, secret)

		require.NoError(t, orphanSecrets(context.Background(), c, postgresDB, databases))

		updated := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}, updated))
		assert.Equal(t, postgresDB.Name, updated.Annotations[annotationRetainedFrom])
		assert.Empty(t, updated.OwnerReferences)
		assert.Equal(t, secret, updated)
	})
}

// Uses a fake client because these helpers delete Kubernetes resources and must verify API state.
func TestDeleteResourceHelpers(t *testing.T) {
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: "dbs"},
	}
	databases := []enterprisev4.DatabaseDefinition{{Name: "payments"}}

	t.Run("deleteCNPGDatabases removes existing object", func(t *testing.T) {
		db := &cnpgv1.Database{ObjectMeta: metav1.ObjectMeta{Name: "primary-payments", Namespace: "dbs"}}
		c := testClient(t, scheme, db)
		require.NoError(t, deleteCNPGDatabases(context.Background(), c, postgresDB, databases))
	})

	t.Run("deleteConfigMaps ignores missing objects", func(t *testing.T) {
		c := testClient(t, scheme)
		require.NoError(t, deleteConfigMaps(context.Background(), c, postgresDB, databases))
	})

	t.Run("deleteSecrets deletes admin and rw secrets", func(t *testing.T) {
		admin := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "primary-payments-admin", Namespace: "dbs"}}
		rw := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "primary-payments-rw", Namespace: "dbs"}}
		c := testClient(t, scheme, admin, rw)
		require.NoError(t, deleteSecrets(context.Background(), c, postgresDB, databases))
	})
}

func TestGeneratePassword(t *testing.T) {
	wantLength := passwordLength
	wantDigits := passwordDigits

	got, err := generatePassword()

	require.NoError(t, err)
	assertGeneratedPassword(t, got, wantLength, wantDigits)
}

// Uses a fake client because the helper creates Secret objects and persists owner references through the Kubernetes API.
func TestCreateUserSecret(t *testing.T) {
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev4.GroupVersion.String(),
			Kind:       "PostgresDatabase",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
			UID:       types.UID("postgresdb-uid"),
		},
	}

	t.Run("creates secret with generated credentials", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		wantManagedBy := "splunk-operator"
		wantReload := "true"
		wantUsername := roleName
		wantOwnerUID := postgresDB.UID
		wantPasswordLength := passwordLength
		wantPasswordDigits := passwordDigits
		c := testClient(t, scheme)

		err := createUserSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, secretName, got.Name)
		assert.Equal(t, postgresDB.Namespace, got.Namespace)
		assert.Equal(t, wantManagedBy, got.Labels[labelManagedBy])
		assert.Equal(t, wantReload, got.Labels[labelCNPGReload])
		assert.Equal(t, wantUsername, string(got.Data["username"]))
		assertGeneratedPassword(t, string(got.Data[secretKeyPassword]), wantPasswordLength, wantPasswordDigits)
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, wantOwnerUID, got.OwnerReferences[0].UID)
	})

	t.Run("returns nil when secret already exists", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		wantUsername := roleName
		wantPassword := "existing-password"
		existing := buildPasswordSecret(postgresDB, secretName, wantUsername, wantPassword)
		c := testClient(t, scheme, existing)

		err := createUserSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, wantUsername, string(got.Data["username"]))
		assert.Equal(t, wantPassword, string(got.Data[secretKeyPassword]))
		assert.Empty(t, got.OwnerReferences)
	})
}

// Uses a fake client because the helper decides between get/create/adopt behavior based on Secret state in the API.
func TestEnsureSecret(t *testing.T) {
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev4.GroupVersion.String(),
			Kind:       "PostgresDatabase",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
			UID:       types.UID("postgresdb-uid"),
		},
	}

	t.Run("creates missing secret", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		wantManagedBy := "splunk-operator"
		wantReload := "true"
		wantUsername := roleName
		wantOwnerUID := postgresDB.UID
		wantPasswordLength := passwordLength
		wantPasswordDigits := passwordDigits
		c := testClient(t, scheme)

		err := ensureSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, wantManagedBy, got.Labels[labelManagedBy])
		assert.Equal(t, wantReload, got.Labels[labelCNPGReload])
		assert.Equal(t, wantUsername, string(got.Data["username"]))
		assertGeneratedPassword(t, string(got.Data[secretKeyPassword]), wantPasswordLength, wantPasswordDigits)
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, wantOwnerUID, got.OwnerReferences[0].UID)
	})

	t.Run("re-adopts retained secret", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		wantUsername := roleName
		wantPassword := "existing-password"
		wantOwnerUID := postgresDB.UID
		wantKeep := "true"
		retained := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: postgresDB.Namespace,
				Annotations: map[string]string{
					annotationRetainedFrom: postgresDB.Name,
					"keep":                 wantKeep,
				},
				OwnerReferences: []metav1.OwnerReference{
					{UID: types.UID("old-owner"), Name: "old-owner"},
				},
			},
			Data: map[string][]byte{
				"username":        []byte(wantUsername),
				secretKeyPassword: []byte(wantPassword),
			},
		}
		c := testClient(t, scheme, retained)

		err := ensureSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, wantKeep, got.Annotations["keep"])
		_, hasRetainedAnnotation := got.Annotations[annotationRetainedFrom]
		assert.False(t, hasRetainedAnnotation)
		assert.Equal(t, wantUsername, string(got.Data["username"]))
		assert.Equal(t, wantPassword, string(got.Data[secretKeyPassword]))
		assert.Contains(t, got.OwnerReferences, metav1.OwnerReference{
			APIVersion:         enterprisev4.GroupVersion.String(),
			Kind:               "PostgresDatabase",
			Name:               postgresDB.Name,
			UID:                wantOwnerUID,
			Controller:         boolPtr(true),
			BlockOwnerDeletion: boolPtr(true),
		})
	})

	t.Run("does nothing for existing managed secret", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		wantUsername := roleName
		wantPassword := "existing-password"
		wantKeep := "true"
		wantOwnerUID := postgresDB.UID
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: postgresDB.Namespace,
				Annotations: map[string]string{
					"keep": wantKeep,
				},
				OwnerReferences: []metav1.OwnerReference{
					{UID: wantOwnerUID, Name: postgresDB.Name},
				},
			},
			Data: map[string][]byte{
				"username":        []byte(wantUsername),
				secretKeyPassword: []byte(wantPassword),
			},
		}
		c := testClient(t, scheme, existing)

		err := ensureSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, wantKeep, got.Annotations["keep"])
		assert.Equal(t, wantUsername, string(got.Data["username"]))
		assert.Equal(t, wantPassword, string(got.Data[secretKeyPassword]))
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, wantOwnerUID, got.OwnerReferences[0].UID)
	})
}

// Uses a fake client because the helper reconciles multiple Secret objects through the Kubernetes API.
func TestReconcileUserSecrets(t *testing.T) {
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev4.GroupVersion.String(),
			Kind:       "PostgresDatabase",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
			UID:       types.UID("postgresdb-uid"),
		},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{
				{Name: "payments"},
				{Name: "analytics"},
			},
		},
	}

	t.Run("creates secrets for each database role", func(t *testing.T) {
		c := testClient(t, scheme)
		wantSecrets := []struct {
			name     string
			username string
		}{
			{name: "primary-payments-admin", username: "payments_admin"},
			{name: "primary-payments-rw", username: "payments_rw"},
			{name: "primary-analytics-admin", username: "analytics_admin"},
			{name: "primary-analytics-rw", username: "analytics_rw"},
		}

		err := reconcileUserSecrets(context.Background(), c, scheme, postgresDB)

		require.NoError(t, err)
		for _, want := range wantSecrets {
			got := &corev1.Secret{}
			require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: want.name, Namespace: postgresDB.Namespace}, got))
			assert.Equal(t, want.username, string(got.Data["username"]))
			assertGeneratedPassword(t, string(got.Data[secretKeyPassword]), passwordLength, passwordDigits)
			require.Len(t, got.OwnerReferences, 1)
			assert.Equal(t, postgresDB.UID, got.OwnerReferences[0].UID)
		}
	})

	t.Run("is idempotent when secrets already exist", func(t *testing.T) {
		c := testClient(t, scheme)

		require.NoError(t, reconcileUserSecrets(context.Background(), c, scheme, postgresDB))

		before := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "primary-payments-admin", Namespace: postgresDB.Namespace}, before))
		beforePassword := append([]byte(nil), before.Data[secretKeyPassword]...)

		err := reconcileUserSecrets(context.Background(), c, scheme, postgresDB)

		require.NoError(t, err)

		after := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "primary-payments-admin", Namespace: postgresDB.Namespace}, after))
		assert.Equal(t, beforePassword, after.Data[secretKeyPassword])
		require.Len(t, after.OwnerReferences, 1)
		assert.Equal(t, postgresDB.UID, after.OwnerReferences[0].UID)
	})
}

// Uses a fake client because the helper reconciles ConfigMaps through CreateOrUpdate and persists re-adoption metadata.
func TestReconcileRoleConfigMaps(t *testing.T) {
	scheme := testScheme(t)
	endpoints := clusterEndpoints{
		RWHost:       "rw.default.svc.cluster.local",
		ROHost:       "ro.default.svc.cluster.local",
		PoolerRWHost: "pooler-rw.default.svc.cluster.local",
		PoolerROHost: "pooler-ro.default.svc.cluster.local",
	}

	t.Run("creates configmaps for all databases", func(t *testing.T) {
		postgresDB := &enterprisev4.PostgresDatabase{
			TypeMeta: metav1.TypeMeta{
				APIVersion: enterprisev4.GroupVersion.String(),
				Kind:       "PostgresDatabase",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary",
				Namespace: "dbs",
				UID:       types.UID("postgresdb-uid"),
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: "payments"},
					{Name: "analytics"},
				},
			},
		}
		wantManagedBy := "splunk-operator"
		wantOwnerUID := postgresDB.UID
		wantPaymentsName := "primary-payments-config"
		wantAnalyticsName := "primary-analytics-config"
		wantPaymentsData := buildDatabaseConfigMapBody("payments", endpoints)
		wantAnalyticsData := buildDatabaseConfigMapBody("analytics", endpoints)
		c := testClient(t, scheme)

		err := reconcileRoleConfigMaps(context.Background(), c, scheme, postgresDB, endpoints)

		require.NoError(t, err)

		gotPayments := &corev1.ConfigMap{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: wantPaymentsName, Namespace: postgresDB.Namespace}, gotPayments))
		assert.Equal(t, wantManagedBy, gotPayments.Labels[labelManagedBy])
		assert.Equal(t, wantPaymentsData, gotPayments.Data)
		require.Len(t, gotPayments.OwnerReferences, 1)
		assert.Equal(t, wantOwnerUID, gotPayments.OwnerReferences[0].UID)

		gotAnalytics := &corev1.ConfigMap{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: wantAnalyticsName, Namespace: postgresDB.Namespace}, gotAnalytics))
		assert.Equal(t, wantManagedBy, gotAnalytics.Labels[labelManagedBy])
		assert.Equal(t, wantAnalyticsData, gotAnalytics.Data)
		require.Len(t, gotAnalytics.OwnerReferences, 1)
		assert.Equal(t, wantOwnerUID, gotAnalytics.OwnerReferences[0].UID)
	})

	t.Run("re-adopts retained configmap", func(t *testing.T) {
		postgresDB := &enterprisev4.PostgresDatabase{
			TypeMeta: metav1.TypeMeta{
				APIVersion: enterprisev4.GroupVersion.String(),
				Kind:       "PostgresDatabase",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary",
				Namespace: "dbs",
				UID:       types.UID("postgresdb-uid"),
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: "payments"},
				},
			},
		}
		cmName := "primary-payments-config"
		wantManagedBy := "splunk-operator"
		wantOwnerUID := postgresDB.UID
		wantKeep := "true"
		wantData := buildDatabaseConfigMapBody("payments", endpoints)
		retained := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: postgresDB.Namespace,
				Labels:    map[string]string{labelManagedBy: wantManagedBy},
				Annotations: map[string]string{
					annotationRetainedFrom: postgresDB.Name,
					"keep":                 wantKeep,
				},
				OwnerReferences: []metav1.OwnerReference{
					{UID: types.UID("old-owner"), Name: "old-owner"},
				},
			},
			Data: map[string]string{
				"dbname": "stale",
			},
		}
		c := testClient(t, scheme, retained)

		err := reconcileRoleConfigMaps(context.Background(), c, scheme, postgresDB, endpoints)

		require.NoError(t, err)

		got := &corev1.ConfigMap{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, wantManagedBy, got.Labels[labelManagedBy])
		assert.Equal(t, wantKeep, got.Annotations["keep"])
		_, hasRetainedAnnotation := got.Annotations[annotationRetainedFrom]
		assert.False(t, hasRetainedAnnotation)
		assert.Equal(t, wantData, got.Data)
		assert.Contains(t, got.OwnerReferences, metav1.OwnerReference{
			APIVersion:         enterprisev4.GroupVersion.String(),
			Kind:               "PostgresDatabase",
			Name:               postgresDB.Name,
			UID:                wantOwnerUID,
			Controller:         boolPtr(true),
			BlockOwnerDeletion: boolPtr(true),
		})
	})
}

func TestBuildDeletionPlan(t *testing.T) {
	databases := []enterprisev4.DatabaseDefinition{
		{Name: "payments", DeletionPolicy: deletionPolicyRetain},
		{Name: "analytics"},
		{Name: "audit", DeletionPolicy: deletionPolicyRetain},
	}
	wantRetainedNames := []string{"payments", "audit"}
	wantDeletedNames := []string{"analytics"}

	got := buildDeletionPlan(databases)

	assert.ElementsMatch(t, wantRetainedNames, databaseNames(got.retained))
	assert.ElementsMatch(t, wantDeletedNames, databaseNames(got.deleted))
}

func TestBuildManagedRoles(t *testing.T) {
	databases := []enterprisev4.DatabaseDefinition{
		{Name: "payments"},
		{Name: "analytics"},
	}
	want := []enterprisev4.ManagedRole{
		{
			Name:   "payments_admin",
			Exists: true,
			PasswordSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "primary-payments-admin"},
				Key:                  secretKeyPassword,
			},
		},
		{
			Name:   "payments_rw",
			Exists: true,
			PasswordSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "primary-payments-rw"},
				Key:                  secretKeyPassword,
			},
		},
		{
			Name:   "analytics_admin",
			Exists: true,
			PasswordSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "primary-analytics-admin"},
				Key:                  secretKeyPassword,
			},
		},
		{
			Name:   "analytics_rw",
			Exists: true,
			PasswordSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "primary-analytics-rw"},
				Key:                  secretKeyPassword,
			},
		},
	}

	got := buildManagedRoles("primary", databases)

	assert.Equal(t, want, got)
}

func TestBuildManagedRolesPatch(t *testing.T) {
	cluster := &enterprisev4.PostgresCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev4.GroupVersion.String(),
			Kind:       "PostgresCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
		},
	}
	roles := buildManagedRoles("primary", []enterprisev4.DatabaseDefinition{{Name: "payments"}})

	got := buildManagedRolesPatch(cluster, roles)

	assert.Equal(t, cluster.APIVersion, got.Object["apiVersion"])
	assert.Equal(t, cluster.Kind, got.Object["kind"])
	assert.Equal(t, map[string]any{"name": cluster.Name, "namespace": cluster.Namespace}, got.Object["metadata"])
	assert.Equal(t, map[string]any{"managedRoles": roles}, got.Object["spec"])
}

func TestPatchManagedRolesOnDeletion(t *testing.T) {
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
		},
	}
	cluster := &enterprisev4.PostgresCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev4.GroupVersion.String(),
			Kind:       "PostgresCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
		},
	}
	retained := []enterprisev4.DatabaseDefinition{{Name: "payments"}}
	want := buildManagedRoles(postgresDB.Name, retained)
	c := testClient(t, scheme, cluster)

	err := patchManagedRolesOnDeletion(context.Background(), c, postgresDB, cluster, retained)

	require.NoError(t, err)

	got := &enterprisev4.PostgresCluster{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, got))
	assert.Equal(t, want, got.Spec.ManagedRoles)
}

func TestStripOwnerReference(t *testing.T) {
	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{UID: types.UID("remove-me"), Name: "db"},
				{UID: types.UID("keep-me"), Name: "cluster"},
			},
		},
	}

	stripOwnerReference(obj, types.UID("remove-me"))

	require.Len(t, obj.OwnerReferences, 1)
	assert.Equal(t, types.UID("keep-me"), obj.OwnerReferences[0].UID)
}

func TestBuildPasswordSecret(t *testing.T) {
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "primary",
			Namespace: "dbs",
		},
	}
	wantName := "primary-payments-admin"
	wantNamespace := "dbs"
	wantManagedBy := "splunk-operator"
	wantReload := "true"
	wantUsername := "payments_admin"
	wantPassword := "topsecret"

	got := buildPasswordSecret(postgresDB, wantName, wantUsername, wantPassword)

	assert.Equal(t, wantName, got.Name)
	assert.Equal(t, wantNamespace, got.Namespace)
	assert.Equal(t, wantManagedBy, got.Labels[labelManagedBy])
	assert.Equal(t, wantReload, got.Labels[labelCNPGReload])
	assert.Equal(t, wantUsername, string(got.Data["username"]))
	assert.Equal(t, wantPassword, string(got.Data[secretKeyPassword]))
}

func TestBuildCNPGDatabaseSpec(t *testing.T) {
	tests := []struct {
		name string
		db   enterprisev4.DatabaseDefinition
		want cnpgv1.DatabaseSpec
	}{
		{
			name: "uses delete reclaim policy by default",
			db:   enterprisev4.DatabaseDefinition{Name: "payments"},
			want: cnpgv1.DatabaseSpec{
				Name:          "payments",
				Owner:         "payments_admin",
				ClusterRef:    corev1.LocalObjectReference{Name: "cnpg-primary"},
				ReclaimPolicy: cnpgv1.DatabaseReclaimDelete,
			},
		},
		{
			name: "uses retain reclaim policy when deletion policy is retain",
			db:   enterprisev4.DatabaseDefinition{Name: "analytics", DeletionPolicy: deletionPolicyRetain},
			want: cnpgv1.DatabaseSpec{
				Name:          "analytics",
				Owner:         "analytics_admin",
				ClusterRef:    corev1.LocalObjectReference{Name: "cnpg-primary"},
				ReclaimPolicy: cnpgv1.DatabaseReclaimRetain,
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			got := buildCNPGDatabaseSpec("cnpg-primary", tst.db)
			assert.Equal(t, tst.want, got)
		})
	}
}

func TestBuildDatabaseConfigMapBody(t *testing.T) {
	tests := []struct {
		name      string
		endpoints clusterEndpoints
		want      map[string]string
	}{
		{
			name: "without pooler endpoints",
			endpoints: clusterEndpoints{
				RWHost: "rw.default.svc.cluster.local",
				ROHost: "ro.default.svc.cluster.local",
			},
			want: map[string]string{
				"dbname":     "payments",
				"port":       postgresPort,
				"rw-host":    "rw.default.svc.cluster.local",
				"ro-host":    "ro.default.svc.cluster.local",
				"admin-user": "payments_admin",
				"rw-user":    "payments_rw",
			},
		},
		{
			name: "includes pooler endpoints when available",
			endpoints: clusterEndpoints{
				RWHost:       "rw.default.svc.cluster.local",
				ROHost:       "ro.default.svc.cluster.local",
				PoolerRWHost: "pooler-rw.default.svc.cluster.local",
				PoolerROHost: "pooler-ro.default.svc.cluster.local",
			},
			want: map[string]string{
				"dbname":         "payments",
				"port":           postgresPort,
				"rw-host":        "rw.default.svc.cluster.local",
				"ro-host":        "ro.default.svc.cluster.local",
				"admin-user":     "payments_admin",
				"rw-user":        "payments_rw",
				"pooler-rw-host": "pooler-rw.default.svc.cluster.local",
				"pooler-ro-host": "pooler-ro.default.svc.cluster.local",
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			got := buildDatabaseConfigMapBody("payments", tst.endpoints)
			assert.Equal(t, tst.want, got)
		})
	}
}

func TestResolveClusterEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		cluster   *enterprisev4.PostgresCluster
		cnpg      *cnpgv1.Cluster
		namespace string
		want      clusterEndpoints
	}{
		{
			name:    "without connection pooler",
			cluster: &enterprisev4.PostgresCluster{},
			cnpg: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"},
				Status: cnpgv1.ClusterStatus{
					WriteService: "primary-rw",
					ReadService:  "primary-ro",
				},
			},
			namespace: "dbs",
			want: clusterEndpoints{
				RWHost: "primary-rw.dbs.svc.cluster.local",
				ROHost: "primary-ro.dbs.svc.cluster.local",
			},
		},
		{
			name: "with connection pooler",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					ConnectionPoolerStatus: &enterprisev4.ConnectionPoolerStatus{Enabled: true},
				},
			},
			cnpg: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"},
				Status: cnpgv1.ClusterStatus{
					WriteService: "primary-rw",
					ReadService:  "primary-ro",
				},
			},
			namespace: "dbs",
			want: clusterEndpoints{
				RWHost:       "primary-rw.dbs.svc.cluster.local",
				ROHost:       "primary-ro.dbs.svc.cluster.local",
				PoolerRWHost: "cnpg-primary-pooler-rw.dbs.svc.cluster.local",
				PoolerROHost: "cnpg-primary-pooler-ro.dbs.svc.cluster.local",
			},
		},
	}

	for _, tst := range tests {

		t.Run(tst.name, func(t *testing.T) {
			got := resolveClusterEndpoints(tst.cluster, tst.cnpg, tst.namespace)
			assert.Equal(t, tst.want, got)
		})
	}
}

func TestPopulateDatabaseStatus(t *testing.T) {
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "primary"},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{
				{Name: "payments"},
				{Name: "analytics"},
			},
		},
	}
	want := []enterprisev4.DatabaseInfo{
		{
			Name:  "payments",
			Ready: true,
			AdminUserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "primary-payments-admin"},
				Key:                  secretKeyPassword,
			},
			RWUserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "primary-payments-rw"},
				Key:                  secretKeyPassword,
			},
			ConfigMapRef: &corev1.LocalObjectReference{Name: "primary-payments-config"},
		},
		{
			Name:  "analytics",
			Ready: true,
			AdminUserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "primary-analytics-admin"},
				Key:                  secretKeyPassword,
			},
			RWUserSecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "primary-analytics-rw"},
				Key:                  secretKeyPassword,
			},
			ConfigMapRef: &corev1.LocalObjectReference{Name: "primary-analytics-config"},
		},
	}

	got := populateDatabaseStatus(postgresDB)

	assert.Equal(t, want, got)
}

func TestHasNewDatabases(t *testing.T) {
	tests := []struct {
		name       string
		postgresDB *enterprisev4.PostgresDatabase
		want       bool
	}{
		{
			name: "returns true when spec contains a new database",
			postgresDB: &enterprisev4.PostgresDatabase{
				Spec: enterprisev4.PostgresDatabaseSpec{
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "payments"},
						{Name: "analytics"},
					},
				},
				Status: enterprisev4.PostgresDatabaseStatus{
					Databases: []enterprisev4.DatabaseInfo{
						{Name: "payments"},
					},
				},
			},
			want: true,
		},
		{
			name: "returns false when all spec databases already exist in status",
			postgresDB: &enterprisev4.PostgresDatabase{
				Spec: enterprisev4.PostgresDatabaseSpec{
					Databases: []enterprisev4.DatabaseDefinition{
						{Name: "payments"},
					},
				},
				Status: enterprisev4.PostgresDatabaseStatus{
					Databases: []enterprisev4.DatabaseInfo{
						{Name: "payments"},
						{Name: "legacy-extra"},
					},
				},
			},
			want: false,
		},
	}

	for _, tst := range tests {

		t.Run(tst.name, func(t *testing.T) {
			got := hasNewDatabases(tst.postgresDB)
			assert.Equal(t, tst.want, got)
		})
	}
}

func TestNamingHelpers(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "field manager", got: fieldManagerName("primary"), want: "postgresdatabase-primary"},
		{name: "admin role", got: adminRoleName("payments"), want: "payments_admin"},
		{name: "rw role", got: rwRoleName("payments"), want: "payments_rw"},
		{name: "cnpg database", got: cnpgDatabaseName("primary", "payments"), want: "primary-payments"},
		{name: "role secret", got: roleSecretName("primary", "payments", "admin"), want: "primary-payments-admin"},
		{name: "config map", got: configMapName("primary", "payments"), want: "primary-payments-config"},
	}

	for _, tst := range tests {

		t.Run(tst.name, func(t *testing.T) {
			assert.Equal(t, tst.want, tst.got)
		})
	}
}
