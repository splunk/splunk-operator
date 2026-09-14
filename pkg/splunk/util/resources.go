// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package util

import (
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SplunkDefaultResources returns the default resource requests and limits for Splunk workloads.
func SplunkDefaultResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(splcommon.DefaultRequestsCPU),
			corev1.ResourceMemory: resource.MustParse(splcommon.DefaultRequestsMemory),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(splcommon.DefaultLimitsCPU),
			corev1.ResourceMemory: resource.MustParse(splcommon.DefaultLimitsMemory),
		},
	}
}

// SetDefaultResources checks resource requests and limits and sets defaults if not provided.
func SetDefaultResources(resources *corev1.ResourceRequirements, defaults corev1.ResourceRequirements) {
	if resources.Requests == nil {
		resources.Requests = make(corev1.ResourceList)
	}
	if resources.Limits == nil {
		resources.Limits = make(corev1.ResourceList)
	}
	if _, ok := resources.Requests[corev1.ResourceCPU]; !ok {
		resources.Requests[corev1.ResourceCPU] = defaults.Requests[corev1.ResourceCPU]
	}
	if _, ok := resources.Requests[corev1.ResourceMemory]; !ok {
		resources.Requests[corev1.ResourceMemory] = defaults.Requests[corev1.ResourceMemory]
	}
	if _, ok := resources.Limits[corev1.ResourceCPU]; !ok {
		resources.Limits[corev1.ResourceCPU] = defaults.Limits[corev1.ResourceCPU]
	}
	if _, ok := resources.Limits[corev1.ResourceMemory]; !ok {
		resources.Limits[corev1.ResourceMemory] = defaults.Limits[corev1.ResourceMemory]
	}
}

// EffectiveResources returns the resources the operator will apply.
func EffectiveResources(resources corev1.ResourceRequirements, disableResourceDefaults bool, defaults corev1.ResourceRequirements) corev1.ResourceRequirements {
	resources = *resources.DeepCopy()
	if !disableResourceDefaults {
		SetDefaultResources(&resources, defaults)
	}
	return resources
}
