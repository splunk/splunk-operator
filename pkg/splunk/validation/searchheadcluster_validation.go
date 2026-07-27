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
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
)

// validateInlineSHCDefaultsUpdate is an admission-time qualification guard for
// OPS-008. Splunk requires approximately simultaneous restart when most
// [shclustering] settings change. The current Operator supports only phased
// member replacement, so it must not admit such a change as an ordinary
// OnDelete or RollingUpdate rollout.
//
// This guard is intentionally limited to inline spec.defaults. Reconciliation
// repeats the same classification against observed ConfigMap state when the
// optional admission webhook is disabled. Namespace shc_secret rotation still
// requires a separate controller guard before OPS-008 is complete.
func validateInlineSHCDefaultsUpdate(defaults, oldDefaults string, fldPath *field.Path) field.ErrorList {
	if defaults == oldDefaults {
		return nil
	}

	classification, err := splunkconfig.ClassifySHCDefaultsRestart(
		defaults,
		oldDefaults,
	)
	if err != nil {
		return field.ErrorList{field.Invalid(
			fldPath,
			"<redacted>",
			fmt.Sprintf("cannot classify inline Search Head Cluster configuration restart safety: %v", err),
		)}
	}
	if classification.RequiresSimultaneousRestart {
		return field.ErrorList{field.Forbidden(
			fldPath,
			fmt.Sprintf(
				"changing [shclustering] setting %q requires an approximately simultaneous restart and cannot be treated as an ordinary phased Search Head Cluster rollout",
				classification.Setting,
			),
		)}
	}
	return nil
}

// validateSHCEsAutoSslNotAllowed rejects ES premium-app sources on SearchHeadCluster
// that specify ssl_enablement: auto. The auto mode writes to web.conf on the SHC deployer
// and is not supported; users must choose strict or ignore.
func validateSHCEsAutoSslNotAllowed(appConfig *enterpriseApi.AppFrameworkSpec, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	for i, source := range appConfig.AppSources {
		effectiveScope := source.Scope
		if effectiveScope == "" {
			effectiveScope = appConfig.Defaults.Scope
		}
		if effectiveScope != enterpriseApi.ScopePremiumApps {
			continue
		}
		effectiveType := source.PremiumAppsProps.Type
		if effectiveType == "" {
			effectiveType = appConfig.Defaults.PremiumAppsProps.Type
		}
		if effectiveType != enterpriseApi.PremiumAppsTypeEs {
			continue
		}
		effectiveSsl := source.PremiumAppsProps.EsDefaults.SslEnablement
		if effectiveSsl == "" {
			effectiveSsl = appConfig.Defaults.PremiumAppsProps.EsDefaults.SslEnablement
		}
		if effectiveSsl == enterpriseApi.SslEnablementAuto {
			allErrs = append(allErrs, field.Invalid(
				fldPath.Child("appSources").Index(i).Child("premiumAppsProps").Child("esDefaults").Child("sslEnablement"),
				effectiveSsl,
				fmt.Sprintf("ssl_enablement %q is not supported for Enterprise Security apps on SearchHeadCluster; use %q or %q",
					enterpriseApi.SslEnablementAuto, enterpriseApi.SslEnablementStrict, enterpriseApi.SslEnablementIgnore)))
		}
	}
	return allErrs
}

func validateSearchHeadClusterLifecyclePolicy(policy *enterpriseApi.SearchHeadClusterLifecyclePolicy, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if policy == nil {
		return allErrs
	}

	if !config.DefaultMutableFeatureGate.Enabled(config.SearchHeadClusterLifecycle) {
		return field.ErrorList{field.Forbidden(fldPath,
			"the SearchHeadClusterLifecycle feature is not enabled; set --feature-gates=SearchHeadClusterLifecycle=true to activate")}
	}
	if !config.DefaultMutableFeatureGate.Enabled(config.SplunkPodLifecycle) {
		return field.ErrorList{field.Forbidden(fldPath,
			"SearchHeadClusterLifecycle requires SplunkPodLifecycle=true")}
	}

	switch policy.PodUpdateStrategy {
	case "", enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete,
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate:
	default:
		allErrs = append(allErrs, field.NotSupported(
			fldPath.Child("podUpdateStrategy"),
			policy.PodUpdateStrategy,
			[]string{
				string(enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete),
				string(enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate),
			},
		))
	}

	timeouts := []struct {
		name  string
		value *int64
	}{
		{name: "detentionTimeoutSeconds", value: policy.DetentionTimeoutSeconds},
		{name: "searchDrainTimeoutSeconds", value: policy.SearchDrainTimeoutSeconds},
		{name: "captainTransferTimeoutSeconds", value: policy.CaptainTransferTimeoutSeconds},
		{name: "podStartupTimeoutSeconds", value: policy.PodStartupTimeoutSeconds},
		{name: "memberRejoinTimeoutSeconds", value: policy.MemberRejoinTimeoutSeconds},
	}
	for _, timeout := range timeouts {
		if timeout.value != nil {
			allErrs = append(allErrs, validateLifecycleSeconds(*timeout.value, fldPath.Child(timeout.name))...)
		}
	}

	return allErrs
}

// ValidateSearchHeadClusterCreate validates a SearchHeadCluster on CREATE
func ValidateSearchHeadClusterCreate(obj *enterpriseApi.SearchHeadCluster) field.ErrorList {
	var allErrs field.ErrorList

	// Validate replicas - SearchHeadCluster requires minimum 3 replicas
	if obj.Spec.Replicas < 3 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("replicas"),
			obj.Spec.Replicas,
			"SearchHeadCluster requires at least 3 replicas"))
	}

	// Validate common spec
	allErrs = append(allErrs, validateCommonSplunkSpec(&obj.Spec.CommonSplunkSpec, field.NewPath("spec"))...)
	allErrs = append(allErrs, validateSearchHeadClusterLifecyclePolicy(
		obj.Spec.LifecyclePolicy,
		field.NewPath("spec").Child("lifecyclePolicy"),
	)...)

	// Validate AppFramework only if user provided config
	if len(obj.Spec.AppFrameworkConfig.VolList) > 0 || len(obj.Spec.AppFrameworkConfig.AppSources) > 0 {
		appFldPath := field.NewPath("spec").Child("appRepo")
		allErrs = append(allErrs, validateAppFramework(&obj.Spec.AppFrameworkConfig, appFldPath, false)...)
		allErrs = append(allErrs, validateSHCEsAutoSslNotAllowed(&obj.Spec.AppFrameworkConfig, appFldPath)...)
	}

	return allErrs
}

// ValidateSearchHeadClusterCreateWithContext validates a SearchHeadCluster on CREATE with ValidationContext
func ValidateSearchHeadClusterCreateWithContext(obj *enterpriseApi.SearchHeadCluster, vc *ValidationContext) field.ErrorList {
	allErrs := ValidateSearchHeadClusterCreate(obj)
	if len(obj.Spec.ImagePullSecrets) > 0 {
		allErrs = append(allErrs, ValidateImagePullSecretsExistence(
			obj.Spec.ImagePullSecrets, vc, field.NewPath("spec").Child("imagePullSecrets"))...)
	}
	return allErrs
}

// ValidateSearchHeadClusterUpdate validates a SearchHeadCluster on UPDATE.
func ValidateSearchHeadClusterUpdate(obj, oldObj *enterpriseApi.SearchHeadCluster) field.ErrorList {
	allErrs := ValidateSearchHeadClusterCreate(obj)
	if oldObj == nil {
		return append(allErrs, field.Invalid(
			field.NewPath("spec").Child("defaults"),
			"<redacted>",
			"cannot classify inline Search Head Cluster configuration without the previous object",
		))
	}
	return append(allErrs, validateInlineSHCDefaultsUpdate(
		obj.Spec.Defaults,
		oldObj.Spec.Defaults,
		field.NewPath("spec").Child("defaults"),
	)...)
}

// ValidateSearchHeadClusterUpdateWithContext validates a SearchHeadCluster on UPDATE with ValidationContext
func ValidateSearchHeadClusterUpdateWithContext(obj, oldObj *enterpriseApi.SearchHeadCluster, vc *ValidationContext) field.ErrorList {
	allErrs := ValidateSearchHeadClusterUpdate(obj, oldObj)
	if len(obj.Spec.ImagePullSecrets) > 0 {
		allErrs = append(allErrs, ValidateImagePullSecretsExistence(
			obj.Spec.ImagePullSecrets, vc, field.NewPath("spec").Child("imagePullSecrets"))...)
	}
	return allErrs
}

// GetSearchHeadClusterWarningsOnCreate returns warnings for SearchHeadCluster CREATE
func GetSearchHeadClusterWarningsOnCreate(obj *enterpriseApi.SearchHeadCluster) []string {
	return getCommonWarnings(&obj.Spec.CommonSplunkSpec)
}

// GetSearchHeadClusterWarningsOnUpdate returns warnings for SearchHeadCluster UPDATE
func GetSearchHeadClusterWarningsOnUpdate(obj, oldObj *enterpriseApi.SearchHeadCluster) []string {
	return GetSearchHeadClusterWarningsOnCreate(obj)
}
