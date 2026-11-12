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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("KVService autoscaling schema", Label("integration"), func() {
	var namespace *corev1.Namespace

	BeforeEach(func(ctx SpecContext) {
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "kv-autoscaling-schema-"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func() {
			Expect(client.IgnoreNotFound(k8sClient.Delete(context.Background(), namespace))).To(Succeed())
		})
	})

	resourceFor := func(settings map[string]any) *unstructured.Unstructured {
		spec := map[string]any{
			"postgres": map[string]any{"clusterRef": map[string]any{"name": "pg"}},
		}
		if settings != nil {
			spec["autoscaling"] = settings
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": enterpriseApi.GroupVersion.String(),
			"kind":       "KVService",
			"metadata": map[string]any{
				"name": "autoscaling", "namespace": namespace.Name,
				"annotations": map[string]any{enterpriseApi.KVServicePausedAnnotation: "true"},
			},
			"spec": spec,
		}}
	}

	DescribeTable("rejects invalid settings at the API server", func(ctx SpecContext, settings map[string]any, field string) {
		err := k8sClient.Create(ctx, resourceFor(settings))
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected a CRD validation error, got %v", err)
		Expect(err).To(MatchError(ContainSubstring(field)))
	},
		Entry("minimum below two", map[string]any{"minReplicas": int64(1)}, "minReplicas"),
		Entry("minimum above ten", map[string]any{"minReplicas": int64(11)}, "minReplicas"),
		Entry("maximum below two", map[string]any{"maxReplicas": int64(1)}, "maxReplicas"),
		Entry("maximum above ten", map[string]any{"maxReplicas": int64(11)}, "maxReplicas"),
		Entry("minimum exceeds maximum", map[string]any{"minReplicas": int64(8), "maxReplicas": int64(4)}, "minReplicas must be less than or equal to maxReplicas"),
		Entry("CPU target below one", map[string]any{"targetAverageUtilizationCPU": int64(0)}, "targetAverageUtilizationCPU"),
		Entry("CPU target above one hundred", map[string]any{"targetAverageUtilizationCPU": int64(101)}, "targetAverageUtilizationCPU"),
		Entry("memory target below one", map[string]any{"targetAverageUtilizationMemory": int64(0)}, "targetAverageUtilizationMemory"),
		Entry("memory target above one hundred", map[string]any{"targetAverageUtilizationMemory": int64(101)}, "targetAverageUtilizationMemory"),
	)

	DescribeTable("accepts valid settings and applies defaults", func(ctx SpecContext, settings map[string]any, expected *enterpriseApi.KVServiceAutoscalingSpec) {
		resource := resourceFor(settings)
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		current := &enterpriseApi.KVService{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), current)).To(Succeed())
		Expect(current.Spec.Autoscaling).To(Equal(expected))
		Expect(current.Spec.Replicas).To(Equal(ptr.To(int32(2))))
	},
		Entry("autoscaling omitted", map[string]any(nil), (*enterpriseApi.KVServiceAutoscalingSpec)(nil)),
		Entry("empty autoscaling object", map[string]any{}, &enterpriseApi.KVServiceAutoscalingSpec{
			MinReplicas: ptr.To(int32(2)), MaxReplicas: ptr.To(int32(10)), TargetAverageUtilizationCPU: ptr.To(int32(60)), TargetAverageUtilizationMemory: ptr.To(int32(60)),
		}),
		Entry("partial autoscaling object", map[string]any{
			"minReplicas": int64(3), "targetAverageUtilizationCPU": int64(70),
		}, &enterpriseApi.KVServiceAutoscalingSpec{
			MinReplicas: ptr.To(int32(3)), MaxReplicas: ptr.To(int32(10)), TargetAverageUtilizationCPU: ptr.To(int32(70)), TargetAverageUtilizationMemory: ptr.To(int32(60)),
		}),
		Entry("lower boundaries", map[string]any{
			"minReplicas": int64(2), "maxReplicas": int64(2), "targetAverageUtilizationCPU": int64(1), "targetAverageUtilizationMemory": int64(1),
		}, &enterpriseApi.KVServiceAutoscalingSpec{
			MinReplicas: ptr.To(int32(2)), MaxReplicas: ptr.To(int32(2)), TargetAverageUtilizationCPU: ptr.To(int32(1)), TargetAverageUtilizationMemory: ptr.To(int32(1)),
		}),
		Entry("upper boundaries", map[string]any{
			"minReplicas": int64(10), "maxReplicas": int64(10), "targetAverageUtilizationCPU": int64(100), "targetAverageUtilizationMemory": int64(100),
		}, &enterpriseApi.KVServiceAutoscalingSpec{
			MinReplicas: ptr.To(int32(10)), MaxReplicas: ptr.To(int32(10)), TargetAverageUtilizationCPU: ptr.To(int32(100)), TargetAverageUtilizationMemory: ptr.To(int32(100)),
		}),
	)

	It("rejects an invalid update and preserves the saved settings", func(ctx SpecContext) {
		resource := resourceFor(map[string]any{})
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		Expect(unstructured.SetNestedField(resource.Object, int64(8), "spec", "autoscaling", "minReplicas")).To(Succeed())
		Expect(unstructured.SetNestedField(resource.Object, int64(4), "spec", "autoscaling", "maxReplicas")).To(Succeed())
		err := k8sClient.Update(ctx, resource)
		Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected a CRD validation error, got %v", err)
		Expect(err).To(MatchError(ContainSubstring("minReplicas must be less than or equal to maxReplicas")))
		current := &enterpriseApi.KVService{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resource), current)).To(Succeed())
		Expect(current.Spec.Autoscaling).To(Equal(&enterpriseApi.KVServiceAutoscalingSpec{
			MinReplicas: ptr.To(int32(2)), MaxReplicas: ptr.To(int32(10)), TargetAverageUtilizationCPU: ptr.To(int32(60)), TargetAverageUtilizationMemory: ptr.To(int32(60)),
		}))
	})
})
