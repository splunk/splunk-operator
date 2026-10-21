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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
)

func TestNewWebhookServer(t *testing.T) {
	options := WebhookServerOptions{
		Validators: DefaultValidators,
	}

	server := NewWebhookServer(options)

	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if len(server.options.Validators) != len(DefaultValidators) {
		t.Errorf("expected %d validators, got %d", len(DefaultValidators), len(server.options.Validators))
	}
}

func TestHandleValidateFreezesPostgresDatabaseWithStaleManagerCache(t *testing.T) {
	featuregatetesting.SetFeatureGatesDuringTest(t, config.DefaultMutableFeatureGate, featuregatetesting.FeatureOverrides{
		config.ValidationWebhook: true, config.PostgresController: true, config.CrossVersionFieldProtection: false,
	})
	scheme := runtime.NewScheme()
	require.NoError(t, platformApi.AddToScheme(scheme))
	cluster := &platformApi.PostgresCluster{ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default"}}
	cluster.Status.PostgresMajorUpgradeStatus = []platformApi.PostgresMajorUpgradeStatus{{BlueGreen: &platformApi.PostgresBlueGreenUpgradeStatus{
		AttemptID: "attempt-1", Blue: &platformApi.BlueGreenEnvironmentStatus{},
	}}}
	cached := cluster.DeepCopy()
	cached.Status.PostgresMajorUpgradeStatus = nil
	server := NewWebhookServer(WebhookServerOptions{Validators: DefaultValidators, Client: NewDirectReadClient(
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(cached).Build(),
		fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build(),
	)})
	oldDatabase := &platformApi.PostgresDatabase{TypeMeta: metav1.TypeMeta{APIVersion: platformApi.GroupVersion.String(), Kind: "PostgresDatabase"}, ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "default"}, Spec: platformApi.PostgresDatabaseSpec{
		ClusterRef: corev1.LocalObjectReference{Name: cluster.Name}, Databases: []platformApi.DatabaseDefinition{{Name: "orders"}},
	}}
	newDatabase := oldDatabase.DeepCopy()
	newDatabase.Spec.Databases = append(newDatabase.Spec.Databases, platformApi.DatabaseDefinition{Name: "extra"})
	oldRaw, err := json.Marshal(oldDatabase)
	require.NoError(t, err)
	newRaw, err := json.Marshal(newDatabase)
	require.NoError(t, err)
	request := &admissionv1.AdmissionReview{TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"}, Request: &admissionv1.AdmissionRequest{
		UID: "freeze-test", Kind: metav1.GroupVersionKind{Group: PostgresDatabaseGVR.Group, Version: PostgresDatabaseGVR.Version, Kind: "PostgresDatabase"},
		Resource:  metav1.GroupVersionResource{Group: PostgresDatabaseGVR.Group, Version: PostgresDatabaseGVR.Version, Resource: PostgresDatabaseGVR.Resource},
		Operation: admissionv1.Update, Name: oldDatabase.Name, Namespace: oldDatabase.Namespace,
		Object: runtime.RawExtension{Raw: newRaw}, OldObject: runtime.RawExtension{Raw: oldRaw},
	}}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	server.HandleValidate(response, httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, response.Code)
	var review admissionv1.AdmissionReview
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &review))
	require.NotNil(t, review.Response)
	assert.False(t, review.Response.Allowed)
	assert.Contains(t, review.Response.Result.Message, "frozen")
}

func TestHandleValidate(t *testing.T) {
	featuregatetesting.SetFeatureGatesDuringTest(t, config.DefaultMutableFeatureGate, featuregatetesting.FeatureOverrides{
		config.ValidationWebhook:           true,
		config.CrossVersionFieldProtection: false,
	})
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
						Raw: mustMarshal(t, &enterpriseApi.Standalone{
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
						Raw: mustMarshal(t, &enterpriseApi.Standalone{
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
			server.HandleValidate(rr, req)

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

func TestHandleValidateWithWarnings(t *testing.T) {
	featuregatetesting.SetFeatureGatesDuringTest(t, config.DefaultMutableFeatureGate, featuregatetesting.FeatureOverrides{
		config.ValidationWebhook:           true,
		config.CrossVersionFieldProtection: false,
	})
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
				Raw: mustMarshal(t, &enterpriseApi.Standalone{
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
	server.HandleValidate(rr, req)

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

// Helper functions

func mustMarshal(t *testing.T, obj interface{}) []byte {
	t.Helper()
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatalf("failed to marshal object: %v", err)
	}
	return data
}

func TestHandleValidateCrossVersion(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, config.DefaultMutableFeatureGate, config.ValidationWebhook, true)
	const (
		oldObject       = `{"metadata":{"namespace":"splunk","name":"idx","resourceVersion":"5"}}`
		oldObjectStale  = `{"metadata":{"namespace":"splunk","name":"idx","resourceVersion":"4"}}`
		strippedV4IDX   = `{"apiVersion":"enterprise.splunk.com/v4","kind":"IndexerCluster","metadata":{"namespace":"splunk","name":"idx","resourceVersion":"5"}}`
		idxUnsupported  = "IndexerCluster splunk/idx cannot be represented in apiVersion enterprise.splunk.com/v3: spec.noahClusterRef set but unsupported in v3. Use enterprise.splunk.com/v4 to read or modify this resource"
		idxConflict     = `Operation cannot be fulfilled on indexerclusters.enterprise.splunk.com "idx": the object has been modified; please apply your changes to the latest version and try again`
		strippedInvalid = `IndexerCluster.enterprise.splunk.com "idx" is invalid: [spec.replicas: Invalid value: 0: IndexerCluster requires at least 3 replicas, spec.clusterManagerRef.name: Required value: IndexerCluster must reference a ClusterManager via clusterManagerRef]`
	)
	v3IDX := metav1.GroupVersionResource{Group: "enterprise.splunk.com", Version: "v3", Resource: "indexerclusters"}
	v3SHC := metav1.GroupVersionResource{Group: "enterprise.splunk.com", Version: "v3", Resource: "searchheadclusters"}
	v4IDX := metav1.GroupVersionResource{Group: "enterprise.splunk.com", Version: "v4", Resource: "indexerclusters"}
	noahIDX := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "splunk", Name: "idx", ResourceVersion: "5"},
		Spec:       enterpriseApi.IndexerClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
	}
	classicIDX := &enterpriseApi.IndexerCluster{
		ObjectMeta: metav1.ObjectMeta{Namespace: "splunk", Name: "idx", ResourceVersion: "5"},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{ClusterManagerRef: corev1.ObjectReference{Name: "cm"}},
		},
	}
	v3Update := admissionv1.AdmissionRequest{
		Operation: admissionv1.Update, Resource: v3IDX, RequestResource: &v3IDX,
		Namespace: "splunk", Name: "idx", OldObject: runtime.RawExtension{Raw: []byte(oldObject)},
	}
	relabelledUpdate := admissionv1.AdmissionRequest{
		Operation: admissionv1.Update, Resource: v4IDX, RequestResource: &v3IDX, Namespace: "splunk", Name: "idx",
		Object: runtime.RawExtension{Raw: []byte(strippedV4IDX)}, OldObject: runtime.RawExtension{Raw: []byte(strippedV4IDX)},
	}
	staleUpdate := v3Update
	staleUpdate.OldObject = runtime.RawExtension{Raw: []byte(oldObjectStale)}
	shcUpdate := v3Update
	shcUpdate.Resource, shcUpdate.RequestResource = v3SHC, &v3SHC
	v4Update := relabelledUpdate
	v4Update.RequestResource = &v4IDX
	guardOff := featuregatetesting.FeatureOverrides{config.CrossVersionFieldProtection: false}

	tests := []struct {
		name        string
		gates       featuregatetesting.FeatureOverrides
		noGuard     bool
		stored      client.Object
		req         admissionv1.AdmissionRequest
		wantPanic   bool
		wantAllowed bool
		wantCode    int32
		wantMessage string
	}{
		{name: "gate off allows a v3 indexercluster update", gates: guardOff, noGuard: true, req: v3Update, wantAllowed: true, wantCode: http.StatusOK},
		{name: "gate off allows a v3 searchheadcluster update", gates: guardOff, noGuard: true, req: shcUpdate, wantAllowed: true, wantCode: http.StatusOK},
		{name: "guard denies a v3 update", stored: noahIDX, req: v3Update, wantCode: http.StatusUnprocessableEntity, wantMessage: idxUnsupported},
		{name: "guard allows a v3 update without running typed validation", stored: classicIDX, req: v3Update, wantAllowed: true, wantCode: http.StatusOK},
		{name: "guard message wins over the v4 validator on a relabelled update", stored: noahIDX, req: relabelledUpdate, wantCode: http.StatusUnprocessableEntity, wantMessage: idxUnsupported},
		{name: "gate off sends a relabelled update to the v4 validator", gates: guardOff, noGuard: true, req: relabelledUpdate, wantCode: http.StatusUnprocessableEntity, wantMessage: strippedInvalid},
		{name: "guard conflict is returned as 409", stored: noahIDX, req: staleUpdate, wantCode: http.StatusConflict, wantMessage: idxConflict},
		{
			name: "ValidationWebhook off skips typed validation", gates: featuregatetesting.FeatureOverrides{config.ValidationWebhook: false},
			req: v4Update, wantAllowed: true, wantCode: http.StatusOK,
		},
		{name: "gate on without a guard panics", noGuard: true, req: v3Update, wantPanic: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGatesDuringTest(t, config.DefaultMutableFeatureGate, tt.gates)
			options := WebhookServerOptions{Validators: DefaultValidators}
			if !tt.noGuard {
				builder := fake.NewClientBuilder().WithScheme(scheme)
				if tt.stored != nil {
					builder = builder.WithObjects(tt.stored.DeepCopyObject().(client.Object))
				}
				options.CrossVersionGuard = NewCrossVersionFieldGuard(builder.Build())
			}
			server := NewWebhookServer(options)

			req := tt.req
			body, err := json.Marshal(&admissionv1.AdmissionReview{
				TypeMeta: metav1.TypeMeta{APIVersion: "admission.k8s.io/v1", Kind: "AdmissionReview"},
				Request:  &req,
			})
			require.NoError(t, err)
			rr := httptest.NewRecorder()
			serve := func() {
				server.HandleValidate(rr, httptest.NewRequest(http.MethodPost, "/validate", bytes.NewReader(body)))
			}

			if tt.wantPanic {
				assert.Panics(t, serve)
				return
			}
			serve()
			require.Equal(t, http.StatusOK, rr.Code)

			var review admissionv1.AdmissionReview
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &review))
			require.NotNil(t, review.Response)
			assert.Equal(t, tt.wantAllowed, review.Response.Allowed)
			assert.Equal(t, tt.wantCode, review.Response.Result.Code)
			assert.Equal(t, tt.wantMessage, review.Response.Result.Message)
		})
	}
}
