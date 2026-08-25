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
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/postgresql/database/adapter/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePostgresDatabaseCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *platformApi.PostgresDatabase
		wantErrCount int
	}{
		{
			name: "valid - minimal spec",
			obj: &platformApi.PostgresDatabase{
				Spec: platformApi.PostgresDatabaseSpec{
					ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
					Databases:  []platformApi.DatabaseDefinition{{Name: "mydb"}},
				},
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := webhook.ValidatePostgresDatabaseCreate(context.Background(), tt.obj, nil)
			assert.Len(t, errs, tt.wantErrCount, "unexpected error count")
		})
	}
}

func TestValidatePostgresDatabaseCreateFeatureGateDisabled(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	})

	obj := &platformApi.PostgresDatabase{
		Spec: platformApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []platformApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}

	errs := webhook.ValidatePostgresDatabaseCreate(context.Background(), obj, nil)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestValidatePostgresDatabaseUpdateFeatureGateDisabled(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	})

	obj := &platformApi.PostgresDatabase{
		Spec: platformApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []platformApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}
	oldObj := obj.DeepCopy()

	errs := webhook.ValidatePostgresDatabaseUpdate(context.Background(), obj, oldObj, nil)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestValidatePostgresDatabaseUpdate(t *testing.T) {
	obj := &platformApi.PostgresDatabase{
		Spec: platformApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []platformApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}
	errs := webhook.ValidatePostgresDatabaseUpdate(context.Background(), obj, obj, nil)
	assert.Empty(t, errs)
}

// TestValidatePostgresDatabaseExternalSecret pins the admission-time policy for
func TestValidatePostgresDatabaseExternalSecret(t *testing.T) {
	const (
		ns        = "default"
		adminRef  = "db-admin-secret"
		rwRef     = "db-rw-secret"
		adminPath = "spec.databases[0].passwordConfig.externalAdminSecretRef.name"
		rwPath    = "spec.databases[0].passwordConfig.externalRWSecretRef.name"
	)

	dbWithRefs := func() *platformApi.PostgresDatabase {
		return &platformApi.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "pgdb", Namespace: ns},
			Spec: platformApi.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
				Databases: []platformApi.DatabaseDefinition{{
					Name: "mydb",
					PasswordConfig: &platformApi.PasswordConfig{
						ExternalAdminSecretRef: corev1.LocalObjectReference{Name: adminRef},
						ExternalRWSecretRef:    corev1.LocalObjectReference{Name: rwRef},
					},
				}},
			},
		}
	}

	secretWith := func(name string, data map[string][]byte, labels map[string]string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels},
			Data:       data,
		}
	}

	validData := map[string][]byte{"username": []byte("mydb_admin"), "password": []byte("s3cr3t")}
	reloadLabel := map[string]string{"cnpg.io/reload": "true"}

	tests := []struct {
		name       string
		secrets    []*corev1.Secret
		wantErrs   int
		wantFields []string
		wantDetail string
	}{
		{
			name:       "both secrets missing rejected (strict policy)",
			secrets:    nil,
			wantErrs:   2,
			wantFields: []string{adminPath, rwPath},
			wantDetail: "does not exist",
		},
		{
			name: "both secrets valid pass",
			secrets: []*corev1.Secret{
				secretWith(adminRef, validData, reloadLabel),
				secretWith(rwRef, validData, reloadLabel),
			},
			wantErrs: 0,
		},
		{
			name: "admin present but missing label, rw missing — both rejected",
			secrets: []*corev1.Secret{
				secretWith(adminRef, validData, nil),
			},
			wantErrs:   2,
			wantFields: []string{adminPath, rwPath},
			wantDetail: "cnpg.io/reload",
		},
		{
			name: "rw present but missing keys rejected, admin valid",
			secrets: []*corev1.Secret{
				secretWith(adminRef, validData, reloadLabel),
				secretWith(rwRef, map[string][]byte{"username": []byte("mydb_rw")}, reloadLabel),
			},
			wantErrs:   1,
			wantFields: []string{rwPath},
			wantDetail: "missing required keys",
		},
		{
			name: "both present and invalid rejected",
			secrets: []*corev1.Secret{
				secretWith(adminRef, nil, reloadLabel),
				secretWith(rwRef, validData, nil),
			},
			wantErrs:   2,
			wantFields: []string{adminPath, rwPath},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(s))
			b := fake.NewClientBuilder().WithScheme(s)
			for _, sec := range tt.secrets {
				b = b.WithRuntimeObjects(sec)
			}
			reader := b.Build()

			errs := webhook.ValidatePostgresDatabaseCreate(context.Background(), dbWithRefs(), reader)
			require.Len(t, errs, tt.wantErrs, "unexpected error count: %v", errs)
			for i, f := range tt.wantFields {
				assert.Equal(t, f, errs[i].Field, "unexpected error field at index %d", i)
			}
			if tt.wantDetail != "" && len(errs) > 0 {
				assert.Contains(t, errs[0].Detail, tt.wantDetail)
			}
		})
	}
}

func TestValidatePostgresDatabaseCustomMetrics(t *testing.T) {
	const ns = "default"

	dbWith := func(mon *platformApi.DatabaseMonitoring) *platformApi.PostgresDatabase {
		return &platformApi.PostgresDatabase{
			ObjectMeta: metav1.ObjectMeta{Name: "pgdb", Namespace: ns},
			Spec: platformApi.PostgresDatabaseSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
				Databases: []platformApi.DatabaseDefinition{
					{Name: "mydb", Monitoring: mon},
				},
			},
		}
	}

	sourceCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "metrics-src", Namespace: ns},
		Data:       map[string]string{"queries.yaml": ""},
	}

	buildReader := func(objs ...runtime.Object) client.Reader {
		s := runtime.NewScheme()
		require.NoError(t, corev1.AddToScheme(s))
		b := fake.NewClientBuilder().WithScheme(s)
		for _, o := range objs {
			b = b.WithRuntimeObjects(o)
		}
		return b.Build()
	}

	t.Run("nil monitoring - skipped", func(t *testing.T) {
		errs := webhook.ValidatePostgresDatabaseCreate(context.Background(), dbWith(nil), buildReader())
		assert.Empty(t, errs)
	})

	t.Run("existing source ConfigMap - admitted", func(t *testing.T) {
		obj := dbWith(&platformApi.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{LocalObjectReference: corev1.LocalObjectReference{Name: "metrics-src"}, Key: "queries.yaml"}},
		})
		errs := webhook.ValidatePostgresDatabaseCreate(context.Background(), obj, buildReader(sourceCM))
		assert.Empty(t, errs)
	})

	t.Run("missing source ConfigMap - rejected", func(t *testing.T) {
		obj := dbWith(&platformApi.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{LocalObjectReference: corev1.LocalObjectReference{Name: "absent"}, Key: "queries.yaml"}},
		})
		errs := webhook.ValidatePostgresDatabaseCreate(context.Background(), obj, buildReader())
		require.Len(t, errs, 1)
		assert.Equal(t, "spec.databases[0].monitoring.customQueriesConfigMap[0].name", errs[0].Field)
		assert.Contains(t, errs[0].Detail, "does not exist")
	})

	t.Run("deleting object skips missing live references", func(t *testing.T) {
		oldObj := dbWith(&platformApi.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{LocalObjectReference: corev1.LocalObjectReference{Name: "removed"}, Key: "queries.yaml"}},
		})
		obj := oldObj.DeepCopy()
		now := metav1.Now()
		obj.DeletionTimestamp = &now

		errs := webhook.ValidatePostgresDatabaseUpdate(context.Background(), obj, oldObj, buildReader())

		assert.Empty(t, errs)
	})

	t.Run("every explicit optional field is rejected", func(t *testing.T) {
		obj := dbWith(&platformApi.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{
				{LocalObjectReference: corev1.LocalObjectReference{Name: "metrics-src"}, Key: "queries.yaml", Optional: ptr.To(true)},
				{LocalObjectReference: corev1.LocalObjectReference{Name: "metrics-src"}, Key: "queries.yaml"},
				{LocalObjectReference: corev1.LocalObjectReference{Name: "metrics-src"}, Key: "queries.yaml", Optional: ptr.To(false)},
			},
		})
		errs := webhook.ValidatePostgresDatabaseCreate(context.Background(), obj, buildReader(sourceCM))

		require.Len(t, errs, 2)
		assert.Equal(t, "spec.databases[0].monitoring.customQueriesConfigMap[0].optional", errs[0].Field)
		assert.Equal(t, "spec.databases[0].monitoring.customQueriesConfigMap[2].optional", errs[1].Field)
		assert.Contains(t, errs[0].Detail, "omit the field")
		assert.Contains(t, errs[1].Detail, "omit the field")
		assert.Contains(t, errs[0].Detail, `ConfigMap "metrics-src"`)
	})

	t.Run("optional policy does not depend on API reader", func(t *testing.T) {
		obj := dbWith(&platformApi.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
				LocalObjectReference: corev1.LocalObjectReference{Name: "metrics-src"},
				Key:                  "queries.yaml",
				Optional:             ptr.To(true),
			}},
		})

		errs := webhook.ValidatePostgresDatabaseCreate(context.Background(), obj, nil)

		require.Len(t, errs, 1)
		assert.Equal(t, "spec.databases[0].monitoring.customQueriesConfigMap[0].optional", errs[0].Field)
	})

	t.Run("unchanged missing source does not block an unrelated update", func(t *testing.T) {
		oldObj := dbWith(&platformApi.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
				LocalObjectReference: corev1.LocalObjectReference{Name: "removed"},
				Key:                  "queries.yaml",
			}},
		})
		obj := oldObj.DeepCopy()
		obj.Labels = map[string]string{"unrelated": "change"}

		errs := webhook.ValidatePostgresDatabaseUpdate(t.Context(), obj, oldObj, buildReader())

		assert.Empty(t, errs)
	})

	t.Run("changed selector list validates every resulting reference", func(t *testing.T) {
		oldObj := dbWith(&platformApi.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{{
				LocalObjectReference: corev1.LocalObjectReference{Name: "metrics-src"},
				Key:                  "queries.yaml",
			}},
		})
		obj := dbWith(&platformApi.DatabaseMonitoring{
			CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{
				{LocalObjectReference: corev1.LocalObjectReference{Name: "metrics-src"}, Key: "queries.yaml"},
				{LocalObjectReference: corev1.LocalObjectReference{Name: "absent"}, Key: "queries.yaml"},
			},
		})

		errs := webhook.ValidatePostgresDatabaseUpdate(t.Context(), obj, oldObj, buildReader(sourceCM))

		require.Len(t, errs, 1)
		assert.Equal(t, "spec.databases[0].monitoring.customQueriesConfigMap[1].name", errs[0].Field)
	})

	t.Run("explicit optional remains rejected when selectors are otherwise unchanged", func(t *testing.T) {
		ref := corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "removed"},
			Key:                  "queries.yaml",
			Optional:             ptr.To(false),
		}
		oldObj := dbWith(&platformApi.DatabaseMonitoring{CustomQueriesConfigMap: []corev1.ConfigMapKeySelector{ref}})
		obj := oldObj.DeepCopy()

		errs := webhook.ValidatePostgresDatabaseUpdate(t.Context(), obj, oldObj, buildReader())

		require.Len(t, errs, 1)
		assert.Equal(t, "spec.databases[0].monitoring.customQueriesConfigMap[0].optional", errs[0].Field)
	})
}

func TestGetPostgresDatabaseWarningsOnCreate(t *testing.T) {
	obj := &platformApi.PostgresDatabase{
		Spec: platformApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []platformApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}
	assert.Empty(t, webhook.GetPostgresDatabaseWarningsOnCreate(obj))
}

func TestGetPostgresDatabaseWarningsOnUpdate(t *testing.T) {
	obj := &platformApi.PostgresDatabase{
		Spec: platformApi.PostgresDatabaseSpec{
			ClusterRef: corev1.LocalObjectReference{Name: "my-cluster"},
			Databases:  []platformApi.DatabaseDefinition{{Name: "mydb"}},
		},
	}
	assert.Empty(t, webhook.GetPostgresDatabaseWarningsOnUpdate(obj, obj))
}
