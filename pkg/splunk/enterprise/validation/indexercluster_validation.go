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

// ValidateIndexerClusterCreate validates an IndexerCluster on CREATE
func ValidateIndexerClusterCreate(obj *enterpriseApi.IndexerCluster) field.ErrorList {
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

	return allErrs
}

// ValidateIndexerClusterUpdate validates an IndexerCluster on UPDATE
func ValidateIndexerClusterUpdate(obj, oldObj *enterpriseApi.IndexerCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Run create validations first
	allErrs = append(allErrs, ValidateIndexerClusterCreate(obj)...)

	return allErrs
}

// GetIndexerClusterWarningsOnCreate returns warnings for IndexerCluster CREATE
func GetIndexerClusterWarningsOnCreate(obj *enterpriseApi.IndexerCluster) []string {
	var warnings []string
	warnings = append(warnings, getCommonWarnings(&obj.Spec.CommonSplunkSpec)...)
	return warnings
}

// GetIndexerClusterWarningsOnUpdate returns warnings for IndexerCluster UPDATE
func GetIndexerClusterWarningsOnUpdate(obj, oldObj *enterpriseApi.IndexerCluster) []string {
	return GetIndexerClusterWarningsOnCreate(obj)
}
