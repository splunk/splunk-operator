package controller

import (
	"context"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
			assert.Equal(t, tt.expected, got)
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
			assert.Equal(t, tt.expected, got)
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
			assert.Equal(t, tt.expected, got)
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

		require.NoError(t, err)
		assert.Equal(t, int32(5), *cfg.ClusterSpec.Instances)
		assert.Equal(t, "18", *cfg.ClusterSpec.PostgresVersion)
		assert.Equal(t, "100Gi", cfg.ClusterSpec.Storage.String())
		assert.Equal(t, "200", cfg.ClusterSpec.PostgreSQLConfig["max_connections"])
		assert.Equal(t, "hostssl all all 0.0.0.0/0 scram-sha-256", cfg.ClusterSpec.PgHBA[0])
	})

	t.Run("class defaults fill in nil cluster fields", func(t *testing.T) {
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg, err := r.getMergedConfig(baseClass, cluster)

		require.NoError(t, err)
		assert.Equal(t, int32(1), *cfg.ClusterSpec.Instances)
		assert.Equal(t, "17", *cfg.ClusterSpec.PostgresVersion)
		assert.Equal(t, "50Gi", cfg.ClusterSpec.Storage.String())
		assert.Equal(t, "128MB", cfg.ClusterSpec.PostgreSQLConfig["shared_buffers"])
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

		require.Error(t, err)
	})

	t.Run("CNPG config comes from class not cluster", func(t *testing.T) {
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg, err := r.getMergedConfig(baseClass, cluster)

		require.NoError(t, err)
		require.NotNil(t, cfg.ProvisionerConfig)
		assert.Equal(t, "switchover", cfg.ProvisionerConfig.PrimaryUpdateMethod)
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

		require.NoError(t, err)
		assert.NotNil(t, cfg.ClusterSpec.PostgreSQLConfig)
		assert.NotNil(t, cfg.ClusterSpec.PgHBA)
		assert.NotNil(t, cfg.ClusterSpec.Resources)
	})
}

func TestBuildCNPGClusterSpec(t *testing.T) {
	r := &PostgresClusterReconciler{}

	version := "18"
	instances := int32(3)
	storage := resource.MustParse("50Gi")
	cfg := &EffectiveClusterConfig{
		ClusterSpec: &enterprisev4.PostgresClusterSpec{
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

	assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18", spec.ImageName)
	assert.Equal(t, 3, spec.Instances)
	require.NotNil(t, spec.SuperuserSecret)
	assert.Equal(t, "my-secret", spec.SuperuserSecret.Name)
	assert.Equal(t, "my-secret", spec.Bootstrap.InitDB.Secret.Name)
	require.NotNil(t, spec.EnableSuperuserAccess)
	assert.True(t, *spec.EnableSuperuserAccess)
	assert.Equal(t, "postgres", spec.Bootstrap.InitDB.Database)
	assert.Equal(t, "postgres", spec.Bootstrap.InitDB.Owner)
	assert.Equal(t, "50Gi", spec.StorageConfiguration.Size)
	assert.Equal(t, "256MB", spec.PostgresConfiguration.Parameters["shared_buffers"])
	assert.Equal(t, "200", spec.PostgresConfiguration.Parameters["max_connections"])
	require.Len(t, spec.PostgresConfiguration.PgHBA, 2)
	assert.Equal(t, "hostssl all all 0.0.0.0/0 scram-sha-256", spec.PostgresConfiguration.PgHBA[0])
	assert.Equal(t, "host replication all 10.0.0.0/8 md5", spec.PostgresConfiguration.PgHBA[1])
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
	cfg := &EffectiveClusterConfig{
		ProvisionerConfig: &enterprisev4.CNPGConfig{
			ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{
				Instances: &poolerInstances,
				Mode:      &poolerMode,
				Config:    map[string]string{"default_pool_size": "25"},
			},
		},
	}

	t.Run("rw pooler", func(t *testing.T) {
		pooler := r.buildCNPGPooler(postgresCluster, cfg, cnpgCluster, "rw")

		assert.Equal(t, "my-cluster-pooler-rw", pooler.Name)
		assert.Equal(t, "db-ns", pooler.Namespace)
		assert.Equal(t, "my-cluster", pooler.Spec.Cluster.Name)
		require.NotNil(t, pooler.Spec.Instances)
		assert.Equal(t, int32(3), *pooler.Spec.Instances)
		assert.Equal(t, cnpgv1.PoolerType("rw"), pooler.Spec.Type)
		assert.Equal(t, cnpgv1.PgBouncerPoolMode("transaction"), pooler.Spec.PgBouncer.PoolMode)
		assert.Equal(t, "25", pooler.Spec.PgBouncer.Parameters["default_pool_size"])
		require.Len(t, pooler.OwnerReferences, 1)
		assert.Equal(t, "test-uid", string(pooler.OwnerReferences[0].UID))
	})

	t.Run("ro pooler", func(t *testing.T) {
		pooler := r.buildCNPGPooler(postgresCluster, cfg, cnpgCluster, "ro")

		assert.Equal(t, "my-cluster-pooler-ro", pooler.Name)
		assert.Equal(t, cnpgv1.PoolerType("ro"), pooler.Spec.Type)
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
	cfg := &EffectiveClusterConfig{
		ClusterSpec: &enterprisev4.PostgresClusterSpec{
			Instances:        &instances,
			PostgresVersion:  &version,
			Storage:          &storage,
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
			Resources:        &corev1.ResourceRequirements{},
		},
	}

	cluster := r.buildCNPGCluster(postgresCluster, cfg, "my-secret")

	assert.Equal(t, "my-cluster", cluster.Name)
	assert.Equal(t, "db-ns", cluster.Namespace)
	require.Len(t, cluster.OwnerReferences, 1)
	assert.Equal(t, "pg-uid", string(cluster.OwnerReferences[0].UID))
	assert.Equal(t, 3, cluster.Spec.Instances)
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

			require.NoError(t, err)
			assert.Equal(t, tt.expectedExists, exists)
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
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}
		original := existing.DeepCopy()
		existing.Data["key"] = []byte("new-value")

		err := r.patchObject(context.Background(), original, existing, "Secret")

		require.NoError(t, err)
		patched := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existing), patched))
		assert.Equal(t, "new-value", string(patched.Data["key"]))
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
			r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

			err := r.deleteCNPGCluster(context.Background(), tt.cluster)

			require.NoError(t, err)
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

			assert.Equal(t, tt.expected, got)
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

		require.NoError(t, err)
		assert.True(t, apierrors.IsNotFound(c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, &cnpgv1.Pooler{})))
		assert.True(t, apierrors.IsNotFound(c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-ro", Namespace: "default"}, &cnpgv1.Pooler{})))
	})

	t.Run("no-op when no poolers exist", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

		err := r.deleteConnectionPoolers(context.Background(), cluster)

		require.NoError(t, err)
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

		err := r.generateSecret(context.Background(), cluster, "my-secret")

		require.NoError(t, err)
		secret := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-secret", Namespace: "default"}, secret))
		assert.Equal(t, "my-secret", secret.Name)
		assert.Equal(t, "default", secret.Namespace)
		assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
		require.Len(t, secret.OwnerReferences, 1)
		assert.Equal(t, "cluster-uid", string(secret.OwnerReferences[0].UID))
	})

	t.Run("no-op when secret already exists", func(t *testing.T) {
		existing := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret",
				Namespace: "default",
			},
			StringData: map[string]string{"username": "existing-user"},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		r := &PostgresClusterReconciler{Client: c, Scheme: scheme}
		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cluster",
				Namespace: "default",
				UID:       "cluster-uid",
			},
		}

		err := r.generateSecret(context.Background(), cluster, "my-secret")

		require.NoError(t, err)
	})
}

func TestArePoolersReady(t *testing.T) {
	r := &PostgresClusterReconciler{}

	readyPooler := func(instances int32) *cnpgv1.Pooler {
		return &cnpgv1.Pooler{
			Spec:   cnpgv1.PoolerSpec{Instances: ptr.To(instances)},
			Status: cnpgv1.PoolerStatus{Instances: instances},
		}
	}
	notReadyPooler := func(desired, actual int32) *cnpgv1.Pooler {
		return &cnpgv1.Pooler{
			Spec:   cnpgv1.PoolerSpec{Instances: ptr.To(desired)},
			Status: cnpgv1.PoolerStatus{Instances: actual},
		}
	}

	tests := []struct {
		name     string
		rw       *cnpgv1.Pooler
		ro       *cnpgv1.Pooler
		expected bool
	}{
		{
			name:     "returns true when both poolers are ready",
			rw:       readyPooler(2),
			ro:       readyPooler(2),
			expected: true,
		},
		{
			name:     "returns false when rw pooler not ready",
			rw:       notReadyPooler(2, 0),
			ro:       readyPooler(2),
			expected: false,
		},
		{
			name:     "returns false when ro pooler not ready",
			rw:       readyPooler(2),
			ro:       notReadyPooler(2, 1),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.arePoolersReady(tt.rw, tt.ro)
			assert.Equal(t, tt.expected, got)
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
	cfg := &EffectiveClusterConfig{
		ProvisionerConfig: &enterprisev4.CNPGConfig{
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
		expectInstances int32
	}{
		{
			name:            "creates pooler when it does not exist",
			objects:         nil,
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
			expectInstances: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()
			r := &PostgresClusterReconciler{Client: c, Scheme: scheme}

			err := r.createConnectionPooler(context.Background(), cluster.DeepCopy(), cfg, cnpg, "rw")

			require.NoError(t, err)
			fetched := &cnpgv1.Pooler{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, fetched))
			require.NotNil(t, fetched.Spec.Instances)
			assert.Equal(t, tt.expectInstances, *fetched.Spec.Instances)
		})
	}
}

func TestGenerateConfigMap(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}

	t.Run("base endpoints without poolers", func(t *testing.T) {
		r := &PostgresClusterReconciler{Scheme: scheme}
		cm, err := r.generateConfigMap(cluster.DeepCopy(), "my-secret", false)

		require.NoError(t, err)
		assert.Equal(t, "my-cluster-configmap", cm.Name)
		assert.Equal(t, "default", cm.Namespace)
		assert.Equal(t, "my-cluster-rw.default", cm.Data["CLUSTER_RW_ENDPOINT"])
		assert.Equal(t, "my-cluster-ro.default", cm.Data["CLUSTER_RO_ENDPOINT"])
		assert.Equal(t, "my-cluster-r.default", cm.Data["CLUSTER_R_ENDPOINT"])
		assert.Equal(t, "5432", cm.Data["DEFAULT_CLUSTER_PORT"])
		assert.Equal(t, "postgres", cm.Data["SUPER_USER_NAME"])
		assert.Equal(t, "my-secret", cm.Data["SUPER_USER_SECRET_REF"])
		assert.NotContains(t, cm.Data, "CLUSTER_POOLER_RW_ENDPOINT")
		require.Len(t, cm.OwnerReferences, 1)
		assert.Equal(t, "cluster-uid", string(cm.OwnerReferences[0].UID))
	})

	t.Run("includes pooler endpoints when poolerEnabled is true", func(t *testing.T) {
		r := &PostgresClusterReconciler{Scheme: scheme}
		cm, err := r.generateConfigMap(cluster.DeepCopy(), "my-secret", true)

		require.NoError(t, err)
		assert.Equal(t, "my-cluster-pooler-rw.default", cm.Data["CLUSTER_POOLER_RW_ENDPOINT"])
		assert.Equal(t, "my-cluster-pooler-ro.default", cm.Data["CLUSTER_POOLER_RO_ENDPOINT"])
	})

	t.Run("uses existing configmap name from status", func(t *testing.T) {
		r := &PostgresClusterReconciler{Scheme: scheme}
		pg := cluster.DeepCopy()
		pg.Status.Resources = &enterprisev4.PostgresClusterResources{
			ConfigMapRef: &corev1.LocalObjectReference{Name: "custom-configmap"},
		}

		cm, err := r.generateConfigMap(pg, "my-secret", false)

		require.NoError(t, err)
		assert.Equal(t, "custom-configmap", cm.Name)
	})
}
