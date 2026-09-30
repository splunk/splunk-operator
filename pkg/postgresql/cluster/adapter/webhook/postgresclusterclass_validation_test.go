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
	"time"

	cmacme "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidatePostgresClusterClassCreate(t *testing.T) {
	tests := []struct {
		name         string
		obj          *platformApi.PostgresClusterClass
		wantErrCount int
		wantErrField string
	}{
		{
			name: "valid - no config",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid - config without pgHBA",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config:      &platformApi.PostgresClusterClassConfig{},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid - correct pgHBA rules",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
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
			name: "valid - tagged postgresImage",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
						PostgresVersion: ptr.To("18"),
						PostgresImage:   ptr.To("registry.example.com/team/postgresql:18.1"),
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "valid - tag plus digest postgresImage",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
						PostgresVersion: ptr.To("18"),
						PostgresImage:   ptr.To("registry.example.com/team/postgresql:18.1@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
					},
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid - latest postgresImage",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
						PostgresVersion: ptr.To("18"),
						PostgresImage:   ptr.To("registry.example.com/team/postgresql:latest"),
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.config.postgresImage",
		},
		{
			name: "invalid - postgresImage major mismatch",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
						PostgresVersion: ptr.To("18"),
						PostgresImage:   ptr.To("registry.example.com/team/postgresql:17.5"),
					},
				},
			},
			wantErrCount: 1,
			wantErrField: "spec.config.postgresImage",
		},
		{
			name: "invalid - bad connection type",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
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
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
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
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
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
		obj          *platformApi.PostgresClusterClass
		oldObj       *platformApi.PostgresClusterClass
		wantErrCount int
	}{
		{
			name: "valid update",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
						PgHBA: []string{"host all all 0.0.0.0/0 scram-sha-256"},
					},
				},
			},
			oldObj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - bad pgHBA",
			obj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
						PgHBA: []string{"host all all 0.0.0.0/0 fake-method"},
					},
				},
			},
			oldObj: &platformApi.PostgresClusterClass{
				Spec: platformApi.PostgresClusterClassSpec{
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

	obj := &platformApi.PostgresClusterClass{
		Spec: platformApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
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

	obj := &platformApi.PostgresClusterClass{
		Spec: platformApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}
	oldObj := obj.DeepCopy()

	errs := webhook.ValidatePostgresClusterClassUpdate(obj, oldObj)
	assert.Len(t, errs, 1)
	assert.Equal(t, "spec", errs[0].Field)
	assert.Equal(t, "the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate", errs[0].Detail)
}

func TestValidatePostgresClusterClassCreateRejectsACMEIssuer(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})
	mode := platformApi.PostgresCertificateModeCertManager
	class := &platformApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "tls", Namespace: "payments"},
		Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			TLS: &platformApi.PostgresClusterClassTLS{Certificates: &platformApi.PostgresCertificateConfig{
				Mode:      &mode,
				IssuerRef: &platformApi.CertificateIssuerReference{Name: "acme", Kind: "ClusterIssuer"},
			}},
		},
	}
	scheme := runtime.NewScheme()
	require.NoError(t, cmapi.AddToScheme(scheme))
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&cmapi.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: "acme"},
		Spec:       cmapi.IssuerSpec{IssuerConfig: cmapi.IssuerConfig{ACME: &cmacme.ACMEIssuer{Server: "https://acme.example"}}},
	}).Build()
	errs := webhook.ValidatePostgresClusterClassCreateWithContext(context.Background(), class, reader)
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Detail, "ACME issuers are not supported")
}

func TestValidatePostgresClusterClassCreateDefersNamespacedIssuerLookup(t *testing.T) {
	mode := platformApi.PostgresCertificateModeCertManager
	class := &platformApi.PostgresClusterClass{Spec: platformApi.PostgresClusterClassSpec{
		Provisioner: "postgresql.cnpg.io",
		TLS: &platformApi.PostgresClusterClassTLS{Certificates: &platformApi.PostgresCertificateConfig{
			Mode:      &mode,
			IssuerRef: &platformApi.CertificateIssuerReference{Name: "namespace-ca", Kind: "Issuer"},
		}},
	}}
	scheme := runtime.NewScheme()
	require.NoError(t, cmapi.AddToScheme(scheme))
	errs := webhook.ValidatePostgresClusterClassCreateWithContext(context.Background(), class, fake.NewClientBuilder().WithScheme(scheme).Build())
	assert.Empty(t, errs)
}

func TestValidatePostgresClusterClassCreateRejectsInvalidCertManagerCertificateSettings(t *testing.T) {
	config.DefaultMutableFeatureGate.SetFromMap(map[string]bool{string(config.PostgresController): true})

	mode := platformApi.PostgresCertificateModeCertManager
	issuerRef := &platformApi.CertificateIssuerReference{Name: "namespace-ca", Kind: "Issuer"}
	certificateClass := func(duration, renewBefore *metav1.Duration, usages []string) *platformApi.PostgresClusterClass {
		return &platformApi.PostgresClusterClass{Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			TLS: &platformApi.PostgresClusterClassTLS{Certificates: &platformApi.PostgresCertificateConfig{
				Mode: &mode, IssuerRef: issuerRef, Duration: duration, RenewBefore: renewBefore, ServerUsages: usages,
			}},
		}}
	}

	tests := []struct {
		name      string
		class     *platformApi.PostgresClusterClass
		wantField string
	}{
		{
			name:      "duration below cert-manager minimum",
			class:     certificateClass(&metav1.Duration{Duration: 30 * time.Minute}, nil, nil),
			wantField: "spec.tls.certificates.duration",
		},
		{
			name:      "renew before below cert-manager minimum",
			class:     certificateClass(nil, &metav1.Duration{Duration: time.Minute}, nil),
			wantField: "spec.tls.certificates.renewBefore",
		},
		{
			name:      "renew before must fit default duration",
			class:     certificateClass(nil, &metav1.Duration{Duration: 90 * 24 * time.Hour}, nil),
			wantField: "spec.tls.certificates.renewBefore",
		},
		{
			name:      "unsupported key usage",
			class:     certificateClass(nil, nil, []string{"typo"}),
			wantField: "spec.tls.certificates.serverUsages[0]",
		},
		{
			name:  "cert-manager duration boundaries are valid",
			class: certificateClass(&metav1.Duration{Duration: time.Hour}, &metav1.Duration{Duration: 5 * time.Minute}, []string{string(cmapi.UsageServerAuth)}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := webhook.ValidatePostgresClusterClassCreate(tt.class)
			if tt.wantField == "" {
				assert.Empty(t, errs)
				return
			}
			require.NotEmpty(t, errs)
			assert.Equal(t, tt.wantField, errs[0].Field)
		})
	}
}

func TestGetPostgresClusterClassWarningsOnCreate(t *testing.T) {
	obj := &platformApi.PostgresClusterClass{
		Spec: platformApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}
	assert.Empty(t, webhook.GetPostgresClusterClassWarningsOnCreate(obj))
}

func TestGetPostgresClusterClassWarningsOnUpdate(t *testing.T) {
	obj := &platformApi.PostgresClusterClass{
		Spec: platformApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}
	oldObj := &platformApi.PostgresClusterClass{
		Spec: platformApi.PostgresClusterClassSpec{Provisioner: "postgresql.cnpg.io"},
	}
	assert.Empty(t, webhook.GetPostgresClusterClassWarningsOnUpdate(obj, oldObj))
}
