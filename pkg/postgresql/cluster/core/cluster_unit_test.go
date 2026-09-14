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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestReconcileErrorPassdownToObserve(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")
	mergedConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
		Spec: platformv1alpha1.PostgresClusterClassSpec{
			Config: &platformv1alpha1.PostgresClusterClassConfig{ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}},
		},
	}

	tests := []struct {
		name              string
		expectedCondition conditionTypes
		expectedReason    conditionReasons
		build             func(updateStatus healthStatusUpdater) component
	}{
		{
			name:              "cluster component: Get error surfaced through Observe",
			expectedCondition: clusterReady,
			expectedReason:    reasonClusterGetFailed,
			build: func(updateStatus healthStatusUpdater) component {
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status: platformv1alpha1.PostgresClusterStatus{
						Resources: &platformv1alpha1.PostgresClusterResources{
							SuperUserSecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "pg1-secret"},
								Key:                  "password",
							},
						},
					},
				}
				errClient := getErrorClient{
					Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*cnpgv1.Cluster)
						return ok
					},
				}
				contracts := &reconcileContracts{Secret: &corev1.Secret{}}
				return newClusterModel(errClient, scheme, noopEventEmitter{}, updateStatus, cluster, clusterClass, mergedConfig, contracts)
			},
		},
		{
			name:              "managedRoles component: Patch error surfaced through Observe",
			expectedCondition: managedRolesReady,
			expectedReason:    reasonManagedRolesFailed,
			build: func(updateStatus healthStatusUpdater) component {
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				}
				postgresDB := postgresDatabaseWithManagedRoles("app-db", []managedRole{{Name: "app_user", Exists: true}})
				contracts := &reconcileContracts{
					CNPGCluster: &cnpgv1.Cluster{
						ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
						Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
					},
					Secret: &corev1.Secret{},
				}
				return newManagedRolesModel(
					patchErrorClient{Client: indexedManagedRolesTestClient(scheme, postgresDB).Build(), err: assert.AnError},
					scheme, noopEventEmitter{}, updateStatus, cluster, contracts, nil,
				)
			},
		},
		{
			name:              "pooler component: Create error surfaced through Observe",
			expectedCondition: poolerReady,
			expectedReason:    reasonPoolerReconciliationFailed,
			build: func(updateStatus healthStatusUpdater) component {
				poolerInstances := int32(2)
				poolerMode := platformv1alpha1.ConnectionPoolerModeTransaction
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				}
				contracts := &reconcileContracts{
					CNPGCluster: &cnpgv1.Cluster{
						ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
						Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
					},
				}
				poolerSpec := mergedConfig.Spec.DeepCopy()
				poolerSpec.ConnectionPooler = &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}
				poolerCfg := &MergedConfig{
					Spec: poolerSpec,
					CNPG: &platformv1alpha1.CNPGConfig{
						ConnectionPooler: &platformv1alpha1.ConnectionPoolerConfig{
							Instances: &poolerInstances,
							Mode:      &poolerMode,
							Config:    map[string]string{},
						},
					},
				}
				errClient := createErrorClient{
					Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*cnpgv1.Pooler)
						return ok
					},
				}
				return newPoolerModel(errClient, scheme, noopEventEmitter{}, updateStatus, cluster, clusterClass, poolerCfg, contracts)
			},
		},
		{
			name:              "configMap component: pooler lookup error surfaced through Observe",
			expectedCondition: configMapsReady,
			expectedReason:    reasonConfigMapFailed,
			build: func(updateStatus healthStatusUpdater) component {
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     platformv1alpha1.PostgresClusterStatus{Resources: &platformv1alpha1.PostgresClusterResources{}},
				}
				contracts := &reconcileContracts{
					CNPGCluster: &cnpgv1.Cluster{
						ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
						Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
					},
					Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}},
				}
				errClient := getErrorClient{
					Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*cnpgv1.Pooler)
						return ok
					},
				}
				return newConfigMapModel(errClient, scheme, noopEventEmitter{}, updateStatus, cluster, contracts)
			},
		},
		{
			name:              "secret component: existence-check error surfaced through Observe",
			expectedCondition: secretsReady,
			expectedReason:    reasonSuperUserSecretFailed,
			build: func(updateStatus healthStatusUpdater) component {
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     platformv1alpha1.PostgresClusterStatus{Resources: &platformv1alpha1.PostgresClusterResources{}},
				}
				errClient := getErrorClient{
					Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*corev1.Secret)
						return ok
					},
				}
				return newSecretModel(errClient, scheme, noopEventEmitter{}, updateStatus, cluster, "pg1-secret", &reconcileContracts{})
			},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Arrange
			var written componentHealth
			var writes int
			updateStatus := func(_ *platformv1alpha1.PostgresClusterStatus, health componentHealth) error {
				written = health
				writes++
				return nil
			}
			model := tt.build(updateStatus)

			// Act
			reconcileErr := model.Reconcile(context.Background())
			health, err := model.Observe(context.Background(), reconcileErr)

			// Assert
			require.Error(t, err)
			require.ErrorIs(t, err, assert.AnError)
			assert.Equal(t, tt.expectedCondition, health.Condition)
			assert.Equal(t, pgcConstants.Failed, health.State)
			assert.Equal(t, tt.expectedReason, health.Reason)
			assert.Equal(t, failedClusterPhase, health.Phase)
			assert.NotEmpty(t, health.Message)
			assert.Equal(t, 1, writes)
			assert.Equal(t, health, written)
		})
	}
}

func TestReconcileFailureEmitsWarningFromObserveNotReconcile(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")
	mergedConfig := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	clusterClass := &platformv1alpha1.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
		Spec: platformv1alpha1.PostgresClusterClassSpec{
			Config: &platformv1alpha1.PostgresClusterClassConfig{ConnectionPooler: &platformv1alpha1.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}},
		},
	}

	tests := []struct {
		name  string
		build func(events *captureEventEmitter) component
	}{
		{
			name: "cluster component emits warning from Observe",
			build: func(events *captureEventEmitter) component {
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status: platformv1alpha1.PostgresClusterStatus{
						Resources: &platformv1alpha1.PostgresClusterResources{
							SuperUserSecretRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "pg1-secret"},
								Key:                  "password",
							},
						},
					},
				}
				errClient := getErrorClient{
					Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*cnpgv1.Cluster)
						return ok
					},
				}
				contracts := &reconcileContracts{Secret: &corev1.Secret{}}
				return newClusterModel(errClient, scheme, events, nil, cluster, clusterClass, mergedConfig, contracts)
			},
		},
		{
			name: "secret component emits warning from Observe",
			build: func(events *captureEventEmitter) component {
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     platformv1alpha1.PostgresClusterStatus{Resources: &platformv1alpha1.PostgresClusterResources{}},
				}
				errClient := getErrorClient{
					Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*corev1.Secret)
						return ok
					},
				}
				return newSecretModel(errClient, scheme, events, nil, cluster, "pg1-secret", &reconcileContracts{})
			},
		},
		{
			name: "configmap component emits warning from Observe",
			build: func(events *captureEventEmitter) component {
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     platformv1alpha1.PostgresClusterStatus{Resources: &platformv1alpha1.PostgresClusterResources{}},
				}
				contracts := &reconcileContracts{
					CNPGCluster: &cnpgv1.Cluster{
						ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
						Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
					},
					Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}},
				}
				errClient := getErrorClient{
					Client: fake.NewClientBuilder().WithScheme(scheme).Build(),
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*cnpgv1.Pooler)
						return ok
					},
				}
				return newConfigMapModel(errClient, scheme, events, nil, cluster, contracts)
			},
		},
		{
			name: "managed roles component emits warning from Observe",
			build: func(events *captureEventEmitter) component {
				cluster := &platformv1alpha1.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "default"},
				}
				postgresDB := postgresDatabaseWithManagedRoles("app-db", []managedRole{{Name: "app_user", Exists: true}})
				cnpg := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
				contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
				return newManagedRolesModel(
					patchErrorClient{Client: indexedManagedRolesTestClient(scheme, postgresDB).Build(), err: assert.AnError},
					scheme, events, nil, cluster, contracts, nil,
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			events := &captureEventEmitter{}
			model := tt.build(events)

			// Reconcile must not emit any warning
			reconcileErr := model.Reconcile(context.Background())
			assert.Empty(t, events.warnings, "Reconcile must not emit warnings — boundary violation")

			// Observe must emit the warning
			_, _ = model.Observe(context.Background(), reconcileErr)
			assert.NotEmpty(t, events.warnings, "Observe must emit warning on reconcile failure")
		})
	}
}

func TestClusterModelStorageResizeInProgress(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	instances := int32(3)
	version := "15.13"
	storageSize := resource.MustParse("10Gi")
	cfg := &MergedConfig{
		Spec: &platformv1alpha1.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &platformv1alpha1.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}

	cases := []struct {
		name         string
		resizingPVCs []string
		instances    int
		wantPending  int
		wantTotal    int
		wantResizing bool
	}{
		{
			name:         "no resize in progress",
			resizingPVCs: nil,
			instances:    3,
			wantResizing: false,
		},
		{
			name:         "all PVCs resizing",
			resizingPVCs: []string{"pg1-1", "pg1-2", "pg1-3"},
			instances:    3,
			wantPending:  3,
			wantTotal:    3,
			wantResizing: true,
		},
		{
			name:         "partial resize: some PVCs still pending",
			resizingPVCs: []string{"pg1-2", "pg1-3"},
			instances:    3,
			wantPending:  2,
			wantTotal:    3,
			wantResizing: true,
		},
		{
			name:         "single instance resize",
			resizingPVCs: []string{"pg1-1"},
			instances:    1,
			wantPending:  1,
			wantTotal:    1,
			wantResizing: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				Status: platformv1alpha1.PostgresClusterStatus{
					Resources: &platformv1alpha1.PostgresClusterResources{
						SuperUserSecretRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "pg1-secret"},
							Key:                  "password",
						},
					},
				},
			}
			model := newClusterModel(
				fake.NewClientBuilder().WithScheme(scheme).Build(),
				scheme, noopEventEmitter{}, nil, cluster,
				&platformv1alpha1.PostgresClusterClass{},
				cfg, &reconcileContracts{Secret: &corev1.Secret{}},
			)
			model.cnpgCluster = &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
				Status: cnpgv1.ClusterStatus{
					Phase:       cnpgv1.PhaseHealthy,
					Instances:   tc.instances,
					ResizingPVC: tc.resizingPVCs,
				},
			}

			pending, total, resizing := model.storageResizeInProgress()
			assert.Equal(t, tc.wantResizing, resizing)
			if tc.wantResizing {
				assert.Equal(t, tc.wantPending, pending)
				assert.Equal(t, tc.wantTotal, total)
			}
		})
	}
}

func TestPhaseWaitingForInstancesToBeActive(t *testing.T) {
	t.Parallel()

	readyPhase := string(readyClusterPhase)
	provisioningPhase := string(provisioningClusterPhase)
	pendingPhase := string(pendingClusterPhase)

	tests := []struct {
		name          string
		clusterPhase  *string
		cnpgInstances int
		setPatch      bool
		wantPhase     reconcileClusterPhases
		wantReason    conditionReasons
		wantEvent     string // non-empty: assert ClusterDegraded event contains this string
	}{
		{
			name:          "requiresPhaseGate holds provisioning",
			clusterPhase:  &readyPhase,
			cnpgInstances: 2,
			setPatch:      true,
			wantPhase:     provisioningClusterPhase,
			wantReason:    reasonCNPGProvisioning,
		},
		{
			name:          "desired!=actual holds provisioning",
			clusterPhase:  &readyPhase,
			cnpgInstances: 1, // desired=2
			wantPhase:     provisioningClusterPhase,
			wantReason:    reasonCNPGProvisioning,
		},
		{
			name:          "alreadyProvisioning holds provisioning",
			clusterPhase:  &provisioningPhase,
			cnpgInstances: 2, // desired=2, equal — only alreadyProvisioning guard fires
			wantPhase:     provisioningClusterPhase,
			wantReason:    reasonCNPGProvisioning,
		},
		{
			name:          "pod crash emits ClusterDegraded",
			clusterPhase:  &readyPhase,
			cnpgInstances: 2, // desired=2, equal — no guards fire
			wantPhase:     pendingClusterPhase,
			wantReason:    reasonCNPGRecovery,
			wantEvent:     "Cluster entered recovery phase: Pending: " + string(cnpgv1.PhaseWaitingForInstancesToBeActive),
		},
		{
			name:          "non-ready to non-ready emits no event",
			clusterPhase:  &pendingPhase,
			cnpgInstances: 2, // desired=2, equal — no guards fire
			wantPhase:     pendingClusterPhase,
			wantReason:    reasonCNPGRecovery,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			scheme := newTestScheme()
			cluster := &platformv1alpha1.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				Spec:       platformv1alpha1.PostgresClusterSpec{},
				Status: platformv1alpha1.PostgresClusterStatus{
					Phase: tt.clusterPhase,
					Conditions: []metav1.Condition{{
						Type:    string(clusterReady),
						Status:  metav1.ConditionTrue,
						Reason:  string(reasonCNPGClusterHealthy),
						Message: msgProvisionerHealthy,
					}},
				},
			}
			cnpg := &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				Status: cnpgv1.ClusterStatus{
					Phase:          cnpgv1.PhaseWaitingForInstancesToBeActive,
					Instances:      tt.cnpgInstances,
					ReadyInstances: tt.cnpgInstances - 1,
				},
			}
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&platformv1alpha1.PostgresCluster{}).
				WithObjects(cluster).
				Build()
			recorder := record.NewFakeRecorder(10)
			rc := &ReconcileContext{Client: c, Scheme: scheme, Recorder: recorder}
			updateStatus := func(before *platformv1alpha1.PostgresClusterStatus, health componentHealth) error {
				oldPhase := ""
				if cluster.Status.Phase != nil {
					oldPhase = *cluster.Status.Phase
				}
				if err := setStatusFromHealth(ctx, c, nil, cluster, before, health); err != nil {
					return err
				}
				newPhase := ""
				if cluster.Status.Phase != nil {
					newPhase = *cluster.Status.Phase
				}
				rc.emitClusterPhaseTransition(cluster, oldPhase, newPhase, health.Reason, health.Message)
				return nil
			}
			instances := int32(2)
			model := newClusterModel(
				c,
				scheme,
				rc,
				updateStatus,
				cluster,
				&platformv1alpha1.PostgresClusterClass{},
				&MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{Instances: &instances}},
				&reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}}},
			)
			model.cnpgCluster = cnpg
			if tt.setPatch {
				model.cnpgPatch = cnpgPatchBody
			}

			health, err := model.Observe(ctx, nil)

			require.NoError(t, err)
			assert.Equal(t, tt.wantPhase, health.Phase)
			assert.Equal(t, tt.wantReason, health.Reason)
			if tt.wantEvent != "" {
				assert.NotEmpty(t, health.Message)
				select {
				case event := <-recorder.Events:
					assert.Contains(t, event, corev1.EventTypeWarning)
					assert.Contains(t, event, EventClusterDegraded)
					assert.Contains(t, event, tt.wantEvent)
				default:
					t.Fatal("expected ClusterDegraded warning event")
				}
			}
			select {
			case extra := <-recorder.Events:
				t.Errorf("unexpected extra event: %s", extra)
			default:
			}
		})
	}
}

func TestPhaseFailOverEmitsClusterDegraded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	scheme := newTestScheme()
	readyPhase := string(readyClusterPhase)
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: platformv1alpha1.PostgresClusterStatus{
			Phase: &readyPhase,
			Conditions: []metav1.Condition{{
				Type:    string(clusterReady),
				Status:  metav1.ConditionTrue,
				Reason:  string(reasonCNPGClusterHealthy),
				Message: msgProvisionerHealthy,
			}},
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseFailOver},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresCluster{}).
		WithObjects(cluster).
		Build()
	recorder := record.NewFakeRecorder(10)
	rc := &ReconcileContext{Client: c, Scheme: scheme, Recorder: recorder}
	updateStatus := func(before *platformv1alpha1.PostgresClusterStatus, health componentHealth) error {
		oldPhase := ""
		if cluster.Status.Phase != nil {
			oldPhase = *cluster.Status.Phase
		}
		if err := setStatusFromHealth(ctx, c, nil, cluster, before, health); err != nil {
			return err
		}
		newPhase := ""
		if cluster.Status.Phase != nil {
			newPhase = *cluster.Status.Phase
		}
		rc.emitClusterPhaseTransition(cluster, oldPhase, newPhase, health.Reason, health.Message)
		return nil
	}
	model := newClusterModel(
		c, scheme, rc, updateStatus, cluster,
		&platformv1alpha1.PostgresClusterClass{},
		&MergedConfig{},
		&reconcileContracts{Secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret"}}},
	)
	model.cnpgCluster = cnpg

	health, err := model.Observe(ctx, nil)

	require.NoError(t, err)
	assert.Equal(t, pendingClusterPhase, health.Phase)
	assert.Equal(t, reasonCNPGFailingOver, health.Reason)
	assert.NotEmpty(t, health.Message)
	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, corev1.EventTypeWarning)
		assert.Contains(t, event, EventClusterDegraded)
		assert.Contains(t, event, "Cluster entered failover phase: Pending: "+string(cnpgv1.PhaseFailOver))
	default:
		t.Fatal("expected ClusterDegraded warning event")
	}
	select {
	case extra := <-recorder.Events:
		t.Errorf("unexpected extra event: %s", extra)
	default:
	}
}

func TestPostgresClusterServiceInitialPhase(t *testing.T) {
	ready := string(readyClusterPhase)
	tests := []struct {
		name            string
		phase           *string
		finalizers      []string
		wantPhase       string
		wantResult      ctrl.Result
		wantInitialized bool
		wantFinalizer   bool
	}{
		{
			name:            "initializes Pending while adding the finalizer",
			wantPhase:       string(pendingClusterPhase),
			wantResult:      ctrl.Result{Requeue: true},
			wantInitialized: true,
			wantFinalizer:   true,
		},
		{
			name:            "initializes Pending when the finalizer already exists",
			finalizers:      []string{PostgresClusterFinalizerName},
			wantPhase:       string(pendingClusterPhase),
			wantResult:      ctrl.Result{Requeue: true},
			wantInitialized: true,
			wantFinalizer:   true,
		},
		{
			name:          "preserves a nonnil phase while adding the finalizer",
			phase:         &ready,
			wantPhase:     ready,
			wantFinalizer: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			scheme := newTestScheme()
			cluster := newTestCluster("primary", "dbs")
			cluster.CreationTimestamp = metav1.Now()
			cluster.Status.Phase = tt.phase
			cluster.Finalizers = tt.finalizers
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&platformv1alpha1.PostgresCluster{}).
				WithObjects(cluster).
				Build()

			result, err := PostgresClusterService(ctx, &ReconcileContext{
				Client:   c,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(1),
			}, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}, nil, nil, nil, nil)

			require.NoError(t, err)
			assert.Equal(t, tt.wantResult, result)
			stored := &platformv1alpha1.PostgresCluster{}
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cluster), stored))
			assert.Equal(t, tt.wantFinalizer, controllerutil.ContainsFinalizer(stored, PostgresClusterFinalizerName))
			require.NotNil(t, stored.Status.Phase)
			assert.Equal(t, tt.wantPhase, *stored.Status.Phase)
			if tt.wantInitialized {
				assert.Empty(t, stored.Status.Conditions)
				assert.Nil(t, stored.Status.ObservedGeneration)
				assert.Nil(t, stored.Status.Resources)
				require.NotNil(t, stored.Status.LastTransitionTime)
				assert.Equal(t, stored.CreationTimestamp, *stored.Status.LastTransitionTime)

				readinessDuration, completed, err := setPhaseStatus(ctx, c, stored, readyClusterPhase)
				require.NoError(t, err)
				assert.True(t, completed)
				assert.Positive(t, readinessDuration)
				assert.Nil(t, stored.Status.LastTransitionTime)
			}
		})
	}
}

func TestPostgresClusterServiceReturnsInitialPhaseConflict(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme()
	cluster := newTestCluster("primary", "dbs")
	conflict := apierrors.NewConflict(
		schema.GroupResource{Group: platformv1alpha1.GroupVersion.Group, Resource: "postgresclusters"},
		cluster.Name,
		errors.New("resource version conflict"),
	)
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresCluster{}).
		WithObjects(cluster).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(_ context.Context, _ client.Client, subresource string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				if subresource == "status" {
					return conflict
				}
				return nil
			},
		}).
		Build()

	result, err := PostgresClusterService(ctx, &ReconcileContext{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(1),
	}, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}, nil, nil, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{Requeue: true}, result)
	stored := &platformv1alpha1.PostgresCluster{}
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(cluster), stored))
	assert.True(t, controllerutil.ContainsFinalizer(stored, PostgresClusterFinalizerName))
	assert.Nil(t, stored.Status.Phase)
}

func TestPostgresClusterServiceSkipsPendingPhaseDuringDeletion(t *testing.T) {
	ctx := context.Background()
	scheme := newTestScheme()
	deletionTimestamp := metav1.Now()
	cluster := newTestCluster("primary", "dbs")
	cluster.DeletionTimestamp = &deletionTimestamp
	cluster.Finalizers = []string{PostgresClusterFinalizerName}
	deletePolicy := clusterDeletionPolicyDelete
	cluster.Spec.ClusterDeletionPolicy = &deletePolicy
	statusUpdates := 0
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&platformv1alpha1.PostgresCluster{}).
		WithObjects(cluster).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, c client.Client, subresource string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if subresource == "status" {
					statusUpdates++
				}
				return c.SubResource(subresource).Update(ctx, obj, opts...)
			},
		}).
		Build()

	result, err := PostgresClusterService(ctx, &ReconcileContext{
		Client:   c,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(1),
	}, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}, nil, nil, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
	assert.Zero(t, statusUpdates)
}

func TestHandleFinalizerUnknownDeletionPolicy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	now := metav1.Now()
	unknownPolicy := "delete" // typo — lowercase, not a valid constant
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pg1",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{PostgresClusterFinalizerName},
		},
		Spec: platformv1alpha1.PostgresClusterSpec{
			ClusterDeletionPolicy: &unknownPolicy,
		},
	}

	rc := &ReconcileContext{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build(),
		Scheme: scheme,
	}

	err := handleFinalizer(context.Background(), rc, cluster)

	require.Error(t, err)
	assert.Contains(t, err.Error(), unknownPolicy)
}

func TestRemoveBarmanWALArchiverPlugin(t *testing.T) {
	t.Parallel()

	otherPlugin := cnpgv1.PluginConfiguration{Name: "some-other.plugin.io"}
	barmanPlugin := cnpgv1.PluginConfiguration{Name: barmanCloudPluginName}

	tests := []struct {
		name            string
		plugins         []cnpgv1.PluginConfiguration
		expectedRemoved bool
		expectedNames   []string
	}{
		{
			name:            "no plugins",
			plugins:         nil,
			expectedRemoved: false,
			expectedNames:   []string{},
		},
		{
			name:            "removes barman, keeps foreign plugin",
			plugins:         []cnpgv1.PluginConfiguration{otherPlugin, barmanPlugin},
			expectedRemoved: true,
			expectedNames:   []string{otherPlugin.Name},
		},
		{
			name:            "only barman present",
			plugins:         []cnpgv1.PluginConfiguration{barmanPlugin},
			expectedRemoved: true,
			expectedNames:   []string{},
		},
		{
			name:            "no barman present leaves plugins untouched",
			plugins:         []cnpgv1.PluginConfiguration{otherPlugin},
			expectedRemoved: false,
			expectedNames:   []string{otherPlugin.Name},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cnpg := &cnpgv1.Cluster{Spec: cnpgv1.ClusterSpec{Plugins: tt.plugins}}

			removed := removeBarmanWALArchiverPlugin(cnpg)

			assert.Equal(t, tt.expectedRemoved, removed)
			gotNames := make([]string, 0, len(cnpg.Spec.Plugins))
			for _, p := range cnpg.Spec.Plugins {
				gotNames = append(gotNames, p.Name)
			}
			assert.Equal(t, tt.expectedNames, gotNames)
		})
	}
}

func TestHandleFinalizerRetainStripsBarmanPlugin(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, platformv1alpha1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	now := metav1.Now()
	retain := clusterDeletionPolicyRetain
	cluster := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pg1",
			Namespace:         "default",
			UID:               "owner-uid",
			DeletionTimestamp: &now,
			Finalizers:        []string{PostgresClusterFinalizerName},
		},
		Spec: platformv1alpha1.PostgresClusterSpec{
			ClusterDeletionPolicy: &retain,
		},
	}

	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "platform.splunk.com/v1alpha1",
				Kind:       "PostgresCluster",
				Name:       "pg1",
				UID:        "owner-uid",
				Controller: ptr.To(true),
			}},
		},
		Spec: cnpgv1.ClusterSpec{
			Plugins: []cnpgv1.PluginConfiguration{
				{Name: "keep-me.plugin.io"},
				{Name: barmanCloudPluginName, IsWALArchiver: ptr.To(true)},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, cnpg).Build()
	rc := &ReconcileContext{Client: c, Scheme: scheme, Recorder: record.NewFakeRecorder(10)}

	require.NoError(t, handleFinalizer(context.Background(), rc, cluster))

	got := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, got))

	gotNames := make([]string, 0, len(got.Spec.Plugins))
	for _, p := range got.Spec.Plugins {
		gotNames = append(gotNames, p.Name)
	}
	assert.Equal(t, []string{"keep-me.plugin.io"}, gotNames, "barman plugin stripped, foreign plugin retained")
	assert.Empty(t, got.OwnerReferences, "owner reference removed so retained cluster is orphaned")
}

func TestRemoveOwnerRef(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	platformv1alpha1.AddToScheme(scheme)

	owner := &platformv1alpha1.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "owner-uid",
		},
	}

	otherOwnerRef := metav1.OwnerReference{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "other-owner",
		UID:        "other-uid",
	}
	ourOwnerRef := metav1.OwnerReference{
		APIVersion: "platform.splunk.com/v1alpha1",
		Kind:       "PostgresCluster",
		Name:       "my-cluster",
		UID:        "owner-uid",
	}

	tests := []struct {
		name            string
		ownerRefs       []metav1.OwnerReference
		expectedRemoved bool
		expectedRefsLen int
	}{
		{
			name:            "returns false when owner ref not present",
			ownerRefs:       nil,
			expectedRemoved: false,
			expectedRefsLen: 0,
		},
		{
			name:            "removes owner ref and returns true",
			ownerRefs:       []metav1.OwnerReference{ourOwnerRef},
			expectedRemoved: true,
			expectedRefsLen: 0,
		},
		{
			name:            "removes only our owner ref and keeps others",
			ownerRefs:       []metav1.OwnerReference{otherOwnerRef, ourOwnerRef},
			expectedRemoved: true,
			expectedRefsLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "my-secret",
					Namespace:       "default",
					OwnerReferences: tt.ownerRefs,
				},
			}

			removed, err := removeOwnerRef(scheme, owner, secret)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedRemoved, removed)
			assert.Len(t, secret.GetOwnerReferences(), tt.expectedRefsLen)
		})
	}
}

func TestPatchObject(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)

	t.Run("patches object successfully", func(t *testing.T) {
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{"key": []byte("old-value")},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		original := existing.DeepCopy()
		existing.Data["key"] = []byte("new-value")

		err := patchObject(context.Background(), c, original, existing, "Secret")

		require.NoError(t, err)
		patched := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existing), patched))
		assert.Equal(t, "new-value", string(patched.Data["key"]))
	})

	t.Run("returns nil when object not found", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		original := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deleted-secret",
				Namespace: "default",
			},
		}
		modified := original.DeepCopy()
		modified.Data = map[string][]byte{"key": []byte("value")}

		err := patchObject(context.Background(), c, original, modified, "Secret")

		assert.NoError(t, err)
	})
}

func TestDeleteCNPGCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	cnpgv1.AddToScheme(scheme)

	tests := []struct {
		name    string
		objects []client.Object
		cluster *cnpgv1.Cluster
	}{
		{
			name: "deletes existing cluster",
			objects: []client.Object{
				&cnpgv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cluster",
						Namespace: "default",
					},
				},
			},
			cluster: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-cluster",
					Namespace: "default",
				},
			},
		},
		{
			name: "already deleted cluster returns nil",
			cluster: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gone-cluster",
					Namespace: "default",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			err := deleteCNPGCluster(context.Background(), c, tt.cluster)

			require.NoError(t, err)
		})
	}
}

func TestGeneratePassword(t *testing.T) {
	pw, err := generatePassword()

	require.NoError(t, err)
	assert.Len(t, pw, 32)

	t.Run("generates unique passwords", func(t *testing.T) {
		pw2, err := generatePassword()

		require.NoError(t, err)
		assert.NotEqual(t, pw, pw2)
	})
}

func TestValidateCrossResource_VersionFloor(t *testing.T) {
	makeClass := func(floor string) *platformv1alpha1.PostgresClusterClass {
		return &platformv1alpha1.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "cls"},
			Spec: platformv1alpha1.PostgresClusterClassSpec{
				Provisioner: "postgresql.cnpg.io",
				Config:      &platformv1alpha1.PostgresClusterClassConfig{PostgresVersion: ptr.To(floor)},
			},
		}
	}
	makeCluster := func(version string) *platformv1alpha1.PostgresCluster {
		return &platformv1alpha1.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg"},
			Spec:       platformv1alpha1.PostgresClusterSpec{PostgresVersion: ptr.To(version)},
		}
	}

	tests := []struct {
		name       string
		classFloor string
		clusterVer string
		wantErr    bool
	}{
		{"major-only cluster below minor floor", "17.2", "17", true},
		{"major-only cluster equal to major-only floor", "17", "17", false},
		{"minor cluster equal to floor", "17.2", "17.2", false},
		{"minor cluster above floor", "17.2", "17.3", false},
		{"minor cluster below floor", "17.2", "17.1", true},
		{"higher major bypasses minor floor", "17.2", "18", false},
		{"lower major rejected", "17.2", "16.9", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateCrossResource(makeClass(tt.classFloor), makeCluster(tt.clusterVer))
			if tt.wantErr {
				assert.NotEmpty(t, errs, "expected validation error")
			} else {
				assert.Empty(t, errs, "expected no validation error")
			}
		})
	}
}
