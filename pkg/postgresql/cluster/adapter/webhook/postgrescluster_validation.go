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
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	hba "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
)

// ValidatePostgresClusterCreate validates a PostgresCluster on CREATE.
func ValidatePostgresClusterCreate(obj *enterpriseApi.PostgresCluster) field.ErrorList {
	var allErrs field.ErrorList

	if len(obj.Spec.PgHBA) > 0 {
		if err := hba.ValidateRules(obj.Spec.PgHBA); err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("pgHBA"),
				obj.Spec.PgHBA,
				err.Error()))
		}
	}

	return allErrs
}

// ValidatePostgresClusterUpdate validates a PostgresCluster on UPDATE.
func ValidatePostgresClusterUpdate(obj, oldObj *enterpriseApi.PostgresCluster) field.ErrorList {
	return ValidatePostgresClusterCreate(obj)
}

// GetPostgresClusterWarningsOnCreate returns warnings for PostgresCluster CREATE.
func GetPostgresClusterWarningsOnCreate(obj *enterpriseApi.PostgresCluster) []string {
	return nil
}

// GetPostgresClusterWarningsOnUpdate returns warnings for PostgresCluster UPDATE.
func GetPostgresClusterWarningsOnUpdate(obj, oldObj *enterpriseApi.PostgresCluster) []string {
	return nil
}
