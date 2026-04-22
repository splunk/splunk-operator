/*
Copyright 2026.

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

package webhook

import (
	"context"
	"strconv"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	hba "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
)

// ValidatePostgresClusterCreate validates a PostgresCluster on CREATE.
func ValidatePostgresClusterCreate(obj *enterpriseApi.PostgresCluster, reader client.Reader) field.ErrorList {
	var allErrs field.ErrorList

	if len(obj.Spec.PgHBA) > 0 {
		pgHBAPath := field.NewPath("spec").Child("pgHBA")
		for _, re := range hba.ValidateRules(obj.Spec.PgHBA) {
			allErrs = append(allErrs, field.Invalid(
				pgHBAPath.Index(re.Index),
				obj.Spec.PgHBA[re.Index],
				re.Message))
		}
	}

	if reader != nil {
		allErrs = append(allErrs, validateAgainstClass(obj, reader)...)
	}

	return allErrs
}

// ValidatePostgresClusterUpdate validates a PostgresCluster on UPDATE.
func ValidatePostgresClusterUpdate(obj, oldObj *enterpriseApi.PostgresCluster, reader client.Reader) field.ErrorList {
	return ValidatePostgresClusterCreate(obj, reader)
}

func validateAgainstClass(obj *enterpriseApi.PostgresCluster, reader client.Reader) field.ErrorList {
	var allErrs field.ErrorList

	class := &enterpriseApi.PostgresClusterClass{}
	if err := reader.Get(context.Background(), client.ObjectKey{Name: obj.Spec.Class}, class); err != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("class"),
			obj.Spec.Class,
			"referenced PostgresClusterClass not found"))
		return allErrs
	}

	classConfig := class.Spec.Config

	mergedInstances := obj.Spec.Instances
	mergedVersion := obj.Spec.PostgresVersion
	mergedStorage := obj.Spec.Storage
	if classConfig != nil {
		if mergedInstances == nil {
			mergedInstances = classConfig.Instances
		}
		if mergedVersion == nil {
			mergedVersion = classConfig.PostgresVersion
		}
		if mergedStorage == nil {
			mergedStorage = classConfig.Storage
		}
	}
	specPath := field.NewPath("spec")
	if mergedInstances == nil {
		allErrs = append(allErrs, field.Required(specPath.Child("instances"),
			"must be set in PostgresCluster or PostgresClusterClass"))
	}
	if mergedVersion == nil {
		allErrs = append(allErrs, field.Required(specPath.Child("postgresVersion"),
			"must be set in PostgresCluster or PostgresClusterClass"))
	}
	if mergedStorage == nil {
		allErrs = append(allErrs, field.Required(specPath.Child("storage"),
			"must be set in PostgresCluster or PostgresClusterClass"))
	}

	if classConfig == nil {
		return allErrs
	}

	if obj.Spec.Storage != nil && classConfig.Storage != nil {
		if obj.Spec.Storage.Cmp(*classConfig.Storage) < 0 {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("storage"),
				obj.Spec.Storage.String(),
				"storage cannot be lower than class default ("+classConfig.Storage.String()+")"))
		}
	}

	if obj.Spec.PostgresVersion != nil && classConfig.PostgresVersion != nil {
		clusterMajor, clusterMinor := parseVersion(*obj.Spec.PostgresVersion)
		classMajor, classMinor := parseVersion(*classConfig.PostgresVersion)
		if clusterMajor > 0 && classMajor > 0 {
			versionTooLow := clusterMajor < classMajor ||
				(clusterMajor == classMajor && classMinor >= 0 && clusterMinor >= 0 && clusterMinor < classMinor)
			if versionTooLow {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec").Child("postgresVersion"),
					*obj.Spec.PostgresVersion,
					"postgresVersion cannot be lower than class default ("+*classConfig.PostgresVersion+")"))
			}
		}
	}

	if obj.Spec.ConnectionPoolerEnabled != nil && *obj.Spec.ConnectionPoolerEnabled {
		classDisabled := classConfig.ConnectionPoolerEnabled == nil || !*classConfig.ConnectionPoolerEnabled
		if classDisabled {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("connectionPoolerEnabled"),
				true,
				"connectionPoolerEnabled cannot be enabled when disabled in PostgresClusterClass"))
		}
	}

	return allErrs
}

func parseVersion(version string) (major, minor int) {
	for i, ch := range version {
		if ch == '.' {
			major, _ = strconv.Atoi(version[:i])
			minor, _ = strconv.Atoi(version[i+1:])
			return major, minor
		}
	}
	major, _ = strconv.Atoi(version)
	return major, -1
}

// GetPostgresClusterWarningsOnCreate returns warnings for PostgresCluster CREATE.
func GetPostgresClusterWarningsOnCreate(obj *enterpriseApi.PostgresCluster) []string {
	return nil
}

// GetPostgresClusterWarningsOnUpdate returns warnings for PostgresCluster UPDATE.
func GetPostgresClusterWarningsOnUpdate(obj, oldObj *enterpriseApi.PostgresCluster) []string {
	return nil
}
