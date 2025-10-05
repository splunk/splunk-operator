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

package v4

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/terms"
)

// nolint:unused
// log is for logging in this package.
var standalonelog = logf.Log.WithName("standalone-resource")

// SetupStandaloneWebhookWithManager registers the webhook for Standalone in the manager.
func SetupStandaloneWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&enterprisev4.Standalone{}).
		WithValidator(&StandaloneCustomValidator{}).
		WithDefaulter(&StandaloneCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-enterprise-splunk-com-v4-standalone,mutating=true,failurePolicy=fail,sideEffects=None,groups=enterprise.splunk.com,resources=standalones,verbs=create;update,versions=v4,name=mstandalone-v4.kb.io,admissionReviewVersions=v1

// StandaloneCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Standalone when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type StandaloneCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &StandaloneCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Standalone.
func (d *StandaloneCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	standalone, ok := obj.(*enterprisev4.Standalone)

	if !ok {
		return fmt.Errorf("expected an Standalone object but got %T", obj)
	}
	standalonelog.Info("Defaulting for Standalone", "name", standalone.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-enterprise-splunk-com-v4-standalone,mutating=false,failurePolicy=fail,sideEffects=None,groups=enterprise.splunk.com,resources=standalones,verbs=create;update,versions=v4,name=vstandalone-v4.kb.io,admissionReviewVersions=v1

// StandaloneCustomValidator struct is responsible for validating the Standalone resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type StandaloneCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &StandaloneCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Standalone.
func (v *StandaloneCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	standalone, ok := obj.(*enterprisev4.Standalone)
	if !ok {
		return nil, fmt.Errorf("expected a Standalone object but got %T", obj)
	}
	if !terms.Accepted() {
		// Return a hard deny with a clear fix
		return nil, fmt.Errorf(
			"SPLUNK_GENERAL_TERMS not accepted. Set env var %q to %q on the Splunk Operator Deployment or Helm values, then retry the create",
			terms.EnvVarName, terms.ExpectedFlag,
		)
	}
	standalonelog.Info("Validation for Standalone upon creation", "name", standalone.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Standalone.
func (v *StandaloneCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	standalone, ok := newObj.(*enterprisev4.Standalone)
	if !ok {
		return nil, fmt.Errorf("expected a Standalone object for the newObj but got %T", newObj)
	}
	standalonelog.Info("Validation for Standalone upon update", "name", standalone.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Standalone.
func (v *StandaloneCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	standalone, ok := obj.(*enterprisev4.Standalone)
	if !ok {
		return nil, fmt.Errorf("expected a Standalone object but got %T", obj)
	}
	standalonelog.Info("Validation for Standalone upon deletion", "name", standalone.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
