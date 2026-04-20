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

package validation

import (
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	hba "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
)

// ValidatePostgresClusterClassCreate validates a PostgresClusterClass on CREATE.
func ValidatePostgresClusterClassCreate(obj *enterpriseApi.PostgresClusterClass) field.ErrorList {
	var allErrs field.ErrorList

	if obj.Spec.Config != nil && len(obj.Spec.Config.PgHBA) > 0 {
		if err := hba.ValidateRules(obj.Spec.Config.PgHBA); err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("config").Child("pgHBA"),
				obj.Spec.Config.PgHBA,
				err.Error()))
		}
	}

	return allErrs
}

// ValidatePostgresClusterClassUpdate validates a PostgresClusterClass on UPDATE.
func ValidatePostgresClusterClassUpdate(obj, oldObj *enterpriseApi.PostgresClusterClass) field.ErrorList {
	return ValidatePostgresClusterClassCreate(obj)
}

// GetPostgresClusterClassWarningsOnCreate returns warnings for PostgresClusterClass CREATE.
func GetPostgresClusterClassWarningsOnCreate(obj *enterpriseApi.PostgresClusterClass) []string {
	return nil
}

// GetPostgresClusterClassWarningsOnUpdate returns warnings for PostgresClusterClass UPDATE.
func GetPostgresClusterClassWarningsOnUpdate(obj, oldObj *enterpriseApi.PostgresClusterClass) []string {
	return nil
}
