// Copyright (c) 2024 Splunk Inc. All rights reserved.
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
	if err := reconcileutil.ValidateSplunkGeneralTerms(); err != nil {
		return err
	}
	if err := reconcileutil.ValidateImagePullSecrets(ctx, c, cr, &spec.CommonSplunkSpec); err != nil {
		return err
	}
	resources.SetVolumeDefaults(&spec.CommonSplunkSpec)
	return reconcileutil.ValidateSpec(&spec.Spec, corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("0.1"), corev1.ResourceMemory: resource.MustParse("512Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("8Gi")},
	})
}
