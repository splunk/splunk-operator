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

package reconcile

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

// ValidateSplunkGeneralTerms verifies that the current Splunk terms have been accepted.
func ValidateSplunkGeneralTerms() error {
	if os.Getenv("SPLUNK_GENERAL_TERMS") == "--accept-sgt-current-at-splunk-com" {
		return nil
	}
	return fmt.Errorf("license not accepted, please adjust SPLUNK_GENERAL_TERMS to indicate you have accepted the current/latest version of the license. See README file for additional information")
}

// ValidateImagePullPolicy checks validity of the ImagePullPolicy spec parameter, and returns error if it is invalid.
func ValidateImagePullPolicy(imagePullPolicy *string) error {
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

// ValidateSpec checks validity and makes default updates to a Spec, and returns error if something is wrong.
func ValidateSpec(spec *enterpriseApi.Spec, defaultResources corev1.ResourceRequirements) error {
	if spec.SchedulerName == "" {
		spec.SchedulerName = "default-scheduler"
	}
	SetServiceTemplateDefaults(spec)
	spec.Resources = splutil.EffectiveResources(spec.Resources, spec.DisableResourceDefaults, defaultResources)
	return ValidateImagePullPolicy(&spec.ImagePullPolicy)
}

// SetServiceTemplateDefaults sets default values for service templates.
func SetServiceTemplateDefaults(spec *enterpriseApi.Spec) {
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

// ValidateCommonSplunkSpec checks validity and makes default updates to a CommonSplunkSpec, and returns error if something is wrong.
func ValidateCommonSplunkSpec(ctx context.Context, c splcommon.ControllerClient, spec *enterpriseApi.CommonSplunkSpec, cr splcommon.MetaObject) error {
	spec.Image = splutil.GetSplunkImage(spec.Image)
	if probe := spec.LivenessProbe; probe != nil {
		if err := splcommon.ValidateProbe("Liveness", probe.InitialDelaySeconds, probe.TimeoutSeconds, probe.PeriodSeconds, probe.FailureThreshold); err != nil {
			return err
		}
	}
	if probe := spec.ReadinessProbe; probe != nil {
		if err := splcommon.ValidateProbe("Readiness", probe.InitialDelaySeconds, probe.TimeoutSeconds, probe.PeriodSeconds, probe.FailureThreshold); err != nil {
			return err
		}
	}
	if probe := spec.StartupProbe; probe != nil {
		if err := splcommon.ValidateProbe("Startup", probe.InitialDelaySeconds, probe.TimeoutSeconds, probe.PeriodSeconds, probe.FailureThreshold); err != nil {
			return err
		}
	}
	if spec.LivenessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Liveness probe initial delay", spec.LivenessInitialDelaySeconds)
	}
	if spec.ReadinessInitialDelaySeconds < 0 {
		return fmt.Errorf("negative value (%d) is not allowed for Readiness probe initial delay", spec.ReadinessInitialDelaySeconds)
	}
	if err := ValidateSplunkGeneralTerms(); err != nil {
		return err
	}
	if err := ValidateImagePullSecrets(ctx, c, cr, spec); err != nil {
		return err
	}
	if err := ValidateKVStoreDefaultTypeExtraEnv(spec.ExtraEnv); err != nil {
		return err
	}
	resources.SetVolumeDefaults(spec)
	return ValidateSpec(&spec.Spec, splutil.SplunkDefaultResources())
}

// ValidateKVStoreDefaultTypeExtraEnv validates the supported KV Store type.
func ValidateKVStoreDefaultTypeExtraEnv(extraEnv []corev1.EnvVar) error {
	for _, env := range extraEnv {
		if env.Name == "SPLUNK_KVSTORE_DEFAULT_TYPE" && env.Value != "local" {
			return fmt.Errorf("SPLUNK_KVSTORE_DEFAULT_TYPE must be %q", "local")
		}
	}
	return nil
}

// ValidateImagePullSecrets sets default values for imagePullSecrets if not provided.
func ValidateImagePullSecrets(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) error {
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
