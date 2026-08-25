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

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	hba "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
)

// ValidatePostgresClusterClassCreate validates a PostgresClusterClass on CREATE.
func ValidatePostgresClusterClassCreate(obj *platformApi.PostgresClusterClass) field.ErrorList {
	var allErrs field.ErrorList

	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec"),
			"the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate"))

		return allErrs
	}

	if obj.Spec.Config != nil && len(obj.Spec.Config.PgHBA) > 0 {
		pgHBAPath := field.NewPath("spec").Child("config").Child("pgHBA")
		for _, re := range hba.ValidateRules(obj.Spec.Config.PgHBA) {
			allErrs = append(allErrs, field.Invalid(
				pgHBAPath.Index(re.Index),
				obj.Spec.Config.PgHBA[re.Index],
				re.Message))
		}
	}

	return allErrs
}

// ValidatePostgresClusterClassUpdate validates a PostgresClusterClass on UPDATE.
func ValidatePostgresClusterClassUpdate(obj, oldObj *platformApi.PostgresClusterClass) field.ErrorList {
	return ValidatePostgresClusterClassCreate(obj)
}

// GetPostgresClusterClassWarningsOnCreate returns warnings for PostgresClusterClass CREATE.
func GetPostgresClusterClassWarningsOnCreate(obj *platformApi.PostgresClusterClass) []string {
	return nil
}

// GetPostgresClusterClassWarningsOnUpdate returns warnings for PostgresClusterClass UPDATE.
func GetPostgresClusterClassWarningsOnUpdate(obj, oldObj *platformApi.PostgresClusterClass) []string {
	return nil
}
