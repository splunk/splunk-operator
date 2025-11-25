package cnpg

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

func TestCNPGProvider_Name(t *testing.T) {
	provider := &CNPGProvider{}
	if provider.Name() != "CNPG" {
		t.Errorf("Expected provider name 'CNPG', got '%s'", provider.Name())
	}
}

func TestCNPGProvider_Ensure_MissingConfig(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	provider := &CNPGProvider{Client: client}

	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				// CNPG config is nil
			},
		},
	}

	ctx := context.Background()
	_, err := provider.Ensure(ctx, db)
	if err == nil {
		t.Error("Expected error when CNPG config is nil, got nil")
	}
	if err.Error() != "CNPG provider config is nil" {
		t.Errorf("Expected 'CNPG provider config is nil' error, got: %v", err)
	}
}

func TestCNPGProvider_Ensure_CreatesCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	provider := &CNPGProvider{Client: client}

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

	ctx := context.Background()
	result, err := provider.Ensure(ctx, db)
	if err != nil {
		t.Fatalf("Ensure() failed: %v", err)
	}

	// Verify endpoints are set
	expectedRW := "test-db-rw.default.svc.cluster.local"
	if result.Endpoints.ReadWrite != expectedRW {
		t.Errorf("Expected ReadWrite endpoint '%s', got '%s'", expectedRW, result.Endpoints.ReadWrite)
	}

	expectedRO := "test-db-ro.default.svc.cluster.local"
	if result.Endpoints.ReadOnly != expectedRO {
		t.Errorf("Expected ReadOnly endpoint '%s', got '%s'", expectedRO, result.Endpoints.ReadOnly)
	}

	// Verify CNPG cluster was created
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(gvkCluster)
	err = client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-db"}, cluster)
	if err != nil {
		t.Fatalf("Expected CNPG cluster to be created, got error: %v", err)
	}

	// Verify owner reference
	ownerRefs := cluster.GetOwnerReferences()
	if len(ownerRefs) != 1 {
		t.Fatalf("Expected 1 owner reference, got %d", len(ownerRefs))
	}
	if ownerRefs[0].UID != db.UID {
		t.Errorf("Expected owner UID '%s', got '%s'", db.UID, ownerRefs[0].UID)
	}
	if ownerRefs[0].Kind != "DatabaseCluster" {
		t.Errorf("Expected owner kind 'DatabaseCluster', got '%s'", ownerRefs[0].Kind)
	}
	if !*ownerRefs[0].Controller {
		t.Error("Expected Controller to be true")
	}
	if !*ownerRefs[0].BlockOwnerDeletion {
		t.Error("Expected BlockOwnerDeletion to be true")
	}
}

func TestCNPGProvider_ExtractConnectionData(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create secret with connection data
	secret := &corev1.Secret{
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
		WithObjects(secret).
		Build()

	provider := &CNPGProvider{Client: client}

	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	data, err := provider.extractConnectionData(ctx, db)
	if err != nil {
		t.Fatalf("extractConnectionData() failed: %v", err)
	}

	// Verify all required keys are present
	requiredKeys := []string{"host", "port", "username", "password", "dbname", "sslmode"}
	for _, key := range requiredKeys {
		if _, ok := data[key]; !ok {
			t.Errorf("Missing required key '%s' in connection data", key)
		}
	}

	// Verify values
	if string(data["host"]) != "test-db-rw.default.svc.cluster.local" {
		t.Errorf("Expected host 'test-db-rw.default.svc.cluster.local', got '%s'", string(data["host"]))
	}
	if string(data["port"]) != "5432" {
		t.Errorf("Expected port '5432', got '%s'", string(data["port"]))
	}
	if string(data["username"]) != "postgres" {
		t.Errorf("Expected username 'postgres', got '%s'", string(data["username"]))
	}
	if string(data["password"]) != "secret123" {
		t.Errorf("Expected password 'secret123', got '%s'", string(data["password"]))
	}
	if string(data["dbname"]) != "app" {
		t.Errorf("Expected dbname 'app', got '%s'", string(data["dbname"]))
	}
	if string(data["sslmode"]) != "require" {
		t.Errorf("Expected sslmode 'require', got '%s'", string(data["sslmode"]))
	}
}

func TestCNPGProvider_ExtractConnectionData_SecretNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	provider := &CNPGProvider{Client: client}

	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	data, err := provider.extractConnectionData(ctx, db)

	// Should return nil data and nil error when secret not found (cluster still initializing)
	if err != nil {
		t.Errorf("Expected nil error, got: %v", err)
	}
	if data != nil {
		t.Errorf("Expected nil data, got: %v", data)
	}
}

func TestCNPGProvider_ExtractConnectionData_IncompleteSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create secret with incomplete data
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db-app",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"username": []byte("postgres"),
			// Missing password and dbname
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret).
		Build()

	provider := &CNPGProvider{Client: client}

	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	_, err := provider.extractConnectionData(ctx, db)

	if err == nil {
		t.Error("Expected error for incomplete connection data, got nil")
	}
	if err.Error() != "incomplete connection data in CNPG secret" {
		t.Errorf("Expected 'incomplete connection data' error, got: %v", err)
	}
}

func TestCNPGProvider_Delete(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create a mock CNPG cluster
	cluster := &unstructured.Unstructured{}
	cluster.SetGroupVersionKind(gvkCluster)
	cluster.SetName("test-db")
	cluster.SetNamespace("default")

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cluster).
		Build()

	provider := &CNPGProvider{Client: client}

	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	err := provider.Delete(ctx, db)
	if err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	// Verify cluster was deleted
	err = client.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-db"}, cluster)
	if err == nil {
		t.Error("Expected CNPG cluster to be deleted, but it still exists")
	}
}

func TestCNPGProvider_GetStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name           string
		cnpgPhase      string
		readyInstances int64
		totalInstances int64
		expectedPhase  string
		expectedMsg    string
	}{
		{
			name:           "Healthy cluster",
			cnpgPhase:      "Cluster in healthy state",
			readyInstances: 3,
			totalInstances: 3,
			expectedPhase:  "Ready",
			expectedMsg:    "3/3 instances ready",
		},
		{
			name:           "Creating resources",
			cnpgPhase:      "Creating resources",
			readyInstances: 0,
			totalInstances: 3,
			expectedPhase:  "Provisioning",
			expectedMsg:    "Creating resources",
		},
		{
			name:           "Not ready",
			cnpgPhase:      "Cluster is not ready",
			readyInstances: 1,
			totalInstances: 3,
			expectedPhase:  "Degraded",
			expectedMsg:    "1/3 instances ready",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock CNPG cluster with status
			cluster := &unstructured.Unstructured{}
			cluster.SetGroupVersionKind(gvkCluster)
			cluster.SetName("test-db")
			cluster.SetNamespace("default")
			cluster.Object["status"] = map[string]interface{}{
				"phase":          tt.cnpgPhase,
				"readyInstances": tt.readyInstances,
				"instances":      tt.totalInstances,
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(cluster).
				Build()

			provider := &CNPGProvider{Client: client}

			db := &v4.DatabaseCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-db",
					Namespace: "default",
				},
			}

			ctx := context.Background()
			status, err := provider.GetStatus(ctx, db)
			if err != nil {
				t.Fatalf("GetStatus() failed: %v", err)
			}

			if status.Phase != tt.expectedPhase {
				t.Errorf("Expected phase '%s', got '%s'", tt.expectedPhase, status.Phase)
			}
			if status.Message != tt.expectedMsg {
				t.Errorf("Expected message '%s', got '%s'", tt.expectedMsg, status.Message)
			}
		})
	}
}

func TestCNPGProvider_TriggerBackup(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	provider := &CNPGProvider{Client: client}

	backup := &v4.DatabaseBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-backup",
			Namespace: "default",
			UID:       "backup-uid",
		},
		Spec: v4.DatabaseBackupSpec{
			TargetRef: v4.LocalRef{
				Name: "test-db",
			},
			BackupLabel: "manual-test",
		},
	}

	srcCluster := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
	}

	ctx := context.Background()
	backupID, err := provider.TriggerBackup(ctx, backup, srcCluster)
	if err != nil {
		t.Fatalf("TriggerBackup() failed: %v", err)
	}

	expectedID := "test-backup-cnpg"
	if backupID != expectedID {
		t.Errorf("Expected backup ID '%s', got '%s'", expectedID, backupID)
	}

	// Verify CNPG Backup was created
	cnpgBackup := &unstructured.Unstructured{}
	cnpgBackup.SetGroupVersionKind(gvkBackup)
	err = client.Get(ctx, types.NamespacedName{Namespace: "default", Name: expectedID}, cnpgBackup)
	if err != nil {
		t.Fatalf("Expected CNPG Backup to be created, got error: %v", err)
	}

	// Verify label
	labels := cnpgBackup.GetLabels()
	if labels["database.splunk.com/backup-label"] != "manual-test" {
		t.Errorf("Expected backup label 'manual-test', got '%s'", labels["database.splunk.com/backup-label"])
	}

	// Verify owner reference
	ownerRefs := cnpgBackup.GetOwnerReferences()
	if len(ownerRefs) != 1 {
		t.Fatalf("Expected 1 owner reference, got %d", len(ownerRefs))
	}
	if ownerRefs[0].UID != backup.UID {
		t.Errorf("Expected owner UID '%s', got '%s'", backup.UID, ownerRefs[0].UID)
	}
}

func TestBuildCNPGClusterSpec(t *testing.T) {
	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				CNPG: &v4.CNPGProviderSpec{
					Instances: 3,
					Storage: v4.StorageSpec{
						Size:             "100Gi",
						StorageClassName: "gp3",
					},
					BootstrapInitDB: &v4.InitSpec{
						CreateDatabaseIfMissing: true,
						DatabaseName:            "myapp",
						Owner:                   "appuser",
					},
				},
			},
			Backup: &v4.BackupIntent{
				Enabled:       true,
				RetentionDays: 30,
			},
		},
	}

	spec := buildCNPGClusterSpec(db, db.Spec.Provider.CNPG)

	// Verify instances
	instances, ok := spec["instances"].(int64)
	if !ok || instances != 3 {
		t.Errorf("Expected instances 3, got %v", spec["instances"])
	}

	// Verify storage
	storage, ok := spec["storage"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected storage to be a map")
	}
	if storage["size"] != "100Gi" {
		t.Errorf("Expected storage size '100Gi', got '%v'", storage["size"])
	}
	if storage["storageClass"] != "gp3" {
		t.Errorf("Expected storage class 'gp3', got '%v'", storage["storageClass"])
	}

	// Verify bootstrap
	bootstrap, ok := spec["bootstrap"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected bootstrap to be a map")
	}
	initdb, ok := bootstrap["initdb"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected initdb to be a map")
	}
	if initdb["database"] != "myapp" {
		t.Errorf("Expected database 'myapp', got '%v'", initdb["database"])
	}
	if initdb["owner"] != "appuser" {
		t.Errorf("Expected owner 'appuser', got '%v'", initdb["owner"])
	}
}

func TestBuildCNPGClusterSpec_ImageName(t *testing.T) {
	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "10Gi",
					},
					ImageName: "ghcr.io/cloudnative-pg/postgresql:15.3",
				},
			},
		},
	}

	spec := buildCNPGClusterSpec(db, db.Spec.Provider.CNPG)

	// Verify imageName
	imageName, ok := spec["imageName"].(string)
	if !ok {
		t.Fatal("Expected imageName to be a string")
	}
	if imageName != "ghcr.io/cloudnative-pg/postgresql:15.3" {
		t.Errorf("Expected imageName 'ghcr.io/cloudnative-pg/postgresql:15.3', got '%s'", imageName)
	}
}

func TestBuildCNPGClusterSpec_ImageCatalogRef(t *testing.T) {
	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "10Gi",
					},
					ImageCatalogRef: &v4.ImageCatalogRef{
						APIGroup: "postgresql.cnpg.io",
						Kind:     "ClusterImageCatalog",
						Name:     "postgresql-15",
					},
				},
			},
		},
	}

	spec := buildCNPGClusterSpec(db, db.Spec.Provider.CNPG)

	// Verify imageCatalogRef
	imageCatalogRef, ok := spec["imageCatalogRef"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected imageCatalogRef to be a map")
	}
	if imageCatalogRef["apiGroup"] != "postgresql.cnpg.io" {
		t.Errorf("Expected apiGroup 'postgresql.cnpg.io', got '%v'", imageCatalogRef["apiGroup"])
	}
	if imageCatalogRef["kind"] != "ClusterImageCatalog" {
		t.Errorf("Expected kind 'ClusterImageCatalog', got '%v'", imageCatalogRef["kind"])
	}
	if imageCatalogRef["name"] != "postgresql-15" {
		t.Errorf("Expected name 'postgresql-15', got '%v'", imageCatalogRef["name"])
	}
}

func TestBuildCNPGClusterSpec_ServiceAccountTemplate(t *testing.T) {
	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "10Gi",
					},
					ServiceAccountTemplate: &v4.ServiceAccountTemplate{
						Metadata: v4.ServiceAccountMetadata{
							Annotations: map[string]string{
								"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/my-db-role",
							},
							Labels: map[string]string{
								"app":         "postgresql",
								"environment": "production",
							},
						},
					},
				},
			},
		},
	}

	spec := buildCNPGClusterSpec(db, db.Spec.Provider.CNPG)

	// Verify serviceAccountTemplate
	saTemplate, ok := spec["serviceAccountTemplate"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected serviceAccountTemplate to be a map")
	}

	metadata, ok := saTemplate["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected metadata to be a map")
	}

	// Verify annotations
	annotations, ok := metadata["annotations"].(map[string]string)
	if !ok {
		t.Fatal("Expected annotations to be a map[string]string")
	}
	expectedRoleArn := "arn:aws:iam::123456789012:role/my-db-role"
	if annotations["eks.amazonaws.com/role-arn"] != expectedRoleArn {
		t.Errorf("Expected role ARN '%s', got '%s'", expectedRoleArn, annotations["eks.amazonaws.com/role-arn"])
	}

	// Verify labels
	labels, ok := metadata["labels"].(map[string]string)
	if !ok {
		t.Fatal("Expected labels to be a map[string]string")
	}
	if labels["app"] != "postgresql" {
		t.Errorf("Expected label app='postgresql', got '%s'", labels["app"])
	}
	if labels["environment"] != "production" {
		t.Errorf("Expected label environment='production', got '%s'", labels["environment"])
	}
}

func TestBuildCNPGClusterSpec_ServiceAccountTemplate_AnnotationsOnly(t *testing.T) {
	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "10Gi",
					},
					ServiceAccountTemplate: &v4.ServiceAccountTemplate{
						Metadata: v4.ServiceAccountMetadata{
							Annotations: map[string]string{
								"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/my-db-role",
							},
							// No labels
						},
					},
				},
			},
		},
	}

	spec := buildCNPGClusterSpec(db, db.Spec.Provider.CNPG)

	// Verify serviceAccountTemplate exists
	saTemplate, ok := spec["serviceAccountTemplate"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected serviceAccountTemplate to be a map")
	}

	metadata, ok := saTemplate["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected metadata to be a map")
	}

	// Verify annotations exist
	annotations, ok := metadata["annotations"].(map[string]string)
	if !ok {
		t.Fatal("Expected annotations to be a map[string]string")
	}
	if len(annotations) != 1 {
		t.Errorf("Expected 1 annotation, got %d", len(annotations))
	}

	// Verify labels are not present
	if _, hasLabels := metadata["labels"]; hasLabels {
		t.Error("Expected no labels, but labels field exists")
	}
}

func TestBuildCNPGClusterSpec_AllNewFeatures(t *testing.T) {
	// Test all three new features together
	db := &v4.DatabaseCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-db",
			Namespace: "default",
		},
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				CNPG: &v4.CNPGProviderSpec{
					Instances: 3,
					Storage: v4.StorageSpec{
						Size:             "100Gi",
						StorageClassName: "gp3",
					},
					ImageName: "ghcr.io/cloudnative-pg/postgresql:15.3",
					ImageCatalogRef: &v4.ImageCatalogRef{
						APIGroup: "postgresql.cnpg.io",
						Kind:     "ClusterImageCatalog",
						Name:     "postgresql-15",
					},
					ServiceAccountTemplate: &v4.ServiceAccountTemplate{
						Metadata: v4.ServiceAccountMetadata{
							Annotations: map[string]string{
								"eks.amazonaws.com/role-arn": "arn:aws:iam::123456789012:role/my-db-role",
							},
						},
					},
				},
			},
		},
	}

	spec := buildCNPGClusterSpec(db, db.Spec.Provider.CNPG)

	// Verify all three features are present
	if _, ok := spec["imageName"]; !ok {
		t.Error("Expected imageName to be present")
	}
	if _, ok := spec["imageCatalogRef"]; !ok {
		t.Error("Expected imageCatalogRef to be present")
	}
	if _, ok := spec["serviceAccountTemplate"]; !ok {
		t.Error("Expected serviceAccountTemplate to be present")
	}

	// Note: In real CNPG usage, you typically use either imageName OR imageCatalogRef, not both.
	// But we test both here to ensure the code handles both fields correctly.
}
