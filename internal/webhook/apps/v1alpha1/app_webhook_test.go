/*
Copyright 2021.

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

package v1alpha1

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/go-logr/logr"
	appsv1alpha1 "github.com/splunk/splunk-operator/api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
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

func TestAppValidatorRejectsWrongTypes(t *testing.T) {
	validator := &AppValidator{}
	wrongObj := &corev1.ConfigMap{}

	if _, err := validator.ValidateCreate(context.Background(), wrongObj); err == nil {
		t.Fatalf("expected create validation to reject wrong object type")
	}
	if _, err := validator.ValidateUpdate(context.Background(), wrongObj, &appsv1alpha1.App{}); err == nil {
		t.Fatalf("expected update validation to reject wrong old object type")
	}
	if _, err := validator.ValidateUpdate(context.Background(), &appsv1alpha1.App{}, wrongObj); err == nil {
		t.Fatalf("expected update validation to reject wrong new object type")
	}
	if _, err := validator.ValidateDelete(context.Background(), wrongObj); err == nil {
		t.Fatalf("expected delete validation to reject wrong object type")
	}
}

func TestSetupWebhookWithManager(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := appsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add App scheme: %v", err)
	}

	webhookServer := webhook.NewServer(webhook.Options{Port: 0})
	mgr := &testManager{
		scheme:        scheme,
		client:        fake.NewClientBuilder().WithScheme(scheme).Build(),
		webhookServer: webhookServer,
	}

	if err := SetupWebhookWithManager(mgr); err != nil {
		t.Fatalf("expected setup to succeed, got %v", err)
	}

	handler, path := webhookServer.WebhookMux().Handler(&http.Request{
		URL: &url.URL{Path: AppValidationPath},
	})
	if path != AppValidationPath {
		t.Fatalf("expected handler path %q, got %q", AppValidationPath, path)
	}
	if handler == nil {
		t.Fatalf("expected webhook handler to be registered")
	}
}

var _ manager.Manager = &testManager{}

type testManager struct {
	scheme        *runtime.Scheme
	client        client.Client
	webhookServer webhook.Server
}

func (m *testManager) AddMetricsServerExtraHandler(string, http.Handler) error { return nil }
func (m *testManager) Add(manager.Runnable) error                              { return nil }
func (m *testManager) Elected() <-chan struct{}                                { return nil }
func (m *testManager) AddHealthzCheck(string, healthz.Checker) error           { return nil }
func (m *testManager) AddReadyzCheck(string, healthz.Checker) error            { return nil }
func (m *testManager) Start(context.Context) error                             { return nil }
func (m *testManager) GetWebhookServer() webhook.Server                        { return m.webhookServer }
func (m *testManager) GetLogger() logr.Logger                                  { return logf.Log.WithName("test-manager") }
func (m *testManager) GetControllerOptions() config.Controller                 { return config.Controller{} }
func (m *testManager) GetHTTPClient() *http.Client                             { return http.DefaultClient }
func (m *testManager) GetConfig() *rest.Config                                 { return &rest.Config{} }
func (m *testManager) GetScheme() *runtime.Scheme                              { return m.scheme }
func (m *testManager) GetClient() client.Client                                { return m.client }
func (m *testManager) GetFieldIndexer() client.FieldIndexer                    { return nil }
func (m *testManager) GetCache() cache.Cache                                   { return &informertest.FakeInformers{} }
func (m *testManager) GetEventRecorderFor(string) record.EventRecorder {
	return record.NewFakeRecorder(1)
}
func (m *testManager) GetRESTMapper() meta.RESTMapper { return nil }
func (m *testManager) GetAPIReader() client.Reader    { return m.client }
