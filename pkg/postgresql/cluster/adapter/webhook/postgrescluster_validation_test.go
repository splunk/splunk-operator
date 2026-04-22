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

func ptrBool(b bool) *bool       { return &b }
func ptrString(s string) *string { return &s }
func ptrInt32(i int32) *int32    { return &i }

func ptrQuantity(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}

func TestValidateAgainstClass(t *testing.T) {
	classWithDefaults := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:               ptrInt32(3),
				Storage:                 ptrQuantity("50Gi"),
				PostgresVersion:         ptrString("17"),
				ConnectionPoolerEnabled: ptrBool(false),
			},
		},
	}

	classWithPoolerEnabled := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "pooler-class"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:               ptrInt32(3),
				Storage:                 ptrQuantity("50Gi"),
				PostgresVersion:         ptrString("17"),
				ConnectionPoolerEnabled: ptrBool(true),
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
			name:  "valid - storage equal to class",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:   "prod",
					Storage: ptrQuantity("50Gi"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - storage higher than class",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:   "prod",
					Storage: ptrQuantity("100Gi"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "invalid - storage lower than class",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:   "prod",
					Storage: ptrQuantity("10Gi"),
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.storage",
		},
		{
			name:  "valid - same postgres version",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptrString("17"),
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
					PostgresVersion: ptrString("18"),
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
					PostgresVersion: ptrString("16"),
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.postgresVersion",
		},
		{
			name:  "valid - minor version higher",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptrString("17.2"),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "invalid - connection pooler enabled when class disables",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:                   "prod",
					ConnectionPoolerEnabled: ptrBool(true),
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.connectionPoolerEnabled",
		},
		{
			name:  "valid - connection pooler disabled when class disables",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:                   "prod",
					ConnectionPoolerEnabled: ptrBool(false),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - connection pooler enabled when class enables",
			class: classWithPoolerEnabled,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:                   "pooler-class",
					ConnectionPoolerEnabled: ptrBool(true),
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - connection pooler unset (inherits class)",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "prod"},
			},
			wantErrCount: 0,
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
						Instances:       ptrInt32(3),
						PostgresVersion: ptrString("17"),
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
					Instances:       ptrInt32(1),
					PostgresVersion: ptrString("17"),
					Storage:         ptrQuantity("10Gi"),
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
