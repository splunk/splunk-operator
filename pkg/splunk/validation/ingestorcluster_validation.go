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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// ValidateIngestorClusterCreate validates an IngestorCluster on CREATE
func ValidateIngestorClusterCreate(obj *enterpriseApi.IngestorCluster) field.ErrorList {
	var allErrs field.ErrorList

	// queueRef.name is required (the struct field is always present but .Name may be empty)
	if obj.Spec.QueueRef.Name == "" {
		allErrs = append(allErrs, field.Required(
			field.NewPath("spec").Child("queueRef").Child("name"),
			"queueRef.name is required"))
	}

	// objectStorageRef.name is required
	if obj.Spec.ObjectStorageRef.Name == "" {
		allErrs = append(allErrs, field.Required(
			field.NewPath("spec").Child("objectStorageRef").Child("name"),
			"objectStorageRef.name is required"))
	}

	// Validate common spec
	allErrs = append(allErrs, validateCommonSplunkSpec(&obj.Spec.CommonSplunkSpec, field.NewPath("spec"))...)

	// Validate AppFramework only if user provided config (local-only CR)
	if len(obj.Spec.AppFrameworkConfig.VolList) > 0 || len(obj.Spec.AppFrameworkConfig.AppSources) > 0 {
		allErrs = append(allErrs, validateAppFramework(&obj.Spec.AppFrameworkConfig, field.NewPath("spec").Child("appRepo"), true)...)
	}

	return allErrs
}

// ValidateIngestorClusterCreateWithContext validates an IngestorCluster on CREATE with ValidationContext
func ValidateIngestorClusterCreateWithContext(obj *enterpriseApi.IngestorCluster, vc *ValidationContext) field.ErrorList {
	allErrs := ValidateIngestorClusterCreate(obj)
	if len(obj.Spec.ImagePullSecrets) > 0 {
		allErrs = append(allErrs, ValidateImagePullSecretsExistence(
			obj.Spec.ImagePullSecrets, vc, field.NewPath("spec").Child("imagePullSecrets"))...)
	}
	return allErrs
}

// ValidateIngestorClusterUpdate validates an IngestorCluster on UPDATE
func ValidateIngestorClusterUpdate(obj, oldObj *enterpriseApi.IngestorCluster) field.ErrorList {
	return ValidateIngestorClusterCreate(obj)
}

// ValidateIngestorClusterUpdateWithContext validates an IngestorCluster on UPDATE with ValidationContext
func ValidateIngestorClusterUpdateWithContext(obj, oldObj *enterpriseApi.IngestorCluster, vc *ValidationContext) field.ErrorList {
	return ValidateIngestorClusterCreateWithContext(obj, vc)
}

// GetIngestorClusterWarningsOnCreate returns warnings for IngestorCluster CREATE
func GetIngestorClusterWarningsOnCreate(obj *enterpriseApi.IngestorCluster) []string {
	return getCommonWarnings(&obj.Spec.CommonSplunkSpec)
}

// GetIngestorClusterWarningsOnUpdate returns warnings for IngestorCluster UPDATE
func GetIngestorClusterWarningsOnUpdate(obj, oldObj *enterpriseApi.IngestorCluster) []string {
	return GetIngestorClusterWarningsOnCreate(obj)
}
