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

	corev1 "k8s.io/api/core/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/postgresql/database/adapter/webhook"
	"github.com/stretchr/testify/assert"
)

func TestValidatePostgresDatabaseCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *enterpriseApi.PostgresDatabase
		wantErrCount int
	}{
		{
			name: "valid - minimal spec",
			obj: &enterpriseApi.PostgresDatabase{
				Spec: enterpriseApi.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
					Databases:  []enterpriseApi.DatabaseDefinition{{Name: "mydb"}},
				},
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := webhook.ValidatePostgresDatabaseCreate(tt.obj)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

func TestValidatePostgresDatabaseCreateFeatureGateDisabled(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	})

	obj := &enterpriseApi.PostgresDatabase{
		Spec: enterpriseApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []enterpriseApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}

	errs := webhook.ValidatePostgresDatabaseCreate(obj)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestValidatePostgresDatabaseUpdateFeatureGateDisabled(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	})

	obj := &enterpriseApi.PostgresDatabase{
		Spec: enterpriseApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []enterpriseApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}
	oldObj := obj.DeepCopy()

	errs := webhook.ValidatePostgresDatabaseUpdate(obj, oldObj)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestValidatePostgresDatabaseUpdate(t *testing.T) {
	obj := &enterpriseApi.PostgresDatabase{
		Spec: enterpriseApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []enterpriseApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}
	errs := webhook.ValidatePostgresDatabaseUpdate(obj, obj)
	assert.Empty(t, errs)
}

func TestGetPostgresDatabaseWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.PostgresDatabase{
		Spec: enterpriseApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []enterpriseApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}
	assert.Empty(t, webhook.GetPostgresDatabaseWarningsOnCreate(obj))
}

func TestGetPostgresDatabaseWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.PostgresDatabase{
		Spec: enterpriseApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []enterpriseApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}
	assert.Empty(t, webhook.GetPostgresDatabaseWarningsOnUpdate(obj, obj))
}
