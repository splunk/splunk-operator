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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestGetKVServiceHorizontalPodAutoscaler(t *testing.T) {
	const (
		wantMinReplicas                   int32 = 2
		wantMaxReplicas                   int32 = 10
		wantCPUUtilization                int32 = 60
		wantMemoryUtilization             int32 = 60
		wantScaleUpStabilizationSeconds   int32 = 60
		wantScaleDownStabilizationSeconds int32 = 600
	)

	cr := &enterpriseApi.KVService{
		TypeMeta: metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "KVService"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
			UID:       "kvservice-uid",
		},
		Spec: enterpriseApi.KVServiceSpec{
			Autoscaling: &enterpriseApi.KVServiceAutoscalingSpec{
				MinReplicas: ptr.To(wantMinReplicas), MaxReplicas: ptr.To(wantMaxReplicas),
				TargetAverageUtilizationCPU: ptr.To(wantCPUUtilization), TargetAverageUtilizationMemory: ptr.To(wantMemoryUtilization),
			},
		},
	}

	hpa := NewKVServiceHorizontalPodAutoscaler(cr)
	if hpa == nil {
		t.Fatal("expected HorizontalPodAutoscaler")
	}
	if hpa.Name != "splunk-stack1-kvservice-hpa" {
		t.Errorf("HPA name = %q; want splunk-stack1-kvservice-hpa", hpa.Name)
	}
	if hpa.Spec.ScaleTargetRef.APIVersion != "enterprise.splunk.com/v4" || hpa.Spec.ScaleTargetRef.Kind != "KVService" || hpa.Spec.ScaleTargetRef.Name != "stack1" {
		t.Errorf("HPA scale target = %#v; want enterprise.splunk.com/v4 KVService stack1", hpa.Spec.ScaleTargetRef)
	}
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != wantMinReplicas || hpa.Spec.MaxReplicas != wantMaxReplicas {
		t.Errorf("HPA replica bounds = %v..%d; want %d..%d", hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas, wantMinReplicas, wantMaxReplicas)
	}
	wantMetrics := []struct {
		name   corev1.ResourceName
		target int32
	}{
		{corev1.ResourceMemory, wantMemoryUtilization},
		{corev1.ResourceCPU, wantCPUUtilization},
	}
	if len(hpa.Spec.Metrics) != len(wantMetrics) {
		t.Errorf("HPA metrics = %d; want %d", len(hpa.Spec.Metrics), len(wantMetrics))
	}
	for i, metric := range wantMetrics {
		if i < len(hpa.Spec.Metrics) {
			assertResourceMetricTarget(t, hpa.Spec.Metrics[i], metric.name, metric.target)
		}
	}

	if hpa.Spec.Behavior == nil {
		t.Error("HPA scale behavior is missing")
	} else {
		if up := hpa.Spec.Behavior.ScaleUp; up == nil {
			t.Error("HPA scale-up behavior is missing")
		} else if up.StabilizationWindowSeconds == nil || *up.StabilizationWindowSeconds != wantScaleUpStabilizationSeconds {
			t.Errorf("scale-up stabilization = %v; want %d", up.StabilizationWindowSeconds, wantScaleUpStabilizationSeconds)
		}
		if down := hpa.Spec.Behavior.ScaleDown; down == nil {
			t.Error("HPA scale-down behavior is missing")
		} else if down.StabilizationWindowSeconds == nil || *down.StabilizationWindowSeconds != wantScaleDownStabilizationSeconds {
			t.Errorf("scale-down stabilization = %v; want %d", down.StabilizationWindowSeconds, wantScaleDownStabilizationSeconds)
		}
	}
	if !metav1.IsControlledBy(hpa, cr) {
		t.Error("HPA should be controlled by the KVService")
	}
	if hpa.Annotations != nil {
		t.Errorf("empty HPA annotations should be normalized to nil, got %#v", hpa.Annotations)
	}
	cr.Labels = map[string]string{"custom": "label"}
	cr.Annotations = map[string]string{"custom": "annotation"}
	hpa = NewKVServiceHorizontalPodAutoscaler(cr)
	if hpa == nil {
		t.Error("expected HorizontalPodAutoscaler with parent metadata")
	} else if hpa.Labels["custom"] != "label" || hpa.Annotations["custom"] != "annotation" {
		t.Error("HPA should inherit parent metadata")
	}
	cr.Spec.Autoscaling = nil
	if hpa := NewKVServiceHorizontalPodAutoscaler(cr); hpa != nil {
		t.Error("disabled autoscaling should not produce an HPA")
	}
}

func assertResourceMetricTarget(t *testing.T, metric autoscalingv2.MetricSpec, name corev1.ResourceName, target int32) {
	t.Helper()
	if metric.Type != autoscalingv2.ResourceMetricSourceType || metric.Resource == nil {
		t.Errorf("metric = %#v; want resource metric", metric)
		return
	}
	if metric.Resource.Name != name || metric.Resource.Target.AverageUtilization == nil || *metric.Resource.Target.AverageUtilization != target {
		t.Errorf("metric target = %#v; want %s at %d%%", metric.Resource, name, target)
	}
}
