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

// ValidateClusterManagerCreate validates a ClusterManager on CREATE
func ValidateClusterManagerCreate(obj *enterpriseApi.ClusterManager) field.ErrorList {
	var allErrs field.ErrorList

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

// ValidateClusterManagerCreateWithContext validates a ClusterManager on CREATE with ValidationContext
func ValidateClusterManagerCreateWithContext(obj *enterpriseApi.ClusterManager, vc *ValidationContext) field.ErrorList {
	allErrs := ValidateClusterManagerCreate(obj)
	if len(obj.Spec.ImagePullSecrets) > 0 {
		allErrs = append(allErrs, ValidateImagePullSecretsExistence(
			obj.Spec.ImagePullSecrets, vc, field.NewPath("spec").Child("imagePullSecrets"))...)
	}
	return allErrs
}

// ValidateClusterManagerUpdate validates a ClusterManager on UPDATE
// TODO: Add immutable field validation here (e.g., compare obj vs oldObj for fields that cannot change after creation)
func ValidateClusterManagerUpdate(obj, oldObj *enterpriseApi.ClusterManager) field.ErrorList {
	return ValidateClusterManagerCreate(obj)
}

// ValidateClusterManagerUpdateWithContext validates a ClusterManager on UPDATE with ValidationContext
func ValidateClusterManagerUpdateWithContext(obj, oldObj *enterpriseApi.ClusterManager, vc *ValidationContext) field.ErrorList {
	return ValidateClusterManagerCreateWithContext(obj, vc)
}

// GetClusterManagerWarningsOnCreate returns warnings for ClusterManager CREATE
func GetClusterManagerWarningsOnCreate(obj *enterpriseApi.ClusterManager) []string {
	return getCommonWarnings(&obj.Spec.CommonSplunkSpec)
}

// GetClusterManagerWarningsOnUpdate returns warnings for ClusterManager UPDATE
func GetClusterManagerWarningsOnUpdate(obj, oldObj *enterpriseApi.ClusterManager) []string {
	return GetClusterManagerWarningsOnCreate(obj)
}
