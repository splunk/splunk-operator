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

// ValidateLicenseManagerCreate validates a LicenseManager on CREATE
func ValidateLicenseManagerCreate(obj *enterpriseApi.LicenseManager) field.ErrorList {
	var allErrs field.ErrorList

	// Validate common spec
	allErrs = append(allErrs, validateCommonSplunkSpec(&obj.Spec.CommonSplunkSpec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateLicenseManagerUpdate validates a LicenseManager on UPDATE
// TODO: Add immutable field validation here (e.g., compare obj vs oldObj for fields that cannot change after creation)
func ValidateLicenseManagerUpdate(obj, oldObj *enterpriseApi.LicenseManager) field.ErrorList {
	return ValidateLicenseManagerCreate(obj)
}

// GetLicenseManagerWarningsOnCreate returns warnings for LicenseManager CREATE
func GetLicenseManagerWarningsOnCreate(obj *enterpriseApi.LicenseManager) []string {
	return getCommonWarnings(&obj.Spec.CommonSplunkSpec)
}

// GetLicenseManagerWarningsOnUpdate returns warnings for LicenseManager UPDATE
func GetLicenseManagerWarningsOnUpdate(obj, oldObj *enterpriseApi.LicenseManager) []string {
	return GetLicenseManagerWarningsOnCreate(obj)
}
