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
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconcileErrorPassdownToObserve(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")
	mergedConfig := &MergedConfig{
		Spec: &enterprisev4.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &enterprisev4.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}},
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
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status: enterprisev4.PostgresClusterStatus{
						Resources: &enterprisev4.PostgresClusterResources{
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
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Spec: enterprisev4.PostgresClusterSpec{
						ManagedRoles: []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
					},
				}
				contracts := &reconcileContracts{
					CNPGCluster: &cnpgv1.Cluster{
						ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
						Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
					},
					Secret: &corev1.Secret{},
				}
				return newManagedRolesModel(
					patchErrorClient{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), err: assert.AnError},
					scheme, noopEventEmitter{}, updateStatus, cluster, contracts,
				)
			},
		},
		{
			name:              "pooler component: Create error surfaced through Observe",
			expectedCondition: poolerReady,
			expectedReason:    reasonPoolerReconciliationFailed,
			build: func(updateStatus healthStatusUpdater) component {
				poolerInstances := int32(2)
				poolerMode := enterprisev4.ConnectionPoolerModeTransaction
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				}
				contracts := &reconcileContracts{
					CNPGCluster: &cnpgv1.Cluster{
						ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
						Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
					},
				}
				poolerSpec := mergedConfig.Spec.DeepCopy()
				poolerSpec.ConnectionPooler = &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}
				poolerCfg := &MergedConfig{
					Spec: poolerSpec,
					CNPG: &enterprisev4.CNPGConfig{
						ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{
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
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     enterprisev4.PostgresClusterStatus{Resources: &enterprisev4.PostgresClusterResources{}},
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
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     enterprisev4.PostgresClusterStatus{Resources: &enterprisev4.PostgresClusterResources{}},
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
			updateStatus := func(_ *enterprisev4.PostgresClusterStatus, health componentHealth) error {
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
		Spec: &enterprisev4.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &enterprisev4.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}},
		},
	}

	tests := []struct {
		name  string
		build func(events *captureEventEmitter) component
	}{
		{
			name: "cluster component emits warning from Observe",
			build: func(events *captureEventEmitter) component {
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status: enterprisev4.PostgresClusterStatus{
						Resources: &enterprisev4.PostgresClusterResources{
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
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     enterprisev4.PostgresClusterStatus{Resources: &enterprisev4.PostgresClusterResources{}},
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
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     enterprisev4.PostgresClusterStatus{Resources: &enterprisev4.PostgresClusterResources{}},
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
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Spec: enterprisev4.PostgresClusterSpec{
						ManagedRoles: []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
					},
				}
				cnpg := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
				contracts := &reconcileContracts{CNPGCluster: cnpg, Secret: &corev1.Secret{}}
				return newManagedRolesModel(
					patchErrorClient{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), err: assert.AnError},
					scheme, events, nil, cluster, contracts,
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

func TestHandleFinalizerUnknownDeletionPolicy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	now := metav1.Now()
	unknownPolicy := "delete" // typo — lowercase, not a valid constant
	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pg1",
			Namespace:         "default",
			DeletionTimestamp: &now,
			Finalizers:        []string{PostgresClusterFinalizerName},
		},
		Spec: enterprisev4.PostgresClusterSpec{
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

func TestRemoveOwnerRef(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	owner := &enterprisev4.PostgresCluster{
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
		APIVersion: "enterprise.splunk.com/v4",
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
