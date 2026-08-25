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
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformApi "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/config"
	core "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	cnpgadapter "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	pgcnpg "github.com/splunk/splunk-operator/pkg/postgresql/shared/cnpg"
)

// recoveryBackend is the provisioner capability oracle the webhook validates
// recovery plans against. It is stateless (a pure capability check), so a single
// shared instance is safe. The operator targets CNPG, so the webhook binds the
// CNPG adapter here; core stays provisioner-agnostic behind the RecoveryBackend port.
var recoveryBackend = cnpgadapter.NewRecoveryBackend()

// ValidatePostgresClusterCreate validates a PostgresCluster on CREATE.
func ValidatePostgresClusterCreate(ctx context.Context, obj *platformApi.PostgresCluster, reader client.Reader) field.ErrorList {
	return validatePostgresCluster(ctx, obj, nil, reader, false)
}

// ValidatePostgresClusterUpdate validates a PostgresCluster on UPDATE.
func ValidatePostgresClusterUpdate(ctx context.Context, obj, oldObj *platformApi.PostgresCluster, reader client.Reader) field.ErrorList {
	specUnchanged := oldObj != nil && reflect.DeepEqual(obj.Spec, oldObj.Spec)
	return validatePostgresCluster(ctx, obj, oldObj, reader, specUnchanged)
}

func validatePostgresCluster(ctx context.Context, obj, oldObj *platformApi.PostgresCluster, reader client.Reader, specUnchanged bool) field.ErrorList {
	var allErrs field.ErrorList

	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		allErrs = append(allErrs, field.Forbidden(
			field.NewPath("spec"),
			"the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate"))

		return allErrs
	}

	if len(obj.Spec.PgHBA) > 0 {
		pgHBAPath := field.NewPath("spec").Child("pgHBA")
		for _, re := range core.ValidateRules(obj.Spec.PgHBA) {
			allErrs = append(allErrs, field.Invalid(
				pgHBAPath.Index(re.Index),
				obj.Spec.PgHBA[re.Index],
				re.Message))
		}
	}
	liveReader := reader
	if obj.GetDeletionTimestamp() != nil {
		liveReader = nil
	}
	metricsReader := liveReader
	if oldObj != nil && clusterMonitoringSelectorsUnchanged(obj, oldObj) {
		metricsReader = nil
	}
	allErrs = append(allErrs, validateCustomMetrics(ctx, obj, metricsReader)...)

	if liveReader != nil {
		allErrs = append(allErrs, validateAgainstClass(ctx, obj, oldObj, liveReader, specUnchanged)...)
		if e := validateExternalSuperuserSecret(ctx, obj, liveReader); e != nil {
			allErrs = append(allErrs, e)
		}
	}

	return allErrs
}

func validateCustomMetrics(ctx context.Context, obj *platformApi.PostgresCluster, reader client.Reader) field.ErrorList {
	if obj.Spec.Monitoring == nil {
		return nil
	}
	basePath := field.NewPath("spec", "monitoring", "customQueriesConfigMap")
	var allErrs field.ErrorList

	for i := range obj.Spec.Monitoring.CustomQueriesConfigMap {
		refPath := basePath.Index(i)
		if obj.Spec.Monitoring.CustomQueriesConfigMap[i].Optional != nil {
			allErrs = append(allErrs, field.Invalid(
				refPath.Child("optional"),
				*obj.Spec.Monitoring.CustomQueriesConfigMap[i].Optional,
				fmt.Sprintf("optional is not supported for ConfigMap %q; omit the field because custom-metrics sources are required",
					obj.Spec.Monitoring.CustomQueriesConfigMap[i].Name)))
		}
		name := obj.Spec.Monitoring.CustomQueriesConfigMap[i].Name
		if name == "" || reader == nil {
			continue
		}
		cm := &corev1.ConfigMap{}
		namePath := refPath.Child("name")
		switch err := reader.Get(ctx, client.ObjectKey{Name: name, Namespace: obj.Namespace}, cm); {
		case apierrors.IsNotFound(err):
			allErrs = append(allErrs, field.Invalid(namePath, name, "referenced custom-metrics ConfigMap does not exist"))
		case err != nil:
			allErrs = append(allErrs, field.InternalError(namePath, err))
		}
	}
	return allErrs
}

func clusterMonitoringSelectorsUnchanged(obj, oldObj *platformApi.PostgresCluster) bool {
	oldRefs := func() []corev1.ConfigMapKeySelector {
		if oldObj == nil || oldObj.Spec.Monitoring == nil {
			return nil
		}
		return oldObj.Spec.Monitoring.CustomQueriesConfigMap
	}()
	newRefs := func() []corev1.ConfigMapKeySelector {
		if obj.Spec.Monitoring == nil {
			return nil
		}
		return obj.Spec.Monitoring.CustomQueriesConfigMap
	}()
	if len(oldRefs) != len(newRefs) {
		return false
	}
	for i := range oldRefs {
		if oldRefs[i].Name != newRefs[i].Name || oldRefs[i].Key != newRefs[i].Key {
			return false
		}
	}
	return true
}

func validateExternalSuperuserSecret(ctx context.Context, obj *platformApi.PostgresCluster, reader client.Reader) *field.Error {
	if obj.Spec.PasswordConfig == nil {
		return nil
	}
	ref := obj.Spec.PasswordConfig.SuperuserExternalSecretRef.Name
	refPath := field.NewPath("spec", "passwordConfig", "superuserExternalSecretRef", "name")
	if ref == "" {
		// Empty ref is already rejected by the CRD's required field; nothing to add.
		return nil
	}

	secret := &corev1.Secret{}
	switch err := reader.Get(ctx, client.ObjectKey{Name: ref, Namespace: obj.Namespace}, secret); {
	case apierrors.IsNotFound(err):
		return field.Invalid(refPath, ref, "referenced external superuser secret does not exist")
	case err != nil:
		return field.InternalError(refPath, err)
	}

	if err := core.ValidateExternalSuperuserSecret(secret); err != nil {
		return field.Invalid(refPath, ref, err.Error())
	}
	return nil
}

func validateAgainstClass(ctx context.Context, obj, oldObj *platformApi.PostgresCluster, reader client.Reader, specUnchanged bool) field.ErrorList {
	var allErrs field.ErrorList

	class := &platformApi.PostgresClusterClass{}
	if err := reader.Get(ctx, client.ObjectKey{Name: obj.Spec.Class}, class); err != nil {
		if apierrors.IsNotFound(err) && specUnchanged {
			return nil
		}
		classPath := field.NewPath("spec").Child("class")
		if apierrors.IsNotFound(err) {
			allErrs = append(allErrs, field.Invalid(classPath, obj.Spec.Class,
				"referenced PostgresClusterClass not found"))
		} else {
			allErrs = append(allErrs, field.InternalError(classPath,
				fmt.Errorf("failed to look up PostgresClusterClass %q: %w", obj.Spec.Class, err)))
		}
		return allErrs
	}

	allErrs = append(allErrs, toFieldErrors(core.ValidateMergedConfig(core.GetMergedConfig(class, obj), class.Name))...)
	allErrs = append(allErrs, toFieldErrors(core.ValidateCrossResource(class, obj))...)
	allErrs = append(allErrs, toFieldErrors(core.ValidateRecoveryCapabilities(recoveryBackend, class, obj))...)
	allErrs = append(allErrs, validateStorageTransition(class, obj, oldObj)...)
	allErrs = append(allErrs, validatePoolerEndpoints(class, obj)...)
	return allErrs
}

func validateStorageTransition(class *platformApi.PostgresClusterClass, obj, oldObj *platformApi.PostgresCluster) field.ErrorList {
	if oldObj == nil {
		return nil
	}

	oldStorage := core.GetMergedConfig(class, oldObj).Spec.Storage
	newStorage := core.GetMergedConfig(class, obj).Spec.Storage
	if oldStorage == nil || newStorage == nil || oldStorage.Cmp(*newStorage) <= 0 {
		return nil
	}

	return field.ErrorList{field.Invalid(
		field.NewPath("spec").Child("storage"),
		newStorage.String(),
		fmt.Sprintf("storage size cannot be decreased (from: %s, to: %s)", oldStorage.String(), newStorage.String()),
	)}
}

// validatePoolerEndpoints is admission-only by design: the reconciler suppresses
// the RO pooler at instances<2 rather than failing, so this fail-fast lives here
// (not in ValidateCrossResource) to reject explicit readOnly=true the cluster
// can never satisfy.
func validatePoolerEndpoints(class *platformApi.PostgresClusterClass, obj *platformApi.PostgresCluster) field.ErrorList {
	merged := core.GetMergedConfig(class, obj)
	if !core.PoolerReadOnlyRequested(merged) {
		return nil
	}
	if merged.Spec.Instances != nil && *merged.Spec.Instances < pgcnpg.MinInstancesForReadOnly {
		return field.ErrorList{field.Invalid(
			field.NewPath("spec").Child("connectionPooler").Child("readOnly"),
			true,
			fmt.Sprintf("connectionPooler.readOnly cannot be true when effective instances=%d (requires >= %d)", *merged.Spec.Instances, pgcnpg.MinInstancesForReadOnly),
		)}
	}
	return nil
}

func toFieldErrors(errs []core.ConfigValidationError) field.ErrorList {
	var out field.ErrorList
	for _, e := range errs {
		out = append(out, field.Invalid(field.NewPath(e.Field), e.Value, e.Message))
	}
	return out
}

// GetPostgresClusterWarningsOnCreate returns warnings for PostgresCluster CREATE.
func GetPostgresClusterWarningsOnCreate(obj *platformApi.PostgresCluster) []string {
	return nil
}

// GetPostgresClusterWarningsOnUpdate returns warnings for PostgresCluster UPDATE.
func GetPostgresClusterWarningsOnUpdate(obj, oldObj *platformApi.PostgresCluster) []string {
	return nil
}
