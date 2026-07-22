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

// ValidateIndexerClusterCreate validates an IndexerCluster on CREATE
func ValidateIndexerClusterCreate(obj *enterpriseApi.IndexerCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Validate replicas - IndexerCluster requires minimum 3 replicas
	if obj.Spec.Replicas < 3 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("replicas"),
			obj.Spec.Replicas,
			"IndexerCluster requires at least 3 replicas"))
	}

	// When either queueRef or objectStorageRef is provided, both names must be non-empty.
	// (The CRD CEL rule enforces structural both-or-neither, but not name content.)
	queueRefName := ""
	if obj.Spec.QueueRef != nil {
		queueRefName = obj.Spec.QueueRef.Name
	}
	objectStorageRefName := ""
	if obj.Spec.ObjectStorageRef != nil {
		objectStorageRefName = obj.Spec.ObjectStorageRef.Name
	}
	if queueRefName != "" || objectStorageRefName != "" {
		if queueRefName == "" {
			allErrs = append(allErrs, field.Required(
				field.NewPath("spec").Child("queueRef").Child("name"),
				"queueRef.name is required when objectStorageRef is set"))
		}
		if objectStorageRefName == "" {
			allErrs = append(allErrs, field.Required(
				field.NewPath("spec").Child("objectStorageRef").Child("name"),
				"objectStorageRef.name is required when queueRef is set"))
		}
	}

	// clusterManagerRef is required (clusterMasterRef accepted for backwards compatibility)
	if obj.Spec.ClusterManagerRef.Name == "" && obj.Spec.ClusterMasterRef.Name == "" {
		allErrs = append(allErrs, field.Required(
			field.NewPath("spec").Child("clusterManagerRef").Child("name"),
			"IndexerCluster must reference a ClusterManager via clusterManagerRef"))
	}

	// Cross-namespace ClusterManagerRef is not allowed: the ClusterManager and its
	// IndexerCluster must reside in the same namespace for multisite replication to work.
	effectiveCMRef := obj.Spec.ClusterManagerRef
	if effectiveCMRef.Name == "" {
		effectiveCMRef = obj.Spec.ClusterMasterRef
	}
	if effectiveCMRef.Namespace != "" && effectiveCMRef.Namespace != obj.Namespace {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("clusterManagerRef").Child("namespace"),
			effectiveCMRef.Namespace,
			"clusterManagerRef.namespace must match the IndexerCluster namespace; cross-namespace references are not supported"))
	}

	// Validate common spec
	allErrs = append(allErrs, validateCommonSplunkSpec(&obj.Spec.CommonSplunkSpec, field.NewPath("spec"))...)

	return allErrs
}

// ValidateIndexerClusterCreateWithContext validates an IndexerCluster on CREATE with ValidationContext
func ValidateIndexerClusterCreateWithContext(obj *enterpriseApi.IndexerCluster, vc *ValidationContext) field.ErrorList {
	allErrs := ValidateIndexerClusterCreate(obj)
	if len(obj.Spec.ImagePullSecrets) > 0 {
		allErrs = append(allErrs, ValidateImagePullSecretsExistence(
			obj.Spec.ImagePullSecrets, vc, field.NewPath("spec").Child("imagePullSecrets"))...)
	}
	return allErrs
}

// ValidateIndexerClusterUpdate validates an IndexerCluster on UPDATE
func ValidateIndexerClusterUpdate(obj, oldObj *enterpriseApi.IndexerCluster) field.ErrorList {
	allErrs := ValidateIndexerClusterCreate(obj)

	// queueRef cannot be cleared once it has been set
	oldQueueRefName := ""
	if oldObj != nil && oldObj.Spec.QueueRef != nil {
		oldQueueRefName = oldObj.Spec.QueueRef.Name
	}
	newQueueRefName := ""
	if obj.Spec.QueueRef != nil {
		newQueueRefName = obj.Spec.QueueRef.Name
	}
	if oldQueueRefName != "" && newQueueRefName == "" {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec").Child("queueRef"),
			"queueRef cannot be removed once it has been set; restore the previous value to recover"))
	}

	return allErrs
}

// ValidateIndexerClusterUpdateWithContext validates an IndexerCluster on UPDATE with ValidationContext
func ValidateIndexerClusterUpdateWithContext(obj, oldObj *enterpriseApi.IndexerCluster, vc *ValidationContext) field.ErrorList {
	allErrs := ValidateIndexerClusterUpdate(obj, oldObj)
	if len(obj.Spec.ImagePullSecrets) > 0 {
		allErrs = append(allErrs, ValidateImagePullSecretsExistence(
			obj.Spec.ImagePullSecrets, vc, field.NewPath("spec").Child("imagePullSecrets"))...)
	}
	return allErrs
}

// GetIndexerClusterWarningsOnCreate returns warnings for IndexerCluster CREATE
func GetIndexerClusterWarningsOnCreate(obj *enterpriseApi.IndexerCluster) []string {
	return getCommonWarnings(&obj.Spec.CommonSplunkSpec)
}

// GetIndexerClusterWarningsOnUpdate returns warnings for IndexerCluster UPDATE
func GetIndexerClusterWarningsOnUpdate(obj, oldObj *enterpriseApi.IndexerCluster) []string {
	return GetIndexerClusterWarningsOnCreate(obj)
}
