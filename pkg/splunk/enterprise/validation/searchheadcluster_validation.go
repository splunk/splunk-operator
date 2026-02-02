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

// ValidateSearchHeadClusterCreate validates a SearchHeadCluster on CREATE
func ValidateSearchHeadClusterCreate(obj *enterpriseApi.SearchHeadCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Validate replicas - SearchHeadCluster requires minimum 3 replicas
	if obj.Spec.Replicas < 3 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("replicas"),
			obj.Spec.Replicas,
			"SearchHeadCluster requires at least 3 replicas"))
	}

	// Validate common spec
	allErrs = append(allErrs, validateCommonSplunkSpec(&obj.Spec.CommonSplunkSpec, field.NewPath("spec"))...)

	// Validate AppFramework only if user provided config
	if len(obj.Spec.AppFrameworkConfig.VolList) > 0 || len(obj.Spec.AppFrameworkConfig.AppSources) > 0 {
		allErrs = append(allErrs, validateAppFramework(&obj.Spec.AppFrameworkConfig, field.NewPath("spec").Child("appRepo"))...)
	}

	return allErrs
}

// ValidateSearchHeadClusterUpdate validates a SearchHeadCluster on UPDATE
func ValidateSearchHeadClusterUpdate(obj, oldObj *enterpriseApi.SearchHeadCluster) field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, ValidateSearchHeadClusterCreate(obj)...)
	return allErrs
}

// GetSearchHeadClusterWarningsOnCreate returns warnings for SearchHeadCluster CREATE
func GetSearchHeadClusterWarningsOnCreate(obj *enterpriseApi.SearchHeadCluster) []string {
	var warnings []string
	warnings = append(warnings, getCommonWarnings(&obj.Spec.CommonSplunkSpec)...)
	return warnings
}

// GetSearchHeadClusterWarningsOnUpdate returns warnings for SearchHeadCluster UPDATE
func GetSearchHeadClusterWarningsOnUpdate(obj, oldObj *enterpriseApi.SearchHeadCluster) []string {
	return GetSearchHeadClusterWarningsOnCreate(obj)
}
