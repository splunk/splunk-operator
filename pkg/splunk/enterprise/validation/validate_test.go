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
	"encoding/json"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

func createStandaloneJSON(name, namespace string, replicas int32) []byte {
	standalone := &enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v4",
			Kind:       "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: enterpriseApi.StandaloneSpec{
			Replicas: replicas,
		},
	}
	data, _ := json.Marshal(standalone)
	return data
}

func createTestValidators() map[schema.GroupVersionResource]Validator {
	return map[schema.GroupVersionResource]Validator{
		StandaloneGVR: &GenericValidator[*enterpriseApi.Standalone]{
			ValidateCreateFunc: func(obj *enterpriseApi.Standalone) field.ErrorList {
				var errs field.ErrorList
				if obj.Spec.Replicas < 0 {
					errs = append(errs, field.Invalid(
						field.NewPath("spec").Child("replicas"),
						obj.Spec.Replicas,
						"must be non-negative"))
				}
				return errs
			},
			ValidateUpdateFunc: func(obj, oldObj *enterpriseApi.Standalone) field.ErrorList {
				var errs field.ErrorList
				if obj.Spec.Replicas < 0 {
					errs = append(errs, field.Invalid(
						field.NewPath("spec").Child("replicas"),
						obj.Spec.Replicas,
						"must be non-negative"))
				}
				return errs
			},
			WarningsOnCreateFunc: func(obj *enterpriseApi.Standalone) []string {
				if obj.Spec.Replicas > 10 {
					return []string{"high replica count may impact performance"}
				}
				return nil
			},
			WarningsOnUpdateFunc: func(obj, oldObj *enterpriseApi.Standalone) []string {
				return nil
			},
			GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
		},
	}
}

func TestValidate(t *testing.T) {
	validators := createTestValidators()

	tests := []struct {
		name         string
		ar           *admissionv1.AdmissionReview
		wantErr      bool
		wantWarnings int
	}{
		{
			name:    "nil admission review",
			ar:      nil,
			wantErr: true,
		},
		{
			name: "nil request",
			ar: &admissionv1.AdmissionReview{
				Request: nil,
			},
			wantErr: true,
		},
		{
			name: "valid CREATE operation",
			ar: &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Object: runtime.RawExtension{
						Raw: createStandaloneJSON("test", "default", 1),
					},
				},
			},
			wantErr:      false,
			wantWarnings: 0,
		},
		{
			name: "invalid CREATE - negative replicas",
			ar: &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Object: runtime.RawExtension{
						Raw: createStandaloneJSON("test", "default", -1),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "CREATE with warnings",
			ar: &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Object: runtime.RawExtension{
						Raw: createStandaloneJSON("test", "default", 15),
					},
				},
			},
			wantErr:      false,
			wantWarnings: 1,
		},
		{
			name: "valid UPDATE operation",
			ar: &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Update,
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Object: runtime.RawExtension{
						Raw: createStandaloneJSON("test", "default", 2),
					},
					OldObject: runtime.RawExtension{
						Raw: createStandaloneJSON("test", "default", 1),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "DELETE operation - allowed by default",
			ar: &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Delete,
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Object: runtime.RawExtension{
						Raw: createStandaloneJSON("test", "default", 1),
					},
				},
			},
			wantErr: false,
		},
		{
			name: "unknown resource - no validator",
			ar: &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "unknownresources",
					},
					Object: runtime.RawExtension{
						Raw: []byte(`{}`),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid JSON - deserialization error",
			ar: &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Object: runtime.RawExtension{
						Raw: []byte(`{invalid json`),
					},
				},
			},
			wantErr: true,
		},
		{
			name: "empty object - deserialization error",
			ar: &admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID:       "test-uid",
					Operation: admissionv1.Create,
					Resource: metav1.GroupVersionResource{
						Group:    "enterprise.splunk.com",
						Version:  "v4",
						Resource: "standalones",
					},
					Object: runtime.RawExtension{
						Raw: []byte{},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings, err := Validate(tt.ar, validators)

			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(warnings) != tt.wantWarnings {
				t.Errorf("Validate() warnings = %d, want %d", len(warnings), tt.wantWarnings)
			}
		})
	}
}

func TestDeserializeObject(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		wantErr bool
	}{
		{
			name:    "valid standalone JSON",
			raw:     createStandaloneJSON("test", "default", 1),
			wantErr: false,
		},
		{
			name:    "empty bytes",
			raw:     []byte{},
			wantErr: true,
		},
		{
			name:    "nil bytes",
			raw:     nil,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			raw:     []byte(`{not valid json`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := deserializeObject(tt.raw)

			if (err != nil) != tt.wantErr {
				t.Errorf("deserializeObject() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && obj == nil {
				t.Error("deserializeObject() returned nil object without error")
			}
		})
	}
}
