// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package standalone

import (
	"context"
	"fmt"
	"os"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
)

// validateProbe checks that all configured probe timing values are non-negative.
func validateProbe(probe *enterpriseApi.Probe) error {
	if probe.InitialDelaySeconds < 0 || probe.TimeoutSeconds < 0 || probe.PeriodSeconds < 0 || probe.FailureThreshold < 0 {
		return fmt.Errorf("negative values are not allowed. Configured values InitialDelaySeconds = %d, TimeoutSeconds = %d, PeriodSeconds = %d, FailureThreshold = %d", probe.InitialDelaySeconds, probe.TimeoutSeconds, probe.PeriodSeconds, probe.FailureThreshold)
	}
	return nil
}

func validateLivenessProbe(_ context.Context, _ splcommon.MetaObject, probe *enterpriseApi.Probe) error {
	if probe == nil {
		return nil
	}
	if err := validateProbe(probe); err != nil {
		return fmt.Errorf("invalid Liveness Probe config. Reason: %s", err)
	}
	return nil
}

func validateReadinessProbe(_ context.Context, _ splcommon.MetaObject, probe *enterpriseApi.Probe) error {
	if probe == nil {
		return nil
	}
	if err := validateProbe(probe); err != nil {
		return fmt.Errorf("invalid Readiness Probe config. Reason: %s", err)
	}
	return nil
}

func validateStartupProbe(_ context.Context, _ splcommon.MetaObject, probe *enterpriseApi.Probe) error {
	if probe == nil {
		return nil
	}
	if err := validateProbe(probe); err != nil {
		return fmt.Errorf("invalid Startup Probe config. Reason: %s", err)
	}
	return nil
}

func validateSplunkGeneralTerms() error {
	if os.Getenv("SPLUNK_GENERAL_TERMS") == "--accept-sgt-current-at-splunk-com" {
		return nil
	}
	return fmt.Errorf("license not accepted, please adjust SPLUNK_GENERAL_TERMS to indicate you have accepted the current/latest version of the license. See README file for additional information")
}

// validateImagePullPolicy checks validity of the ImagePullPolicy spec parameter, and returns error if it is invalid.
func validateImagePullPolicy(imagePullPolicy *string) error {
	if *imagePullPolicy == "" {
		*imagePullPolicy = os.Getenv("IMAGE_PULL_POLICY")
	}
	switch *imagePullPolicy {
	case "":
		*imagePullPolicy = "IfNotPresent"
	case "Always", "IfNotPresent":
	default:
		return fmt.Errorf("ImagePullPolicy must be one of \"Always\" or \"IfNotPresent\"; value=\"%s\"", *imagePullPolicy)
	}
	return nil
}

// validateSpec checks validity and makes default updates to a Spec, and returns error if something is wrong.
func validateSpec(spec *enterpriseApi.Spec, defaultResources corev1.ResourceRequirements) error {
	if spec.SchedulerName == "" {
		spec.SchedulerName = "default-scheduler"
	}
	setServiceTemplateDefaults(spec)
	spec.Resources = splutil.EffectiveResources(spec.Resources, spec.DisableResourceDefaults, defaultResources)
	return validateImagePullPolicy(&spec.ImagePullPolicy)
}

// setServiceTemplateDefaults sets default values for service templates.
func setServiceTemplateDefaults(spec *enterpriseApi.Spec) {
	if spec.ServiceTemplate.Spec.Ports != nil {
		for idx := range spec.ServiceTemplate.Spec.Ports {
			p := &spec.ServiceTemplate.Spec.Ports[idx]
			if p.Protocol == "" {
				p.Protocol = corev1.ProtocolTCP
			}
			if p.TargetPort.IntValue() == 0 {
				p.TargetPort.IntVal = p.Port
			}
		}
	}
}

// validateCommonSplunkSpec checks validity and makes default updates to a CommonSplunkSpec, and returns error if something is wrong.
func validateCommonSplunkSpec(ctx context.Context, c splcommon.ControllerClient, spec *enterpriseApi.CommonSplunkSpec, cr splcommon.MetaObject) error {
	spec.Image = splutil.GetSplunkImage(spec.Image)
	if err := validateLivenessProbe(ctx, cr, spec.LivenessProbe); err != nil {
		return err
	}
	if err := validateReadinessProbe(ctx, cr, spec.ReadinessProbe); err != nil {
		return err
	}
	if err := validateStartupProbe(ctx, cr, spec.StartupProbe); err != nil {
		return err
	}
	if spec.LivenessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Liveness probe initial delay", spec.LivenessInitialDelaySeconds)
	}
	if spec.ReadinessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Readiness probe initial delay", spec.ReadinessInitialDelaySeconds)
	}
	if err := validateSplunkGeneralTerms(); err != nil {
		return err
	}
	if err := validateImagePullSecrets(ctx, c, cr, spec); err != nil {
		return err
	}
	if err := validateKVStoreDefaultTypeExtraEnv(spec.ExtraEnv); err != nil {
		return err
	}
	resources.SetVolumeDefaults(spec)
	return validateSpec(&spec.Spec, splutil.SplunkDefaultResources())
}

func validateKVStoreDefaultTypeExtraEnv(extraEnv []corev1.EnvVar) error {
	for _, env := range extraEnv {
		if env.Name == "SPLUNK_KVSTORE_DEFAULT_TYPE" && env.Value != "local" {
			return fmt.Errorf("SPLUNK_KVSTORE_DEFAULT_TYPE must be %q", "local")
		}
	}
	return nil
}

// validateImagePullSecrets sets default values for imagePullSecrets if not provided.
func validateImagePullSecrets(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) error {
	logger := logging.FromContext(ctx).With("func", "ValidateImagePullSecrets")
	if len(spec.ImagePullSecrets) == 0 {
		spec.ImagePullSecrets = nil
		return nil
	}
	for _, secret := range spec.ImagePullSecrets {
		if _, err := splutil.GetSecretByName(ctx, c, cr.GetNamespace(), secret.Name); err != nil {
			logger.ErrorContext(ctx, "couldn't get secret in the imagePullSecrets config", "Secret", secret.Name, "error", err)
		}
	}
	return nil
}

// validateRemoteVolumeSpec validates SmartStore remote volumes.
func validateRemoteVolumeSpec(volList []enterpriseApi.VolumeSpec) error {
	duplicateChecker := make(map[string]bool)
	for i, volume := range volList {
		if duplicateChecker[volume.Name] {
			return fmt.Errorf("duplicate volume name detected: %s. Remove the duplicate entry and reapply the configuration", volume.Name)
		}
		duplicateChecker[volume.Name] = true
		if volume.Name == "" {
			return fmt.Errorf("volume name is missing for volume at : %d", i)
		}
		if volume.Endpoint == "" {
			return fmt.Errorf("volume Endpoint URI is missing")
		}
		if volume.Path == "" {
			return fmt.Errorf("volume Path is missing")
		}
	}
	return nil
}

// validateSmartstoreIndexesSpec validates SmartStore index entries.
func validateSmartstoreIndexesSpec(smartstore *enterpriseApi.SmartStoreSpec) error {
	duplicateChecker := make(map[string]bool)
	for i, index := range smartstore.IndexList {
		if index.Name == "" {
			return fmt.Errorf("index name is missing for index at: %d", i)
		}
		if duplicateChecker[index.Name] {
			return fmt.Errorf("duplicate index name detected: %s.Remove the duplicate entry and reapply the configuration", index.Name)
		}
		duplicateChecker[index.Name] = true
		if index.VolName == "" && smartstore.Defaults.VolName == "" {
			return fmt.Errorf("volumeName is missing for index: %s", index.Name)
		}
		if index.VolName != "" {
			if _, err := splutil.CheckIfVolumeExists(smartstore.VolList, index.VolName); err != nil {
				return fmt.Errorf("invalid configuration for index: %s. %s", index.Name, err)
			}
		}
	}
	return nil
}

// validateSmartstoreSpec validates the SmartStore configuration.
func validateSmartstoreSpec(smartstore *enterpriseApi.SmartStoreSpec) error {
	if !resources.IsSmartstoreConfigured(smartstore) {
		return nil
	}
	if len(smartstore.IndexList) > 0 && len(smartstore.VolList) == 0 {
		return fmt.Errorf("volume configuration is missing. Num. of indexes = %d. Num. of Volumes = %d", len(smartstore.IndexList), len(smartstore.VolList))
	}
	if err := validateRemoteVolumeSpec(smartstore.VolList); err != nil {
		return err
	}
	if smartstore.Defaults.VolName != "" {
		if _, err := splutil.CheckIfVolumeExists(smartstore.VolList, smartstore.Defaults.VolName); err != nil {
			return fmt.Errorf("invalid configuration for defaults volume. %s", err)
		}
	}
	return validateSmartstoreIndexesSpec(smartstore)
}
