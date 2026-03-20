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
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// ValidateStandaloneCreate validates a Standalone on CREATE
func ValidateStandaloneCreate(obj *enterpriseApi.Standalone) field.ErrorList {
	var allErrs field.ErrorList

	// Validate replicas
	if obj.Spec.Replicas < 0 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("replicas"),
			obj.Spec.Replicas,
			"replicas must be non-negative"))
	}

	// Validate common spec
	allErrs = append(allErrs, validateCommonSplunkSpec(&obj.Spec.CommonSplunkSpec, field.NewPath("spec"))...)

	// Validate SmartStore only if user provided config
	if len(obj.Spec.SmartStore.VolList) > 0 || len(obj.Spec.SmartStore.IndexList) > 0 {
		allErrs = append(allErrs, validateSmartStore(&obj.Spec.SmartStore, field.NewPath("spec").Child("smartstore"))...)
	}

	// Validate AppFramework only if user provided config
	if len(obj.Spec.AppFrameworkConfig.VolList) > 0 || len(obj.Spec.AppFrameworkConfig.AppSources) > 0 {
		allErrs = append(allErrs, validateAppFramework(&obj.Spec.AppFrameworkConfig, field.NewPath("spec").Child("appRepo"))...)
	}

	return allErrs
}

// ValidateStandaloneUpdate validates a Standalone on UPDATE
// TODO: Add immutable field validation here (e.g., compare obj vs oldObj for fields that cannot change after creation)
func ValidateStandaloneUpdate(obj, oldObj *enterpriseApi.Standalone) field.ErrorList {
	return ValidateStandaloneCreate(obj)
}

// GetStandaloneWarningsOnCreate returns warnings for Standalone CREATE
func GetStandaloneWarningsOnCreate(obj *enterpriseApi.Standalone) []string {
	return getCommonWarnings(&obj.Spec.CommonSplunkSpec)
}

// GetStandaloneWarningsOnUpdate returns warnings for Standalone UPDATE
func GetStandaloneWarningsOnUpdate(obj, oldObj *enterpriseApi.Standalone) []string {
	return GetStandaloneWarningsOnCreate(obj)
}
