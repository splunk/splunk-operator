// Copyright (c) 2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validation

import (
	"fmt"
	"slices"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/util"
	"k8s.io/apimachinery/pkg/api/equality"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateKVServiceCreate checks the controllers required to admit a new KVService.
func ValidateKVServiceCreate(obj *enterpriseApi.KVService) field.ErrorList {
	var allErrs field.ErrorList
	if !config.DefaultMutableFeatureGate.Enabled(config.KVService) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec"),
			"the KVService feature is not enabled; set --feature-gates=KVService=true to activate"))
	}
	if !config.DefaultMutableFeatureGate.Enabled(config.PostgresController) {
		allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "postgres"),
			"the PostgresController feature is not enabled; set --feature-gates=PostgresController=true to activate"))
	}
	return append(allErrs, validateKVServiceSpec(obj)...)
}

// ValidateKVServiceCreateWithContext rejects a second KVService in its hosting
// namespace. Admission uses a direct API reader, but simultaneous creates can
// still pass together; reconciliation and keyring ownership guard that race.
func ValidateKVServiceCreateWithContext(obj *enterpriseApi.KVService, vc *ValidationContext) field.ErrorList {
	allErrs := ValidateKVServiceCreate(obj)
	path := field.NewPath("metadata", "name")
	if vc == nil || vc.Client == nil {
		return append(allErrs, field.InternalError(path, fmt.Errorf("cannot check KVService uniqueness without a Kubernetes client")))
	}
	namespace := vc.Namespace
	if namespace == "" {
		namespace = obj.Namespace
	}
	var services enterpriseApi.KVServiceList
	if err := vc.Client.List(vc.Ctx, &services, client.InNamespace(namespace)); err != nil {
		return append(allErrs, field.InternalError(path, fmt.Errorf("list KVServices in namespace %q for admission: %w", namespace, err)))
	}
	if len(services.Items) > 0 {
		names := make([]string, 0, len(services.Items))
		for _, service := range services.Items {
			names = append(names, service.Name)
		}
		slices.Sort(names)
		allErrs = append(allErrs, field.Forbidden(path, fmt.Sprintf("namespace %q already contains KVService %q; only one KVService is allowed per hosting namespace. Remove the existing KVService and wait for deletion to finish before creating a replacement", namespace, names[0])))
	}
	return allErrs
}

// ValidateKVServiceUpdate validates an existing KVService. The feature gate is
// intentionally not checked so existing resources remain manageable if it is disabled.
func ValidateKVServiceUpdate(obj, old *enterpriseApi.KVService) field.ErrorList {
	// Finalizer cleanup must remain possible after feature gates or validation change.
	if !obj.DeletionTimestamp.IsZero() && old != nil && equality.Semantic.DeepEqual(obj.Spec, old.Spec) {
		return nil
	}
	return validateKVServiceSpec(obj)
}

func validateKVServiceSpec(obj *enterpriseApi.KVService) field.ErrorList {
	var allErrs field.ErrorList
	specPath := field.NewPath("spec")

	workload := obj.Spec.CommonWorkloadSpec
	// The CRD enforces the scheme enum and requires certificates for HTTPS.
	// Only active certificate references need additional Secret-name validation.
	if obj.Spec.Scheme == "https" {
		for i, cert := range obj.Spec.Certs {
			if problems := k8svalidation.IsDNS1123Subdomain(cert.SecretRef.Name); len(problems) > 0 {
				allErrs = append(allErrs, field.Invalid(specPath.Child("certs").Index(i).Child("secretRef", "name"), cert.SecretRef.Name, strings.Join(problems, "; ")))
			}
		}
	} else {
		// Certificates are inactive in HTTP mode, including their feature gate.
		workload.Certs = nil
	}
	allErrs = append(allErrs, validateCommonWorkloadSpec(&workload, specPath)...)
	allErrs = append(allErrs, validateKVServiceProbe(obj.Spec.LivenessProbe, specPath.Child("livenessProbe"))...)
	allErrs = append(allErrs, validateKVServiceProbe(obj.Spec.ReadinessProbe, specPath.Child("readinessProbe"))...)
	for i, env := range obj.Spec.ExtraEnv {
		if _, managed := util.KVServiceManagedEnvironmentCategory(env.Name, obj.Spec.Scheme == "https"); managed {
			allErrs = append(allErrs, field.Forbidden(specPath.Child("extraEnv").Index(i).Child("name"),
				"is managed by the operator"))
		}
	}
	return allErrs
}

// validateKVServiceProbe checks submitted timings without applying defaults.
// Zero values are valid: the runtime supplies defaults for omitted timings.
func validateKVServiceProbe(probe *enterpriseApi.Probe, path *field.Path) field.ErrorList {
	if probe == nil {
		return nil
	}
	if err := common.ValidateProbeValues(probe.InitialDelaySeconds, probe.TimeoutSeconds, probe.PeriodSeconds, probe.FailureThreshold); err != nil {
		return field.ErrorList{field.Invalid(path, probe, err.Error())}
	}
	return nil
}
