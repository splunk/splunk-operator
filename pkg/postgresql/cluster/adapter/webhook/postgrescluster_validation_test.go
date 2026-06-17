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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/webhook"
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
			errs := webhook.ValidatePostgresClusterCreate(context.Background(), tt.obj, nil)
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
			errs := webhook.ValidatePostgresClusterUpdate(context.Background(), tt.obj, tt.oldObj, nil)
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

	errs := webhook.ValidatePostgresClusterCreate(context.Background(), obj, nil)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestValidatePostgresClusterUpdateFeatureGateDisabled(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): false})
	t.Cleanup(func() {
		config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	})

	obj := &enterpriseApi.PostgresCluster{
		Spec: enterpriseApi.PostgresClusterSpec{Class: "dev"},
	}
	oldObj := obj.DeepCopy()

	errs := webhook.ValidatePostgresClusterUpdate(context.Background(), obj, oldObj, nil)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestValidatePostgresClusterUpdateDeletedClass(t *testing.T) {
	reader := newFakeReader().Build()

	t.Run("allowed - spec unchanged (metadata-only update)", func(t *testing.T) {
		oldObj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class:           "deleted-class",
				PostgresVersion: ptr.To("16"),
			},
		}
		newObj := oldObj.DeepCopy()

		errs := webhook.ValidatePostgresClusterUpdate(context.Background(), newObj, oldObj, reader)
		assert.Empty(t, errs)
	})

	t.Run("rejected - spec.class changed to nonexistent", func(t *testing.T) {
		oldObj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{Class: "old-class"},
		}
		newObj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{Class: "new-class"},
		}

		errs := webhook.ValidatePostgresClusterUpdate(context.Background(), newObj, oldObj, reader)
		require.NotEmpty(t, errs)
		assert.Equal(t, "spec.class", errs[0].Field)
		assert.Contains(t, errs[0].Detail, "referenced PostgresClusterClass not found")
	})

	t.Run("rejected - spec fields changed with deleted class", func(t *testing.T) {
		oldObj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class:           "deleted-class",
				PostgresVersion: ptr.To("17"),
			},
		}
		newObj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class:           "deleted-class",
				PostgresVersion: ptr.To("15"),
			},
		}

		errs := webhook.ValidatePostgresClusterUpdate(context.Background(), newObj, oldObj, reader)
		require.NotEmpty(t, errs)
		assert.Equal(t, "spec.class", errs[0].Field)
		assert.Contains(t, errs[0].Detail, "referenced PostgresClusterClass not found")
	})
}

// TestValidatePostgresClusterExternalSecret pins the admission-time policy for
func TestValidatePostgresClusterExternalSecret(t *testing.T) {
	const (
		ns        = "default"
		secretRef = "external-superuser"
		refField  = "spec.passwordConfig.superuserExternalSecretRef.name"
	)

	validClass := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "dev"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:        ptr.To(int32(3)),
				Storage:          ptr.To(resource.MustParse("50Gi")),
				PostgresVersion:  ptr.To("17"),
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{Enabled: ptr.To(false)},
			},
		},
	}

	clusterWithRef := func() *enterpriseApi.PostgresCluster {
		return &enterpriseApi.PostgresCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: ns},
			Spec: enterpriseApi.PostgresClusterSpec{
				Class: "dev",
				PasswordConfig: &enterpriseApi.SuperuserPasswordConfig{
					SuperuserExternalSecretRef: corev1.LocalObjectReference{Name: secretRef},
				},
			},
		}
	}

	secretWith := func(data map[string][]byte, labels map[string]string) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretRef, Namespace: ns, Labels: labels},
			Data:       data,
		}
	}

	validData := map[string][]byte{"username": []byte("postgres"), "password": []byte("s3cr3t")}
	reloadLabel := map[string]string{"cnpg.io/reload": "true"}

	tests := []struct {
		name       string
		secret     *corev1.Secret
		wantErr    bool
		wantDetail string
	}{
		{
			name:       "missing secret rejected (strict policy)",
			secret:     nil,
			wantErr:    true,
			wantDetail: "does not exist",
		},
		{
			name:    "valid secret passes",
			secret:  secretWith(validData, reloadLabel),
			wantErr: false,
		},
		{
			name:       "present but empty data rejected",
			secret:     secretWith(nil, reloadLabel),
			wantErr:    true,
			wantDetail: "External superuser secret is invalid",
		},
		{
			name:       "present but missing password key rejected",
			secret:     secretWith(map[string][]byte{"username": []byte("postgres")}, reloadLabel),
			wantErr:    true,
			wantDetail: "External superuser secret is invalid",
		},
		{
			name:       "present but wrong username rejected",
			secret:     secretWith(map[string][]byte{"username": []byte("admin"), "password": []byte("x")}, reloadLabel),
			wantErr:    true,
			wantDetail: "External superuser secret username is invalid",
		},
		{
			name:       "present but missing reload label rejected",
			secret:     secretWith(validData, nil),
			wantErr:    true,
			wantDetail: "cnpg.io/reload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := []runtime.Object{validClass}
			if tt.secret != nil {
				objs = append(objs, tt.secret)
			}
			reader := newFakeReader(objs...).Build()

			errs := webhook.ValidatePostgresClusterCreate(context.Background(), clusterWithRef(), reader)

			if !tt.wantErr {
				assert.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1)
			assert.Equal(t, refField, errs[0].Field)
			assert.Contains(t, errs[0].Detail, tt.wantDetail)
		})
	}
}

func TestValidatePostgresClusterStorageUpdate(t *testing.T) {
	class := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(3)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17"),
			},
		},
	}
	reader := newFakeReader(class).Build()

	tests := []struct {
		name         string
		oldStorage   *resource.Quantity
		newStorage   *resource.Quantity
		wantErrCount int
		wantErrMsg   string
	}{
		{
			name:         "reject inherited class storage decrease",
			newStorage:   ptr.To(resource.MustParse("10Gi")),
			wantErrCount: 1,
			wantErrMsg:   "storage size cannot be decreased (from: 50Gi, to: 10Gi)",
		},
		{
			name:       "allow inherited class storage increase",
			newStorage: ptr.To(resource.MustParse("100Gi")),
		},
		{
			name:       "allow equal storage with different quantity spelling",
			oldStorage: ptr.To(resource.MustParse("50Gi")),
			newStorage: ptr.To(resource.MustParse("51200Mi")),
		},
		{
			name:         "reject explicit storage removal when class default is smaller",
			oldStorage:   ptr.To(resource.MustParse("100Gi")),
			wantErrCount: 1,
			wantErrMsg:   "storage size cannot be decreased (from: 100Gi, to: 50Gi)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldObj := &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:   "prod",
					Storage: tt.oldStorage,
				},
			}
			newObj := &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:   "prod",
					Storage: tt.newStorage,
				},
			}

			errs := webhook.ValidatePostgresClusterUpdate(t.Context(), newObj, oldObj, reader)
			require.Len(t, errs, tt.wantErrCount, "unexpected error count: %v", errs)
			if tt.wantErrCount > 0 {
				assert.Equal(t, "spec.storage", errs[0].Field)
				assert.Contains(t, errs[0].Detail, tt.wantErrMsg)
			}
		})
	}
}

func TestValidatePostgresClusterScaling(t *testing.T) {
	switchoverClass := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "switchover-class"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(3)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17"),
			},
			CNPG: &enterpriseApi.CNPGConfig{
				PrimaryUpdateMethod: ptr.To("switchover"),
			},
		},
	}
	restartClass := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "restart-class"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(1)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17"),
			},
			CNPG: &enterpriseApi.CNPGConfig{
				PrimaryUpdateMethod: ptr.To("restart"),
			},
		},
	}
	reader := newFakeReader(switchoverClass, restartClass).Build()
	readyPhase := "Ready"
	failedPhase := "Failed"

	makeCluster := func(className string, instances int32, phase *string) *enterpriseApi.PostgresCluster {
		c := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class:     className,
				Instances: ptr.To(instances),
			},
		}
		if phase != nil {
			c.Status.Phase = phase
		}
		return c
	}

	t.Run("create: switchover with 1 instance rejected", func(t *testing.T) {
		obj := makeCluster("switchover-class", 1, nil)
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		require.NotEmpty(t, errs)
		assert.Equal(t, "spec.instances", errs[0].Field)
		assert.Contains(t, errs[0].Detail, "switchover")
	})

	t.Run("create: switchover with 2 instances allowed", func(t *testing.T) {
		obj := makeCluster("switchover-class", 2, nil)
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		assert.Empty(t, errs)
	})

	t.Run("create: restart class with 1 instance allowed", func(t *testing.T) {
		obj := makeCluster("restart-class", 1, nil)
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		assert.Empty(t, errs)
	})

	t.Run("create: switchover, instances unset (inherits class default 3) allowed", func(t *testing.T) {
		obj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{Class: "switchover-class"},
		}
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		assert.Empty(t, errs)
	})

	t.Run("update: cluster-level override removed, falls back to class scalar default below switchover floor", func(t *testing.T) {
		oneClass := &enterpriseApi.PostgresClusterClass{
			ObjectMeta: metav1.ObjectMeta{Name: "switchover-one"},
			Spec: enterpriseApi.PostgresClusterClassSpec{
				Provisioner: "postgresql.cnpg.io",
				Config: &enterpriseApi.PostgresClusterClassConfig{
					Instances:       ptr.To(int32(1)),
					Storage:         ptr.To(resource.MustParse("50Gi")),
					PostgresVersion: ptr.To("17"),
				},
				CNPG: &enterpriseApi.CNPGConfig{PrimaryUpdateMethod: ptr.To("switchover")},
			},
		}
		r := newFakeReader(oneClass).Build()
		oldObj := makeCluster("switchover-one", 2, &readyPhase)
		newObj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{Class: "switchover-one"},
		}
		newObj.Status.Phase = &readyPhase
		errs := webhook.ValidatePostgresClusterUpdate(t.Context(), newObj, oldObj, r)
		require.NotEmpty(t, errs)
		assert.Equal(t, "spec.instances", errs[0].Field)
	})

	t.Run("update: switchover scale-down 2->1 while Ready rejected", func(t *testing.T) {
		oldObj := makeCluster("switchover-class", 2, &readyPhase)
		newObj := makeCluster("switchover-class", 1, &readyPhase)
		errs := webhook.ValidatePostgresClusterUpdate(t.Context(), newObj, oldObj, reader)
		require.NotEmpty(t, errs)
		assert.Equal(t, "spec.instances", errs[0].Field)
		assert.Contains(t, errs[0].Detail, "switchover")
	})

	t.Run("update: editing pre-existing 1-instance switchover cluster (no instance change) rejected", func(t *testing.T) {
		oldObj := makeCluster("switchover-class", 1, &readyPhase)
		newObj := makeCluster("switchover-class", 1, &readyPhase)
		newObj.Spec.PgHBA = []string{"host all all 0.0.0.0/0 scram-sha-256"}
		errs := webhook.ValidatePostgresClusterUpdate(t.Context(), newObj, oldObj, reader)
		require.NotEmpty(t, errs)
		assert.Equal(t, "spec.instances", errs[0].Field)
		assert.Contains(t, errs[0].Detail, "switchover")
	})

	t.Run("update: bump 1->2 on switchover class allowed regardless of phase", func(t *testing.T) {
		oldObj := makeCluster("switchover-class", 1, &failedPhase)
		newObj := makeCluster("switchover-class", 2, &failedPhase)
		errs := webhook.ValidatePostgresClusterUpdate(t.Context(), newObj, oldObj, reader)
		assert.Empty(t, errs)
	})

	t.Run("update: mid-flight retarget 3->4 while Provisioning allowed (level-based reconciliation)", func(t *testing.T) {
		provisioningPhase := "Provisioning"
		oldObj := makeCluster("restart-class", 3, &provisioningPhase)
		oldObj.Status.Instances = ptr.To(int32(2))
		oldObj.Status.ReadyInstances = ptr.To(int32(2))
		newObj := makeCluster("restart-class", 4, &provisioningPhase)
		errs := webhook.ValidatePostgresClusterUpdate(t.Context(), newObj, oldObj, reader)
		assert.Empty(t, errs)
	})
}

// TestValidatePoolerEndpoints exercises the admission webhook check that
// rejects connectionPooler.readOnly=true when the effective instance count
// is below the RO threshold (2). The check applies to both CREATE and UPDATE
// since validateAgainstClass runs on both paths.
func TestValidatePoolerEndpoints(t *testing.T) {
	classOneInstance := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "single"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(1)),
				Storage:         ptr.To(resource.MustParse("10Gi")),
				PostgresVersion: ptr.To("17"),
			},
			CNPG: &enterpriseApi.CNPGConfig{
				PrimaryUpdateMethod: ptr.To("restart"),
				ConnectionPooler:    &enterpriseApi.ConnectionPoolerConfig{},
			},
		},
	}
	classTwoInstances := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "ha"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(2)),
				Storage:         ptr.To(resource.MustParse("10Gi")),
				PostgresVersion: ptr.To("17"),
			},
			CNPG: &enterpriseApi.CNPGConfig{
				PrimaryUpdateMethod: ptr.To("switchover"),
				ConnectionPooler:    &enterpriseApi.ConnectionPoolerConfig{},
			},
		},
	}
	reader := newFakeReader(classOneInstance, classTwoInstances).Build()

	t.Run("create: readOnly=true with effective instances=1 rejected", func(t *testing.T) {
		obj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class: "single",
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
					Enabled:  ptr.To(true),
					ReadOnly: ptr.To(true),
				},
			},
		}
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		require.NotEmpty(t, errs)
		var found bool
		for _, e := range errs {
			if e.Field == "spec.connectionPooler.readOnly" {
				assert.Contains(t, e.Detail, "requires >= 2")
				found = true
			}
		}
		assert.True(t, found, "expected readOnly endpoint validation error")
	})

	t.Run("create: readOnly=false with effective instances=1 accepted", func(t *testing.T) {
		obj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class: "single",
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
					Enabled:  ptr.To(true),
					ReadOnly: ptr.To(false),
				},
			},
		}
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		for _, e := range errs {
			assert.NotEqual(t, "spec.connectionPooler.readOnly", e.Field, "should not flag readOnly when explicitly opted out")
		}
	})

	t.Run("create: readOnly=true with effective instances=2 accepted", func(t *testing.T) {
		obj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class: "ha",
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
					Enabled:  ptr.To(true),
					ReadOnly: ptr.To(true),
				},
			},
		}
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		for _, e := range errs {
			assert.NotEqual(t, "spec.connectionPooler.readOnly", e.Field)
		}
	})

	t.Run("create: pooler disabled, no readOnly check fires", func(t *testing.T) {
		obj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class: "single",
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
					Enabled: ptr.To(false),
				},
			},
		}
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		for _, e := range errs {
			assert.NotEqual(t, "spec.connectionPooler.readOnly", e.Field)
		}
	})

	t.Run("update: readOnly=true with effective instances=1 rejected", func(t *testing.T) {
		readyPhase := "Ready"
		oldObj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class: "single",
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
					Enabled:  ptr.To(true),
					ReadOnly: ptr.To(false),
				},
			},
		}
		oldObj.Status.Phase = &readyPhase
		newObj := oldObj.DeepCopy()
		newObj.Spec.ConnectionPooler.ReadOnly = ptr.To(true)
		errs := webhook.ValidatePostgresClusterUpdate(t.Context(), newObj, oldObj, reader)
		require.NotEmpty(t, errs)
		var found bool
		for _, e := range errs {
			if e.Field == "spec.connectionPooler.readOnly" {
				found = true
			}
		}
		assert.True(t, found, "update path must also reject readOnly=true at instances=1")
	})

	t.Run("create: readOnly unset with effective instances=1 rejected (nil treated as opted-in)", func(t *testing.T) {
		// readOnly carries no CRD default (it would break per-field class inheritance),
		// so an enabled pooler with readOnly unset arrives nil. The webhook treats nil
		// as opted-in, matching the reconciler's poolerReadOnlyWanted.
		obj := &enterpriseApi.PostgresCluster{
			Spec: enterpriseApi.PostgresClusterSpec{
				Class: "single",
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
					Enabled: ptr.To(true),
				},
			},
		}
		errs := webhook.ValidatePostgresClusterCreate(t.Context(), obj, reader)
		require.NotEmpty(t, errs)
		var found bool
		for _, e := range errs {
			if e.Field == "spec.connectionPooler.readOnly" {
				found = true
			}
		}
		assert.True(t, found, "nil readOnly should be treated as opted-in by the webhook")
	})
}

func TestGetPostgresClusterWarningsOnCreate(t *testing.T) {
	obj := &enterpriseApi.PostgresCluster{
		Spec: enterpriseApi.PostgresClusterSpec{Class: "dev"},
	}
	assert.Empty(t, webhook.GetPostgresClusterWarningsOnCreate(obj))
}

func TestGetPostgresClusterWarningsOnUpdate(t *testing.T) {
	obj := &enterpriseApi.PostgresCluster{
		Spec: enterpriseApi.PostgresClusterSpec{Class: "dev"},
	}
	oldObj := &enterpriseApi.PostgresCluster{
		Spec: enterpriseApi.PostgresClusterSpec{Class: "dev"},
	}
	assert.Empty(t, webhook.GetPostgresClusterWarningsOnUpdate(obj, oldObj))
}

func TestValidateAgainstClass(t *testing.T) {
	classWithDefaults := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(3)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17"),
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
					Enabled: ptr.To(false),
				},
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
				Instances:       ptr.To(int32(3)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17"),
				ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
					Enabled: ptr.To(true),
				},
			},
			CNPG: &enterpriseApi.CNPGConfig{
				ConnectionPooler: &enterpriseApi.ConnectionPoolerConfig{},
			},
		},
	}

	classWithBackupEnabled := &enterpriseApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "backup-class"},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(3)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17"),
				Backup: &enterpriseApi.BackupConfig{
					Enabled:  ptr.To(true),
					Schedule: ptr.To("0 2 * * *"),
				},
			},
			CNPG: &enterpriseApi.CNPGConfig{
				Backup: &enterpriseApi.CNPGBackupConfig{
					VolumeSnapshot: &enterpriseApi.CNPGVolumeSnapshotConfig{},
				},
			},
		},
	}

	tests := []struct {
		name          string
		class         *enterpriseApi.PostgresClusterClass
		obj           *enterpriseApi.PostgresCluster
		wantErrCount  int
		wantErrFields []string
		wantErrMsgs   []string
		wantErrValues []any
	}{
		{
			name:  "class not found",
			class: nil,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "nonexistent"},
			},
			wantErrCount:  1,
			wantErrFields: []string{"spec.class"},
			wantErrMsgs:   []string{"referenced PostgresClusterClass not found"},
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
			wantErrCount:  1,
			wantErrFields: []string{"spec.postgresVersion"},
			wantErrMsgs:   []string{"postgresVersion cannot be lower than class default (17)"},
			wantErrValues: []any{"16"},
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
			wantErrCount:  1,
			wantErrFields: []string{"spec.postgresVersion"},
			wantErrMsgs:   []string{"postgresVersion cannot be lower than class default (17.2)"},
			wantErrValues: []any{"17.1"},
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
			wantErrCount:  1,
			wantErrFields: []string{"spec.postgresVersion"},
			wantErrMsgs:   []string{"postgresVersion cannot be lower than class default (17.2)"},
			wantErrValues: []any{"16.9"},
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
					Class: "prod",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(true),
					},
				},
			},
			wantErrCount:  1,
			wantErrFields: []string{"spec.connectionPooler.enabled"},
			wantErrMsgs:   []string{"connection pooler requires cnpg.connectionPooler configuration in PostgresClusterClass"},
			wantErrValues: []any{true},
		},
		{
			name:  "valid - pooler disabled, class has no cnpg config",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "prod",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(false),
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - pooler enabled and class has cnpg.connectionPooler",
			class: classWithPoolerEnabled,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "pooler-class",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(true),
					},
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
						Instances:       ptr.To(int32(3)),
						Storage:         ptr.To(resource.MustParse("50Gi")),
						PostgresVersion: ptr.To("17"),
						ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
							Enabled: ptr.To(true),
						},
					},
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{Class: "pooler-no-cnpg"},
			},
			wantErrCount:  1,
			wantErrFields: []string{"spec.connectionPooler.enabled"},
			wantErrMsgs:   []string{"connection pooler requires cnpg.connectionPooler configuration in PostgresClusterClass"},
		},
		{
			name: "invalid - pooler enabled against class with no config",
			class: &enterpriseApi.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "bare-pooler"},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "bare-pooler",
					Instances:       ptr.To(int32(1)),
					PostgresVersion: ptr.To("17"),
					Storage:         ptr.To(resource.MustParse("10Gi")),
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled:  ptr.To(true),
						ReadOnly: ptr.To(false),
					},
				},
			},
			wantErrCount:  1,
			wantErrFields: []string{"spec.connectionPooler.enabled"},
			wantErrMsgs:   []string{"connection pooler requires cnpg.connectionPooler configuration in PostgresClusterClass"},
		},
		{
			name:  "invalid - cluster enables backup but class has no cnpg.backup.volumeSnapshot",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "prod",
					Backup: &enterpriseApi.BackupConfig{
						Enabled:  ptr.To(true),
						Schedule: ptr.To("0 2 * * *"),
					},
				},
			},
			wantErrCount:  1,
			wantErrFields: []string{"spec.backup.enabled"},
			wantErrMsgs:   []string{"backup requires cnpg.backup.volumeSnapshot configuration in PostgresClusterClass"},
			wantErrValues: []any{true},
		},
		{
			name:  "valid - backup disabled, class has no cnpg.backup config",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "prod",
					Backup: &enterpriseApi.BackupConfig{
						Enabled: ptr.To(false),
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "valid - backup enabled and class has cnpg.backup.volumeSnapshot",
			class: classWithBackupEnabled,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "backup-class",
				},
			},
			wantErrCount: 0,
		},
		{
			name:  "invalid - backup enabled but no volumeSnapshot and no schedule",
			class: classWithDefaults,
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "prod",
					Backup: &enterpriseApi.BackupConfig{
						Enabled: ptr.To(true),
					},
				},
			},
			wantErrCount:  2,
			wantErrFields: []string{"spec.backup.schedule", "spec.backup.enabled"},
			wantErrMsgs:   []string{"backup.schedule is required when backup.enabled is true", "backup requires cnpg.backup.volumeSnapshot configuration in PostgresClusterClass"},
		},
		{
			name: "invalid - backup enabled, volumeSnapshot present, but no schedule anywhere",
			class: &enterpriseApi.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "backup-no-schedule"},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						Instances:       ptr.To(int32(3)),
						Storage:         ptr.To(resource.MustParse("50Gi")),
						PostgresVersion: ptr.To("17"),
					},
					CNPG: &enterpriseApi.CNPGConfig{
						Backup: &enterpriseApi.CNPGBackupConfig{
							VolumeSnapshot: &enterpriseApi.CNPGVolumeSnapshotConfig{},
						},
					},
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "backup-no-schedule",
					Backup: &enterpriseApi.BackupConfig{
						Enabled: ptr.To(true),
					},
				},
			},
			wantErrCount:  1,
			wantErrFields: []string{"spec.backup.schedule"},
			wantErrMsgs:   []string{"backup.schedule is required when backup.enabled is true"},
		},
		{
			name: "valid - class enables backup without schedule, cluster provides schedule",
			class: &enterpriseApi.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "backup-no-schedule-class"},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						Instances:       ptr.To(int32(3)),
						Storage:         ptr.To(resource.MustParse("50Gi")),
						PostgresVersion: ptr.To("17"),
						Backup: &enterpriseApi.BackupConfig{
							Enabled: ptr.To(true),
						},
					},
					CNPG: &enterpriseApi.CNPGConfig{
						Backup: &enterpriseApi.CNPGBackupConfig{
							VolumeSnapshot: &enterpriseApi.CNPGVolumeSnapshotConfig{},
						},
					},
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "backup-no-schedule-class",
					Backup: &enterpriseApi.BackupConfig{
						Schedule: ptr.To("0 3 * * *"),
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid - class enables backup but missing cnpg.backup.volumeSnapshot",
			class: &enterpriseApi.PostgresClusterClass{
				ObjectMeta: metav1.ObjectMeta{Name: "backup-no-cnpg"},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						Instances:       ptr.To(int32(3)),
						Storage:         ptr.To(resource.MustParse("50Gi")),
						PostgresVersion: ptr.To("17"),
						Backup: &enterpriseApi.BackupConfig{
							Enabled: ptr.To(true),
						},
					},
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "backup-no-cnpg",
					Backup: &enterpriseApi.BackupConfig{
						Schedule: ptr.To("0 2 * * *"),
					},
				},
			},
			wantErrCount:  1,
			wantErrFields: []string{"spec.backup.enabled"},
			wantErrMsgs:   []string{"backup requires cnpg.backup.volumeSnapshot configuration in PostgresClusterClass"},
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
			wantErrCount:  3,
			wantErrFields: []string{"spec.instances"},
			wantErrMsgs:   []string{"must be set in PostgresCluster or PostgresClusterClass"},
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
			wantErrCount:  1,
			wantErrFields: []string{"spec.storage"},
			wantErrMsgs:   []string{"must be set in PostgresCluster or PostgresClusterClass"},
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

			errs := webhook.ValidatePostgresClusterCreate(context.Background(), tt.obj, reader)
			require.Len(t, errs, tt.wantErrCount, "unexpected error count: %v", errs)
			for i, field := range tt.wantErrFields {
				assert.Equal(t, field, errs[i].Field, "unexpected error field at index %d", i)
			}
			for i, msg := range tt.wantErrMsgs {
				assert.Contains(t, errs[i].Detail, msg, "unexpected error message at index %d", i)
			}
			for i, val := range tt.wantErrValues {
				assert.Equal(t, val, errs[i].BadValue, "unexpected bad value at index %d", i)
			}
		})
	}
}
