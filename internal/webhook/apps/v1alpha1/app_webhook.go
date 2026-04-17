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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "github.com/splunk/splunk-operator/api/apps/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var applog = logf.Log.WithName("app-resource")

const AppValidationPath = "/validate-apps"

// SetupWebhookWithManager registers the webhook for App in the manager.
func SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&appsv1alpha1.App{}).
		WithValidator(&AppValidator{Client: mgr.GetClient()}).
		Complete()
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-apps,mutating=false,failurePolicy=fail,sideEffects=None,groups=apps.splunk.com,resources=apps,verbs=create;update,versions=v1alpha1,name=vapp-v1alpha1.kb.io,admissionReviewVersions=v1

// AppValidator is a scaffold validator for the App resource.
type AppValidator struct {
	Client client.Client
}

// NewAppValidator creates an App validator backed by a Kubernetes client.
func NewAppValidator(k8sClient client.Client) *AppValidator {
	return &AppValidator{Client: k8sClient}
}

func appFromObject(obj runtime.Object) (*appsv1alpha1.App, error) {
	app, ok := obj.(*appsv1alpha1.App)
	if !ok {
		return nil, fmt.Errorf("expected *appsv1alpha1.App but got %T", obj)
	}

	return app, nil
}

// ValidateCreate implements admission.CustomValidator so a webhook will be registered for the type App.
func (v *AppValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	app, err := appFromObject(obj)
	if err != nil {
		return nil, err
	}

	applog.Info("Validation for App upon creation", "name", app.GetName())

	allErrs := ValidateAppCreate(v.Client, app)
	if len(allErrs) > 0 {
		return GetAppWarningsOnCreate(app), apierrors.NewInvalid(v.GetGroupKind(obj), v.GetName(obj), allErrs)
	}

	return GetAppWarningsOnCreate(app), nil
}

// ValidateUpdate implements admission.CustomValidator so a webhook will be registered for the type App.
func (v *AppValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldApp, err := appFromObject(oldObj)
	if err != nil {
		return nil, err
	}

	app, err := appFromObject(newObj)
	if err != nil {
		return nil, err
	}

	applog.Info("Validation for App upon update", "name", app.GetName())

	allErrs := ValidateAppUpdate(v.Client, app, oldApp)
	if len(allErrs) > 0 {
		return GetAppWarningsOnUpdate(app, oldApp), apierrors.NewInvalid(v.GetGroupKind(newObj), v.GetName(newObj), allErrs)
	}

	return GetAppWarningsOnUpdate(app, oldApp), nil
}

// ValidateDelete implements admission.CustomValidator so a webhook will be registered for the type App.
func (v *AppValidator) ValidateDelete(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	app, err := appFromObject(obj)
	if err != nil {
		return nil, err
	}

	applog.Info("Validation for App upon deletion", "name", app.GetName())

	return nil, nil
}

// GetGroupKind returns the GroupKind for App.
func (v *AppValidator) GetGroupKind(runtime.Object) schema.GroupKind {
	return schema.GroupKind{Group: appsv1alpha1.GroupVersion.Group, Kind: "App"}
}

// GetName returns the App name.
func (v *AppValidator) GetName(obj runtime.Object) string {
	app, err := appFromObject(obj)
	if err != nil {
		return ""
	}

	return app.GetName()
}

// GetAppWarningsOnCreate returns warnings for App CREATE.
func GetAppWarningsOnCreate(*appsv1alpha1.App) []string {
	return nil
}

// GetAppWarningsOnUpdate returns warnings for App UPDATE.
func GetAppWarningsOnUpdate(*appsv1alpha1.App, *appsv1alpha1.App) []string {
	return nil
}

// ValidateAppCreate validates an App on CREATE.
func ValidateAppCreate(k8sClient client.Client, obj *appsv1alpha1.App) field.ErrorList {
	return validateApp(context.Background(), k8sClient, obj)
}

// ValidateAppUpdate validates an App on UPDATE.
func ValidateAppUpdate(k8sClient client.Client, obj, _ *appsv1alpha1.App) field.ErrorList {
	return validateApp(context.Background(), k8sClient, obj)
}

func validateApp(ctx context.Context, k8sClient client.Client, app *appsv1alpha1.App) field.ErrorList {
	if k8sClient == nil {
		return field.ErrorList{
			field.InternalError(field.NewPath("spec"), fmt.Errorf("kubernetes client is required for App validation")),
		}
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
		return field.ErrorList{
			field.InternalError(field.NewPath("spec"), fmt.Errorf("failed to validate App uniqueness: %w", err)),
		}
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
				fmt.Sprintf("another App %q already exists in namespace %q with the same targetRef, appID, and scope", other.Name, app.Namespace),
			))
			break
		}
	}

	return allErrs
}
