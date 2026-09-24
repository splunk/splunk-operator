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

package webhook_test

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/webhook"
	"github.com/stretchr/testify/assert"
)

func TestValidatePostgresClusterClassCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.PostgresClusterClass
		wantErrCount int
		wantErrField string
	}{
		{
			name: "valid - no config",
			obj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid - config without pgHBA",
			obj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config:      &enterpriseApi.PostgresClusterClassConfig{},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid - correct pgHBA rules",
			obj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{
							"hostnossl all all 0.0.0.0/0 reject",
							"hostssl all all 0.0.0.0/0 scram-sha-256",
						},
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid - bad connection type",
			obj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{
							"hostx all all 0.0.0.0/0 md5",
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.config.pgHBA[0]",
		},
		{
			name: "invalid - bad CIDR in class",
			obj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{
							"host all all 256.1.1.1/24 md5",
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.config.pgHBA[0]",
		},
		{
			name: "invalid - unknown auth method in class",
			obj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{
							"host all all 0.0.0.0/0 bogus",
						},
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.config.pgHBA[0]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := webhook.ValidatePostgresClusterClassCreate(tt.obj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
			if tt.wantErrField != "" && len(errs) > 0 {
				assert.Equal(t, tt.wantErrField, errs[0].Field, "unexpected error field")
			}
		})
	}
}

func TestValidatePostgresClusterClassUpdate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.PostgresClusterClass
		oldObj       *enterpriseApi.PostgresClusterClass
		wantErrCount int
	}{
		{
			name: "valid update",
			obj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{"host all all 0.0.0.0/0 scram-sha-256"},
					},
				},
			},
			oldObj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - bad pgHBA",
			obj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{"host all all 0.0.0.0/0 fake-method"},
					},
				},
			},
			oldObj: &enterpriseApi.PostgresClusterClass{
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			wantErrCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := webhook.ValidatePostgresClusterClassUpdate(tt.obj, tt.oldObj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

func TestValidatePostgresClusterClassCreateFeatureGateDisabled(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	})

	obj := &enterpriseApi.PostgresClusterClass{
		Spec: enterpriseApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}

	errs := webhook.ValidatePostgresClusterClassCreate(obj)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestValidatePostgresClusterClassUpdateFeatureGateDisabled(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	})

	obj := &enterpriseApi.PostgresClusterClass{
		Spec: enterpriseApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}
	oldObj := obj.DeepCopy()

	errs := webhook.ValidatePostgresClusterClassUpdate(obj, oldObj)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestGetPostgresClusterClassWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.PostgresClusterClass{
		Spec: enterpriseApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}
	assert.Empty(t, webhook.GetPostgresClusterClassWarningsOnCreate(obj))
}

func TestGetPostgresClusterClassWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.PostgresClusterClass{
		Spec: enterpriseApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}
	oldObj := &enterpriseApi.PostgresClusterClass{
		Spec: enterpriseApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}
	assert.Empty(t, webhook.GetPostgresClusterClassWarningsOnUpdate(obj, oldObj))
}
