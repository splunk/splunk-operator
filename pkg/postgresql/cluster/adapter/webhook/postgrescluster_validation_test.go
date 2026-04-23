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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/stretchr/testify/assert"
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
			errs := ValidatePostgresClusterCreate(tt.obj)
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
			errs := ValidatePostgresClusterUpdate(tt.obj, tt.oldObj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

func TestValidatePostgresClusterCreateFeatureGateDisabled(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	})

	obj := &enterpriseApi.PostgresCluster{
		Spec: enterpriseApi.PostgresClusterSpec{Class: "dev"},
	}

	errs := ValidatePostgresClusterCreate(obj)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
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
