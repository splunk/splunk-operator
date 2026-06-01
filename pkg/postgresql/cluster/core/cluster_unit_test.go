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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"testing"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/google/go-cmp/cmp"
	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	pgcConstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	structuraldefaulting "k8s.io/apiextensions-apiserver/pkg/apiserver/schema/defaulting"
	"k8s.io/apimachinery/pkg/api/equality"
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
	"sigs.k8s.io/yaml"
)

type sanPolicyMock struct {
	ensureSANPolicyFn      func(context.Context) error
	isSANPolicyConvergedFn func(context.Context) (bool, error)
}

func (m *sanPolicyMock) EnsureSANPolicy(ctx context.Context) error {
	if m.ensureSANPolicyFn == nil {
		return nil
	}
	return m.ensureSANPolicyFn(ctx)
}

func (m *sanPolicyMock) IsSANPolicyConverged(ctx context.Context) (bool, error) {
	if m.isSANPolicyConvergedFn == nil {
		return true, nil
	}
	return m.isSANPolicyConvergedFn(ctx)
}

// clusterRuntimeProbeMock is the read-side counterpart of sanPolicyMock.
// Zero-value returns (true, nil) so tests that don't care about runtime
// observations can pass `&clusterRuntimeProbeMock{}` unconfigured.
type clusterRuntimeProbeMock struct {
	isServerTLSLeafAlignedWithSpecFn func(context.Context) (bool, error)
}

func (m *clusterRuntimeProbeMock) IsServerTLSLeafAlignedWithSpec(ctx context.Context) (bool, error) {
	if m.isServerTLSLeafAlignedWithSpecFn == nil {
		return true, nil
	}
	return m.isServerTLSLeafAlignedWithSpecFn(ctx)
}

func testSelfSignedLeafCertPEM(t *testing.T, dnsNames []string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     dnsNames,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if len(dnsNames) > 0 {
		tmpl.Subject = pkix.Name{CommonName: dnsNames[0]}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

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
			name: "image digest suffix stripped for drift detection",
			spec: cnpgv1.ClusterSpec{
				ImageName:            "ghcr.io/cloudnative-pg/postgresql:18@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Instances:            1,
				StorageConfiguration: cnpgv1.StorageConfiguration{Size: "10Gi"},
			},
			customDefinedParameters: nil,
			expected: normalizedCNPGClusterSpec{
				ImageName:           "ghcr.io/cloudnative-pg/postgresql:18",
				Instances:           1,
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

// Only cnpgPatchBody may force Converge to hold ClusterReady=Provisioning;
// cnpgPatchNone / cnpgPatchMetadata must not gate. Add a row when adding a
// new cnpgPatchKind constant.
func TestCNPGPatchKind_RequiresPhaseGate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		kind     cnpgPatchKind
		wantGate bool
	}{
		{"none does not gate", cnpgPatchNone, false},
		{"annotation-only does not gate", cnpgPatchMetadata, false},
		{"material drift gates", cnpgPatchBody, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.wantGate, tc.kind.requiresPhaseGate())
		})
	}
}

// TestisClusterDrift locks the materiality contract used by Actuate to gate
// ClusterReady. Adding a new field to normalizedCNPGClusterSpec should add a
// matching positive-case row here so the contract stays observable in tests.
func TestIsClusterDrift(t *testing.T) {
	t.Parallel()

	// clone copies the fields that hold reference types (slices, maps) so each
	// table row mutates an independent value; otherwise a row that swaps the
	// annotations map header would still share backing storage with `base`.
	clone := func(s normalizedCNPGClusterSpec) normalizedCNPGClusterSpec {
		out := s
		if s.InheritedAnnotations != nil {
			out.InheritedAnnotations = make(map[string]string, len(s.InheritedAnnotations))
			for k, v := range s.InheritedAnnotations {
				out.InheritedAnnotations[k] = v
			}
		}
		if s.CustomDefinedParameters != nil {
			out.CustomDefinedParameters = make(map[string]string, len(s.CustomDefinedParameters))
			for k, v := range s.CustomDefinedParameters {
				out.CustomDefinedParameters[k] = v
			}
		}
		if s.PgHBA != nil {
			out.PgHBA = append([]string(nil), s.PgHBA...)
		}
		return out
	}

	base := normalizedCNPGClusterSpec{
		ImageName:           "ghcr.io/cloudnative-pg/postgresql:17",
		Instances:           3,
		PrimaryUpdateMethod: "restart",
		StorageSize:         "10Gi",
		Resources:           corev1.ResourceRequirements{},
		DefaultDatabase:     "app",
		Owner:               "app",
		InheritedAnnotations: map[string]string{
			prometheusScrapeAnnotation: "true",
			prometheusPortAnnotation:   postgresMetricsPortString,
		},
	}

	tests := []struct {
		name   string
		mutate func(s *normalizedCNPGClusterSpec)
		want   bool
	}{
		{
			name:   "identical specs are not material drift",
			mutate: func(s *normalizedCNPGClusterSpec) {},
			want:   false,
		},
		{
			name: "annotation-only drift is NOT material",
			// Prometheus-scrape annotations toggle on/off without recreating pods or
			// transitioning CNPG phase; gating ready on this would deadlock the gate.
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.InheritedAnnotations = map[string]string{
					prometheusScrapeAnnotation: "false",
				}
			},
			want: false,
		},
		{
			name: "annotation cleared (non-nil → nil) is NOT material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.InheritedAnnotations = nil
			},
			want: false,
		},
		{
			name: "image rollout IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.ImageName = "ghcr.io/cloudnative-pg/postgresql:18"
			},
			want: true,
		},
		{
			name: "instance scale IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.Instances = 5
			},
			want: true,
		},
		{
			name: "storage resize IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.StorageSize = "20Gi"
			},
			want: true,
		},
		{
			name: "PrimaryUpdateMethod drift (\"\" → \"restart\") IS material",
			// This is the exact shape of the phantom-drift bug; isClusterDrift must
			// flag it so Converge defers ready until phase reflects the patch.
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.PrimaryUpdateMethod = ""
			},
			want: true,
		},
		{
			name: "pg_hba change IS material",
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.PgHBA = []string{"host all all all md5"}
			},
			want: true,
		},
		{
			name: "image change + annotation change together IS material",
			// Co-occurring material drift must not be hidden by the annotation strip.
			mutate: func(s *normalizedCNPGClusterSpec) {
				s.ImageName = "ghcr.io/cloudnative-pg/postgresql:18"
				s.InheritedAnnotations = map[string]string{prometheusScrapeAnnotation: "false"}
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := clone(base)
			b := clone(base)
			tt.mutate(&b)
			assert.Equal(t, tt.want, isClusterDrift(a, b),
				"isClusterDrift result mismatch; remember: every normalized field is material EXCEPT InheritedAnnotations")
		})
	}

	t.Run("does not mutate caller's specs", func(t *testing.T) {
		// Pass-by-value is deliberate; this test guards against a future refactor
		// that "optimizes" the helper into pointer receivers and accidentally
		// nukes the caller's InheritedAnnotations map header.
		a := clone(base)
		b := clone(base)
		b.ImageName = "different"

		_ = isClusterDrift(a, b)

		require.NotNil(t, a.InheritedAnnotations, "isClusterDrift must not mutate caller's a.InheritedAnnotations")
		require.NotNil(t, b.InheritedAnnotations, "isClusterDrift must not mutate caller's b.InheritedAnnotations")
		assert.Equal(t, "true", a.InheritedAnnotations[prometheusScrapeAnnotation])
		assert.Equal(t, "true", b.InheritedAnnotations[prometheusScrapeAnnotation])
	})
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
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, currentConfig, "pg1-secret", false),
	}
	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingCNPG).Build()

	model := newClusterModel(c, scheme, events, nil, cluster, clusterClass, desiredConfig, "pg1-secret")
	model.Actuate(context.Background())

	// PrimaryUpdateMethod is a material (structural) field — Converge must
	// gate phase on this patch. Stronger than the previous "cnpgPatched true"
	// assertion: also locks the classification.
	require.Equal(t, cnpgPatchBody, model.cnpgPatch)
	assert.True(t, model.cnpgPatch.requiresPhaseGate())
	assert.False(t, model.cnpgCreated)

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.Equal(t, cnpgv1.PrimaryUpdateMethodSwitchover, updated.Spec.PrimaryUpdateMethod)
	assert.Contains(t, events.normals, EventClusterUpdateStarted+":CNPG cluster spec updated for PostgresCluster pg1, waiting for healthy state")
}

func TestClusterModelActuatePreservesManagedRoles(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	instances := int32(3)
	version := "15.13"
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

	existingCNPG := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.Name, Namespace: cluster.Namespace},
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "pg1-secret", false),
	}
	// Simulate managed roles having been written by managedRolesModel on a prior reconcile.
	roleConfig := cnpgv1.ManagedConfiguration{
		Roles: []cnpgv1.RoleConfiguration{
			{Name: "app-user", Ensure: cnpgv1.EnsurePresent, Login: true},
		},
	}
	existingCNPG.Spec.Managed = roleConfig.DeepCopy()

	// Trigger a spec drift so clusterModel.Actuate actually patches.
	updatedInstances := int32(5)
	driftedCfg := &MergedConfig{
		Spec: &enterprisev4.PostgresClusterSpec{
			Instances:        &updatedInstances,
			PostgresVersion:  &version,
			Storage:          &storageSize,
			Resources:        &corev1.ResourceRequirements{},
			PostgreSQLConfig: map[string]string{},
			PgHBA:            []string{},
		},
		CNPG: &enterprisev4.CNPGConfig{PrimaryUpdateMethod: ptr.To("restart")},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingCNPG).Build()
	model := newClusterModel(c, scheme, &captureEventEmitter{}, nil, cluster, clusterClass, driftedCfg, "pg1-secret")
	model.Actuate(context.Background())

	require.True(t, model.cnpgPatch.requiresPhaseGate())

	updated := &cnpgv1.Cluster{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(existingCNPG), updated))
	assert.NotNil(t, updated.Spec.Managed)
	assert.Equal(t, &roleConfig, updated.Spec.Managed)
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

	spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "my-secret", false)

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
		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "my-secret", true)

		require.NotNil(t, spec.InheritedMetadata)
		assert.Equal(t, "true", spec.InheritedMetadata.Annotations[prometheusScrapeAnnotation])
		assert.Equal(t, metricsPath, spec.InheritedMetadata.Annotations[prometheusPathAnnotation])
		assert.Equal(t, postgresMetricsPortString, spec.InheritedMetadata.Annotations[prometheusPortAnnotation])
	})

	t.Run("preserves unowned fields from live spec", func(t *testing.T) {

		managedRoles := []cnpgv1.RoleConfiguration{
			{
				Name:   "app_user",
				Ensure: cnpgv1.EnsurePresent,
			},
			{
				Name:   "app_admin",
				Ensure: cnpgv1.EnsurePresent,
			},
		}

		liveCluster := cnpgv1.ClusterSpec{Managed: &cnpgv1.ManagedConfiguration{Roles: managedRoles}}
		spec := buildCNPGClusterSpec(liveCluster, cfg, "my-secret", true)

		require.NotNil(t, spec.Managed)
		assert.Equal(t, managedRoles, spec.Managed.Roles)
		assert.Equal(t, "ghcr.io/cloudnative-pg/postgresql:18", spec.ImageName)
		assert.Equal(t, 3, spec.Instances)
		require.NotNil(t, spec.SuperuserSecret)
		assert.Equal(t, "my-secret", spec.SuperuserSecret.Name)
		assert.Equal(t, "my-secret", spec.Bootstrap.InitDB.Secret.Name)

	})

	t.Run("sets backup when enabled and volume snapshot configured", func(t *testing.T) {
		t.Parallel()
		enabled := true
		className := "csi-snapclass"
		backupCfg := *cfg
		backupCfg.Spec.Backup = &enterprisev4.BackupConfig{Enabled: &enabled}
		backupCfg.CNPG = &enterprisev4.CNPGConfig{
			PrimaryUpdateMethod: cfg.CNPG.PrimaryUpdateMethod,
			Backup: &enterprisev4.CNPGBackupConfig{
				VolumeSnapshot: &enterprisev4.CNPGVolumeSnapshotConfig{
					ClassName: &className,
				},
			},
		}

		spec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, &backupCfg, "my-secret", false)

		require.NotNil(t, spec.Backup)
		require.NotNil(t, spec.Backup.VolumeSnapshot)
		assert.Equal(t, className, spec.Backup.VolumeSnapshot.ClassName)
	})

	t.Run("clears stale backup from live spec when backup is disabled", func(t *testing.T) {
		t.Parallel()
		// live spec has a backup block left over from when backup was previously enabled
		staleBackup := &cnpgv1.BackupConfiguration{
			VolumeSnapshot: &cnpgv1.VolumeSnapshotConfiguration{ClassName: "old-snapclass"},
		}
		liveSpec := cnpgv1.ClusterSpec{Backup: staleBackup}

		disabled := false
		disabledCfg := *cfg
		disabledCfg.Spec.Backup = &enterprisev4.BackupConfig{Enabled: &disabled}

		spec := buildCNPGClusterSpec(liveSpec, &disabledCfg, "my-secret", false)

		assert.Nil(t, spec.Backup, "stale backup config must be cleared when backup is disabled")
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
		Status: cnpgv1.ClusterStatus{
			WriteService: "my-cluster-rw",
			ReadService:  "my-cluster-ro",
		},
	}

	t.Run("base endpoints without poolers", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(context.Background(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret", true)

		require.NoError(t, err)
		assert.Equal(t, "my-cluster-configmap", cm.Name)
		assert.Equal(t, "default", cm.Namespace)
		assert.Equal(t, "my-cluster-rw.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterRWEndpoint])
		assert.Equal(t, "my-cluster-ro.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterROEndpoint])
		assert.Equal(t, "my-cluster-r.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterREndpoint])
		assert.Equal(t, pgconninfo.DefaultPort, cm.Data[pgconninfo.KeyDefaultClusterPort])
		assert.Equal(t, "postgres", cm.Data[configMapKeySuperUserName])
		assert.Equal(t, "my-secret", cm.Data[configMapKeySuperUserSecretRef])
		assert.NotContains(t, cm.Data, pgconninfo.KeyPoolerRWEndpoint)
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
		cm, err := generateConfigMap(context.Background(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret", true)

		require.NoError(t, err)
		assert.Equal(t, "my-cluster-pooler-rw.default.svc.cluster.local", cm.Data[pgconninfo.KeyPoolerRWEndpoint])
		assert.Equal(t, "my-cluster-pooler-ro.default.svc.cluster.local", cm.Data[pgconninfo.KeyPoolerROEndpoint])
	})

	t.Run("uses existing configmap name from status", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		pg := cluster.DeepCopy()
		pg.Status.Resources = &enterprisev4.PostgresClusterResources{
			ConfigMapRef: &corev1.LocalObjectReference{Name: "custom-configmap"},
		}

		cm, err := generateConfigMap(context.Background(), c, scheme, pg, cnpgCluster, "my-secret", true)

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
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpg, "my-secret", true)
		require.NoError(t, err)
		expectedCASecretRef := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "my-server-ca"},
			Key:                  defaultServerCACertKey,
		}
		assert.Equal(t, fmt.Sprintf("%s/%s", expectedCASecretRef.Name, expectedCASecretRef.Key), cm.Data[configMapKeyServerCASecretRef])
		assert.NotContains(t, cm.Data, "SERVER_CA_CERT_KEY")
	})

	t.Run("omits CA metadata when not available", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret", true)
		require.NoError(t, err)
		assert.NotContains(t, cm.Data, configMapKeyServerCASecretRef)
		assert.NotContains(t, cm.Data, "SERVER_CA_CERT_KEY")
	})

	t.Run("omits CA metadata when status is not available", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret", true)
		require.NoError(t, err)
		assert.NotContains(t, cm.Data, configMapKeyServerCASecretRef)
		assert.NotContains(t, cm.Data, "SERVER_CA_CERT_KEY")
	})

	t.Run("omits CA metadata when secret is not found", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cm, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpgCluster, "my-secret", true)
		require.NoError(t, err)
		assert.NotContains(t, cm.Data, configMapKeyServerCASecretRef)
		assert.NotContains(t, cm.Data, "SERVER_CA_CERT_KEY")
	})

	t.Run("fails when CNPG service names are not available yet", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cnpg := cnpgCluster.DeepCopy()
		cnpg.Status.WriteService = ""

		_, err := generateConfigMap(t.Context(), c, scheme, cluster.DeepCopy(), cnpg, "my-secret", false)

		require.Error(t, err)
		assert.ErrorContains(t, err, "write service name is required")
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
			pgconninfo.KeyClusterRWEndpoint:  "pg1-rw.default.svc.cluster.local",
			pgconninfo.KeyClusterROEndpoint:  "pg1-ro.default.svc.cluster.local",
			pgconninfo.KeyClusterREndpoint:   "pg1-r.default.svc.cluster.local",
			pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
			configMapKeySuperUserSecretRef:   "pg1-secret",
		},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: cnpgv1.ClusterStatus{
			Phase:        cnpgv1.PhaseHealthy,
			WriteService: "pg1-rw",
			ReadService:  "pg1-ro",
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
		clusterRuntimeViewAdapter{model: &clusterModel{client: c, cluster: cluster, cnpgCluster: cnpg}},
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

// Asserts configMapModel.Actuate omits CLUSTER_POOLER_* until the server TLS
// leaf covers the pooler SANs. Two subtests share one fake apiserver to mirror
// the live reconcile sequence (stale leaf, then rotated leaf).
func TestConfigMapActuateGatesPoolerKeysOnTLSLeafAlignment(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, enterprisev4.AddToScheme(scheme))

	const (
		clusterName   = "pg1"
		namespace     = "default"
		tlsSecretName = "pg1-server-tls"
		baseRWSAN     = "pg1-rw.default.svc.cluster.local"
		poolerRWSAN   = "pg1-pooler-rw.default.svc.cluster.local"
		poolerROSAN   = "pg1-pooler-ro.default.svc.cluster.local"
		configMapName = clusterName + defaultConfigMapSuffix
	)
	desiredSANs := []string{baseRWSAN, poolerRWSAN, poolerROSAN}

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace, UID: "cluster-uid"},
	}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{ServerAltDNSNames: desiredSANs},
		},
		Status: cnpgv1.ClusterStatus{
			Phase:        cnpgv1.PhaseHealthy,
			WriteService: clusterName + "-rw",
			ReadService:  clusterName + "-ro",
			Certificates: cnpgv1.CertificatesStatus{
				CertificatesConfiguration: cnpgv1.CertificatesConfiguration{ServerTLSSecret: tlsSecretName},
			},
		},
	}
	rwPooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(clusterName, readWriteEndpoint), Namespace: namespace},
	}
	roPooler := &cnpgv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{Name: poolerResourceName(clusterName, readOnlyEndpoint), Namespace: namespace},
	}
	staleSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: tlsSecretName, Namespace: namespace},
		Data:       map[string][]byte{corev1.TLSCertKey: testSelfSignedLeafCertPEM(t, []string{baseRWSAN})},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cnpg.DeepCopy(), rwPooler, roPooler, staleSecret).
		Build()

	model := newConfigMapModel(
		c,
		scheme,
		noopEventEmitter{},
		nil,
		clusterRuntimeViewAdapter{model: &clusterModel{client: c, cluster: cluster, cnpgCluster: cnpg}},
		cluster,
		"pg1-secret",
	)

	t.Run("stale_leaf_omits_pooler_keys", func(t *testing.T) {
		model.Actuate(t.Context())
		require.NoError(t, model.actuateErr)

		var cm corev1.ConfigMap
		require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: configMapName, Namespace: namespace}, &cm))

		assert.Equal(t, "pg1-rw.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterRWEndpoint])
		assert.Equal(t, "pg1-ro.default.svc.cluster.local", cm.Data[pgconninfo.KeyClusterROEndpoint])
		assert.NotContains(t, cm.Data, pgconninfo.KeyPoolerRWEndpoint)
		assert.NotContains(t, cm.Data, pgconninfo.KeyPoolerROEndpoint)
	})

	t.Run("aligned_leaf_publishes_pooler_keys", func(t *testing.T) {
		var rotated corev1.Secret
		require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: tlsSecretName, Namespace: namespace}, &rotated))
		rotated.Data[corev1.TLSCertKey] = testSelfSignedLeafCertPEM(t, desiredSANs)
		require.NoError(t, c.Update(t.Context(), &rotated))

		model.Actuate(t.Context())
		require.NoError(t, model.actuateErr)

		var cm corev1.ConfigMap
		require.NoError(t, c.Get(t.Context(), client.ObjectKey{Name: configMapName, Namespace: namespace}, &cm))

		assert.Equal(t, "pg1-pooler-rw.default.svc.cluster.local", cm.Data[pgconninfo.KeyPoolerRWEndpoint])
		assert.Equal(t, "pg1-pooler-ro.default.svc.cluster.local", cm.Data[pgconninfo.KeyPoolerROEndpoint])
	})
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
			pgconninfo.KeyClusterRWEndpoint:  "pg1-rw.default.svc.cluster.local",
			pgconninfo.KeyClusterROEndpoint:  "pg1-ro.default.svc.cluster.local",
			pgconninfo.KeyClusterREndpoint:   "pg1-r.default.svc.cluster.local",
			pgconninfo.KeyDefaultClusterPort: pgconninfo.DefaultPort,
			configMapKeySuperUserSecretRef:   "pg1-secret",
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
			Spec: buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, mergedConfig, "pg1-secret", false),
			Status: cnpgv1.ClusterStatus{
				Phase:        cnpgv1.PhaseHealthy,
				WriteService: cluster.Name + "-rw",
				ReadService:  cluster.Name + "-ro",
			},
		}
		require.NoError(t, ctrl.SetControllerReference(cluster, cnpg, scheme))
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()
		return newClusterModel(c, scheme, noopEventEmitter{}, nil, cluster, exampleClusterClass, mergedConfig, "pg1-secret")
	}

	// client+cluster are required for IsServerTLSLeafAlignedWithSpec to
	// short-circuit (no CNPG seeded → probe returns true).
	makeRuntimeView := func(c client.Client, cluster *enterprisev4.PostgresCluster, healthy bool) clusterRuntimeView {
		if !healthy {
			return clusterRuntimeViewAdapter{model: &clusterModel{client: c, cluster: cluster}}
		}
		return clusterRuntimeViewAdapter{model: &clusterModel{
			client:  c,
			cluster: cluster,
			cnpgCluster: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				Status: cnpgv1.ClusterStatus{
					Phase:        cnpgv1.PhaseHealthy,
					WriteService: "pg1-rw",
					ReadService:  "pg1-ro",
				},
			},
		}}
	}

	makeRuntimeViewWithCA := func(c client.Client, cluster *enterprisev4.PostgresCluster, healthy bool) clusterRuntimeView {
		if !healthy {
			return clusterRuntimeViewAdapter{model: &clusterModel{client: c, cluster: cluster}}
		}
		return clusterRuntimeViewAdapter{model: &clusterModel{
			client:  c,
			cluster: cluster,
			cnpgCluster: &cnpgv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
				Status: cnpgv1.ClusterStatus{
					Phase:        cnpgv1.PhaseHealthy,
					WriteService: "pg1-rw",
					ReadService:  "pg1-ro",
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
					&sanPolicyMock{},
					&clusterRuntimeProbeMock{},
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
					&sanPolicyMock{},
					&clusterRuntimeProbeMock{},
				)
				cmClient := configMapNotFoundClient{
					Client: fake.NewClientBuilder().
						WithScheme(scheme).
						Build(),
				}
				configMap := newConfigMapModel(
					cmClient,
					scheme,
					noopEventEmitter{},
					nil,
					makeRuntimeView(cmClient, cluster, true),
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
					&sanPolicyMock{},
					&clusterRuntimeProbeMock{},
				)
				cmClient := fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(exampleCm, exampleCASecret).
					Build()
				configMap := newConfigMapModel(
					cmClient,
					scheme,
					noopEventEmitter{},
					nil,
					makeRuntimeViewWithCA(cmClient, cluster, true),
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
				return newPoolerModel(errClient, scheme, noopEventEmitter{}, updateStatus, cluster, clusterClass, poolerCfg, cnpg, true, true, &sanPolicyMock{}, &clusterRuntimeProbeMock{})
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
					clusterRuntimeViewAdapter{model: &clusterModel{client: errClient, cluster: cluster, cnpgCluster: cnpg}},
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
			&sanPolicyMock{},
			&clusterRuntimeProbeMock{},
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
			&sanPolicyMock{},
			&clusterRuntimeProbeMock{},
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
		model := newPoolerModel(
			c, scheme, noopEventEmitter{},
			nil, cluster, clusterClass, &MergedConfig{},
			&cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}},
			true, true,
			&sanPolicyMock{},
			&clusterRuntimeProbeMock{})

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
		model := newPoolerModel(c, scheme, noopEventEmitter{},
			nil, cluster, clusterClass, &MergedConfig{},
			&cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}},
			true, true,
			&sanPolicyMock{},
			&clusterRuntimeProbeMock{})

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
			&sanPolicyMock{},
			&clusterRuntimeProbeMock{},
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
		&sanPolicyMock{},
		&clusterRuntimeProbeMock{},
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

func TestPoolerModelConvergeWaitsForSANPolicy(t *testing.T) {
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

	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		scheme,
		noopEventEmitter{},
		nil,
		cluster,
		clusterClass,
		&MergedConfig{},
		&cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}},
		true,
		true,
		&sanPolicyMock{
			isSANPolicyConvergedFn: func(context.Context) (bool, error) { return false, nil },
		},
		&clusterRuntimeProbeMock{},
	)

	health, err := model.Converge(context.Background())
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Provisioning, health.State)
	assert.Equal(t, reasonPoolerSANsPending, health.Reason)
	assert.Equal(t, msgPoolerSANsPending, health.Message)
	assert.True(t, health.Result.RequeueAfter > 0)
}

func TestPoolerModelConvergeWaitsForTLSLeafMaterial(t *testing.T) {
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

	// Healthy CNPG so upstream gates pass; the ClusterRuntimeProbe mock is the seam.
	cnpgLive := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpgLive.DeepCopy()).Build()

	// Asserts the port contract only: probe-false ⇒ PoolerTLSLeafPending + requeue.
	// Probe internals are covered by TestClusterModelIsServerTLSLeafAlignedWithSpec.
	model := newPoolerModel(
		c,
		scheme,
		noopEventEmitter{},
		nil,
		cluster,
		clusterClass,
		&MergedConfig{},
		cnpgLive,
		true,
		true,
		&sanPolicyMock{
			isSANPolicyConvergedFn: func(context.Context) (bool, error) { return true, nil },
		},
		&clusterRuntimeProbeMock{
			isServerTLSLeafAlignedWithSpecFn: func(context.Context) (bool, error) { return false, nil },
		},
	)

	health, err := model.Converge(context.Background())
	require.NoError(t, err)
	assert.Equal(t, pgcConstants.Provisioning, health.State)
	assert.Equal(t, reasonPoolerTLSLeafPending, health.Reason)
	assert.Equal(t, msgPoolerTLSLeafPending, health.Message)
	assert.True(t, health.Result.RequeueAfter > 0)
}

// Drives the structural-failure leg of the TLS-leaf check: probe returns the
// errServerTLSLeafInvalid sentinel → Converge must surface a Failed condition
// with the dedicated reason and a SCRUBBED Message/event (no parser internals).
func TestPoolerModelConvergeTLSLeafInvalidCertEmitsFailed(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "demo"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "demo"},
		Spec: enterprisev4.PostgresClusterClassSpec{
			Config: &enterprisev4.PostgresClusterClassConfig{
				ConnectionPoolerEnabled: ptr.To(true),
			},
		},
	}

	// Seed Status.Certificates.ServerTLSSecret so the demux can resolve a
	// secret name independent of the wrapped error string (caller MUST NOT
	// parse the error to learn the secret name).
	cnpgLive := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "demo"},
		Status: cnpgv1.ClusterStatus{
			Phase: cnpgv1.PhaseHealthy,
			Certificates: cnpgv1.CertificatesStatus{
				CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
					ServerTLSSecret: "pg1-server-tls",
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpgLive.DeepCopy()).Build()

	events := &captureEventEmitter{}
	probeSentinel := fmt.Errorf("%w: x509 parse failed for secret demo/pg1-server-tls: x509: malformed certificate", errServerTLSLeafInvalid)

	model := newPoolerModel(
		c,
		scheme,
		events,
		nil,
		cluster,
		clusterClass,
		&MergedConfig{},
		cnpgLive,
		true,
		true,
		&sanPolicyMock{
			isSANPolicyConvergedFn: func(context.Context) (bool, error) { return true, nil },
		},
		&clusterRuntimeProbeMock{
			isServerTLSLeafAlignedWithSpecFn: func(context.Context) (bool, error) {
				return false, probeSentinel
			},
		},
	)

	health, err := model.Converge(context.Background())

	require.Error(t, err, "structural TLS-leaf failure must propagate so controller-runtime requeues with backoff")
	assert.True(t, errors.Is(err, errServerTLSLeafInvalid), "returned error must still wrap the sentinel for upstream callers")
	assert.Equal(t, pgcConstants.Failed, health.State, "structural failure must escalate to Failed, not Provisioning")
	assert.Equal(t, reasonPoolerTLSLeafInvalidCert, health.Reason, "Failed must use the dedicated reason, not the generic PoolerReconciliationFailed")
	assert.Equal(t, failedClusterPhase, health.Phase)

	expectedMsg := fmt.Sprintf(string(msgFmtPoolerTLSLeafInvalidCert), "demo", "pg1-server-tls")
	assert.Equal(t, expectedMsg, health.Message, "Condition.Message must be the canonical scrubbed format, not the wrapped error string")
	assert.NotContains(t, health.Message, "x509 parse failed", "Condition.Message must NOT leak parser internals")
	assert.NotContains(t, health.Message, "malformed certificate", "Condition.Message must NOT leak parser internals")

	require.Len(t, events.warnings, 1, "exactly one warning event must be emitted")
	emitted := events.warnings[0]
	assert.Contains(t, emitted, string(EventPoolerReconcileFailed)+":")
	assert.Contains(t, emitted, expectedMsg, "event payload must match Condition.Message exactly")
	assert.NotContains(t, emitted, "x509 parse failed", "event must NOT leak parser internals")
	assert.NotContains(t, emitted, "malformed certificate", "event must NOT leak parser internals")
}

func TestPoolerModelActuatePropagatesSANEnsureFailure(t *testing.T) {
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

	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		scheme,
		noopEventEmitter{},
		nil,
		cluster,
		clusterClass,
		&MergedConfig{},
		&cnpgv1.Cluster{Status: cnpgv1.ClusterStatus{Phase: cnpgv1.PhaseHealthy}},
		true,
		true,
		&sanPolicyMock{
			ensureSANPolicyFn: func(context.Context) error { return assert.AnError },
		},
		&clusterRuntimeProbeMock{},
	)

	model.Actuate(context.Background())
	health, err := model.Converge(context.Background())
	require.Error(t, err)
	require.ErrorIs(t, err, assert.AnError)
	assert.Equal(t, pgcConstants.Failed, health.State)
	assert.Equal(t, reasonPoolerReconciliationFailed, health.Reason)
}

// Disabled-branch + nil CNPG snapshot is the bootstrap-race case: user has
// poolerEnabled=false and the cnpgv1.Cluster has not been reconciled into
// existence yet. The disabled branch deliberately skips EnsureSANPolicy
// (SAN policy is a no-op without a pooler), so Actuate must complete
// cleanly without surfacing a Failed condition or an actuateErr.
func TestPoolerModelActuateDisabledIsCleanWhenCNPGAbsent(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, enterprisev4.AddToScheme(scheme))
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	cluster := &enterprisev4.PostgresCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
	}
	clusterClass := &enterprisev4.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1-class", Namespace: "default"},
	}

	// Mock is wired for completeness but the disabled branch must not call
	// it — SAN policy enforcement is gated out at the orchestration layer.
	// We're asserting the outcome: no Failed, no error, no warning event.
	events := &captureEventEmitter{}
	model := newPoolerModel(
		fake.NewClientBuilder().WithScheme(scheme).Build(),
		scheme,
		events,
		nil,
		cluster,
		clusterClass,
		&MergedConfig{},
		nil, // cnpgCluster absent — bootstrap race
		false,
		false,
		&sanPolicyMock{},
		&clusterRuntimeProbeMock{},
	)

	model.Actuate(context.Background())

	assert.NoError(t, model.actuateErr, "Actuate must complete cleanly; SAN leaf short-circuits and pooler deletion is CNPG-independent")
	assert.NotEqual(t, pgcConstants.Failed, model.health.State, "disabled-branch + nil CNPG must not produce a Failed health condition")
	assert.Empty(t, events.warnings, "no warning events should be emitted on the happy bootstrap-race path")
}

// EnsureSANPolicy must NOT silently succeed when the cnpgv1.Cluster has
// vanished between the orchestration-layer snapshot and the leaf's fresh Get
// (the TOCTOU window). It must surface a real error so the caller can route
// to Failed and controller-runtime requeues with backoff.
func TestClusterModelEnsureSANPolicyReturnsErrorWhenCNPGMissing(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	// Empty client → getCNPGCluster returns (nil, nil) → leaf must escalate.
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	model := &clusterModel{
		client: c,
		cluster: &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		},
		mergedConfig: &MergedConfig{
			Spec: &enterprisev4.PostgresClusterSpec{
				ConnectionPoolerEnabled: ptr.To(true),
			},
		},
	}

	err := model.EnsureSANPolicy(context.Background())
	require.Error(t, err, "missing CNPG MUST escalate; silent nil would lie about idempotent convergence")
	assert.Contains(t, err.Error(), "default/pg1", "error must identify the offending resource for log search")
	assert.Contains(t, err.Error(), "not found", "error must name the actual failure mode for the operator")
}

func TestClusterModelEnsureSANPolicy(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: []string{"existing.example"},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()

	model := &clusterModel{
		client: c,
		cluster: &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		},
		mergedConfig: &MergedConfig{
			Spec: &enterprisev4.PostgresClusterSpec{
				ConnectionPoolerEnabled: ptr.To(true),
			},
		},
	}
	require.NoError(t, model.EnsureSANPolicy(context.Background()))

	var got cnpgv1.Cluster
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, &got))
	assert.Contains(t, got.Spec.Certificates.ServerAltDNSNames, "existing.example")
	assert.Contains(t, got.Spec.Certificates.ServerAltDNSNames, "pg1-pooler-rw.default"+poolerSANSuffix)
	assert.Contains(t, got.Spec.Certificates.ServerAltDNSNames, "pg1-pooler-ro.default"+poolerSANSuffix)

	// Idempotency: the second call must NOT patch the object. Probe via the
	// fake client's ResourceVersion, which the fake bumps only on writes.
	rvBefore := got.ResourceVersion
	require.NoError(t, model.EnsureSANPolicy(context.Background()))
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, &got))
	assert.Equal(t, rvBefore, got.ResourceVersion, "second EnsureSANPolicy call must be a no-op (no patch)")
}

func TestClusterModelSANPolicyPoolerDisabledRetainsPoolerSANs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	rwShort := "pg1-pooler-rw.default"
	roShort := "pg1-pooler-ro.default"
	initial := []string{
		"existing.example",
		rwShort,
		rwShort + poolerSANSuffix,
		roShort,
		roShort + poolerSANSuffix,
	}
	// Desired SAN list is always sorted in reconcile; spec order must match for DeepEqual(no-op).
	specSANs := append([]string(nil), initial...)
	sort.Strings(specSANs)
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: specSANs,
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()

	model := &clusterModel{
		client: c,
		cluster: &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		},
		mergedConfig: &MergedConfig{
			Spec: &enterprisev4.PostgresClusterSpec{
				ConnectionPoolerEnabled: ptr.To(false),
			},
		},
	}
	require.NoError(t, model.EnsureSANPolicy(context.Background()))

	var got cnpgv1.Cluster
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, &got))
	assert.ElementsMatch(t, initial, got.Spec.Certificates.ServerAltDNSNames, "pooler disabled must not patch SANs away")

	converged, err := model.IsSANPolicyConverged(context.Background())
	require.NoError(t, err)
	assert.True(t, converged)
}

func TestClusterModelSANPolicyPoolerDisabledDoesNotInjectPoolerSANsWhenAbsent(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: []string{"only-custom.example"},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()

	model := &clusterModel{
		client: c,
		cluster: &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		},
		mergedConfig: &MergedConfig{
			Spec: &enterprisev4.PostgresClusterSpec{
				ConnectionPoolerEnabled: ptr.To(false),
			},
		},
	}
	require.NoError(t, model.EnsureSANPolicy(context.Background()))

	var got cnpgv1.Cluster
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, &got))
	assert.Equal(t, []string{"only-custom.example"}, got.Spec.Certificates.ServerAltDNSNames,
		"pooler disabled must not add pooler DNS names when they are absent from spec")

	converged, err := model.IsSANPolicyConverged(context.Background())
	require.NoError(t, err)
	assert.True(t, converged)

	for _, s := range got.Spec.Certificates.ServerAltDNSNames {
		assert.NotContains(t, s, "pooler-rw")
		assert.NotContains(t, s, "pooler-ro")
	}
}

func TestClusterModelIsSANPolicyConvergedNilVsEmptyServerAltDNSNames(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: nil,
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()

	model := &clusterModel{
		client: c,
		cluster: &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		},
		mergedConfig: &MergedConfig{
			Spec: &enterprisev4.PostgresClusterSpec{
				ConnectionPoolerEnabled: ptr.To(false),
			},
		},
	}
	require.NoError(t, model.EnsureSANPolicy(context.Background()))

	converged, err := model.IsSANPolicyConverged(context.Background())
	require.NoError(t, err)
	assert.True(t, converged)
}

func TestClusterModelSANPolicyPoolerEnabledAddsShortAndFQDNPoolerSANs(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: []string{"static.example"},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()

	model := &clusterModel{
		client: c,
		cluster: &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		},
		mergedConfig: &MergedConfig{
			Spec: &enterprisev4.PostgresClusterSpec{
				ConnectionPoolerEnabled: ptr.To(true),
			},
		},
	}
	require.NoError(t, model.EnsureSANPolicy(context.Background()))

	var got cnpgv1.Cluster
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, &got))
	ns := "default"
	rwShort := "pg1-pooler-rw." + ns
	roShort := "pg1-pooler-ro." + ns
	for _, want := range []string{
		rwShort,
		rwShort + poolerSANSuffix,
		roShort,
		roShort + poolerSANSuffix,
	} {
		assert.Contains(t, got.Spec.Certificates.ServerAltDNSNames, want)
	}
}

func TestClusterModelIsSANPolicyConvergedPoolerEnabledDetectsDrift(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: []string{"static.example", "pg1-pooler-rw.default"},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()

	model := &clusterModel{
		client: c,
		cluster: &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		},
		mergedConfig: &MergedConfig{
			Spec: &enterprisev4.PostgresClusterSpec{
				ConnectionPoolerEnabled: ptr.To(true),
			},
		},
	}
	ok, err := model.IsSANPolicyConverged(context.Background())
	require.NoError(t, err)
	assert.False(t, ok, "missing RO / fqdn pooler SANs must not converge")

	require.NoError(t, model.EnsureSANPolicy(context.Background()))

	ok, err = model.IsSANPolicyConverged(context.Background())
	require.NoError(t, err)
	assert.True(t, ok, "EnsureSANPolicy must have patched the missing pooler SANs")
}

// When the pooler is disabled, EnsureSANPolicy MUST be a strict no-op: no
// CNPG Get, no patch, no normalization. Cosmetic sort-order drift introduced
// by external edits (e.g., kubectl edit reordering ServerAltDNSNames) is
// intentionally NOT canonicalized in this state — the microseconds saved per
// reconcile outweigh the defense, because CNPG generates the server cert from
// the SAN list as a set, so order alone does not trigger rotation. Any
// structural drift is re-canonicalized when pooler is re-enabled.
func TestClusterModelSANPolicyPoolerDisabledIsStrictNoOp(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))

	unsorted := []string{"zebra.internal", "alpha.internal"}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: append([]string(nil), unsorted...),
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg).Build()

	model := &clusterModel{
		client: c,
		cluster: &enterprisev4.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		},
		mergedConfig: &MergedConfig{
			Spec: &enterprisev4.PostgresClusterSpec{
				ConnectionPoolerEnabled: ptr.To(false),
			},
		},
	}

	// Convergence is trivially true when pooler is disabled — there is no
	// desired SAN policy to converge to.
	ok, err := model.IsSANPolicyConverged(context.Background())
	require.NoError(t, err)
	assert.True(t, ok, "IsSANPolicyConverged must return true unconditionally when pooler is disabled")

	// EnsureSANPolicy MUST short-circuit on the pooler-disabled fast-path: no
	// patch is issued, the unsorted spec is preserved verbatim. Probe both via
	// ResourceVersion (write-detector) and the slice content itself.
	var preCNPG cnpgv1.Cluster
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, &preCNPG))
	rvBefore := preCNPG.ResourceVersion

	require.NoError(t, model.EnsureSANPolicy(context.Background()))

	var got cnpgv1.Cluster
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "pg1", Namespace: "default"}, &got))
	assert.Equal(t, rvBefore, got.ResourceVersion, "EnsureSANPolicy must be a strict no-op when pooler is disabled — no patch")
	assert.Equal(t, unsorted, got.Spec.Certificates.ServerAltDNSNames, "the unsorted spec must be preserved verbatim; pooler-disabled does not normalize")

	// And convergence stays trivially true regardless of the unsorted state.
	ok, err = model.IsSANPolicyConverged(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
}

// Drives the ClusterRuntimeProbe.IsServerTLSLeafAlignedWithSpec method against
// a fake client and locks the PEM/parse soft-fail contract.
func TestClusterModelIsServerTLSLeafAlignedWithSpec(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, cnpgv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))

	// Minimum shape the method reads: client + cluster Name/Namespace.
	makeModel := func(c client.Client, name string) *clusterModel {
		return &clusterModel{
			client: c,
			cluster: &enterprisev4.PostgresCluster{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			},
		}
	}

	wantSANs := []string{"pg1-rw.default.svc.cluster.local", "pg1-pooler-rw.default.svc.cluster.local"}
	cnpg := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "default"},
		Spec: cnpgv1.ClusterSpec{
			Certificates: &cnpgv1.CertificatesConfiguration{
				ServerAltDNSNames: wantSANs,
			},
		},
		Status: cnpgv1.ClusterStatus{
			Certificates: cnpgv1.CertificatesStatus{
				CertificatesConfiguration: cnpgv1.CertificatesConfiguration{
					ServerTLSSecret: "pg1-server-tls",
				},
			},
		},
	}

	t.Run("missing_secret", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg.DeepCopy()).Build()
		ok, err := makeModel(c, "pg1").IsServerTLSLeafAlignedWithSpec(context.Background())
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("leaf_missing_dns", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-tls", Namespace: "default"},
			Data: map[string][]byte{
				corev1.TLSCertKey: testSelfSignedLeafCertPEM(t, []string{"pg1-rw.default.svc.cluster.local"}),
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg.DeepCopy(), sec).Build()
		ok, err := makeModel(c, "pg1").IsServerTLSLeafAlignedWithSpec(context.Background())
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("leaf_aligned", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-tls", Namespace: "default"},
			Data: map[string][]byte{
				corev1.TLSCertKey: testSelfSignedLeafCertPEM(t, wantSANs),
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg.DeepCopy(), sec).Build()
		ok, err := makeModel(c, "pg1").IsServerTLSLeafAlignedWithSpec(context.Background())
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("empty_spec_sans_skips_secret", func(t *testing.T) {
		emptySAN := &cnpgv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg2", Namespace: "default"},
			Spec:       cnpgv1.ClusterSpec{},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(emptySAN).Build()
		ok, err := makeModel(c, "pg2").IsServerTLSLeafAlignedWithSpec(context.Background())
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("cnpg_cluster_not_found_short_circuits_true", func(t *testing.T) {
		// Missing CNPG Cluster must not gate pooler readiness behind a leaf check.
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		ok, err := makeModel(c, "pg-absent").IsServerTLSLeafAlignedWithSpec(context.Background())
		require.NoError(t, err)
		assert.True(t, ok)
	})

	// PEM/parse failures are structural, not transient: caller demuxes the
	// sentinel to surface reasonPoolerTLSLeafInvalidCert (Failed) instead of
	// looping forever on PoolerTLSLeafPending. Detail stays in the wrapped
	// error for logs; the caller scrubs it out of events/Condition.Message.
	t.Run("malformed_pem_returns_sentinel", func(t *testing.T) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-tls", Namespace: "default"},
			Data:       map[string][]byte{corev1.TLSCertKey: []byte("this is not a PEM block")},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg.DeepCopy(), sec).Build()
		ok, err := makeModel(c, "pg1").IsServerTLSLeafAlignedWithSpec(context.Background())
		require.Error(t, err, "malformed PEM must escalate via the sentinel so callers can route to Failed")
		assert.True(t, errors.Is(err, errServerTLSLeafInvalid), "error must wrap errServerTLSLeafInvalid for errors.Is demux at the call site")
		assert.Contains(t, err.Error(), "PEM decode failed", "wrapped error must carry the sub-cause for logs")
		assert.Contains(t, err.Error(), "default/pg1-server-tls", "wrapped error must identify the offending Secret for log search")
		assert.False(t, ok)
	})

	t.Run("invalid_certificate_bytes_returns_sentinel", func(t *testing.T) {
		badDER := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage-not-asn1")})
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "pg1-server-tls", Namespace: "default"},
			Data:       map[string][]byte{corev1.TLSCertKey: badDER},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cnpg.DeepCopy(), sec).Build()
		ok, err := makeModel(c, "pg1").IsServerTLSLeafAlignedWithSpec(context.Background())
		require.Error(t, err, "x509.ParseCertificate failure must escalate via the sentinel")
		assert.True(t, errors.Is(err, errServerTLSLeafInvalid), "error must wrap errServerTLSLeafInvalid for errors.Is demux at the call site")
		assert.Contains(t, err.Error(), "x509 parse failed", "wrapped error must carry the sub-cause for logs")
		assert.Contains(t, err.Error(), "default/pg1-server-tls", "wrapped error must identify the offending Secret for log search")
		assert.False(t, ok)
	})
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

// Round-trip invariant for buildCNPGClusterSpec — guards against phantom drift
// from apiserver-side defaulting:
//
//	normalize(build(cfg)) == normalize(read_back(apply(build(cfg))))
//
// apply() is approximated by the kube-apiserver structural-schema defaulter
// run against cnpgClusterDefaultsContractYAML below. To extend the contract,
// add the field + default to that YAML and assert it in
// TestCNPGClusterDefaultsContract_HasExpectedDefaults.
// Rationale: docs/postgres/internal-cnpg-phantom-drift-kt.md (§7, §10).

// cnpgClusterDefaultsContractYAML is a minimal hand-authored CRD schema (not
// the vendored upstream CRD) that models only the spec defaults
// buildCNPGClusterSpec must mirror. Unmodelled fields pass through untouched.
const cnpgClusterDefaultsContractYAML = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clusters.postgresql.cnpg.io
spec:
  group: postgresql.cnpg.io
  scope: Namespaced
  names:
    kind: Cluster
    listKind: ClusterList
    plural: clusters
    singular: cluster
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                primaryUpdateMethod:
                  type: string
                  default: restart
`

// loadCNPGClusterStructuralSchema parses the contract YAML above and returns
// the structural schema the apiserver uses to default cnpgv1.Cluster objects
// — but scoped to the fields this operator promises to mirror. Cheap; call
// once per test and reuse.
func loadCNPGClusterStructuralSchema(t *testing.T) *structuralschema.Structural {
	t.Helper()

	var crd apiextv1.CustomResourceDefinition
	require.NoError(t,
		yaml.Unmarshal([]byte(cnpgClusterDefaultsContractYAML), &crd),
		"parse cnpgClusterDefaultsContractYAML",
	)

	var v1Schema *apiextv1.JSONSchemaProps
	for i := range crd.Spec.Versions {
		v := &crd.Spec.Versions[i]
		if v.Name == "v1" && v.Schema != nil {
			v1Schema = v.Schema.OpenAPIV3Schema
			break
		}
	}
	require.NotNil(t, v1Schema, "defaults contract YAML has no v1 schema")

	var internalSchema apiext.JSONSchemaProps
	require.NoError(t,
		apiextv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(v1Schema, &internalSchema, nil),
		"convert v1 -> internal JSONSchemaProps",
	)

	ss, err := structuralschema.NewStructural(&internalSchema)
	require.NoError(t, err, "build structural schema from defaults contract")
	return ss
}

// applyCRDDefaulting runs CRD structural-schema defaulting over obj (the same
// code path kube-apiserver runs on Create) and returns a fresh Cluster
// matching what a subsequent Get would return — restricted to contract fields.
func applyCRDDefaulting(t *testing.T, ss *structuralschema.Structural, obj *cnpgv1.Cluster) *cnpgv1.Cluster {
	t.Helper()

	raw, err := json.Marshal(obj)
	require.NoError(t, err, "marshal Cluster -> JSON")

	asMap := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &asMap), "unmarshal JSON -> map[string]any")

	structuraldefaulting.Default(asMap, ss)

	raw2, err := json.Marshal(asMap)
	require.NoError(t, err, "marshal defaulted map -> JSON")

	out := &cnpgv1.Cluster{}
	require.NoError(t, json.Unmarshal(raw2, out), "unmarshal defaulted JSON -> Cluster")
	return out
}

// roundTripFixture is a small builder for the MergedConfig shapes the table
// test exercises. Every case starts from the same minimal baseline so each row
// varies one axis at a time — that keeps a future diff pointing at the
// case-specific input, not at boilerplate.
type roundTripFixture struct {
	instances           int32
	postgresVersion     string
	storage             resource.Quantity
	resources           corev1.ResourceRequirements
	postgresqlConfig    map[string]string
	pgHBA               []string
	primaryUpdateMethod *string // nil = let the builder default it; non-nil = explicit override
	metricsEnabled      bool
}

func defaultRoundTripFixture() roundTripFixture {
	return roundTripFixture{
		instances:        3,
		postgresVersion:  "17",
		storage:          resource.MustParse("10Gi"),
		resources:        corev1.ResourceRequirements{},
		postgresqlConfig: map[string]string{},
		pgHBA:            []string{},
	}
}

func (f roundTripFixture) mergedConfig() *MergedConfig {
	cfg := &MergedConfig{
		Spec: &enterprisev4.PostgresClusterSpec{
			Instances:        ptr.To(f.instances),
			PostgresVersion:  ptr.To(f.postgresVersion),
			Storage:          ptr.To(f.storage),
			Resources:        f.resources.DeepCopy(),
			PostgreSQLConfig: f.postgresqlConfig,
			PgHBA:            f.pgHBA,
		},
	}
	if f.primaryUpdateMethod != nil {
		cfg.CNPG = &enterprisev4.CNPGConfig{PrimaryUpdateMethod: f.primaryUpdateMethod}
	}
	return cfg
}

// Asserts the round-trip invariant across every MergedConfig shape. A failure
// means a CRD-defaulted field projected by normalizeCNPGClusterSpec is no
// longer mirrored by buildCNPGClusterSpec — phantom drift.
func TestBuildCNPGClusterSpec_RoundTripUnderCRDDefaulting(t *testing.T) {
	t.Parallel()

	ss := loadCNPGClusterStructuralSchema(t)

	cases := []struct {
		name    string
		fixture roundTripFixture
	}{
		{
			name:    "default_no_overrides",
			fixture: defaultRoundTripFixture(),
		},
		{
			name: "primaryUpdateMethod_explicit_restart",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.primaryUpdateMethod = ptr.To("restart")
				return f
			}(),
		},
		{
			name: "primaryUpdateMethod_explicit_switchover",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.primaryUpdateMethod = ptr.To("switchover")
				return f
			}(),
		},
		{
			name: "with_custom_postgres_parameters",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.postgresqlConfig = map[string]string{
					"shared_buffers":  "256MB",
					"max_connections": "200",
				}
				return f
			}(),
		},
		{
			name: "with_pg_hba_rules",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.pgHBA = []string{
					"hostnossl all all 0.0.0.0/0 reject",
					"hostssl   all all 0.0.0.0/0 scram-sha-256",
				}
				return f
			}(),
		},
		{
			name: "with_resource_overrides",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				}
				return f
			}(),
		},
		{
			name: "with_postgres_metrics_enabled",
			fixture: func() roundTripFixture {
				f := defaultRoundTripFixture()
				f.metricsEnabled = true
				return f
			}(),
		},
		{
			name: "everything_set_together",
			fixture: roundTripFixture{
				instances:           5,
				postgresVersion:     "17",
				storage:             resource.MustParse("50Gi"),
				postgresqlConfig:    map[string]string{"shared_buffers": "256MB"},
				pgHBA:               []string{"hostssl all all 0.0.0.0/0 scram-sha-256"},
				resources:           corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}},
				primaryUpdateMethod: ptr.To("switchover"),
				metricsEnabled:      true,
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := tc.fixture.mergedConfig()
			desiredSpec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "test-secret", tc.fixture.metricsEnabled)

			// Wrap into a full Cluster so the structural defaulter walks the
			// real tree (TypeMeta + ObjectMeta + Spec), the same shape the
			// apiserver sees at admission.
			beforeRT := &cnpgv1.Cluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: cnpgv1.SchemeGroupVersion.String(),
					Kind:       cnpgv1.ClusterKind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "round-trip",
					Namespace: "default",
				},
				Spec: *desiredSpec.DeepCopy(),
			}
			afterRT := applyCRDDefaulting(t, ss, beforeRT)

			left := normalizeCNPGClusterSpec(desiredSpec, cfg.Spec.PostgreSQLConfig)
			right := normalizeCNPGClusterSpec(afterRT.Spec, cfg.Spec.PostgreSQLConfig)

			if !equality.Semantic.DeepEqual(left, right) {
				t.Fatalf(
					"phantom drift: normalized spec diverges across CRD-defaulting round-trip\n"+
						"--- LEFT  (build output, normalized)\n"+
						"+++ RIGHT (after CRD defaulting, normalized)\n"+
						"%s\n"+
						"This usually means buildCNPGClusterSpec left a field empty that the\n"+
						"CNPG CRD schema fills in via `default:` (kube-apiserver applies that\n"+
						"on Create). Mirror the default in buildCNPGClusterSpec, then keep the\n"+
						"override path. See docs/postgres/internal-cnpg-phantom-drift-kt.md.",
					cmp.Diff(left, right),
				)
			}
		})
	}
}

// TestBuildCNPGClusterSpec_RoundTrip_NegativeControl proves the round-trip has
// teeth: bypassing the phantom-drift fix (PrimaryUpdateMethod="") MUST detect
// drift. A passing test implies either the contract YAML lost its
// `default: restart` marker or the defaulter no longer applies it.
func TestBuildCNPGClusterSpec_RoundTrip_NegativeControl(t *testing.T) {
	t.Parallel()

	ss := loadCNPGClusterStructuralSchema(t)

	cfg := defaultRoundTripFixture().mergedConfig()
	desiredSpec := buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "test-secret", false)
	desiredSpec.PrimaryUpdateMethod = "" // simulate pre-fix builder.

	beforeRT := &cnpgv1.Cluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: cnpgv1.SchemeGroupVersion.String(), Kind: cnpgv1.ClusterKind},
		ObjectMeta: metav1.ObjectMeta{Name: "round-trip-neg", Namespace: "default"},
		Spec:       *desiredSpec.DeepCopy(),
	}
	afterRT := applyCRDDefaulting(t, ss, beforeRT)

	left := normalizeCNPGClusterSpec(desiredSpec, cfg.Spec.PostgreSQLConfig)
	right := normalizeCNPGClusterSpec(afterRT.Spec, cfg.Spec.PostgreSQLConfig)

	require.Equal(t, "", left.PrimaryUpdateMethod, "precondition: desired-side primaryUpdateMethod must be empty")
	require.Equal(t, "restart", right.PrimaryUpdateMethod,
		"CRD-schema defaulting must materialize spec.primaryUpdateMethod=\"restart\"; "+
			"if this fails, cnpgClusterDefaultsContractYAML lost the default: restart marker for spec.primaryUpdateMethod")
	require.False(t,
		equality.Semantic.DeepEqual(left, right),
		"negative control: empty desired PrimaryUpdateMethod must round-trip to a different value, "+
			"otherwise the positive test above is asserting nothing",
	)
}

// TestCNPGClusterDefaultsContract_HasExpectedDefaults guards the contract
// YAML: every default the round-trip relies on must be modeled with the
// exact upstream value. Extend this test when extending the contract YAML.
func TestCNPGClusterDefaultsContract_HasExpectedDefaults(t *testing.T) {
	t.Parallel()

	ss := loadCNPGClusterStructuralSchema(t)

	specSchema, ok := ss.Properties["spec"]
	require.True(t, ok, "defaults contract: top-level spec property missing from cnpgClusterDefaultsContractYAML")

	updateMethodSchema, ok := specSchema.Properties["primaryUpdateMethod"]
	require.True(t, ok, "defaults contract: spec.primaryUpdateMethod missing from cnpgClusterDefaultsContractYAML")

	require.NotNil(t, updateMethodSchema.Default, "defaults contract: spec.primaryUpdateMethod has no default in cnpgClusterDefaultsContractYAML")
	assert.Equal(t, "restart", updateMethodSchema.Default.Object,
		"defaults contract: spec.primaryUpdateMethod default is not \"restart\"; "+
			"the contract YAML at the top of this section disagrees with the upstream CNPG default we rely on")
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
		Spec:       buildCNPGClusterSpec(cnpgv1.ClusterSpec{}, cfg, "pg1-secret", false),
	}
	events := &captureEventEmitter{}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(orphanedCNPG).Build()

	model := newClusterModel(c, scheme, events, nil, cluster, clusterClass, cfg, "pg1-secret")
	model.Actuate(context.Background())

	require.True(t, model.cnpgPatch.requiresPhaseGate(), "adoption must set cnpgPatched to requeue")
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
