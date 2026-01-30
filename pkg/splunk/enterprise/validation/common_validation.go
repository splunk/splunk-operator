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
	"regexp"

	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// storageCapacityRegex validates storage capacity format (e.g., "10Gi", "100Gi")
var storageCapacityRegex = regexp.MustCompile(`^[0-9]+Gi$`)

// validateCommonSplunkSpec validates fields common to all Splunk CRDs
func validateCommonSplunkSpec(spec *enterpriseApi.CommonSplunkSpec, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Validate image pull policy if specified
	if spec.ImagePullPolicy != "" {
		validPolicies := []string{"Always", "Never", "IfNotPresent"}
		valid := false
		for _, p := range validPolicies {
			if string(spec.ImagePullPolicy) == p {
				valid = true
				break
			}
		}
		if !valid {
			allErrs = append(allErrs, field.NotSupported(
				fldPath.Child("imagePullPolicy"),
				spec.ImagePullPolicy,
				validPolicies))
		}
	}

	// Validate LivenessInitialDelaySeconds
	if spec.LivenessInitialDelaySeconds < 0 {
		allErrs = append(allErrs, field.Invalid(
			fldPath.Child("livenessInitialDelaySeconds"),
			spec.LivenessInitialDelaySeconds,
			"must be non-negative"))
	}

	// Validate ReadinessInitialDelaySeconds
	if spec.ReadinessInitialDelaySeconds < 0 {
		allErrs = append(allErrs, field.Invalid(
			fldPath.Child("readinessInitialDelaySeconds"),
			spec.ReadinessInitialDelaySeconds,
			"must be non-negative"))
	}

	// Validate EtcVolumeStorageConfig
	allErrs = append(allErrs, validateStorageConfig(&spec.EtcVolumeStorageConfig, fldPath.Child("etcVolumeStorageConfig"))...)

	// Validate VarVolumeStorageConfig
	allErrs = append(allErrs, validateStorageConfig(&spec.VarVolumeStorageConfig, fldPath.Child("varVolumeStorageConfig"))...)

	return allErrs
}

// validateStorageConfig validates storage configuration
func validateStorageConfig(config *enterpriseApi.StorageClassSpec, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Validate storageCapacity format (must be in Gi format, e.g., "10Gi", "100Gi")
	if config.StorageCapacity != "" {
		if !storageCapacityRegex.MatchString(config.StorageCapacity) {
			allErrs = append(allErrs, field.Invalid(
				fldPath.Child("storageCapacity"),
				config.StorageCapacity,
				"must be in Gi format (e.g., '10Gi', '100Gi')"))
		}
	}

	// Validate storageClassName is not empty when ephemeralStorage is false and storageCapacity is set
	if !config.EphemeralStorage && config.StorageCapacity != "" && config.StorageClassName == "" {
		allErrs = append(allErrs, field.Required(
			fldPath.Child("storageClassName"),
			"storageClassName is required when using persistent storage"))
	}

	return allErrs
}

// validateSmartStore validates SmartStore configuration
func validateSmartStore(smartStore *enterpriseApi.SmartStoreSpec, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Validate volume definitions
	for i, vol := range smartStore.VolList {
		volPath := fldPath.Child("volumes").Index(i)
		if vol.Name == "" {
			allErrs = append(allErrs, field.Required(volPath.Child("name"), "volume name is required"))
		}
		if vol.Endpoint == "" && vol.Path == "" {
			allErrs = append(allErrs, field.Required(volPath, "either endpoint or path must be specified"))
		}
	}

	// Validate index definitions
	for i, idx := range smartStore.IndexList {
		idxPath := fldPath.Child("indexes").Index(i)
		if idx.Name == "" {
			allErrs = append(allErrs, field.Required(idxPath.Child("name"), "index name is required"))
		}
		if idx.VolName == "" {
			allErrs = append(allErrs, field.Required(idxPath.Child("volumeName"), "volume name is required for index"))
		}
	}

	return allErrs
}

// validateAppFramework validates App Framework configuration
func validateAppFramework(appConfig *enterpriseApi.AppFrameworkSpec, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// Validate app sources
	for i, source := range appConfig.AppSources {
		sourcePath := fldPath.Child("appSources").Index(i)
		if source.Name == "" {
			allErrs = append(allErrs, field.Required(sourcePath.Child("name"), "app source name is required"))
		}
		if source.Location == "" {
			allErrs = append(allErrs, field.Required(sourcePath.Child("location"), "app source location is required"))
		}
	}

	// Validate volume definitions
	for i, vol := range appConfig.VolList {
		volPath := fldPath.Child("volumes").Index(i)
		if vol.Name == "" {
			allErrs = append(allErrs, field.Required(volPath.Child("name"), "volume name is required"))
		}
	}

	return allErrs
}

// getCommonWarnings returns warnings for common Splunk spec fields
func getCommonWarnings(spec *enterpriseApi.CommonSplunkSpec) []string {
	var warnings []string

	// Warn about deprecated fields or configurations
	// Add warnings as needed based on spec fields

	return warnings
}
