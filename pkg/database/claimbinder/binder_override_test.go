package claimbinder

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v4 "github.com/splunk/splunk-operator/api/v4"
)

func TestApplyStructuralOverrides_S3BucketOnly(t *testing.T) {
	// Base cluster from DatabaseClass
	cluster := v4.DatabaseCluster{
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "10Gi",
					},
					ObjectStore: &v4.ObjectStoreSpec{
						S3: &v4.S3Spec{
							Bucket:            "default-bucket",
							Path:              "default/path",
							Region:            "us-west-2",
							CredentialsSecret: "s3-creds",
						},
					},
				},
			},
		},
	}

	// Override just the S3 bucket
	overrides := &v4.DatabaseClaimOverrides{
		BackupConfig: &v4.BackupOverride{
			S3: &v4.S3Override{
				Bucket: "my-custom-bucket",
			},
		},
	}

	applyStructuralOverrides(&cluster, overrides)

	// Verify bucket was overridden
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket != "my-custom-bucket" {
		t.Errorf("Expected bucket 'my-custom-bucket', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket)
	}

	// Verify other S3 fields were preserved
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Path != "default/path" {
		t.Errorf("Expected path 'default/path', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Path)
	}
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Region != "us-west-2" {
		t.Errorf("Expected region 'us-west-2', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Region)
	}
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.CredentialsSecret != "s3-creds" {
		t.Errorf("Expected credentialsSecret 's3-creds', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.CredentialsSecret)
	}
}

func TestApplyStructuralOverrides_S3BucketAndPath(t *testing.T) {
	cluster := v4.DatabaseCluster{
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "10Gi",
					},
					ObjectStore: &v4.ObjectStoreSpec{
						S3: &v4.S3Spec{
							Bucket: "default-bucket",
							Path:   "default/path",
							Region: "us-west-2",
						},
					},
				},
			},
		},
	}

	overrides := &v4.DatabaseClaimOverrides{
		BackupConfig: &v4.BackupOverride{
			S3: &v4.S3Override{
				Bucket: "my-custom-bucket",
				Path:   "shc/my-cluster",
			},
		},
	}

	applyStructuralOverrides(&cluster, overrides)

	// Verify both bucket and path were overridden
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket != "my-custom-bucket" {
		t.Errorf("Expected bucket 'my-custom-bucket', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket)
	}
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Path != "shc/my-cluster" {
		t.Errorf("Expected path 'shc/my-cluster', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Path)
	}
}

func TestApplyStructuralOverrides_BackupEnabled(t *testing.T) {
	cluster := v4.DatabaseCluster{
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "10Gi",
					},
				},
			},
			Backup: &v4.BackupIntent{
				Enabled:       false,
				RetentionDays: 7,
			},
		},
	}

	enabled := true
	overrides := &v4.DatabaseClaimOverrides{
		BackupConfig: &v4.BackupOverride{
			Enabled: &enabled,
		},
	}

	applyStructuralOverrides(&cluster, overrides)

	// Verify backup was enabled
	if !cluster.Spec.Backup.Enabled {
		t.Error("Expected backup to be enabled")
	}
	// Verify other backup settings were preserved
	if cluster.Spec.Backup.RetentionDays != 7 {
		t.Errorf("Expected retentionDays 7, got %d", cluster.Spec.Backup.RetentionDays)
	}
}

func TestApplyStructuralOverrides_NoObjectStore(t *testing.T) {
	// Cluster with no ObjectStore configured
	cluster := v4.DatabaseCluster{
		Spec: v4.DatabaseClusterSpec{
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "10Gi",
					},
				},
			},
		},
	}

	overrides := &v4.DatabaseClaimOverrides{
		BackupConfig: &v4.BackupOverride{
			S3: &v4.S3Override{
				Bucket: "my-custom-bucket",
				Path:   "backups",
			},
		},
	}

	applyStructuralOverrides(&cluster, overrides)

	// Verify ObjectStore was created
	if cluster.Spec.Provider.CNPG.ObjectStore == nil {
		t.Fatal("Expected ObjectStore to be created")
	}
	if cluster.Spec.Provider.CNPG.ObjectStore.S3 == nil {
		t.Fatal("Expected S3 to be created")
	}
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket != "my-custom-bucket" {
		t.Errorf("Expected bucket 'my-custom-bucket', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket)
	}
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Path != "backups" {
		t.Errorf("Expected path 'backups', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Path)
	}
}

func TestSynthesize_WithOverrides(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim",
			Namespace: "default",
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:            "cnpg-standard",
			ConnectionSecretName: "test-conn",
			Overrides: &v4.DatabaseClaimOverrides{
				BackupConfig: &v4.BackupOverride{
					S3: &v4.S3Override{
						Bucket: "override-bucket",
						Path:   "override/path",
					},
				},
			},
		},
	}

	class := &v4.DatabaseClass{
		Spec: v4.DatabaseClassSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size: "20Gi",
					},
					ObjectStore: &v4.ObjectStoreSpec{
						S3: &v4.S3Spec{
							Bucket:            "default-bucket",
							Path:              "default/path",
							Region:            "us-east-1",
							CredentialsSecret: "s3-creds",
						},
					},
				},
			},
			Backup: &v4.BackupIntent{
				Enabled:       true,
				RetentionDays: 30,
			},
		},
	}

	cluster, err := synthesizeCluster(claim, class)
	if err != nil {
		t.Fatalf("synthesizeCluster failed: %v", err)
	}

	// Verify overrides were applied
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket != "override-bucket" {
		t.Errorf("Expected bucket 'override-bucket', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Bucket)
	}
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Path != "override/path" {
		t.Errorf("Expected path 'override/path', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Path)
	}

	// Verify non-overridden values were preserved from class
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.Region != "us-east-1" {
		t.Errorf("Expected region 'us-east-1', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.Region)
	}
	if cluster.Spec.Provider.CNPG.ObjectStore.S3.CredentialsSecret != "s3-creds" {
		t.Errorf("Expected credentialsSecret 's3-creds', got '%s'", cluster.Spec.Provider.CNPG.ObjectStore.S3.CredentialsSecret)
	}
	if cluster.Spec.Provider.CNPG.Instances != 1 {
		t.Errorf("Expected instances 1, got %d", cluster.Spec.Provider.CNPG.Instances)
	}
	if cluster.Spec.Provider.CNPG.Storage.Size != "20Gi" {
		t.Errorf("Expected storage size '20Gi', got '%s'", cluster.Spec.Provider.CNPG.Storage.Size)
	}
	if !cluster.Spec.Backup.Enabled {
		t.Error("Expected backup to be enabled")
	}
	if cluster.Spec.Backup.RetentionDays != 30 {
		t.Errorf("Expected retentionDays 30, got %d", cluster.Spec.Backup.RetentionDays)
	}
}

func TestReconcile_WithS3Override(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v4.AddToScheme(scheme)

	class := &v4.DatabaseClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cnpg-standard",
		},
		Spec: v4.DatabaseClassSpec{
			Engine: "Postgres",
			Provider: v4.ProviderConfig{
				Type: "CNPG",
				CNPG: &v4.CNPGProviderSpec{
					Instances: 1,
					Storage: v4.StorageSpec{
						Size:             "20Gi",
						StorageClassName: "gp2",
					},
					ObjectStore: &v4.ObjectStoreSpec{
						S3: &v4.S3Spec{
							Bucket:            "default-bucket",
							Path:              "default",
							Region:            "us-west-2",
							CredentialsSecret: "s3-creds",
						},
					},
				},
			},
			Backup: &v4.BackupIntent{
				Enabled:       true,
				RetentionDays: 30,
			},
		},
	}

	claim := &v4.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim",
			Namespace: "default",
		},
		Spec: v4.DatabaseClaimSpec{
			ClassName:            "cnpg-standard",
			ConnectionSecretName: "test-conn",
			Overrides: &v4.DatabaseClaimOverrides{
				BackupConfig: &v4.BackupOverride{
					S3: &v4.S3Override{
						Bucket: "my-shc-backups",
						Path:   "shc/my-cluster",
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(class, claim).
		Build()

	ctx := context.Background()

	// First reconcile adds finalizer
	if err := ReconcileDatabaseClaim(ctx, c, scheme, claim); err != nil {
		t.Fatalf("ReconcileDatabaseClaim (first pass) failed: %v", err)
	}

	// Get updated claim for second reconcile
	if err := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "test-claim"}, claim); err != nil {
		t.Fatalf("Failed to get updated claim: %v", err)
	}

	// Second reconcile creates the DatabaseCluster
	if err := ReconcileDatabaseClaim(ctx, c, scheme, claim); err != nil {
		t.Fatalf("ReconcileDatabaseClaim (second pass) failed: %v", err)
	}

	// Verify DatabaseCluster was created with S3 overrides
	var db v4.DatabaseCluster
	if err := c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "test-claim"}, &db); err != nil {
		t.Fatalf("Failed to get DatabaseCluster: %v", err)
	}

	if db.Spec.Provider.CNPG.ObjectStore.S3.Bucket != "my-shc-backups" {
		t.Errorf("Expected bucket 'my-shc-backups', got '%s'", db.Spec.Provider.CNPG.ObjectStore.S3.Bucket)
	}
	if db.Spec.Provider.CNPG.ObjectStore.S3.Path != "shc/my-cluster" {
		t.Errorf("Expected path 'shc/my-cluster', got '%s'", db.Spec.Provider.CNPG.ObjectStore.S3.Path)
	}

	// Verify non-overridden values from DatabaseClass were preserved
	if db.Spec.Provider.CNPG.ObjectStore.S3.Region != "us-west-2" {
		t.Errorf("Expected region 'us-west-2', got '%s'", db.Spec.Provider.CNPG.ObjectStore.S3.Region)
	}
	if db.Spec.Provider.CNPG.ObjectStore.S3.CredentialsSecret != "s3-creds" {
		t.Errorf("Expected credentialsSecret 's3-creds', got '%s'", db.Spec.Provider.CNPG.ObjectStore.S3.CredentialsSecret)
	}
	if db.Spec.Provider.CNPG.Instances != 1 {
		t.Errorf("Expected instances 1, got %d", db.Spec.Provider.CNPG.Instances)
	}
	if db.Spec.Provider.CNPG.Storage.Size != "20Gi" {
		t.Errorf("Expected storage size '20Gi', got '%s'", db.Spec.Provider.CNPG.Storage.Size)
	}
}
