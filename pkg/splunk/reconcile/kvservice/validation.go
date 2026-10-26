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

package kvservice

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func validateKVServiceSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService) error {
	if cr.Spec.Replicas == 0 {
		cr.Spec.Replicas = 1
	}
	spec := &cr.Spec
	spec.Image = splutil.GetKVServiceImage(spec.Image)
	for name, probe := range map[string]*enterpriseApi.Probe{"Liveness": spec.LivenessProbe, "Readiness": spec.ReadinessProbe} {
		if probe != nil {
			if err := splcommon.ValidateProbe(name, probe.InitialDelaySeconds, probe.TimeoutSeconds, probe.PeriodSeconds, probe.FailureThreshold); err != nil {
				return err
			}
		}
	}
	if spec.LivenessInitialDelaySeconds < 0 || spec.ReadinessInitialDelaySeconds < 0 {
		return fmt.Errorf("probe initial delay must not be negative")
	}
	if len(spec.Certs) > 0 && !config.DefaultMutableFeatureGate.Enabled(config.CertManagement) {
		return fmt.Errorf("spec.certs requires the CertManagement feature gate")
	}
	if err := reconcileutil.ValidateSplunkGeneralTerms(); err != nil {
		return err
	}
	if len(spec.ImagePullSecrets) == 0 {
		spec.ImagePullSecrets = nil
	}
	for _, secret := range spec.ImagePullSecrets {
		if _, err := splutil.GetSecretByName(ctx, c, cr.Namespace, secret.Name); err != nil {
			logging.FromContext(ctx).ErrorContext(ctx, "couldn't get secret in the imagePullSecrets config", "Secret", secret.Name, "error", err)
		}
	}
	if len(spec.Certs) > 0 {
		reserved := map[string]struct{}{"TLS_CERTIFICATE": {}, "TLS_PRIVATE_KEY": {}, "TLS_HOST": {}, "TLS_PORT": {}}
		for _, env := range spec.ExtraEnv {
			if _, ok := reserved[env.Name]; ok {
				return fmt.Errorf("spec.extraEnv cannot override operator-managed KVService TLS environment variable %q", env.Name)
			}
		}
	}
	resources.SetKVServiceVolumeDefaults(spec)
	return reconcileutil.ValidateSpec(&spec.Spec, corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("4Gi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("16Gi")},
	})
}
