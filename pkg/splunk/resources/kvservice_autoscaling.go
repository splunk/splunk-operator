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

package resources

import (
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	kvServiceAutoscalingScaleUpStabilizationSeconds   int32 = 60
	kvServiceAutoscalingScaleDownStabilizationSeconds int32 = 600
	kvServiceAutoscalingPolicyPeriodSeconds           int32 = 60
	kvServiceAutoscalingScaleUpPods                   int32 = 2
	kvServiceAutoscalingScaleDownPods                 int32 = 1
	kvServiceAutoscalingScaleChangePercentage         int32 = 25
)

// KVServiceHorizontalPodAutoscalerName returns the managed HPA name.
func KVServiceHorizontalPodAutoscalerName(cr *enterpriseApi.KVService) string {
	return splutil.GetSplunkDeploymentName(splcommon.SplunkKVService, cr.GetName()) + "-hpa"
}

// NewKVServiceHorizontalPodAutoscaler builds the desired HPA without API calls.
func NewKVServiceHorizontalPodAutoscaler(cr *enterpriseApi.KVService) *autoscalingv2.HorizontalPodAutoscaler {
	autoscaling := cr.Spec.Autoscaling.DeepCopy()
	if autoscaling == nil {
		return nil
	}

	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: autoscalingv2.SchemeGroupVersion.String(),
			Kind:       "HorizontalPodAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        KVServiceHorizontalPodAutoscalerName(cr),
			Namespace:   cr.GetNamespace(),
			Labels:      GetSplunkLabels(cr.GetName(), splcommon.SplunkKVService, ""),
			Annotations: map[string]string{},
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: enterpriseApi.GroupVersion.String(),
				Kind:       "KVService",
				Name:       cr.GetName(),
			},
			MinReplicas: autoscaling.MinReplicas,
			MaxReplicas: ptr.Deref(autoscaling.MaxReplicas, 0),
			Metrics: []autoscalingv2.MetricSpec{
				resourceUtilizationMetric(corev1.ResourceMemory, autoscaling.TargetAverageUtilizationMemory),
				resourceUtilizationMetric(corev1.ResourceCPU, autoscaling.TargetAverageUtilizationCPU),
			},
			Behavior: &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleUp: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: ptr.To(kvServiceAutoscalingScaleUpStabilizationSeconds),
					SelectPolicy:               ptr.To(autoscalingv2.MaxChangePolicySelect),
					Policies: []autoscalingv2.HPAScalingPolicy{
						{Type: autoscalingv2.PodsScalingPolicy, Value: kvServiceAutoscalingScaleUpPods, PeriodSeconds: kvServiceAutoscalingPolicyPeriodSeconds},
						{Type: autoscalingv2.PercentScalingPolicy, Value: kvServiceAutoscalingScaleChangePercentage, PeriodSeconds: kvServiceAutoscalingPolicyPeriodSeconds},
					},
				},
				ScaleDown: &autoscalingv2.HPAScalingRules{
					StabilizationWindowSeconds: ptr.To(kvServiceAutoscalingScaleDownStabilizationSeconds),
					SelectPolicy:               ptr.To(autoscalingv2.MaxChangePolicySelect),
					Policies: []autoscalingv2.HPAScalingPolicy{
						{Type: autoscalingv2.PodsScalingPolicy, Value: kvServiceAutoscalingScaleDownPods, PeriodSeconds: kvServiceAutoscalingPolicyPeriodSeconds},
						{Type: autoscalingv2.PercentScalingPolicy, Value: kvServiceAutoscalingScaleChangePercentage, PeriodSeconds: kvServiceAutoscalingPolicyPeriodSeconds},
					},
				},
			},
		},
	}

	splcommon.AppendParentMeta(hpa.GetObjectMeta(), cr.GetObjectMeta())
	if len(hpa.Annotations) == 0 {
		hpa.Annotations = nil
	}
	hpa.SetOwnerReferences([]metav1.OwnerReference{splcommon.AsOwner(cr, true)})
	return hpa
}

func resourceUtilizationMetric(name corev1.ResourceName, target *int32) autoscalingv2.MetricSpec {
	return autoscalingv2.MetricSpec{
		Type: autoscalingv2.ResourceMetricSourceType,
		Resource: &autoscalingv2.ResourceMetricSource{
			Name: name,
			Target: autoscalingv2.MetricTarget{
				Type:               autoscalingv2.UtilizationMetricType,
				AverageUtilization: target,
			},
		},
	}
}
