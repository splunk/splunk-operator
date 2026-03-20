package controller

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPoolerResourceName(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		poolerType  string
		expected    string
	}{
		{
			name:        "read-write pooler",
			clusterName: "my-cluster",
			poolerType:  "rw",
			expected:    "my-cluster-pooler-rw",
		},
		{
			name:        "cluster name with mixed case and alphanumeric suffix",
			clusterName: "My-Cluster-12x2f",
			poolerType:  "rw",
			expected:    "My-Cluster-12x2f-pooler-rw",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := poolerResourceName(tt.clusterName, tt.poolerType)
			if got != tt.expected {
				t.Errorf("poolerResourceName(%q, %q) = %q, want %q",
					tt.clusterName, tt.poolerType, got, tt.expected)
			}
		})
	}
}

func TestIsPoolerReady(t *testing.T) {
	r := &PostgresClusterReconciler{}
	tests := []struct {
		name     string
		pooler   *cnpgv1.Pooler
		expected bool
	}{
		{
			name: "nil instances defaults desired to 1, zero scheduled means not ready",
			pooler: &cnpgv1.Pooler{
				Status: cnpgv1.PoolerStatus{Instances: 0},
			},
			expected: false,
		},
		{
			name: "nil instances defaults desired to 1, one scheduled means ready",
			pooler: &cnpgv1.Pooler{
				Status: cnpgv1.PoolerStatus{Instances: 1},
			},
			expected: true,
		},
		{
			name: "scheduled meets desired",
			pooler: &cnpgv1.Pooler{
				Spec:   cnpgv1.PoolerSpec{Instances: ptr.To(int32(3))},
				Status: cnpgv1.PoolerStatus{Instances: 3},
			},
			expected: true,
		},
		{
			name: "scheduled below desired",
			pooler: &cnpgv1.Pooler{
				Spec:   cnpgv1.PoolerSpec{Instances: ptr.To(int32(3))},
				Status: cnpgv1.PoolerStatus{Instances: 2},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got := r.isPoolerReady(tt.pooler)

			if got != tt.expected {
				t.Errorf("isPoolerReady() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNormalizeCNPGClusterSpec(t *testing.T) {
	tests := []struct {
		name                    string
		spec                    cnpgv1.ClusterSpec
		customDefinedParameters map[string]string
		expected                normalizedCNPGClusterSpec
	}{
		{
			name: "basic fields are copied",
			spec: cnpgv1.ClusterSpec{
				ImageName:            "ghcr.io/cloudnative-pg/postgresql:18",
				Instances:            3,
				StorageConfiguration: cnpgv1.StorageConfiguration{Size: "10Gi"},
			},
			customDefinedParameters: nil,
			expected: normalizedCNPGClusterSpec{
				ImageName:   "ghcr.io/cloudnative-pg/postgresql:18",
				Instances:   3,
				StorageSize: "10Gi",
			},
		},
		{
			name: "CNPG-injected parameters are excluded from comparison",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers":  "256MB",
						"max_connections": "200",
						"cnpg_injected":   "should-not-appear",
					},
				},
			},
			customDefinedParameters: map[string]string{
				"shared_buffers":  "256MB",
				"max_connections": "200",
			},
			expected: normalizedCNPGClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				CustomDefinedParameters: map[string]string{
					"shared_buffers":  "256MB",
					"max_connections": "200",
				},
			},
		},
		{
			name: "empty custom params does not populate CustomDefinedParameters",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					Parameters: map[string]string{"cnpg_injected": "val"},
				},
			},
			customDefinedParameters: map[string]string{},
			expected: normalizedCNPGClusterSpec{
				ImageName: "img:18",
				Instances: 1,
			},
		},
		{
			name: "PgHBA included when non-empty",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					PgHBA: []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PgHBA:     []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
			},
		},
		{
			name: "empty PgHBA is excluded",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				PostgresConfiguration: cnpgv1.PostgresConfiguration{
					PgHBA: []string{},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName: "img:18",
				Instances: 1,
			},
		},
		{
			name: "bootstrap populates database and owner",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Bootstrap: &cnpgv1.BootstrapConfiguration{
					InitDB: &cnpgv1.BootstrapInitDB{
						Database: "mydb",
						Owner:    "admin",
					},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:       "img:18",
				Instances:       1,
				DefaultDatabase: "mydb",
				Owner:           "admin",
			},
		},
		{
			name: "nil bootstrap leaves database and owner empty",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
			},
			expected: normalizedCNPGClusterSpec{
				ImageName: "img:18",
				Instances: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got := normalizeCNPGClusterSpec(tt.spec, tt.customDefinedParameters)

			if !equality.Semantic.DeepEqual(got, tt.expected) {
				t.Errorf("normalizeCNPGClusterSpec() =\n  %+v\nwant\n  %+v", got, tt.expected)
			}
		})
	}
}

func TestGetMergedConfig(t *testing.T) {
	r := &PostgresClusterReconciler{}

	classInstances := int32(1)
	classVersion := "17"
	classStorage := resource.MustParse("50Gi")
	baseClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "standard"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: enterprisev4.PosgresClusterClassConfig{
				Instances:        &classInstances,
				PostgresVersion:  &classVersion,
				Storage:          &classStorage,
				Resources:        &corev1.ResourceRequirements{},
				PostgreSQLConfig: map[string]string{"shared_buffers": "128MB"},
				PgHBA:            []string{"host all all 0.0.0.0/0 md5"},
			},
			CNPG: &enterprisev4.CNPGConfig{PrimaryUpdateMethod: "switchover"},
		},
	}

	t.Run("cluster spec overrides class defaults", func(t *testing.T) {
		overrideInstances := int32(5)
		overrideVersion := "18"
		overrideStorage := resource.MustParse("100Gi")
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{
				Instances:        &overrideInstances,
				PostgresVersion:  &overrideVersion,
				Storage:          &overrideStorage,
				PostgreSQLConfig: map[string]string{"max_connections": "200"},
				PgHBA:            []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
			},
		}

		cfg, err := r.getMergedConfig(baseClass, cluster)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if *cfg.Spec.Instances != 5 {
			t.Errorf("Instances = %d, want 5", *cfg.Spec.Instances)
		}
		if *cfg.Spec.PostgresVersion != "18" {
			t.Errorf("PostgresVersion = %s, want 18", *cfg.Spec.PostgresVersion)
		}
		if cfg.Spec.Storage.String() != "100Gi" {
			t.Errorf("Storage = %s, want 100Gi", cfg.Spec.Storage.String())
		}
		if cfg.Spec.PostgreSQLConfig["max_connections"] != "200" {
			t.Errorf("PostgreSQLConfig = %v, want max_connections=200", cfg.Spec.PostgreSQLConfig)
		}
		if cfg.Spec.PgHBA[0] != "hostssl all all 0.0.0.0/0 scram-sha-256" {
			t.Errorf("PgHBA = %v, want hostssl rule", cfg.Spec.PgHBA)
		}
	})

	t.Run("class defaults fill in nil cluster fields", func(t *testing.T) {
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg, err := r.getMergedConfig(baseClass, cluster)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if *cfg.Spec.Instances != 1 {
			t.Errorf("Instances = %d, want 1", *cfg.Spec.Instances)
		}
		if *cfg.Spec.PostgresVersion != "17" {
			t.Errorf("PostgresVersion = %s, want 17", *cfg.Spec.PostgresVersion)
		}
		if cfg.Spec.Storage.String() != "50Gi" {
			t.Errorf("Storage = %s, want 50Gi", cfg.Spec.Storage.String())
		}
		if cfg.Spec.PostgreSQLConfig["shared_buffers"] != "128MB" {
			t.Errorf("PostgreSQLConfig = %v, want shared_buffers=128MB", cfg.Spec.PostgreSQLConfig)
		}
	})

	t.Run("returns error when required fields missing from both", func(t *testing.T) {
		emptyClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "empty"},
			Spec:       enterprisev4.PostgresClusterClassSpec{},
		}
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		_, err := r.getMergedConfig(emptyClass, cluster)

		if err == nil {
			t.Fatal("expected error for missing required fields, got nil")
		}
	})

	t.Run("CNPG config comes from class not cluster", func(t *testing.T) {
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg, err := r.getMergedConfig(baseClass, cluster)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.CNPG == nil || cfg.CNPG.PrimaryUpdateMethod != "switchover" {
			t.Errorf("CNPG.PrimaryUpdateMethod = %v, want switchover", cfg.CNPG)
		}
	})

	t.Run("nil maps and slices initialized to safe zero values", func(t *testing.T) {
		classWithNoMaps := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "minimal"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: enterprisev4.PosgresClusterClassConfig{
					Instances:       &classInstances,
					PostgresVersion: &classVersion,
					Storage:         &classStorage,
				},
			},
		}
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg, err := r.getMergedConfig(classWithNoMaps, cluster)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Spec.PostgreSQLConfig == nil {
			t.Error("PostgreSQLConfig should be initialized, got nil")
		}
		if cfg.Spec.PgHBA == nil {
			t.Error("PgHBA should be initialized, got nil")
		}
		if cfg.Spec.Resources == nil {
			t.Error("Resources should be initialized, got nil")
		}
	})
}

func TestBuildCNPGClusterSpec(t *testing.T) {
	r := &PostgresClusterReconciler{}

	version := "18"
	instances := int32(3)
	storage := resource.MustParse("50Gi")
	cfg := &MergedConfig{
		Spec: &enterprisev4.PostgresClusterSpec{
			PostgresVersion: &version,
			Instances:       &instances,
			Storage:         &storage,
			PostgreSQLConfig: map[string]string{
				"shared_buffers":  "256MB",
				"max_connections": "200",
			},
			PgHBA: []string{
				"hostssl all all 0.0.0.0/0 scram-sha-256",
				"host replication all 10.0.0.0/8 md5",
			},
			Resources: &corev1.ResourceRequirements{},
		},
	}

	spec := r.buildCNPGClusterSpec(cfg, "my-secret")

	t.Run("image name includes version", func(t *testing.T) {

		if spec.ImageName != "ghcr.io/cloudnative-pg/postgresql:18" {
			t.Errorf("ImageName = %q, want ghcr.io/cloudnative-pg/postgresql:18", spec.ImageName)
		}
	})

	t.Run("instances matches config", func(t *testing.T) {

		if spec.Instances != 3 {
			t.Errorf("Instances = %d, want 3", spec.Instances)
		}
	})

	t.Run("secret wired into superuser and bootstrap", func(t *testing.T) {

		if spec.SuperuserSecret == nil || spec.SuperuserSecret.Name != "my-secret" {
			t.Errorf("SuperuserSecret.Name = %v, want my-secret", spec.SuperuserSecret)
		}
		if spec.Bootstrap.InitDB.Secret.Name != "my-secret" {
			t.Errorf("InitDB.Secret.Name = %q, want my-secret", spec.Bootstrap.InitDB.Secret.Name)
		}
	})

	t.Run("superuser access is always enabled", func(t *testing.T) {

		if spec.EnableSuperuserAccess == nil || !*spec.EnableSuperuserAccess {
			t.Error("EnableSuperuserAccess should be true")
		}
	})

	t.Run("bootstrap creates default database owned by postgres", func(t *testing.T) {

		if spec.Bootstrap.InitDB.Database != "postgres" {
			t.Errorf("InitDB.Database = %q, want postgres", spec.Bootstrap.InitDB.Database)
		}
		if spec.Bootstrap.InitDB.Owner != "postgres" {
			t.Errorf("InitDB.Owner = %q, want postgres", spec.Bootstrap.InitDB.Owner)
		}
	})

	t.Run("storage size matches config", func(t *testing.T) {

		if spec.StorageConfiguration.Size != "50Gi" {
			t.Errorf("StorageConfiguration.Size = %q, want 50Gi", spec.StorageConfiguration.Size)
		}
	})

	t.Run("postgres parameters passed through", func(t *testing.T) {

		if spec.PostgresConfiguration.Parameters["shared_buffers"] != "256MB" {
			t.Errorf("Parameters[shared_buffers] = %q, want 256MB", spec.PostgresConfiguration.Parameters["shared_buffers"])
		}
		if spec.PostgresConfiguration.Parameters["max_connections"] != "200" {
			t.Errorf("Parameters[max_connections] = %q, want 200", spec.PostgresConfiguration.Parameters["max_connections"])
		}
	})

	t.Run("pg_hba rules passed through", func(t *testing.T) {

		if len(spec.PostgresConfiguration.PgHBA) != 2 {
			t.Fatalf("PgHBA len = %d, want 2", len(spec.PostgresConfiguration.PgHBA))
		}
		if spec.PostgresConfiguration.PgHBA[0] != "hostssl all all 0.0.0.0/0 scram-sha-256" {
			t.Errorf("PgHBA[0] = %q, want hostssl rule", spec.PostgresConfiguration.PgHBA[0])
		}
		if spec.PostgresConfiguration.PgHBA[1] != "host replication all 10.0.0.0/8 md5" {
			t.Errorf("PgHBA[1] = %q, want replication rule", spec.PostgresConfiguration.PgHBA[1])
		}
	})
}

func TestBuildCNPGPooler(t *testing.T) {
	scheme := runtime.NewScheme()
	enterprisev4.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)
	r := &PostgresClusterReconciler{Scheme: scheme}

	poolerInstances := int32(3)
	poolerMode := enterprisev4.ConnectionPoolerModeTransaction
	postgresCluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "db-ns",
			UID:       "test-uid",
		},
	}
	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-cluster",
		},
	}
	cfg := &MergedConfig{
		CNPG: &enterprisev4.CNPGConfig{
			ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{
				Instances: &poolerInstances,
				Mode:      &poolerMode,
				Config:    map[string]string{"default_pool_size": "25"},
			},
		},
	}

	t.Run("rw pooler", func(t *testing.T) {

		pooler := r.buildCNPGPooler(postgresCluster, cfg, cnpgCluster, "rw")

		if pooler.Name != "my-cluster-pooler-rw" {
			t.Errorf("Name = %q, want my-cluster-pooler-rw", pooler.Name)
		}
		if pooler.Namespace != "db-ns" {
			t.Errorf("Namespace = %q, want db-ns", pooler.Namespace)
		}
		if pooler.Spec.Cluster.Name != "my-cluster" {
			t.Errorf("Cluster.Name = %q, want my-cluster", pooler.Spec.Cluster.Name)
		}
		if pooler.Spec.Instances == nil || *pooler.Spec.Instances != 3 {
			t.Errorf("Instances = %v, want 3", pooler.Spec.Instances)
		}
		if pooler.Spec.Type != cnpgv1.PoolerType("rw") {
			t.Errorf("Type = %q, want rw", pooler.Spec.Type)
		}
		if pooler.Spec.PgBouncer.PoolMode != cnpgv1.PgBouncerPoolMode("transaction") {
			t.Errorf("PoolMode = %q, want transaction", pooler.Spec.PgBouncer.PoolMode)
		}
		if pooler.Spec.PgBouncer.Parameters["default_pool_size"] != "25" {
			t.Errorf("Parameters[default_pool_size] = %q, want 25", pooler.Spec.PgBouncer.Parameters["default_pool_size"])
		}
		if len(pooler.OwnerReferences) != 1 || pooler.OwnerReferences[0].UID != "test-uid" {
			t.Errorf("OwnerReferences = %v, want single ref with UID test-uid", pooler.OwnerReferences)
		}
	})

	t.Run("ro pooler", func(t *testing.T) {

		pooler := r.buildCNPGPooler(postgresCluster, cfg, cnpgCluster, "ro")

		if pooler.Name != "my-cluster-pooler-ro" {
			t.Errorf("Name = %q, want my-cluster-pooler-ro", pooler.Name)
		}
		if pooler.Spec.Type != cnpgv1.PoolerType("ro") {
			t.Errorf("Type = %q, want ro", pooler.Spec.Type)
		}
	})
}

func TestBuildCNPGCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	enterprisev4.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)
	r := &PostgresClusterReconciler{Scheme: scheme}

	instances := int32(3)
	version := "18"
	storage := resource.MustParse("50Gi")
	postgresCluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "db-ns",
			UID:       "pg-uid",
		},
	}
	cfg := &MergedConfig{
		Spec: &enterprisev4.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storage,
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
			Resources:        &corev1.ResourceRequirements{},
		},
	}

	cluster := r.buildCNPGCluster(postgresCluster, cfg, "my-secret")

	if cluster.Name != "my-cluster" {
		t.Errorf("Name = %q, want my-cluster", cluster.Name)
	}
	if cluster.Namespace != "db-ns" {
		t.Errorf("Namespace = %q, want db-ns", cluster.Namespace)
	}
	if len(cluster.OwnerReferences) != 1 || cluster.OwnerReferences[0].UID != "pg-uid" {
		t.Errorf("OwnerReferences = %v, want single ref with UID pg-uid", cluster.OwnerReferences)
	}
	if cluster.Spec.Instances != 3 {
		t.Errorf("Spec.Instances = %d, want 3", cluster.Spec.Instances)
	}
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
			r := &PostgresClusterReconciler{Client: c, Scheme: scheme}
			secret := &corev1.Secret{}

			exists, err := r.clusterSecretExists(context.Background(), "default", tt.secretName, secret)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if exists != tt.expectedExists {
				t.Errorf("clusterSecretExists() = %v, want %v", exists, tt.expectedExists)
			}
		})
	}
}

func TestRemoveOwnerRef(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)
	r := &PostgresClusterReconciler{Scheme: scheme}

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

			removed, err := r.removeOwnerRef(owner, secret, "Secret")

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if removed != tt.expectedRemoved {
				t.Errorf("removeOwnerRef() = %v, want %v", removed, tt.expectedRemoved)
			}
			if len(secret.GetOwnerReferences()) != tt.expectedRefsLen {
				t.Errorf("OwnerReferences len = %d, want %d", len(secret.GetOwnerReferences()), tt.expectedRefsLen)
			}
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
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}
		original := existing.DeepCopy()
		existing.Data["key"] = []byte("new-value")

		err := r.patchObject(context.Background(), original, existing, "Secret")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		patched := &corev1.Secret{}
		c.Get(context.Background(), client.ObjectKeyFromObject(existing), patched)
		if string(patched.Data["key"]) != "new-value" {
			t.Errorf("Data[key] = %q, want new-value", string(patched.Data["key"]))
		}
	})

	t.Run("returns nil when object not found", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}
		original := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deleted-secret",
				Namespace: "default",
			},
		}
		modified := original.DeepCopy()
		modified.Data = map[string][]byte{"key": []byte("value")}

		err := r.patchObject(context.Background(), original, modified, "Secret")

		if err != nil {
			t.Errorf("expected nil for NotFound, got %v", err)
		}
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
			r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

			err := r.deleteCNPGCluster(context.Background(), tt.cluster)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPoolerExists(t *testing.T) {
	scheme := runtime.NewScheme()
	cnpgv1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
	}

	tests := []struct {
		name     string
		objects  []client.Object
		expected bool
	}{
		{
			name: "returns true when pooler exists",
			objects: []client.Object{
				&cnpgv1.Pooler{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cluster-pooler-rw",
						Namespace: "default",
					},
				},
			},
			expected: true,
		},
		{
			name: "returns false when given pooler is not found",
			objects: []client.Object{
				&cnpgv1.Pooler{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cluster-pooler-ro",
						Namespace: "default",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

			got := r.poolerExists(context.Background(), cluster, "rw")

			if got != tt.expected {
				t.Errorf("poolerExists() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDeleteConnectionPoolers(t *testing.T) {
	scheme := runtime.NewScheme()
	cnpgv1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
	}

	rwPooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster-pooler-rw",
			Namespace: "default",
		},
	}
	roPooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster-pooler-ro",
			Namespace: "default",
		},
	}

	t.Run("deletes both poolers when they exist", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler.DeepCopy(), roPooler.DeepCopy()).Build()
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

		err := r.deleteConnectionPoolers(context.Background(), cluster)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !apierrors.IsNotFound(c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, &cnpgv1.Pooler{})) {
			t.Error("expected rw pooler to be deleted")
		}
		if !apierrors.IsNotFound(c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-ro", Namespace: "default"}, &cnpgv1.Pooler{})) {
			t.Error("expected ro pooler to be deleted")
		}
	})

	t.Run("no-op when no poolers exist", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

		err := r.deleteConnectionPoolers(context.Background(), cluster)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGenerateSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	t.Run("creates secret with credentials and owner reference", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}
		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cluster",
				Namespace: "default",
				UID:       "cluster-uid",
			},
		}

		secret, err := r.generateSecret(context.Background(), cluster, "my-secret")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if secret.Name != "my-secret" {
			t.Errorf("Name = %q, want my-secret", secret.Name)
		}
		if secret.Namespace != "default" {
			t.Errorf("Namespace = %q, want default", secret.Namespace)
		}
		if secret.StringData["username"] != "postgres" {
			t.Errorf("username = %q, want postgres", secret.StringData["username"])
		}
		if len(secret.StringData["password"]) == 0 {
			t.Error("expected password to be generated")
		}
		if secret.Type != corev1.SecretTypeOpaque {
			t.Errorf("Type = %q, want Opaque", secret.Type)
		}
		if len(secret.OwnerReferences) != 1 || secret.OwnerReferences[0].UID != "cluster-uid" {
			t.Errorf("OwnerReferences = %v, want single ref with UID cluster-uid", secret.OwnerReferences)
		}
	})
}

func TestArePoolersReady(t *testing.T) {
	scheme := runtime.NewScheme()
	cnpgv1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
	}

	readyRW := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster-pooler-rw",
			Namespace: "default",
		},
		Spec:   cnpgv1.PoolerSpec{Instances: ptr.To(int32(2))},
		Status: cnpgv1.PoolerStatus{Instances: 2},
	}
	readyRO := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster-pooler-ro",
			Namespace: "default",
		},
		Spec:   cnpgv1.PoolerSpec{Instances: ptr.To(int32(2))},
		Status: cnpgv1.PoolerStatus{Instances: 2},
	}

	tests := []struct {
		name     string
		objects  []client.Object
		expected bool
	}{
		{
			name:     "returns true when both poolers are ready",
			objects:  []client.Object{readyRW.DeepCopy(), readyRO.DeepCopy()},
			expected: true,
		},
		{
			name:     "returns false when rw pooler not found",
			objects:  []client.Object{readyRO.DeepCopy()},
			expected: false,
		},
		{
			name:     "returns false when ro pooler not found",
			objects:  []client.Object{readyRW.DeepCopy()},
			expected: false,
		},
		{
			name: "returns false when rw pooler not ready",
			objects: []client.Object{
				func() *cnpgv1.Pooler {
					p := readyRW.DeepCopy()
					p.Status.Instances = 0
					return p
				}(),
				readyRO.DeepCopy(),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

			got := r.arePoolersReady(context.Background(), cluster)

			if got != tt.expected {
				t.Errorf("arePoolersReady() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCreateConnectionPooler(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	poolerInstances := int32(2)
	poolerMode := enterprisev4.ConnectionPoolerModeTransaction
	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
	}
	cfg := &MergedConfig{
		CNPG: &enterprisev4.CNPGConfig{
			ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{
				Instances: &poolerInstances,
				Mode:      &poolerMode,
				Config:    map[string]string{"default_pool_size": "25"},
			},
		},
	}

	tests := []struct {
		name            string
		objects         []client.Object
		expectCreated   bool
		expectInstances int32
	}{
		{
			name:            "creates pooler when it does not exist",
			objects:         nil,
			expectCreated:   true,
			expectInstances: 2,
		},
		{
			name: "no-op when pooler already exists",
			objects: []client.Object{
				&cnpgv1.Pooler{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cluster-pooler-rw",
						Namespace: "default",
					},
					Spec: cnpgv1.PoolerSpec{Instances: ptr.To(int32(1))},
				},
			},
			expectCreated:   false,
			expectInstances: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

			created, err := r.createConnectionPooler(context.Background(), cluster.DeepCopy(), cfg, cnpg, "rw")

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if created != tt.expectCreated {
				t.Errorf("created = %v, want %v", created, tt.expectCreated)
			}
			fetched := &cnpgv1.Pooler{}
			if err := c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, fetched); err != nil {
				t.Fatalf("expected pooler to exist: %v", err)
			}
			if fetched.Spec.Instances == nil || *fetched.Spec.Instances != tt.expectInstances {
				t.Errorf("Instances = %v, want %d", fetched.Spec.Instances, tt.expectInstances)
			}
		})
	}
}

func TestBuildConfigMapData(t *testing.T) {
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
	}

	t.Run("base endpoints without poolers", func(t *testing.T) {

		data := buildConfigMapData(cnpg, "my-secret", false)

		if data["CLUSTER_RW_ENDPOINT"] != "my-cluster-rw.default" {
			t.Errorf("CLUSTER_RW_ENDPOINT = %q, want my-cluster-rw.default", data["CLUSTER_RW_ENDPOINT"])
		}
		if data["CLUSTER_RO_ENDPOINT"] != "my-cluster-ro.default" {
			t.Errorf("CLUSTER_RO_ENDPOINT = %q, want my-cluster-ro.default", data["CLUSTER_RO_ENDPOINT"])
		}
		if data["CLUSTER_R_ENDPOINT"] != "my-cluster-r.default" {
			t.Errorf("CLUSTER_R_ENDPOINT = %q, want my-cluster-r.default", data["CLUSTER_R_ENDPOINT"])
		}
		if data["DEFAULT_CLUSTER_PORT"] != "5432" {
			t.Errorf("DEFAULT_CLUSTER_PORT = %q, want 5432", data["DEFAULT_CLUSTER_PORT"])
		}
		if data["SUPER_USER_NAME"] != "postgres" {
			t.Errorf("SUPER_USER_NAME = %q, want postgres", data["SUPER_USER_NAME"])
		}
		if data["SUPER_USER_SECRET_REF"] != "my-secret" {
			t.Errorf("SUPER_USER_SECRET_REF = %q, want my-secret", data["SUPER_USER_SECRET_REF"])
		}
		if _, ok := data["CLUSTER_POOLER_RW_ENDPOINT"]; ok {
			t.Error("expected no pooler endpoints when poolersExist is false")
		}
	})

	t.Run("includes pooler endpoints when poolersExist is true", func(t *testing.T) {

		data := buildConfigMapData(cnpg, "my-secret", true)

		if data["CLUSTER_POOLER_RW_ENDPOINT"] != "my-cluster-pooler-rw.default" {
			t.Errorf("CLUSTER_POOLER_RW_ENDPOINT = %q, want my-cluster-pooler-rw.default", data["CLUSTER_POOLER_RW_ENDPOINT"])
		}
		if data["CLUSTER_POOLER_RO_ENDPOINT"] != "my-cluster-pooler-ro.default" {
			t.Errorf("CLUSTER_POOLER_RO_ENDPOINT = %q, want my-cluster-pooler-ro.default", data["CLUSTER_POOLER_RO_ENDPOINT"])
		}
	})
}

func TestReconcileConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
	}

	t.Run("creates configmap with default name", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

		name, err := r.reconcileConfigMap(context.Background(), cluster.DeepCopy(), cnpg, "my-secret")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "my-cluster-configmap" {
			t.Errorf("name = %q, want my-cluster-configmap", name)
		}
		created := &corev1.ConfigMap{}
		if err := c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-configmap", Namespace: "default"}, created); err != nil {
			t.Fatalf("expected configmap to exist: %v", err)
		}
		if created.Data["CLUSTER_RW_ENDPOINT"] != "my-cluster-rw.default" {
			t.Errorf("CLUSTER_RW_ENDPOINT = %q, want my-cluster-rw.default", created.Data["CLUSTER_RW_ENDPOINT"])
		}
	})

	t.Run("uses existing configmap name from status", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}
		pg := cluster.DeepCopy()
		pg.Status.Resources = &enterprisev4.PostgresClusterResources{
			ConfigMapRef: &corev1.LocalObjectReference{Name: "custom-configmap"},
		}

		name, err := r.reconcileConfigMap(context.Background(), pg, cnpg, "my-secret")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != "custom-configmap" {
			t.Errorf("name = %q, want custom-configmap", name)
		}
	})
}
