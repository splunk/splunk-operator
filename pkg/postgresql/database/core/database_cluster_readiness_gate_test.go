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

import (
	"context"
	"errors"
	"fmt"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	dbclusterreadiness "github.com/splunk/splunk-operator/pkg/postgresql/database/core/components/clusterreadiness"
	dbmetrics "github.com/splunk/splunk-operator/pkg/postgresql/database/core/custom_metrics"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

type clusterReadinessReaderFunc func(context.Context, string, string) (dbclusterreadiness.ResolvedClusterFacts, error)

func (f clusterReadinessReaderFunc) Read(ctx context.Context, namespace, name string) (dbclusterreadiness.ResolvedClusterFacts, error) {
	return f(ctx, namespace, name)
}

func TestObserveClusterReadinessFacade(t *testing.T) {
	transient := errors.New("apiserver unavailable")
	readyFacts := dbclusterreadiness.ResolvedClusterFacts{
		Name: "primary", Namespace: "dbs", Lifecycle: dbclusterreadiness.LifecycleReady,
		Provider: &dbclusterreadiness.ProviderReference{Kind: dbclusterreadiness.ProviderCNPG, Name: "primary-cnpg", Namespace: "dbs"},
	}

	tests := []struct {
		name           string
		facts          dbclusterreadiness.ResolvedClusterFacts
		readErr        error
		wasReady       bool
		priorCondition *metav1.Condition
		wantStop       bool
		wantResult     ctrl.Result
		wantErr        error
		wantReason     conditionReasons
		wantStatus     metav1.ConditionStatus
		wantPhase      reconcileDBPhases
		wantEvent      string
	}{
		{
			name:       "ready persists available condition and continues",
			facts:      readyFacts,
			wantReason: reasonClusterAvailable,
			wantStatus: metav1.ConditionTrue,
			wantPhase:  provisioningDBPhase,
			wantEvent:  EventClusterValidated,
		},
		{
			name:       "missing persists not found and waits",
			readErr:    fmt.Errorf("read: %w", dbclusterreadiness.ErrClusterNotFound),
			wantStop:   true,
			wantResult: ctrl.Result{RequeueAfter: clusterNotFoundRetryDelay},
			wantReason: reasonClusterNotFound,
			wantStatus: metav1.ConditionFalse,
			wantPhase:  pendingDBPhase,
			wantEvent:  EventClusterNotFound,
		},
		{
			name:       "provisioning persists ordinary wait",
			facts:      dbclusterreadiness.ResolvedClusterFacts{Lifecycle: "Provisioning"},
			wantStop:   true,
			wantResult: ctrl.Result{RequeueAfter: retryDelay},
			wantReason: reasonClusterProvisioning,
			wantStatus: metav1.ConditionFalse,
			wantPhase:  pendingDBPhase,
			wantEvent:  EventClusterNotReady,
		},
		{
			name:  "existing provisioning condition suppresses duplicate warning",
			facts: dbclusterreadiness.ResolvedClusterFacts{Lifecycle: "Provisioning"},
			priorCondition: &metav1.Condition{
				Type: string(clusterReady), Status: metav1.ConditionFalse, Reason: string(reasonClusterProvisioning),
			},
			wantStop:   true,
			wantResult: ctrl.Result{RequeueAfter: retryDelay},
			wantReason: reasonClusterProvisioning,
			wantStatus: metav1.ConditionFalse,
			wantPhase:  pendingDBPhase,
		},
		{
			name:       "recovery after ready persists recovery wait",
			facts:      dbclusterreadiness.ResolvedClusterFacts{Lifecycle: "Pending", Recovery: dbclusterreadiness.RecoveryInProgress},
			wasReady:   true,
			wantStop:   true,
			wantResult: ctrl.Result{RequeueAfter: retryDelay},
			wantReason: reasonClusterRecovery,
			wantStatus: metav1.ConditionFalse,
			wantPhase:  pendingDBPhase,
			wantEvent:  EventWaitingForClusterRecovery,
		},
		{
			name:       "transient read persists retryable condition and returns source error",
			readErr:    transient,
			wantStop:   true,
			wantErr:    transient,
			wantReason: reasonClusterInfoFetchFailed,
			wantStatus: metav1.ConditionFalse,
			wantPhase:  pendingDBPhase,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := testScheme(t)
			postgresDB := &platformv1alpha1.PostgresDatabase{
				ObjectMeta: metav1.ObjectMeta{Name: "database", Namespace: "dbs", UID: types.UID("database-uid"), Generation: 1},
				Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "primary"}},
			}
			if tt.priorCondition != nil {
				postgresDB.Status.Conditions = []metav1.Condition{*tt.priorCondition}
			}
			client := testClient(t, scheme, postgresDB)
			recorder := record.NewFakeRecorder(2)
			rc := &ReconcileContext{
				Client:   client,
				Recorder: recorder,
				ClusterReader: clusterReadinessReaderFunc(func(_ context.Context, namespace, name string) (dbclusterreadiness.ResolvedClusterFacts, error) {
					assert.Equal(t, "dbs", namespace)
					assert.Equal(t, "primary", name)
					return tt.facts, tt.readErr
				}),
			}
			updateStatus := func(condition conditionTypes, status metav1.ConditionStatus, reason conditionReasons, message string, phase reconcileDBPhases) error {
				return persistStatus(t.Context(), client, nil, postgresDB, tt.wasReady, condition, status, reason, message, phase)
			}

			_, result, err, stop := observeClusterReadiness(t.Context(), rc, postgresDB, tt.wasReady, updateStatus)
			assert.Equal(t, tt.wantStop, stop)
			assert.Equal(t, tt.wantResult, result)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			stored := &platformv1alpha1.PostgresDatabase{}
			require.NoError(t, client.Get(t.Context(), types.NamespacedName{Name: postgresDB.Name, Namespace: postgresDB.Namespace}, stored))
			condition := meta.FindStatusCondition(stored.Status.Conditions, string(clusterReady))
			require.NotNil(t, condition)
			assert.Equal(t, tt.wantStatus, condition.Status)
			assert.Equal(t, string(tt.wantReason), condition.Reason)
			require.NotNil(t, stored.Status.Phase)
			assert.Equal(t, string(tt.wantPhase), *stored.Status.Phase)
			if tt.wantEvent == "" {
				select {
				case event := <-recorder.Events:
					t.Fatalf("unexpected event: %s", event)
				default:
				}
				return
			}
			select {
			case event := <-recorder.Events:
				assert.Contains(t, event, tt.wantEvent)
			default:
				t.Fatalf("expected event %s", tt.wantEvent)
			}
		})
	}
}

func TestObserveClusterReadinessPrefersTransientReadErrorOverStatusPersistenceError(t *testing.T) {
	readErr := errors.New("apiserver unavailable")
	statusErr := errors.New("status update unavailable")
	postgresDB := &platformv1alpha1.PostgresDatabase{
		ObjectMeta: metav1.ObjectMeta{Name: "database", Namespace: "dbs"},
		Spec:       platformv1alpha1.PostgresDatabaseSpec{ClusterRef: corev1.LocalObjectReference{Name: "primary"}},
	}
	rc := &ReconcileContext{
		Recorder: record.NewFakeRecorder(1),
		ClusterReader: clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterreadiness.ResolvedClusterFacts, error) {
			return dbclusterreadiness.ResolvedClusterFacts{}, readErr
		}),
	}

	_, result, err, stop := observeClusterReadiness(t.Context(), rc, postgresDB, false, func(conditionTypes, metav1.ConditionStatus, conditionReasons, string, reconcileDBPhases) error {
		return statusErr
	})

	assert.True(t, stop)
	assert.Equal(t, ctrl.Result{}, result)
	assert.ErrorIs(t, err, readErr)
	assert.NotErrorIs(t, err, statusErr)
}

func TestPostgresDatabaseServiceDoesNotReadRawClusterAfterReadinessGate(t *testing.T) {
	ctx := t.Context()
	requestName := types.NamespacedName{Name: "primary", Namespace: "dbs"}
	const databaseName = "payments"
	clusterReads := 0
	unexpectedClusterRead := errors.New("unexpected PostgresCluster read after readiness gate")
	scheme := testScheme(t)
	objects := buildFreshDatabaseObjects(requestName, databaseName)
	phase := string(provisioningDBPhase)
	objects[0].(*platformv1alpha1.PostgresDatabase).Status.Phase = &phase

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresDatabase{}).
		WithObjects(objects...).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, next client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*platformv1alpha1.PostgresCluster); ok {
					clusterReads++
					return unexpectedClusterRead
				}
				return next.Get(ctx, key, obj, opts...)
			},
		}).
		Build()
	postgresDB := &platformv1alpha1.PostgresDatabase{}
	require.NoError(t, c.Get(ctx, requestName, postgresDB))

	facts := dbclusterreadiness.ResolvedClusterFacts{
		Name:      "primary-cluster",
		Namespace: requestName.Namespace,
		Lifecycle: dbclusterreadiness.LifecycleReady,
		Provider: &dbclusterreadiness.ProviderReference{
			Kind:      dbclusterreadiness.ProviderCNPG,
			Name:      "primary-cnpg",
			Namespace: requestName.Namespace,
		},
		ManagedRolesStatus: &platformv1alpha1.ManagedRolesStatus{
			Reconciled: []string{adminRoleName(databaseName), rwRoleName(databaseName)},
			RoleOwners: map[string]platformv1alpha1.RoleOwnerReference{
				adminRoleName(databaseName): {Name: postgresDB.Name, UID: string(postgresDB.UID)},
				rwRoleName(databaseName):    {Name: postgresDB.Name, UID: string(postgresDB.UID)},
			},
		},
		SuperUserSecretRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "primary-superuser"},
			Key:                  secretKeyPassword,
		},
		ConnectionPoolerStatus: &platformv1alpha1.ConnectionPoolerStatus{
			Enabled: true, ReadWriteEnabled: true,
		},
		CustomMetricsStatus: &platformv1alpha1.CustomMetricsStatus{},
	}
	var observedCustomMetricsStatus *platformv1alpha1.CustomMetricsStatus

	result, err := PostgresDatabaseService(ctx, &ReconcileContext{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		ClusterReader: clusterReadinessReaderFunc(func(context.Context, string, string) (dbclusterreadiness.ResolvedClusterFacts, error) {
			return facts, nil
		}),
		DatabaseProvisioner: &stubDatabaseProvisioner{},
		NewCustomMetricsAcknowledgementRepo: func(status *platformv1alpha1.CustomMetricsStatus) dbmetrics.AcknowledgementRepository {
			observedCustomMetricsStatus = status
			return emptyAcknowledgementRepository{}
		},
	}, postgresDB, func(context.Context, string, string, string) (ports.DBRepo, error) {
		return &stubDBRepo{}, nil
	})

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	assert.Zero(t, clusterReads)
	assert.Same(t, facts.CustomMetricsStatus, observedCustomMetricsStatus)
	configMap := &corev1.ConfigMap{}
	require.NoError(t, c.Get(ctx, types.NamespacedName{Name: configMapName(postgresDB.Name, databaseName), Namespace: requestName.Namespace}, configMap))
	assert.Equal(t, "primary-cnpg-pooler-rw.dbs.svc.cluster.local", configMap.Data[pgconninfo.KeyPoolerRWEndpoint])
}
