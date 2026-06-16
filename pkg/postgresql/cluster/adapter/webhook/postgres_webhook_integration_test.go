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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise/validation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	config.EnableFeatureGate(config.PostgresController)
}

func mustMarshal(t *testing.T, obj interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("failed to marshal object: %v", err)
	}
	return data
}

func newPostgresClusterAdmissionReview(t *testing.T, uid string, op admissionv1.Operation, obj *enterpriseApi.PostgresCluster, oldObj *enterpriseApi.PostgresCluster) *admissionv1.AdmissionReview {
	t.Helper()
	ar := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: types.UID(uid),
			Kind: metav1.GroupVersionKind{
				Group:   "enterprise.splunk.com",
				Version: "v4",
				Kind:    "PostgresCluster",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "enterprise.splunk.com",
				Version:  "v4",
				Resource: "postgresclusters",
			},
			Name:      obj.Name,
			Namespace: obj.Namespace,
			Operation: op,
			Object: runtime.RawExtension{
				Raw: mustMarshal(t, obj),
			},
			UserInfo: authenticationv1.UserInfo{Username: "test-user"},
		},
	}
	if oldObj != nil {
		ar.Request.OldObject = runtime.RawExtension{
			Raw: mustMarshal(t, oldObj),
		}
	}
	return ar
}

func newPostgresClusterClassAdmissionReview(t *testing.T, uid string, op admissionv1.Operation, obj *enterpriseApi.PostgresClusterClass, oldObj *enterpriseApi.PostgresClusterClass) *admissionv1.AdmissionReview {
	t.Helper()
	ar := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: types.UID(uid),
			Kind: metav1.GroupVersionKind{
				Group:   "enterprise.splunk.com",
				Version: "v4",
				Kind:    "PostgresClusterClass",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "enterprise.splunk.com",
				Version:  "v4",
				Resource: "postgresclusterclasses",
			},
			Name:      obj.Name,
			Operation: op,
			Object: runtime.RawExtension{
				Raw: mustMarshal(t, obj),
			},
			UserInfo: authenticationv1.UserInfo{Username: "test-user"},
		},
	}
	if oldObj != nil {
		ar.Request.OldObject = runtime.RawExtension{
			Raw: mustMarshal(t, oldObj),
		}
	}
	return ar
}

func sendAdmissionReview(t *testing.T, server *validation.WebhookServer, ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	t.Helper()
	body, err := json.Marshal(ar)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.HandleValidate(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var response admissionv1.AdmissionReview
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
	require.NotNil(t, response.Response)
	return response.Response
}

func TestPostgresClusterPgHBAIntegration(t *testing.T) {
	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
	})

	tests := []struct {
		name         string
		obj          *enterpriseApi.PostgresCluster
		wantAllowed  bool
		wantMessage  string
		wantMessages []string
	}{
		{
			name: "valid - no pgHBA rules",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
				},
			},
			wantAllowed: true,
		},
		{
			name: "valid - correct pgHBA rules",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"hostnossl all all 0.0.0.0/0 reject",
						"hostssl all all 0.0.0.0/0 scram-sha-256",
						"local all all peer",
					},
				},
			},
			wantAllowed: true,
		},
		{
			name: "rejected - bad connection type",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"hostx all all 0.0.0.0/0 md5",
					},
				},
			},
			wantAllowed: false,
			wantMessage: "unknown connection type",
		},
		{
			name: "rejected - bad CIDR",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"host all all 192.168.0.0/33 md5",
					},
				},
			},
			wantAllowed: false,
			wantMessage: "invalid CIDR",
		},
		{
			name: "rejected - unknown auth method",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"host all all 0.0.0.0/0 bogus",
					},
				},
			},
			wantAllowed: false,
			wantMessage: "unknown auth method",
		},
		{
			name: "rejected - too few fields",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"host all all",
					},
				},
			},
			wantAllowed: false,
			wantMessage: "too few fields",
		},
		{
			name: "rejected - multiple bad rules reports all errors",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"hostssl all all 0.0.0.0/0 scram-sha-256",
						"hostx all all 0.0.0.0/0 md5",
						"host all all 10.0.0.0/8 bogus",
					},
				},
			},
			wantAllowed:  false,
			wantMessages: []string{"spec.pgHBA[1]", "spec.pgHBA[2]"},
		},
		{
			name: "valid - rules with auth options and comments",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "dev",
					PgHBA: []string{
						"host all all 0.0.0.0/0 ldap ldapserver=ldap.example.com ldapport=389",
						"host all all 0.0.0.0/0 md5 # office access",
					},
				},
			},
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ar := newPostgresClusterAdmissionReview(t, "uid-"+tt.name, admissionv1.Create, tt.obj, nil)
			resp := sendAdmissionReview(t, server, ar)

			assert.Equal(t, tt.wantAllowed, resp.Allowed, "unexpected admission result")
			if !tt.wantAllowed {
				require.NotNil(t, resp.Result)
				assert.Equal(t, metav1.StatusReasonInvalid, resp.Result.Reason)
				assert.Equal(t, int32(http.StatusUnprocessableEntity), resp.Result.Code)
			}
			if tt.wantMessage != "" {
				require.NotNil(t, resp.Result)
				assert.Contains(t, resp.Result.Message, tt.wantMessage)
			}
			for _, msg := range tt.wantMessages {
				require.NotNil(t, resp.Result)
				assert.Contains(t, resp.Result.Message, msg)
			}
		})
	}
}

func TestPostgresClusterPgHBAUpdateIntegration(t *testing.T) {
	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
	})

	oldObj := &enterpriseApi.PostgresCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v4",
			Kind:       "PostgresCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: enterpriseApi.PostgresClusterSpec{
			Class: "dev",
			PgHBA: []string{
				"host all all 0.0.0.0/0 scram-sha-256",
			},
		},
	}

	t.Run("valid update - change rules", func(t *testing.T) {
		newObj := oldObj.DeepCopy()
		newObj.Spec.PgHBA = []string{
			"hostssl all all 0.0.0.0/0 scram-sha-256",
			"local all all peer",
		}
		ar := newPostgresClusterAdmissionReview(t, "uid-update-valid", admissionv1.Update, newObj, oldObj)
		resp := sendAdmissionReview(t, server, ar)
		assert.True(t, resp.Allowed)
	})

	t.Run("rejected update - invalid new rules", func(t *testing.T) {
		newObj := oldObj.DeepCopy()
		newObj.Spec.PgHBA = []string{
			"hostx all all 0.0.0.0/0 md5",
		}
		ar := newPostgresClusterAdmissionReview(t, "uid-update-invalid", admissionv1.Update, newObj, oldObj)
		resp := sendAdmissionReview(t, server, ar)
		assert.False(t, resp.Allowed)
		assert.Equal(t, metav1.StatusReasonInvalid, resp.Result.Reason)
		assert.Equal(t, int32(http.StatusUnprocessableEntity), resp.Result.Code)
		assert.Contains(t, resp.Result.Message, "unknown connection type")
	})
}

func TestPostgresClusterClassPgHBAIntegration(t *testing.T) {
	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
	})

	tests := []struct {
		name        string
		obj         *enterpriseApi.PostgresClusterClass
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "valid - no pgHBA rules",
			obj: &enterpriseApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			wantAllowed: true,
		},
		{
			name: "valid - correct pgHBA rules",
			obj: &enterpriseApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
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
			wantAllowed: true,
		},
		{
			name: "rejected - bad connection type",
			obj: &enterpriseApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{
							"hostx all all 0.0.0.0/0 md5",
						},
					},
				},
			},
			wantAllowed: false,
			wantMessage: "unknown connection type",
		},
		{
			name: "rejected - invalid CIDR in class",
			obj: &enterpriseApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{
							"host all all 256.1.1.1/24 md5",
						},
					},
				},
			},
			wantAllowed: false,
			wantMessage: "invalid CIDR",
		},
		{
			name: "rejected - unknown auth method in class",
			obj: &enterpriseApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "enterprise.splunk.com/v4",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
				Spec: enterpriseApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &enterpriseApi.PostgresClusterClassConfig{
						PgHBA: []string{
							"host all all 0.0.0.0/0 fake-method",
						},
					},
				},
			},
			wantAllowed: false,
			wantMessage: "unknown auth method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ar := newPostgresClusterClassAdmissionReview(t, "uid-"+tt.name, admissionv1.Create, tt.obj, nil)
			resp := sendAdmissionReview(t, server, ar)

			assert.Equal(t, tt.wantAllowed, resp.Allowed, "unexpected admission result")
			if !tt.wantAllowed {
				require.NotNil(t, resp.Result)
				assert.Equal(t, metav1.StatusReasonInvalid, resp.Result.Reason)
				assert.Equal(t, int32(http.StatusUnprocessableEntity), resp.Result.Code)
			}
			if tt.wantMessage != "" {
				require.NotNil(t, resp.Result)
				assert.Contains(t, resp.Result.Message, tt.wantMessage)
			}
		})
	}
}

func TestPostgresClusterClassPgHBAUpdateIntegration(t *testing.T) {
	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
	})

	oldObj := &enterpriseApi.PostgresClusterClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v4",
			Kind:       "PostgresClusterClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "dev",
		},
		Spec: enterpriseApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &enterpriseApi.PostgresClusterClassConfig{
				PgHBA: []string{
					"host all all 0.0.0.0/0 scram-sha-256",
				},
			},
		},
	}

	t.Run("valid update - change rules", func(t *testing.T) {
		newObj := oldObj.DeepCopy()
		newObj.Spec.Config.PgHBA = []string{
			"hostssl all all 0.0.0.0/0 scram-sha-256",
			"hostnossl all all 0.0.0.0/0 reject",
		}
		ar := newPostgresClusterClassAdmissionReview(t, "uid-class-update-valid", admissionv1.Update, newObj, oldObj)
		resp := sendAdmissionReview(t, server, ar)
		assert.True(t, resp.Allowed)
	})

	t.Run("rejected update - invalid new rules", func(t *testing.T) {
		newObj := oldObj.DeepCopy()
		newObj.Spec.Config.PgHBA = []string{
			"host all all 0.0.0.0/0 bogus",
		}
		ar := newPostgresClusterClassAdmissionReview(t, "uid-class-update-invalid", admissionv1.Update, newObj, oldObj)
		resp := sendAdmissionReview(t, server, ar)
		assert.False(t, resp.Allowed)
		assert.Equal(t, metav1.StatusReasonInvalid, resp.Result.Reason)
		assert.Equal(t, int32(http.StatusUnprocessableEntity), resp.Result.Code)
		assert.Contains(t, resp.Result.Message, "unknown auth method")
	})
}

func newFakeReader(objects ...runtime.Object) *fake.ClientBuilder {
	s := runtime.NewScheme()
	enterpriseApi.AddToScheme(s)
	corev1.AddToScheme(s)
	b := fake.NewClientBuilder().WithScheme(s)
	for _, obj := range objects {
		b = b.WithRuntimeObjects(obj)
	}
	return b
}

func TestCrossResourceValidationIntegration(t *testing.T) {
	prodClass := &enterpriseApi.PostgresClusterClass{
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

	fakeClient := newFakeReader(prodClass).Build()

	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
		Client:     fakeClient,
	})

	tests := []struct {
		name        string
		obj         *enterpriseApi.PostgresCluster
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "allowed - inherits all from class",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       enterpriseApi.PostgresClusterSpec{Class: "prod"},
			},
			wantAllowed: true,
		},
		{
			name: "rejected - class not found",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       enterpriseApi.PostgresClusterSpec{Class: "nonexistent"},
			},
			wantAllowed: false,
			wantMessage: "PostgresClusterClass not found",
		},
		{
			name: "rejected - version below class floor",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("16"),
				},
			},
			wantAllowed: false,
			wantMessage: "postgresVersion cannot be lower than class default",
		},
		{
			name: "rejected - pooler enabled but class has no cnpg.connectionPooler",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "prod",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(true),
					},
				},
			},
			wantAllowed: false,
			wantMessage: "connection pooler requires cnpg.connectionPooler configuration",
		},
		{
			name: "allowed - higher version",
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("18"),
				},
			},
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ar := newPostgresClusterAdmissionReview(t, "uid-xref-"+tt.name, admissionv1.Create, tt.obj, nil)
			resp := sendAdmissionReview(t, server, ar)

			assert.Equal(t, tt.wantAllowed, resp.Allowed, "unexpected admission result")
			if tt.wantMessage != "" {
				require.NotNil(t, resp.Result)
				assert.Contains(t, resp.Result.Message, tt.wantMessage)
			}
		})
	}
}

func TestCrossResourceValidationUpdateIntegration(t *testing.T) {
	prodClass := &enterpriseApi.PostgresClusterClass{
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

	fakeClient := newFakeReader(prodClass).Build()

	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
		Client:     fakeClient,
	})

	oldObj := &enterpriseApi.PostgresCluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: enterpriseApi.PostgresClusterSpec{
			Class:           "prod",
			PostgresVersion: ptr.To("17"),
		},
	}

	tests := []struct {
		name        string
		newObj      *enterpriseApi.PostgresCluster
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "allowed - upgrade version",
			newObj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("18"),
				},
			},
			wantAllowed: true,
		},
		{
			name: "rejected - downgrade version below class floor",
			newObj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("16"),
				},
			},
			wantAllowed: false,
			wantMessage: "postgresVersion cannot be lower than class default",
		},
		{
			name: "rejected - enable pooler without cnpg config",
			newObj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "prod",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(true),
					},
				},
			},
			wantAllowed: false,
			wantMessage: "connection pooler requires cnpg.connectionPooler configuration",
		},
		{
			name: "allowed - no changes",
			newObj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("17"),
				},
			},
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ar := newPostgresClusterAdmissionReview(t, "uid-xref-update-"+tt.name, admissionv1.Update, tt.newObj, oldObj)
			resp := sendAdmissionReview(t, server, ar)

			assert.Equal(t, tt.wantAllowed, resp.Allowed, "unexpected admission result")
			if tt.wantMessage != "" {
				require.NotNil(t, resp.Result)
				assert.Contains(t, resp.Result.Message, tt.wantMessage)
			}
		})
	}
}

// TestPoolerEndpointAdmissionIntegration covers the readOnly + instances<2
// rejection end-to-end through the admission webhook server.
func TestPoolerEndpointAdmissionIntegration(t *testing.T) {
	classOne := &enterpriseApi.PostgresClusterClass{
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
	classHA := &enterpriseApi.PostgresClusterClass{
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

	fakeClient := newFakeReader(classOne, classHA).Build()
	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
		Client:     fakeClient,
	})

	tests := []struct {
		name        string
		op          admissionv1.Operation
		obj         *enterpriseApi.PostgresCluster
		oldObj      *enterpriseApi.PostgresCluster
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "create rejected - readOnly=true at instances=1",
			op:   admissionv1.Create,
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "single",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled:  ptr.To(true),
						ReadOnly: ptr.To(true),
					},
				},
			},
			wantAllowed: false,
			wantMessage: "connectionPooler.readOnly cannot be true when effective instances=1",
		},
		{
			name: "create allowed - readOnly=false at instances=1",
			op:   admissionv1.Create,
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "single",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled:  ptr.To(true),
						ReadOnly: ptr.To(false),
					},
				},
			},
			wantAllowed: true,
		},
		{
			name: "create allowed - readOnly=true at instances=2",
			op:   admissionv1.Create,
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "ha",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled:  ptr.To(true),
						ReadOnly: ptr.To(true),
					},
				},
			},
			wantAllowed: true,
		},
		{
			name: "update rejected - flipping readOnly true at instances=1",
			op:   admissionv1.Update,
			oldObj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "single",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled:  ptr.To(true),
						ReadOnly: ptr.To(false),
					},
				},
			},
			obj: &enterpriseApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: enterpriseApi.PostgresClusterSpec{
					Class: "single",
					ConnectionPooler: &enterpriseApi.ConnectionPoolerEnableConfig{
						Enabled:  ptr.To(true),
						ReadOnly: ptr.To(true),
					},
				},
			},
			wantAllowed: false,
			wantMessage: "connectionPooler.readOnly cannot be true when effective instances=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ar := newPostgresClusterAdmissionReview(t, "uid-pooler-"+tt.name, tt.op, tt.obj, tt.oldObj)
			resp := sendAdmissionReview(t, server, ar)
			assert.Equal(t, tt.wantAllowed, resp.Allowed, "unexpected admission result")
			if tt.wantMessage != "" {
				require.NotNil(t, resp.Result)
				assert.Contains(t, resp.Result.Message, tt.wantMessage)
			}
		})
	}
}

func TestCrossResourceValidationDisabledWithoutClient(t *testing.T) {
	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
	})

	obj := &enterpriseApi.PostgresCluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "PostgresCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       enterpriseApi.PostgresClusterSpec{Class: "nonexistent"},
	}

	ar := newPostgresClusterAdmissionReview(t, "uid-no-client", admissionv1.Create, obj, nil)
	resp := sendAdmissionReview(t, server, ar)

	assert.True(t, resp.Allowed, "without a client, cross-resource validation should be skipped")
}
