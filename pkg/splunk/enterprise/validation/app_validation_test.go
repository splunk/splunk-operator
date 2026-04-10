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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appsv1alpha1 "github.com/splunk/splunk-operator/api/apps/v1alpha1"
)

func TestValidateAppCreate(t *testing.T) {
	tests := []struct {
		name          string
		app           *appsv1alpha1.App
		objects       []client.Object
		wantErrCount  int
		wantErrFields []string
		wantMessage   string
	}{
		{
			name: "valid app",
			app:  newValidApp("app-one"),
			objects: []client.Object{
				newAppSource("source-one"),
			},
			wantErrCount: 0,
		},
		{
			name:         "missing referenced AppSource",
			app:          newValidApp("app-one"),
			wantErrCount: 1,
			wantErrFields: []string{
				"spec.sourceRef.metadata.name",
			},
			wantMessage: "Not found",
		},
		{
			name: "duplicate target appID scope tuple",
			app:  newValidApp("app-two"),
			objects: []client.Object{
				newAppSource("source-one"),
				newValidApp("app-existing"),
			},
			wantErrCount: 1,
			wantErrFields: []string{
				"spec",
			},
			wantMessage: "same targetRef, appID, and scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateAppCreate(newValidationClient(t, tt.objects...), tt.app)
			require.Len(t, errs, tt.wantErrCount)

			for i, wantField := range tt.wantErrFields {
				assert.Equal(t, wantField, errs[i].Field)
			}

			if tt.wantMessage != "" && len(errs) > 0 {
				assert.Contains(t, errs[0].Error(), tt.wantMessage)
			}
		})
	}
}

func TestValidateAppUpdate(t *testing.T) {
	tests := []struct {
		name          string
		app           *appsv1alpha1.App
		oldApp        *appsv1alpha1.App
		objects       []client.Object
		wantErrCount  int
		wantErrFields []string
	}{
		{
			name: "webhook allows mutable field changes",
			app: func() *appsv1alpha1.App {
				app := newValidApp("app-one")
				app.Spec.Version = "2.0.0"
				app.Spec.Package.Path = "apps/sample-app-v2.tgz"
				return app
			}(),
			oldApp: func() *appsv1alpha1.App {
				app := newValidApp("app-one")
				app.Spec.Version = "1.0.0"
				app.Spec.Package.Path = "apps/sample-app-v1.tgz"
				return app
			}(),
			objects: []client.Object{
				newAppSource("source-one"),
			},
			wantErrCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateAppUpdate(newValidationClient(t, tt.objects...), tt.app, tt.oldApp)
			require.Len(t, errs, tt.wantErrCount)

			for i, wantField := range tt.wantErrFields {
				assert.Equal(t, wantField, errs[i].Field)
			}
		})
	}
}

func TestValidateAdmissionReviewForApp(t *testing.T) {
	app := newValidApp("app-one")
	validators := map[schema.GroupVersionResource]Validator{
		AppGVR: NewAppValidator(newValidationClient(t, newAppSource("source-one"))),
	}

	raw, err := json.Marshal(app)
	require.NoError(t, err)

	warnings, err := Validate(&admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID:       "app-test-uid",
			Operation: admissionv1.Create,
			Resource: metav1.GroupVersionResource{
				Group:    appsv1alpha1.GroupVersion.Group,
				Version:  appsv1alpha1.GroupVersion.Version,
				Resource: "apps",
			},
			Object: runtime.RawExtension{Raw: raw},
		},
	}, validators)

	assert.NoError(t, err)
	assert.Empty(t, warnings)
}

func newValidationClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, appsv1alpha1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

func newValidApp(name string) *appsv1alpha1.App {
	return &appsv1alpha1.App{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1alpha1.GroupVersion.String(),
			Kind:       "App",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: appsv1alpha1.AppSpec{
			AppID:   "sample-app",
			Version: "1.0.0",
			TargetRef: appsv1alpha1.AppTargetRef{
				Kind: "Standalone",
				Name: "standalone-sample",
			},
			SourceRef: appsv1alpha1.AppSource{
				TypeMeta: metav1.TypeMeta{
					APIVersion: appsv1alpha1.GroupVersion.String(),
					Kind:       "AppSource",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "source-one",
				},
			},
			Package: appsv1alpha1.AppPackageSpec{
				Path: "apps/sample-app.tgz",
			},
			Scope: "local",
		},
	}
}

func newAppSource(name string) *appsv1alpha1.AppSource {
	return &appsv1alpha1.AppSource{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1alpha1.GroupVersion.String(),
			Kind:       "AppSource",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
		Spec: appsv1alpha1.AppSourceSpec{
			Type: "s3",
			S3: &appsv1alpha1.AppSourceS3Spec{
				Endpoint: "https://s3.amazonaws.com",
			},
		},
	}
}
