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

// ValidateMonitoringConsoleCreate validates a MonitoringConsole on CREATE
func ValidateMonitoringConsoleCreate(obj *enterpriseApi.MonitoringConsole) field.ErrorList {
	var allErrs field.ErrorList

	// Validate common spec
	allErrs = append(allErrs, validateCommonSplunkSpec(&obj.Spec.CommonSplunkSpec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateMonitoringConsoleCreateWithContext validates a MonitoringConsole on CREATE with ValidationContext
func ValidateMonitoringConsoleCreateWithContext(obj *enterpriseApi.MonitoringConsole, vc *ValidationContext) field.ErrorList {
	allErrs := ValidateMonitoringConsoleCreate(obj)
	if len(obj.Spec.ImagePullSecrets) > 0 {
		allErrs = append(allErrs, ValidateImagePullSecretsExistence(
			obj.Spec.ImagePullSecrets, vc, field.NewPath("spec").Child("imagePullSecrets"))...)
	}
	return allErrs
}

// ValidateMonitoringConsoleUpdate validates a MonitoringConsole on UPDATE
// TODO: Add immutable field validation here (e.g., compare obj vs oldObj for fields that cannot change after creation)
func ValidateMonitoringConsoleUpdate(obj, oldObj *enterpriseApi.MonitoringConsole) field.ErrorList {
	return ValidateMonitoringConsoleCreate(obj)
}

// ValidateMonitoringConsoleUpdateWithContext validates a MonitoringConsole on UPDATE with ValidationContext
func ValidateMonitoringConsoleUpdateWithContext(obj, oldObj *enterpriseApi.MonitoringConsole, vc *ValidationContext) field.ErrorList {
	return ValidateMonitoringConsoleCreateWithContext(obj, vc)
}

// GetMonitoringConsoleWarningsOnCreate returns warnings for MonitoringConsole CREATE
func GetMonitoringConsoleWarningsOnCreate(obj *enterpriseApi.MonitoringConsole) []string {
	return getCommonWarnings(&obj.Spec.CommonSplunkSpec)
}

// GetMonitoringConsoleWarningsOnUpdate returns warnings for MonitoringConsole UPDATE
func GetMonitoringConsoleWarningsOnUpdate(obj, oldObj *enterpriseApi.MonitoringConsole) []string {
	return GetMonitoringConsoleWarningsOnCreate(obj)
}
