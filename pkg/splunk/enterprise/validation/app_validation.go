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
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "github.com/splunk/splunk-operator/api/apps/v1alpha1"
)

// AppValidator validates App resources at admission time.
// This implements the Validator interface and can be used
// with the generic validation registry to route App validation
// requests to this struct.
type AppValidator struct {
	k8sClient client.Client
}

// NewAppValidator creates an App validator backed by a Kubernetes client.
func NewAppValidator(k8sClient client.Client) Validator {
	return &AppValidator{k8sClient: k8sClient}
}

// ValidateCreate validates an App on CREATE.
func (v *AppValidator) ValidateCreate(obj runtime.Object) field.ErrorList {
	app, ok := obj.(*appsv1alpha1.App)
	if !ok {
		return field.ErrorList{field.InternalError(nil,
			&TypeAssertionError{Expected: &appsv1alpha1.App{}, Actual: obj})}
	}

	return ValidateAppCreate(v.k8sClient, app)
}

// ValidateUpdate validates an App on UPDATE.
func (v *AppValidator) ValidateUpdate(obj, oldObj runtime.Object) field.ErrorList {
	app, ok := obj.(*appsv1alpha1.App)
	if !ok {
		return field.ErrorList{field.InternalError(nil,
			&TypeAssertionError{Expected: &appsv1alpha1.App{}, Actual: obj})}
	}

	oldApp, ok := oldObj.(*appsv1alpha1.App)
	if !ok {
		return field.ErrorList{field.InternalError(nil,
			&TypeAssertionError{Expected: &appsv1alpha1.App{}, Actual: oldObj})}
	}

	return ValidateAppUpdate(v.k8sClient, app, oldApp)
}

// GetGroupKind returns the GroupKind for App.
func (v *AppValidator) GetGroupKind(obj runtime.Object) schema.GroupKind {
	return schema.GroupKind{Group: appsv1alpha1.GroupVersion.Group, Kind: "App"}
}

// GetName returns the App name.
func (v *AppValidator) GetName(obj runtime.Object) string {
	app, ok := obj.(*appsv1alpha1.App)
	if !ok {
		return ""
	}

	return app.GetName()
}

// GetWarningsOnCreate returns warnings for App CREATE.
func (v *AppValidator) GetWarningsOnCreate(obj runtime.Object) []string {
	app, ok := obj.(*appsv1alpha1.App)
	if !ok {
		return nil
	}

	return GetAppWarningsOnCreate(app)
}

// GetWarningsOnUpdate returns warnings for App UPDATE.
func (v *AppValidator) GetWarningsOnUpdate(obj, oldObj runtime.Object) []string {
	app, ok := obj.(*appsv1alpha1.App)
	if !ok {
		return nil
	}

	oldApp, ok := oldObj.(*appsv1alpha1.App)
	if !ok {
		return nil
	}

	return GetAppWarningsOnUpdate(app, oldApp)
}

// ValidateAppCreate validates an App on CREATE.
func ValidateAppCreate(k8sClient client.Client, obj *appsv1alpha1.App) field.ErrorList {
	return validateApp(context.Background(), k8sClient, obj)
}

// ValidateAppUpdate validates an App on UPDATE.
func ValidateAppUpdate(k8sClient client.Client, obj, _ *appsv1alpha1.App) field.ErrorList {
	return validateApp(context.Background(), k8sClient, obj)
}

// GetAppWarningsOnCreate returns warnings for App CREATE.
func GetAppWarningsOnCreate(obj *appsv1alpha1.App) []string {
	return nil
}

// GetAppWarningsOnUpdate returns warnings for App UPDATE.
func GetAppWarningsOnUpdate(obj, oldObj *appsv1alpha1.App) []string {
	return nil
}

func validateApp(ctx context.Context, k8sClient client.Client, app *appsv1alpha1.App) field.ErrorList {
	if k8sClient == nil {
		return field.ErrorList{field.InternalError(field.NewPath("spec"), fmt.Errorf("kubernetes client is required for App validation"))}
	}

	var allErrs field.ErrorList
	allErrs = append(allErrs, validateAppSourceRef(ctx, k8sClient, app)...)
	allErrs = append(allErrs, validateAppUniqueness(ctx, k8sClient, app)...)

	return allErrs
}

func validateAppSourceRef(ctx context.Context, k8sClient client.Client, app *appsv1alpha1.App) field.ErrorList {
	var allErrs field.ErrorList

	sourceRefPath := field.NewPath("spec").Child("sourceRef").Child("name")
	key := client.ObjectKey{Name: app.Spec.SourceRef.Name, Namespace: app.Namespace}

	var source appsv1alpha1.AppSource
	if err := k8sClient.Get(ctx, key, &source); err != nil {
		if apierrors.IsNotFound(err) {
			allErrs = append(allErrs, field.NotFound(sourceRefPath, app.Spec.SourceRef.Name))
			return allErrs
		}

		allErrs = append(allErrs, field.InternalError(sourceRefPath, fmt.Errorf("failed to validate AppSource reference: %w", err)))
	}

	return allErrs
}

func validateAppUniqueness(ctx context.Context, k8sClient client.Client, app *appsv1alpha1.App) field.ErrorList {
	var allErrs field.ErrorList

	var appList appsv1alpha1.AppList
	if err := k8sClient.List(ctx, &appList, client.InNamespace(app.Namespace)); err != nil {
		return field.ErrorList{field.InternalError(field.NewPath("spec"), fmt.Errorf("failed to validate App uniqueness: %w", err))}
	}

	for i := range appList.Items {
		other := &appList.Items[i]
		if other.Name == app.Name {
			continue
		}

		if other.Spec.AppID == app.Spec.AppID &&
			other.Spec.Scope == app.Spec.Scope &&
			other.Spec.TargetRef == app.Spec.TargetRef {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec"),
				fmt.Sprintf("%s/%s:%s:%s/%s", app.Namespace, app.Name, app.Spec.AppID, app.Spec.TargetRef.Kind, app.Spec.TargetRef.Name),
				fmt.Sprintf("another App %q already exists in namespace %q with the same targetRef, appID, and scope", other.Name, app.Namespace)))
			break
		}
	}

	return allErrs
}
