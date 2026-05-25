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
	"fmt"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type configMapNotFoundClient struct {
	client.Client
}

type getErrorClient struct {
	client.Client
	err        error
	matcher    func(client.Object) bool
	keyMatcher func(client.ObjectKey) bool
}

func (c getErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if c.keyMatcher != nil && c.keyMatcher(key) {
		return c.err
	}
	if c.matcher != nil && c.matcher(obj) {
		return c.err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

type createErrorClient struct {
	client.Client
	err     error
	matcher func(client.Object) bool
}

func (c createErrorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if c.matcher != nil && c.matcher(obj) {
		return c.err
	}
	return c.Client.Create(ctx, obj, opts...)
}

type patchErrorClient struct {
	client.Client
	err error
}

func (c patchErrorClient) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
	return c.err
}

type noopEventEmitter struct{}

func (noopEventEmitter) emitNormal(_ client.Object, _, _ string)                         {}
func (noopEventEmitter) emitWarning(_ client.Object, _, _ string)                        {}
func (noopEventEmitter) emitPoolerReadyTransition(_ client.Object, _ []metav1.Condition) {}
func (noopEventEmitter) emitPoolerCreationTransition(_ client.Object, _ []metav1.Condition) {
}

type captureEventEmitter struct {
	normals  []string
	warnings []string
}

func (c *captureEventEmitter) emitNormal(_ client.Object, reason, message string) {
	c.normals = append(c.normals, reason+":"+message)
}

func (c *captureEventEmitter) emitWarning(_ client.Object, reason, message string) {
	c.warnings = append(c.warnings, reason+":"+message)
}

func (c *captureEventEmitter) emitPoolerReadyTransition(_ client.Object, conditions []metav1.Condition) {
	if !meta.IsStatusConditionTrue(conditions, string(poolerReady)) {
		c.normals = append(c.normals, EventPoolerReady+":Connection poolers are ready")
	}
}

func (c *captureEventEmitter) emitPoolerCreationTransition(_ client.Object, conditions []metav1.Condition) {
	cond := meta.FindStatusCondition(conditions, string(poolerReady))
	if cond != nil && cond.Status == metav1.ConditionFalse && cond.Reason == string(reasonPoolerCreating) {
		return
	}
	c.normals = append(c.normals, EventPoolerCreationStarted+":Connection poolers created, waiting for readiness")
}

func (c configMapNotFoundClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*corev1.ConfigMap); ok {
		return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, key.Name)
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

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
			got := isPoolerReady(tt.pooler)

			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPoolerInstanceCountManual(t *testing.T) {
	tests := []struct {
		name              string
		pooler            *cnpgv1.Pooler
		expectedDesired   int32
		expectedScheduled int32
	}{
		{
			name: "nil instances defaults desired to 1",
			pooler: &cnpgv1.Pooler{
				Status: cnpgv1.PoolerStatus{Instances: 3},
			},
			expectedDesired:   1,
			expectedScheduled: 3,
		},
		{
			name: "explicit instances uses spec value",
			pooler: &cnpgv1.Pooler{
				Spec:   cnpgv1.PoolerSpec{Instances: ptr.To(int32(5))},
				Status: cnpgv1.PoolerStatus{Instances: 2},
			},
			expectedDesired:   5,
			expectedScheduled: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := int32(1)
			if tt.pooler.Spec.Instances != nil {
				desired = *tt.pooler.Spec.Instances
			}
			scheduled := tt.pooler.Status.Instances

			assert.Equal(t, tt.expectedDesired, desired)
			assert.Equal(t, tt.expectedScheduled, scheduled)
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
				ImageName:           "ghcr.io/cloudnative-pg/postgresql:18",
				Instances:           3,
				PrimaryUpdateMethod: "",
				StorageSize:         "10Gi",
			},
		},
		{
			name: "primary update method is included in drift detection",
			spec: cnpgv1.ClusterSpec{
				ImageName:           "img:18",
				Instances:           3,
				PrimaryUpdateMethod: cnpgv1.PrimaryUpdateMethodSwitchover,
			},
			customDefinedParameters: nil,
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           3,
				PrimaryUpdateMethod: string(cnpgv1.PrimaryUpdateMethodSwitchover),
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
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
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
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
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
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				PgHBA:               []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
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
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
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
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				DefaultDatabase:     "mydb",
				Owner:               "admin",
			},
		},
		{
			name: "inherited annotations included when non-empty",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				InheritedMetadata: &cnpgv1.EmbeddedObjectMetadata{
					Annotations: map[string]string{
						prometheusScrapeAnnotation: "true",
						prometheusPortAnnotation:   postgresMetricsPortString,
					},
				},
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
				InheritedAnnotations: map[string]string{
					prometheusScrapeAnnotation: "true",
					prometheusPortAnnotation:   postgresMetricsPortString,
				},
			},
		},
		{
			name: "nil bootstrap leaves database and owner empty",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
			},
			expected: normalizedCNPGClusterSpec{
				ImageName:           "img:18",
				Instances:           1,
				PrimaryUpdateMethod: "",
			},
		},
		{
			name: "certificates ServerAltDNSNames not included in normalization",
			spec: cnpgv1.ClusterSpec{
				ImageName: "img:18",
				Instances: 1,
				Certificates: &cnpgv1.CertificatesConfiguration{
					ServerAltDNSNames: []string{"z.example", "a.example"},
				},
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

func TestClusterModelActuatePatchesPrimaryUpdateMethodDrift(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	instances := int32(3)
	version := "15.13"
	storageSize := resource.MustParse("10Gi")
	restart := "restart"
	switchover := "switchover"

	baseSpec := &enterprisev4.PostgresClusterSpec{
		Instances:        &instances,
		PostgresVersion:  &version,
		Storage:          &storageSize,
		Resources:        &corev1.ResourceRequirements{},
		PostgreSQLConfig: map[string]string{},
		PgHBA:            []string{},
	}
	currentConfig := &MergedConfig{
		Spec: baseSpec.DeepCopy(),
		CNPG: &enterprisev4.CNPGConfig{PrimaryUpdateMethod: &restart},
	}
	desiredConfig := &MergedConfig{
		Spec: baseSpec.DeepCopy(),
		CNPG: &enterprisev4.CNPGConfig{PrimaryUpdateMethod: &switchover},
	}

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}
	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(currentConfig, "pg1-secret", false),
	}
	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingCNPG).Build()

	model := newClusterModel(c, scheme, events, nil, cluster, clusterClass, desiredConfig, "pg1-secret")
	model.Actuate(context.Background())

	require.True(t, model.cnpgPatched)
	assert.False(t, model.cnpgCreated)

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.Equal(t, cnpgv1.PrimaryUpdateMethodSwitchover, updated.Spec.PrimaryUpdateMethod)
	assert.Contains(t, events.normals, EventClusterUpdateStarted+":CNPG cluster spec updated for PostgresCluster pg1, waiting for healthy state")
}

func TestGetMergedConfig(t *testing.T) {
	classInstances := int32(1)
	classVersion := "17"
	classStorage := resource.MustParse("50Gi")
	baseClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "standard"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				Instances:        &classInstances,
				PostgresVersion:  &classVersion,
				Storage:          &classStorage,
				Resources:        &corev1.ResourceRequirements{},
				PostgreSQLConfig: map[string]string{"shared_buffers": "128MB"},
				PgHBA:            []string{"host all all 0.0.0.0/0 md5"},
			},
			CNPG: &enterprisev4.CNPGConfig{PrimaryUpdateMethod: ptr.To("switchover")},
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

		cfg := GetMergedConfig(baseClass, cluster)

		require.Empty(t, ValidateMergedConfig(cfg, baseClass.Name))
		assert.Equal(t, int32(5), *cfg.Spec.Instances)
		assert.Equal(t, "18", *cfg.Spec.PostgresVersion)
		assert.Equal(t, "100Gi", cfg.Spec.Storage.String())
		assert.Equal(t, "200", cfg.Spec.PostgreSQLConfig["max_connections"])
		assert.Equal(t, "hostssl all all 0.0.0.0/0 scram-sha-256", cfg.Spec.PgHBA[0])
	})

	t.Run("class defaults fill in nil cluster fields", func(t *testing.T) {
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg := GetMergedConfig(baseClass, cluster)

		require.Empty(t, ValidateMergedConfig(cfg, baseClass.Name))
		assert.Equal(t, int32(1), *cfg.Spec.Instances)
		assert.Equal(t, "17", *cfg.Spec.PostgresVersion)
		assert.Equal(t, "50Gi", cfg.Spec.Storage.String())
		assert.Equal(t, "128MB", cfg.Spec.PostgreSQLConfig["shared_buffers"])
	})

	t.Run("returns error when required fields missing from both", func(t *testing.T) {
		emptyClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "empty"},
			Spec:       enterprisev4.PostgresClusterClassSpec{},
		}
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg := GetMergedConfig(emptyClass, cluster)

		require.NotEmpty(t, ValidateMergedConfig(cfg, emptyClass.Name))
	})

	t.Run("CNPG config comes from class not cluster", func(t *testing.T) {
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg := GetMergedConfig(baseClass, cluster)

		require.Empty(t, ValidateMergedConfig(cfg, baseClass.Name))
		require.NotNil(t, cfg.CNPG)
		assert.Equal(t, "switchover", *cfg.CNPG.PrimaryUpdateMethod)
	})

	t.Run("rejects postgresqlConfig containing CNPG fixed parameters", func(t *testing.T) {
		badClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "bad"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:        &classInstances,
					PostgresVersion:  &classVersion,
					Storage:          &classStorage,
					Resources:        &corev1.ResourceRequirements{},
					PostgreSQLConfig: map[string]string{"ssl": "on"},
				},
			},
		}
		cluster := &enterprisev4.PostgresCluster{Spec: enterprisev4.PostgresClusterSpec{}}

		cfg := GetMergedConfig(badClass, cluster)
		errs := ValidateMergedConfig(cfg, badClass.Name)
		require.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Error(), "postgresqlConfig must not set CNPG-managed parameters")
		assert.Contains(t, errs[0].Error(), "ssl")

		clusterOverride := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{
				Instances:       &classInstances,
				PostgresVersion: &classVersion,
				Storage:         &classStorage,
				PostgreSQLConfig: map[string]string{
					"shared_buffers": "128MB",
					"ssl_cert_file":  "/tmp/x.crt",
				},
				Resources: &corev1.ResourceRequirements{},
			},
		}
		cfg2 := GetMergedConfig(baseClass, clusterOverride)
		errs2 := ValidateMergedConfig(cfg2, baseClass.Name)
		require.NotEmpty(t, errs2)
		assert.Contains(t, errs2[0].Error(), "ssl_cert_file")
	})

	t.Run("nil maps and slices initialized to safe zero values", func(t *testing.T) {
		classWithNoMaps := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "minimal"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					Instances:       &classInstances,
					PostgresVersion: &classVersion,
					Storage:         &classStorage,
				},
			},
		}
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{},
		}

		cfg := GetMergedConfig(classWithNoMaps, cluster)

		require.Empty(t, ValidateMergedConfig(cfg, classWithNoMaps.Name))
		assert.NotNil(t, cfg.Spec.PostgreSQLConfig)
		assert.NotNil(t, cfg.Spec.PgHBA)
		assert.NotNil(t, cfg.Spec.Resources)
	})

	t.Run("rejects 6-field cron schedule", func(t *testing.T) {
		enabled := true
		sixField := "0 */5 * * * *"
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{
				Backup: &enterprisev4.BackupConfig{
					Enabled:  &enabled,
					Schedule: &sixField,
				},
			},
		}

		cfg := GetMergedConfig(baseClass, cluster)
		errs := ValidateMergedConfig(cfg, baseClass.Name)

		require.NotEmpty(t, errs)
		assert.Contains(t, errs[0].Field, "schedule")
		assert.Contains(t, errs[0].Error(), "5-field")
	})

	t.Run("accepts valid 5-field cron schedule", func(t *testing.T) {
		enabled := true
		fiveField := "*/5 * * * *"
		cluster := &enterprisev4.PostgresCluster{
			Spec: enterprisev4.PostgresClusterSpec{
				Backup: &enterprisev4.BackupConfig{
					Enabled:  &enabled,
					Schedule: &fiveField,
				},
			},
		}

		cfg := GetMergedConfig(baseClass, cluster)
		errs := ValidateMergedConfig(cfg, baseClass.Name)

		require.Empty(t, errs)
	})
}

func TestBuildCNPGClusterSpec(t *testing.T) {
	version := "18"
	instances := int32(3)
	storage := resource.MustParse("50Gi")
	primaryUpdateMethod := "switchover"
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
		CNPG: &enterprisev4.CNPGConfig{
			PrimaryUpdateMethod: &primaryUpdateMethod,
		},
	}

	spec := buildCNPGClusterSpec(cfg, "my-secret", false)

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
	assert.Equal(t, cnpgv1.PrimaryUpdateMethodSwitchover, spec.PrimaryUpdateMethod)
	require.Len(t, spec.PostgresConfiguration.PgHBA, 2)
	assert.Equal(t, "hostssl all all 0.0.0.0/0 scram-sha-256", spec.PostgresConfiguration.PgHBA[0])
	assert.Equal(t, "host replication all 10.0.0.0/8 md5", spec.PostgresConfiguration.PgHBA[1])
	require.NotNil(t, spec.InheritedMetadata)
	assert.Empty(t, spec.InheritedMetadata.Annotations)

	t.Run("adds postgres scrape annotations when enabled", func(t *testing.T) {
		spec := buildCNPGClusterSpec(cfg, "my-secret", true)

		require.NotNil(t, spec.InheritedMetadata)
		assert.Equal(t, "true", spec.InheritedMetadata.Annotations[prometheusScrapeAnnotation])
		assert.Equal(t, metricsPath, spec.InheritedMetadata.Annotations[prometheusPathAnnotation])
		assert.Equal(t, postgresMetricsPortString, spec.InheritedMetadata.Annotations[prometheusPortAnnotation])
	})

}

func TestBuildCNPGPooler(t *testing.T) {
	scheme := runtime.NewScheme()
	enterprisev4.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)

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
		pooler, err := buildCNPGPooler(scheme, postgresCluster, cfg, cnpgCluster, "rw", false)

		require.NoError(t, err)
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
		require.NotNil(t, pooler.Spec.Template)
		assert.Empty(t, pooler.Spec.Template.ObjectMeta.Annotations)
	})

	t.Run("ro pooler", func(t *testing.T) {
		pooler, err := buildCNPGPooler(scheme, postgresCluster, cfg, cnpgCluster, "ro", true)

		require.NoError(t, err)
		assert.Equal(t, "my-cluster-pooler-ro", pooler.Name)
		assert.Equal(t, cnpgv1.PoolerType("ro"), pooler.Spec.Type)
		require.NotNil(t, pooler.Spec.Template)
		assert.Equal(t, "true", pooler.Spec.Template.ObjectMeta.Annotations[prometheusScrapeAnnotation])
		assert.Equal(t, metricsPath, pooler.Spec.Template.ObjectMeta.Annotations[prometheusPathAnnotation])
		assert.Equal(t, poolerMetricsPortString, pooler.Spec.Template.ObjectMeta.Annotations[prometheusPortAnnotation])
		require.Len(t, pooler.Spec.Template.Spec.Containers, 1)
		assert.Equal(t, "pgbouncer", pooler.Spec.Template.Spec.Containers[0].Name)
	})
}

func TestBuildCNPGCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	enterprisev4.AddToScheme(scheme)
	cnpgv1.AddToScheme(scheme)

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
		CNPG: &enterprisev4.CNPGConfig{
			PrimaryUpdateMethod: ptr.To("restart"),
		},
	}

	cluster, err := buildCNPGCluster(scheme, postgresCluster, cfg, "my-secret", true)

	require.NoError(t, err)
	assert.Equal(t, "my-cluster", cluster.Name)
	assert.Equal(t, "db-ns", cluster.Namespace)
	require.Len(t, cluster.OwnerReferences, 1)
	assert.Equal(t, "pg-uid", string(cluster.OwnerReferences[0].UID))
	assert.Equal(t, 3, cluster.Spec.Instances)
	require.NotNil(t, cluster.Spec.InheritedMetadata)
	assert.Equal(t, postgresMetricsPortString, cluster.Spec.InheritedMetadata.Annotations[prometheusPortAnnotation])
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
			got, err := poolerExists(context.Background(), c, cluster, "rw")
			assert.NoError(t, err)
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

		err := deleteConnectionPoolers(context.Background(), c, cluster)

		require.NoError(t, err)
		assert.True(t, apierrors.IsNotFound(c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, &cnpgv1.Pooler{})))
		assert.True(t, apierrors.IsNotFound(c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-ro", Namespace: "default"}, &cnpgv1.Pooler{})))
	})

	t.Run("no-op when no poolers exist", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		err := deleteConnectionPoolers(context.Background(), c, cluster)

		require.NoError(t, err)
	})
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

	err := handleFinalizer(context.Background(), rc, cluster, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), unknownPolicy)
}

func TestEnsureClusterSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	t.Run("creates secret with credentials and owner reference", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-cluster",
				Namespace: "default",
				UID:       "cluster-uid",
			},
		}

		err := ensureClusterSecret(context.Background(), c, scheme, cluster, "my-secret")

		require.NoError(t, err)
		secret := &corev1.Secret{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-secret", Namespace: "default"}, secret))
		assert.Equal(t, "my-secret", secret.Name)
		assert.Equal(t, "default", secret.Namespace)
		assert.Equal(t, corev1.SecretTypeOpaque, secret.Type)
		require.Len(t, secret.OwnerReferences, 1)
		assert.Equal(t, "cluster-uid", string(secret.OwnerReferences[0].UID))
	})

}

func TestArePoolersReady(t *testing.T) {
	makePooler := func(desired, actual int32) *cnpgv1.Pooler {
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
			rw:       makePooler(2, 2),
			ro:       makePooler(2, 2),
			expected: true,
		},
		{
			name:     "returns false when rw pooler not ready",
			rw:       makePooler(2, 0),
			ro:       makePooler(2, 2),
			expected: false,
		},
		{
			name:     "returns false when ro pooler not ready",
			rw:       makePooler(2, 2),
			ro:       makePooler(2, 1),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := arePoolersReady(tt.rw, tt.ro)

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

			err := createConnectionPooler(context.Background(), c, scheme, cluster.DeepCopy(), cfg, cnpg, "rw", false)

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
	cnpgv1.AddToScheme(scheme)
	enterprisev4.AddToScheme(scheme)

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
			UID:       "cluster-uid",
		},
	}
	cnpgCluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "default",
		},
	}

	t.Run("base endpoints without poolers", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(context.Background(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret")

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

	t.Run("includes pooler endpoints when poolers exist", func(t *testing.T) {
		rwPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-pooler-rw", Namespace: "default"},
		}
		roPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-pooler-ro", Namespace: "default"},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build()
		cm, err := generateConfigMap(context.Background(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret")

		require.NoError(t, err)
		assert.Equal(t, "my-cluster-pooler-rw.default", cm.Data["CLUSTER_POOLER_RW_ENDPOINT"])
		assert.Equal(t, "my-cluster-pooler-ro.default", cm.Data["CLUSTER_POOLER_RO_ENDPOINT"])
	})

	t.Run("uses existing configmap name from status", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		pg := cluster.DeepCopy()
		pg.Status.Resources = &enterprisev4.PostgresClusterResources{
			ConfigMapRef: &corev1.LocalObjectReference{Name: "custom-configmap"},
		}

		cm, err := generateConfigMap(context.Background(), c, scheme, pg, cnpgCluster, "my-secret")

		require.NoError(t, err)
		assert.Equal(t, "custom-configmap", cm.Name)
	})

	t.Run("includes CA metadata when available", func(t *testing.T) {
		caSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-server-ca", Namespace: "default"},
			Data:       map[string][]byte{"ca.crt": []byte("-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n")},
		}
		cnpg := cnpgCluster.DeepCopy()
		cnpg.Status.Certificates.ServerCASecret = "my-server-ca"
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(caSecret).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpg, "my-secret")
		require.NoError(t, err)
		expectedCASecretRef := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "my-server-ca"},
			Key:                  defaultServerCACertKey,
		}
		assert.Equal(t, expectedCASecretRef.String(), cm.Data[configKeyServerCASecretRef])
		assert.NotContains(t, cm.Data, "SERVER_CA_CERT_KEY")
	})

	t.Run("omits CA metadata when not available", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret")
		require.NoError(t, err)
		assert.NotContains(t, cm.Data, configKeyServerCASecretRef)
		assert.NotContains(t, cm.Data, "SERVER_CA_CERT_KEY")
	})

	t.Run("omits CA metadata when status is not available", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret")
		require.NoError(t, err)
		assert.NotContains(t, cm.Data, configKeyServerCASecretRef)
		assert.NotContains(t, cm.Data, "SERVER_CA_CERT_KEY")
	})

	t.Run("omits CA metadata when secret is not found", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret")
		require.NoError(t, err)
		assert.NotContains(t, cm.Data, configKeyServerCASecretRef)
		assert.NotContains(t, cm.Data, "SERVER_CA_CERT_KEY")
	})
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

func TestConfigMapConverge_RequeuesWhenCNPGPublishesCASecretButMetadataMissing(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: enterprisev4.PostgresClusterStatus{
			Resources: &enterprisev4.PostgresClusterResources{
				ConfigMapRef: &corev1.LocalObjectReference{Name: "pg1-configmap"},
			},
		},
	}
	existingCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-configmap", Namespace: "default"},
		Data: map[string]string{
			configKeyClusterRWEndpoint:  "pg1-rw.default",
			configKeyClusterROEndpoint:  "pg1-ro.default",
			configKeyClusterREndpoint:   "pg1-r.default",
			configKeyDefaultClusterPort: "5432",
			configKeySuperUserSecretRef: "pg1-secret",
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy,
			Certificates: cnpgv1.CertificatesStatus{
				CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
					ServerCASecret: "pg1-server-ca",
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingCM).Build()
	model := newConfigMapModel(
		c,
		scheme,
		noopEventEmitter{},
		nil,
		clusterRuntimeViewAdapter{model: &clusterModel{cnpgCluster: cnpg}},
		cluster,
		"pg1-secret",
	)

	model.Actuate(t.Context())
	health, err := model.Converge(t.Context())
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Provisioning, health.State)
	assert.Equal(t, reasonConfigMapFailed, health.Reason)
	assert.Equal(t, msgConfigMapCAMetadataPending, health.Message)
	assert.Equal(t, provisioningClusterPhase, health.Phase)
	assert.True(t, health.Result.RequeueAfter > 0)
}

func TestCreateOrUpdateConnectionPoolers(t *testing.T) {
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
	cnpgCluster := &cnpgv1.Cluster{
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

	expectedPoolerSpec := func(poolerType string) cnpgv1.PoolerSpec {
		return cnpgv1.PoolerSpec{
			Cluster:   cnpgv1.LocalObjectReference{Name: "my-cluster"},
			Instances: ptr.To(int32(2)),
			Type:      cnpgv1.PoolerType(poolerType),
			PgBouncer: &cnpgv1.PgBouncerSpec{
				PoolMode:   cnpgv1.PgBouncerPoolMode("transaction"),
				Parameters: map[string]string{"default_pool_size": "25"},
			},
			Template: &cnpgv1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "pgbouncer"}}},
			},
		}
	}

	t.Run("creates both rw and ro poolers", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		err := createOrUpdateConnectionPoolers(context.Background(), c, scheme, cluster.DeepCopy(), cfg, cnpgCluster, false)

		require.NoError(t, err)

		rw := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, rw))
		assert.Equal(t, expectedPoolerSpec("rw"), rw.Spec)
		require.Len(t, rw.OwnerReferences, 1)
		assert.Equal(t, "cluster-uid", string(rw.OwnerReferences[0].UID))

		ro := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-ro", Namespace: "default"}, ro))
		assert.Equal(t, expectedPoolerSpec("ro"), ro.Spec)
		require.Len(t, ro.OwnerReferences, 1)
		assert.Equal(t, "cluster-uid", string(ro.OwnerReferences[0].UID))
	})

	t.Run("no-op when both poolers already exist", func(t *testing.T) {
		existing := []client.Object{
			&cnpgv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-pooler-rw", Namespace: "default"},
				Spec:       cnpgv1.PoolerSpec{Instances: ptr.To(int32(1))},
			},
			&cnpgv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-pooler-ro", Namespace: "default"},
				Spec:       cnpgv1.PoolerSpec{Instances: ptr.To(int32(1))},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing...).Build()

		err := createOrUpdateConnectionPoolers(context.Background(), c, scheme, cluster.DeepCopy(), cfg, cnpgCluster, false)

		require.NoError(t, err)
		rw := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, rw))
		assert.Equal(t, int32(1), *rw.Spec.Instances)
		ro := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-ro", Namespace: "default"}, ro))
		assert.Equal(t, int32(1), *ro.Spec.Instances)
	})

	t.Run("creates both rw and ro poolers with scrape annotations when metrics are enabled", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		err := createOrUpdateConnectionPoolers(context.Background(), c, scheme, cluster.DeepCopy(), cfg, cnpgCluster, true)

		require.NoError(t, err)

		rw := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, rw))
		require.NotNil(t, rw.Spec.Template)
		assert.Equal(t, "true", rw.Spec.Template.ObjectMeta.Annotations[prometheusScrapeAnnotation])
		assert.Equal(t, metricsPath, rw.Spec.Template.ObjectMeta.Annotations[prometheusPathAnnotation])
		assert.Equal(t, poolerMetricsPortString, rw.Spec.Template.ObjectMeta.Annotations[prometheusPortAnnotation])

		ro := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-ro", Namespace: "default"}, ro))
		require.NotNil(t, ro.Spec.Template)
		assert.Equal(t, "true", ro.Spec.Template.ObjectMeta.Annotations[prometheusScrapeAnnotation])
		assert.Equal(t, metricsPath, ro.Spec.Template.ObjectMeta.Annotations[prometheusPathAnnotation])
		assert.Equal(t, poolerMetricsPortString, ro.Spec.Template.ObjectMeta.Annotations[prometheusPortAnnotation])
	})
}

func TestComponentStateTriggerConditions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	exampleClusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1-class",
			Namespace: "default",
		},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPoolerEnabled: ptr.To(true),
			},
		},
		Status: enterprisev4.PostgresClusterClassStatus{
			Phase: ptr.To(string(enterprisev4.PhaseReady)),
		},
	}

	exampleCm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1-config",
			Namespace: "default",
		},
		Data: map[string]string{
			configKeyClusterRWEndpoint:  "pg1-rw.default",
			configKeyClusterROEndpoint:  "pg1-ro.default",
			configKeyClusterREndpoint:   "pg1-r.default",
			configKeyDefaultClusterPort: "5432",
			configKeySuperUserSecretRef: "pg1-secret",
		},
	}
	examplePgCluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "default",
		},
		Status: enterprisev4.PostgresClusterStatus{
			Resources: &enterprisev4.PostgresClusterResources{
				ConfigMapRef: &corev1.LocalObjectReference{Name: "pg1-config"},
				SuperUserSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "pg1-secret"},
					Key:                  "password",
				},
			},
		},
	}
	exampleSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("s3cr3t"),
		},
	}
	exampleCASecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1-server-ca",
			Namespace: "default",
		},
		Data: map[string][]byte{
			defaultServerCACertKey: []byte("-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----\n"),
		},
	}

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
		CNPG: &enterprisev4.CNPGConfig{
			PrimaryUpdateMethod: ptr.To("restart"),
		},
	}

	makeReadyProvisioner := func(cluster *enterprisev4.PostgresCluster) *clusterModel {
		cnpg := &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
			Spec: buildCNPGClusterSpec(mergedConfig, "pg1-secret", false),
			Status: cnpgv1.ClusterStatus{
				Phase: cnpgv1.PhaseHealthy,
			},
		}
		require.NoError(t, ctrl.SetControllerReference(cluster, cnpg, scheme))
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()
		return newClusterModel(c, scheme, noopEventEmitter{}, nil, cluster, exampleClusterClass, mergedConfig, "pg1-secret")
	}

	makeRuntimeView := func(healthy bool) clusterRuntimeView {
		if !healthy {
			return clusterRuntimeViewAdapter{model: &clusterModel{}}
		}
		return clusterRuntimeViewAdapter{model: &clusterModel{
			cnpgCluster: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
			},
		}}
	}

	makeRuntimeViewWithCA := func(healthy bool) clusterRuntimeView {
		if !healthy {
			return clusterRuntimeViewAdapter{model: &clusterModel{}}
		}
		return clusterRuntimeViewAdapter{model: &clusterModel{
			cnpgCluster: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				Status: cnpgv1.ClusterStatus{
					Phase: cnpgv1.PhaseHealthy,
					Certificates: cnpgv1.CertificatesStatus{
						CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
							ServerCASecret: exampleCASecret.Name,
						},
					},
				},
			},
		}}
	}

	// TODO: as soon as coupling is addressed, remove this monster of a test.
	combinations := []struct {
		name       string
		components []component
		conditions []conditionTypes
		requeue    []bool
		expectAll  bool
		message    string
	}{
		{
			name: "Provisioner ready, pooler blocked by prerequisites",
			components: func() []component {
				cluster := examplePgCluster.DeepCopy()
				provisioner := makeReadyProvisioner(cluster)
				pooler := newPoolerModel(
					fake.NewClientBuilder().WithScheme(scheme).Build(),
					scheme,
					noopEventEmitter{},
					nil,
					cluster,
					exampleClusterClass,
					mergedConfig,
					nil,
					true,
					true,
				)
				return []component{provisioner, pooler}
			}(),
			conditions: []conditionTypes{clusterReady, poolerReady},
			requeue:    []bool{false, true},
			expectAll:  false,
			message:    "Provisioner ready but pooler gate is blocked until CNPG is healthy",
		},
		{
			name: "Provisioner ready, pooler ready, configMap pending from NotFound",
			components: func() []component {
				cluster := examplePgCluster.DeepCopy()
				provisioner := makeReadyProvisioner(cluster)
				pooler := newPoolerModel(
					fake.NewClientBuilder().WithScheme(scheme).Build(),
					scheme,
					noopEventEmitter{},
					nil,
					cluster,
					exampleClusterClass,
					mergedConfig,
					nil,
					false,
					false,
				)
				configMap := newConfigMapModel(
					configMapNotFoundClient{
						Client: fake.NewClientBuilder().
							WithScheme(scheme).
							Build(),
					},
					scheme,
					noopEventEmitter{},
					nil,
					makeRuntimeView(true),
					cluster,
					"pg1-secret",
				)
				return []component{provisioner, pooler, configMap}
			}(),
			conditions: []conditionTypes{clusterReady, poolerReady, configMapsReady},
			requeue:    []bool{false, false, true},
			expectAll:  false,
			message:    "Provisioner and pooler ready are not enough when ConfigMap check returns NotFound/pending",
		},
		{
			name: "Flow successful, all components ready",
			components: func() []component {
				cluster := examplePgCluster.DeepCopy()
				provisioner := makeReadyProvisioner(cluster)
				pooler := newPoolerModel(
					fake.NewClientBuilder().WithScheme(scheme).Build(),
					scheme,
					noopEventEmitter{},
					nil,
					cluster,
					exampleClusterClass,
					mergedConfig,
					nil,
					false,
					false,
				)
				configMap := newConfigMapModel(
					fake.NewClientBuilder().
						WithScheme(scheme).
						WithObjects(exampleCm, exampleCASecret).
						Build(),
					scheme,
					noopEventEmitter{},
					nil,
					makeRuntimeViewWithCA(true),
					cluster,
					"pg1-secret",
				)
				secret := newSecretModel(
					fake.NewClientBuilder().
						WithScheme(scheme).
						WithObjects(exampleSecret).
						Build(),
					scheme,
					noopEventEmitter{},
					nil,
					cluster,
					"pg1-secret",
				)
				return []component{provisioner, pooler, configMap, secret}
			}(),
			conditions: []conditionTypes{clusterReady, poolerReady, configMapsReady, secretsReady},
			requeue:    []bool{false, false, false, false},
			expectAll:  true,
			message:    "",
		},
	}

	for _, tt := range combinations {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := pgcConstants.Empty
			for i, check := range tt.components {
				gate := check.EvaluatePrerequisites(ctx)
				if !gate.Allowed {
					info := gate.Health
					state = info.State
					assert.Equal(t, tt.conditions[i], info.Condition)
					assert.Equal(t, tt.requeue[i], info.Result.RequeueAfter > 0)
					continue
				}

				check.Actuate(ctx)
				info, err := check.Converge(ctx)
				require.NoError(t, err)
				state = info.State
				assert.Equal(t, tt.conditions[i], info.Condition)
				assert.Equal(t, tt.requeue[i], info.Result.RequeueAfter > 0)
			}
			assert.Equal(t, tt.expectAll, state&pgcConstants.Ready == pgcConstants.Ready,
				tt.message)
		})
	}
}

func TestSyncManagedRolesStatusFromCNPG(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		specRoles  []enterprisev4.ManagedRole
		cnpgStatus cnpgv1.ManagedRoles
		reconciled []string
		pending    []string
		failed     map[string]string
	}{
		{
			name: "marks unreconciled desired role as pending",
			specRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
			cnpgStatus: cnpgv1.ManagedRoles{},
			reconciled: nil,
			pending:    []string{"app_user"},
			failed:     nil,
		},
		{
			name: "maps reconciled and pending roles from CNPG status",
			specRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
				{Name: "app_rw", Exists: true},
			},
			cnpgStatus: cnpgv1.ManagedRoles{
				ByStatus: map[cnpgv1.RoleStatus][]string{
					cnpgv1.RoleStatusReconciled:            {"app_user"},
					cnpgv1.RoleStatusPendingReconciliation: {"app_rw"},
				},
			},
			reconciled: []string{"app_user"},
			pending:    []string{"app_rw"},
			failed:     nil,
		},
		{
			name: "maps cannot reconcile errors as failed",
			specRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
			cnpgStatus: cnpgv1.ManagedRoles{
				CannotReconcile: map[string][]string{
					"app_user": {"reserved role"},
				},
			},
			reconciled: nil,
			pending:    nil,
			failed: map[string]string{
				"app_user": "reserved role",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cluster := &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					ManagedRoles: tt.specRoles,
				},
			}
			cnpgCluster := &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					ManagedRolesStatus: tt.cnpgStatus,
				},
			}

			syncManagedRolesStatusFromCNPG(cluster, cnpgCluster)

			require.NotNil(t, cluster.Status.ManagedRolesStatus)
			assert.Equal(t, tt.reconciled, cluster.Status.ManagedRolesStatus.Reconciled)
			assert.Equal(t, tt.pending, cluster.Status.ManagedRolesStatus.Pending)
			assert.Equal(t, tt.failed, cluster.Status.ManagedRolesStatus.Failed)
		})
	}
}

func TestManagedRolesModelConverge(t *testing.T) {
	t.Parallel()

	makeRuntimeView := func(phase string, managedRoles cnpgv1.ManagedRoles) clusterRuntimeView {
		return clusterRuntimeViewAdapter{model: &clusterModel{
			cnpgCluster: &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					Phase:              phase,
					ManagedRolesStatus: managedRoles,
				},
			},
		}}
	}

	tests := []struct {
		name                  string
		runtimeView           clusterRuntimeView
		specRoles             []enterprisev4.ManagedRole
		expectedState         pgcConstants.State
		expectedReason        conditionReasons
		expectErr             bool
		expectStatusPublished bool
		expectPending         []string
		expectFailed          map[string]string
	}{
		{
			name:        "returns pending when runtime is not healthy",
			runtimeView: makeRuntimeView(cnpgv1.PhaseFirstPrimary, cnpgv1.ManagedRoles{}),
			specRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
			expectedState:         pgcConstants.Pending,
			expectedReason:        reasonManagedRolesPending,
			expectErr:             false,
			expectStatusPublished: false,
		},
		{
			name: "returns pending when role is still pending reconciliation",
			runtimeView: makeRuntimeView(cnpgv1.PhaseHealthy, cnpgv1.ManagedRoles{
				ByStatus: map[cnpgv1.RoleStatus][]string{
					cnpgv1.RoleStatusPendingReconciliation: {"app_user"},
				},
			}),
			specRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
			expectedState:         pgcConstants.Pending,
			expectedReason:        reasonManagedRolesPending,
			expectErr:             false,
			expectStatusPublished: true,
			expectPending:         []string{"app_user"},
		},
		{
			name: "returns failed when role cannot reconcile",
			runtimeView: makeRuntimeView(cnpgv1.PhaseHealthy, cnpgv1.ManagedRoles{
				CannotReconcile: map[string][]string{
					"app_user": {"reserved role"},
				},
			}),
			specRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
			expectedState:         pgcConstants.Failed,
			expectedReason:        reasonManagedRolesFailed,
			expectErr:             true,
			expectStatusPublished: true,
			expectFailed: map[string]string{
				"app_user": "reserved role",
			},
		},
		{
			name: "returns ready when all desired roles are reconciled",
			runtimeView: makeRuntimeView(cnpgv1.PhaseHealthy, cnpgv1.ManagedRoles{
				ByStatus: map[cnpgv1.RoleStatus][]string{
					cnpgv1.RoleStatusReconciled: {"app_user", "app_user_rw"},
				},
			}),
			specRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
				{Name: "app_user_rw", Exists: true},
			},
			expectedState:         pgcConstants.Ready,
			expectedReason:        reasonManagedRolesReady,
			expectErr:             false,
			expectStatusPublished: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cluster := &enterprisev4.PostgresCluster{
				Spec: enterprisev4.PostgresClusterSpec{
					ManagedRoles: tt.specRoles,
				},
			}
			model := newManagedRolesModel(
				fake.NewClientBuilder().Build(),
				nil,
				noopEventEmitter{},
				nil,
				tt.runtimeView,
				cluster,
				"pg1-secret",
			)

			health, err := model.Converge(context.Background())
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, managedRolesReady, health.Condition)
			assert.Equal(t, tt.expectedState, health.State)
			assert.Equal(t, tt.expectedReason, health.Reason)
			if tt.expectStatusPublished {
				require.NotNil(t, cluster.Status.ManagedRolesStatus)
				assert.Equal(t, tt.expectPending, cluster.Status.ManagedRolesStatus.Pending)
				assert.Equal(t, tt.expectFailed, cluster.Status.ManagedRolesStatus.Failed)
			} else {
				assert.Nil(t, cluster.Status.ManagedRolesStatus)
			}
		})
	}
}

func TestManagedRolesRuntimeGateHealthMatchesConverge(t *testing.T) {
	t.Parallel()

	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			ManagedRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
		},
	}
	model := newManagedRolesModel(
		fake.NewClientBuilder().Build(),
		nil,
		noopEventEmitter{},
		nil,
		clusterRuntimeViewAdapter{model: &clusterModel{
			cnpgCluster: &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseFirstPrimary}},
		}},
		cluster,
		"pg1-secret",
	)

	gate := model.EvaluatePrerequisites(context.Background())
	require.False(t, gate.Allowed)

	health, err := model.Converge(context.Background())
	require.NoError(t, err)
	assert.Equal(t, gate.Health, health)
}

func TestActuateErrorPassdownConvergeHandling(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

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
		CNPG: &enterprisev4.CNPGConfig{
			PrimaryUpdateMethod: ptr.To("restart"),
		},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPoolerEnabled: ptr.To(true),
			},
		},
	}

	type convergeComponent interface {
		Actuate(ctx context.Context)
		Converge(ctx context.Context) (componentHealth, error)
	}
	type testCase struct {
		name              string
		expectedCondition conditionTypes
		expectedReason    conditionReasons
		build             func(updateStatus healthStatusUpdater) convergeComponent
	}

	tests := []testCase{
		{
			name:              "cluster component passes actuate get error through converge",
			expectedCondition: clusterReady,
			expectedReason:    reasonClusterGetFailed,
			build: func(updateStatus healthStatusUpdater) convergeComponent {
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
				base := fake.NewClientBuilder().WithScheme(scheme).Build()
				errClient := getErrorClient{
					Client: base,
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*cnpgv1.Cluster)
						return ok
					},
				}
				return newClusterModel(errClient, scheme, noopEventEmitter{}, updateStatus, cluster, clusterClass, mergedConfig, "pg1-secret")
			},
		},
		{
			name:              "managed roles component passes actuate patch error through converge",
			expectedCondition: managedRolesReady,
			expectedReason:    reasonManagedRolesFailed,
			build: func(updateStatus healthStatusUpdater) convergeComponent {
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Spec: enterprisev4.PostgresClusterSpec{
						ManagedRoles: []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
					},
				}
				cnpg := &cnpgv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
				}
				base := fake.NewClientBuilder().WithScheme(scheme).Build()
				errClient := patchErrorClient{Client: base, err: assert.AnError}
				return newManagedRolesModel(
					errClient,
					scheme,
					noopEventEmitter{},
					updateStatus,
					clusterRuntimeViewAdapter{model: &clusterModel{cnpgCluster: cnpg}},
					cluster,
					"pg1-secret",
				)
			},
		},
		{
			name:              "pooler component passes actuate create error through converge",
			expectedCondition: poolerReady,
			expectedReason:    reasonPoolerReconciliationFailed,
			build: func(updateStatus healthStatusUpdater) convergeComponent {
				poolerInstances := int32(2)
				poolerMode := enterprisev4.ConnectionPoolerModeTransaction
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				}
				cnpg := &cnpgv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
				}
				base := fake.NewClientBuilder().WithScheme(scheme).Build()
				errClient := createErrorClient{
					Client: base,
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*cnpgv1.Pooler)
						return ok
					},
				}
				poolerCfg := &MergedConfig{
					Spec: mergedConfig.Spec,
					CNPG: &enterprisev4.CNPGConfig{
						ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{
							Instances: &poolerInstances,
							Mode:      &poolerMode,
							Config:    map[string]string{},
						},
					},
				}
				return newPoolerModel(errClient, scheme, noopEventEmitter{}, updateStatus, cluster, clusterClass, poolerCfg, cnpg, true, true)
			},
		},
		{
			name:              "configmap component passes actuate pooler lookup error through converge",
			expectedCondition: configMapsReady,
			expectedReason:    reasonConfigMapFailed,
			build: func(updateStatus healthStatusUpdater) convergeComponent {
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     enterprisev4.PostgresClusterStatus{Resources: &enterprisev4.PostgresClusterResources{}},
				}
				cnpg := &cnpgv1.Cluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
				}
				base := fake.NewClientBuilder().WithScheme(scheme).Build()
				errClient := getErrorClient{
					Client: base,
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*cnpgv1.Pooler)
						return ok
					},
				}
				return newConfigMapModel(
					errClient,
					scheme,
					noopEventEmitter{},
					updateStatus,
					clusterRuntimeViewAdapter{model: &clusterModel{cnpgCluster: cnpg}},
					cluster,
					"pg1-secret",
				)
			},
		},
		{
			name:              "secret component passes actuate existence-check error through converge",
			expectedCondition: secretsReady,
			expectedReason:    reasonSuperUserSecretFailed,
			build: func(updateStatus healthStatusUpdater) convergeComponent {
				cluster := &enterprisev4.PostgresCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
					Status: enterprisev4.PostgresClusterStatus{
						Resources: &enterprisev4.PostgresClusterResources{},
					},
				}
				base := fake.NewClientBuilder().WithScheme(scheme).Build()
				errClient := getErrorClient{
					Client: base,
					err:    assert.AnError,
					matcher: func(obj client.Object) bool {
						_, ok := obj.(*corev1.Secret)
						return ok
					},
				}
				return newSecretModel(errClient, scheme, noopEventEmitter{}, updateStatus, cluster, "pg1-secret")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var (
				written componentHealth
				writes  int
			)
			updateStatus := func(health componentHealth) error {
				written = health
				writes++
				return nil
			}
			model := tt.build(updateStatus)

			model.Actuate(context.Background())
			health, err := model.Converge(context.Background())

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

func TestPoolerModelConvergeSetsConnectionPoolerStatus(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	t.Run("does not set enabled true while pooler is pending", func(t *testing.T) {
		t.Parallel()

		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		}
		clusterClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg1-class",
				Namespace: "default",
			},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					ConnectionPoolerEnabled: ptr.To(true),
				},
			},
		}
		model := newPoolerModel(
			fake.NewClientBuilder().WithScheme(scheme).Build(),
			scheme,
			noopEventEmitter{},
			nil,
			cluster,
			clusterClass,
			&MergedConfig{},
			nil,
			true,
			true,
		)

		health, err := model.Converge(context.Background())
		require.NoError(t, err)
		assert.Nil(t, cluster.Status.ConnectionPoolerStatus)
		assert.Equal(t, pgcConstants.Pending, health.State)
	})

	t.Run("sets enabled true when pooler converges ready", func(t *testing.T) {
		t.Parallel()

		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		}
		clusterClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg1-class",
				Namespace: "default",
			},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					ConnectionPoolerEnabled: ptr.To(true),
				},
			},
		}
		rwPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolerResourceName(cluster.Name, readWriteEndpoint),
				Namespace: cluster.Namespace,
			},
			Status: cnpgv1.PoolerStatus{Instances: 1},
		}
		roPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolerResourceName(cluster.Name, readOnlyEndpoint),
				Namespace: cluster.Namespace,
			},
			Status: cnpgv1.PoolerStatus{Instances: 1},
		}
		model := newPoolerModel(
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build(),
			scheme,
			noopEventEmitter{},
			nil,
			cluster,
			clusterClass,
			&MergedConfig{},
			&cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}},
			true,
			true,
		)

		health, err := model.Converge(context.Background())
		require.NoError(t, err)
		assert.Equal(t, &enterprisev4.ConnectionPoolerStatus{Enabled: true}, cluster.Status.ConnectionPoolerStatus)
		assert.Equal(t, pgcConstants.Ready, health.State)
	})

	t.Run("returns Failed when RW pooler Get returns non-NotFound error", func(t *testing.T) {
		t.Parallel()

		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		}
		clusterClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					ConnectionPoolerEnabled: ptr.To(true),
				},
			},
		}
		rwPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 1},
		}
		roPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 1},
		}
		rwName := poolerResourceName(cluster.Name, readWriteEndpoint)
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build()
		c := getErrorClient{
			Client: base,
			err:    apierrors.NewInternalError(fmt.Errorf("api unavailable")),
			keyMatcher: func(key client.ObjectKey) bool {
				return key.Name == rwName
			},
		}
		model := newPoolerModel(c, scheme, noopEventEmitter{}, nil, cluster, clusterClass, &MergedConfig{}, &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}}, true, true)

		health, err := model.Converge(context.Background())
		require.Error(t, err)
		assert.Equal(t, pgcConstants.Failed, health.State)
	})

	t.Run("returns Failed when RO pooler Get returns non-NotFound error", func(t *testing.T) {
		t.Parallel()

		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		}
		clusterClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					ConnectionPoolerEnabled: ptr.To(true),
				},
			},
		}
		rwPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 1},
		}
		roPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 1},
		}
		roName := poolerResourceName(cluster.Name, readOnlyEndpoint)
		base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build()
		c := getErrorClient{
			Client: base,
			err:    apierrors.NewInternalError(fmt.Errorf("api unavailable")),
			keyMatcher: func(key client.ObjectKey) bool {
				return key.Name == roName
			},
		}
		model := newPoolerModel(c, scheme, noopEventEmitter{}, nil, cluster, clusterClass, &MergedConfig{}, &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}}, true, true)

		health, err := model.Converge(context.Background())
		require.Error(t, err)
		assert.Equal(t, pgcConstants.Failed, health.State)
	})

	t.Run("sets status nil when pooler disabled", func(t *testing.T) {
		t.Parallel()

		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
			Status: enterprisev4.PostgresClusterStatus{
				ConnectionPoolerStatus: &enterprisev4.ConnectionPoolerStatus{Enabled: true},
			},
		}
		clusterClass := &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pg1-class",
				Namespace: "default",
			},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Config: &enterprisev4.PostgresClusterClassConfig{
					ConnectionPoolerEnabled: ptr.To(true),
				},
			},
		}
		model := newPoolerModel(
			fake.NewClientBuilder().WithScheme(scheme).Build(),
			scheme,
			noopEventEmitter{},
			nil,
			cluster,
			clusterClass,
			&MergedConfig{},
			nil,
			false,
			false,
		)

		model.Actuate(context.Background())
		health, err := model.Converge(context.Background())
		require.NoError(t, err)
		assert.Nil(t, cluster.Status.ConnectionPoolerStatus)
		assert.Equal(t, pgcConstants.Ready, health.State)
	})
}

func TestPoolerConvergeEmitsReadyEventOnTransition(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1-class",
			Namespace: "default",
		},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPoolerEnabled: ptr.To(true),
			},
		},
	}
	events := &captureEventEmitter{}
	rwPooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolerResourceName(cluster.Name, readWriteEndpoint),
			Namespace: cluster.Namespace,
		},
		Status: cnpgv1.PoolerStatus{Instances: 1},
	}
	roPooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolerResourceName(cluster.Name, readOnlyEndpoint),
			Namespace: cluster.Namespace,
		},
		Status: cnpgv1.PoolerStatus{Instances: 1},
	}
	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build(),
		scheme,
		events,
		nil,
		cluster,
		clusterClass,
		&MergedConfig{},
		&cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}},
		true,
		true,
	)

	_, err := model.Converge(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, events.normals)
	assert.Contains(t, events.normals[0], EventPoolerReady)

	// No re-emission when condition already True.
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   string(poolerReady),
		Status: metav1.ConditionTrue,
	}}
	events.normals = nil
	_, err = model.Converge(context.Background())
	require.NoError(t, err)
	assert.Empty(t, events.normals)
}

func TestManagedRolesConvergeDoesNotEmitFailureForPending(t *testing.T) {
	t.Parallel()

	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			ManagedRoles: []enterprisev4.ManagedRole{{Name: "app_user", Exists: true}},
		},
	}
	events := &captureEventEmitter{}
	model := newManagedRolesModel(
		fake.NewClientBuilder().Build(),
		nil,
		events,
		nil,
		clusterRuntimeViewAdapter{model: &clusterModel{
			cnpgCluster: &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					Phase:              cnpgv1.PhaseHealthy,
					ManagedRolesStatus: cnpgv1.ManagedRoles{},
				},
			},
		}},
		cluster,
		"pg1-secret",
	)

	_, err := model.Converge(context.Background())
	require.NoError(t, err)
	assert.Empty(t, events.warnings)
}

func TestManagedRolesConvergeEmitsReadyEventOnTransition(t *testing.T) {
	t.Parallel()

	cluster := &enterprisev4.PostgresCluster{
		Spec: enterprisev4.PostgresClusterSpec{
			ManagedRoles: []enterprisev4.ManagedRole{
				{Name: "app_user", Exists: true},
			},
		},
	}
	events := &captureEventEmitter{}
	model := newManagedRolesModel(
		fake.NewClientBuilder().Build(),
		nil,
		events,
		nil,
		clusterRuntimeViewAdapter{model: &clusterModel{
			cnpgCluster: &cnpgv1.Cluster{
				Status: cnpgv1.ClusterStatus{
					Phase: cnpgv1.PhaseHealthy,
					ManagedRolesStatus: cnpgv1.ManagedRoles{
						ByStatus: map[cnpgv1.RoleStatus][]string{
							cnpgv1.RoleStatusReconciled: {"app_user"},
						},
					},
				},
			},
		}},
		cluster,
		"pg1-secret",
	)

	_, err := model.Converge(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, events.normals)
	assert.Contains(t, events.normals[0], EventManagedRolesReady)

	// No re-emission when condition already True.
	cluster.Status.Conditions = []metav1.Condition{{
		Type:   string(managedRolesReady),
		Status: metav1.ConditionTrue,
	}}
	events.normals = nil
	_, err = model.Converge(context.Background())
	require.NoError(t, err)
	assert.Empty(t, events.normals)
}

func TestClusterModelActuateAdoptsOrphanedCNPGCluster(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	instances := int32(1)
	version := "16"
	storageSize := resource.MustParse("10Gi")
	cfg := &MergedConfig{
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

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class"},
	}

	// Orphaned CNPG cluster: same name/namespace but no owner reference.
	orphanedCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cfg, "pg1-secret", false),
	}
	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(orphanedCNPG).Build()

	model := newClusterModel(c, scheme, events, nil, cluster, clusterClass, cfg, "pg1-secret")
	model.Actuate(context.Background())

	require.True(t, model.cnpgPatched, "adoption must set cnpgPatched to requeue")
	assert.False(t, model.cnpgCreated)
	assert.Nil(t, model.actuateErr)

	adopted := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(orphanedCNPG), adopted))
	require.Len(t, adopted.OwnerReferences, 1, "owner reference must be set after adoption")
	assert.Equal(t, cluster.Name, adopted.OwnerReferences[0].Name)

	assert.Contains(t, events.normals, EventClusterAdopted+":Adopted existing CNPG cluster for PostgresCluster pg1")
}

func TestValidateCrossResource_VersionFloor(t *testing.T) {
	makeClass := func(floor string) *enterprisev4.PostgresClusterClass {
		return &enterprisev4.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "cls"},
			Spec: enterprisev4.PostgresClusterClassSpec{
				Provisioner: "postgresql.cnpg.io",
				Config:      &enterprisev4.PostgresClusterClassConfig{PostgresVersion: ptr.To(floor)},
			},
		}
	}
	makeCluster := func(version string) *enterprisev4.PostgresCluster {
		return &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg"},
			Spec:       enterprisev4.PostgresClusterSpec{PostgresVersion: ptr.To(version)},
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
