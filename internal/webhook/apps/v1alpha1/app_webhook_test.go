// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	"context"
	"testing"

	appsv1alpha1 "github.com/splunk/splunk-operator/api/apps/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAppValidatorAllowsValidObjects(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add App scheme: %v", err)
	}

	validator := NewAppValidator(fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&appsv1alpha1.AppSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "source-a",
				Namespace: "test",
			},
		},
	).Build())
	app := &appsv1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-a",
			Namespace: "test",
		},
		Spec: appsv1alpha1.AppSpec{
			AppID:   "my-app",
			Version: "1.0.0",
			TargetRef: appsv1alpha1.AppTargetRef{
				Kind: "Standalone",
				Name: "target-a",
			},
			SourceRef: appsv1alpha1.AppSourceRef{
				Name: "source-a",
			},
			Package: appsv1alpha1.AppPackageSpec{
				Path: "apps/my-app.tgz",
			},
			Scope: "local",
		},
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "create",
			run: func() error {
				warnings, err := validator.ValidateCreate(context.Background(), app)
				if len(warnings) != 0 {
					t.Fatalf("expected no warnings, got %v", warnings)
				}
				return err
			},
		},
		{
			name: "update",
			run: func() error {
				warnings, err := validator.ValidateUpdate(context.Background(), app, app)
				if len(warnings) != 0 {
					t.Fatalf("expected no warnings, got %v", warnings)
				}
				return err
			},
		},
		{
			name: "delete",
			run: func() error {
				warnings, err := validator.ValidateDelete(context.Background(), app)
				if len(warnings) != 0 {
					t.Fatalf("expected no warnings, got %v", warnings)
				}
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestAppValidatorRejectsMissingAppSource(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add App scheme: %v", err)
	}

	validator := NewAppValidator(fake.NewClientBuilder().WithScheme(scheme).Build())
	app := &appsv1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-a",
			Namespace: "test",
		},
		Spec: appsv1alpha1.AppSpec{
			AppID:   "my-app",
			Version: "1.0.0",
			TargetRef: appsv1alpha1.AppTargetRef{
				Kind: "Standalone",
				Name: "target-a",
			},
			SourceRef: appsv1alpha1.AppSourceRef{
				Name: "missing-source",
			},
			Package: appsv1alpha1.AppPackageSpec{
				Path: "apps/my-app.tgz",
			},
			Scope: "local",
		},
	}

	if _, err := validator.ValidateCreate(context.Background(), app); err == nil || !apierrors.IsInvalid(err) {
		t.Fatalf("expected Invalid validation error, got %v", err)
	}
}

func TestAppValidatorRejectsDuplicateAppTarget(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add App scheme: %v", err)
	}

	existing := &appsv1alpha1.App{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app-existing",
			Namespace: "test",
		},
		Spec: appsv1alpha1.AppSpec{
			AppID:   "my-app",
			Version: "1.0.0",
			TargetRef: appsv1alpha1.AppTargetRef{
				Kind: "Standalone",
				Name: "target-a",
			},
			SourceRef: appsv1alpha1.AppSourceRef{
				Name: "source-a",
			},
			Package: appsv1alpha1.AppPackageSpec{
				Path: "apps/my-app.tgz",
			},
			Scope: "local",
		},
	}

	validator := NewAppValidator(fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&appsv1alpha1.AppSource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "source-a",
				Namespace: "test",
			},
		},
		existing,
	).Build())

	app := existing.DeepCopy()
	app.Name = "app-new"

	if _, err := validator.ValidateCreate(context.Background(), app); err == nil || !apierrors.IsInvalid(err) {
		t.Fatalf("expected Invalid validation error, got %v", err)
	}
}
