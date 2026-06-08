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

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureClusterSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	t.Run("creates secret with credentials and owner reference", func(t *testing.T) {
		// Arrange
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cluster",
				Namespace: "default",
				UID:       "cluster-uid",
			},
		}

		// Act
		secret, err := ensureClusterSecret(context.Background(), c, scheme, cluster, "my-secret")

		// Assert
		require.NoError(t, err)
		require.NotNil(t, secret)
		fetched := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-secret", Namespace: "default"}, fetched))
		assert.Equal(t, "my-secret", fetched.Name)
		assert.Equal(t, "default", fetched.Namespace)
		assert.Equal(t, corev1.SecretTypeOpaque, fetched.Type)
		require.Len(t, fetched.OwnerReferences, 1)
		assert.Equal(t, "cluster-uid", string(fetched.OwnerReferences[0].UID))
	})
}

func TestClusterSecretExists(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)

	tests := []struct {
		name           string
		objects        []client.Object
		secretName     string
		expectedExists bool
	}{
		{
			name: "returns true when secret exists",
			objects: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-secret",
						Namespace: "default",
					},
				},
			},
			secretName:     "my-secret",
			expectedExists: true,
		},
		{
			name: "returns false when secret not found",
			objects: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-secret",
						Namespace: "default",
					},
				},
			},
			secretName:     "missing-secret",
			expectedExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			secret := &corev1.Secret{}

			exists, err := clusterSecretExists(context.Background(), c, "default", tt.secretName, secret)

			require.NoError(t, err)
			assert.Equal(t, tt.expectedExists, exists)
		})
	}
}

func TestSecretModelAdoptsOrphanedSecret(t *testing.T) {
	t.Parallel()

	// Arrange: secret exists but has no owner reference — secretModel must patch it.
	scheme := newTestScheme()
	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default", UID: "pg-uid"},
		Status:     enterprisev4.PostgresClusterStatus{Resources: &enterprisev4.PostgresClusterResources{}},
	}
	orphanedSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"},
		Data:       map[string][]byte{secretKeyPassword: []byte("s3cr3t")},
	}
	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(orphanedSecret).Build()
	contracts := &reconcileContracts{}
	model := newSecretModel(c, scheme, events, nil, cluster, "pg1-secret", contracts)

	// Act
	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Ready, health.State)
	adopted := &corev1.Secret{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1-secret", Namespace: "default"}, adopted))
	require.Len(t, adopted.OwnerReferences, 1)
	assert.Equal(t, cluster.Name, adopted.OwnerReferences[0].Name)
}

func TestSecretModelObserveFailsWhenPasswordKeyMissing(t *testing.T) {
	t.Parallel()

	// Arrange: secret exists but is missing the expected password key.
	scheme := newTestScheme()
	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status:     enterprisev4.PostgresClusterStatus{Resources: &enterprisev4.PostgresClusterResources{}},
	}
	secretWithoutPassword := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-secret", Namespace: "default"},
		Data:       map[string][]byte{"other-key": []byte("value")},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secretWithoutPassword).Build()
	model := newSecretModel(c, scheme, noopEventEmitter{}, nil, cluster, "pg1-secret", &reconcileContracts{})

	// Act
	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	// Assert
	require.NoError(t, reconcileErr)
	require.Error(t, err)
	assert.Equal(t, pgcConstants.Failed, health.State)
	assert.Equal(t, reasonSuperUserSecretFailed, health.Reason)
	assert.Contains(t, health.Message, secretKeyPassword)
}
