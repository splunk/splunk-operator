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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

func TestGenericValidatorValidateCreate(t *testing.T) {
	tests := []struct {
		name            string
		validator       *GenericValidator[*enterpriseApi.Standalone]
		obj             runtime.Object
		wantErrCount    int
		wantInternalErr bool
	}{
		{
			name: "valid standalone - no errors",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				ValidateCreateFunc: func(obj *enterpriseApi.Standalone) field.ErrorList {
					return nil
				},
				GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       enterpriseApi.StandaloneSpec{Replicas: 1},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid standalone - validation error",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				ValidateCreateFunc: func(obj *enterpriseApi.Standalone) field.ErrorList {
					return field.ErrorList{
						field.Invalid(field.NewPath("spec").Child("replicas"), obj.Spec.Replicas, "must be positive"),
					}
				},
				GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       enterpriseApi.StandaloneSpec{Replicas: -1},
			},
			wantErrCount: 1,
		},
		{
			name: "nil ValidateCreateFunc - returns nil",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				ValidateCreateFunc: nil,
				GroupKind:          schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			wantErrCount: 0,
		},
		{
			name: "wrong type - internal error",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				ValidateCreateFunc: func(obj *enterpriseApi.Standalone) field.ErrorList {
					return nil
				},
				GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
			},
			obj: &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			wantErrCount:    1,
			wantInternalErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.validator.ValidateCreate(tt.obj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateCreate() got %d errors, want %d", len(errs), tt.wantErrCount)
			}
			if tt.wantInternalErr && len(errs) > 0 {
				if errs[0].Type != field.ErrorTypeInternal {
					t.Errorf("ValidateCreate() expected internal error, got %v", errs[0].Type)
				}
			}
		})
	}
}

func TestGenericValidatorValidateUpdate(t *testing.T) {
	tests := []struct {
		name         string
		validator    *GenericValidator[*enterpriseApi.Standalone]
		obj          runtime.Object
		oldObj       runtime.Object
		wantErrCount int
	}{
		{
			name: "valid update - no errors",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				ValidateUpdateFunc: func(obj, oldObj *enterpriseApi.Standalone) field.ErrorList {
					return nil
				},
				GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       enterpriseApi.StandaloneSpec{Replicas: 2},
			},
			oldObj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec:       enterpriseApi.StandaloneSpec{Replicas: 1},
			},
			wantErrCount: 0,
		},
		{
			name: "invalid update - immutable field changed",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				ValidateUpdateFunc: func(obj, oldObj *enterpriseApi.Standalone) field.ErrorList {
					if obj.Name != oldObj.Name {
						return field.ErrorList{
							field.Forbidden(field.NewPath("metadata").Child("name"), "field is immutable"),
						}
					}
					return nil
				},
				GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test-new", Namespace: "default"},
			},
			oldObj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test-old", Namespace: "default"},
			},
			wantErrCount: 1,
		},
		{
			name: "nil ValidateUpdateFunc - returns nil",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				ValidateUpdateFunc: nil,
				GroupKind:          schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			oldObj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.validator.ValidateUpdate(tt.obj, tt.oldObj)
			if len(errs) != tt.wantErrCount {
				t.Errorf("ValidateUpdate() got %d errors, want %d", len(errs), tt.wantErrCount)
			}
		})
	}
}

func TestGenericValidatorGetGroupKind(t *testing.T) {
	validator := &GenericValidator[*enterpriseApi.Standalone]{
		GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
	}

	obj := &enterpriseApi.Standalone{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}

	gk := validator.GetGroupKind(obj)
	if gk.Group != "enterprise.splunk.com" {
		t.Errorf("GetGroupKind() group = %s, want enterprise.splunk.com", gk.Group)
	}
	if gk.Kind != "Standalone" {
		t.Errorf("GetGroupKind() kind = %s, want Standalone", gk.Kind)
	}
}

func TestGenericValidatorGetName(t *testing.T) {
	validator := &GenericValidator[*enterpriseApi.Standalone]{
		GroupKind: schema.GroupKind{Group: "enterprise.splunk.com", Kind: "Standalone"},
	}

	tests := []struct {
		name     string
		obj      runtime.Object
		wantName string
	}{
		{
			name: "valid object",
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "my-standalone", Namespace: "default"},
			},
			wantName: "my-standalone",
		},
		{
			name: "wrong type - returns empty",
			obj: &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-indexer", Namespace: "default"},
			},
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := validator.GetName(tt.obj)
			if name != tt.wantName {
				t.Errorf("GetName() = %s, want %s", name, tt.wantName)
			}
		})
	}
}

func TestGenericValidatorGetWarningsOnCreate(t *testing.T) {
	tests := []struct {
		name         string
		validator    *GenericValidator[*enterpriseApi.Standalone]
		obj          runtime.Object
		wantWarnings int
	}{
		{
			name: "returns warnings",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				WarningsOnCreateFunc: func(obj *enterpriseApi.Standalone) []string {
					return []string{"warning1", "warning2"}
				},
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			wantWarnings: 2,
		},
		{
			name: "nil func - returns nil",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				WarningsOnCreateFunc: nil,
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			wantWarnings: 0,
		},
		{
			name: "wrong type - returns nil",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				WarningsOnCreateFunc: func(obj *enterpriseApi.Standalone) []string {
					return []string{"warning"}
				},
			},
			obj: &enterpriseApi.IndexerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.validator.GetWarningsOnCreate(tt.obj)
			if len(warnings) != tt.wantWarnings {
				t.Errorf("GetWarningsOnCreate() got %d warnings, want %d", len(warnings), tt.wantWarnings)
			}
		})
	}
}

func TestGenericValidatorGetWarningsOnUpdate(t *testing.T) {
	tests := []struct {
		name         string
		validator    *GenericValidator[*enterpriseApi.Standalone]
		obj          runtime.Object
		oldObj       runtime.Object
		wantWarnings int
	}{
		{
			name: "returns warnings",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				WarningsOnUpdateFunc: func(obj, oldObj *enterpriseApi.Standalone) []string {
					return []string{"update warning"}
				},
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			oldObj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			wantWarnings: 1,
		},
		{
			name: "nil func - returns nil",
			validator: &GenericValidator[*enterpriseApi.Standalone]{
				WarningsOnUpdateFunc: nil,
			},
			obj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			oldObj: &enterpriseApi.Standalone{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			},
			wantWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.validator.GetWarningsOnUpdate(tt.obj, tt.oldObj)
			if len(warnings) != tt.wantWarnings {
				t.Errorf("GetWarningsOnUpdate() got %d warnings, want %d", len(warnings), tt.wantWarnings)
			}
		})
	}
}

func TestTypeAssertionError(t *testing.T) {
	err := &TypeAssertionError{
		Expected: &enterpriseApi.Standalone{},
		Actual:   &enterpriseApi.IndexerCluster{},
	}

	if err.Error() != "type assertion failed" {
		t.Errorf("TypeAssertionError.Error() = %s, want 'type assertion failed'", err.Error())
	}
}
