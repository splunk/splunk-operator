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

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	sharedreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

// createExternalClusterSecret seeds an opaque Secret (without the cnpg.io/reload
// label unless one is supplied) so external-secret tests can drive validation,
// including the required-reload-label check.
func createExternalClusterSecret(t *testing.T, c client.Client, name, namespace string, labels map[string]string, data map[string][]byte) {
	t.Helper()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeOpaque,
	}
	if data != nil {
		secret.Data = data
	}
	if labels != nil {
		secret.Labels = labels
	}
	require.NoError(t, c.Create(t.Context(), secret))
}

// patchCountingClient counts Patch invocations so tests can assert that the
// operator never mutates an externally managed Secret.
type patchCountingClient struct {
	client.Client
	count int
}

func (p *patchCountingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	p.count++
	return p.Client.Patch(ctx, obj, patch, opts...)
}

// provideExternalSecretsPostgresCluster returns a cluster configured for the
// external-secret path with an initially empty superuser ref.
func provideExternalSecretsPostgresCluster() *enterprisev4.PostgresCluster {
	return &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterSpec{
			PasswordConfig: &enterprisev4.SuperuserPasswordConfig{
				SuperuserExternalSecretRef: corev1.LocalObjectReference{Name: ""},
			},
		},
		Status: enterprisev4.PostgresClusterStatus{
			Resources: &enterprisev4.PostgresClusterResources{},
		},
	}
}

// capturedStatus records the health passed to a secretModel's
// healthStatusUpdater so tests can assert the Observe -> writeComponentStatus
// contract (status is written exactly once, with the same health Observe returns).
type capturedStatus struct {
	health componentHealth
	count  int
}

func (cs *capturedStatus) updater() healthStatusUpdater {
	return func(_ *enterprisev4.PostgresClusterStatus, h componentHealth) error {
		cs.health = h
		cs.count++
		return nil
	}
}

// conflictUpdater simulates a status write that loses an optimistic-concurrency
// race: it records the health like the real updater but reports a 409 Conflict.
func (cs *capturedStatus) conflictUpdater() healthStatusUpdater {
	return cs.failingUpdater(apierrors.NewConflict(schema.GroupResource{Group: "enterprise.splunk.com", Resource: "postgresclusters"}, "pg1", errors.New("the object has been modified")))
}

// failingUpdater simulates a status write that records the health but fails with
// the supplied error (e.g. a transient API error), so callers can exercise the
// non-conflict status-write failure path.
func (cs *capturedStatus) failingUpdater(err error) healthStatusUpdater {
	return func(_ *enterprisev4.PostgresClusterStatus, h componentHealth) error {
		cs.health = h
		cs.count++
		return err
	}
}

// TestSecretModel_ExternalSecretActuate covers the external-secret branch of
// secretModel through the public Reconcile + Observe contract. Each sub-test
// drives the full cycle and pins both the returned (health, err) tuple and the
// deferred updateStatus invocation that Observe wraps around computeHealth.
func TestSecretModel_ExternalSecretActuate(t *testing.T) {
	scheme := newTestScheme()

	t.Run("returns error when name is empty", func(t *testing.T) {
		cluster := provideExternalSecretsPostgresCluster()

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		status := &capturedStatus{}
		s := newSecretModel(c, scheme, noopEventEmitter{}, status.updater(), cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		health, err := s.Observe(t.Context(), reconcileErr)

		var invalidSecretErr secretReconcileError
		require.ErrorAs(t, err, &invalidSecretErr,
			"Observe must surface the empty-name guard set by reconcileExternalSecret")
		assert.Equal(t, reasonExternalSecretInvalid, invalidSecretErr.reason)
		assert.Equal(t, pgcConstants.Failed, health.State)
		assert.Equal(t, secretsReady, health.Condition)
		assert.Equal(t, reasonSuperUserSecretFailed, health.Reason)
		assert.Equal(t, failedClusterPhase, health.Phase)

		assert.Equal(t, 1, status.count, "Observe must invoke updateStatus exactly once")
		assert.Equal(t, health, status.health, "updateStatus must receive the same health Observe returns")
	})

	t.Run("returns secret missing when k8s api cant fetch", func(t *testing.T) {
		cluster := provideExternalSecretsPostgresCluster()
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = "external-superuser-secret"

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		status := &capturedStatus{}
		s := newSecretModel(c, scheme, noopEventEmitter{}, status.updater(), cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		health, err := s.Observe(t.Context(), reconcileErr)

		var missingSecretErr secretReconcileError
		require.ErrorAs(t, err, &missingSecretErr)
		assert.Equal(t, reasonExternalSecretMissing, missingSecretErr.reason,
			"a NotFound Get must produce secretReconcileError with reasonExternalSecretMissing")
		assert.True(t, errors.Is(err, reconcile.TerminalError(nil)),
			"a missing external secret is not retry-recoverable and must be terminal")
		assert.Equal(t, pgcConstants.Failed, health.State)
		assert.Equal(t, secretsReady, health.Condition)
		assert.Equal(t, reasonExternalSecretMissing, health.Reason)
		assert.Equal(t, failedClusterPhase, health.Phase)

		assert.Equal(t, 1, status.count)
		assert.Equal(t, health, status.health)
	})

	t.Run("missing secret with a status-write conflict requeues instead of terminalizing", func(t *testing.T) {
		// Regression: a missing external secret is terminal, but only once the
		// Failed condition is persisted. If the status write loses an
		// optimistic-concurrency race, Observe must surface a pure conflict so the
		// controller requeues — terminalizing here would embed the conflict in a
		// TerminalError that controller-runtime never requeues, stranding the
		// cluster with stale status until the next external-Secret event.
		cluster := provideExternalSecretsPostgresCluster()
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = "external-superuser-secret"

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		status := &capturedStatus{}
		s := newSecretModel(c, scheme, noopEventEmitter{}, status.conflictUpdater(), cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		health, err := s.Observe(t.Context(), reconcileErr)

		require.Error(t, err)
		assert.False(t, errors.Is(err, reconcile.TerminalError(nil)),
			"a status-write conflict must not be terminalized — it must stay requeueable")
		assert.True(t, sharedreconcile.IsPureConflict(err),
			"the returned error must be a pure conflict so the controller requeues to persist status")
		assert.Equal(t, pgcConstants.Failed, health.State)
		assert.Equal(t, reasonExternalSecretMissing, health.Reason)

		assert.Equal(t, 1, status.count, "Observe must still attempt the status write exactly once")
	})

	t.Run("missing secret with a transient status-write error stays requeueable", func(t *testing.T) {
		// Regression: a missing external secret is terminal, but only after the
		// Failed condition is persisted. If the status write fails with a
		// non-conflict transient error (e.g. API unavailability), terminalizing it
		// would suppress controller-runtime retry and the Failed condition might
		// never be persisted until a later Secret event. Observe must instead
		// surface the joined error non-terminally so the controller retries.
		cluster := provideExternalSecretsPostgresCluster()
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = "external-superuser-secret"

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		status := &capturedStatus{}
		transient := apierrors.NewServiceUnavailable("apiserver is on a coffee break")
		s := newSecretModel(c, scheme, noopEventEmitter{}, status.failingUpdater(transient), cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		health, err := s.Observe(t.Context(), reconcileErr)

		require.Error(t, err)
		assert.False(t, errors.Is(err, reconcile.TerminalError(nil)),
			"a transient status-write error must not be terminalized — it must stay requeueable")
		assert.False(t, sharedreconcile.IsPureConflict(err),
			"a non-conflict status error is not a pure conflict")
		assert.ErrorIs(t, err, transient,
			"the returned error must carry the status-write failure so controller-runtime retries")
		var missingSecretErr secretReconcileError
		assert.True(t, errors.As(err, &missingSecretErr),
			"the joined error must still carry the missing-secret cause")
		assert.Equal(t, pgcConstants.Failed, health.State)
		assert.Equal(t, reasonExternalSecretMissing, health.Reason)

		assert.Equal(t, 1, status.count, "Observe must still attempt the status write exactly once")
	})

	t.Run("succeeds when reload label present without mutating the secret", func(t *testing.T) {
		cluster := provideExternalSecretsPostgresCluster()
		const externalSecretName = "external-superuser-secret"
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = externalSecretName

		const foreignKey, foreignValue = "example", "karpatka"

		initialData := map[string][]byte{
			"username": []byte("postgres"),
			"password": []byte("EXT-su-pw"),
		}
		// The user/owner is responsible for setting the reload label.
		labels := map[string]string{foreignKey: foreignValue, labelCNPGReload: "true"}

		base := fake.NewClientBuilder().WithScheme(scheme).Build()
		createExternalClusterSecret(t, base, externalSecretName, cluster.Namespace, labels, initialData)

		counter := &patchCountingClient{Client: base}
		status := &capturedStatus{}
		s := newSecretModel(counter, scheme, noopEventEmitter{}, status.updater(), cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		health, err := s.Observe(t.Context(), reconcileErr)

		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Ready, health.State)
		assert.Equal(t, secretsReady, health.Condition)
		assert.Equal(t, reasonSuperUserSecretReady, health.Reason)
		assert.Equal(t, 0, counter.count,
			"operator is a pure consumer — it must never Patch an external secret")

		got := &corev1.Secret{}
		require.NoError(t, base.Get(t.Context(),
			client.ObjectKey{Name: externalSecretName, Namespace: cluster.Namespace}, got))
		assert.Equal(t, "true", got.Labels[labelCNPGReload])
		assert.Equal(t, foreignValue, got.Labels[foreignKey], "foreign labels must be preserved")

		assert.Equal(t, 1, status.count)
		assert.Equal(t, health, status.health)
	})

	t.Run("fails when reload label is absent — user must set it", func(t *testing.T) {
		cluster := provideExternalSecretsPostgresCluster()
		const externalSecretName = "external-superuser-secret"
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = externalSecretName

		initialData := map[string][]byte{
			"username": []byte("postgres"),
			"password": []byte("EXT-su-pw"),
		}

		base := fake.NewClientBuilder().WithScheme(scheme).Build()
		// No cnpg.io/reload label — the operator must reject rather than stamp it.
		createExternalClusterSecret(t, base, externalSecretName, cluster.Namespace, nil, initialData)

		counter := &patchCountingClient{Client: base}
		status := &capturedStatus{}
		s := newSecretModel(counter, scheme, noopEventEmitter{}, status.updater(), cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		health, err := s.Observe(t.Context(), reconcileErr)

		var secretReconcileErr secretReconcileError
		require.ErrorAs(t, err, &secretReconcileErr)
		assert.Equal(t, reasonExternalSecretMissingLabel, secretReconcileErr.reason)
		assert.False(t, errors.Is(err, reconcile.TerminalError(nil)),
			"a present-but-invalid secret stays requeueable — only an absent secret is terminal")
		assert.Equal(t, pgcConstants.Failed, health.State)
		assert.Equal(t, reasonExternalSecretMissingLabel, health.Reason)
		assert.Equal(t, 0, counter.count,
			"operator must never add the label itself")

		// The secret must be left untouched — no label was added behind the user's back.
		got := &corev1.Secret{}
		require.NoError(t, base.Get(t.Context(),
			client.ObjectKey{Name: externalSecretName, Namespace: cluster.Namespace}, got))
		assert.NotContains(t, got.Labels, labelCNPGReload)

		assert.Equal(t, 1, status.count)
		assert.Equal(t, health, status.health)
	})

	t.Run("non-NotFound Get error is not classified as missing", func(t *testing.T) {
		cluster := provideExternalSecretsPostgresCluster()
		const externalSecretName = "external-superuser-secret"
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = externalSecretName

		base := fake.NewClientBuilder().WithScheme(scheme).Build()
		errClient := getErrorClient{
			Client: base,
			err:    apierrors.NewServiceUnavailable("apiserver is on a coffee break"),
			matcher: func(obj client.Object) bool {
				_, ok := obj.(*corev1.Secret)
				return ok
			},
		}

		status := &capturedStatus{}
		s := newSecretModel(errClient, scheme, noopEventEmitter{}, status.updater(), cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		health, err := s.Observe(t.Context(), reconcileErr)

		require.Error(t, err)
		var reconcileErrType secretReconcileError
		assert.False(t, errors.As(err, &reconcileErrType),
			"a non-NotFound/transient Get error must not be classified as secretReconcileError")
		assert.False(t, errors.Is(err, reconcile.TerminalError(nil)),
			"a transient API error must stay requeueable, never terminal")
		assert.NotEqual(t, reasonExternalSecretMissing, health.Reason,
			"transient/operational error must not flip health to ExternalSecretMissing")

		assert.Equal(t, 1, status.count)
		assert.Equal(t, health, status.health)
	})

	t.Run("never writes .data on external Secret", func(t *testing.T) {
		cluster := provideExternalSecretsPostgresCluster()
		const externalSecretName = "external-superuser-secret"
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = externalSecretName

		initialData := map[string][]byte{
			"username": []byte("postgres"),
			"password": []byte("EXT-su-pw"),
		}

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		createExternalClusterSecret(t, c, externalSecretName, cluster.Namespace,
			map[string]string{labelCNPGReload: "true"}, initialData)

		s := newSecretModel(c, scheme, noopEventEmitter{}, nil, cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		_, err := s.Observe(t.Context(), reconcileErr)
		require.NoError(t, err)

		got := &corev1.Secret{}
		require.NoError(t, c.Get(t.Context(),
			client.ObjectKey{Name: externalSecretName, Namespace: cluster.Namespace}, got))
		// The operator never mutates external secrets at all, so .data is unchanged.
		assert.Equal(t, initialData, got.Data,
			"operator must never write to externally managed .data")
		assert.Equal(t, "true", got.Labels[labelCNPGReload])
	})

	t.Run("marks secret failed when data map is empty missing username-password keys", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		var secretReconcileErr secretReconcileError

		// missing a key (password absent)
		cluster := provideExternalSecretsPostgresCluster()
		missingKey := "missing-key-secret"
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = missingKey
		createExternalClusterSecret(t, c, missingKey, cluster.Namespace, nil, map[string][]byte{
			"username": []byte("postgres"),
		})

		s := newSecretModel(c, scheme, noopEventEmitter{}, nil, cluster, "pg1-superuser", &reconcileContracts{})
		reconcileErr := s.Reconcile(t.Context())
		health, err := s.Observe(t.Context(), reconcileErr)
		require.Error(t, err)
		require.ErrorAs(t, err, &secretReconcileErr)
		require.Equal(t, reasonExternalSecretMissingKeys, health.Reason)
		require.Equal(t, reasonExternalSecretMissingKeys, secretReconcileErr.reason)

		// missing data map entirely
		cluster = provideExternalSecretsPostgresCluster()
		missingData := "missing-data-secret"
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = missingData
		createExternalClusterSecret(t, c, missingData, cluster.Namespace, nil, nil)

		s = newSecretModel(c, scheme, noopEventEmitter{}, nil, cluster, "pg213-superuser", &reconcileContracts{})
		reconcileErr = s.Reconcile(t.Context())
		health, err = s.Observe(t.Context(), reconcileErr)
		require.Error(t, err)
		require.ErrorAs(t, err, &secretReconcileErr)
		require.Equal(t, reasonExternalSecretMissingData, health.Reason)
		require.Equal(t, reasonExternalSecretMissingData, secretReconcileErr.reason)

		// username not postgres
		cluster = provideExternalSecretsPostgresCluster()
		usernameNotPg := "username-not-postgres"
		cluster.Spec.PasswordConfig.SuperuserExternalSecretRef.Name = usernameNotPg
		createExternalClusterSecret(t, c, usernameNotPg, cluster.Namespace, nil, map[string][]byte{
			"username": []byte("not-so-postgres"),
			"password": []byte("random"),
		})

		s = newSecretModel(c, scheme, noopEventEmitter{}, nil, cluster, "pg214-superuser", &reconcileContracts{})
		reconcileErr = s.Reconcile(t.Context())
		health, err = s.Observe(t.Context(), reconcileErr)
		require.Error(t, err)
		require.ErrorAs(t, err, &secretReconcileErr)
		require.Equal(t, reasonExternalSecretInvalidUsername, health.Reason)
		require.Equal(t, reasonExternalSecretInvalidUsername, secretReconcileErr.reason)
	})
}

// TestSecretModel_ActuateDispatch pins the dispatch in Reconcile/computeHealth:
// PasswordConfig presence selects the external vs. internal branch. Each sub-test
// drives the full Reconcile + Observe cycle so the externally observable contract
// — the returned (health, err) tuple — is the assertion subject.
func TestSecretModel_ActuateDispatch(t *testing.T) {
	scheme := newTestScheme()

	t.Run("Reconcile routes to external path when PasswordConfig is set", func(t *testing.T) {
		cluster := provideExternalSecretsPostgresCluster()
		// An empty ref drives the external path's invalid-name branch — the
		// signal we use to distinguish external from internal routing here.

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		s := newSecretModel(c, scheme, noopEventEmitter{}, nil, cluster, "pg1-superuser", &reconcileContracts{})

		reconcileErr := s.Reconcile(t.Context())
		_, err := s.Observe(t.Context(), reconcileErr)

		var invalidSecretErr secretReconcileError
		require.ErrorAs(t, err, &invalidSecretErr,
			"PasswordConfig set must dispatch to reconcileExternalSecret, not the owned-secret path")
		assert.Equal(t, reasonExternalSecretInvalid, invalidSecretErr.reason)
	})

	t.Run("Reconcile routes to internal path when PasswordConfig is nil", func(t *testing.T) {
		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
			Status:     enterprisev4.PostgresClusterStatus{Resources: &enterprisev4.PostgresClusterResources{}},
		}

		c := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&enterprisev4.PostgresCluster{}).
			Build()
		contracts := &reconcileContracts{}
		s := newSecretModel(c, scheme, noopEventEmitter{}, nil, cluster, "pg1-superuser", contracts)

		reconcileErr := s.Reconcile(t.Context())
		require.NoError(t, reconcileErr)

		// The owned path uniquely creates a Secret named s.name in the cluster's
		// namespace; the external path never does — its presence is the dispatch signal.
		owned := &corev1.Secret{}
		require.NoError(t, c.Get(t.Context(),
			client.ObjectKey{Name: "pg1-superuser", Namespace: cluster.Namespace}, owned),
			"PasswordConfig nil must dispatch to the owned-secret path")
		require.Len(t, owned.OwnerReferences, 1,
			"owned Secret must carry an ownerReference to the cluster")

		// The fake client doesn't run the apiserver's StringData -> Data
		// conversion; mirror it onto the published contract so computeHealth's
		// .data["password"] check sees the freshly minted password.
		if contracts.Secret != nil && contracts.Secret.Data == nil {
			contracts.Secret.Data = map[string][]byte{}
			for k, v := range contracts.Secret.StringData {
				contracts.Secret.Data[k] = []byte(v)
			}
		}

		health, err := s.Observe(t.Context(), reconcileErr)
		require.NoError(t, err)
		assert.Equal(t, pgcConstants.Ready, health.State,
			"internal path must reach Ready once the Secret carries .data[\"password\"]")
		assert.Equal(t, reasonSuperUserSecretReady, health.Reason)
	})
}
