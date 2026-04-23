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
	"github.com/splunk/splunk-operator/pkg/config"
)

// ValidatePostgresDatabaseCreate validates a PostgresDatabase on CREATE.
func ValidatePostgresDatabaseCreate(obj *enterpriseApi.PostgresDatabase) field.ErrorList {
	var allErrs field.ErrorList

	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec"),
			"the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate"))

		return allErrs
	}

	return allErrs
}

// ValidatePostgresDatabaseUpdate validates a PostgresDatabase on UPDATE.
func ValidatePostgresDatabaseUpdate(obj, oldObj *enterpriseApi.PostgresDatabase) field.ErrorList {
	return ValidatePostgresDatabaseCreate(obj)
}

// GetPostgresDatabaseWarningsOnCreate returns warnings for PostgresDatabase CREATE.
func GetPostgresDatabaseWarningsOnCreate(obj *enterpriseApi.PostgresDatabase) []string {
	return nil
}

// GetPostgresDatabaseWarningsOnUpdate returns warnings for PostgresDatabase UPDATE.
func GetPostgresDatabaseWarningsOnUpdate(obj, oldObj *enterpriseApi.PostgresDatabase) []string {
	return nil
}
