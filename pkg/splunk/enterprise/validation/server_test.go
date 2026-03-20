/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package validation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

func TestNewWebhookServer(t *testing.T) {
	options := WebhookServerOptions{
		Port:    9443,
		CertDir: "/tmp/certs",
	}

	server := NewWebhookServer(options)

	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.options.Port != 9443 {
		t.Errorf("expected port 9443, got %d", server.options.Port)
	}
	if server.options.CertDir != "/tmp/certs" {
		t.Errorf("expected certDir /tmp/certs, got %s", server.options.CertDir)
	}
}

func TestHandleValidate(t *testing.T) {
	// Create test validators
	validators := map[schema.GroupVersionResource]Validator{
		StandaloneGVR: &GenericValidator[*enterpriseApi.Standalone]{
			ValidateCreateFunc: func(obj *enterpriseApi.Standalone) field.ErrorList {
				var allErrs field.ErrorList
				if obj.Spec.Replicas < 0 {
					allErrs = append(allErrs, field.Invalid(
						field.NewPath("spec").Child("replicas"),
						obj.Spec.Replicas,
						"replicas must be non-negative"))
				}
				return allErrs
			},
			ValidateUpdateFunc: func(obj, oldObj *enterpriseApi.Standalone) field.ErrorList {
				return nil
			},
			GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
		},
	}

	server := NewWebhookServer(WebhookServerOptions{
		Port:       9443,
		Validators: validators,
	})

	tests := []struct {
		name           string
		method         string
		body           interface{}
		wantStatusCode int
		wantAllowed    bool
		checkResponse  bool
	}{
		{
			name:           "method not allowed - GET",
			method:         http.MethodGet,
			body:           nil,
			wantStatusCode: http.StatusMethodNotAllowed,
			checkResponse:  false,
		},
		{
			name:           "method not allowed - PUT",
			method:         http.MethodPut,
			body:           nil,
			wantStatusCode: http.StatusMethodNotAllowed,
			checkResponse:  false,
		},
		{
			name:           "invalid JSON body",
			method:         http.MethodPost,
			body:           "not valid json",
			wantStatusCode: http.StatusBadRequest,
			checkResponse:  false,
		},
		{
			name:   "valid CREATE - allowed",
			method: http.MethodPost,
			body: &admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid-1",
					Kind: metav1.GroupVersionKind{
						Group:   "enterprise.splunk.com",
						Version: "v4",
						Kind:    "Standalone",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Name:      "test-standalone",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: mustMarshal(&enterpriseApi.Standalone{
							TypeMeta: metav1.TypeMeta{
								APIVersion: "enterprise.splunk.com/v4",
								Kind:       "Standalone",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-standalone",
								Namespace: "default",
							},
							Spec: enterpriseApi.StandaloneSpec{
								Replicas: 1,
							},
						}),
					},
					UserInfo: authenticationv1.UserInfo{Username: "test-user"},
				},
			},
			wantStatusCode: http.StatusOK,
			wantAllowed:    true,
			checkResponse:  true,
		},
		{
			name:   "invalid CREATE - negative replicas",
			method: http.MethodPost,
			body: &admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid-2",
					Kind: metav1.GroupVersionKind{
						Group:   "enterprise.splunk.com",
						Version: "v4",
						Kind:    "Standalone",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Name:      "test-standalone",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: mustMarshal(&enterpriseApi.Standalone{
							TypeMeta: metav1.TypeMeta{
								APIVersion: "enterprise.splunk.com/v4",
								Kind:       "Standalone",
							},
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-standalone",
								Namespace: "default",
							},
							Spec: enterpriseApi.StandaloneSpec{
								Replicas: -1,
							},
						}),
					},
					UserInfo: authenticationv1.UserInfo{Username: "test-user"},
				},
			},
			wantStatusCode: http.StatusOK,
			wantAllowed:    false,
			checkResponse:  true,
		},
		{
			name:   "unknown resource - no validator - rejected",
			method: http.MethodPost,
			body: &admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "admission.k8s.io/v1",
					Kind:       "AdmissionReview",
				},
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid-3",
					Kind: metav1.GroupVersionKind{
						Group:   "enterprise.splunk.com",
						Version: "v4",
						Kind:    "Unknown",
					},
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "unknowns",
					},
					Name:      "test-unknown",
					Namespace: "default",
					Operation: admissionv1.Create,
					Object: runtime.RawExtension{
						Raw: []byte(`{"apiVersion":"enterprise.splunk.com/v4","kind":"Unknown"}`),
					},
					UserInfo: authenticationv1.UserInfo{Username: "test-user"},
				},
			},
			wantStatusCode: http.StatusOK,
			wantAllowed:    false,
			checkResponse:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyBytes []byte
			var err error

			if tt.body != nil {
				switch v := tt.body.(type) {
				case string:
					bodyBytes = []byte(v)
				default:
					bodyBytes, err = json.Marshal(tt.body)
					if err != nil {
						t.Fatalf("failed to marshal body: %v", err)
					}
				}
			}

			req := httptest.NewRequest(tt.method, "/validate", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleValidate(rr, req)

			if rr.Code != tt.wantStatusCode {
				t.Errorf("expected status code %d, got %d", tt.wantStatusCode, rr.Code)
			}

			if tt.checkResponse {
				var response admissionv1.AdmissionReview
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				if response.Response == nil {
					t.Fatal("expected non-nil response")
				}

				if response.Response.Allowed != tt.wantAllowed {
					t.Errorf("expected allowed=%v, got %v", tt.wantAllowed, response.Response.Allowed)
				}
			}
		})
	}
}

func TestHandleReadyz(t *testing.T) {
	server := NewWebhookServer(WebhookServerOptions{
		Port: 9443,
	})

	tests := []struct {
		name           string
		method         string
		wantStatusCode int
		wantBody       string
	}{
		{
			name:           "GET request",
			method:         http.MethodGet,
			wantStatusCode: http.StatusOK,
			wantBody:       "ok",
		},
		{
			name:           "POST request",
			method:         http.MethodPost,
			wantStatusCode: http.StatusOK,
			wantBody:       "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/readyz", nil)
			rr := httptest.NewRecorder()

			server.handleReadyz(rr, req)

			if rr.Code != tt.wantStatusCode {
				t.Errorf("expected status code %d, got %d", tt.wantStatusCode, rr.Code)
			}

			if rr.Body.String() != tt.wantBody {
				t.Errorf("expected body %q, got %q", tt.wantBody, rr.Body.String())
			}
		})
	}
}

func TestHandleValidateWithWarnings(t *testing.T) {
	validators := map[schema.GroupVersionResource]Validator{
		StandaloneGVR: &GenericValidator[*enterpriseApi.Standalone]{
			ValidateCreateFunc: func(obj *enterpriseApi.Standalone) field.ErrorList {
				return nil
			},
			WarningsOnCreateFunc: func(obj *enterpriseApi.Standalone) []string {
				return []string{"test warning 1", "test warning 2"}
			},
			GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
		},
	}

	server := NewWebhookServer(WebhookServerOptions{
		Port:       9443,
		Validators: validators,
	})

	ar := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid-warnings",
			Kind: metav1.GroupVersionKind{
				Group:   "enterprise.splunk.com",
				Version: "v4",
				Kind:    "Standalone",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "enterprise.splunk.com",
				Version:  "v4",
				Resource: "standalones",
			},
			Name:      "test-standalone",
			Namespace: "default",
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: mustMarshal(&enterpriseApi.Standalone{
					TypeMeta: metav1.TypeMeta{
						APIVersion: "enterprise.splunk.com/v4",
						Kind:       "Standalone",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-standalone",
						Namespace: "default",
					},
					Spec: enterpriseApi.StandaloneSpec{
						Replicas: 1,
					},
				}),
			},
			UserInfo: authenticationv1.UserInfo{Username: "test-user"},
		},
	}

	bodyBytes, _ := json.Marshal(ar)
	req := httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.handleValidate(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status code %d, got %d", http.StatusOK, rr.Code)
	}

	var response admissionv1.AdmissionReview
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !response.Response.Allowed {
		t.Error("expected allowed=true")
	}

	if len(response.Response.Warnings) != 2 {
		t.Errorf("expected 2 warnings, got %d", len(response.Response.Warnings))
	}
}

func TestWebhookServerOptions(t *testing.T) {
	tests := []struct {
		name     string
		options  WebhookServerOptions
		wantPort int
	}{
		{
			name: "default options",
			options: WebhookServerOptions{
				Port: 9443,
			},
			wantPort: 9443,
		},
		{
			name: "custom port",
			options: WebhookServerOptions{
				Port: 8443,
			},
			wantPort: 8443,
		},
		{
			name: "with cert paths",
			options: WebhookServerOptions{
				Port:        9443,
				TLSCertFile: "/path/to/cert.pem",
				TLSKeyFile:  "/path/to/key.pem",
			},
			wantPort: 9443,
		},
		{
			name: "with cert dir",
			options: WebhookServerOptions{
				Port:    9443,
				CertDir: "/tmp/k8s-webhook-server/serving-certs",
			},
			wantPort: 9443,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewWebhookServer(tt.options)
			if server.options.Port != tt.wantPort {
				t.Errorf("expected port %d, got %d", tt.wantPort, server.options.Port)
			}
		})
	}
}

// Helper functions

func mustMarshal(obj interface{}) []byte {
	data, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return data
}
