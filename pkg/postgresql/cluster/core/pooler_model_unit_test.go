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
	"fmt"
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// poolerEnabledConfig returns a MergedConfig with pooler enabled and config present.
func poolerEnabledConfig() *MergedConfig {
	instances := int32(3)
	mode := enterprisev4.ConnectionPoolerModeTransaction
	return &MergedConfig{
		Spec: &enterprisev4.PostgresClusterSpec{
			ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)},
		},
		CNPG: &enterprisev4.CNPGConfig{
			ConnectionPooler: &enterprisev4.ConnectionPoolerConfig{
				Instances: &instances,
				Mode:      &mode,
			},
		},
	}
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

func TestCreateConnectionPooler(t *testing.T) {
	scheme := newTestScheme()

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
			name: "updates pooler when it already exists",
			objects: []client.Object{
				&cnpgv1.Pooler{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-cluster-pooler-rw",
						Namespace: "default",
					},
					Spec: cnpgv1.PoolerSpec{Instances: ptr.To(int32(1))},
				},
			},
			expectInstances: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.objects...).Build()

			err := createAndUpdateConnectionPooler(context.Background(), c, scheme, cluster.DeepCopy(), cfg, cnpg, "rw", false)

			require.NoError(t, err)
			fetched := &cnpgv1.Pooler{}
			require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, fetched))
			require.NotNil(t, fetched.Spec.Instances)
			assert.Equal(t, tt.expectInstances, *fetched.Spec.Instances)
		})
	}
}

func TestCreateOrUpdateConnectionPoolers(t *testing.T) {
	scheme := newTestScheme()

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

	t.Run("updates both poolers when they already exist", func(t *testing.T) {
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
		assert.Equal(t, expectedPoolerSpec("rw"), rw.Spec)
		ro := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-ro", Namespace: "default"}, ro))
		assert.Equal(t, expectedPoolerSpec("ro"), ro.Spec)
	})

	t.Run("removes scrape annotations from existing poolers when metrics are disabled", func(t *testing.T) {
		existingPoolerSpec := func(poolerType string) cnpgv1.PoolerSpec {
			spec := expectedPoolerSpec(poolerType)
			spec.Template.ObjectMeta.Annotations = buildPoolerScrapeAnnotations()
			return spec
		}
		existing := []client.Object{
			&cnpgv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-pooler-rw", Namespace: "default"},
				Spec:       existingPoolerSpec("rw"),
			},
			&cnpgv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster-pooler-ro", Namespace: "default"},
				Spec:       existingPoolerSpec("ro"),
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing...).Build()

		err := createOrUpdateConnectionPoolers(context.Background(), c, scheme, cluster.DeepCopy(), cfg, cnpgCluster, false)

		require.NoError(t, err)
		rw := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-rw", Namespace: "default"}, rw))
		require.NotNil(t, rw.Spec.Template)
		assert.Empty(t, rw.Spec.Template.ObjectMeta.Annotations)

		ro := &cnpgv1.Pooler{}
		require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "my-cluster-pooler-ro", Namespace: "default"}, ro))
		require.NotNil(t, ro.Spec.Template)
		assert.Empty(t, ro.Spec.Template.ObjectMeta.Annotations)
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

func TestNormalizeCNPGPoolerSpec(t *testing.T) {
	t.Run("treats CRD and pod template defaults as equivalent", func(t *testing.T) {
		enableServiceLinks := true
		desired := cnpgv1.PoolerSpec{
			Cluster: cnpgv1.LocalObjectReference{Name: "pg1"},
			PgBouncer: &cnpgv1.PgBouncerSpec{
				Parameters: map[string]string{"default_pool_size": "25"},
			},
			Template: &cnpgv1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "pgbouncer"}},
				},
			},
		}
		defaulted := desired.DeepCopy()
		defaulted.Type = cnpgv1.PoolerTypeRW
		defaulted.Instances = ptr.To(int32(1))
		defaulted.PgBouncer.PoolMode = cnpgv1.PgBouncerPoolModeSession
		defaulted.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways
		defaulted.Template.Spec.DNSPolicy = corev1.DNSClusterFirst
		defaulted.Template.Spec.EnableServiceLinks = &enableServiceLinks
		defaulted.Template.Spec.Containers[0].TerminationMessagePath = corev1.TerminationMessagePathDefault
		defaulted.Template.Spec.Containers[0].TerminationMessagePolicy = corev1.TerminationMessageReadFile
		defaulted.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{
			Name:     "postgresql",
			Protocol: corev1.ProtocolTCP,
		}}

		assert.Equal(t, normalizeCNPGPoolerSpec(desired), normalizeCNPGPoolerSpec(*defaulted))
	})

	t.Run("detects drift in managed fields", func(t *testing.T) {
		instances := int32(2)
		base := cnpgv1.PoolerSpec{
			Cluster:   cnpgv1.LocalObjectReference{Name: "pg1"},
			Type:      cnpgv1.PoolerTypeRW,
			Instances: &instances,
			PgBouncer: &cnpgv1.PgBouncerSpec{
				PoolMode:   cnpgv1.PgBouncerPoolModeTransaction,
				Parameters: map[string]string{"default_pool_size": "25"},
			},
			Template: &cnpgv1.PodTemplateSpec{
				ObjectMeta: cnpgv1.Metadata{Annotations: map[string]string{prometheusScrapeAnnotation: "true"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "pgbouncer"}}},
			},
		}

		tests := []struct {
			name   string
			mutate func(*cnpgv1.PoolerSpec)
		}{
			{
				name: "cluster reference",
				mutate: func(spec *cnpgv1.PoolerSpec) {
					spec.Cluster.Name = "pg2"
				},
			},
			{
				name: "type",
				mutate: func(spec *cnpgv1.PoolerSpec) {
					spec.Type = cnpgv1.PoolerTypeRO
				},
			},
			{
				name: "instances",
				mutate: func(spec *cnpgv1.PoolerSpec) {
					spec.Instances = ptr.To(int32(3))
				},
			},
			{
				name: "pool mode",
				mutate: func(spec *cnpgv1.PoolerSpec) {
					spec.PgBouncer.PoolMode = cnpgv1.PgBouncerPoolModeSession
				},
			},
			{
				name: "parameters",
				mutate: func(spec *cnpgv1.PoolerSpec) {
					spec.PgBouncer.Parameters["default_pool_size"] = "50"
				},
			},
			{
				name: "template annotations",
				mutate: func(spec *cnpgv1.PoolerSpec) {
					spec.Template.ObjectMeta.Annotations[prometheusScrapeAnnotation] = "false"
				},
			},
			{
				name: "template containers",
				mutate: func(spec *cnpgv1.PoolerSpec) {
					spec.Template.Spec.Containers[0].Name = "sidecar"
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				changed := base.DeepCopy()
				tt.mutate(changed)

				assert.NotEqual(t, normalizeCNPGPoolerSpec(base), normalizeCNPGPoolerSpec(*changed))
			})
		}
	})
}

func TestPoolerModelConvergeSetsConnectionPoolerStatus(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}},
		},
	}
	// healthyCNPG has no SANs in spec so both isSANPolicyConverged (poolerEnabled=true → needs
	// SANs added → not converged) and isServerTLSLeafAlignedWithSpec (no spec SANs → true) are
	// bypassed. Tests that require the pooler to reach Ready must seed SANs + a valid TLS cert.
	// Tests that don't care about SAN/TLS gates and use poolerEnabled=false work correctly.
	healthyCNPG := &cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}}

	t.Run("does not set enabled true while pooler is pending (no CNPG contract)", func(t *testing.T) {
		t.Parallel()

		// Arrange
		cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
		contracts := &reconcileContracts{} // no CNPGCluster → contracts not ready
		model := newPoolerModel(
			fake.NewClientBuilder().WithScheme(scheme).Build(),
			scheme, noopEventEmitter{}, nil, cluster, clusterClass, poolerEnabledConfig(), contracts,
		)

		// Act
		reconcileErr := model.CheckContracts()
		health, err := model.Observe(context.Background(), reconcileErr)

		// Assert
		require.NoError(t, err)
		assert.Nil(t, cluster.Status.ConnectionPoolerStatus)
		assert.Equal(t, pgcConstants.Pending, health.State)
	})

	t.Run("sets enabled true when pooler converges ready", func(t *testing.T) {
		t.Parallel()

		// Arrange: CNPG with converged SANs + valid TLS cert so all gates pass
		cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
		cnpgReady, tlsSecret := makePoolerReadyCNPG(t, "pg1", "default")
		rwPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 3},
		}
		roPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 3},
		}
		contracts := &reconcileContracts{CNPGCluster: cnpgReady}
		model := newPoolerModel(
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler, tlsSecret).Build(),
			scheme, noopEventEmitter{}, nil, cluster, clusterClass, poolerEnabledConfig(), contracts,
		)

		// Act
		reconcileErr := model.Reconcile(context.Background())
		health, err := model.Observe(context.Background(), reconcileErr)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, &enterprisev4.ConnectionPoolerStatus{Enabled: true, ReadWriteEnabled: true}, cluster.Status.ConnectionPoolerStatus)
		assert.Equal(t, pgcConstants.Ready, health.State)
	})

	t.Run("returns Failed when RW pooler Get returns non-NotFound error", func(t *testing.T) {
		t.Parallel()

		// Arrange
		cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
		rwPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 1},
		}
		roPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 1},
		}
		rwName := poolerResourceName(cluster.Name, readWriteEndpoint)
		c := getErrorClient{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build(),
			err:    apierrors.NewInternalError(fmt.Errorf("api unavailable")),
			keyMatcher: func(key client.ObjectKey) bool {
				return key.Name == rwName
			},
		}
		contracts := &reconcileContracts{CNPGCluster: healthyCNPG}
		model := newPoolerModel(c, scheme, noopEventEmitter{}, nil, cluster, clusterClass, poolerEnabledConfig(), contracts)

		// Act
		reconcileErr := model.Reconcile(context.Background())
		health, err := model.Observe(context.Background(), reconcileErr)

		// Assert
		require.Error(t, err)
		assert.Equal(t, pgcConstants.Failed, health.State)
	})

	t.Run("returns Failed when RO pooler Get returns non-NotFound error", func(t *testing.T) {
		t.Parallel()

		// Arrange
		cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
		rwPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 1},
		}
		roPooler := &cnpgv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace},
			Status:     cnpgv1.PoolerStatus{Instances: 1},
		}
		roName := poolerResourceName(cluster.Name, readOnlyEndpoint)
		c := getErrorClient{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build(),
			err:    apierrors.NewInternalError(fmt.Errorf("api unavailable")),
			keyMatcher: func(key client.ObjectKey) bool {
				return key.Name == roName
			},
		}
		contracts := &reconcileContracts{CNPGCluster: healthyCNPG}
		model := newPoolerModel(c, scheme, noopEventEmitter{}, nil, cluster, clusterClass, poolerEnabledConfig(), contracts)

		// Act
		reconcileErr := model.Reconcile(context.Background())
		health, err := model.Observe(context.Background(), reconcileErr)

		// Assert
		require.Error(t, err)
		assert.Equal(t, pgcConstants.Failed, health.State)
	})

	t.Run("sets status nil when pooler disabled", func(t *testing.T) {
		t.Parallel()

		// Arrange
		cluster := &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
			Status:     enterprisev4.PostgresClusterStatus{ConnectionPoolerStatus: &enterprisev4.ConnectionPoolerStatus{Enabled: true}},
		}
		contracts := &reconcileContracts{CNPGCluster: healthyCNPG}
		model := newPoolerModel(
			fake.NewClientBuilder().WithScheme(scheme).Build(),
			scheme, noopEventEmitter{}, nil, cluster, clusterClass, &MergedConfig{Spec: &enterprisev4.PostgresClusterSpec{}}, contracts,
		)

		// Act
		reconcileErr := model.Reconcile(context.Background())
		health, err := model.Observe(context.Background(), reconcileErr)

		// Assert
		require.NoError(t, err)
		assert.Nil(t, cluster.Status.ConnectionPoolerStatus)
		assert.Equal(t, pgcConstants.Ready, health.State)
	})
}

func TestPoolerConvergeEmitsReadyEventOnTransition(t *testing.T) {
	t.Parallel()

	// Arrange
	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"}}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}},
		},
	}
	events := &captureEventEmitter{}
	rwPooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace},
		Status:     cnpgv1.PoolerStatus{Instances: 3},
	}
	roPooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace},
		Status:     cnpgv1.PoolerStatus{Instances: 3},
	}
	cnpgReady, tlsSecret := makePoolerReadyCNPG(t, "pg1", "default")
	contracts := &reconcileContracts{CNPGCluster: cnpgReady}
	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler, tlsSecret).Build(),
		scheme, events, nil, cluster, clusterClass, poolerEnabledConfig(), contracts,
	)

	// Act: first Observe — condition is False, event must fire.
	_, err := model.Observe(context.Background(), nil)

	// Assert
	require.NoError(t, err)
	require.NotEmpty(t, events.normals)
	assert.Contains(t, events.normals[0], EventPoolerReady)

	// Act: second Observe with condition already True — no re-emission.
	cluster.Status.Conditions = []metav1.Condition{{Type: string(poolerReady), Status: metav1.ConditionTrue}}
	events.normals = nil
	_, err = model.Observe(context.Background(), nil)

	// Assert
	require.NoError(t, err)
	assert.Empty(t, events.normals)
}

func TestPoolerModelConvergeWaitsForSANPolicy(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)},
			},
		},
	}

	rwPooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace}}
	roPooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace}}
	// SANs not yet converged: pooler SANs absent from spec
	contracts := &reconcileContracts{
		CNPGCluster: &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
			Status:     cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy},
		},
	}
	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build(),
		scheme, noopEventEmitter{}, nil, cluster, clusterClass, poolerEnabledConfig(), contracts,
	)

	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Provisioning, health.State)
	assert.Equal(t, reasonPoolerSANsPending, health.Reason)
	assert.Equal(t, msgPoolerSANsPending, health.Message)
	assert.True(t, health.Result.RequeueAfter > 0)
}

func TestPoolerModelConvergeWaitsForTLSLeafMaterial(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)},
			},
		},
	}

	rwPooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace}}
	roPooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace}}
	// SANs converged but TLS secret NOT seeded → isServerTLSLeafAlignedWithSpec returns false
	cnpgReady, _ := makePoolerReadyCNPG(t, "pg1", "default")
	contracts := &reconcileContracts{CNPGCluster: cnpgReady}
	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler).Build(),
		scheme, noopEventEmitter{}, nil, cluster, clusterClass, poolerEnabledConfig(), contracts,
	)

	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Provisioning, health.State)
	assert.Equal(t, reasonPoolerTLSLeafPending, health.Reason)
	assert.Equal(t, msgPoolerTLSLeafPending, health.Message)
	assert.True(t, health.Result.RequeueAfter > 0)
}

func TestPoolerModelConvergeTLSLeafInvalidCertEmitsFailed(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "demo"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "demo"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPooler: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)},
			},
		},
	}

	// SANs converged; TLS secret exists but contains malformed PEM → errServerTLSLeafInvalid
	cnpgLive, validSecret := makePoolerReadyCNPG(t, "pg1", "demo")
	badTLSSecret := &corev1.Secret{
		ObjectMeta: validSecret.ObjectMeta, // same name/ns — overrides the valid cert
		Data:       map[string][]byte{corev1.TLSCertKey: []byte("not a valid PEM block")},
	}
	contracts := &reconcileContracts{CNPGCluster: cnpgLive}

	rwPooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readWriteEndpoint), Namespace: cluster.Namespace}}
	roPooler := &cnpgv1.Pooler{ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(cluster.Name, readOnlyEndpoint), Namespace: cluster.Namespace}}
	events := &captureEventEmitter{}
	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(rwPooler, roPooler, badTLSSecret).Build(),
		scheme, events, nil, cluster, clusterClass, poolerEnabledConfig(), contracts,
	)

	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)

	require.Error(t, err, "structural TLS-leaf failure must propagate so controller-runtime requeues with backoff")
	assert.True(t, errors.Is(err, errServerTLSLeafInvalid), "returned error must still wrap the sentinel for upstream callers")
	assert.Equal(t, pgcConstants.Failed, health.State, "structural failure must escalate to Failed, not Provisioning")
	assert.Equal(t, reasonPoolerTLSLeafInvalidCert, health.Reason, "Failed must use the dedicated reason")
	assert.Equal(t, failedClusterPhase, health.Phase)

	expectedMsg := fmt.Sprintf(string(msgFmtPoolerTLSLeafInvalidCert), "demo", "pg1-server-tls")
	assert.Equal(t, expectedMsg, health.Message, "Condition.Message must be the canonical scrubbed format")
	assert.NotContains(t, health.Message, "x509 parse failed", "Condition.Message must NOT leak parser internals")
	assert.NotContains(t, health.Message, "malformed certificate", "Condition.Message must NOT leak parser internals")

	require.Len(t, events.warnings, 1, "exactly one warning event must be emitted")
	emitted := events.warnings[0]
	assert.Contains(t, emitted, string(EventPoolerReconcileFailed)+":")
	assert.Contains(t, emitted, expectedMsg, "event payload must match Condition.Message exactly")
	assert.NotContains(t, emitted, "x509 parse failed", "event must NOT leak parser internals")
	assert.NotContains(t, emitted, "malformed certificate", "event must NOT leak parser internals")
}

func TestPoolerModelActuateDisabledIsCleanWhenCNPGAbsent(t *testing.T) {
	t.Parallel()

	scheme := newTestScheme()

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
	}

	events := &captureEventEmitter{}
	contracts := &reconcileContracts{} // no CNPGCluster — bootstrap race
	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		scheme, events, nil, cluster, clusterClass, &MergedConfig{Spec: &enterprisev4.PostgresClusterSpec{}}, contracts,
	)

	reconcileErr := model.Reconcile(context.Background())
	health, err := model.Observe(context.Background(), reconcileErr)
	require.NoError(t, err, "disabled-branch + nil CNPG must not produce an error")
	assert.NotEqual(t, pgcConstants.Failed, health.State, "disabled-branch + nil CNPG must not produce a Failed health condition")
	assert.Empty(t, events.warnings, "no warning events should be emitted on the happy bootstrap-race path")
}

func TestPoolerModelROPoolerWanted(t *testing.T) {
	t.Parallel()

	poolerOptedIn := &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)}
	specWith := func(instances *int32, pooler *enterprisev4.ConnectionPoolerEnableConfig) *enterprisev4.PostgresClusterSpec {
		return &enterprisev4.PostgresClusterSpec{Instances: instances, ConnectionPooler: pooler}
	}

	tests := []struct {
		name      string
		mergedCfg *MergedConfig
		want      bool
	}{
		{name: "nil merged config", mergedCfg: nil, want: false},
		{name: "nil spec", mergedCfg: &MergedConfig{}, want: false},
		{name: "nil instances", mergedCfg: &MergedConfig{Spec: specWith(nil, poolerOptedIn)}, want: false},
		{name: "ro opted out", mergedCfg: &MergedConfig{Spec: specWith(ptr.To(int32(2)), &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadOnly: ptr.To(false)})}, want: false},
		{name: "instances 1", mergedCfg: &MergedConfig{Spec: specWith(ptr.To(int32(1)), poolerOptedIn)}, want: false},
		{name: "instances 2", mergedCfg: &MergedConfig{Spec: specWith(ptr.To(int32(2)), poolerOptedIn)}, want: true},
		{name: "instances 3", mergedCfg: &MergedConfig{Spec: specWith(ptr.To(int32(3)), poolerOptedIn)}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &poolerModel{mergedConfig: tt.mergedCfg}
			assert.Equal(t, tt.want, p.roPoolerWanted())
		})
	}
}

func TestPoolerModelRWPoolerWanted(t *testing.T) {
	t.Parallel()

	specWith := func(pooler *enterprisev4.ConnectionPoolerEnableConfig) *enterprisev4.PostgresClusterSpec {
		return &enterprisev4.PostgresClusterSpec{ConnectionPooler: pooler}
	}

	tests := []struct {
		name      string
		mergedCfg *MergedConfig
		want      bool
	}{
		{name: "nil merged config", mergedCfg: nil, want: false},
		{name: "nil spec", mergedCfg: &MergedConfig{}, want: false},
		{name: "nil pooler config", mergedCfg: &MergedConfig{Spec: specWith(nil)}, want: false},
		{name: "rw default true", mergedCfg: &MergedConfig{Spec: specWith(&enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)})}, want: true},
		{name: "rw opted out", mergedCfg: &MergedConfig{Spec: specWith(&enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadWrite: ptr.To(false)})}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &poolerModel{mergedConfig: tt.mergedCfg}
			assert.Equal(t, tt.want, p.rwPoolerWanted())
		})
	}
}

func TestMergeConnectionPoolerEnable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cluster *enterprisev4.ConnectionPoolerEnableConfig
		class   *enterprisev4.ConnectionPoolerEnableConfig
		want    *enterprisev4.ConnectionPoolerEnableConfig
	}{
		{name: "both nil", cluster: nil, class: nil, want: nil},
		{
			name:    "class only",
			cluster: nil,
			class:   &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadOnly: ptr.To(false)},
			want:    &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadOnly: ptr.To(false)},
		},
		{
			name:    "cluster overrides one field, class supplies the rest",
			cluster: &enterprisev4.ConnectionPoolerEnableConfig{ReadOnly: ptr.To(false)},
			class:   &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadWrite: ptr.To(true), ReadOnly: ptr.To(true)},
			want:    &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadWrite: ptr.To(true), ReadOnly: ptr.To(false)},
		},
		{
			name:    "cluster fully specified ignores class",
			cluster: &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(false), ReadWrite: ptr.To(false), ReadOnly: ptr.To(false)},
			class:   &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadWrite: ptr.To(true), ReadOnly: ptr.To(true)},
			want:    &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(false), ReadWrite: ptr.To(false), ReadOnly: ptr.To(false)},
		},
		{
			// Regression for MR1935 P1: a cluster that overrides only readOnly must still
			// inherit enabled=true from its class. This relies on cluster.Enabled being the
			// nil "inherit" sentinel — which CRD-level +kubebuilder:default markers would
			// have destroyed by materializing enabled=false onto the stored cluster object.
			name:    "cluster overrides only readOnly, inherits enabled from minimal class",
			cluster: &enterprisev4.ConnectionPoolerEnableConfig{ReadOnly: ptr.To(false)},
			class:   &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true)},
			want:    &enterprisev4.ConnectionPoolerEnableConfig{Enabled: ptr.To(true), ReadOnly: ptr.To(false)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mergeConnectionPoolerEnable(tt.cluster, tt.class))
		})
	}
}
