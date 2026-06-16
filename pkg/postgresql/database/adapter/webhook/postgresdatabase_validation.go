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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	core "github.com/splunk/splunk-operator/pkg/postgresql/database/core"
)

// ValidatePostgresDatabaseCreate validates a PostgresDatabase on CREATE.
func ValidatePostgresDatabaseCreate(ctx context.Context, obj *enterpriseApi.PostgresDatabase, reader client.Reader) field.ErrorList {
	var allErrs field.ErrorList

	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec"),
			"the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate"))

		return allErrs
	}

	if reader != nil {
		allErrs = append(allErrs, validateExternalDatabaseSecrets(ctx, obj, reader)...)
	}

	return allErrs
}

// ValidatePostgresDatabaseUpdate validates a PostgresDatabase on UPDATE.
func ValidatePostgresDatabaseUpdate(ctx context.Context, obj, oldObj *enterpriseApi.PostgresDatabase, reader client.Reader) field.ErrorList {
	return ValidatePostgresDatabaseCreate(ctx, obj, reader)
}

func validateExternalDatabaseSecrets(ctx context.Context, obj *enterpriseApi.PostgresDatabase, reader client.Reader) field.ErrorList {
	var allErrs field.ErrorList
	for i := range obj.Spec.Databases {
		db := obj.Spec.Databases[i]
		if db.PasswordConfig == nil {
			continue
		}
		base := field.NewPath("spec", "databases").Index(i).Child("passwordConfig")
		refs := []struct {
			name string
			path *field.Path
		}{
			{db.PasswordConfig.ExternalAdminSecretRef.Name, base.Child("externalAdminSecretRef", "name")},
			{db.PasswordConfig.ExternalRWSecretRef.Name, base.Child("externalRWSecretRef", "name")},
		}
		for _, r := range refs {
			if e := validateExternalDatabaseSecret(ctx, obj.Namespace, r.name, r.path, reader); e != nil {
				allErrs = append(allErrs, e)
			}
		}
	}
	return allErrs
}

func validateExternalDatabaseSecret(ctx context.Context, namespace, name string, refPath *field.Path, reader client.Reader) *field.Error {
	if name == "" {
		// Empty ref is already rejected by the CRD's required field; nothing to add.
		return nil
	}

	secret := &corev1.Secret{}
	switch err := reader.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, secret); {
	case apierrors.IsNotFound(err):
		// Strict policy: the referenced secret must already exist at admission.
		return field.Invalid(refPath, name, "referenced external secret does not exist")
	case err != nil:
		return field.InternalError(refPath, err)
	}

	if err := core.ValidateExternalDatabaseSecret(secret, name); err != nil {
		return field.Invalid(refPath, name, err.Error())
	}
	return nil
}

// GetPostgresDatabaseWarningsOnCreate returns warnings for PostgresDatabase CREATE.
func GetPostgresDatabaseWarningsOnCreate(obj *enterpriseApi.PostgresDatabase) []string {
	return nil
}

// GetPostgresDatabaseWarningsOnUpdate returns warnings for PostgresDatabase UPDATE.
func GetPostgresDatabaseWarningsOnUpdate(obj, oldObj *enterpriseApi.PostgresDatabase) []string {
	return nil
}
