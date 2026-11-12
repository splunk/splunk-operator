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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

func TestReconcileKVServiceHorizontalPodAutoscalerLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add Kubernetes types to scheme: %v", err)
	}
	if err := enterpriseApi.AddToScheme(scheme); err != nil {
		t.Fatalf("add enterprise types to scheme: %v", err)
	}

	cr := &enterpriseApi.KVService{
		TypeMeta: metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "KVService"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
			UID:       "kvservice-uid",
		},
		Spec: enterpriseApi.KVServiceSpec{
			Autoscaling: &enterpriseApi.KVServiceAutoscalingSpec{
				MinReplicas:                    ptr.To(int32(5)),
				MaxReplicas:                    ptr.To(int32(10)),
				TargetAverageUtilizationCPU:    ptr.To(int32(60)),
				TargetAverageUtilizationMemory: ptr.To(int32(60)),
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr.DeepCopy()).Build()
	ctx := context.Background()
	key := client.ObjectKey{Namespace: cr.Namespace, Name: "splunk-stack1-kvservice-hpa"}

	if err := reconcileAutoscaler(ctx, c, cr); err != nil {
		t.Fatalf("create HPA: %v", err)
	}
	hpa := &autoscalingv2.HorizontalPodAutoscaler{}
	if err := c.Get(ctx, key, hpa); err != nil {
		t.Fatalf("get created HPA: %v", err)
	}
	if len(hpa.Spec.Metrics) > 1 {
		assertResourceMetricTarget(t, hpa.Spec.Metrics[1], corev1.ResourceCPU, 60)
	} else {
		t.Errorf("HPA metrics = %d; want memory and CPU metrics", len(hpa.Spec.Metrics))
	}
	version := hpa.ResourceVersion
	if err := reconcileAutoscaler(ctx, c, cr); err != nil {
		t.Errorf("reconcile unchanged HPA: %v", err)
	}
	if err := c.Get(ctx, key, hpa); err != nil {
		t.Fatal(err)
	}
	if hpa.ResourceVersion != version {
		t.Error("unchanged HPA should not be updated")
	}

	cr.Spec.Autoscaling.TargetAverageUtilizationCPU = ptr.To(int32(75))
	if err := reconcileAutoscaler(ctx, c, cr); err != nil {
		t.Errorf("update HPA: %v", err)
	}
	if err := c.Get(ctx, key, hpa); err != nil {
		t.Fatalf("get updated HPA: %v", err)
	}
	if len(hpa.Spec.Metrics) > 1 {
		assertResourceMetricTarget(t, hpa.Spec.Metrics[1], corev1.ResourceCPU, 75)
	} else {
		t.Errorf("HPA metrics = %d; want memory and CPU metrics", len(hpa.Spec.Metrics))
	}

	cr.Spec.Autoscaling = nil
	if err := reconcileAutoscaler(ctx, c, cr); err != nil {
		t.Errorf("delete HPA: %v", err)
	}
	if err := c.Get(ctx, key, &autoscalingv2.HorizontalPodAutoscaler{}); !k8serrors.IsNotFound(err) {
		t.Errorf("get deleted HPA error = %v; want NotFound", err)
	}
}

func TestReconcileKVServiceHorizontalPodAutoscalerDoesNotAdoptForeignHPA(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add Kubernetes types to scheme: %v", err)
	}
	if err := enterpriseApi.AddToScheme(scheme); err != nil {
		t.Fatalf("add enterprise types to scheme: %v", err)
	}

	cr := &enterpriseApi.KVService{
		TypeMeta: metav1.TypeMeta{APIVersion: "enterprise.splunk.com/v4", Kind: "KVService"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
			UID:       "kvservice-uid",
		},
		Spec: enterpriseApi.KVServiceSpec{
			Autoscaling: &enterpriseApi.KVServiceAutoscalingSpec{
				MinReplicas:                    ptr.To(int32(5)),
				MaxReplicas:                    ptr.To(int32(10)),
				TargetAverageUtilizationCPU:    ptr.To(int32(60)),
				TargetAverageUtilizationMemory: ptr.To(int32(60)),
			},
		},
	}
	foreign := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-kvservice-hpa",
			Namespace: "test",
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			MaxReplicas: 10,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cr.DeepCopy(), foreign).Build()

	err := reconcileAutoscaler(context.Background(), c, cr)
	if err == nil {
		t.Error("expected conflict with foreign HPA")
	}

	current := &autoscalingv2.HorizontalPodAutoscaler{}
	key := client.ObjectKey{Namespace: foreign.Namespace, Name: foreign.Name}
	if getErr := c.Get(context.Background(), key, current); getErr != nil {
		t.Fatalf("get foreign HPA: %v", getErr)
	}
	if current.Spec.MaxReplicas != 10 || len(current.OwnerReferences) != 0 {
		t.Errorf("foreign HPA was modified: %#v", current)
	}
	cr.Spec.Autoscaling = nil
	if err := reconcileAutoscaler(context.Background(), c, cr); err != nil {
		t.Errorf("disable autoscaling with foreign HPA: %v", err)
	}
	if err := c.Get(context.Background(), key, current); err != nil {
		t.Errorf("foreign HPA should be preserved when autoscaling is disabled: %v", err)
	}
}
