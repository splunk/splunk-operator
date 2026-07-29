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
	"fmt"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	if statefulSet.Spec.PodManagementPolicy != appsv1.ParallelPodManagement {
		t.Fatalf(
			"Pod management policy = %q, want %q for initial formation",
			statefulSet.Spec.PodManagementPolicy,
			appsv1.ParallelPodManagement,
		)
	}
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

func TestSearchHeadStatefulSetRollbackWaitsForActiveOperationCompletion(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	replicas := int32(3)
	partition := int32(2)
	target := int32(2)
	cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:     "pod-update-2",
		Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision: "revision-2",
		TargetPod:       GetSplunkStatefulsetPodName(SplunkSearchHead, cr.GetName(), target),
		TargetOrdinal:   &target,
		Stage:           enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
	}
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
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
		t.Fatalf("resolve pending rollback strategy: %v", err)
	}
	assertRollingUpdatePartition(t, strategy, partition)

	cr.Status.LifecycleOperation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	strategy, err = getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		&current.Spec.Template,
	)
	if err != nil {
		t.Fatalf("resolve completed rollback strategy: %v", err)
	}
	if strategy.Type != appsv1.OnDeleteStatefulSetStrategyType ||
		strategy.RollingUpdate != nil {
		t.Fatalf("completed rollback strategy = %#v, want OnDelete", strategy)
	}
}

func TestSearchHeadStatefulSetRollbackBeforePartitionAdvanceRestoresOnDelete(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	replicas := int32(3)
	partition := int32(3)
	target := int32(2)
	cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:     "pod-update-2",
		Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision: "revision-2",
		TargetPod:       GetSplunkStatefulsetPodName(SplunkSearchHead, cr.GetName(), target),
		TargetOrdinal:   &target,
		Stage:           enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
	}
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
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
		t.Fatalf("resolve rollback before partition advance: %v", err)
	}
	if strategy.Type != appsv1.OnDeleteStatefulSetStrategyType ||
		strategy.RollingUpdate != nil {
		t.Fatalf("pre-advance rollback strategy = %#v, want OnDelete", strategy)
	}
	operation := cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != target ||
		operation.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement {
		t.Fatalf(
			"pre-advance rollback operation = %#v, want retained ordinal 2 authorization",
			operation,
		)
	}
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

func TestSearchHeadStatefulSetRollingUpdateRetainsAuthorizedTargetForNewTemplate(
	t *testing.T,
) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	replicas := int32(3)
	partition := int32(2)
	target := int32(2)
	authorizedAt := metav1.Now()
	cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:             "pod-update-2",
			Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision:         "revision-2",
			TargetPod:               GetSplunkStatefulsetPodName(SplunkSearchHead, cr.GetName(), target),
			TargetOrdinal:           &target,
			TargetPodUID:            "original-pod-uid",
			Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
			ReplacementAuthorizedAt: &authorizedAt,
		}
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
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

	if _, err := holdSearchHeadStatefulSetTemplateForActiveReplacement(
		context.Background(),
		client,
		cr,
		desiredTemplate,
	); err != nil {
		t.Fatalf("hold active authorized template: %v", err)
	}
	if desiredTemplate.Labels["revision-input"] != "" {
		t.Fatalf(
			"active replacement template = %#v, want current template retained",
			desiredTemplate.Labels,
		)
	}
	strategy, err := getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		desiredTemplate,
	)
	if err != nil {
		t.Fatalf("resolve active authorized strategy: %v", err)
	}
	assertRollingUpdatePartition(t, strategy, target)

	cr.Status.LifecycleOperation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	desiredTemplate = current.Spec.Template.DeepCopy()
	desiredTemplate.Labels = map[string]string{"revision-input": "changed"}
	if _, err := holdSearchHeadStatefulSetTemplateForActiveReplacement(
		context.Background(),
		client,
		cr,
		desiredTemplate,
	); err != nil {
		t.Fatalf("hold completed replacement before Kubernetes readiness: %v", err)
	}
	if desiredTemplate.Labels["revision-input"] != "" {
		t.Fatalf(
			"completed but unready replacement template = %#v, want current template retained",
			desiredTemplate.Labels,
		)
	}

	replacementPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Status.LifecycleOperation.TargetPod,
			Namespace: cr.GetNamespace(),
			UID:       types.UID("replacement-pod-uid"),
			Labels: map[string]string{
				"controller-revision-hash": "revision-2",
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
				{
					Type:   searchHeadServingCondition,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}
	if err := client.Create(context.Background(), replacementPod); err != nil {
		t.Fatalf("create unready replacement Pod: %v", err)
	}
	desiredTemplate = current.Spec.Template.DeepCopy()
	desiredTemplate.Labels = map[string]string{"revision-input": "changed"}
	if _, err := holdSearchHeadStatefulSetTemplateForActiveReplacement(
		context.Background(),
		client,
		cr,
		desiredTemplate,
	); err != nil {
		t.Fatalf("hold completed but unready replacement template: %v", err)
	}
	if desiredTemplate.Labels["revision-input"] != "" {
		t.Fatalf(
			"unready replacement template = %#v, want current template retained",
			desiredTemplate.Labels,
		)
	}

	for index := range replacementPod.Status.Conditions {
		replacementPod.Status.Conditions[index].Status = corev1.ConditionTrue
	}
	if err := client.Status().Update(
		context.Background(),
		replacementPod,
	); err != nil {
		t.Fatalf("mark replacement Pod ready and serving: %v", err)
	}
	desiredTemplate = current.Spec.Template.DeepCopy()
	desiredTemplate.Labels = map[string]string{"revision-input": "changed"}
	if _, err := holdSearchHeadStatefulSetTemplateForActiveReplacement(
		context.Background(),
		client,
		cr,
		desiredTemplate,
	); err != nil {
		t.Fatalf("release Kubernetes-ready replacement template: %v", err)
	}
	if desiredTemplate.Labels["revision-input"] != "changed" {
		t.Fatalf(
			"ready replacement template = %#v, want queued template released",
			desiredTemplate.Labels,
		)
	}
	strategy, err = getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		desiredTemplate,
	)
	if err != nil {
		t.Fatalf("resolve completed authorized strategy: %v", err)
	}
	assertRollingUpdatePartition(t, strategy, replicas)
}

func TestSearchHeadStatefulSetRecoversFailedAuthorizedRevisionBeforeQueuedTemplate(
	t *testing.T,
) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	replicas := int32(3)
	target := int32(2)
	partition := target
	authorizedAt := metav1.Now()
	cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:             "pod-update-2",
			Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision:         "revision-2",
			TargetPod:               GetSplunkStatefulsetPodName(SplunkSearchHead, cr.GetName(), target),
			TargetOrdinal:           &target,
			TargetPodUID:            "original-pod-uid",
			Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageWaitingForScheduling,
			Reason:                  enterpriseApi.SearchHeadClusterLifecycleReasonPodUnschedulable,
			ReplacementAuthorizedAt: &authorizedAt,
		}
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"revision-input": "failed",
					},
				},
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &partition,
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			CurrentRevision: "revision-1",
			UpdateRevision:  "revision-2",
		},
	}
	client := spltest.NewMockClient()
	if err := client.Create(context.Background(), current); err != nil {
		t.Fatalf("create StatefulSet: %v", err)
	}
	for ordinal := int32(0); ordinal < replicas; ordinal++ {
		revision := "revision-1"
		ready := corev1.ConditionTrue
		uid := types.UID("stable-pod-uid")
		if ordinal == target {
			revision = "revision-2"
			ready = corev1.ConditionFalse
			uid = types.UID("failed-replacement-uid")
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", current.GetName(), ordinal),
				Namespace: current.GetNamespace(),
				UID:       uid,
				Labels: map[string]string{
					"controller-revision-hash": revision,
				},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: ready,
					},
					{
						Type:   searchHeadServingCondition,
						Status: ready,
					},
				},
			},
		}
		if err := client.Create(context.Background(), pod); err != nil {
			t.Fatalf("create Pod %d: %v", ordinal, err)
		}
	}

	peer := &corev1.Pod{}
	if err := client.Get(
		context.Background(),
		types.NamespacedName{
			Namespace: current.GetNamespace(),
			Name:      fmt.Sprintf("%s-1", current.GetName()),
		},
		peer,
	); err != nil {
		t.Fatalf("read peer at failed revision boundary: %v", err)
	}
	peer.Labels["controller-revision-hash"] = "revision-2"
	if err := client.Update(context.Background(), peer); err != nil {
		t.Fatalf("put peer at failed revision: %v", err)
	}
	unsafeTemplate := current.Spec.Template.DeepCopy()
	unsafeTemplate.Labels["revision-input"] = "queued"
	requested, err :=
		holdSearchHeadStatefulSetTemplateForActiveReplacement(
			context.Background(),
			client,
			cr,
			unsafeTemplate,
		)
	if err != nil {
		t.Fatalf("evaluate partially completed rollout: %v", err)
	}
	if requested ||
		unsafeTemplate.Labels["revision-input"] != "failed" {
		t.Fatalf(
			"partially completed rollout request=%t template=%#v, want fail-closed hold",
			requested,
			unsafeTemplate.Labels,
		)
	}
	peer.Labels["controller-revision-hash"] = "revision-1"
	if err := client.Update(context.Background(), peer); err != nil {
		t.Fatalf("restore peer revision: %v", err)
	}

	cr.Status.ImageUpgrade =
		&enterpriseApi.SearchHeadClusterImageUpgradeStatus{
			Phase: enterpriseApi.
				SearchHeadClusterImageUpgradePhaseRollingMembers,
		}
	unsafeTemplate = current.Spec.Template.DeepCopy()
	unsafeTemplate.Labels["revision-input"] = "queued"
	requested, err =
		holdSearchHeadStatefulSetTemplateForActiveReplacement(
			context.Background(),
			client,
			cr,
			unsafeTemplate,
		)
	if err != nil {
		t.Fatalf("evaluate active image-upgrade boundary: %v", err)
	}
	if requested ||
		unsafeTemplate.Labels["revision-input"] != "failed" {
		t.Fatalf(
			"active image upgrade request=%t template=%#v, want fail-closed hold",
			requested,
			unsafeTemplate.Labels,
		)
	}
	cr.Status.ImageUpgrade = nil

	queuedTemplate := current.Spec.Template.DeepCopy()
	queuedTemplate.Labels["revision-input"] = "queued"
	requested, err =
		holdSearchHeadStatefulSetTemplateForActiveReplacement(
			context.Background(),
			client,
			cr,
			queuedTemplate,
		)
	if err != nil {
		t.Fatalf("detect authorized revision withdrawal: %v", err)
	}
	if !requested ||
		queuedTemplate.Labels["revision-input"] != "failed" {
		t.Fatalf(
			"withdrawal request=%t held template=%#v, want request and failed template held",
			requested,
			queuedTemplate.Labels,
		)
	}

	recovery, started :=
		shcworkflow.StartAuthorizedPodUpdateRevisionRecovery(
			cr.Status.LifecycleOperation,
			current.Status.CurrentRevision,
			time.Now(),
		)
	if !started {
		t.Fatal("start authorized revision recovery")
	}
	cr.Status.LifecycleOperation = recovery
	strategy, err := getSearchHeadStatefulSetUpdateStrategy(
		context.Background(),
		client,
		cr,
		&current.Spec.Template,
	)
	if err != nil {
		t.Fatalf("raise recovery partition: %v", err)
	}
	assertRollingUpdatePartition(t, strategy, replicas)

	current.Spec.UpdateStrategy = strategy
	if err := client.Update(context.Background(), current); err != nil {
		t.Fatalf("store recovery partition: %v", err)
	}
	failedTarget := &corev1.Pod{}
	if err := client.Get(
		context.Background(),
		types.NamespacedName{
			Namespace: current.GetNamespace(),
			Name:      cr.Status.LifecycleOperation.TargetPod,
		},
		failedTarget,
	); err != nil {
		t.Fatalf("read failed target: %v", err)
	}
	if err := client.Delete(context.Background(), failedTarget); err != nil {
		t.Fatalf("delete failed target: %v", err)
	}
	recoveredTarget := failedTarget.DeepCopy()
	recoveredTarget.ResourceVersion = ""
	recoveredTarget.UID = types.UID("recovered-pod-uid")
	recoveredTarget.Labels["controller-revision-hash"] = "revision-1"
	for index := range recoveredTarget.Status.Conditions {
		recoveredTarget.Status.Conditions[index].Status =
			corev1.ConditionTrue
	}
	if err := client.Create(context.Background(), recoveredTarget); err != nil {
		t.Fatalf("create recovered target: %v", err)
	}
	cr.Status.LifecycleOperation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted

	queuedTemplate = current.Spec.Template.DeepCopy()
	queuedTemplate.Labels["revision-input"] = "queued"
	requested, err =
		holdSearchHeadStatefulSetTemplateForActiveReplacement(
			context.Background(),
			client,
			cr,
			queuedTemplate,
		)
	if err != nil {
		t.Fatalf("release queued template: %v", err)
	}
	if requested ||
		queuedTemplate.Labels["revision-input"] != "queued" {
		t.Fatalf(
			"completed recovery request=%t template=%#v, want queued template released",
			requested,
			queuedTemplate.Labels,
		)
	}
}

func TestSearchHeadStatefulSetDoesNotQueueTemplateBeforePartitionAuthorization(
	t *testing.T,
) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	replicas := int32(3)
	partition := replicas
	target := int32(2)
	authorizedAt := metav1.Now()
	cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:             "pod-update-2",
			Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision:         "revision-2",
			TargetPod:               GetSplunkStatefulsetPodName(SplunkSearchHead, cr.GetName(), target),
			TargetOrdinal:           &target,
			TargetPodUID:            "original-pod-uid",
			Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
			ReplacementAuthorizedAt: &authorizedAt,
		}
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
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

	if _, err := holdSearchHeadStatefulSetTemplateForActiveReplacement(
		context.Background(),
		client,
		cr,
		desiredTemplate,
	); err != nil {
		t.Fatalf("evaluate pre-partition template: %v", err)
	}
	if desiredTemplate.Labels["revision-input"] != "changed" {
		t.Fatalf(
			"pre-partition template = %#v, want superseding template available",
			desiredTemplate.Labels,
		)
	}
}

func TestSearchHeadStatefulSetScaleDownKeepsCurrentReplicasFullyPartitioned(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	cr := searchHeadRolloutStrategyTestCR()
	cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate
	currentReplicas := int32(5)
	partition := int32(0)
	current := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetSplunkStatefulsetName(SplunkSearchHead, cr.GetName()),
			Namespace: cr.GetNamespace(),
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &currentReplicas,
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
	assertRollingUpdatePartition(t, strategy, currentReplicas)
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
