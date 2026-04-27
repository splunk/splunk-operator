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

package webhook

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePostgresClusterCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.PostgresCluster
		wantErrCount int
		wantErrField string
	}{
		{
			name: "valid - no pgHBA rules",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid - empty pgHBA",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid - correct pgHBA rules",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"hostnossl all all 0.0.0.0/0 reject",
						"hostssl all all 0.0.0.0/0 scram-sha-256",
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid - bad connection type",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"hostx all all 0.0.0.0/0 md5",
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.pgHBA[0]",
		},
		{
			name: "invalid - bad CIDR",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"host all all 192.168.0.0/33 md5",
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.pgHBA[0]",
		},
		{
			name: "invalid - bad auth method",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"host all all 0.0.0.0/0 bogus-auth",
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.pgHBA[0]",
		},
		{
			name: "invalid - missing fields",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"host all all",
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.pgHBA[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidatePostgresClusterCreate(tt.obj, nil)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
			if tt.wantErrField != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantErrField, errs[0].Field, "unexpected error field")
			}
		})
	}
}

func TestValidatePostgresClusterUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.PostgresCluster
		oldObj       *enterpriseApi.PostgresCluster
		wantErrCount int
	}{
		{
			name: "valid update - add pgHBA rules",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{"host all all 0.0.0.0/0 scram-sha-256"},
				},
			},
			oldObj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - bad pgHBA",
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{"hostx all all 0.0.0.0/0 md5"},
				},
			},
			oldObj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
				},
			},
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidatePostgresClusterUpdate(tt.obj, tt.oldObj, nil)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

func TestGetPostgresClusterWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.PostgresCluster{
		Spec: enterpriseApi.PostgresClusterSpec{Class: "dev"},
	}
	assert.Empty(t, GetPostgresClusterWarningsOnCreate(obj))
}

func TestGetPostgresClusterWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.PostgresCluster{
		Spec: enterpriseApi.PostgresClusterSpec{Class: "dev"},
	}
	oldObj := &enterpriseApi.PostgresCluster{
		Spec: enterpriseApi.PostgresClusterSpec{Class: "dev"},
	}
	assert.Empty(t, GetPostgresClusterWarningsOnUpdate(obj, oldObj))
}

func newFakeReader(objects ...runtime.Object) *fake.ClientBuilder {
	s := runtime.NewScheme()
	enterpriseApi.AddToScheme(s)
	b := fake.NewClientBuilder().WithScheme(s)
	for _, obj := range objects {
		b = b.WithRuntimeObjects(obj)
	}
	return b
}

func TestValidateAgainstClass(t *testing.T) {
	classWithDefaults := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:               ptr.To(int32(3)),
				Storage:                 ptr.To(resource.MustParse("50Gi")),
				PostgresVersion:         ptr.To("17"),
				ConnectionPoolerEnabled: ptr.To(false),
			},
		},
	}

	classWithMinorVersion := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "prod-pinned"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(3)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17.2"),
			},
		},
	}

	classWithPoolerEnabled := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pooler-class"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:               ptr.To(int32(3)),
				Storage:                 ptr.To(resource.MustParse("50Gi")),
				PostgresVersion:         ptr.To("17"),
				ConnectionPoolerEnabled: ptr.To(true),
			},
			CNPG: &enterpriseApi.CNPGConfig{
				ConnectionPooler: &enterpriseApi.ConnectionPoolerConfig{},
			},
		},
	}

	tests := []struct {
		name         string
		class        *enterpriseApi.PostgresClusterClass
		obj          *enterpriseApi.PostgresCluster
		wantErrCount int
		wantErrField string
	}{
		{
			name:  "class not found",
			class: nil,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "nonexistent"},
			},
			wantErrCount: 1,
			wantErrField: "spec.class",
		},
		{
			name:  "valid - no overrides",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "prod"},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - same postgres version",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("17"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - higher postgres version",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("18"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "invalid - lower postgres version",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("16"),
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.postgresVersion",
		},
		{
			name:  "valid - minor version ignored when class has major only",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("17.2"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - lower minor ignored when class has major only",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("17.0"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - cluster minor equal to class minor",
			class: classWithMinorVersion,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod-pinned",
					PostgresVersion: ptr.To("17.2"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - cluster minor higher than class minor",
			class: classWithMinorVersion,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod-pinned",
					PostgresVersion: ptr.To("17.5"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "invalid - cluster minor lower than class minor",
			class: classWithMinorVersion,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod-pinned",
					PostgresVersion: ptr.To("17.1"),
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.postgresVersion",
		},
		{
			name:  "invalid - cluster major lower even with higher minor",
			class: classWithMinorVersion,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod-pinned",
					PostgresVersion: ptr.To("16.9"),
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.postgresVersion",
		},
		{
			name:  "valid - cluster major higher than class with minor",
			class: classWithMinorVersion,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod-pinned",
					PostgresVersion: ptr.To("18"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "invalid - pooler enabled but class has no cnpg.connectionPooler",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:                   "prod",
					ConnectionPoolerEnabled: ptr.To(true),
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.connectionPoolerEnabled",
		},
		{
			name:  "valid - pooler disabled, class has no cnpg config",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:                   "prod",
					ConnectionPoolerEnabled: ptr.To(false),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - pooler enabled and class has cnpg.connectionPooler",
			class: classWithPoolerEnabled,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:                   "pooler-class",
					ConnectionPoolerEnabled: ptr.To(true),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - pooler unset (inherits class)",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "prod"},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid - class enables pooler but missing cnpg config",
			class: &enterpriseApi.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "pooler-no-cnpg"},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						Instances:               ptr.To(int32(3)),
						Storage:                 ptr.To(resource.MustParse("50Gi")),
						PostgresVersion:         ptr.To("17"),
						ConnectionPoolerEnabled: ptr.To(true),
					},
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "pooler-no-cnpg"},
			},
			wantErrCount: 1,
			wantErrField: "spec.connectionPoolerEnabled",
		},
		{
			name: "invalid - class has no config, cluster missing required fields",
			class: &enterpriseApi.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "bare"},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "bare"},
			},
			wantErrCount: 3,
			wantErrField: "spec.instances",
		},
		{
			name: "invalid - class config missing storage, cluster doesn't provide it",
			class: &enterpriseApi.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "no-storage"},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						Instances:       ptr.To(int32(3)),
						PostgresVersion: ptr.To("17"),
					},
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "no-storage"},
			},
			wantErrCount: 1,
			wantErrField: "spec.storage",
		},
		{
			name: "valid - cluster fills in what class is missing",
			class: &enterpriseApi.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "minimal"},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config:      &enterpriseApi.PostgresClusterClassConfig{},
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "minimal",
					Instances:       ptr.To(int32(1)),
					PostgresVersion: ptr.To("17"),
					Storage:         ptr.To(resource.MustParse("10Gi")),
				},
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := newFakeReader()
			if tt.class != nil {
				builder = newFakeReader(tt.class)
			}
			reader := builder.Build()

			errs := ValidatePostgresClusterCreate(tt.obj, reader)
			require.Len(t, errs, tt.wantErrCount, "unexpected error count: %v", errs)
			if tt.wantErrField != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantErrField, errs[0].Field, "unexpected error field")
			}
		})
	}
}
