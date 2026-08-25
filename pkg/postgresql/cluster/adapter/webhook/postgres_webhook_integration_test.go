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

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/validation"
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

func newPostgresClusterAdmissionReview(t *testing.T, uid string, op admissionv1.Operation, obj *platformApi.PostgresCluster, oldObj *platformApi.PostgresCluster) *admissionv1.AdmissionReview {
	t.Helper()
	ar := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: types.UID(uid),
			Kind: metav1.GroupVersionKind{
				Group:   platformApi.GroupVersion.Group,
				Version: platformApi.GroupVersion.Version,
				Kind:    "PostgresCluster",
			},
			Resource: metav1.GroupVersionResource{
				Group:    platformApi.GroupVersion.Group,
				Version:  platformApi.GroupVersion.Version,
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

func newPostgresClusterClassAdmissionReview(t *testing.T, uid string, op admissionv1.Operation, obj *platformApi.PostgresClusterClass, oldObj *platformApi.PostgresClusterClass) *admissionv1.AdmissionReview {
	t.Helper()
	ar := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: types.UID(uid),
			Kind: metav1.GroupVersionKind{
				Group:   platformApi.GroupVersion.Group,
				Version: platformApi.GroupVersion.Version,
				Kind:    "PostgresClusterClass",
			},
			Resource: metav1.GroupVersionResource{
				Group:    platformApi.GroupVersion.Group,
				Version:  platformApi.GroupVersion.Version,
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
		obj          *platformApi.PostgresCluster
		wantAllowed  bool
		wantMessage  string
		wantMessages []string
	}{
		{
			name: "valid - no pgHBA rules",
			obj: &platformApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: platformApi.PostgresClusterSpec{
					Class: "dev",
				},
			},
			wantAllowed: true,
		},
		{
			name: "valid - correct pgHBA rules",
			obj: &platformApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: platformApi.PostgresClusterSpec{
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
			obj: &platformApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: platformApi.PostgresClusterSpec{
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
			obj: &platformApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: platformApi.PostgresClusterSpec{
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
			obj: &platformApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: platformApi.PostgresClusterSpec{
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
			obj: &platformApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: platformApi.PostgresClusterSpec{
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
			obj: &platformApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: platformApi.PostgresClusterSpec{
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
			obj: &platformApi.PostgresCluster{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresCluster",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cluster",
					Namespace: "default",
				},
				Spec: platformApi.PostgresClusterSpec{
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

	oldObj := &platformApi.PostgresCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "platform.splunk.com/v1alpha1",
			Kind:       "PostgresCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
		Spec: platformApi.PostgresClusterSpec{
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
		obj         *platformApi.PostgresClusterClass
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "valid - no pgHBA rules",
			obj: &platformApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
				},
			},
			wantAllowed: true,
		},
		{
			name: "valid - correct pgHBA rules",
			obj: &platformApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
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
			wantAllowed: true,
		},
		{
			name: "rejected - bad connection type",
			obj: &platformApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
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
			obj: &platformApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
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
			obj: &platformApi.PostgresClusterClass{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "platform.splunk.com/v1alpha1",
					Kind:       "PostgresClusterClass",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev",
				},
				Spec: platformApi.PostgresClusterClassSpec{
					Provisioner: "postgresql.cnpg.io",
					Config: &platformApi.PostgresClusterClassConfig{
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

	oldObj := &platformApi.PostgresClusterClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "platform.splunk.com/v1alpha1",
			Kind:       "PostgresClusterClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "dev",
		},
		Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &platformApi.PostgresClusterClassConfig{
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
	platformApi.AddToScheme(s)
	corev1.AddToScheme(s)
	b := fake.NewClientBuilder().WithScheme(s)
	for _, obj := range objects {
		b = b.WithRuntimeObjects(obj)
	}
	return b
}

func TestCrossResourceValidationIntegration(t *testing.T) {
	prodClass := &platformApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &platformApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(3)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17"),
				ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
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
		obj         *platformApi.PostgresCluster
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "allowed - inherits all from class",
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       platformApi.PostgresClusterSpec{Class: "prod"},
			},
			wantAllowed: true,
		},
		{
			name: "rejected - class not found",
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       platformApi.PostgresClusterSpec{Class: "nonexistent"},
			},
			wantAllowed: false,
			wantMessage: "PostgresClusterClass not found",
		},
		{
			name: "rejected - version below class floor",
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("16"),
				},
			},
			wantAllowed: false,
			wantMessage: "postgresVersion cannot be lower than class default",
		},
		{
			name: "rejected - pooler enabled but class has no cnpg.connectionPooler",
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class: "prod",
					ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(true),
					},
				},
			},
			wantAllowed: false,
			wantMessage: "connection pooler requires cnpg.connectionPooler configuration",
		},
		{
			name: "allowed - higher version",
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
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
	prodClass := &platformApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "prod"},
		Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &platformApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(3)),
				Storage:         ptr.To(resource.MustParse("50Gi")),
				PostgresVersion: ptr.To("17"),
				ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
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

	oldObj := &platformApi.PostgresCluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: platformApi.PostgresClusterSpec{
			Class:           "prod",
			PostgresVersion: ptr.To("17"),
		},
	}

	tests := []struct {
		name        string
		newObj      *platformApi.PostgresCluster
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "allowed - upgrade version",
			newObj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("18"),
				},
			},
			wantAllowed: true,
		},
		{
			name: "rejected - downgrade version below class floor",
			newObj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class:           "prod",
					PostgresVersion: ptr.To("16"),
				},
			},
			wantAllowed: false,
			wantMessage: "postgresVersion cannot be lower than class default",
		},
		{
			name: "rejected - enable pooler without cnpg config",
			newObj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class: "prod",
					ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
						Enabled: ptr.To(true),
					},
				},
			},
			wantAllowed: false,
			wantMessage: "connection pooler requires cnpg.connectionPooler configuration",
		},
		{
			name: "allowed - no changes",
			newObj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
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
	classOne := &platformApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "single"},
		Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &platformApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(1)),
				Storage:         ptr.To(resource.MustParse("10Gi")),
				PostgresVersion: ptr.To("17"),
			},
			CNPG: &platformApi.CNPGConfig{
				PrimaryUpdateMethod: ptr.To("restart"),
				ConnectionPooler:    &platformApi.ConnectionPoolerConfig{},
			},
		},
	}
	classHA := &platformApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "ha"},
		Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config: &platformApi.PostgresClusterClassConfig{
				Instances:       ptr.To(int32(2)),
				Storage:         ptr.To(resource.MustParse("10Gi")),
				PostgresVersion: ptr.To("17"),
			},
			CNPG: &platformApi.CNPGConfig{
				PrimaryUpdateMethod: ptr.To("switchover"),
				ConnectionPooler:    &platformApi.ConnectionPoolerConfig{},
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
		obj         *platformApi.PostgresCluster
		oldObj      *platformApi.PostgresCluster
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "create rejected - readOnly=true at instances=1",
			op:   admissionv1.Create,
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class: "single",
					ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
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
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class: "single",
					ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
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
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class: "ha",
					ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
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
			oldObj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class: "single",
					ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
						Enabled:  ptr.To(true),
						ReadOnly: ptr.To(false),
					},
				},
			},
			obj: &platformApi.PostgresCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: platformApi.PostgresClusterSpec{
					Class: "single",
					ConnectionPooler: &platformApi.ConnectionPoolerEnableConfig{
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

// TestBootstrapFromPITRAdmissionIntegration exercises the recovery/PITR bootstrapFrom validation
// end-to-end through the admission webhook: exactly-one-source, walArchive-required-for-PITR, and
// the class-must-define-barmanObjectStore coupling for object-store recovery sources.
func TestBootstrapFromPITRAdmissionIntegration(t *testing.T) {
	baseConfig := func() *platformApi.PostgresClusterClassConfig {
		return &platformApi.PostgresClusterClassConfig{
			Instances:       ptr.To(int32(1)),
			Storage:         ptr.To(resource.MustParse("10Gi")),
			PostgresVersion: ptr.To("17"),
		}
	}

	// Class with a barman object store — supports object-store and WAL-archive recovery sources.
	classWithStore := &platformApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "with-store"},
		Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config:      baseConfig(),
			CNPG: &platformApi.CNPGConfig{
				Backup: &platformApi.CNPGBackupConfig{
					BarmanObjectStore: &platformApi.CNPGBarmanObjectStoreConfig{
						DestinationPath: "s3://bucket/pg",
					},
				},
			},
		},
	}
	// Class without a barman object store — only plain snapshot restore is valid.
	classNoStore := &platformApi.PostgresClusterClass{
		ObjectMeta: metav1.ObjectMeta{Name: "no-store"},
		Spec: platformApi.PostgresClusterClassSpec{
			Provisioner: "postgresql.cnpg.io",
			Config:      baseConfig(),
		},
	}

	fakeClient := newFakeReader(classWithStore, classNoStore).Build()
	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
		Client:     fakeClient,
	})

	cluster := func(class string, b *platformApi.BootstrapFrom) *platformApi.PostgresCluster {
		return &platformApi.PostgresCluster{
			TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       platformApi.PostgresClusterSpec{Class: class, BootstrapFrom: b},
		}
	}

	tests := []struct {
		name        string
		obj         *platformApi.PostgresCluster
		wantAllowed bool
		wantMessage string
	}{
		{
			name: "allowed - plain snapshot restore without object store",
			obj: cluster("no-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{Storage: "snap-1"},
			}),
			wantAllowed: true,
		},
		{
			name: "rejected - both sources set",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{Storage: "snap-1"},
				ObjectStorage:  &platformApi.ObjectStorageSource{ServerName: "src"},
			}),
			wantAllowed: false,
			wantMessage: "exactly one of volumeSnapshot or objectStorage must be set",
		},
		{
			name:        "rejected - neither source set",
			obj:         cluster("with-store", &platformApi.BootstrapFrom{}),
			wantAllowed: false,
			wantMessage: "exactly one of volumeSnapshot or objectStorage must be set",
		},
		{
			name: "rejected - snapshot PITR without walArchive",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{Storage: "snap-1"},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"},
			}),
			wantAllowed: false,
			wantMessage: "walArchive is required when recoveryTarget is set",
		},
		{
			name: "allowed - snapshot PITR with walArchive",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{
					Storage:    "snap-1",
					WalArchive: &platformApi.ObjectStorageSource{ServerName: "src"},
				},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"},
			}),
			wantAllowed: true,
		},
		{
			name: "rejected - walArchive but class has no barmanObjectStore",
			obj: cluster("no-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{
					Storage:    "snap-1",
					WalArchive: &platformApi.ObjectStorageSource{ServerName: "src"},
				},
			}),
			wantAllowed: false,
			wantMessage: "requires cnpg.backup.barmanObjectStore to be configured",
		},
		{
			name: "rejected - objectStorage source but class has no barmanObjectStore",
			obj: cluster("no-store", &platformApi.BootstrapFrom{
				ObjectStorage: &platformApi.ObjectStorageSource{ServerName: "src"},
			}),
			wantAllowed: false,
			wantMessage: "requires cnpg.backup.barmanObjectStore to be configured",
		},
		{
			name: "allowed - objectStorage source with class barmanObjectStore",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				ObjectStorage:  &platformApi.ObjectStorageSource{ServerName: "src"},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetTime, Value: "2026-05-01T13:30:00Z"},
			}),
			wantAllowed: true,
		},
		{
			name: "rejected - objectStorage source with type xid (no backupID selection)",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				ObjectStorage:  &platformApi.ObjectStorageSource{ServerName: "src"},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetXID, Value: "1234567"},
			}),
			wantAllowed: false,
			wantMessage: "not supported for an objectStorage source",
		},
		{
			name: "rejected - malformed type time value",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				ObjectStorage:  &platformApi.ObjectStorageSource{ServerName: "src"},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetTime, Value: "May 1 2026"},
			}),
			wantAllowed: false,
			wantMessage: "value for target type time must be an RFC 3339 timestamp",
		},
		{
			name: "rejected - malformed type lsn value",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				ObjectStorage:  &platformApi.ObjectStorageSource{ServerName: "src"},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetLSN, Value: "nope"},
			}),
			wantAllowed: false,
			wantMessage: "value for target type lsn must be a WAL log sequence number",
		},
		{
			name: "rejected - non-numeric type xid value on snapshot base",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{
					Storage:    "snap-1",
					WalArchive: &platformApi.ObjectStorageSource{ServerName: "src"},
				},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetXID, Value: "12ab"},
			}),
			wantAllowed: false,
			wantMessage: "value for target type xid must be a numeric transaction ID",
		},
		{
			// An empty value is normally rejected by the CRD CEL rule (self.value != ''), but the
			// value-format validators are the last line of defense if that rule is ever weakened, so
			// assert an empty value is still rejected here on the webhook path.
			name: "rejected - empty type time value",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				ObjectStorage:  &platformApi.ObjectStorageSource{ServerName: "src"},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetTime, Value: ""},
			}),
			wantAllowed: false,
			wantMessage: "value for target type time must be an RFC 3339 timestamp",
		},
		{
			name: "rejected - empty type name value on snapshot base",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{
					Storage:    "snap-1",
					WalArchive: &platformApi.ObjectStorageSource{ServerName: "src"},
				},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetName, Value: ""},
			}),
			wantAllowed: false,
			wantMessage: "value for target type name must be a restore-point name",
		},
		{
			name: "rejected - control character in type name value",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{
					Storage:    "snap-1",
					WalArchive: &platformApi.ObjectStorageSource{ServerName: "src"},
				},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetName, Value: "bad\tname"},
			}),
			wantAllowed: false,
			wantMessage: "value for target type name must be a restore-point name",
		},
		{
			name: "allowed - valid type name value on snapshot base",
			obj: cluster("with-store", &platformApi.BootstrapFrom{
				VolumeSnapshot: &platformApi.VolumeSnapshotSource{
					Storage:    "snap-1",
					WalArchive: &platformApi.ObjectStorageSource{ServerName: "src"},
				},
				RecoveryTarget: &platformApi.RecoveryTarget{Type: platformApi.RecoveryTargetName, Value: "before-upgrade"},
			}),
			wantAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ar := newPostgresClusterAdmissionReview(t, "uid-pitr-"+tt.name, admissionv1.Create, tt.obj, nil)
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

func TestCrossResourceValidationDisabledWithoutClient(t *testing.T) {
	server := validation.NewWebhookServer(validation.WebhookServerOptions{
		Port:       9443,
		Validators: validation.DefaultValidators,
	})

	obj := &platformApi.PostgresCluster{
		TypeMeta:   metav1.TypeMeta{APIVersion: "platform.splunk.com/v1alpha1", Kind: "PostgresCluster"},
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec:       platformApi.PostgresClusterSpec{Class: "nonexistent"},
	}

	ar := newPostgresClusterAdmissionReview(t, "uid-no-client", admissionv1.Create, obj, nil)
	resp := sendAdmissionReview(t, server, ar)

	assert.True(t, resp.Allowed, "without a client, cross-resource validation should be skipped")
}
