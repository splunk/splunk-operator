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

package enterprise

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetSearchHeadStatefulSetRendersRollingUpdateStrategy(t *testing.T) {
	t.Setenv("SPLUNK_GENERAL_TERMS", "--accept-sgt-current-at-splunk-com")
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	client := spltest.NewMockClient()
	if _, err := splutil.ApplyNamespaceScopedSecretObject(
		context.Background(),
		client,
		cr.GetNamespace(),
	); err != nil {
		t.Fatalf("create namespace-scoped secret: %v", err)
	}

	statefulSet, err := getSearchHeadStatefulSet(context.Background(), client, cr)
	if err != nil {
		t.Fatalf("render SearchHead StatefulSet: %v", err)
	}

	assertRollingUpdatePartition(
		t,
		statefulSet.Spec.UpdateStrategy,
		cr.Spec.Replicas,
	)
}

func TestSearchHeadStatefulSetUpdateStrategyDefaultsToOnDelete(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	client := spltest.NewMockClient()

	strategy, err := getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		&corev1.PodTemplateSpec{},
	)
	if err != nil {
		t.Fatalf("resolve strategy: %v", err)
	}
	if strategy.Type != appsv1.OnDeleteStatefulSetStrategyType ||
		strategy.RollingUpdate != nil {
		t.Fatalf("strategy = %#v, want OnDelete", strategy)
	}
}

func TestSearchHeadStatefulSetRollingUpdateStartsFullyPartitioned(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	client := spltest.NewMockClient()

	strategy, err := getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		&corev1.PodTemplateSpec{},
	)
	if err != nil {
		t.Fatalf("resolve strategy: %v", err)
	}
	assertRollingUpdatePartition(t, strategy, cr.Spec.Replicas)
}

func TestSearchHeadStatefulSetRollingUpdatePreservesExistingPartition(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	partition := int32(1)
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &partition,
				},
			},
		},
	}
	client := spltest.NewMockClient()
	if err := client.Create(context.Background(), current); err != nil {
		t.Fatalf("create StatefulSet: %v", err)
	}

	strategy, err := getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		&current.Spec.Template,
	)
	if err != nil {
		t.Fatalf("resolve strategy: %v", err)
	}
	assertRollingUpdatePartition(t, strategy, partition)
}

func TestSearchHeadStatefulSetRollingUpdateRejectsInvalidStoredPartition(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	invalidPartition := cr.Spec.Replicas + 1
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &invalidPartition,
				},
			},
		},
	}
	client := spltest.NewMockClient()
	if err := client.Create(context.Background(), current); err != nil {
		t.Fatalf("create StatefulSet: %v", err)
	}

	strategy, err := getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		&current.Spec.Template,
	)
	if err != nil {
		t.Fatalf("resolve strategy: %v", err)
	}
	assertRollingUpdatePartition(t, strategy, cr.Spec.Replicas)
}

func TestSearchHeadStatefulSetRollingUpdateResetsPartitionForNewTemplate(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	partition := int32(0)
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &partition,
				},
			},
		},
	}
	client := spltest.NewMockClient()
	if err := client.Create(context.Background(), current); err != nil {
		t.Fatalf("create StatefulSet: %v", err)
	}
	desiredTemplate := current.Spec.Template.DeepCopy()
	desiredTemplate.Labels = map[string]string{"revision-input": "changed"}

	strategy, err := getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		desiredTemplate,
	)
	if err != nil {
		t.Fatalf("resolve strategy: %v", err)
	}
	assertRollingUpdatePartition(t, strategy, cr.Spec.Replicas)
}

func searchHeadRolloutStrategyTestCR() *enterpriseApi.SearchHeadCluster {
	return &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: 3,
			LifecyclePolicy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy: enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete,
			},
		},
	}
}

func assertRollingUpdatePartition(
	t *testing.T,
	strategy appsv1.StatefulSetUpdateStrategy,
	want int32,
) {
	t.Helper()
	if strategy.Type != appsv1.RollingUpdateStatefulSetStrategyType ||
		strategy.RollingUpdate == nil ||
		strategy.RollingUpdate.Partition == nil ||
		*strategy.RollingUpdate.Partition != want {
		t.Fatalf("strategy = %#v, want RollingUpdate partition %d", strategy, want)
	}
}
