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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splmetrics "github.com/splunk/splunk-operator/pkg/splunk/client/metrics"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	upgrade "github.com/splunk/splunk-operator/pkg/splunk/workflow/upgrade"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func TestRollingUpdateControllerLetsDurableAppWorkFinishFirst(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	tests := []struct {
		name                 string
		deploymentInProgress bool
		bundleStage          enterpriseApi.BundlePushStageType
	}{
		{
			name:                 "App Framework deployment",
			deploymentInProgress: true,
			bundleStage:          enterpriseApi.BundlePushComplete,
		},
		{
			name:        "pending bundle",
			bundleStage: enterpriseApi.BundlePushPending,
		},
		{
			name:        "in-progress bundle",
			bundleStage: enterpriseApi.BundlePushInProgress,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mgr, statefulSet, client := rollingUpdateControllerFixture(
				t,
				3,
				"revision-1",
				"revision-2",
				[]string{"revision-1", "revision-1", "revision-1"},
			)
			mgr.cr.Status.AppContext.IsDeploymentInProgress =
				test.deploymentInProgress
			mgr.cr.Status.AppContext.BundlePushStatus.BundlePushStage =
				test.bundleStage

			phase, err := mgr.updateRollingStatefulSetPods(
				context.Background(),
				statefulSet,
				3,
			)
			if err != nil {
				t.Fatalf("coordinate rollout with App Framework: %v", err)
			}
			if phase != enterpriseApi.PhaseReady {
				t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseReady)
			}
			if mgr.cr.Status.LifecycleOperation != nil {
				t.Fatalf(
					"App Framework hold started lifecycle operation: %#v",
					mgr.cr.Status.LifecycleOperation,
				)
			}
			if !strings.Contains(
				mgr.cr.Status.Message,
				"AppFrameworkOperationActive",
			) {
				t.Fatalf(
					"status message = %q, want App Framework hold reason",
					mgr.cr.Status.Message,
				)
			}
			assertRollingUpdatePartition(
				t,
				statefulSet.Spec.UpdateStrategy,
				3,
			)
			if len(client.Calls["Update"]) != 0 {
				t.Fatalf(
					"App Framework hold changed Kubernetes state: %v",
					client.Calls["Update"],
				)
			}
			assertNoRollingUpdatePodDelete(t, client)

			mgr.cr.Status.AppContext.IsDeploymentInProgress = false
			mgr.cr.Status.AppContext.BundlePushStatus.BundlePushStage =
				enterpriseApi.BundlePushComplete
			phase, err = mgr.updateRollingStatefulSetPods(
				context.Background(),
				statefulSet,
				3,
			)
			if err != nil {
				t.Fatalf("start rollout after App Framework completion: %v", err)
			}
			if phase != enterpriseApi.PhaseUpdating {
				t.Fatalf(
					"post-App Framework phase = %q, want %q",
					phase,
					enterpriseApi.PhaseUpdating,
				)
			}
			operation := mgr.cr.Status.LifecycleOperation
			if operation == nil ||
				operation.TargetOrdinal == nil ||
				*operation.TargetOrdinal != 2 {
				t.Fatalf(
					"post-App Framework operation = %#v, want ordinal 2",
					operation,
				)
			}
		})
	}
}

func TestRollingUpdateControllerDoesNotYieldBlockedOwnerToPendingBundle(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:     "PodUpdate:splunk-stack1-search-head-2:revision-2",
			Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision: "revision-2",
			TargetPod:       "splunk-stack1-search-head-2",
			TargetOrdinal:   &target,
			Stage:           enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		}
	mgr.cr.Status.AppContext.BundlePushStatus.BundlePushStage =
		enterpriseApi.BundlePushPending

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err == nil || !strings.Contains(err.Error(), "LifecycleBlocked") {
		t.Fatalf(
			"active blocked rollout returned phase=%q error=%v, want LifecycleBlocked",
			phase,
			err,
		)
	}
	if mgr.cr.Status.LifecycleOperation.OperationID !=
		"PodUpdate:splunk-stack1-search-head-2:revision-2" {
		t.Fatalf(
			"bundle work replaced rollout owner: %#v",
			mgr.cr.Status.LifecycleOperation,
		)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("blocked rollout changed partition: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestRollingUpdateControllerStartsDurablePreparationWithoutDeletingPod(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("update RollingUpdate StatefulSet: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != 2 ||
		operation.DesiredRevision != "revision-2" ||
		operation.Intent != enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate {
		t.Fatalf("lifecycle operation = %#v, want durable preparation for ordinal 2", operation)
	}
	assertNoRollingUpdatePodDelete(t, client)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("unexpected Kubernetes update before authorization: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerPersistsImageInitializationBeforeMemberLifecycle(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	configureImageUpgradeControllerFixture(
		mgr,
		statefulSet,
		"splunk/splunk:9.4.0",
		"splunk/splunk:10.0.0",
	)
	// Prove target selection does not depend on ordinal zero.
	mgr.cr.Status.Members[0].Registered = false

	now := time.Date(2026, 7, 25, 14, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterImageUpgradeNow
	oldInitiate := initiateSearchHeadClusterUpgrade
	t.Cleanup(func() {
		searchHeadClusterImageUpgradeNow = oldNow
		initiateSearchHeadClusterUpgrade = oldInitiate
	})
	searchHeadClusterImageUpgradeNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	initializationTargets := make([]int32, 0, 1)
	initiateSearchHeadClusterUpgrade = func(
		_ context.Context,
		_ *searchHeadClusterPodManager,
		ordinal int32,
	) error {
		initializationTargets = append(initializationTargets, ordinal)
		return nil
	}

	// Reconcile 1 persists intent and cannot call Splunk or start detention.
	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("persist initialization intent phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.ImageUpgrade.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing ||
		mgr.cr.Status.ImageUpgrade.InitializationIntentAt == nil ||
		len(initializationTargets) != 0 ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"intent barrier image=%#v targets=%v lifecycle=%#v",
			mgr.cr.Status.ImageUpgrade,
			initializationTargets,
			mgr.cr.Status.LifecycleOperation,
		)
	}

	// Reconcile 2 observes persisted intent, calls one eligible member, and
	// records success while remaining in Initializing.
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("initialize image upgrade phase=%q error=%v", phase, err)
	}
	if !reflect.DeepEqual(initializationTargets, []int32{1}) ||
		mgr.cr.Status.ImageUpgrade.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing ||
		mgr.cr.Status.ImageUpgrade.InitializationSucceededAt == nil ||
		mgr.cr.Status.ImageUpgrade.InitializationAttemptCount != 1 ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"success barrier image=%#v targets=%v lifecycle=%#v",
			mgr.cr.Status.ImageUpgrade,
			initializationTargets,
			mgr.cr.Status.LifecycleOperation,
		)
	}
	if mgr.cr.Status.UpgradePhase != enterpriseApi.UpgradePhaseUpgrading ||
		mgr.cr.Status.UpgradeStartTimestamp == 0 {
		t.Fatalf(
			"legacy upgrade projection phase=%q start=%d",
			mgr.cr.Status.UpgradePhase,
			mgr.cr.Status.UpgradeStartTimestamp,
		)
	}

	// Reconcile 3 persists RollingMembers and still cannot detain a member.
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("persist RollingMembers phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.ImageUpgrade.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers ||
		len(initializationTargets) != 1 ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"RollingMembers barrier image=%#v targets=%v lifecycle=%#v",
			mgr.cr.Status.ImageUpgrade,
			initializationTargets,
			mgr.cr.Status.LifecycleOperation,
		)
	}

	// Reconcile 4 observes persisted RollingMembers and may create the first
	// per-member lifecycle identity, but does not call upgrade-init again.
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("start first member phase=%q error=%v", phase, err)
	}
	if len(initializationTargets) != 1 ||
		mgr.cr.Status.LifecycleOperation == nil ||
		mgr.cr.Status.LifecycleOperation.TargetOrdinal == nil ||
		*mgr.cr.Status.LifecycleOperation.TargetOrdinal != 2 {
		t.Fatalf(
			"member start targets=%v lifecycle=%#v",
			initializationTargets,
			mgr.cr.Status.LifecycleOperation,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf(
			"initialization barriers moved StatefulSet partition: %v",
			client.Calls["Update"],
		)
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestRollingUpdateControllerPersistsImageInitializationFailureForRetry(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	configureImageUpgradeControllerFixture(
		mgr,
		statefulSet,
		"splunk/splunk:9.4.0",
		"splunk/splunk:10.0.0",
	)

	now := time.Date(2026, 7, 25, 15, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterImageUpgradeNow
	oldInitiate := initiateSearchHeadClusterUpgrade
	t.Cleanup(func() {
		searchHeadClusterImageUpgradeNow = oldNow
		initiateSearchHeadClusterUpgrade = oldInitiate
	})
	searchHeadClusterImageUpgradeNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	initializationCalls := 0
	initiateSearchHeadClusterUpgrade = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		initializationCalls++
		if initializationCalls == 1 {
			return fmt.Errorf("transient endpoint failure")
		}
		return nil
	}

	// Persist intent.
	if _, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	); err != nil {
		t.Fatalf("persist initialization intent: %v", err)
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err == nil || phase != enterpriseApi.PhaseError {
		t.Fatalf("failed initialization phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.ImageUpgrade.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing ||
		mgr.cr.Status.ImageUpgrade.Reason != enterpriseApi.
			SearchHeadClusterImageUpgradeReasonInitializationRetrying ||
		mgr.cr.Status.ImageUpgrade.InitializationAttemptCount != 1 ||
		mgr.cr.Status.ImageUpgrade.InitializationSucceededAt != nil ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf("retry status = %#v", mgr.cr.Status.ImageUpgrade)
	}
	if strings.Contains(
		mgr.cr.Status.ImageUpgrade.Message,
		"transient endpoint failure",
	) {
		t.Fatalf(
			"retry status exposed endpoint error: %q",
			mgr.cr.Status.ImageUpgrade.Message,
		)
	}

	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("retry initialization phase=%q error=%v", phase, err)
	}
	if initializationCalls != 2 ||
		mgr.cr.Status.ImageUpgrade.InitializationAttemptCount != 2 ||
		mgr.cr.Status.ImageUpgrade.InitializationSucceededAt == nil ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"successful retry calls=%d image=%#v lifecycle=%#v",
			initializationCalls,
			mgr.cr.Status.ImageUpgrade,
			mgr.cr.Status.LifecycleOperation,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("initialization retry moved partition: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerWaitsWithoutEligibleImageManagementTarget(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	configureImageUpgradeControllerFixture(
		mgr,
		statefulSet,
		"splunk/splunk:9.4.0",
		"splunk/splunk:10.0.0",
	)
	for ordinal := range mgr.cr.Status.Members {
		mgr.cr.Status.Members[ordinal].Registered = false
	}

	oldInitiate := initiateSearchHeadClusterUpgrade
	t.Cleanup(func() { initiateSearchHeadClusterUpgrade = oldInitiate })
	initializationCalls := 0
	initiateSearchHeadClusterUpgrade = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		initializationCalls++
		return nil
	}

	// Persist intent, then observe no eligible target.
	for reconcile := 0; reconcile < 2; reconcile++ {
		phase, err := mgr.updateRollingStatefulSetPods(
			context.Background(),
			statefulSet,
			3,
		)
		if err != nil || phase != enterpriseApi.PhaseUpdating {
			t.Fatalf(
				"target wait reconcile=%d phase=%q error=%v",
				reconcile,
				phase,
				err,
			)
		}
	}
	if initializationCalls != 0 ||
		mgr.cr.Status.ImageUpgrade.InitializationAttemptCount != 0 ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"ineligible target calls=%d image=%#v lifecycle=%#v",
			initializationCalls,
			mgr.cr.Status.ImageUpgrade,
			mgr.cr.Status.LifecycleOperation,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("target wait moved partition: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerBlocksMemberLifecycleBeforeImageInitialization(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	configureImageUpgradeControllerFixture(
		mgr,
		statefulSet,
		"splunk/splunk:9.4.0",
		"splunk/splunk:10.0.0",
	)
	target := int32(2)
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:     "unexpected-pre-init-member",
			Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision: statefulSet.Status.UpdateRevision,
			TargetPod:       statefulSet.GetName() + "-2",
			TargetOrdinal:   &target,
			Stage: enterpriseApi.
				SearchHeadClusterLifecycleStageDetainingTarget,
		}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err == nil || phase != enterpriseApi.PhaseError {
		t.Fatalf("pre-init lifecycle phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.ImageUpgrade.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseBlocked ||
		mgr.cr.Status.ImageUpgrade.Reason != enterpriseApi.
			SearchHeadClusterImageUpgradeReasonConflictingPlannedOperation ||
		mgr.cr.Status.ImageUpgrade.InitializationIntentAt != nil {
		t.Fatalf("pre-init lifecycle did not fail closed: %#v", mgr.cr.Status.ImageUpgrade)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("pre-init conflict moved partition: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerIgnoresHistoricalCompletedImageWorkflow(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, _ := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	configureImageUpgradeControllerFixture(
		mgr,
		statefulSet,
		"splunk/splunk:9.4.0",
		"splunk/splunk:10.0.0",
	)
	mgr.cr.Status.ImageUpgrade.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("historical image workflow phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.LifecycleOperation == nil ||
		mgr.cr.Status.LifecycleOperation.TargetOrdinal == nil ||
		*mgr.cr.Status.LifecycleOperation.TargetOrdinal != 2 {
		t.Fatalf(
			"historical workflow gated ordinary rollout: %#v",
			mgr.cr.Status.LifecycleOperation,
		)
	}
}

func TestRollingUpdateControllerWaitsForParallelInitialFormation(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	statefulSet.Spec.PodManagementPolicy = appsv1.ParallelPodManagement
	mgr.cr.Status.MinPeersJoined = false

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("wait for initial SHC formation: %v", err)
	}
	if phase != enterpriseApi.PhasePending {
		t.Fatalf("initial formation phase = %q, want %q", phase, enterpriseApi.PhasePending)
	}
	if mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"initial formation started lifecycle operation: %#v",
			mgr.cr.Status.LifecycleOperation,
		)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("initial formation changed partition: %v", client.Calls["Update"])
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	assertNoRollingUpdatePodDelete(t, client)
	if !strings.Contains(
		mgr.cr.Status.Message,
		string(upgrade.SHCRolloutReasonInitialFormationPending),
	) {
		t.Fatalf(
			"initial formation status = %q, want %s",
			mgr.cr.Status.Message,
			upgrade.SHCRolloutReasonInitialFormationPending,
		)
	}

	mgr.cr.Status.MinPeersJoined = true
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("start rollout after initial formation: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("post-formation phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != 2 {
		t.Fatalf(
			"post-formation operation = %#v, want preparation for ordinal 2",
			operation,
		)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("post-formation preparation changed partition: %v", client.Calls["Update"])
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	assertNoRollingUpdatePodDelete(t, client)
}

func TestSearchHeadScaleUpAddsOneNewOrdinalWithoutRecyclingMembers(t *testing.T) {
	for _, strategy := range []appsv1.StatefulSetUpdateStrategyType{
		appsv1.OnDeleteStatefulSetStrategyType,
		appsv1.RollingUpdateStatefulSetStrategyType,
	} {
		t.Run(string(strategy), func(t *testing.T) {
			setLifecyclePolicyTestGates(t, true, true)
			mgr, statefulSet, client := rollingUpdateControllerFixture(
				t,
				3,
				"revision-2",
				"revision-2",
				[]string{"revision-2", "revision-2", "revision-2"},
			)
			statefulSet.Spec.UpdateStrategy.Type = strategy
			if strategy == appsv1.OnDeleteStatefulSetStrategyType {
				statefulSet.Spec.UpdateStrategy.RollingUpdate = nil
			}
			mgr.cr.Status.Captain = statefulSet.GetName() + "-0"
			mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
				{Name: statefulSet.GetName() + "-0", Status: "Up", Registered: true},
				{Name: statefulSet.GetName() + "-1", Status: "Up", Registered: true},
				{Name: statefulSet.GetName() + "-2", Status: "Up", Registered: true},
			}
			oldGetMembers := getSearchHeadCaptainMembers
			t.Cleanup(func() { getSearchHeadCaptainMembers = oldGetMembers })
			getSearchHeadCaptainMembers = func(
				context.Context,
				*searchHeadClusterPodManager,
				int32,
			) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
				return map[string]splclient.SearchHeadCaptainMemberInfo{
					statefulSet.GetName() + "-0": {
						Identifier: "member-guid-0",
						Label:      statefulSet.GetName() + "-0",
						Status:     "Up",
						Captain:    true,
					},
					statefulSet.GetName() + "-1": {
						Identifier: "member-guid-1",
						Label:      statefulSet.GetName() + "-1",
						Status:     "Up",
					},
					statefulSet.GetName() + "-2": {
						Identifier: "member-guid-2",
						Label:      statefulSet.GetName() + "-2",
						Status:     "Up",
					},
				}, nil
			}
			client.ResetCalls()

			phase, err := mgr.updateStatefulSetPods(
				context.Background(),
				statefulSet,
				5,
			)
			if err != nil {
				t.Fatalf("scale established SHC from 3 to 5: %v", err)
			}
			if phase != enterpriseApi.PhaseScalingUp {
				t.Fatalf(
					"scale-up phase = %q, want %q",
					phase,
					enterpriseApi.PhaseScalingUp,
				)
			}
			if statefulSet.Spec.Replicas == nil ||
				*statefulSet.Spec.Replicas != 4 {
				t.Fatalf(
					"first scale-up target = %v, want 4",
					statefulSet.Spec.Replicas,
				)
			}
			if len(client.Calls["Update"]) != 1 {
				t.Fatalf(
					"scale-up updates = %v, want one replica update",
					client.Calls["Update"],
				)
			}
			assertNoRollingUpdatePodDelete(t, client)
			if mgr.cr.Status.LifecycleOperation != nil {
				t.Fatalf(
					"scale-up started replacement lifecycle: %#v",
					mgr.cr.Status.LifecycleOperation,
				)
			}
		})
	}
}

func TestSearchHeadScaleUpWaitsForCurrentOrdinalBeforeAddingNext(t *testing.T) {
	replicas := int32(4)
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{Replicas: &replicas},
		Status: appsv1.StatefulSetStatus{
			Replicas:      4,
			ReadyReplicas: 3,
		},
	}

	if target := nextSearchHeadClusterReplicaTarget(
		statefulSet,
		5,
		false,
	); target != 4 {
		t.Fatalf("in-flight scale-up target = %d, want 4", target)
	}
	statefulSet.Status.ReadyReplicas = 4
	if target := nextSearchHeadClusterReplicaTarget(
		statefulSet,
		5,
		false,
	); target != 4 {
		t.Fatalf("locally ready but unjoined scale-up target = %d, want 4", target)
	}
	if target := nextSearchHeadClusterReplicaTarget(
		statefulSet,
		5,
		true,
	); target != 5 {
		t.Fatalf("qualified next scale-up target = %d, want 5", target)
	}
	if target := nextSearchHeadClusterReplicaTarget(
		statefulSet,
		3,
		false,
	); target != 3 {
		t.Fatalf("scale-down target = %d, want unchanged desired 3", target)
	}
}

func TestSearchHeadScaleUpRemainsInProgressUntilCaptainObservesNewOrdinal(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		4,
		"revision-2",
		"revision-2",
		[]string{"revision-2", "revision-2", "revision-2", "revision-2"},
	)
	mgr.cr.Status.Captain = statefulSet.GetName() + "-0"
	mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: statefulSet.GetName() + "-0", Status: "Up", Registered: true},
		{Name: statefulSet.GetName() + "-1", Status: "Up", Registered: true},
		{Name: statefulSet.GetName() + "-2", Status: "Up", Registered: true},
		{Name: statefulSet.GetName() + "-3", Status: "Up", Registered: true},
	}
	oldGetMembers := getSearchHeadCaptainMembers
	t.Cleanup(func() { getSearchHeadCaptainMembers = oldGetMembers })
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			statefulSet.GetName() + "-0": {
				Identifier: "member-guid-0",
				Label:      statefulSet.GetName() + "-0",
				Status:     "Up",
				Captain:    true,
			},
			statefulSet.GetName() + "-1": {
				Identifier: "member-guid-1",
				Label:      statefulSet.GetName() + "-1",
				Status:     "Up",
			},
			statefulSet.GetName() + "-2": {
				Identifier: "member-guid-2",
				Label:      statefulSet.GetName() + "-2",
				Status:     "Up",
			},
		}, nil
	}
	client.ResetCalls()

	phase, err := mgr.updateStatefulSetPods(
		context.Background(),
		statefulSet,
		5,
	)
	if err != nil {
		t.Fatalf("wait for captain to observe ordinal 3: %v", err)
	}
	if phase != enterpriseApi.PhaseScalingUp {
		t.Fatalf(
			"unjoined ordinal phase = %q, want %q",
			phase,
			enterpriseApi.PhaseScalingUp,
		)
	}
	if statefulSet.Spec.Replicas == nil ||
		*statefulSet.Spec.Replicas != 4 {
		t.Fatalf(
			"unjoined ordinal changed replica target to %v",
			statefulSet.Spec.Replicas,
		)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf(
			"unjoined ordinal changed Kubernetes state: %v",
			client.Calls["Update"],
		)
	}
	assertNoRollingUpdatePodDelete(t, client)
	if mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"unjoined ordinal started replacement lifecycle: %#v",
			mgr.cr.Status.LifecycleOperation,
		)
	}
}

func TestRollingUpdateControllerAdvancesOnlyAfterPersistedAuthorization(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		ReplacementAuthorizedAt: &authorizedAt,
	}
	recorder := &mockEventRecorder{}
	eventPublisher := &K8EventPublisher{
		recorder: recorder,
		instance: mgr.cr,
	}
	ctx := context.WithValue(
		context.Background(),
		splcommon.EventPublisherKey,
		eventPublisher,
	)
	decisionMetric := splmetrics.SHCRolloutDecisionCounters.WithLabelValues(
		string(upgrade.SHCRolloutActionSetPartition),
		string(upgrade.SHCRolloutReasonPartitionAdvanceAuthorized),
	)
	decisionBefore := testutil.ToFloat64(decisionMetric)
	partitionBefore := testutil.ToFloat64(
		splmetrics.SHCRolloutPartitionAdvanceCounter,
	)

	phase, err := mgr.updateRollingStatefulSetPods(
		ctx,
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("update RollingUpdate StatefulSet: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	if len(client.Calls["Update"]) != 1 {
		t.Fatalf("Kubernetes updates = %d, want one partition update", len(client.Calls["Update"]))
	}
	assertNoRollingUpdatePodDelete(t, client)
	if got := testutil.ToFloat64(decisionMetric); got != decisionBefore+1 {
		t.Fatalf("decision metric = %f, want %f", got, decisionBefore+1)
	}
	if got := testutil.ToFloat64(
		splmetrics.SHCRolloutPartitionAdvanceCounter,
	); got != partitionBefore+1 {
		t.Fatalf("partition metric = %f, want %f", got, partitionBefore+1)
	}
	if !strings.Contains(
		mgr.cr.Status.Message,
		string(upgrade.SHCRolloutReasonPartitionAdvanceAuthorized),
	) {
		t.Fatalf("status message = %q, want rollout reason", mgr.cr.Status.Message)
	}
	assertRolloutEvent(t, recorder, EventReasonSHCRolloutAdvanced, corev1.EventTypeNormal)

	stored := &appsv1.StatefulSet{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      statefulSet.GetName(),
	}, stored); err != nil {
		t.Fatalf("get StatefulSet: %v", err)
	}
	if stored.Spec.UpdateStrategy.RollingUpdate == nil ||
		stored.Spec.UpdateStrategy.RollingUpdate.Partition == nil ||
		*stored.Spec.UpdateStrategy.RollingUpdate.Partition != target {
		t.Fatalf("stored strategy = %#v, want partition %d",
			stored.Spec.UpdateStrategy, target)
	}
}

func TestRollingUpdateControllerRetriesPartitionConflictWithoutSkippingOrdinal(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	// The mock client stores pointers, unlike the Kubernetes API. Keep the
	// reconcile-local object independent so a failed Update cannot mutate the
	// object representing persisted API state.
	statefulSet = statefulSet.DeepCopy()
	mgr.statefulSet = statefulSet
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		ReplacementAuthorizedAt: &authorizedAt,
	}
	conflict := k8serrors.NewConflict(
		schema.GroupResource{
			Group:    appsv1.GroupName,
			Resource: "statefulsets",
		},
		statefulSet.GetName(),
		fmt.Errorf("simulated concurrent StatefulSet update"),
	)
	client.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = conflict

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if !k8serrors.IsConflict(err) {
		t.Fatalf("partition conflict error = %v, want Kubernetes Conflict", err)
	}
	if phase != enterpriseApi.PhaseError {
		t.Fatalf("partition conflict phase = %q, want %q", phase, enterpriseApi.PhaseError)
	}
	assertNoRollingUpdatePodDelete(t, client)

	stored := getRollingUpdateFixtureStatefulSet(t, client, statefulSet)
	assertRollingUpdatePartition(t, stored.Spec.UpdateStrategy, 3)
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != target ||
		operation.DesiredRevision != "revision-2" ||
		operation.ReplacementAuthorizedAt == nil {
		t.Fatalf(
			"authorization after conflict = %#v, want persisted ordinal 2 authorization",
			operation,
		)
	}

	// A new reconciliation discards the locally mutated object and observes
	// the unchanged partition from the API before retrying the same ordinal.
	client.InduceErrorKind[splcommon.MockClientInduceErrorUpdate] = nil
	client.ResetCalls()
	mgr.statefulSet = stored
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		stored,
		3,
	)
	if err != nil {
		t.Fatalf("retry partition update: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("partition retry phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	if len(client.Calls["Update"]) != 1 {
		t.Fatalf(
			"partition retry updates = %d, want one",
			len(client.Calls["Update"]),
		)
	}
	assertNoRollingUpdatePodDelete(t, client)

	stored = getRollingUpdateFixtureStatefulSet(t, client, stored)
	assertRollingUpdatePartition(t, stored.Spec.UpdateStrategy, target)
	operation = mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != target {
		t.Fatalf(
			"operation after partition retry = %#v, want ordinal 2",
			operation,
		)
	}
}

func TestRollingUpdateControllerWaitsForKubernetesWithoutDeletingPod(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageWaitingForTermination,
		ReplacementAuthorizedAt: &authorizedAt,
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("update RollingUpdate StatefulSet: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	assertNoRollingUpdatePodDelete(t, client)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("unexpected update while waiting for Kubernetes: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerBlocksFailedReplacementWithoutAdvancing(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-2"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
		ReplacementAuthorizedAt: &authorizedAt,
	}
	setRollingUpdateFixturePodReady(t, client, statefulSet, target, false)
	client.ResetCalls()

	for observation := 1; observation <= 3; observation++ {
		phase, err := mgr.updateRollingStatefulSetPods(
			context.Background(),
			statefulSet,
			3,
		)
		if err != nil {
			t.Fatalf(
				"waiting observation %d returned error: %v",
				observation,
				err,
			)
		}
		if phase != enterpriseApi.PhaseUpdating {
			t.Fatalf(
				"waiting observation %d phase = %q, want Updating",
				observation,
				phase,
			)
		}
		if len(client.Calls["Update"]) != 0 {
			t.Fatalf(
				"waiting observation %d changed Kubernetes state: %v",
				observation,
				client.Calls["Update"],
			)
		}
		assertRollingUpdatePartition(
			t,
			statefulSet.Spec.UpdateStrategy,
			target,
		)
		assertNoRollingUpdatePodDelete(t, client)
		if !strings.Contains(
			mgr.cr.Status.Message,
			string(upgrade.SHCRolloutReasonWaitingForKubernetes),
		) {
			t.Fatalf(
				"waiting status = %q, want %s",
				mgr.cr.Status.Message,
				upgrade.SHCRolloutReasonWaitingForKubernetes,
			)
		}
		if operation := mgr.cr.Status.LifecycleOperation; operation == nil ||
			operation.TargetOrdinal == nil ||
			*operation.TargetOrdinal != target ||
			operation.Stage !=
				enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer {
			t.Fatalf(
				"waiting operation = %#v, want ordinal 2 container wait",
				operation,
			)
		}
	}

	operation := mgr.cr.Status.LifecycleOperation
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageBlocked
	operation.Reason =
		enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed
	operation.Message = "replacement Splunk process did not become ready"
	recorder := &mockEventRecorder{}
	ctx := context.WithValue(
		context.Background(),
		splcommon.EventPublisherKey,
		&K8EventPublisher{recorder: recorder, instance: mgr.cr},
	)
	client.ResetCalls()

	phase, err := mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
	if err == nil {
		t.Fatal("expected terminal replacement failure to block rollout")
	}
	if phase != enterpriseApi.PhaseError {
		t.Fatalf("blocked replacement phase = %q, want %q", phase, enterpriseApi.PhaseError)
	}
	if !strings.Contains(
		err.Error(),
		string(upgrade.SHCRolloutReasonLifecycleBlocked),
	) {
		t.Fatalf("blocked replacement error = %q, want lifecycle reason", err)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("blocked replacement changed Kubernetes state: %v", client.Calls["Update"])
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, target)
	assertNoRollingUpdatePodDelete(t, client)
	assertRolloutEvent(
		t,
		recorder,
		EventReasonSHCRolloutBlocked,
		corev1.EventTypeWarning,
	)

	operation = mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != target ||
		operation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageBlocked ||
		operation.Reason !=
			enterpriseApi.SearchHeadClusterLifecycleReasonSplunkStartupFailed {
		t.Fatalf(
			"blocked operation = %#v, want classified ordinal 2 startup failure",
			operation,
		)
	}
}

func TestRollingUpdateControllerHoldsPartitionWhileMemberCannotRejoinCaptain(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-2"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	rejoinStartedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		Reason:                  enterpriseApi.SearchHeadClusterLifecycleReasonMemberNotRegistered,
		ReplacementAuthorizedAt: &authorizedAt,
		MemberRejoinStartedAt:   &rejoinStartedAt,
	}
	client.ResetCalls()

	for observation := 1; observation <= 3; observation++ {
		phase, err := mgr.updateRollingStatefulSetPods(
			context.Background(),
			statefulSet,
			3,
		)
		if err != nil {
			t.Fatalf(
				"member-rejoin observation %d returned error: %v",
				observation,
				err,
			)
		}
		if phase != enterpriseApi.PhaseUpdating {
			t.Fatalf(
				"member-rejoin observation %d phase = %q, want Updating",
				observation,
				phase,
			)
		}
		assertRollingUpdatePartition(
			t,
			statefulSet.Spec.UpdateStrategy,
			target,
		)
		if len(client.Calls["Update"]) != 0 {
			t.Fatalf(
				"member-rejoin observation %d changed Kubernetes state: %v",
				observation,
				client.Calls["Update"],
			)
		}
		assertNoRollingUpdatePodDelete(t, client)
		operation := mgr.cr.Status.LifecycleOperation
		if operation == nil ||
			operation.TargetOrdinal == nil ||
			*operation.TargetOrdinal != target ||
			operation.Stage !=
				enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin {
			t.Fatalf(
				"member-rejoin operation = %#v, want retained ordinal 2",
				operation,
			)
		}
	}
}

func TestRollingUpdateControllerBlocksAuthorizedTargetAfterManualPodDeletion(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		ReplacementAuthorizedAt: &authorizedAt,
	}

	unplannedPod := &corev1.Pod{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      statefulSet.GetName() + "-1",
	}, unplannedPod); err != nil {
		t.Fatalf("get manually deleted Pod: %v", err)
	}
	if err := client.Delete(context.Background(), unplannedPod); err != nil {
		t.Fatalf("simulate manual Pod deletion: %v", err)
	}
	client.ResetCalls()

	recorder := &mockEventRecorder{}
	ctx := context.WithValue(
		context.Background(),
		splcommon.EventPublisherKey,
		&K8EventPublisher{recorder: recorder, instance: mgr.cr},
	)
	phase, err := mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
	if err == nil {
		t.Fatal("expected an existing unavailable Pod to block partition advancement")
	}
	if phase != enterpriseApi.PhaseError {
		t.Fatalf("manual deletion phase = %q, want %q", phase, enterpriseApi.PhaseError)
	}
	if !strings.Contains(
		err.Error(),
		string(upgrade.SHCRolloutReasonExistingUnavailablePod),
	) {
		t.Fatalf(
			"manual deletion error = %q, want %s",
			err,
			upgrade.SHCRolloutReasonExistingUnavailablePod,
		)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("manual deletion advanced partition: %v", client.Calls["Update"])
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	assertNoRollingUpdatePodDelete(t, client)
	assertRolloutEvent(
		t,
		recorder,
		EventReasonSHCRolloutBlocked,
		corev1.EventTypeWarning,
	)

	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != target ||
		operation.ReplacementAuthorizedAt == nil {
		t.Fatalf(
			"operation after manual deletion = %#v, want retained ordinal 2 authorization",
			operation,
		)
	}
}

func TestRollingUpdateRecoveryObservationProjectsWaitingStateReadOnly(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageWaitingForTermination,
		TargetPodUID:            "original-pod-uid",
		ReplacementAuthorizedAt: &authorizedAt,
	}
	decisionMetric := splmetrics.SHCRolloutDecisionCounters.WithLabelValues(
		string(upgrade.SHCRolloutActionWait),
		string(upgrade.SHCRolloutReasonWaitingForKubernetes),
	)
	metricBefore := testutil.ToFloat64(decisionMetric)

	err := mgr.recordRollingUpdateObservation(context.Background(), statefulSet)
	if err != nil {
		t.Fatalf("record recovery observation: %v", err)
	}
	if got := testutil.ToFloat64(decisionMetric); got != metricBefore+1 {
		t.Fatalf("waiting metric = %f, want %f", got, metricBefore+1)
	}
	if !strings.Contains(
		mgr.cr.Status.Message,
		string(upgrade.SHCRolloutReasonWaitingForKubernetes),
	) {
		t.Fatalf("status message = %q, want Kubernetes wait reason", mgr.cr.Status.Message)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("read-only observation mutated Kubernetes state: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestRollingUpdateControllerReportsStableRevisionReady(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-2",
		"revision-2",
		[]string{"revision-2", "revision-2", "revision-2"},
	)

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("update RollingUpdate StatefulSet: %v", err)
	}
	if phase != enterpriseApi.PhaseReady {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseReady)
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestLifecycleRecoveryWaitsForRollingPartitionAuthorization(t *testing.T) {
	target := int32(2)
	partition := int32(3)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		TargetOrdinal: &target,
		TargetPodUID:  "original-pod-uid",
		Stage:         enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
	}
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
					Partition: &partition,
				},
			},
		},
	}

	if lifecycleRecoveryActiveForStatefulSet(statefulSet, operation) {
		t.Fatal("recovery became active before the partition authorized replacement")
	}

	partition = target
	if !lifecycleRecoveryActiveForStatefulSet(statefulSet, operation) {
		t.Fatal("recovery did not become active after the partition authorized replacement")
	}
}

func TestLifecycleRecoveryPreservesOnDeleteOrdering(t *testing.T) {
	target := int32(2)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		TargetOrdinal: &target,
		TargetPodUID:  "original-pod-uid",
		Stage:         enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
	}
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
		},
	}

	if !lifecycleRecoveryActiveForStatefulSet(statefulSet, operation) {
		t.Fatal("OnDelete recovery ordering changed")
	}
}

func TestRollingUpdateControllerBlockedDecisionEmitsWarning(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	for _, ordinal := range []int32{1, 2} {
		pod := &corev1.Pod{}
		if err := client.Get(context.Background(), types.NamespacedName{
			Namespace: statefulSet.GetNamespace(),
			Name:      fmt.Sprintf("%s-%d", statefulSet.GetName(), ordinal),
		}, pod); err != nil {
			t.Fatalf("get Pod %d: %v", ordinal, err)
		}
		pod.Status.Conditions[0].Status = corev1.ConditionFalse
		if err := client.Update(context.Background(), pod); err != nil {
			t.Fatalf("update Pod %d: %v", ordinal, err)
		}
	}
	client.ResetCalls()

	recorder := &mockEventRecorder{}
	ctx := context.WithValue(
		context.Background(),
		splcommon.EventPublisherKey,
		&K8EventPublisher{recorder: recorder, instance: mgr.cr},
	)
	decisionMetric := splmetrics.SHCRolloutDecisionCounters.WithLabelValues(
		string(upgrade.SHCRolloutActionBlock),
		string(upgrade.SHCRolloutReasonTooManyUnavailable),
	)
	metricBefore := testutil.ToFloat64(decisionMetric)

	phase, err := mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
	if err == nil {
		t.Fatal("expected blocked rollout error")
	}
	if phase != enterpriseApi.PhaseError {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseError)
	}
	if got := testutil.ToFloat64(decisionMetric); got != metricBefore+1 {
		t.Fatalf("blocked metric = %f, want %f", got, metricBefore+1)
	}
	if !strings.Contains(
		mgr.cr.Status.Message,
		string(upgrade.SHCRolloutReasonTooManyUnavailable),
	) {
		t.Fatalf("status message = %q, want blocked reason", mgr.cr.Status.Message)
	}
	assertRolloutEvent(t, recorder, EventReasonSHCRolloutBlocked, corev1.EventTypeWarning)
	assertNoRollingUpdatePodDelete(t, client)
}

func TestRollingUpdateStatusProjectionPersistsWithoutReconcileError(t *testing.T) {
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}
	cr.Status.Message = shcRollingUpdateStatusPrefix +
		"WaitingForKubernetes: waiting for ordinal 2"
	client := spltest.NewMockClient()
	if err := client.Create(context.Background(), cr); err != nil {
		t.Fatalf("create SearchHeadCluster: %v", err)
	}

	current, err := fetchCurrentCRWithStatusUpdate(
		context.Background(),
		client,
		cr,
		nil,
	)
	if err != nil {
		t.Fatalf("fetch SearchHeadCluster status: %v", err)
	}
	got := current.(*enterpriseApi.SearchHeadCluster).Status.Message
	if got != cr.Status.Message {
		t.Fatalf("status message = %q, want %q", got, cr.Status.Message)
	}
}

func TestRollingUpdateControllerPauseAndResumePreservesAuthorization(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	mgr.cr.Annotations = map[string]string{
		enterpriseApi.SearchHeadClusterPausedAnnotation: "true",
	}
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		ReplacementAuthorizedAt: &authorizedAt,
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("paused RollingUpdate: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("paused phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("paused rollout changed partition: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)

	delete(mgr.cr.Annotations, enterpriseApi.SearchHeadClusterPausedAnnotation)
	client.ResetCalls()
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("resumed RollingUpdate: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating || len(client.Calls["Update"]) != 1 {
		t.Fatalf("resume phase=%q updates=%d, want Updating and one partition update",
			phase, len(client.Calls["Update"]))
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestRollingUpdateControllerSupersedingRevisionReplacesStaleOperation(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-3",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "old-revision-operation",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		ReplacementAuthorizedAt: &authorizedAt,
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("superseding RollingUpdate: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.DesiredRevision != "revision-3" ||
		operation.ReplacementAuthorizedAt != nil {
		t.Fatalf("replacement operation = %#v, want fresh revision-3 preparation", operation)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("superseding revision advanced partition: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestRollingUpdateControllerRollbackCompletionResetsPartition(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-1",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "rollback-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-1",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		ReplacementAuthorizedAt: &authorizedAt,
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("complete rollback: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating || len(client.Calls["Update"]) != 1 {
		t.Fatalf("completion phase=%q updates=%d, want partition reset",
			phase, len(client.Calls["Update"]))
	}
	assertNoRollingUpdatePodDelete(t, client)

	stored := &appsv1.StatefulSet{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      statefulSet.GetName(),
	}, stored); err != nil {
		t.Fatalf("get StatefulSet: %v", err)
	}
	if stored.Spec.UpdateStrategy.RollingUpdate == nil ||
		stored.Spec.UpdateStrategy.RollingUpdate.Partition == nil ||
		*stored.Spec.UpdateStrategy.RollingUpdate.Partition != 3 {
		t.Fatalf("stored strategy = %#v, want fail-closed partition 3",
			stored.Spec.UpdateStrategy)
	}

	client.ResetCalls()
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		stored,
		3,
	)
	if err != nil {
		t.Fatalf("observe reset rollback: %v", err)
	}
	if phase != enterpriseApi.PhaseReady {
		t.Fatalf("phase after reset = %q, want %q", phase, enterpriseApi.PhaseReady)
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestRollingUpdateControllerHoldsPartitionDuringOnDeleteRollback(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-2"},
	)
	mgr.cr.Spec.LifecyclePolicy.PodUpdateStrategy =
		enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "pod-update-2",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               statefulSet.GetName() + "-2",
		TargetOrdinal:           &target,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin,
		ReplacementAuthorizedAt: &authorizedAt,
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil {
		t.Fatalf("hold rollback partition: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("rollback hold phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("rollback hold changed partition: %v", client.Calls["Update"])
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, target)
	assertNoRollingUpdatePodDelete(t, client)
	if !strings.Contains(
		mgr.cr.Status.Message,
		string(upgrade.SHCRolloutReasonRollbackPending),
	) {
		t.Fatalf(
			"rollback hold status = %q, want %s",
			mgr.cr.Status.Message,
			upgrade.SHCRolloutReasonRollbackPending,
		)
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != target ||
		operation.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin {
		t.Fatalf(
			"rollback hold operation = %#v, want active ordinal 2 recovery",
			operation,
		)
	}
}

func TestRollingUpdateControllerCompletesThreeMembersInReverseOrdinalOrder(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	ctx := context.Background()
	observedPartitions := make([]int32, 0, 4)

	for target := int32(2); target >= 0; target-- {
		if target == 2 {
			phase, err := mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
			if err != nil {
				t.Fatalf("prepare ordinal %d: %v", target, err)
			}
			if phase != enterpriseApi.PhaseUpdating {
				t.Fatalf("prepare ordinal %d phase = %q, want Updating", target, phase)
			}
		}

		operation := mgr.cr.Status.LifecycleOperation
		if operation == nil ||
			operation.TargetOrdinal == nil ||
			*operation.TargetOrdinal != target ||
			operation.DesiredRevision != "revision-2" ||
			operation.ReplacementAuthorizedAt != nil {
			t.Fatalf(
				"ordinal %d preparation = %#v, want unapproved durable operation",
				target,
				operation,
			)
		}
		assertRollingUpdatePartition(
			t,
			statefulSet.Spec.UpdateStrategy,
			target+1,
		)
		assertNoRollingUpdatePodDelete(t, client)

		authorizedAt := metav1.Now()
		operation.Stage =
			enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement
		operation.ReplacementAuthorizedAt = &authorizedAt
		client.ResetCalls()

		phase, err := mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
		if err != nil {
			t.Fatalf("authorize ordinal %d: %v", target, err)
		}
		if phase != enterpriseApi.PhaseUpdating {
			t.Fatalf("authorize ordinal %d phase = %q, want Updating", target, phase)
		}
		if len(client.Calls["Update"]) != 1 {
			t.Fatalf(
				"ordinal %d partition updates = %d, want one",
				target,
				len(client.Calls["Update"]),
			)
		}
		assertNoRollingUpdatePodDelete(t, client)

		statefulSet = getRollingUpdateFixtureStatefulSet(t, client, statefulSet)
		mgr.statefulSet = statefulSet
		assertRollingUpdatePartition(
			t,
			statefulSet.Spec.UpdateStrategy,
			target,
		)
		observedPartitions = append(observedPartitions, target)

		setRollingUpdateFixturePodRevision(
			t,
			client,
			statefulSet,
			target,
			"revision-2",
		)
		operation.Stage =
			enterpriseApi.SearchHeadClusterLifecycleStageWaitingForMemberRejoin
		client.ResetCalls()

		phase, err = mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
		if err != nil {
			t.Fatalf("wait for ordinal %d SHC recovery: %v", target, err)
		}
		if phase != enterpriseApi.PhaseUpdating {
			t.Fatalf("recovery wait ordinal %d phase = %q, want Updating", target, phase)
		}
		if len(client.Calls["Update"]) != 0 {
			t.Fatalf(
				"ordinal %d recovery wait changed Kubernetes state: %v",
				target,
				client.Calls["Update"],
			)
		}
		assertRollingUpdatePartition(
			t,
			statefulSet.Spec.UpdateStrategy,
			target,
		)
		assertNoRollingUpdatePodDelete(t, client)

		operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageCompleted
		client.ResetCalls()
		phase, err = mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
		if err != nil {
			t.Fatalf("complete ordinal %d: %v", target, err)
		}
		if phase != enterpriseApi.PhaseUpdating {
			t.Fatalf("complete ordinal %d phase = %q, want Updating", target, phase)
		}
		assertNoRollingUpdatePodDelete(t, client)

		if target > 0 {
			if len(client.Calls["Update"]) != 0 {
				t.Fatalf(
					"ordinal %d completion changed partition before preparing next: %v",
					target,
					client.Calls["Update"],
				)
			}
			nextOperation := mgr.cr.Status.LifecycleOperation
			if nextOperation == nil ||
				nextOperation.TargetOrdinal == nil ||
				*nextOperation.TargetOrdinal != target-1 ||
				nextOperation.ReplacementAuthorizedAt != nil {
				t.Fatalf(
					"operation after ordinal %d = %#v, want preparation for ordinal %d",
					target,
					nextOperation,
					target-1,
				)
			}
			assertRollingUpdatePartition(
				t,
				statefulSet.Spec.UpdateStrategy,
				target,
			)
			continue
		}

		if len(client.Calls["Update"]) != 1 {
			t.Fatalf(
				"final recovery updates = %d, want one fail-closed partition reset",
				len(client.Calls["Update"]),
			)
		}
		statefulSet = getRollingUpdateFixtureStatefulSet(t, client, statefulSet)
		mgr.statefulSet = statefulSet
		assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
		observedPartitions = append(observedPartitions, 3)
	}

	statefulSet.Status.CurrentRevision = statefulSet.Status.UpdateRevision
	client.ResetCalls()
	phase, err := mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
	if err != nil {
		t.Fatalf("observe converged rollout: %v", err)
	}
	if phase != enterpriseApi.PhaseReady {
		t.Fatalf("converged rollout phase = %q, want Ready", phase)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("converged rollout changed Kubernetes state: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)

	wantPartitions := []int32{2, 1, 0, 3}
	if !reflect.DeepEqual(observedPartitions, wantPartitions) {
		t.Fatalf(
			"partition history = %v, want reverse rollout and reset %v",
			observedPartitions,
			wantPartitions,
		)
	}
}

func rollingUpdateControllerFixture(
	t *testing.T,
	partition int32,
	currentRevision string,
	updateRevision string,
	podRevisions []string,
) (*searchHeadClusterPodManager, *appsv1.StatefulSet, *spltest.MockClient) {
	t.Helper()
	replicas := int32(len(podRevisions))
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			Replicas: replicas,
			LifecyclePolicy: &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				PodUpdateStrategy: enterpriseApi.SearchHeadClusterPodUpdateStrategyRollingUpdate,
			},
		},
		Status: enterpriseApi.SearchHeadClusterStatus{
			Initialized:    true,
			MinPeersJoined: true,
			CaptainReady:   true,
		},
	}
	statefulSet := &appsv1.StatefulSet{
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
		Status: appsv1.StatefulSetStatus{
			Replicas:        replicas,
			ReadyReplicas:   replicas,
			CurrentRevision: currentRevision,
			UpdateRevision:  updateRevision,
		},
	}

	client := spltest.NewMockClient()
	ctx := context.Background()
	if err := client.Create(ctx, statefulSet); err != nil {
		t.Fatalf("create StatefulSet: %v", err)
	}
	for ordinal, revision := range podRevisions {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", statefulSet.GetName(), ordinal),
				Namespace: statefulSet.GetNamespace(),
				Labels: map[string]string{
					"controller-revision-hash": revision,
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		if err := client.Create(ctx, pod); err != nil {
			t.Fatalf("create Pod %d: %v", ordinal, err)
		}
	}
	client.ResetCalls()

	mgr := &searchHeadClusterPodManager{
		c:           client,
		cr:          cr,
		statefulSet: statefulSet,
	}
	return mgr, statefulSet, client
}

func configureImageUpgradeControllerFixture(
	mgr *searchHeadClusterPodManager,
	statefulSet *appsv1.StatefulSet,
	sourceImage string,
	targetImage string,
) {
	statefulSet.Spec.Template.Spec.Containers = []corev1.Container{
		{Name: "splunk", Image: targetImage},
	}
	mgr.cr.Status.Captain = statefulSet.GetName() + "-0"
	mgr.cr.Status.Members = make(
		[]enterpriseApi.SearchHeadClusterMemberStatus,
		*statefulSet.Spec.Replicas,
	)
	for ordinal := range mgr.cr.Status.Members {
		mgr.cr.Status.Members[ordinal] =
			enterpriseApi.SearchHeadClusterMemberStatus{
				Name: fmt.Sprintf(
					"%s-%d",
					statefulSet.GetName(),
					ordinal,
				),
				Status:     "Up",
				Registered: true,
			}
	}
	startedAt := metav1.NewTime(
		time.Date(2026, 7, 25, 13, 0, 0, 0, time.UTC),
	)
	mgr.cr.Status.ImageUpgrade =
		&enterpriseApi.SearchHeadClusterImageUpgradeStatus{
			OperationID: fmt.Sprintf(
				"image-upgrade:%s:%s",
				statefulSet.GetName(),
				statefulSet.Status.UpdateRevision,
			),
			StatefulSetName: statefulSet.GetName(),
			DesiredRevision: statefulSet.Status.UpdateRevision,
			SourceImage:     sourceImage,
			TargetImage:     targetImage,
			TargetReplicas:  *statefulSet.Spec.Replicas,
			Phase: enterpriseApi.
				SearchHeadClusterImageUpgradePhasePendingInitialization,
			Reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonWorkflowRecorded,
			StartedAt:          &startedAt,
			PhaseStartedAt:     &startedAt,
			LastTransitionTime: &startedAt,
		}
}

func getRollingUpdateFixtureStatefulSet(
	t *testing.T,
	client *spltest.MockClient,
	statefulSet *appsv1.StatefulSet,
) *appsv1.StatefulSet {
	t.Helper()
	stored := &appsv1.StatefulSet{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      statefulSet.GetName(),
	}, stored); err != nil {
		t.Fatalf("get StatefulSet: %v", err)
	}
	return stored
}

func setRollingUpdateFixturePodRevision(
	t *testing.T,
	client *spltest.MockClient,
	statefulSet *appsv1.StatefulSet,
	ordinal int32,
	revision string,
) {
	t.Helper()
	pod := &corev1.Pod{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      fmt.Sprintf("%s-%d", statefulSet.GetName(), ordinal),
	}, pod); err != nil {
		t.Fatalf("get Pod %d: %v", ordinal, err)
	}
	pod.Labels["controller-revision-hash"] = revision
	if err := client.Update(context.Background(), pod); err != nil {
		t.Fatalf("update Pod %d revision: %v", ordinal, err)
	}
}

func setRollingUpdateFixturePodReady(
	t *testing.T,
	client *spltest.MockClient,
	statefulSet *appsv1.StatefulSet,
	ordinal int32,
	ready bool,
) {
	t.Helper()
	pod := &corev1.Pod{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      fmt.Sprintf("%s-%d", statefulSet.GetName(), ordinal),
	}, pod); err != nil {
		t.Fatalf("get Pod %d: %v", ordinal, err)
	}
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	for index := range pod.Status.Conditions {
		if pod.Status.Conditions[index].Type == corev1.PodReady {
			pod.Status.Conditions[index].Status = status
		}
	}
	if err := client.Update(context.Background(), pod); err != nil {
		t.Fatalf("update Pod %d readiness: %v", ordinal, err)
	}
}

func assertNoRollingUpdatePodDelete(t *testing.T, client *spltest.MockClient) {
	t.Helper()
	if len(client.Calls["Delete"]) != 0 {
		t.Fatalf("RollingUpdate controller called Delete: %v", client.Calls["Delete"])
	}
}

func assertRolloutEvent(
	t *testing.T,
	recorder *mockEventRecorder,
	reason string,
	eventType string,
) {
	t.Helper()
	for _, event := range recorder.events {
		if event.reason == reason {
			if event.eventType != eventType {
				t.Fatalf("event %s type = %q, want %q", reason, event.eventType, eventType)
			}
			return
		}
	}
	t.Fatalf("event %s not found in %#v", reason, recorder.events)
}
