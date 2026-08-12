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
package core

// The following functions are intentionally not tested directly here.
// Their business logic is covered by narrower helper tests where practical,
// and the remaining behavior is mostly controller-runtime orchestration:
// - reconcileCNPGDatabases
// - handleDeletion
// - orphanRetainedResources
// - deleteRemovedResources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
	"unicode"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	dbmetrics "github.com/splunk/splunk-operator/pkg/postgresql/database/core/custom_metrics"
	pgprometheus "github.com/splunk/splunk-operator/pkg/postgresql/shared/adapter/prometheus"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type stubDBRepo struct {
	execErr error
	calls   []string
}

type stubAcknowledgementRepository struct {
	ack        mtypes.DatabaseAcknowledgement
	found      bool
	err        error
	identities []mtypes.ContributorIdentity
}

func (r *stubAcknowledgementRepository) Find(
	_ context.Context,
	identity mtypes.ContributorIdentity,
) (mtypes.DatabaseAcknowledgement, bool, error) {
	r.identities = append(r.identities, identity)
	return r.ack, r.found, r.err
}

type provisioningDurationObservation struct {
	controller string
	seconds    float64
}

type captureMetricsRecorder struct {
	provisioningDurations []provisioningDurationObservation
}

func (r *captureMetricsRecorder) IncStatusTransition(string, string, string, string) {}
func (r *captureMetricsRecorder) ObserveProvisioningDuration(controller string, seconds float64) {
	r.provisioningDurations = append(r.provisioningDurations, provisioningDurationObservation{controller: controller, seconds: seconds})
}
func (r *captureMetricsRecorder) SetClusterPhases(map[string]float64)        {}
func (r *captureMetricsRecorder) SetPoolerEnabledClusters(float64)           {}
func (r *captureMetricsRecorder) SetDatabasePhases(map[string]float64)       {}
func (r *captureMetricsRecorder) SetManagedUsers(string, map[string]float64) {}

var _ ports.Recorder = (*captureMetricsRecorder)(nil)

// ExecGrants is a stub implementation of the DBRepo interface that records calls and returns a predefined error.
func (r *stubDBRepo) ExecGrants(_ context.Context, dbName string) error {
	r.calls = append(r.calls, dbName)
	return r.execErr
}

// boolPtr is a helper to get a pointer to a bool value, used for testing conditions with pointer fields.
func boolPtr(v bool) *bool {
	return ptr.To(v)
}

// strPtr is a helper to get a pointer to a string value, used for testing pointer string fields.
func strPtr(s string) *string {
	return ptr.To(s)
}

// int64Ptr is a helper to get a pointer to an int64 value, used for testing pointer integer fields.
func int64Ptr(v int64) *int64 {
	return ptr.To(v)
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

func postgresDatabaseConflict(name string) error {
	return apierrors.NewConflict(
		schema.GroupResource{
			Group:    enterprisev4.GroupVersion.Group,
			Resource: "postgresdatabases",
		},
		name,
		errors.New("resource version conflict"),
	)
}

func TestPostgresDatabaseServiceRequeuesOnConflict(t *testing.T) {
	scheme := testScheme(t)
	tests := []struct {
		name     string
		existing *enterprisev4.PostgresDatabase
		build    func(*enterprisev4.PostgresDatabase) client.Client
	}{
		{
			name: "when adding the finalizer",
			existing: &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "primary",
					Namespace: "dbs",
				},
			},
			build: func(existing *enterprisev4.PostgresDatabase) client.Client {
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithStatusSubresource(&enterprisev4.PostgresDatabase{}).
					WithObjects(existing).
					WithInterceptorFuncs(interceptor.Funcs{
						Update: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.UpdateOption) error {
							return postgresDatabaseConflict(obj.GetName())
						},
					}).
					Build()
			},
		},
		{
			name: "when persisting status",
			existing: &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "primary",
					Namespace:  "dbs",
					Finalizers: []string{postgresDatabaseFinalizerName},
				},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "missing-cluster"},
				},
			},
			build: func(existing *enterprisev4.PostgresDatabase) client.Client {
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithStatusSubresource(&enterprisev4.PostgresDatabase{}).
					WithObjects(existing).
					WithInterceptorFuncs(interceptor.Funcs{
						SubResourceUpdate: func(_ context.Context, _ client.Client, subResourceName string, obj client.Object, _ ...client.SubResourceUpdateOption) error {
							if subResourceName != "status" {
								return nil
							}
							return postgresDatabaseConflict(obj.GetName())
						},
					}).
					Build()
			},
		},
		{
			name: "when status update conflicts while handling another error",
			existing: &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "primary",
					Namespace:  "dbs",
					Finalizers: []string{postgresDatabaseFinalizerName},
				},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "primary"},
				},
			},
			build: func(existing *enterprisev4.PostgresDatabase) client.Client {
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithStatusSubresource(&enterprisev4.PostgresDatabase{}).
					WithObjects(existing).
					WithInterceptorFuncs(interceptor.Funcs{
						Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
							if _, ok := obj.(*enterprisev4.PostgresCluster); ok {
								return errors.New("temporary get failure")
							}
							return client.Get(ctx, key, obj, opts...)
						},
						SubResourceUpdate: func(_ context.Context, _ client.Client, subResourceName string, obj client.Object, _ ...client.SubResourceUpdateOption) error {
							if subResourceName != "status" {
								return nil
							}
							return postgresDatabaseConflict(obj.GetName())
						},
					}).
					Build()
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			c := tst.build(tst.existing)

			postgresDB := &enterprisev4.PostgresDatabase{}
			require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: tst.existing.Name, Namespace: tst.existing.Namespace}, postgresDB))

			result, err := PostgresDatabaseService(
				context.Background(),
				&ReconcileContext{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), Metrics: &pgprometheus.NoopRecorder{}},
				postgresDB,
				nil,
			)

			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{Requeue: true}, result)
		})
	}
}

// TestPostgresDatabaseServiceTerminalOnMissingExternalSecret verifies that a
// referenced-but-absent external role secret surfaces as a reconcile.TerminalError
// from the service entry point. The operator only consumes external secrets, so a
// missing one is not recoverable by backoff — recovery is driven by the
// external-Secret watch. controller-runtime must therefore stop requeueing.
func TestPostgresDatabaseServiceTerminalOnMissingExternalSecret(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	const ns = "dbs"

	postgresDB := &enterprisev4.PostgresDatabase{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresDatabase"},
		ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: ns, UID: types.UID("pdb-uid"), Generation: 1, Finalizers: []string{postgresDatabaseFinalizerName}},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary-cluster"},
			Databases: []enterprisev4.DatabaseDefinition{
				{Name: "payments", PasswordConfig: &enterprisev4.PasswordConfig{
					ExternalAdminSecretRef: corev1.LocalObjectReference{Name: "external-admin-secret"},
					ExternalRWSecretRef:    corev1.LocalObjectReference{Name: "external-rw-secret"},
				}},
			},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			Phase:     strPtr(string(readyDBPhase)),
			Databases: []enterprisev4.DatabaseInfo{{Name: "payments"}},
		},
	}

	postgresCluster := &enterprisev4.PostgresCluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "primary-cluster", Namespace: ns},
		Status: enterprisev4.PostgresClusterStatus{
			Phase: strPtr(string(ClusterReady)),
			ProvisionerRef: &corev1.ObjectReference{
				APIVersion: cnpgv1.SchemeGroupVersion.String(),
				Kind:       "Cluster",
				Name:       "primary-cnpg",
				Namespace:  ns,
			},
		},
	}

	cnpgCluster := &cnpgv1.Cluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "primary-cnpg", Namespace: ns},
	}

	c := testClient(t, scheme, postgresDB, postgresCluster, cnpgCluster)

	// newDBRepo is never reached: we fail during credential provisioning, before
	// any role is patched.
	result, err := PostgresDatabaseService(
		ctx,
		&ReconcileContext{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), Metrics: &pgprometheus.NoopRecorder{}},
		postgresDB,
		nil,
	)

	require.Error(t, err)
	assert.True(t, errors.Is(err, reconcile.TerminalError(nil)),
		"a missing external role secret must surface as a terminal error")
	assert.Equal(t, ctrl.Result{}, result)
	// The combined error must name both missing secrets, not just the first.
	assert.Contains(t, err.Error(), "external-admin-secret")
	assert.Contains(t, err.Error(), "external-rw-secret")
}

func TestDatabaseClusterNotReadyConditionReason(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	const ns = "dbs"

	pendingPhase := "Pending"
	readyDBCondition := metav1.Condition{
		Type:    string(clusterReady),
		Status:  metav1.ConditionTrue,
		Reason:  string(reasonClusterAvailable),
		Message: "Cluster is operational",
	}
	recoveryDBCondition := metav1.Condition{
		Type:    string(clusterReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(reasonClusterRecovery),
		Message: "Cluster is recovering; waiting for it to become ready",
	}
	recoveryClusterCondition := metav1.Condition{
		Type:   "ClusterReady",
		Status: metav1.ConditionFalse,
		Reason: string(cnpgReasonRecovery),
	}

	tests := []struct {
		name                string
		dbPhase             string
		dbCondition         metav1.Condition
		clusterConditions   []metav1.Condition
		wantConditionReason string
		wantEvent           string // non-empty: assert event contains this string
	}{
		{
			name:                "planned cluster change reports provisioning",
			dbPhase:             string(readyDBPhase),
			dbCondition:         readyDBCondition,
			wantConditionReason: string(reasonClusterProvisioning),
		},
		{
			name:                "wasReady + recovery cluster reports recovery and emits event",
			dbPhase:             string(readyDBPhase),
			dbCondition:         readyDBCondition,
			clusterConditions:   []metav1.Condition{recoveryClusterCondition},
			wantConditionReason: string(reasonClusterRecovery),
			wantEvent:           EventWaitingForClusterRecovery,
		},
		{
			name:                "existing recovery condition persists on subsequent reconcile",
			dbPhase:             pendingPhase,
			dbCondition:         recoveryDBCondition,
			clusterConditions:   []metav1.Condition{recoveryClusterCondition},
			wantConditionReason: string(reasonClusterRecovery),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postgresDB := &enterprisev4.PostgresDatabase{
				TypeMeta: metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresDatabase"},
				ObjectMeta: metav1.ObjectMeta{
					Name:       "primary",
					Namespace:  ns,
					UID:        types.UID("pdb-uid"),
					Generation: 1,
					Finalizers: []string{postgresDatabaseFinalizerName},
				},
				Spec: enterprisev4.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "primary-cluster"},
					Databases:  []enterprisev4.DatabaseDefinition{{Name: "payments"}},
				},
				Status: enterprisev4.PostgresDatabaseStatus{
					Phase:      &tt.dbPhase,
					Conditions: []metav1.Condition{tt.dbCondition},
					Databases:  []enterprisev4.DatabaseInfo{{Name: "payments"}},
				},
			}
			postgresCluster := &enterprisev4.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "primary-cluster", Namespace: ns},
				Status: enterprisev4.PostgresClusterStatus{
					Phase:      &pendingPhase,
					Conditions: tt.clusterConditions,
				},
			}
			c := testClient(t, scheme, postgresDB, postgresCluster)
			recorder := record.NewFakeRecorder(10)

			result, err := PostgresDatabaseService(
				ctx,
				&ReconcileContext{Client: c, Scheme: scheme, Recorder: recorder, Metrics: &pgprometheus.NoopRecorder{}},
				postgresDB,
				nil,
			)

			require.NoError(t, err)
			assert.Equal(t, ctrl.Result{RequeueAfter: retryDelay}, result)
			updated := &enterprisev4.PostgresDatabase{}
			require.NoError(t, c.Get(ctx, types.NamespacedName{Name: postgresDB.Name, Namespace: postgresDB.Namespace}, updated))
			condition := meta.FindStatusCondition(updated.Status.Conditions, string(clusterReady))
			require.NotNil(t, condition)
			assert.Equal(t, metav1.ConditionFalse, condition.Status)
			assert.Equal(t, tt.wantConditionReason, condition.Reason)
			if tt.wantEvent != "" {
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, corev1.EventTypeWarning)
					assert.Contains(t, event, tt.wantEvent)
				default:
					t.Fatal("expected warning event")
				}
			}
		})
	}
}

// TestPostgresDatabaseServiceRequeuesWhenMissingSecretStatusWriteFailsTransiently
// verifies that when an external role secret is missing AND the SecretsReady=False
// status write fails for a non-conflict reason (e.g. transient API unavailability),
// the service stays requeueable rather than terminalizing. Terminalizing would stop
// controller-runtime retry and leave the status stale until the next Secret event.
func TestPostgresDatabaseServiceRequeuesWhenMissingSecretStatusWriteFailsTransiently(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	const ns = "dbs"

	postgresDB := &enterprisev4.PostgresDatabase{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresDatabase"},
		ObjectMeta: metav1.ObjectMeta{Name: "primary", Namespace: ns, UID: types.UID("pdb-uid"), Generation: 1, Finalizers: []string{postgresDatabaseFinalizerName}},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary-cluster"},
			Databases: []enterprisev4.DatabaseDefinition{
				{Name: "payments", PasswordConfig: &enterprisev4.PasswordConfig{
					ExternalAdminSecretRef: corev1.LocalObjectReference{Name: "external-admin-secret"},
					ExternalRWSecretRef:    corev1.LocalObjectReference{Name: "external-rw-secret"},
				}},
			},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			Phase:     strPtr(string(readyDBPhase)),
			Databases: []enterprisev4.DatabaseInfo{{Name: "payments"}},
		},
	}

	postgresCluster := &enterprisev4.PostgresCluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "primary-cluster", Namespace: ns},
		Status: enterprisev4.PostgresClusterStatus{
			Phase: strPtr(string(ClusterReady)),
			ProvisionerRef: &corev1.ObjectReference{
				APIVersion: cnpgv1.SchemeGroupVersion.String(),
				Kind:       "Cluster",
				Name:       "primary-cnpg",
				Namespace:  ns,
			},
		},
	}

	cnpgCluster := &cnpgv1.Cluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "primary-cnpg", Namespace: ns},
	}

	transient := apierrors.NewServiceUnavailable("apiserver is on a coffee break")
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&enterprisev4.PostgresDatabase{}).
		WithObjects(postgresDB, postgresCluster, cnpgCluster).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(_ context.Context, _ client.Client, subResourceName string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				if subResourceName != "status" {
					return nil
				}
				return transient
			},
		}).
		Build()

	result, err := PostgresDatabaseService(
		ctx,
		&ReconcileContext{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), Metrics: &pgprometheus.NoopRecorder{}},
		postgresDB,
		nil,
	)

	require.Error(t, err)
	assert.False(t, errors.Is(err, reconcile.TerminalError(nil)),
		"a transient status-write failure must not be terminalized — it must stay requeueable")
	assert.ErrorIs(t, err, transient,
		"the returned error must carry the status-write failure so controller-runtime retries")
	assert.Equal(t, ctrl.Result{}, result)
}

func TestSecretMissingPolicyForDB(t *testing.T) {
	tests := []struct {
		name        string
		dbName      string
		existingDBs map[string]struct{}
		want        secretMissingPolicy
	}{
		{
			name:        "creates secrets for new databases",
			dbName:      "payments",
			existingDBs: map[string]struct{}{},
			want:        createSecretIfMissing,
		},
		{
			name:   "reports drift for previously provisioned databases",
			dbName: "payments",
			existingDBs: map[string]struct{}{
				"payments": {},
			},
			want: reportSecretDriftIfMissing,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			assert.Equal(t, tst.want, secretMissingPolicyForDB(tst.dbName, tst.existingDBs))
		})
	}
}

func TestExistingDatabaseStatusDoesNotDependOnCurrentPhase(t *testing.T) {
	for _, phase := range []string{
		string(provisioningDBPhase),
		string(failedDBPhase),
		string(readyDBPhase),
	} {
		t.Run(phase, func(t *testing.T) {
			postgresDB := &enterprisev4.PostgresDatabase{
				Status: enterprisev4.PostgresDatabaseStatus{
					Phase:     &phase,
					Databases: []enterprisev4.DatabaseInfo{{Name: "payments"}},
				},
			}

			assert.Contains(t, existingDatabaseStatus(postgresDB), "payments")
		})
	}
}

func TestGetDesiredRoles(t *testing.T) {
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

	got := getDesiredRoles(postgresDB)

	assert.Equal(t, want, got)
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
				"connecting on database payments failed",
				"granting RW privileges on database analytics failed",
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

func TestReconcileRWRolePrivilegesLogsCompleteOperationWithoutCredentials(t *testing.T) {
	var logOutput bytes.Buffer
	ctx := logging.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&logOutput, nil)))
	terminalErr := fmt.Errorf("%w: authentication error containing supersecret", ErrTerminal)
	retryableGrantErr := errors.New("grant failure containing supersecret")
	newDBRepo := func(_ context.Context, _, dbName, _ string) (DBRepo, error) {
		if dbName == "analytics" {
			return nil, terminalErr
		}
		if dbName == "audit" {
			return &stubDBRepo{execErr: retryableGrantErr}, nil
		}
		return &stubDBRepo{}, nil
	}

	err := reconcileRWRolePrivileges(ctx, "rw.example.internal", "supersecret", []string{"payments", "analytics", "audit"}, newDBRepo)

	require.ErrorIs(t, err, ErrTerminal)
	assert.NotErrorIs(t, err, terminalErr)
	assert.NotErrorIs(t, err, retryableGrantErr)
	assert.NotContains(t, err.Error(), "supersecret")
	assert.NotContains(t, logOutput.String(), "supersecret")

	decoder := json.NewDecoder(&logOutput)
	var records []map[string]any
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else {
			require.NoError(t, err)
		}
		records = append(records, record)
	}
	require.Len(t, records, 3)

	assert.Equal(t, "PostgreSQL privilege reconciliation completed", records[0]["msg"])
	assert.Equal(t, "rw.example.internal", records[0]["host"])
	assert.Equal(t, "payments", records[0]["database"])
	assert.NotNil(t, records[0]["duration"])
	assert.Equal(t, "success", records[0]["outcome"])

	assert.Equal(t, "PostgreSQL privilege reconciliation failed", records[1]["msg"])
	assert.Equal(t, "rw.example.internal", records[1]["host"])
	assert.Equal(t, "analytics", records[1]["database"])
	assert.NotNil(t, records[1]["duration"])
	assert.Equal(t, "failure", records[1]["outcome"])
	assert.Equal(t, "connect", records[1]["failure_stage"])
	assert.Equal(t, "terminal", records[1]["error_category"])

	assert.Equal(t, "PostgreSQL privilege reconciliation failed", records[2]["msg"])
	assert.Equal(t, "rw.example.internal", records[2]["host"])
	assert.Equal(t, "audit", records[2]["database"])
	assert.NotNil(t, records[2]["duration"])
	assert.Equal(t, "failure", records[2]["outcome"])
	assert.Equal(t, "grant", records[2]["failure_stage"])
	assert.Equal(t, "retryable", records[2]["error_category"])
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
		&pgprometheus.NoopRecorder{},
		postgresDB,
		false,
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

func TestPersistCustomMetricsPublication(t *testing.T) {
	scheme := testScheme(t)
	existing := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "database-owner",
			Namespace:  "dbs",
			UID:        types.UID("database-owner-uid"),
			Generation: 7,
		},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{
				{
					Name: "orders",
					Monitoring: &enterprisev4.DatabaseMonitoring{
						CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
							LocalObjectReference: corev1.LocalObjectReference{Name: "orders-metrics"},
							Key:                  "queries.yaml",
						}},
					},
				},
				{Name: "analytics"},
			},
		},
	}
	c := testClient(t, scheme, existing)
	postgresDB := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), postgresDB))

	changed, err := persistCustomMetricsPublication(t.Context(), c, postgresDB)

	require.NoError(t, err)
	assert.True(t, changed)
	got := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), got))
	require.NotNil(t, got.Status.CustomMetricsPublication)
	assert.Equal(t, int64(7), got.Status.CustomMetricsPublication.ObservedGeneration)
	require.Len(t, got.Status.CustomMetricsPublication.Contributions, 2)
	assert.Equal(t, "orders", got.Status.CustomMetricsPublication.Contributions[0].DatabaseName)
	assert.True(t, got.Status.CustomMetricsPublication.Contributions[0].Exists)
	assert.Equal(t, "orders-metrics", got.Status.CustomMetricsPublication.Contributions[0].CustomQueriesConfigMap[0].Name)
	assert.Equal(t, "analytics", got.Status.CustomMetricsPublication.Contributions[1].DatabaseName)
	assert.False(t, got.Status.CustomMetricsPublication.Contributions[1].Exists)
	assert.Nil(t, got.Status.ObservedGeneration,
		"publishing one component must not claim that unrelated database reconciliation observed the generation")

	changed, err = persistCustomMetricsPublication(t.Context(), c, got)
	require.NoError(t, err)
	assert.False(t, changed)

	got.Spec.Databases[0].Monitoring = nil
	changed, err = persistCustomMetricsPublication(t.Context(), c, got)
	require.NoError(t, err)
	assert.True(t, changed)
	condition := meta.FindStatusCondition(got.Status.Conditions, string(customMetricsReady))
	require.NotNil(t, condition)
	assert.Equal(t, metav1.ConditionUnknown, condition.Status)
	assert.Equal(t, string(reasonCustomMetricsPending), condition.Reason,
		"replacing an active publication with a tombstone must durably keep the acknowledgement gate pending")
}

func TestReconcileCustomMetricsGateMapsAPIState(t *testing.T) {
	const (
		namespace = "dbs"
		ownerName = "database-owner"
		ownerUID  = types.UID("database-owner-uid")
	)
	selector := corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "orders-metrics"},
		Key:                  "queries.yaml",
	}
	querySelector := mtypes.QuerySelector{
		ConfigMapName: selector.Name,
		ConfigMapKey:  selector.Key,
	}
	ordersRevision := mtypes.ContributionRevision("orders", true, []mtypes.QuerySelector{querySelector})
	disabledRevision := mtypes.ContributionRevision("analytics", false, nil)
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ownerName,
			Namespace: namespace,
			UID:       ownerUID,
		},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{
				{
					Name: "orders",
					Monitoring: &enterprisev4.DatabaseMonitoring{
						CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{selector},
					},
				},
				{Name: "analytics"},
			},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			CustomMetricsPublication: &enterprisev4.PostgresDatabaseCustomMetricsPublication{
				ObservedGeneration: 1,
				Contributions: []enterprisev4.DatabaseCustomMetricsContribution{
					{
						DatabaseName:           "orders",
						Revision:               ordersRevision,
						Exists:                 true,
						CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{selector},
					},
					{
						DatabaseName: "analytics",
						Revision:     disabledRevision,
						Exists:       false,
					},
				},
			},
		},
	}
	repository := &stubAcknowledgementRepository{
		found: true,
		ack: mtypes.DatabaseAcknowledgement{
			DesiredRevision: ordersRevision,
			AppliedRevision: ordersRevision,
			Status:          mtypes.AcknowledgementTrue,
		},
	}
	rc := &ReconcileContext{
		NewCustomMetricsAcknowledgementRepo: func(*enterprisev4.PostgresCluster) dbmetrics.AcknowledgementRepository {
			return repository
		},
	}

	outcome, err := reconcileCustomMetricsGate(
		t.Context(),
		rc,
		postgresDB,
		&enterprisev4.PostgresCluster{},
	)

	require.NoError(t, err)
	assert.Equal(t, dbmetrics.GateReady, outcome.State)
	ordersContribution := mtypes.DatabaseContribution{
		Identity: mtypes.ContributorIdentity{
			PostgresDatabaseName: ownerName,
			PostgresDatabaseUID:  string(ownerUID),
			DatabaseName:         "orders",
			Namespace:            namespace,
		},
	}
	require.Len(t, repository.identities, 1)
	assert.Equal(t, ordersContribution.Identity, repository.identities[0])
}

func TestPersistCustomMetricsStatus(t *testing.T) {
	tests := []struct {
		name            string
		outcome         dbmetrics.Outcome
		conditionStatus metav1.ConditionStatus
		phase           reconcileDBPhases
		wantReason      string
	}{
		{
			name: "ready",
			outcome: dbmetrics.Outcome{
				State:   dbmetrics.GateReady,
				Reason:  "CustomMetricsReady",
				Message: "Database custom metrics are applied",
			},
			conditionStatus: metav1.ConditionTrue,
			phase:           readyDBPhase,
			wantReason:      "CustomMetricsReady",
		},
		{
			name: "pending",
			outcome: dbmetrics.Outcome{
				State:   dbmetrics.GatePending,
				Reason:  "CustomMetricsPending",
				Message: "Waiting for PostgresCluster acknowledgement",
			},
			conditionStatus: metav1.ConditionUnknown,
			phase:           provisioningDBPhase,
			wantReason:      "CustomMetricsPending",
		},
		{
			name: "failed",
			outcome: dbmetrics.Outcome{
				State:   dbmetrics.GateFailed,
				Reason:  "InvalidQueryDefinition",
				Message: `Custom metrics for database "orders" failed`,
			},
			conditionStatus: metav1.ConditionFalse,
			phase:           failedDBPhase,
			wantReason:      "InvalidQueryDefinition",
		},
		{
			name: "disabled tombstone",
			outcome: dbmetrics.Outcome{
				State:   dbmetrics.GateReady,
				Reason:  "CustomMetricsDisabled",
				Message: "Database custom metrics are disabled",
			},
			conditionStatus: metav1.ConditionTrue,
			phase:           readyDBPhase,
			wantReason:      "CustomMetricsDisabled",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			scheme := testScheme(t)
			existing := &enterprisev4.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "database-owner",
					Namespace:  "dbs",
					Generation: 3,
				},
				Spec: enterprisev4.PostgresDatabaseSpec{
					Databases: []enterprisev4.DatabaseDefinition{{Name: "orders"}},
				},
				Status: enterprisev4.PostgresDatabaseStatus{
					CustomMetricsPublication: &enterprisev4.PostgresDatabaseCustomMetricsPublication{
						ObservedGeneration: 3,
						Contributions: []enterprisev4.DatabaseCustomMetricsContribution{{
							DatabaseName: "orders",
							Revision:     "published-revision",
							Exists:       true,
						}},
					},
				},
			}
			c := testClient(t, scheme, existing)
			postgresDB := &enterprisev4.PostgresDatabase{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), postgresDB))
			rc := &ReconcileContext{
				Client:  c,
				Metrics: &pgprometheus.NoopRecorder{},
			}

			err := persistCustomMetricsStatus(
				t.Context(),
				rc,
				postgresDB,
				tst.outcome,
				tst.conditionStatus,
				tst.phase,
			)

			require.NoError(t, err)
			got := &enterprisev4.PostgresDatabase{}
			require.NoError(t, c.Get(t.Context(), client.ObjectKeyFromObject(existing), got))
			require.NotNil(t, got.Status.Phase)
			assert.Equal(t, string(tst.phase), *got.Status.Phase)
			require.Len(t, got.Status.Databases, 1)
			require.NotNil(t, got.Status.CustomMetricsPublication)
			assert.Equal(t, "published-revision", got.Status.CustomMetricsPublication.Contributions[0].Revision,
				"the acknowledgement gate must not rewrite the early publication")
			condition := meta.FindStatusCondition(got.Status.Conditions, string(customMetricsReady))
			require.NotNil(t, condition)
			assert.Equal(t, tst.conditionStatus, condition.Status)
			assert.Equal(t, tst.wantReason, condition.Reason)
			assert.Equal(t, tst.outcome.Message, condition.Message)
			assert.Equal(t, got.Generation, condition.ObservedGeneration)
		})
	}
}

func TestReconcileCustomMetricsGatePropagatesAcknowledgementRepositoryError(t *testing.T) {
	transient := apierrors.NewServiceUnavailable("cluster status temporarily unavailable")
	selector := corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "orders-metrics"},
		Key:                  "queries.yaml",
	}
	revision := mtypes.ContributionRevision("orders", true, []mtypes.QuerySelector{{
		ConfigMapName: selector.Name,
		ConfigMapKey:  selector.Key,
	}})
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "database-owner",
			Namespace: "dbs",
			UID:       types.UID("database-owner-uid"),
		},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{{
				Name: "orders",
				Monitoring: &enterprisev4.DatabaseMonitoring{
					CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{selector},
				},
			}},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			CustomMetricsPublication: &enterprisev4.PostgresDatabaseCustomMetricsPublication{
				ObservedGeneration: 1,
				Contributions: []enterprisev4.DatabaseCustomMetricsContribution{{
					DatabaseName:           "orders",
					Revision:               revision,
					Exists:                 true,
					CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{selector},
				}},
			},
		},
	}
	repository := &stubAcknowledgementRepository{err: transient}
	rc := &ReconcileContext{
		NewCustomMetricsAcknowledgementRepo: func(*enterprisev4.PostgresCluster) dbmetrics.AcknowledgementRepository {
			return repository
		},
	}

	_, err := reconcileCustomMetricsGate(
		t.Context(),
		rc,
		postgresDB,
		&enterprisev4.PostgresCluster{},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, transient)
	assert.False(t, errors.Is(err, reconcile.TerminalError(nil)),
		"acknowledgement read failures must remain retryable")
}

func TestPersistStatusStartsReadinessCycleOnce(t *testing.T) {
	scheme := testScheme(t)
	creationTime := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	ready := string(readyDBPhase)
	generation := int64(1)
	existing := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "primary",
			Namespace:         "dbs",
			Generation:        generation,
			CreationTimestamp: creationTime,
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			Phase:              &ready,
			ObservedGeneration: &generation,
		},
	}
	c := testClient(t, scheme, existing)

	db := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}, db))
	require.NoError(t, persistStatus(
		context.Background(), c, &pgprometheus.NoopRecorder{}, db, true,
		clusterReady, metav1.ConditionFalse, reasonClusterProvisioning,
		"Cluster is not in ready state yet", pendingDBPhase,
	))
	require.NotNil(t, db.Status.LastTransitionTime)
	lastTransitionTime := *db.Status.LastTransitionTime

	stored := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: db.Name, Namespace: db.Namespace}, stored))
	require.NotNil(t, stored.Status.LastTransitionTime)
	assert.Equal(t, lastTransitionTime, *stored.Status.LastTransitionTime)

	require.NoError(t, persistStatus(
		context.Background(), c, &pgprometheus.NoopRecorder{}, stored, false,
		clusterReady, metav1.ConditionFalse, reasonClusterProvisioning,
		"Cluster is not in ready state yet", pendingDBPhase,
	))
	require.NotNil(t, stored.Status.LastTransitionTime)
	assert.Equal(t, lastTransitionTime, *stored.Status.LastTransitionTime)
}

func TestPersistStatusStartsReadinessCycleForProvisioningBlockerAfterRoutineUpdate(t *testing.T) {
	scheme := testScheme(t)
	generation := int64(1)
	ready := string(readyDBPhase)
	existing := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "primary",
			Namespace:  "dbs",
			Generation: generation,
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			Phase:              &ready,
			ObservedGeneration: &generation,
		},
	}
	c := testClient(t, scheme, existing)

	db := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: existing.Name, Namespace: existing.Namespace}, db))
	require.NoError(t, persistStatus(
		context.Background(), c, &pgprometheus.NoopRecorder{}, db, true,
		clusterReady, metav1.ConditionTrue, reasonClusterAvailable,
		"Cluster is operational", provisioningDBPhase,
	))
	require.Nil(t, db.Status.LastTransitionTime)

	require.NoError(t, persistStatus(
		context.Background(), c, &pgprometheus.NoopRecorder{}, db, true,
		secretsReady, metav1.ConditionFalse, reasonSecretsDriftDetected,
		"managed role secret drift detected", provisioningDBPhase,
	))
	require.NotNil(t, db.Status.LastTransitionTime)

	persisted := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: db.Name, Namespace: db.Namespace}, persisted))
	require.NotNil(t, persisted.Status.LastTransitionTime)
	assert.Equal(t, *db.Status.LastTransitionTime, *persisted.Status.LastTransitionTime)
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
		wantReasons  map[string]string
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
			wantReasons:  map[string]string{},
		},
		{
			name: "returns names and reasons for databases that are not applied",
			objects: []client.Object{
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-payments", Namespace: "dbs"},
					Status:     cnpgv1.DatabaseStatus{Applied: boolPtr(false), Message: "role \"payments_rw\" does not exist"},
				},
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-analytics", Namespace: "dbs"},
				},
			},
			wantNotReady: []string{"payments", "analytics"},
			wantReasons: map[string]string{
				"payments":  "role \"payments_rw\" does not exist",
				"analytics": "Waiting for CNPG to apply the database",
			},
		},
		{
			name: "returns not ready and not-found reason when a database is missing",
			objects: []client.Object{
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-payments", Namespace: "dbs"},
					Status:     cnpgv1.DatabaseStatus{Applied: boolPtr(true)},
				},
			},
			wantNotReady: []string{"analytics"},
			wantReasons:  map[string]string{"analytics": "CNPG Database not found"},
		},
		{
			name: "prefers the failing extension detail over the generic top-level message",
			objects: []client.Object{
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-payments", Namespace: "dbs"},
					Status: cnpgv1.DatabaseStatus{
						Applied: boolPtr(false),
						Message: "database object reconciliation failed",
						Extensions: []cnpgv1.DatabaseObjectStatus{
							{Name: "missing_ext", Applied: false, Message: "ERROR: extension \"missing_ext\" is not available (SQLSTATE 0A000)"},
						},
					},
				},
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{Name: "primary-analytics", Namespace: "dbs"},
					Status:     cnpgv1.DatabaseStatus{Applied: boolPtr(true)},
				},
			},
			wantNotReady: []string{"payments"},
			wantReasons: map[string]string{
				"payments": "extension \"missing_ext\": ERROR: extension \"missing_ext\" is not available (SQLSTATE 0A000)",
			},
		},
	}

	for _, tst := range tests {

		t.Run(tst.name, func(t *testing.T) {
			c := testClient(t, scheme, tst.objects...)

			reasons := make(map[string]string)
			got, err := verifyDatabasesReady(context.Background(), c, postgresDB, reasons)

			if tst.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tst.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tst.wantNotReady, got)
			assert.Equal(t, tst.wantReasons, reasons)
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

func TestAdoptResourceNilAnnotations(t *testing.T) {
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
			Name:      "primary-payments-config",
			Namespace: "dbs",
			// no annotations — must not panic
		},
	}
	c := testClient(t, scheme, postgresDB, configMap)

	err := adoptResource(context.Background(), c, scheme, postgresDB, configMap)

	require.NoError(t, err)
	updated := &corev1.ConfigMap{}
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: configMap.Name, Namespace: configMap.Namespace}, updated))
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

	t.Run("orphanSecrets leaves external secrets untouched", func(t *testing.T) {
		// The external secret deliberately shares the derived name to exercise the
		// worst case: without the PasswordConfig guard we would mutate a secret we
		// do not own.
		external := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-payments-admin",
				Namespace: "dbs",
				Labels:    map[string]string{labelCNPGReload: "true"},
			},
			Data: map[string][]byte{secretKeyUsername: []byte("u"), secretKeyPassword: []byte("p")},
		}
		c := testClient(t, scheme, external)
		externalDatabases := []enterprisev4.DatabaseDefinition{{
			Name: "payments",
			PasswordConfig: &enterprisev4.PasswordConfig{
				ExternalAdminSecretRef: corev1.LocalObjectReference{Name: "primary-payments-admin"},
				ExternalRWSecretRef:    corev1.LocalObjectReference{Name: "external-rw"},
			},
		}}

		require.NoError(t, orphanSecrets(context.Background(), c, postgresDB, externalDatabases))

		updated := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: external.Name, Namespace: external.Namespace}, updated))
		assert.NotContains(t, updated.Annotations, annotationRetainedFrom, "external secret must not be annotated by retention")
		assert.Equal(t, external, updated, "external secret must be left byte-for-byte untouched")
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

	t.Run("deleteSecrets never deletes external secrets", func(t *testing.T) {
		// Name collides with the derived admin secret name on purpose: this is the
		// exact data-loss case the PasswordConfig guard prevents.
		external := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "primary-payments-admin", Namespace: "dbs"},
			Data:       map[string][]byte{secretKeyPassword: []byte("p")},
		}
		c := testClient(t, scheme, external)
		externalDatabases := []enterprisev4.DatabaseDefinition{{
			Name: "payments",
			PasswordConfig: &enterprisev4.PasswordConfig{
				ExternalAdminSecretRef: corev1.LocalObjectReference{Name: "primary-payments-admin"},
				ExternalRWSecretRef:    corev1.LocalObjectReference{Name: "external-rw"},
			},
		}}

		require.NoError(t, deleteSecrets(context.Background(), c, postgresDB, externalDatabases))

		survivor := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: external.Name, Namespace: external.Namespace}, survivor),
			"external secret must survive PostgresDatabase deletion")
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
func TestCreateRoleSecret(t *testing.T) {
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
		wantRolename := roleName
		wantOwnerUID := postgresDB.UID
		wantPasswordLength := passwordLength
		wantPasswordDigits := passwordDigits
		c := testClient(t, scheme)

		err := createRoleSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, secretName, got.Name)
		assert.Equal(t, postgresDB.Namespace, got.Namespace)
		assert.Equal(t, wantManagedBy, got.Labels[labelManagedBy])
		assert.Equal(t, wantReload, got.Labels[labelCNPGReload])
		assert.Equal(t, wantRolename, string(got.Data["username"]))
		assertGeneratedPassword(t, string(got.Data[secretKeyPassword]), wantPasswordLength, wantPasswordDigits)
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, wantOwnerUID, got.OwnerReferences[0].UID)
	})

	t.Run("returns nil when secret already exists", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		wantRolename := roleName
		wantPassword := "existing-password"
		existing := buildPasswordSecret(postgresDB, secretName, wantRolename, wantPassword)
		c := testClient(t, scheme, existing)

		err := createRoleSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, wantRolename, string(got.Data["username"]))
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
		wantRolename := roleName
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
		assert.Equal(t, wantRolename, string(got.Data["username"]))
		assertGeneratedPassword(t, string(got.Data[secretKeyPassword]), wantPasswordLength, wantPasswordDigits)
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, wantOwnerUID, got.OwnerReferences[0].UID)
	})

	t.Run("re-adopts retained secret", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		wantRolename := roleName
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
				"username":        []byte(wantRolename),
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
		assert.Equal(t, wantRolename, string(got.Data["username"]))
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
		wantRolename := roleName
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
					{
						APIVersion:         enterprisev4.GroupVersion.String(),
						Kind:               "PostgresDatabase",
						Name:               postgresDB.Name,
						UID:                wantOwnerUID,
						Controller:         boolPtr(true),
						BlockOwnerDeletion: boolPtr(true),
					},
				},
			},
			Data: map[string][]byte{
				"username":        []byte(wantRolename),
				secretKeyPassword: []byte(wantPassword),
			},
		}
		c := testClient(t, scheme, existing)

		err := ensureSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, wantKeep, got.Annotations["keep"])
		assert.Equal(t, wantRolename, string(got.Data["username"]))
		assert.Equal(t, wantPassword, string(got.Data[secretKeyPassword]))
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, wantOwnerUID, got.OwnerReferences[0].UID)
	})

	t.Run("returns drift error when a previously provisioned secret is missing", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		c := testClient(t, scheme)

		err := ensureProvisionedSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.Error(t, err)
		var driftErr secretReconcileError
		require.ErrorAs(t, err, &driftErr)
		assert.Equal(t, reasonSecretsDriftDetected, driftErr.reason)
		assert.ErrorContains(t, err, secretName)
	})

	t.Run("re-attaches owner reference when ownership was manually stripped", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: postgresDB.Namespace,
				Labels: map[string]string{
					labelManagedBy:  "splunk-operator",
					labelCNPGReload: "true",
				},
				Annotations: map[string]string{"keep": "true"},
			},
			Data: map[string][]byte{
				"username":        []byte(roleName),
				secretKeyPassword: []byte("existing-password"),
			},
		}
		c := testClient(t, scheme, existing)

		err := ensureProvisionedSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, "true", got.Annotations["keep"])
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, postgresDB.UID, got.OwnerReferences[0].UID)
	})

	t.Run("accepts an existing secret with mutated data without rewriting it", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		wantUsername := "wrong_user"
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: postgresDB.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{UID: postgresDB.UID, Name: postgresDB.Name},
				},
			},
			Data: map[string][]byte{
				"username":        []byte(wantUsername),
				secretKeyPassword: []byte("existing-password"),
			},
		}
		c := testClient(t, scheme, existing)

		err := ensureProvisionedSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, wantUsername, string(got.Data["username"]))
		assert.Equal(t, "existing-password", string(got.Data[secretKeyPassword]))
	})

	t.Run("returns drift error when secret is owned by a different controller", func(t *testing.T) {
		roleName := "payments_admin"
		secretName := "primary-payments-admin"
		otherOwnerUID := types.UID("other-owner-uid")
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: postgresDB.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "v1",
						Kind:               "SomeOtherController",
						Name:               "other-controller",
						UID:                otherOwnerUID,
						Controller:         boolPtr(true),
						BlockOwnerDeletion: boolPtr(true),
					},
				},
			},
			Data: map[string][]byte{
				"username":        []byte(roleName),
				secretKeyPassword: []byte("existing-password"),
			},
		}
		c := testClient(t, scheme, existing)

		err := ensureProvisionedSecret(context.Background(), c, scheme, postgresDB, roleName, secretName)

		require.Error(t, err)
		var driftErr secretReconcileError
		require.ErrorAs(t, err, &driftErr)
		assert.Equal(t, reasonSecretsDriftDetected, driftErr.reason)
		assert.ErrorContains(t, err, secretName)
	})
}

// no cnpg reload label added
func createExternalSecrets(t *testing.T, c client.Client, secretNames []string, namespace string, labels []map[string]string, data []map[string][]byte) error {
	for i, secretName := range secretNames {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			}}
		if data != nil && data[i] != nil {
			secret.Data = data[i]
		}
		if labels != nil && labels[i] != nil {
			secret.Labels = labels[i]
		}
		if err := c.Create(t.Context(), secret); err != nil {
			return err
		}
	}
	return nil
}

// Uses a fake client because the helper reconciles multiple Secret objects through the Kubernetes API.
func TestReconcileRoleSecrets(t *testing.T) {
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

	provideExternalSecretsPostgresDB := func() *enterprisev4.PostgresDatabase {
		return &enterprisev4.PostgresDatabase{
			TypeMeta: metav1.TypeMeta{
				APIVersion: enterprisev4.GroupVersion.String(),
				Kind:       "PostgresDatabase",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary",
				Namespace: "dbs",
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				Databases: []enterprisev4.DatabaseDefinition{
					{Name: "payments", PasswordConfig: &enterprisev4.PasswordConfig{
						ExternalAdminSecretRef: corev1.LocalObjectReference{Name: ""},
						ExternalRWSecretRef:    corev1.LocalObjectReference{Name: ""},
					}},
					{Name: "analytics", PasswordConfig: &enterprisev4.PasswordConfig{
						ExternalAdminSecretRef: corev1.LocalObjectReference{Name: ""},
						ExternalRWSecretRef:    corev1.LocalObjectReference{Name: ""},
					}},
				},
			},
		}
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

		err := reconcileRoleSecrets(context.Background(), c, scheme, postgresDB, existingDatabaseStatus(postgresDB))

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

		require.NoError(t, reconcileRoleSecrets(context.Background(), c, scheme, postgresDB, existingDatabaseStatus(postgresDB)))

		before := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "primary-payments-admin", Namespace: postgresDB.Namespace}, before))
		beforePassword := append([]byte(nil), before.Data[secretKeyPassword]...)

		err := reconcileRoleSecrets(context.Background(), c, scheme, postgresDB, existingDatabaseStatus(postgresDB))

		require.NoError(t, err)

		after := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "primary-payments-admin", Namespace: postgresDB.Namespace}, after))
		assert.Equal(t, beforePassword, after.Data[secretKeyPassword])
		require.Len(t, after.OwnerReferences, 1)
		assert.Equal(t, postgresDB.UID, after.OwnerReferences[0].UID)
	})

	for _, phase := range []reconcileDBPhases{provisioningDBPhase, failedDBPhase, readyDBPhase} {
		t.Run("does not recreate missing secrets for previously provisioned databases in phase "+string(phase), func(t *testing.T) {
			existing := postgresDB.DeepCopy()
			existing.Status.Phase = strPtr(string(phase))
			existing.Status.Databases = []enterprisev4.DatabaseInfo{{Name: "payments"}}
			c := testClient(t, scheme)

			err := reconcileRoleSecrets(context.Background(), c, scheme, existing, existingDatabaseStatus(existing))

			require.Error(t, err)
			var driftErr secretReconcileError
			require.ErrorAs(t, err, &driftErr)
			assert.Equal(t, reasonSecretsDriftDetected, driftErr.reason)
			assert.Error(t, c.Get(t.Context(), types.NamespacedName{
				Name:      "primary-payments-admin",
				Namespace: existing.Namespace,
			}, &corev1.Secret{}))
		})
	}

	t.Run("returns error when names are empty", func(t *testing.T) {

		externalSecretsPostgresDB := provideExternalSecretsPostgresDB()
		externalSecretsPostgresDB.Status.Phase = strPtr(string(readyDBPhase))
		externalSecretsPostgresDB.Status.Databases = []enterprisev4.DatabaseInfo{{Name: "payments"}}

		c := testClient(t, scheme)

		var invalidSecretErr secretReconcileError
		err := reconcileRoleSecrets(t.Context(), c, scheme, externalSecretsPostgresDB, existingDatabaseStatus(externalSecretsPostgresDB))

		require.ErrorAs(t, err, &invalidSecretErr)
		assert.Equal(t, reasonExternalSecretInvalid, invalidSecretErr.reason)
	})

	t.Run("returns secret missing when k8s api cant fetch", func(t *testing.T) {

		externalSecretsPostgresDB := provideExternalSecretsPostgresDB()
		externalSecretsPostgresDB.Status.Phase = strPtr(string(readyDBPhase))
		externalSecretsPostgresDB.Status.Databases = []enterprisev4.DatabaseInfo{{Name: "payments"}}

		externalSecretNames := []string{
			"external-admin-secret",
			"external-rw-secret",
		}
		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretNames[0]
		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalRWSecretRef.Name = externalSecretNames[1]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretNames[0]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalRWSecretRef.Name = externalSecretNames[1]

		c := testClient(t, scheme)

		err := reconcileRoleSecrets(t.Context(), c, scheme, externalSecretsPostgresDB, existingDatabaseStatus(externalSecretsPostgresDB))
		require.Error(t, err)

		var missingSecretErr secretReconcileError
		require.ErrorAs(t, err, &missingSecretErr)
		assert.Equal(t, reasonExternalSecretMissing, missingSecretErr.reason)

		// Both the admin and RW secrets are missing for the same database, so the
		// combined error must report both rather than only the admin one.
		assert.Contains(t, err.Error(), externalSecretNames[0])
		assert.Contains(t, err.Error(), externalSecretNames[1])
	})

	t.Run("succeeds with user-set reload label, never mutating the external secret", func(t *testing.T) {
		externalSecretsPostgresDB := provideExternalSecretsPostgresDB()
		externalSecretsPostgresDB.Status.Phase = strPtr(string(readyDBPhase))
		externalSecretsPostgresDB.Status.Databases = []enterprisev4.DatabaseInfo{{Name: "payments"}}

		externalSecretNames := []string{
			"external-admin-secret",
			"external-rw-secret",
		}
		key := "example"
		value := "karpatka"
		// The user/owner is responsible for setting cnpg.io/reload — we only validate it.
		exampleLabels := []map[string]string{
			{key: value, labelCNPGReload: "true"},
			{key: value, labelCNPGReload: "true"},
		}
		exampleDataValue := "kwas"
		exampleData := []map[string][]byte{
			{
				secretKeyUsername: []byte(exampleDataValue),
				secretKeyPassword: []byte(exampleDataValue),
			},
			{
				secretKeyPassword: []byte(exampleDataValue),
				secretKeyUsername: []byte(exampleDataValue),
			},
		}
		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretNames[0]
		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalRWSecretRef.Name = externalSecretNames[1]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretNames[0]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalRWSecretRef.Name = externalSecretNames[1]

		c := testClient(t, scheme)
		createExternalSecrets(t, c, externalSecretNames, externalSecretsPostgresDB.Namespace, exampleLabels, exampleData)

		err := reconcileRoleSecrets(t.Context(), c, scheme, externalSecretsPostgresDB, existingDatabaseStatus(externalSecretsPostgresDB))
		require.NoError(t, err)

		for _, secretName := range externalSecretNames {
			got := &corev1.Secret{}
			require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
			assert.Equal(t, exampleDataValue, string(got.Data[secretKeyUsername]))
			assert.Equal(t, exampleDataValue, string(got.Data[secretKeyPassword]))
			assert.Equal(t, "true", got.Labels["cnpg.io/reload"])
			assert.Equal(t, value, got.Labels[key])
		}
	})

	t.Run("fails when external secret is missing the reload label — user must set it", func(t *testing.T) {
		externalSecretsPostgresDB := provideExternalSecretsPostgresDB()
		externalSecretsPostgresDB.Status.Phase = strPtr(string(readyDBPhase))
		externalSecretsPostgresDB.Status.Databases = []enterprisev4.DatabaseInfo{{Name: "payments"}}

		externalSecretNames := []string{
			"external-admin-nolabel",
			"external-rw-nolabel",
		}
		validData := []map[string][]byte{
			{secretKeyUsername: []byte("u"), secretKeyPassword: []byte("p")},
			{secretKeyUsername: []byte("u"), secretKeyPassword: []byte("p")},
		}
		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretNames[0]
		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalRWSecretRef.Name = externalSecretNames[1]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretNames[0]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalRWSecretRef.Name = externalSecretNames[1]

		c := testClient(t, scheme)
		// Valid data but no cnpg.io/reload label.
		createExternalSecrets(t, c, externalSecretNames, externalSecretsPostgresDB.Namespace, nil, validData)

		err := reconcileRoleSecrets(t.Context(), c, scheme, externalSecretsPostgresDB, existingDatabaseStatus(externalSecretsPostgresDB))
		require.Error(t, err)

		var invalidSecretErr secretReconcileError
		require.ErrorAs(t, err, &invalidSecretErr)
		assert.Equal(t, reasonExternalSecretMissingLabel, invalidSecretErr.reason)

		// The operator must not have added the label behind the user's back.
		for _, secretName := range externalSecretNames {
			got := &corev1.Secret{}
			require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: secretName, Namespace: postgresDB.Namespace}, got))
			assert.NotContains(t, got.Labels, labelCNPGReload)
		}
	})

	t.Run("signals the external secret being invalid, missing data keys or data map", func(t *testing.T) {
		externalSecretsPostgresDB := provideExternalSecretsPostgresDB()
		externalSecretsPostgresDB.Status.Phase = strPtr(string(readyDBPhase))
		externalSecretsPostgresDB.Status.Databases = []enterprisev4.DatabaseInfo{{Name: "payments"}}

		externalSecretsNoDataMap := []string{
			"external-admin-missingdata",
			"external-rw-missingdata",
		}

		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretsNoDataMap[0]
		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalRWSecretRef.Name = externalSecretsNoDataMap[1]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretsNoDataMap[0]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalRWSecretRef.Name = externalSecretsNoDataMap[1]

		c := testClient(t, scheme)
		createExternalSecrets(t, c, externalSecretsNoDataMap, externalSecretsPostgresDB.Namespace, nil, nil)

		err := reconcileRoleSecrets(t.Context(), c, scheme, externalSecretsPostgresDB, existingDatabaseStatus(externalSecretsPostgresDB))

		require.Error(t, err)
		var invalidSecretErr secretReconcileError
		require.ErrorAs(t, err, &invalidSecretErr)
		assert.Equal(t, reasonExternalSecretMissingData, invalidSecretErr.reason)

		externalSecretDataMapMissingKey := []string{
			"external-admin-missing-user-key",
			"external-rw-missing-user-key",
		}

		exampleDataValue := "kwas"
		exampleData := []map[string][]byte{
			{
				secretKeyPassword: []byte(exampleDataValue),
			},
			{
				secretKeyPassword: []byte(exampleDataValue),
				secretKeyUsername: []byte(exampleDataValue),
			},
		}

		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretDataMapMissingKey[0]
		externalSecretsPostgresDB.Spec.Databases[0].PasswordConfig.ExternalRWSecretRef.Name = externalSecretDataMapMissingKey[1]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalAdminSecretRef.Name = externalSecretDataMapMissingKey[0]
		externalSecretsPostgresDB.Spec.Databases[1].PasswordConfig.ExternalRWSecretRef.Name = externalSecretDataMapMissingKey[1]

		createExternalSecrets(t, c, externalSecretDataMapMissingKey, externalSecretsPostgresDB.Namespace, nil, exampleData)

		err = reconcileRoleSecrets(t.Context(), c, scheme, externalSecretsPostgresDB, existingDatabaseStatus(externalSecretsPostgresDB))

		require.Error(t, err)
		require.ErrorAs(t, err, &invalidSecretErr)
		assert.Equal(t, reasonExternalSecretMissingKeys, invalidSecretErr.reason)
	})
}

// Uses a fake client because the helper reconciles ConfigMaps through CreateOrUpdate and persists re-adoption metadata.
func TestReconcileRoleConfigMaps(t *testing.T) {
	scheme := testScheme(t)
	endpoints := clusterEndpoints{
		RWHost:       "rw.default.svc.cluster.local",
		ROHost:       "ro.default.svc.cluster.local",
		RHost:        "r.default.svc.cluster.local",
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
		wantPaymentsData := mustBuildDatabaseConfigMapData(t, "payments", endpoints)
		wantAnalyticsData := mustBuildDatabaseConfigMapData(t, "analytics", endpoints)
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
		wantData := mustBuildDatabaseConfigMapData(t, "payments", endpoints)
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
				ConfigMapKeyDatabaseName: "stale",
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

	t.Run("re-attaches owner reference when configmap ownership was manually stripped", func(t *testing.T) {
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
		existing := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:        cmName,
				Namespace:   postgresDB.Namespace,
				Labels:      map[string]string{labelManagedBy: "splunk-operator"},
				Annotations: map[string]string{"keep": "true"},
			},
			Data: map[string]string{ConfigMapKeyDatabaseName: "payments"},
		}
		c := testClient(t, scheme, existing)

		err := reconcileRoleConfigMaps(context.Background(), c, scheme, postgresDB, endpoints)

		require.NoError(t, err)

		got := &corev1.ConfigMap{}
		require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: cmName, Namespace: postgresDB.Namespace}, got))
		assert.Equal(t, "true", got.Annotations["keep"])
		require.Len(t, got.OwnerReferences, 1)
		assert.Equal(t, postgresDB.UID, got.OwnerReferences[0].UID)
		assert.Equal(t, mustBuildDatabaseConfigMapData(t, "payments", endpoints), got.Data)
	})

	t.Run("fails when endpoints are incomplete", func(t *testing.T) {
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
				Databases: []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			},
		}
		c := testClient(t, scheme)

		err := reconcileRoleConfigMaps(context.Background(), c, scheme, postgresDB, clusterEndpoints{
			RWHost: "rw.default.svc.cluster.local",
			ROHost: "ro.default.svc.cluster.local",
		})

		require.Error(t, err)
		assert.ErrorContains(t, err, "RHost is required")
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
	wantRolename := "payments_admin"
	wantPassword := "topsecret"

	got := buildPasswordSecret(postgresDB, wantName, wantRolename, wantPassword)

	assert.Equal(t, wantName, got.Name)
	assert.Equal(t, wantNamespace, got.Namespace)
	assert.Equal(t, wantManagedBy, got.Labels[labelManagedBy])
	assert.Equal(t, wantReload, got.Labels[labelCNPGReload])
	assert.Equal(t, wantRolename, string(got.Data["username"]))
	assert.Equal(t, wantPassword, string(got.Data[secretKeyPassword]))
}

func TestBuildCNPGDatabaseSpec(t *testing.T) {
	tests := []struct {
		name       string
		db         enterprisev4.DatabaseDefinition
		extensions []cnpgv1.ExtensionSpec
		want       cnpgv1.DatabaseSpec
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
		{
			name: "passes extensions through unchanged",
			db:   enterprisev4.DatabaseDefinition{Name: "myapp"},
			extensions: []cnpgv1.ExtensionSpec{
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
			},
			want: cnpgv1.DatabaseSpec{
				Name:          "myapp",
				Owner:         "myapp_admin",
				ClusterRef:    corev1.LocalObjectReference{Name: "cnpg-primary"},
				ReclaimPolicy: cnpgv1.DatabaseReclaimDelete,
				Extensions: []cnpgv1.ExtensionSpec{
					{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
				},
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			got := buildCNPGDatabaseSpec("cnpg-primary", tst.db, tst.extensions)
			assert.Equal(t, tst.want, got)
		})
	}
}

func TestReconcileExtensions(t *testing.T) {
	tests := []struct {
		name     string
		desired  []string
		existing []cnpgv1.ExtensionSpec
		want     []cnpgv1.ExtensionSpec
	}{
		{
			name:    "no extensions returns nil",
			desired: nil,
			want:    nil,
		},
		{
			name:    "desired extensions all marked present",
			desired: []string{"pg_trgm", "unaccent"},
			want: []cnpgv1.ExtensionSpec{
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsurePresent}},
			},
		},
		{
			name:    "removed extension marked absent",
			desired: []string{"pg_trgm"},
			existing: []cnpgv1.ExtensionSpec{
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsurePresent}},
			},
			want: []cnpgv1.ExtensionSpec{
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsureAbsent}},
			},
		},
		{
			name:    "already absent extension persisted",
			desired: []string{},
			existing: []cnpgv1.ExtensionSpec{
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsureAbsent}},
			},
			want: []cnpgv1.ExtensionSpec{
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsureAbsent}},
			},
		},
		{
			name:    "all extensions removed marks all absent",
			desired: []string{},
			existing: []cnpgv1.ExtensionSpec{
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsurePresent}},
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsurePresent}},
			},
			want: []cnpgv1.ExtensionSpec{
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "pg_trgm", Ensure: cnpgv1.EnsureAbsent}},
				{DatabaseObjectSpec: cnpgv1.DatabaseObjectSpec{Name: "unaccent", Ensure: cnpgv1.EnsureAbsent}},
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			got := reconcileExtensions(tst.desired, tst.existing)
			assert.Equal(t, tst.want, got)
		})
	}
}

func TestBuildDatabaseConfigMapData(t *testing.T) {
	tests := []struct {
		name      string
		endpoints clusterEndpoints
		want      map[string]string
		wantError string
	}{
		{
			name: "without pooler endpoints",
			endpoints: clusterEndpoints{
				RWHost: "rw.default.svc.cluster.local",
				ROHost: "ro.default.svc.cluster.local",
				RHost:  "r.default.svc.cluster.local",
			},
			want: map[string]string{
				ConfigMapKeyDatabaseName:         "payments",
				pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
				pgconninfo.KeyClusterRWEndpoint:  "rw.default.svc.cluster.local",
				pgconninfo.KeyClusterROEndpoint:  "ro.default.svc.cluster.local",
				pgconninfo.KeyClusterREndpoint:   "r.default.svc.cluster.local",
				ConfigMapKeyAdminUser:            "payments_admin",
				ConfigMapKeyRWUser:               "payments_rw",
			},
		},
		{
			name: "includes pooler endpoints when available",
			endpoints: clusterEndpoints{
				RWHost:       "rw.default.svc.cluster.local",
				ROHost:       "ro.default.svc.cluster.local",
				RHost:        "r.default.svc.cluster.local",
				PoolerRWHost: "pooler-rw.default.svc.cluster.local",
				PoolerROHost: "pooler-ro.default.svc.cluster.local",
			},
			want: map[string]string{
				ConfigMapKeyDatabaseName:         "payments",
				pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
				pgconninfo.KeyClusterRWEndpoint:  "rw.default.svc.cluster.local",
				pgconninfo.KeyClusterROEndpoint:  "ro.default.svc.cluster.local",
				pgconninfo.KeyClusterREndpoint:   "r.default.svc.cluster.local",
				ConfigMapKeyAdminUser:            "payments_admin",
				ConfigMapKeyRWUser:               "payments_rw",
				pgconninfo.KeyPoolerRWEndpoint:   "pooler-rw.default.svc.cluster.local",
				pgconninfo.KeyPoolerROEndpoint:   "pooler-ro.default.svc.cluster.local",
			},
		},
		{
			name: "fails when endpoints are incomplete",
			endpoints: clusterEndpoints{
				RWHost: "rw.default.svc.cluster.local",
				ROHost: "ro.default.svc.cluster.local",
			},
			wantError: "RHost is required",
		},
		{
			name: "publishes pooler keys with empty values when pooler enabled and a side is unavailable",
			endpoints: clusterEndpoints{
				RWHost:        "rw.default.svc.cluster.local",
				ROHost:        "ro.default.svc.cluster.local",
				RHost:         "r.default.svc.cluster.local",
				PoolerEnabled: true,
				PoolerRWHost:  "pooler-rw.default.svc.cluster.local",
			},
			want: map[string]string{
				ConfigMapKeyDatabaseName:         "payments",
				pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
				pgconninfo.KeyClusterRWEndpoint:  "rw.default.svc.cluster.local",
				pgconninfo.KeyClusterROEndpoint:  "ro.default.svc.cluster.local",
				pgconninfo.KeyClusterREndpoint:   "r.default.svc.cluster.local",
				ConfigMapKeyAdminUser:            "payments_admin",
				ConfigMapKeyRWUser:               "payments_rw",
				pgconninfo.KeyPoolerRWEndpoint:   "pooler-rw.default.svc.cluster.local",
				pgconninfo.KeyPoolerROEndpoint:   "",
			},
		},
		{
			name: "publishes empty ro endpoint when ro unavailable",
			endpoints: clusterEndpoints{
				RWHost:        "rw.default.svc.cluster.local",
				RHost:         "r.default.svc.cluster.local",
				ROUnavailable: true,
			},
			want: map[string]string{
				ConfigMapKeyDatabaseName:         "payments",
				pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
				pgconninfo.KeyClusterRWEndpoint:  "rw.default.svc.cluster.local",
				pgconninfo.KeyClusterROEndpoint:  "",
				pgconninfo.KeyClusterREndpoint:   "r.default.svc.cluster.local",
				ConfigMapKeyAdminUser:            "payments_admin",
				ConfigMapKeyRWUser:               "payments_rw",
			},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			got, required, err := buildDatabaseConfigMapData("payments", tst.endpoints)
			if tst.wantError == "" {
				require.NoError(t, err)
				assert.Equal(t, tst.want, got)
				assert.ElementsMatch(t, append(pgconninfo.RequiredKeys(),
					ConfigMapKeyDatabaseName,
					ConfigMapKeyAdminUser,
					ConfigMapKeyRWUser,
				), required)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tst.wantError)
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
		wantError string
	}{
		{
			name:    "without connection pooler",
			cluster: &enterprisev4.PostgresCluster{},
			cnpg: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"},
				Status: cnpgv1.ClusterStatus{
					WriteService:   "primary-rw",
					ReadService:    "primary-ro",
					ReadyInstances: 2,
				},
			},
			namespace: "dbs",
			want: clusterEndpoints{
				RWHost: "primary-rw.dbs.svc.cluster.local",
				ROHost: "primary-ro.dbs.svc.cluster.local",
				RHost:  "cnpg-primary-r.dbs.svc.cluster.local",
			},
		},
		{
			name:      "fails when CNPG service names are not available",
			cluster:   &enterprisev4.PostgresCluster{},
			cnpg:      &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"}},
			namespace: "dbs",
			wantError: "write service name is required",
		},
		{
			name: "with connection pooler and both endpoints reconciled",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					ConnectionPoolerStatus: &enterprisev4.ConnectionPoolerStatus{
						Enabled:          true,
						ReadWriteEnabled: true,
						ReadOnlyEnabled:  true,
					},
				},
			},
			cnpg: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"},
				Spec:       cnpgv1.ClusterSpec{Instances: 2},
				Status: cnpgv1.ClusterStatus{
					WriteService:   "primary-rw",
					ReadService:    "primary-ro",
					ReadyInstances: 2,
				},
			},
			namespace: "dbs",
			want: clusterEndpoints{
				RWHost:        "primary-rw.dbs.svc.cluster.local",
				ROHost:        "primary-ro.dbs.svc.cluster.local",
				RHost:         "cnpg-primary-r.dbs.svc.cluster.local",
				PoolerEnabled: true,
				PoolerRWHost:  "cnpg-primary-pooler-rw.dbs.svc.cluster.local",
				PoolerROHost:  "cnpg-primary-pooler-ro.dbs.svc.cluster.local",
			},
		},
		{
			name: "with connection pooler but RO disabled in status omits PoolerROHost",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					ConnectionPoolerStatus: &enterprisev4.ConnectionPoolerStatus{
						Enabled:          true,
						ReadWriteEnabled: true,
					},
				},
			},
			cnpg: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"},
				Spec:       cnpgv1.ClusterSpec{Instances: 2},
				Status: cnpgv1.ClusterStatus{
					WriteService:   "primary-rw",
					ReadService:    "primary-ro",
					ReadyInstances: 2,
				},
			},
			namespace: "dbs",
			want: clusterEndpoints{
				RWHost:        "primary-rw.dbs.svc.cluster.local",
				ROHost:        "primary-ro.dbs.svc.cluster.local",
				RHost:         "cnpg-primary-r.dbs.svc.cluster.local",
				PoolerEnabled: true,
				PoolerRWHost:  "cnpg-primary-pooler-rw.dbs.svc.cluster.local",
			},
		},
		{
			name: "with connection pooler but RW disabled in status omits PoolerRWHost",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					ConnectionPoolerStatus: &enterprisev4.ConnectionPoolerStatus{
						Enabled:         true,
						ReadOnlyEnabled: true,
					},
				},
			},
			cnpg: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"},
				Spec:       cnpgv1.ClusterSpec{Instances: 2},
				Status: cnpgv1.ClusterStatus{
					WriteService:   "primary-rw",
					ReadService:    "primary-ro",
					ReadyInstances: 2,
				},
			},
			namespace: "dbs",
			want: clusterEndpoints{
				RWHost:        "primary-rw.dbs.svc.cluster.local",
				ROHost:        "primary-ro.dbs.svc.cluster.local",
				RHost:         "cnpg-primary-r.dbs.svc.cluster.local",
				PoolerEnabled: true,
				PoolerROHost:  "cnpg-primary-pooler-ro.dbs.svc.cluster.local",
			},
		},
		{
			name: "clears ROHost when fewer than two instances are ready",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					ConnectionPoolerStatus: &enterprisev4.ConnectionPoolerStatus{
						Enabled:          true,
						ReadWriteEnabled: true,
						ReadOnlyEnabled:  true,
					},
				},
			},
			cnpg: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"},
				Status: cnpgv1.ClusterStatus{
					WriteService:   "primary-rw",
					ReadService:    "primary-ro",
					ReadyInstances: 1,
				},
			},
			namespace: "dbs",
			want: clusterEndpoints{
				RWHost:        "primary-rw.dbs.svc.cluster.local",
				RHost:         "cnpg-primary-r.dbs.svc.cluster.local",
				ROUnavailable: true,
				PoolerEnabled: true,
				PoolerRWHost:  "cnpg-primary-pooler-rw.dbs.svc.cluster.local",
			},
		},
		{
			name: "clears ROHost and PoolerROHost when no instances are ready",
			cluster: &enterprisev4.PostgresCluster{
				Status: enterprisev4.PostgresClusterStatus{
					ConnectionPoolerStatus: &enterprisev4.ConnectionPoolerStatus{
						Enabled:          true,
						ReadWriteEnabled: true,
						ReadOnlyEnabled:  true,
					},
				},
			},
			cnpg: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cnpg-primary"},
				Status: cnpgv1.ClusterStatus{
					WriteService:   "primary-rw",
					ReadService:    "primary-ro",
					ReadyInstances: 0,
				},
			},
			namespace: "dbs",
			want: clusterEndpoints{
				RWHost:        "primary-rw.dbs.svc.cluster.local",
				RHost:         "cnpg-primary-r.dbs.svc.cluster.local",
				ROUnavailable: true,
				PoolerEnabled: true,
				PoolerRWHost:  "cnpg-primary-pooler-rw.dbs.svc.cluster.local",
			},
		},
	}

	for _, tst := range tests {

		t.Run(tst.name, func(t *testing.T) {
			got, err := resolveClusterEndpoints(tst.cluster, tst.cnpg, tst.namespace)
			if tst.wantError == "" {
				require.NoError(t, err)
				assert.Equal(t, tst.want, got)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tst.wantError)
		})
	}
}

func mustBuildDatabaseConfigMapData(t *testing.T, dbName string, endpoints clusterEndpoints) map[string]string {
	t.Helper()

	data, _, err := buildDatabaseConfigMapData(dbName, endpoints)
	require.NoError(t, err)
	return data
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
		Status: enterprisev4.PostgresDatabaseStatus{
			Databases: []enterprisev4.DatabaseInfo{{Name: "payments"}},
		},
	}
	want := []enterprisev4.DatabaseInfo{
		{
			Name:        "payments",
			Ready:       true,
			DatabaseRef: &corev1.LocalObjectReference{Name: "primary-payments"},
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
			Name:        "analytics",
			Ready:       true,
			DatabaseRef: &corev1.LocalObjectReference{Name: "primary-analytics"},
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

// TestPersistDatabaseMessages verifies the message overlay: a database named in reasons is
// marked not-ready with its message, one absent from the map is cleared and restored to ready,
// and Roles and the DatabaseRef provisioning marker hasNewDatabases relies on are never touched.
func TestPersistDatabaseMessages(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	requestName := types.NamespacedName{Name: "primary", Namespace: "dbs"}

	build := func() *enterprisev4.PostgresDatabase {
		return &enterprisev4.PostgresDatabase{
			TypeMeta:   metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresDatabase"},
			ObjectMeta: metav1.ObjectMeta{Name: requestName.Name, Namespace: requestName.Namespace, Generation: 1},
			Status: enterprisev4.PostgresDatabaseStatus{
				Databases: []enterprisev4.DatabaseInfo{
					{Name: "payments", Ready: true, DatabaseRef: &corev1.LocalObjectReference{Name: "primary-payments"}, Message: "extension \"pgcrypto\": not available", Roles: []enterprisev4.DatabaseRoleInfo{{Name: "payments_admin"}, {Name: "payments_rw"}}},
					{Name: "analytics", Ready: true, DatabaseRef: &corev1.LocalObjectReference{Name: "primary-analytics"}, Roles: []enterprisev4.DatabaseRoleInfo{{Name: "analytics_admin"}, {Name: "analytics_rw"}}},
				},
			},
		}
	}

	// payments absent from the map is cleared and stays ready; analytics is reasoned so it flips
	// not-ready. Roles and DatabaseRef survive on both, so hasNewDatabases still sees them as provisioned.
	db := build()
	c := testClient(t, scheme, db)
	require.NoError(t, persistDatabaseMessages(ctx, c, db, map[string]string{"analytics": "Waiting for CNPG to apply the database"}))

	updated := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, requestName, updated))
	require.Len(t, updated.Status.Databases, 2)
	assert.Empty(t, updated.Status.Databases[0].Message, "recovered database must not retain its stale message")
	assert.True(t, updated.Status.Databases[0].Ready, "cleared database stays ready")
	assert.Len(t, updated.Status.Databases[0].Roles, 2, "overlay must not touch Roles")
	assert.Equal(t, "Waiting for CNPG to apply the database", updated.Status.Databases[1].Message)
	assert.False(t, updated.Status.Databases[1].Ready, "reasoned database is marked not-ready")
	assert.NotNil(t, updated.Status.Databases[1].DatabaseRef, "provisioning marker survives a not-ready blip")
	assert.False(t, hasNewDatabases(&enterprisev4.PostgresDatabase{
		Spec:   enterprisev4.PostgresDatabaseSpec{Databases: []enterprisev4.DatabaseDefinition{{Name: "payments"}, {Name: "analytics"}}},
		Status: updated.Status,
	}), "a reasoned not-ready database must not re-trigger the privileges phase")

	// A nil map clears every message and restores readiness, even on an entry left not-ready by
	// an earlier requeue — otherwise it could linger ready:false with no message once the
	// aggregate DatabasesReady condition flips true.
	db2 := build()
	db2.Status.Databases[0].Ready = false
	c2 := testClient(t, scheme, db2)
	require.NoError(t, persistDatabaseMessages(ctx, c2, db2, nil))

	cleared := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c2.Get(ctx, requestName, cleared))
	require.Len(t, cleared.Status.Databases, 2)
	assert.Empty(t, cleared.Status.Databases[0].Message)
	assert.True(t, cleared.Status.Databases[0].Ready, "clearing a message restores per-database readiness")
}

// TestPopulateDatabaseStatusPreservesMessage confirms the builder carries an existing not-ready
// message forward so a later message overlay is not the only thing keeping it alive.
func TestPopulateDatabaseStatusPreservesMessage(t *testing.T) {
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "primary"},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{
				{Name: "payments"},
				{Name: "analytics"},
			},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			Databases: []enterprisev4.DatabaseInfo{
				{Name: "payments", Ready: false, Message: "extension \"pgcrypto\": not available", Roles: []enterprisev4.DatabaseRoleInfo{{Name: "payments_admin"}, {Name: "payments_rw"}}},
				{Name: "analytics", Ready: false, Message: "Waiting for CNPG to apply the database", Roles: []enterprisev4.DatabaseRoleInfo{{Name: "analytics_admin"}, {Name: "analytics_rw"}}},
			},
		},
	}

	preserved := populateDatabaseStatusForDefinitions(postgresDB, postgresDB.Spec.Databases, false, true)
	require.Len(t, preserved, 2)
	assert.Equal(t, "extension \"pgcrypto\": not available", preserved[0].Message)
	assert.Equal(t, "Waiting for CNPG to apply the database", preserved[1].Message)
}

// TestReconcileClearsRecoveredMessageWhenLaterPhaseFails exercises the flow where a
// database recovers (DatabasesReady transitions to true) and a later phase then fails
// and returns before the final status write. The recovered database's stale message
// must already be cleared at the DatabasesReady transition.
func TestReconcileClearsRecoveredMessageWhenLaterPhaseFails(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	requestName := types.NamespacedName{Name: "primary", Namespace: "dbs"}

	postgresDB := &enterprisev4.PostgresDatabase{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresDatabase"},
		ObjectMeta: metav1.ObjectMeta{Name: requestName.Name, Namespace: requestName.Namespace, UID: types.UID("postgresdb-uid"), Generation: 1, Finalizers: []string{postgresDatabaseFinalizerName}},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary-cluster"},
			Databases:  []enterprisev4.DatabaseDefinition{{Name: "payments"}},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			// Stale terminal failure recovering: retryAfterStaleReconcileFailure runs the
			// privileges phase for the already-provisioned database.
			Phase:                strPtr(string(failedDBPhase)),
			ObservedGeneration:   int64Ptr(1),
			ReconcileFailureType: reconcileFailurePrivileges,
			// The database was not-ready last reconcile (message set, roles published),
			// so its message is preserved through credential provisioning and only the
			// DatabasesReady transition can clear it.
			Databases: []enterprisev4.DatabaseInfo{
				{
					Name:    "payments",
					Ready:   false,
					Message: "extension \"pgcrypto\": not available",
					Roles: []enterprisev4.DatabaseRoleInfo{
						{Name: adminRoleName("payments"), Exists: true},
						{Name: rwRoleName("payments"), Exists: true},
					},
				},
			},
		},
	}

	roleOwners := make(map[string]enterprisev4.RoleOwnerReference, len(getDesiredRoles(postgresDB)))
	for _, roleName := range getDesiredRoles(postgresDB) {
		roleOwners[roleName] = enterprisev4.RoleOwnerReference{Name: postgresDB.Name, UID: string(postgresDB.UID)}
	}

	postgresCluster := &enterprisev4.PostgresCluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "primary-cluster", Namespace: requestName.Namespace},
		Status: enterprisev4.PostgresClusterStatus{
			Phase:          strPtr(string(ClusterReady)),
			ProvisionerRef: &corev1.ObjectReference{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster", Name: "primary-cnpg", Namespace: requestName.Namespace},
			Resources: &enterprisev4.PostgresClusterResources{
				SuperUserSecretRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "primary-superuser"}, Key: secretKeyPassword},
			},
			ManagedRolesStatus: &enterprisev4.ManagedRolesStatus{Reconciled: getDesiredRoles(postgresDB), RoleOwners: roleOwners},
		},
	}
	cnpgCluster := &cnpgv1.Cluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: "Cluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "primary-cnpg", Namespace: requestName.Namespace},
		Status:     cnpgv1.ClusterStatus{WriteService: "primary-rw", ReadService: "primary-ro"},
	}
	superSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "primary-superuser", Namespace: requestName.Namespace},
		Data:       map[string][]byte{secretKeyPassword: []byte("supersecret")},
	}
	// CNPG database has recovered (Applied=true), so DatabasesReady transitions to true.
	cnpgDB := &cnpgv1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: cnpgDatabaseName(requestName.Name, "payments"), Namespace: requestName.Namespace},
		Status:     cnpgv1.DatabaseStatus{Applied: boolPtr(true)},
	}
	ownerRef := metav1.OwnerReference{APIVersion: enterprisev4.GroupVersion.String(), Kind: "PostgresDatabase", Name: requestName.Name, UID: postgresDB.UID, Controller: boolPtr(true), BlockOwnerDeletion: boolPtr(true)}
	adminSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: roleSecretName(requestName.Name, "payments", secretRoleAdmin), Namespace: requestName.Namespace, OwnerReferences: []metav1.OwnerReference{ownerRef}},
		Data:       map[string][]byte{"username": []byte(adminRoleName("payments")), secretKeyPassword: []byte("admin-password")},
	}
	rwSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: roleSecretName(requestName.Name, "payments", secretRoleRW), Namespace: requestName.Namespace, OwnerReferences: []metav1.OwnerReference{ownerRef}},
		Data:       map[string][]byte{"username": []byte(rwRoleName("payments")), secretKeyPassword: []byte("rw-password")},
	}

	c := testClient(t, scheme, postgresDB, postgresCluster, cnpgCluster, superSecret, cnpgDB, adminSecret, rwSecret)

	// Privilege grant fails terminally, so reconcile returns before the final status write.
	newDBRepo := func(_ context.Context, _, _ string, _ string) (DBRepo, error) {
		return nil, fmt.Errorf("%w: password authentication failed", ErrTerminal)
	}
	_, err := PostgresDatabaseService(ctx, &ReconcileContext{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10), Metrics: &pgprometheus.NoopRecorder{}}, postgresDB.DeepCopy(), newDBRepo)
	require.NoError(t, err)

	updated := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, requestName, updated))
	require.Len(t, updated.Status.Databases, 1)
	assert.Empty(t, updated.Status.Databases[0].Message, "recovered database must not retain its stale message after a later phase fails")
	// The terminal privileges failure is still reported on its own condition.
	assert.Equal(t, string(failedDBPhase), *updated.Status.Phase)
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

func TestDeletionTakesPrecedenceOverCurrentTerminalPrivilegesFailure(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	deletionTime := metav1.Now()
	generation := int64(7)
	postgresDB := &enterprisev4.PostgresDatabase{
		TypeMeta: metav1.TypeMeta{
			APIVersion: enterprisev4.GroupVersion.String(),
			Kind:       "PostgresDatabase",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "primary",
			Namespace:         "dbs",
			UID:               types.UID("postgresdb-uid"),
			Generation:        generation,
			DeletionTimestamp: &deletionTime,
			Finalizers:        []string{postgresDatabaseFinalizerName},
		},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "primary-cluster"},
			Databases:  []enterprisev4.DatabaseDefinition{{Name: "payments"}},
		},
		Status: enterprisev4.PostgresDatabaseStatus{
			Phase:                strPtr(string(failedDBPhase)),
			ObservedGeneration:   int64Ptr(generation),
			ReconcileFailureType: reconcileFailurePrivileges,
		},
	}
	c := testClient(t, scheme, postgresDB)
	repoCalls := 0
	newDBRepo := func(_ context.Context, _, _, _ string) (DBRepo, error) {
		repoCalls++
		return &stubDBRepo{}, nil
	}

	result, err := PostgresDatabaseService(
		ctx,
		&ReconcileContext{
			Client:   c,
			Scheme:   scheme,
			Recorder: record.NewFakeRecorder(10),
			Metrics:  &pgprometheus.NoopRecorder{},
		},
		postgresDB,
		newDBRepo,
	)

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	assert.Equal(t, 0, repoCalls)

	updated := &enterprisev4.PostgresDatabase{}
	err = c.Get(ctx, types.NamespacedName{Name: postgresDB.Name, Namespace: postgresDB.Namespace}, updated)
	if apierrors.IsNotFound(err) {
		return
	}
	require.NoError(t, err)
	assert.NotContains(t, updated.Finalizers, postgresDatabaseFinalizerName)
}

func TestPrivilegesTerminalFailureState(t *testing.T) {
	scheme := testScheme(t)
	ctx := context.Background()
	requestName := types.NamespacedName{Name: "primary", Namespace: "dbs"}

	failingGrantRepoFunc := func(dbName string) NewDBRepoFunc {
		return func(_ context.Context, _, _ string, _ string) (DBRepo, error) {
			return &stubDBRepo{execErr: fmt.Errorf("grant failed for %s", dbName)}, nil
		}
	}

	failingGrantThenSuccessfulRepoFunc := func(dbName string) NewDBRepoFunc {
		attempts := 0
		return func(_ context.Context, _, _ string, _ string) (DBRepo, error) {
			attempts++
			if attempts == 1 {
				return &stubDBRepo{execErr: fmt.Errorf("grant failed for %s", dbName)}, nil
			}
			return &stubDBRepo{}, nil
		}
	}

	failingConnectionRepoFunc := func(calls *int, err error) NewDBRepoFunc {
		return func(_ context.Context, _, _ string, _ string) (DBRepo, error) {
			(*calls)++
			return nil, err
		}
	}

	failingTerminalRepoFunc := func(errMsg string) NewDBRepoFunc {
		return func(_ context.Context, _, _ string, _ string) (DBRepo, error) {
			return nil, fmt.Errorf("%w: %s", ErrTerminal, errMsg)
		}
	}

	successfulRepoFunc := func() NewDBRepoFunc {
		return func(_ context.Context, _, _ string, _ string) (DBRepo, error) {
			return &stubDBRepo{}, nil
		}
	}

	buildObjects := func(tst struct {
		generation         int64
		databases          []enterprisev4.DatabaseDefinition
		statusPhase        *string
		observedGeneration *int64
		failureState       bool
		omitFinalizer      bool
		statusDatabases    []enterprisev4.DatabaseInfo
		conditions         []metav1.Condition
		databaseApplied    *bool
		omittedSecrets     []string
	}) []client.Object {
		reconcileFailureType := ""
		if tst.failureState {
			reconcileFailureType = reconcileFailurePrivileges
		}
		postgresDB := &enterprisev4.PostgresDatabase{
			TypeMeta: metav1.TypeMeta{
				APIVersion: enterprisev4.GroupVersion.String(),
				Kind:       "PostgresDatabase",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:       requestName.Name,
				Namespace:  requestName.Namespace,
				UID:        types.UID("postgresdb-uid"),
				Generation: tst.generation,
			},
			Spec: enterprisev4.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "primary-cluster"},
				Databases:  tst.databases,
			},
			Status: enterprisev4.PostgresDatabaseStatus{
				Phase:                tst.statusPhase,
				ObservedGeneration:   tst.observedGeneration,
				ReconcileFailureType: reconcileFailureType,
				Databases:            tst.statusDatabases,
				Conditions:           tst.conditions,
			},
		}
		if !tst.omitFinalizer {
			postgresDB.Finalizers = []string{postgresDatabaseFinalizerName}
		}

		roleOwners := make(map[string]enterprisev4.RoleOwnerReference, len(getDesiredRoles(postgresDB)))
		for _, roleName := range getDesiredRoles(postgresDB) {
			roleOwners[roleName] = enterprisev4.RoleOwnerReference{Name: postgresDB.Name, UID: string(postgresDB.UID)}
		}

		postgresCluster := &enterprisev4.PostgresCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: enterprisev4.GroupVersion.String(),
				Kind:       "PostgresCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-cluster",
				Namespace: requestName.Namespace,
			},
			Status: enterprisev4.PostgresClusterStatus{
				Phase: strPtr(string(ClusterReady)),
				ProvisionerRef: &corev1.ObjectReference{
					APIVersion: cnpgv1.SchemeGroupVersion.String(),
					Kind:       "Cluster",
					Name:       "primary-cnpg",
					Namespace:  requestName.Namespace,
				},
				Resources: &enterprisev4.PostgresClusterResources{
					SuperUserSecretRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "primary-superuser"},
						Key:                  secretKeyPassword,
					},
				},
				ManagedRolesStatus: &enterprisev4.ManagedRolesStatus{
					Reconciled: getDesiredRoles(postgresDB),
					RoleOwners: roleOwners,
				},
			},
		}

		cnpgCluster := &cnpgv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: cnpgv1.SchemeGroupVersion.String(),
				Kind:       "Cluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-cnpg",
				Namespace: requestName.Namespace,
			},
			Status: cnpgv1.ClusterStatus{
				ManagedRolesStatus: cnpgv1.ManagedRoles{
					ByStatus: map[cnpgv1.RoleStatus][]string{
						cnpgv1.RoleStatusReconciled: getDesiredRoles(postgresDB),
					},
				},
				WriteService: "primary-rw",
				ReadService:  "primary-ro",
			},
		}

		superSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "primary-superuser",
				Namespace: requestName.Namespace,
			},
			Data: map[string][]byte{secretKeyPassword: []byte("supersecret")},
		}

		objects := []client.Object{postgresDB, postgresCluster, cnpgCluster, superSecret}
		omittedSecrets := make(map[string]struct{}, len(tst.omittedSecrets))
		for _, name := range tst.omittedSecrets {
			omittedSecrets[name] = struct{}{}
		}
		ownerRef := metav1.OwnerReference{
			APIVersion:         enterprisev4.GroupVersion.String(),
			Kind:               "PostgresDatabase",
			Name:               requestName.Name,
			UID:                postgresDB.UID,
			Controller:         boolPtr(true),
			BlockOwnerDeletion: boolPtr(true),
		}
		for _, dbInfo := range tst.statusDatabases {
			adminSecretName := roleSecretName(requestName.Name, dbInfo.Name, secretRoleAdmin)
			rwSecretName := roleSecretName(requestName.Name, dbInfo.Name, secretRoleRW)
			if _, omitted := omittedSecrets[adminSecretName]; !omitted {
				objects = append(objects,
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:            adminSecretName,
							Namespace:       requestName.Namespace,
							OwnerReferences: []metav1.OwnerReference{ownerRef},
						},
						Data: map[string][]byte{
							"username":        []byte(adminRoleName(dbInfo.Name)),
							secretKeyPassword: []byte("admin-password"),
						},
					},
				)
			}
			if _, omitted := omittedSecrets[rwSecretName]; !omitted {
				objects = append(objects,
					&corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:            rwSecretName,
							Namespace:       requestName.Namespace,
							OwnerReferences: []metav1.OwnerReference{ownerRef},
						},
						Data: map[string][]byte{
							"username":        []byte(rwRoleName(dbInfo.Name)),
							secretKeyPassword: []byte("rw-password"),
						},
					},
				)
			}
		}
		for _, dbDef := range tst.databases {
			databaseApplied := boolPtr(true)
			if tst.databaseApplied != nil {
				databaseApplied = tst.databaseApplied
			}
			objects = append(objects,
				&cnpgv1.Database{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cnpgDatabaseName(requestName.Name, dbDef.Name),
						Namespace: requestName.Namespace,
					},
					Spec: cnpgv1.DatabaseSpec{
						ClusterRef: corev1.LocalObjectReference{Name: "primary-cnpg"},
						Name:       dbDef.Name,
						Owner:      adminRoleName(dbDef.Name),
					},
					Status: cnpgv1.DatabaseStatus{Applied: databaseApplied},
				},
			)
		}
		return objects
	}

	runService := func(t *testing.T, c client.Client, newDBRepo NewDBRepoFunc, metrics ports.Recorder) (ctrl.Result, *enterprisev4.PostgresDatabase, error) {
		t.Helper()

		before := &enterprisev4.PostgresDatabase{}
		require.NoError(t, c.Get(ctx, requestName, before))

		result, err := PostgresDatabaseService(
			ctx,
			&ReconcileContext{
				Client:   c,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
				Metrics:  metrics,
			},
			before,
			newDBRepo,
		)

		updated := &enterprisev4.PostgresDatabase{}
		require.NoError(t, c.Get(ctx, requestName, updated))
		return result, updated, err
	}

	tests := []struct {
		name                         string
		generation                   int64
		databases                    []enterprisev4.DatabaseDefinition
		statusPhase                  *string
		observedGeneration           *int64
		failureState                 bool
		omitFinalizer                bool
		statusDatabases              []enterprisev4.DatabaseInfo
		conditions                   []metav1.Condition
		databaseApplied              *bool
		omittedSecrets               []string
		newDBRepo                    NewDBRepoFunc
		reconcileCount               int
		wantRepoCalls                int
		wantErr                      bool
		wantErrContains              []string
		wantErrExcludes              []string
		statusUpdateErrOnReason      conditionReasons
		statusUpdateConflictOnReason conditionReasons
		wantResult                   ctrl.Result
		wantFailureState             bool
		wantFailureFieldsCleared     bool
		wantFinalizer                bool
		wantPhase                    reconcileDBPhases
		wantConditionType            conditionTypes
		wantConditionStatus          metav1.ConditionStatus
		wantConditionReason          conditionReasons
		wantConditionMessageContains []string
		wantConditionMessageExcludes []string
		wantProvisioningObservations *int
	}{
		{
			name:        "retryable privileges error stays provisioning",
			generation:  7,
			databases:   []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase: strPtr(string(readyDBPhase)),
			newDBRepo: func(_ context.Context, _, _, _ string) (DBRepo, error) {
				return &stubDBRepo{execErr: errors.New("grant failure containing supersecret")}, nil
			},
			wantErr:                  true,
			wantFailureFieldsCleared: true,
			wantPhase:                provisioningDBPhase,
			wantConditionReason:      reasonPrivilegesGrantFailed,
			wantConditionMessageContains: []string{
				"Will retry automatically",
			},
			wantConditionMessageExcludes: []string{"supersecret"},
			wantErrExcludes:              []string{"supersecret"},
		},
		{
			name:                         "provisioning blocker after routine update records one duration on recovery",
			generation:                   7,
			databases:                    []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:                  strPtr(string(readyDBPhase)),
			newDBRepo:                    failingGrantThenSuccessfulRepoFunc("payments"),
			reconcileCount:               2,
			wantPhase:                    readyDBPhase,
			wantProvisioningObservations: new(1),
		},
		{
			name:                "retryable connection failures keep retrying",
			generation:          7,
			databases:           []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:         strPtr(string(readyDBPhase)),
			wantPhase:           provisioningDBPhase,
			wantConditionReason: reasonPrivilegesGrantFailed,
			reconcileCount:      3,
			wantRepoCalls:       3,
			wantErr:             true,
		},
		{
			name:                "retryable failure keeps stale failure marker for retry",
			generation:          7,
			databases:           []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:         strPtr(string(readyDBPhase)),
			failureState:        true,
			wantRepoCalls:       1,
			wantErr:             true,
			wantFailureState:    true,
			wantPhase:           provisioningDBPhase,
			wantConditionReason: reasonPrivilegesGrantFailed,
			wantConditionMessageContains: []string{
				"Will retry automatically",
			},
		},
		{
			name:               "does not requeue after current terminal failure",
			generation:         7,
			databases:          []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:        strPtr(string(failedDBPhase)),
			observedGeneration: int64Ptr(7),
			failureState:       true,
			conditions: []metav1.Condition{
				{
					Type:               string(privilegesReady),
					Status:             metav1.ConditionFalse,
					Reason:             string(reasonPrivilegesTerminalFailure),
					Message:            "Failed to grant RW role privileges. Manual intervention required.",
					ObservedGeneration: 7,
				},
			},
			wantFailureState: true,
			wantRepoCalls:    0,
		},
		{
			name:               "repairs missing finalizer before current terminal failure early return",
			generation:         7,
			databases:          []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:        strPtr(string(failedDBPhase)),
			observedGeneration: int64Ptr(7),
			failureState:       true,
			omitFinalizer:      true,
			wantFailureState:   true,
			wantFinalizer:      true,
			wantRepoCalls:      0,
		},
		{
			name:         "successful reconcile clears stale failure marker",
			generation:   7,
			databases:    []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:  strPtr(string(readyDBPhase)),
			failureState: true,
			conditions: []metav1.Condition{
				{
					Type:               string(privilegesReady),
					Status:             metav1.ConditionFalse,
					Reason:             string(reasonPrivilegesGrantFailed),
					Message:            "previous grant failed",
					ObservedGeneration: 7,
				},
			},
			newDBRepo:                successfulRepoFunc(),
			wantFailureFieldsCleared: true,
			wantPhase:                readyDBPhase,
			wantConditionStatus:      metav1.ConditionTrue,
			wantConditionReason:      reasonPrivilegesGranted,
			wantConditionMessageContains: []string{
				"RW role privileges granted for all 1 databases",
			},
		},
		{
			name:            "marks privileges already current when no new databases require live grants",
			generation:      7,
			databases:       []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:     strPtr(string(readyDBPhase)),
			statusDatabases: []enterprisev4.DatabaseInfo{{Name: "payments"}},
			conditions: []metav1.Condition{
				{
					Type:               string(privilegesReady),
					Status:             metav1.ConditionTrue,
					Reason:             string(reasonPrivilegesGranted),
					Message:            "RW role privileges granted for all 1 databases",
					ObservedGeneration: 7,
				},
			},
			wantPhase:           readyDBPhase,
			wantConditionStatus: metav1.ConditionTrue,
			wantConditionReason: reasonPrivilegesGranted,
			wantConditionMessageContains: []string{
				"RW role privileges already current for all 1 databases",
			},
		},
		{
			name:               "pending database keeps stale failure marker before privileges retry",
			generation:         8,
			databases:          []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:        strPtr(string(failedDBPhase)),
			observedGeneration: int64Ptr(7),
			failureState:       true,
			statusDatabases:    []enterprisev4.DatabaseInfo{{Name: "payments"}},
			conditions: []metav1.Condition{
				{
					Type:               string(privilegesReady),
					Status:             metav1.ConditionFalse,
					Reason:             string(reasonPrivilegesTerminalFailure),
					Message:            "Failed to grant RW role privileges. Manual intervention required.",
					ObservedGeneration: 7,
				},
			},
			databaseApplied:  boolPtr(false),
			wantFailureState: true,
			wantResult:       ctrl.Result{RequeueAfter: retryDelay},
			wantPhase:        provisioningDBPhase,
			wantRepoCalls:    0,
		},
		{
			name:               "stale terminal recovery reports missing existing secret as drift",
			generation:         8,
			databases:          []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:        strPtr(string(failedDBPhase)),
			observedGeneration: int64Ptr(7),
			failureState:       true,
			statusDatabases:    []enterprisev4.DatabaseInfo{{Name: "payments"}},
			conditions: []metav1.Condition{
				{
					Type:               string(privilegesReady),
					Status:             metav1.ConditionFalse,
					Reason:             string(reasonPrivilegesTerminalFailure),
					Message:            "Failed to grant RW role privileges. Manual intervention required.",
					ObservedGeneration: 7,
				},
			},
			omittedSecrets:      []string{roleSecretName(requestName.Name, "payments", secretRoleRW)},
			wantFailureState:    true,
			wantResult:          ctrl.Result{RequeueAfter: retryDelay},
			wantPhase:           provisioningDBPhase,
			wantRepoCalls:       0,
			wantConditionType:   secretsReady,
			wantConditionStatus: metav1.ConditionFalse,
			wantConditionReason: reasonSecretsDriftDetected,
			wantConditionMessageContains: []string{
				"Managed Secret primary-payments-rw is missing",
				"previously provisioned role payments_rw",
			},
		},
		{
			name:               "spec change retries privileges after terminal failure without new databases",
			generation:         8,
			databases:          []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:        strPtr(string(failedDBPhase)),
			observedGeneration: int64Ptr(7),
			failureState:       true,
			statusDatabases:    []enterprisev4.DatabaseInfo{{Name: "payments"}},
			conditions: []metav1.Condition{
				{
					Type:               string(privilegesReady),
					Status:             metav1.ConditionFalse,
					Reason:             string(reasonPrivilegesTerminalFailure),
					Message:            "Failed to grant RW role privileges. Manual intervention required.",
					ObservedGeneration: 7,
				},
			},
			newDBRepo:                successfulRepoFunc(),
			wantFailureFieldsCleared: true,
			wantPhase:                readyDBPhase,
			wantConditionStatus:      metav1.ConditionTrue,
			wantConditionReason:      reasonPrivilegesGranted,
			wantConditionMessageContains: []string{
				"RW role privileges granted for all 1 databases",
			},
			wantConditionMessageExcludes: []string{
				"already current",
			},
			wantProvisioningObservations: new(1),
		},
		{
			name:               "does not observe provisioning duration when final Ready status write fails",
			generation:         8,
			databases:          []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:        strPtr(string(failedDBPhase)),
			observedGeneration: int64Ptr(7),
			failureState:       true,
			statusDatabases:    []enterprisev4.DatabaseInfo{{Name: "payments"}},
			conditions: []metav1.Condition{
				{
					Type:               string(privilegesReady),
					Status:             metav1.ConditionFalse,
					Reason:             string(reasonPrivilegesTerminalFailure),
					Message:            "Failed to grant RW role privileges. Manual intervention required.",
					ObservedGeneration: 7,
				},
			},
			newDBRepo:                    successfulRepoFunc(),
			wantErr:                      true,
			statusUpdateErrOnReason:      reasonPrivilegesGranted,
			wantErrContains:              []string{"failed to persist final status", "apiserver timeout"},
			wantProvisioningObservations: new(0),
		},
		{
			name:       "spec change restarts from Failed",
			generation: 8,
			databases: []enterprisev4.DatabaseDefinition{
				{Name: "payments"},
				{Name: "analytics"},
			},
			statusPhase:              strPtr(string(failedDBPhase)),
			observedGeneration:       int64Ptr(7),
			failureState:             true,
			newDBRepo:                successfulRepoFunc(),
			wantFailureFieldsCleared: true,
			wantPhase:                readyDBPhase,
		},
		{
			name:                "terminal privileges error transitions to Failed",
			generation:          7,
			databases:           []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:         strPtr(string(readyDBPhase)),
			newDBRepo:           failingTerminalRepoFunc("password authentication failed"),
			wantFailureState:    true,
			wantPhase:           failedDBPhase,
			wantConditionReason: reasonPrivilegesTerminalFailure,
			wantConditionMessageContains: []string{
				"Manual intervention required",
				"spec change",
			},
			wantConditionMessageExcludes: []string{
				"password authentication failed",
			},
		},
		{
			name:                    "returns joined error when non-terminal privileges status update fails",
			generation:              7,
			databases:               []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:             strPtr(string(readyDBPhase)),
			newDBRepo:               failingGrantRepoFunc("payments"),
			wantErr:                 true,
			statusUpdateErrOnReason: reasonPrivilegesGrantFailed,
			wantErrContains: []string{
				"granting RW privileges on database payments failed",
				"failed to persist privileges status",
				"apiserver timeout",
			},
		},
		{
			name:                         "returns clean requeue when non-terminal privileges status update conflicts",
			generation:                   7,
			databases:                    []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:                  strPtr(string(readyDBPhase)),
			newDBRepo:                    failingGrantRepoFunc("payments"),
			statusUpdateConflictOnReason: reasonPrivilegesGrantFailed,
			wantResult:                   ctrl.Result{Requeue: true},
		},
		{
			name:                    "returns error when terminal status update fails",
			generation:              7,
			databases:               []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:             strPtr(string(readyDBPhase)),
			newDBRepo:               failingTerminalRepoFunc("password authentication failed"),
			wantErr:                 true,
			statusUpdateErrOnReason: reasonPrivilegesTerminalFailure,
			wantErrContains: []string{
				"connecting on database payments failed",
				"failed to persist terminal privileges status",
				"apiserver timeout",
			},
		},
		{
			name:                         "returns clean requeue when terminal status update conflicts",
			generation:                   7,
			databases:                    []enterprisev4.DatabaseDefinition{{Name: "payments"}},
			statusPhase:                  strPtr(string(readyDBPhase)),
			newDBRepo:                    failingTerminalRepoFunc("password authentication failed"),
			statusUpdateConflictOnReason: reasonPrivilegesTerminalFailure,
			wantResult:                   ctrl.Result{Requeue: true},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			objects := buildObjects(struct {
				generation         int64
				databases          []enterprisev4.DatabaseDefinition
				statusPhase        *string
				observedGeneration *int64
				failureState       bool
				omitFinalizer      bool
				statusDatabases    []enterprisev4.DatabaseInfo
				conditions         []metav1.Condition
				databaseApplied    *bool
				omittedSecrets     []string
			}{
				generation:         tst.generation,
				databases:          tst.databases,
				statusPhase:        tst.statusPhase,
				observedGeneration: tst.observedGeneration,
				failureState:       tst.failureState,
				omitFinalizer:      tst.omitFinalizer,
				statusDatabases:    tst.statusDatabases,
				conditions:         tst.conditions,
				databaseApplied:    tst.databaseApplied,
				omittedSecrets:     tst.omittedSecrets,
			})
			c := testClient(t, scheme, objects...)
			if tst.statusUpdateErrOnReason != "" || tst.statusUpdateConflictOnReason != "" {
				c = fake.NewClientBuilder().
					WithScheme(scheme).
					WithStatusSubresource(&enterprisev4.PostgresDatabase{}).
					WithObjects(objects...).
					WithInterceptorFuncs(interceptor.Funcs{
						SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
							if subResourceName != "status" {
								return client.SubResource(subResourceName).Update(ctx, obj, opts...)
							}
							postgresDB, ok := obj.(*enterprisev4.PostgresDatabase)
							if ok {
								condition := meta.FindStatusCondition(postgresDB.Status.Conditions, string(privilegesReady))
								if condition != nil && condition.Reason == string(tst.statusUpdateErrOnReason) {
									return errors.New("apiserver timeout")
								}
								if condition != nil && condition.Reason == string(tst.statusUpdateConflictOnReason) {
									return postgresDatabaseConflict(postgresDB.Name)
								}
							}
							return client.SubResource(subResourceName).Update(ctx, obj, opts...)
						},
					}).
					Build()
			}

			reconcileCount := tst.reconcileCount
			if reconcileCount == 0 {
				reconcileCount = 1
			}
			repoCalls := 0
			newDBRepo := tst.newDBRepo
			if newDBRepo == nil {
				newDBRepo = failingConnectionRepoFunc(&repoCalls, errors.New("connection refused"))
			}

			var result ctrl.Result
			var err error
			var updated *enterprisev4.PostgresDatabase
			metrics := &captureMetricsRecorder{}
			for i := 1; i <= reconcileCount; i++ {
				result, updated, err = runService(t, c, newDBRepo, metrics)
				if i < reconcileCount {
					require.Error(t, err)
					assert.Equal(t, ctrl.Result{}, result)
					continue
				}
			}

			if tst.wantErr {
				require.Error(t, err)
				for _, wantErr := range tst.wantErrContains {
					assert.Contains(t, err.Error(), wantErr)
				}
				for _, unwantedErr := range tst.wantErrExcludes {
					assert.NotContains(t, err.Error(), unwantedErr)
				}
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tst.wantResult, result)
			assert.Equal(t, tst.wantRepoCalls, repoCalls)

			if tst.wantFailureFieldsCleared {
				assert.Empty(t, updated.Status.ReconcileFailureType)
			}
			if tst.wantFailureState {
				assert.Equal(t, reconcileFailurePrivileges, updated.Status.ReconcileFailureType)
			}
			if tst.wantFinalizer {
				assert.Contains(t, updated.Finalizers, postgresDatabaseFinalizerName)
			}
			if tst.wantPhase != "" {
				require.NotNil(t, updated.Status.Phase)
				assert.Equal(t, string(tst.wantPhase), *updated.Status.Phase)
			}
			if tst.wantConditionReason != "" {
				conditionType := tst.wantConditionType
				if conditionType == "" {
					conditionType = privilegesReady
				}
				condition := meta.FindStatusCondition(updated.Status.Conditions, string(conditionType))
				require.NotNil(t, condition)
				if tst.wantConditionStatus != "" {
					assert.Equal(t, tst.wantConditionStatus, condition.Status)
				}
				assert.Equal(t, string(tst.wantConditionReason), condition.Reason)
				for _, wantMessage := range tst.wantConditionMessageContains {
					assert.Contains(t, condition.Message, wantMessage)
				}
				for _, unwantedMessage := range tst.wantConditionMessageExcludes {
					assert.NotContains(t, condition.Message, unwantedMessage)
				}
			}
			if tst.wantProvisioningObservations != nil {
				require.Len(t, metrics.provisioningDurations, *tst.wantProvisioningObservations)
				if *tst.wantProvisioningObservations == 1 {
					assert.Equal(t, ports.ControllerDatabase, metrics.provisioningDurations[0].controller)
					assert.Positive(t, metrics.provisioningDurations[0].seconds)
					assert.Nil(t, updated.Status.LastTransitionTime)

					_, _, err = runService(t, c, newDBRepo, metrics)
					require.NoError(t, err)
					assert.Len(t, metrics.provisioningDurations, 1)
				}
			}
		})
	}
}

func gateDB() *enterprisev4.PostgresDatabase {
	return &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", UID: types.UID("db-uid")},
		Spec:       enterprisev4.PostgresDatabaseSpec{Databases: []enterprisev4.DatabaseDefinition{{Name: "app"}}},
	}
}

func TestEvaluateRoleGateProceedWhenReconciledAndOwnedBySelf(t *testing.T) {
	decision := evaluateRoleGate(gateDB(), &enterprisev4.ManagedRolesStatus{
		Reconciled: []string{"app_admin", "app_rw"},
		RoleOwners: map[string]enterprisev4.RoleOwnerReference{
			"app_admin": {Name: "orders", UID: "db-uid"},
			"app_rw":    {Name: "orders", UID: "db-uid"},
		},
	})
	assert.Equal(t, roleGateProceed, decision.State)
}

func TestEvaluateRoleGateConflictForAttemptedBySelf(t *testing.T) {
	decision := evaluateRoleGate(gateDB(), &enterprisev4.ManagedRolesStatus{
		Conflicts: []enterprisev4.RoleConflict{{Role: "app_admin", AttemptedBy: enterprisev4.RoleOwnerReference{Name: "orders", UID: "db-uid"}}},
	})
	assert.Equal(t, roleGateConflict, decision.State)
	assert.Equal(t, "app_admin", decision.Role)
}

func TestRoleGateReasons(t *testing.T) {
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", UID: types.UID("db-uid")},
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{{Name: "app"}, {Name: "reports"}},
		},
	}

	t.Run("blames the offending role's database and marks the rest blocked", func(t *testing.T) {
		reasons := roleGateReasons(postgresDB, roleGateDecision{
			State:   roleGateConflict,
			Message: "role app_rw is already claimed",
			Role:    "app_rw",
		})
		assert.Equal(t, map[string]string{
			"app":     "role app_rw is already claimed",
			"reports": `blocked by role gate on database "app"`,
		}, reasons)
	})

	t.Run("applies the message to all databases when no single role is implicated", func(t *testing.T) {
		reasons := roleGateReasons(postgresDB, roleGateDecision{
			State:   roleGatePending,
			Message: "Waiting for cluster to publish managed role status",
		})
		assert.Equal(t, map[string]string{
			"app":     "Waiting for cluster to publish managed role status",
			"reports": "Waiting for cluster to publish managed role status",
		}, reasons)
	})
}

func TestDatabaseForRole(t *testing.T) {
	postgresDB := &enterprisev4.PostgresDatabase{
		Spec: enterprisev4.PostgresDatabaseSpec{
			Databases: []enterprisev4.DatabaseDefinition{{Name: "app"}, {Name: "reports"}},
		},
	}
	assert.Equal(t, "app", databaseForRole(postgresDB, "app_admin"))
	assert.Equal(t, "reports", databaseForRole(postgresDB, "reports_rw"))
	assert.Equal(t, "", databaseForRole(postgresDB, "unknown_admin"))
	assert.Equal(t, "", databaseForRole(postgresDB, ""))
}

func TestEvaluateRoleGatePendingUntilOwnedAndReconciled(t *testing.T) {
	decision := evaluateRoleGate(gateDB(), &enterprisev4.ManagedRolesStatus{
		Reconciled: []string{"app_admin"},
		RoleOwners: map[string]enterprisev4.RoleOwnerReference{
			"app_admin": {Name: "orders", UID: "db-uid"},
		},
	})
	assert.Equal(t, roleGatePending, decision.State)
}

func TestEvaluateRoleGateFailedSurfacesCNPGFailure(t *testing.T) {
	decision := evaluateRoleGate(gateDB(), &enterprisev4.ManagedRolesStatus{
		RoleOwners: map[string]enterprisev4.RoleOwnerReference{
			"app_admin": {Name: "orders", UID: "db-uid"},
			"app_rw":    {Name: "orders", UID: "db-uid"},
		},
		Failed: map[string]string{"app_admin": "permission denied for role app_admin"},
	})
	assert.Equal(t, roleGateFailed, decision.State)
	assert.Contains(t, decision.Message, "app_admin")
	assert.Contains(t, decision.Message, "permission denied for role app_admin")
}

func TestEvaluateRoleGateIgnoresFailureForUnrelatedRole(t *testing.T) {
	decision := evaluateRoleGate(gateDB(), &enterprisev4.ManagedRolesStatus{
		Reconciled: []string{"app_admin", "app_rw"},
		RoleOwners: map[string]enterprisev4.RoleOwnerReference{
			"app_admin": {Name: "orders", UID: "db-uid"},
			"app_rw":    {Name: "orders", UID: "db-uid"},
		},
		Failed: map[string]string{"other_admin": "some error"},
	})
	assert.Equal(t, roleGateProceed, decision.State)
}

func TestEvaluateRoleGateFailedIsDeterministicAcrossDatabases(t *testing.T) {
	multiDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", UID: types.UID("db-uid")},
		Spec: enterprisev4.PostgresDatabaseSpec{Databases: []enterprisev4.DatabaseDefinition{
			{Name: "payments"},
			{Name: "analytics"},
		}},
	}
	// Two databases fail at once; spec order (payments before analytics) must decide the blame
	// on every reconcile rather than whichever map key Go happens to visit first.
	status := &enterprisev4.ManagedRolesStatus{
		Failed: map[string]string{
			"analytics_rw":    "permission denied",
			"payments_admin":  "permission denied",
			"analytics_admin": "permission denied",
			"payments_rw":     "permission denied",
		},
	}
	for i := 0; i < 20; i++ {
		decision := evaluateRoleGate(multiDB, status)
		assert.Equal(t, roleGateFailed, decision.State)
		assert.Equal(t, "payments_admin", decision.Role)
	}
}

func TestEvaluateRoleGatePendingIsDeterministicAcrossDatabases(t *testing.T) {
	multiDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", UID: types.UID("db-uid")},
		Spec: enterprisev4.PostgresDatabaseSpec{Databases: []enterprisev4.DatabaseDefinition{
			{Name: "payments"},
			{Name: "analytics"},
		}},
	}
	// No roles owned yet: the first-declared role must be reported every time.
	for i := 0; i < 20; i++ {
		decision := evaluateRoleGate(multiDB, &enterprisev4.ManagedRolesStatus{})
		assert.Equal(t, roleGatePending, decision.State)
		assert.Equal(t, "payments_admin", decision.Role)
	}
}

func TestCleanupManagedRolesPublishesAbsentRolesAndWaitsForClusterDrop(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	deletionTime := metav1.NewTime(time.Now().Add(-roleCleanupTimeout - time.Minute))
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: types.UID("db-uid"), DeletionTimestamp: &deletionTime, Finalizers: []string{postgresDatabaseFinalizerName}},
		Spec: enterprisev4.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "pg"},
			Databases:  []enterprisev4.DatabaseDefinition{{Name: "app"}},
		},
	}
	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
		Status: enterprisev4.PostgresClusterStatus{ManagedRolesStatus: &enterprisev4.ManagedRolesStatus{RoleOwners: map[string]enterprisev4.RoleOwnerReference{
			"app_admin": {Name: "orders", UID: "db-uid"},
			"app_rw":    {Name: "orders", UID: "db-uid"},
		}}},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&enterprisev4.PostgresDatabase{}).WithObjects(postgresDB, cluster).Build()
	rc := &ReconcileContext{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

	err := cleanupManagedRoles(ctx, rc, postgresDB, deletionPlan{deleted: []enterprisev4.DatabaseDefinition{{Name: "app"}}})
	require.ErrorIs(t, err, errRoleCleanupPending)

	updated := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "orders", Namespace: "default"}, updated))
	require.Len(t, updated.Status.Databases, 1)
	assert.False(t, updated.Status.Databases[0].Ready)
	require.Len(t, updated.Status.Databases[0].Roles, 2)
	for _, role := range updated.Status.Databases[0].Roles {
		assert.False(t, role.Exists)
	}
	require.NotNil(t, updated.Status.Phase)
	assert.Equal(t, string(deletingDBPhase), *updated.Status.Phase)
	condition := meta.FindStatusCondition(updated.Status.Conditions, string(rolesReady))
	require.NotNil(t, condition)
	assert.Equal(t, string(reasonRoleCleanupBlocked), condition.Reason)
	assert.Contains(t, condition.Message, "retaining finalizer")
}

func TestCleanupManagedRolesReleasesWhenClusterNoLongerOwnsRoles(t *testing.T) {
	ctx := t.Context()
	scheme := testScheme(t)
	postgresDB := &enterprisev4.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default", UID: types.UID("db-uid")},
		Spec:       enterprisev4.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "pg"}, Databases: []enterprisev4.DatabaseDefinition{{Name: "app"}}},
	}
	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"}, Status: enterprisev4.PostgresClusterStatus{ManagedRolesStatus: &enterprisev4.ManagedRolesStatus{}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&enterprisev4.PostgresDatabase{}).WithObjects(postgresDB, cluster).Build()
	rc := &ReconcileContext{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

	err := cleanupManagedRoles(ctx, rc, postgresDB, deletionPlan{deleted: []enterprisev4.DatabaseDefinition{{Name: "app"}}})
	require.NoError(t, err)

	updated := &enterprisev4.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: "orders", Namespace: "default"}, updated))
	require.Len(t, updated.Status.Databases, 1)
	for _, role := range updated.Status.Databases[0].Roles {
		assert.False(t, role.Exists)
	}
	condition := meta.FindStatusCondition(updated.Status.Conditions, string(rolesReady))
	if condition != nil {
		assert.NotEqual(t, string(reasonRoleCleanupBlocked), condition.Reason)
	}
}
