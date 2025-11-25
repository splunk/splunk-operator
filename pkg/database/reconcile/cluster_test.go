package reconcile

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

func TestReconcileDatabaseCluster_AddsFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: v4.DatabaseClusterSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(db).
		WithStatusSubresource(db).
		Build()

	ctx := context.Background()
	_, err := ReconcileDatabaseCluster(ctx, client, db)
	if err != nil {
		t.Fatalf("ReconcileDatabaseCluster() failed: %v", err)
	}

	// Retrieve updated object
	var updated v4.DatabaseCluster
	if err := client.Get(ctx, types.NamespacedName{Name: "test-db", Namespace: "default"}, &updated); err != nil {
		t.Fatalf("Failed to get updated DatabaseCluster: %v", err)
	}

	// Verify finalizer was added
	hasFinalizer := false
	for _, f := range updated.Finalizers {
		if f == connectionSecretFinalizer {
			hasFinalizer = true
			break
		}
	}
	if !hasFinalizer {
		t.Error("Expected finalizer to be added, but it wasn't")
	}
}

func TestReconcileDatabaseCluster_CreatesNormalizedSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-db",
			Namespace:  "default",
			UID:        "test-uid",
			Finalizers: []string{connectionSecretFinalizer},
		},
		Spec: v4.DatabaseClusterSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
				},
			},
			Connection: &v4.ConnectionSpec{
				SecretName: "test-db-conn",
			},
		},
	}

	// Create CNPG app secret that provider would read
	cnpgSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db-app",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"username": []byte("postgres"),
			"password": []byte("secret123"),
			"dbname":   []byte("app"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(db, cnpgSecret).
		WithStatusSubresource(db).
		Build()

	ctx := context.Background()
	_, err := ReconcileDatabaseCluster(ctx, client, db)
	if err != nil {
		t.Fatalf("ReconcileDatabaseCluster() failed: %v", err)
	}

	// Verify normalized secret was created
	var connSecret corev1.Secret
	err = client.Get(ctx, types.NamespacedName{Name: "test-db-conn", Namespace: "default"}, &connSecret)
	if err != nil {
		t.Fatalf("Expected normalized connection secret to be created, got error: %v", err)
	}

	// Verify secret has all required keys
	requiredKeys := []string{"host", "port", "username", "password", "dbname", "sslmode"}
	for _, key := range requiredKeys {
		if _, ok := connSecret.Data[key]; !ok {
			t.Errorf("Missing required key '%s' in connection secret", key)
		}
	}

	// Verify owner reference
	if len(connSecret.OwnerReferences) != 1 {
		t.Fatalf("Expected 1 owner reference, got %d", len(connSecret.OwnerReferences))
	}
	if connSecret.OwnerReferences[0].UID != db.UID {
		t.Errorf("Expected owner UID '%s', got '%s'", db.UID, connSecret.OwnerReferences[0].UID)
	}

	// Verify labels
	if connSecret.Labels["app.kubernetes.io/managed-by"] != "splunk-operator" {
		t.Error("Expected managed-by label to be 'splunk-operator'")
	}
	if connSecret.Labels["database.splunk.com/cluster"] != "test-db" {
		t.Error("Expected cluster label to be 'test-db'")
	}
}

func TestReconcileDatabaseCluster_UpdatesStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-db",
			Namespace:  "default",
			UID:        "test-uid",
			Finalizers: []string{connectionSecretFinalizer},
		},
		Spec: v4.DatabaseClusterSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
				},
			},
			Connection: &v4.ConnectionSpec{
				SecretName: "test-db-conn",
			},
		},
	}

	// Create CNPG app secret
	cnpgSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db-app",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"username": []byte("postgres"),
			"password": []byte("secret123"),
			"dbname":   []byte("app"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(db, cnpgSecret).
		WithStatusSubresource(db).
		Build()

	ctx := context.Background()
	requeue, err := ReconcileDatabaseCluster(ctx, client, db)
	if err != nil {
		t.Fatalf("ReconcileDatabaseCluster() failed: %v", err)
	}

	// Verify requeue time
	if requeue == 0 {
		t.Error("Expected non-zero requeue time")
	}

	// The reconcile function modifies db in-place, check the modified object
	// (Note: In real controller, status is persisted via r.Status().Update())

	// Verify status fields were set on the object
	if db.Status.Provider != "CNPG" {
		t.Errorf("Expected provider 'CNPG', got '%s'", db.Status.Provider)
	}
	if db.Status.ConnectionSecret != "test-db-conn" {
		t.Errorf("Expected connection secret 'test-db-conn', got '%s'", db.Status.ConnectionSecret)
	}
	if db.Status.Endpoints == nil {
		t.Fatal("Expected endpoints to be set")
	}
	expectedRW := "test-db-rw.default.svc.cluster.local"
	if db.Status.Endpoints.ReadWrite != expectedRW {
		t.Errorf("Expected ReadWrite endpoint '%s', got '%s'", expectedRW, db.Status.Endpoints.ReadWrite)
	}

	// Verify conditions
	if len(db.Status.Conditions) == 0 {
		t.Error("Expected conditions to be set")
	}

	// Find Ready condition
	var readyCondition *metav1.Condition
	for i := range db.Status.Conditions {
		if db.Status.Conditions[i].Type == "Ready" {
			readyCondition = &db.Status.Conditions[i]
			break
		}
	}
	if readyCondition == nil {
		t.Fatal("Expected Ready condition to be set")
	}
}

func TestReconcileDatabaseCluster_HandlesDeletion(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	now := metav1.Now()
	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-db",
			Namespace:         "default",
			UID:               "test-uid",
			Finalizers:        []string{connectionSecretFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: v4.DatabaseClusterSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
				},
			},
			ReclaimPolicy: "Delete",
		},
	}

	// Create connection secret that should be deleted
	connSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db-conn",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"host": []byte("test"),
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(db, connSecret).
		WithStatusSubresource(db).
		Build()

	ctx := context.Background()
	_, err := ReconcileDatabaseCluster(ctx, client, db)
	if err != nil {
		t.Fatalf("ReconcileDatabaseCluster() failed during deletion: %v", err)
	}

	// Verify finalizer was removed from the object passed in
	hasFinalizer := false
	for _, f := range db.Finalizers {
		if f == connectionSecretFinalizer {
			hasFinalizer = true
			break
		}
	}
	if hasFinalizer {
		t.Error("Expected finalizer to be removed, but it's still present")
	}

	// Verify connection secret was deleted
	var secret corev1.Secret
	err = client.Get(ctx, types.NamespacedName{Name: "test-db-conn", Namespace: "default"}, &secret)
	if err == nil {
		t.Error("Expected connection secret to be deleted, but it still exists")
	}
}

func TestReconcileDatabaseCluster_RespectsReclaimPolicyRetain(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	now := metav1.Now()
	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-db",
			Namespace:         "default",
			UID:               "test-uid",
			Finalizers:        []string{connectionSecretFinalizer},
			DeletionTimestamp: &now,
		},
		Spec: v4.DatabaseClusterSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
				},
			},
			ReclaimPolicy: "Retain",
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(db).
		WithStatusSubresource(db).
		Build()

	ctx := context.Background()
	_, err := ReconcileDatabaseCluster(ctx, client, db)
	if err != nil {
		t.Fatalf("ReconcileDatabaseCluster() failed during deletion with Retain: %v", err)
	}

	// Verify finalizer was removed (cleanup still happens)
	hasFinalizer := false
	for _, f := range db.Finalizers {
		if f == connectionSecretFinalizer {
			hasFinalizer = true
			break
		}
	}
	if hasFinalizer {
		t.Error("Expected finalizer to be removed even with Retain policy")
	}
}

func TestConnectionSecretName(t *testing.T) {
	tests := []struct {
		name     string
		db       *v4.DatabaseCluster
		expected string
	}{
		{
			name: "Explicit secret name",
			db: &v4.DatabaseCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db"},
				Spec: v4.DatabaseClusterSpec{
					Connection: &v4.ConnectionSpec{
						SecretName: "custom-secret",
					},
				},
			},
			expected: "custom-secret",
		},
		{
			name: "Default secret name",
			db: &v4.DatabaseCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db"},
				Spec:       v4.DatabaseClusterSpec{},
			},
			expected: "test-db-conn",
		},
		{
			name: "Empty secret name falls back to default",
			db: &v4.DatabaseCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db"},
				Spec: v4.DatabaseClusterSpec{
					Connection: &v4.ConnectionSpec{
						SecretName: "",
					},
				},
			},
			expected: "test-db-conn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := connectionSecretName(tt.db)
			if result != tt.expected {
				t.Errorf("Expected secret name '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestBuildWaitMessage(t *testing.T) {
	tests := []struct {
		name        string
		provReady   bool
		secretReady bool
		certsReady  bool
		expected    string
	}{
		{
			name:        "All ready",
			provReady:   true,
			secretReady: true,
			certsReady:  true,
			expected:    "All checks passed",
		},
		{
			name:        "Waiting on provider",
			provReady:   false,
			secretReady: true,
			certsReady:  true,
			expected:    "Waiting on: [provider]",
		},
		{
			name:        "Waiting on secret",
			provReady:   true,
			secretReady: false,
			certsReady:  true,
			expected:    "Waiting on: [connection secret]",
		},
		{
			name:        "Waiting on multiple",
			provReady:   false,
			secretReady: false,
			certsReady:  false,
			expected:    "Waiting on: [provider connection secret certificates]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildWaitMessage(tt.provReady, tt.secretReady, tt.certsReady)
			if result != tt.expected {
				t.Errorf("Expected message '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestProviderFor(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	tests := []struct {
		name         string
		providerType string
		expectedName string
	}{
		{
			name:         "CNPG provider",
			providerType: "CNPG",
			expectedName: "CNPG",
		},
		{
			name:         "Empty defaults to CNPG",
			providerType: "",
			expectedName: "CNPG",
		},
		{
			name:         "Unknown defaults to CNPG",
			providerType: "Unknown",
			expectedName: "CNPG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &v4.DatabaseCluster{
				Spec: v4.DatabaseClusterSpec{
					Provider: v4.ProviderConfig{
						Type: tt.providerType,
					},
				},
			}

			provider := providerFor(client, db)
			if provider.Name() != tt.expectedName {
				t.Errorf("Expected provider name '%s', got '%s'", tt.expectedName, provider.Name())
			}
		})
	}
}
