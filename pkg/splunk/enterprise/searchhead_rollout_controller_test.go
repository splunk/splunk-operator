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
	"slices"
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

func TestRollingUpdateControllerRecordsSupportedImageWorkflowBeforeInitialization(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	statefulSet.Spec.Template.Spec.Containers[0].Image =
		"splunk/splunk:10.0.0"

	oldValidate := validateSearchHeadClusterImageUpgradePath
	oldInitiate := initiateSearchHeadClusterUpgrade
	t.Cleanup(func() {
		validateSearchHeadClusterImageUpgradePath = oldValidate
		initiateSearchHeadClusterUpgrade = oldInitiate
	})
	validations := 0
	validateSearchHeadClusterImageUpgradePath = func(
		_ context.Context,
		sourceImage string,
		targetImage string,
	) (upgrade.SHCImageUpgradePathDecision, error) {
		validations++
		if sourceImage != "splunk/splunk:9.4.0" ||
			targetImage != "splunk/splunk:10.0.0" {
			t.Fatalf(
				"validated path %q -> %q",
				sourceImage,
				targetImage,
			)
		}
		return upgrade.SHCImageUpgradePathSupported, nil
	}
	initializationCalls := 0
	initiateSearchHeadClusterUpgrade = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		initializationCalls++
		return nil
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("record image workflow phase=%q error=%v", phase, err)
	}
	imageUpgrade := mgr.cr.Status.ImageUpgrade
	if imageUpgrade == nil ||
		imageUpgrade.Phase != enterpriseApi.
			SearchHeadClusterImageUpgradePhasePendingInitialization ||
		imageUpgrade.Reason != enterpriseApi.
			SearchHeadClusterImageUpgradeReasonWorkflowRecorded ||
		imageUpgrade.SourceImage != "splunk/splunk:9.4.0" ||
		imageUpgrade.TargetImage != "splunk/splunk:10.0.0" ||
		imageUpgrade.DesiredRevision != "revision-2" ||
		imageUpgrade.InitializationIntentAt != nil ||
		imageUpgrade.InitializationAttemptCount != 0 {
		t.Fatalf("recorded image workflow = %#v", imageUpgrade)
	}
	if validations != 1 || initializationCalls != 0 ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"identity barrier validations=%d init=%d lifecycle=%#v",
			validations,
			initializationCalls,
			mgr.cr.Status.LifecycleOperation,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("identity barrier changed Kubernetes state: %v", client.Calls["Update"])
	}

	// The durable image owner does not yield to App Framework work that
	// appears after workflow creation, and it does not initialize concurrently.
	mgr.cr.Status.AppContext.IsDeploymentInProgress = true
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("hold image owner phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.ImageUpgrade.Phase != enterpriseApi.
		SearchHeadClusterImageUpgradePhasePendingInitialization ||
		mgr.cr.Status.ImageUpgrade.InitializationIntentAt != nil ||
		initializationCalls != 0 ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"image owner overlap image=%#v init=%d lifecycle=%#v",
			mgr.cr.Status.ImageUpgrade,
			initializationCalls,
			mgr.cr.Status.LifecycleOperation,
		)
	}
	mgr.cr.Status.AppContext.IsDeploymentInProgress = false

	// The next reconciliation records intent but still cannot call Splunk.
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("record image intent phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.ImageUpgrade.Phase !=
		enterpriseApi.SearchHeadClusterImageUpgradePhaseInitializing ||
		mgr.cr.Status.ImageUpgrade.InitializationIntentAt == nil ||
		initializationCalls != 0 ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"intent barrier image=%#v init=%d lifecycle=%#v",
			mgr.cr.Status.ImageUpgrade,
			initializationCalls,
			mgr.cr.Status.LifecycleOperation,
		)
	}
	// Validation belongs to workflow creation and is not repeated after the
	// durable operation is recorded.
	if validations != 1 {
		t.Fatalf("path validations = %d, want one", validations)
	}
}

func TestRollingUpdateControllerBlocksUnapprovedImagePath(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	tests := []struct {
		name     string
		decision upgrade.SHCImageUpgradePathDecision
		reason   enterpriseApi.SearchHeadClusterImageUpgradeReason
	}{
		{
			name:     "unknown",
			decision: upgrade.SHCImageUpgradePathUnknown,
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonUnknownUpgradePath,
		},
		{
			name:     "unsupported",
			decision: upgrade.SHCImageUpgradePathUnsupported,
			reason: enterpriseApi.
				SearchHeadClusterImageUpgradeReasonUnsupportedUpgradePath,
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
			statefulSet.Spec.Template.Spec.Containers[0].Image =
				"splunk/splunk:10.0.0"

			oldValidate := validateSearchHeadClusterImageUpgradePath
			t.Cleanup(func() {
				validateSearchHeadClusterImageUpgradePath = oldValidate
			})
			validateSearchHeadClusterImageUpgradePath = func(
				context.Context,
				string,
				string,
			) (upgrade.SHCImageUpgradePathDecision, error) {
				return test.decision, nil
			}

			phase, err := mgr.updateRollingStatefulSetPods(
				context.Background(),
				statefulSet,
				3,
			)
			if err == nil || phase != enterpriseApi.PhaseError {
				t.Fatalf("unapproved path phase=%q error=%v", phase, err)
			}
			if mgr.cr.Status.ImageUpgrade != nil ||
				mgr.cr.Status.LifecycleOperation != nil ||
				!strings.Contains(
					mgr.cr.Status.Message,
					string(test.reason),
				) {
				t.Fatalf(
					"unapproved path image=%#v lifecycle=%#v message=%q",
					mgr.cr.Status.ImageUpgrade,
					mgr.cr.Status.LifecycleOperation,
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
					"unapproved path changed Kubernetes state: %v",
					client.Calls["Update"],
				)
			}
		})
	}
}

func TestRollingUpdateControllerLeavesValidatorErrorRetryable(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	statefulSet.Spec.Template.Spec.Containers[0].Image =
		"splunk/splunk:10.0.0"

	oldValidate := validateSearchHeadClusterImageUpgradePath
	t.Cleanup(func() {
		validateSearchHeadClusterImageUpgradePath = oldValidate
	})
	validateSearchHeadClusterImageUpgradePath = func(
		context.Context,
		string,
		string,
	) (upgrade.SHCImageUpgradePathDecision, error) {
		return upgrade.SHCImageUpgradePathUnknown,
			fmt.Errorf("compatibility source unavailable")
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err == nil || phase != enterpriseApi.PhaseError {
		t.Fatalf("validator error phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.ImageUpgrade != nil ||
		mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"validator error changed ownership image=%#v lifecycle=%#v",
			mgr.cr.Status.ImageUpgrade,
			mgr.cr.Status.LifecycleOperation,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("validator error changed Kubernetes state: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerBlocksMixedImagesBeforeWorkflowCreation(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	statefulSet.Spec.Template.Spec.Containers[0].Image =
		"splunk/splunk:10.0.0"
	setRollingUpdateFixturePodImage(
		t,
		client,
		statefulSet,
		2,
		"splunk/splunk:9.3.0",
	)
	client.ResetCalls()

	oldValidate := validateSearchHeadClusterImageUpgradePath
	t.Cleanup(func() {
		validateSearchHeadClusterImageUpgradePath = oldValidate
	})
	validationCalls := 0
	validateSearchHeadClusterImageUpgradePath = func(
		context.Context,
		string,
		string,
	) (upgrade.SHCImageUpgradePathDecision, error) {
		validationCalls++
		return upgrade.SHCImageUpgradePathSupported, nil
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err == nil || phase != enterpriseApi.PhaseError {
		t.Fatalf("mixed images phase=%q error=%v", phase, err)
	}
	if validationCalls != 0 ||
		mgr.cr.Status.ImageUpgrade != nil ||
		mgr.cr.Status.LifecycleOperation != nil ||
		!strings.Contains(
			mgr.cr.Status.Message,
			string(enterpriseApi.
				SearchHeadClusterImageUpgradeReasonMixedSourceImages),
		) {
		t.Fatalf(
			"mixed image classification validations=%d image=%#v lifecycle=%#v message=%q",
			validationCalls,
			mgr.cr.Status.ImageUpgrade,
			mgr.cr.Status.LifecycleOperation,
			mgr.cr.Status.Message,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("mixed images changed Kubernetes state: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerTreatsSameImageAsOrdinaryRollout(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, _ := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	oldValidate := validateSearchHeadClusterImageUpgradePath
	t.Cleanup(func() {
		validateSearchHeadClusterImageUpgradePath = oldValidate
	})
	validationCalls := 0
	validateSearchHeadClusterImageUpgradePath = func(
		context.Context,
		string,
		string,
	) (upgrade.SHCImageUpgradePathDecision, error) {
		validationCalls++
		return upgrade.SHCImageUpgradePathSupported, nil
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("ordinary rollout phase=%q error=%v", phase, err)
	}
	if validationCalls != 0 ||
		mgr.cr.Status.ImageUpgrade != nil ||
		mgr.cr.Status.LifecycleOperation == nil {
		t.Fatalf(
			"ordinary rollout validations=%d image=%#v lifecycle=%#v",
			validationCalls,
			mgr.cr.Status.ImageUpgrade,
			mgr.cr.Status.LifecycleOperation,
		)
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
	mgr.cr.Status.Members[0].Name = ""

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

func TestRollingUpdateControllerWaitsForKVStoreBeforeUpgradeInitialization(t *testing.T) {
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

	oldInitiate := initiateSearchHeadClusterUpgrade
	t.Cleanup(func() {
		initiateSearchHeadClusterUpgrade = oldInitiate
	})
	initializationCalls := 0
	initiateSearchHeadClusterUpgrade = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		initializationCalls++
		return nil
	}
	getSearchHeadKVStoreStatus = func(
		_ context.Context,
		_ *searchHeadClusterPodManager,
		ordinal int32,
	) (string, error) {
		if ordinal == 1 {
			return "starting", nil
		}
		return "ready", nil
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("persist initialization intent phase=%q error=%v", phase, err)
	}
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("KV Store preflight phase=%q error=%v", phase, err)
	}
	if initializationCalls != 0 ||
		mgr.cr.Status.ImageUpgrade.InitializationAttemptCount != 0 ||
		!strings.Contains(mgr.cr.Status.Message, "KV Store") ||
		!strings.Contains(
			mgr.cr.Status.Message,
			"splunk-stack1-search-head-1=starting",
		) {
		t.Fatalf(
			"KV Store preflight calls=%d status=%#v message=%q",
			initializationCalls,
			mgr.cr.Status.ImageUpgrade,
			mgr.cr.Status.Message,
		)
	}
}

func TestRollingUpdateControllerPersistsRecoveredOrdinalBeforeNextMember(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-2"},
	)
	configureImageUpgradeControllerFixture(
		mgr,
		statefulSet,
		"splunk/splunk:9.4.0",
		"splunk/splunk:10.0.0",
	)
	initializationSucceededAt := metav1.Now()
	mgr.cr.Status.ImageUpgrade.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers
	mgr.cr.Status.ImageUpgrade.InitializationSucceededAt =
		&initializationSucceededAt
	target := int32(2)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:     "pod-update-2",
			Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision: "revision-2",
			TargetPod:       statefulSet.GetName() + "-2",
			TargetOrdinal:   &target,
			Stage: enterpriseApi.
				SearchHeadClusterLifecycleStageCompleted,
			ReplacementAuthorizedAt: &authorizedAt,
		}
	setRollingUpdateFixturePodImage(
		t,
		client,
		statefulSet,
		2,
		"splunk/splunk:10.0.0",
	)
	client.ResetCalls()

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("record recovered ordinal phase=%q error=%v", phase, err)
	}
	if !reflect.DeepEqual(
		mgr.cr.Status.ImageUpgrade.CompletedOrdinals,
		[]int32{2},
	) {
		t.Fatalf(
			"completed ordinals = %v, want [2]",
			mgr.cr.Status.ImageUpgrade.CompletedOrdinals,
		)
	}
	if mgr.cr.Status.LifecycleOperation.TargetOrdinal == nil ||
		*mgr.cr.Status.LifecycleOperation.TargetOrdinal != 2 {
		t.Fatalf(
			"next member started before ordinal persistence: %#v",
			mgr.cr.Status.LifecycleOperation,
		)
	}

	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("prepare next ordinal phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.LifecycleOperation.TargetOrdinal == nil ||
		*mgr.cr.Status.LifecycleOperation.TargetOrdinal != 1 ||
		!reflect.DeepEqual(
			mgr.cr.Status.ImageUpgrade.CompletedOrdinals,
			[]int32{2},
		) {
		t.Fatalf(
			"next member lifecycle=%#v completed=%v",
			mgr.cr.Status.LifecycleOperation,
			mgr.cr.Status.ImageUpgrade.CompletedOrdinals,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 2)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("ordinal projection moved partition: %v", client.Calls["Update"])
	}
}

func TestRollingUpdateControllerFinalizationBarriersRetryAndReplay(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-2",
		"revision-2",
		[]string{"revision-2", "revision-2", "revision-2"},
	)
	configureImageUpgradeControllerFixture(
		mgr,
		statefulSet,
		"splunk/splunk:9.4.0",
		"splunk/splunk:10.0.0",
	)
	initializationSucceededAt := metav1.Now()
	mgr.cr.Status.ImageUpgrade.Phase =
		enterpriseApi.SearchHeadClusterImageUpgradePhaseRollingMembers
	mgr.cr.Status.ImageUpgrade.InitializationSucceededAt =
		&initializationSucceededAt
	mgr.cr.Status.ImageUpgrade.CompletedOrdinals = []int32{0, 1, 2}
	target := int32(0)
	authorizedAt := metav1.Now()
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:             "pod-update-0",
			Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision:         "revision-2",
			TargetPod:               statefulSet.GetName() + "-0",
			TargetOrdinal:           &target,
			Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
			ReplacementAuthorizedAt: &authorizedAt,
		}
	for ordinal := int32(0); ordinal < 3; ordinal++ {
		setRollingUpdateFixturePodImage(
			t,
			client,
			statefulSet,
			ordinal,
			"splunk/splunk:10.0.0",
		)
	}
	// Prove finalization does not depend on ordinal zero.
	mgr.cr.Status.Members[0].Name = ""
	client.ResetCalls()

	now := time.Date(2026, 7, 25, 18, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterImageUpgradeNow
	oldFinalize := finalizeSearchHeadClusterUpgrade
	t.Cleanup(func() {
		searchHeadClusterImageUpgradeNow = oldNow
		finalizeSearchHeadClusterUpgrade = oldFinalize
	})
	searchHeadClusterImageUpgradeNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	finalizationTargets := make([]int32, 0, 2)
	finalizeSearchHeadClusterUpgrade = func(
		_ context.Context,
		_ *searchHeadClusterPodManager,
		ordinal int32,
	) error {
		finalizationTargets = append(finalizationTargets, ordinal)
		if len(finalizationTargets) == 1 {
			return fmt.Errorf("transient finalization failure")
		}
		return nil
	}

	// Reconcile 1 persists finalization eligibility.
	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating ||
		mgr.cr.Status.ImageUpgrade.Phase != enterpriseApi.
			SearchHeadClusterImageUpgradePhasePendingFinalization ||
		len(finalizationTargets) != 0 {
		t.Fatalf(
			"eligibility barrier phase=%q error=%v image=%#v targets=%v",
			phase,
			err,
			mgr.cr.Status.ImageUpgrade,
			finalizationTargets,
		)
	}

	// Reconcile 2 persists intent.
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating ||
		mgr.cr.Status.ImageUpgrade.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing ||
		mgr.cr.Status.ImageUpgrade.FinalizationIntentAt == nil ||
		len(finalizationTargets) != 0 {
		t.Fatalf(
			"intent barrier phase=%q error=%v image=%#v targets=%v",
			phase,
			err,
			mgr.cr.Status.ImageUpgrade,
			finalizationTargets,
		)
	}

	// Reconcile 3 calls an eligible member and persists retry evidence.
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err == nil || phase != enterpriseApi.PhaseError ||
		!reflect.DeepEqual(finalizationTargets, []int32{1}) ||
		mgr.cr.Status.ImageUpgrade.FinalizationAttemptCount != 1 ||
		mgr.cr.Status.ImageUpgrade.FinalizationSucceededAt != nil ||
		mgr.cr.Status.UpgradePhase == enterpriseApi.UpgradePhaseUpgraded {
		t.Fatalf(
			"failed finalization phase=%q error=%v image=%#v targets=%v legacy=%q",
			phase,
			err,
			mgr.cr.Status.ImageUpgrade,
			finalizationTargets,
			mgr.cr.Status.UpgradePhase,
		)
	}
	if strings.Contains(
		mgr.cr.Status.ImageUpgrade.Message,
		"transient finalization failure",
	) {
		t.Fatalf(
			"finalization status exposed endpoint error: %q",
			mgr.cr.Status.ImageUpgrade.Message,
		)
	}

	// Reconcile 4 retries the same logical operation and records success
	// without completing it in the endpoint reconciliation.
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating ||
		!reflect.DeepEqual(finalizationTargets, []int32{1, 1}) ||
		mgr.cr.Status.ImageUpgrade.FinalizationAttemptCount != 2 ||
		mgr.cr.Status.ImageUpgrade.FinalizationSucceededAt == nil ||
		mgr.cr.Status.ImageUpgrade.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseFinalizing ||
		mgr.cr.Status.UpgradePhase != enterpriseApi.UpgradePhaseUpgraded ||
		mgr.cr.Status.UpgradeEndTimestamp == 0 {
		t.Fatalf(
			"successful finalization phase=%q error=%v image=%#v targets=%v legacy=%q/%d",
			phase,
			err,
			mgr.cr.Status.ImageUpgrade,
			finalizationTargets,
			mgr.cr.Status.UpgradePhase,
			mgr.cr.Status.UpgradeEndTimestamp,
		)
	}

	// Reconcile 5 persists Completed; Reconcile 6 may finally report Ready.
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating ||
		mgr.cr.Status.ImageUpgrade.Phase !=
			enterpriseApi.SearchHeadClusterImageUpgradePhaseCompleted ||
		mgr.cr.Status.ImageUpgrade.CompletedAt == nil ||
		len(finalizationTargets) != 2 {
		t.Fatalf(
			"completion barrier phase=%q error=%v image=%#v targets=%v",
			phase,
			err,
			mgr.cr.Status.ImageUpgrade,
			finalizationTargets,
		)
	}
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseReady ||
		len(finalizationTargets) != 2 {
		t.Fatalf(
			"persisted completion phase=%q error=%v targets=%v",
			phase,
			err,
			finalizationTargets,
		)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("finalization changed StatefulSet: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestRollingUpdateControllerCompletesThreeMemberImageUpgradeAcrossRestarts(
	t *testing.T,
) {
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
	// Exercise classification as part of the complete scenario.
	mgr.cr.Status.ImageUpgrade = nil

	now := time.Date(2026, 7, 25, 20, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterImageUpgradeNow
	oldValidate := validateSearchHeadClusterImageUpgradePath
	oldInitiate := initiateSearchHeadClusterUpgrade
	oldFinalize := finalizeSearchHeadClusterUpgrade
	t.Cleanup(func() {
		searchHeadClusterImageUpgradeNow = oldNow
		validateSearchHeadClusterImageUpgradePath = oldValidate
		initiateSearchHeadClusterUpgrade = oldInitiate
		finalizeSearchHeadClusterUpgrade = oldFinalize
	})
	searchHeadClusterImageUpgradeNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	validateSearchHeadClusterImageUpgradePath = func(
		context.Context,
		string,
		string,
	) (upgrade.SHCImageUpgradePathDecision, error) {
		return upgrade.SHCImageUpgradePathSupported, nil
	}
	initializationCalls := 0
	initiateSearchHeadClusterUpgrade = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		initializationCalls++
		return nil
	}
	finalizationCalls := 0
	finalizeSearchHeadClusterUpgrade = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		finalizationCalls++
		return nil
	}

	ctx := context.Background()
	reconcile := func(wantPhase enterpriseApi.Phase, step string) {
		t.Helper()
		phase, err := mgr.updateRollingStatefulSetPods(
			ctx,
			statefulSet,
			3,
		)
		if err != nil || phase != wantPhase {
			t.Fatalf("%s phase=%q error=%v", step, phase, err)
		}
	}
	operationID := ""
	restart := func(step string) {
		t.Helper()
		if mgr.cr.Status.ImageUpgrade == nil {
			t.Fatalf("%s has no durable image-upgrade operation", step)
		}
		if operationID == "" {
			operationID = mgr.cr.Status.ImageUpgrade.OperationID
		} else if mgr.cr.Status.ImageUpgrade.OperationID != operationID {
			t.Fatalf(
				"%s operation ID = %q, want %q",
				step,
				mgr.cr.Status.ImageUpgrade.OperationID,
				operationID,
			)
		}
		mgr = restartRollingUpdateController(
			mgr,
			statefulSet,
			client,
		)
	}

	reconcile(enterpriseApi.PhaseUpdating, "classify image upgrade")
	if mgr.cr.Status.ImageUpgrade == nil ||
		mgr.cr.Status.ImageUpgrade.Phase != enterpriseApi.
			SearchHeadClusterImageUpgradePhasePendingInitialization {
		t.Fatalf("classification status = %#v", mgr.cr.Status.ImageUpgrade)
	}
	restart("after classification")

	reconcile(enterpriseApi.PhaseUpdating, "persist initialization intent")
	restart("after initialization intent")
	reconcile(enterpriseApi.PhaseUpdating, "call initialization endpoint")
	if initializationCalls != 1 ||
		mgr.cr.Status.ImageUpgrade.InitializationSucceededAt == nil {
		t.Fatalf(
			"initialization calls=%d status=%#v",
			initializationCalls,
			mgr.cr.Status.ImageUpgrade,
		)
	}
	restart("after initialization endpoint")
	reconcile(enterpriseApi.PhaseUpdating, "persist member-roll phase")
	restart("after member-roll phase")
	reconcile(enterpriseApi.PhaseUpdating, "prepare ordinal 2")

	observedPartitions := make([]int32, 0, 4)
	for target := int32(2); target >= 0; target-- {
		operation := mgr.cr.Status.LifecycleOperation
		if operation == nil ||
			operation.TargetOrdinal == nil ||
			*operation.TargetOrdinal != target {
			t.Fatalf(
				"ordinal %d lifecycle = %#v",
				target,
				operation,
			)
		}

		authorizedAt := metav1.NewTime(now.Add(time.Second))
		operation.Stage =
			enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement
		operation.ReplacementAuthorizedAt = &authorizedAt
		reconcile(
			enterpriseApi.PhaseUpdating,
			fmt.Sprintf("authorize ordinal %d", target),
		)
		statefulSet = getRollingUpdateFixtureStatefulSet(
			t,
			client,
			statefulSet,
		)
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
		setRollingUpdateFixturePodImage(
			t,
			client,
			statefulSet,
			target,
			"splunk/splunk:10.0.0",
		)
		mgr.cr.Status.LifecycleOperation.Stage =
			enterpriseApi.SearchHeadClusterLifecycleStageCompleted
		restart(fmt.Sprintf("before recording ordinal %d", target))
		reconcile(
			enterpriseApi.PhaseUpdating,
			fmt.Sprintf("record ordinal %d", target),
		)
		if !slices.Contains(
			mgr.cr.Status.ImageUpgrade.CompletedOrdinals,
			target,
		) {
			t.Fatalf(
				"ordinal %d not recorded: %v",
				target,
				mgr.cr.Status.ImageUpgrade.CompletedOrdinals,
			)
		}
		restart(fmt.Sprintf("after recording ordinal %d", target))
		if target > 0 {
			reconcile(
				enterpriseApi.PhaseUpdating,
				fmt.Sprintf("prepare ordinal %d", target-1),
			)
		}
	}

	reconcile(enterpriseApi.PhaseUpdating, "reset fail-closed partition")
	statefulSet = getRollingUpdateFixtureStatefulSet(
		t,
		client,
		statefulSet,
	)
	mgr.statefulSet = statefulSet
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	observedPartitions = append(observedPartitions, 3)
	statefulSet.Status.CurrentRevision = statefulSet.Status.UpdateRevision
	restart("after StatefulSet convergence")

	reconcile(enterpriseApi.PhaseUpdating, "persist finalization eligibility")
	restart("after finalization eligibility")
	reconcile(enterpriseApi.PhaseUpdating, "persist finalization intent")
	restart("after finalization intent")
	reconcile(enterpriseApi.PhaseUpdating, "call finalization endpoint")
	if finalizationCalls != 1 ||
		mgr.cr.Status.ImageUpgrade.FinalizationSucceededAt == nil {
		t.Fatalf(
			"finalization calls=%d status=%#v",
			finalizationCalls,
			mgr.cr.Status.ImageUpgrade,
		)
	}
	restart("after finalization endpoint")
	reconcile(enterpriseApi.PhaseUpdating, "persist workflow completion")
	restart("after workflow completion")
	reconcile(enterpriseApi.PhaseReady, "observe durable completion")

	if initializationCalls != 1 ||
		finalizationCalls != 1 ||
		!reflect.DeepEqual(
			mgr.cr.Status.ImageUpgrade.CompletedOrdinals,
			[]int32{0, 1, 2},
		) ||
		!reflect.DeepEqual(
			observedPartitions,
			[]int32{2, 1, 0, 3},
		) {
		t.Fatalf(
			"completed scenario init=%d finalize=%d ordinals=%v partitions=%v status=%#v",
			initializationCalls,
			finalizationCalls,
			mgr.cr.Status.ImageUpgrade.CompletedOrdinals,
			observedPartitions,
			mgr.cr.Status.ImageUpgrade,
		)
	}
	assertNoRollingUpdatePodDelete(t, client)
}

func TestOrdinaryRollingUpdateCompletionCallsNoUpgradeFinalization(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, _ := rollingUpdateControllerFixture(
		t,
		3,
		"revision-2",
		"revision-2",
		[]string{"revision-2", "revision-2", "revision-2"},
	)
	mgr.cr.Status.UpgradePhase = enterpriseApi.UpgradePhaseUpgrading
	oldFinalize := finalizeSearchHeadClusterUpgrade
	t.Cleanup(func() {
		finalizeSearchHeadClusterUpgrade = oldFinalize
	})
	finalizationCalls := 0
	finalizeSearchHeadClusterUpgrade = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		finalizationCalls++
		return nil
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseReady {
		t.Fatalf("ordinary completion phase=%q error=%v", phase, err)
	}
	if finalizationCalls != 0 ||
		mgr.cr.Status.UpgradePhase != enterpriseApi.UpgradePhaseUpgrading {
		t.Fatalf(
			"ordinary completion finalized upgrade calls=%d phase=%q",
			finalizationCalls,
			mgr.cr.Status.UpgradePhase,
		)
	}
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
		mgr.cr.Status.Members[ordinal].Name = ""
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
	mgr.cr.Status.ImageUpgrade =
		&enterpriseApi.SearchHeadClusterImageUpgradeStatus{
			OperationID:     "image-upgrade:search-head:revision-0",
			StatefulSetName: statefulSet.GetName(),
			DesiredRevision: "revision-0",
			SourceImage:     "splunk/splunk:9.3.0",
			TargetImage:     "splunk/splunk:9.4.0",
			TargetReplicas:  3,
			Phase: enterpriseApi.
				SearchHeadClusterImageUpgradePhaseCompleted,
		}

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

func TestRollingUpdateControllerReplacesHistoricalWorkflowForNewSupportedImage(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, _ := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	statefulSet.Spec.Template.Spec.Containers[0].Image =
		"splunk/splunk:10.0.0"
	mgr.cr.Status.ImageUpgrade =
		&enterpriseApi.SearchHeadClusterImageUpgradeStatus{
			OperationID:     "image-upgrade:search-head:revision-0",
			StatefulSetName: statefulSet.GetName(),
			DesiredRevision: "revision-0",
			SourceImage:     "splunk/splunk:9.3.0",
			TargetImage:     "splunk/splunk:9.4.0",
			TargetReplicas:  3,
			Phase: enterpriseApi.
				SearchHeadClusterImageUpgradePhaseCompleted,
		}

	oldValidate := validateSearchHeadClusterImageUpgradePath
	t.Cleanup(func() {
		validateSearchHeadClusterImageUpgradePath = oldValidate
	})
	validateSearchHeadClusterImageUpgradePath = func(
		context.Context,
		string,
		string,
	) (upgrade.SHCImageUpgradePathDecision, error) {
		return upgrade.SHCImageUpgradePathSupported, nil
	}

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)
	if err != nil || phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("new image after completed phase=%q error=%v", phase, err)
	}
	if mgr.cr.Status.ImageUpgrade.OperationID !=
		"image-upgrade:splunk-stack1-search-head:revision-2" ||
		mgr.cr.Status.ImageUpgrade.Phase != enterpriseApi.
			SearchHeadClusterImageUpgradePhasePendingInitialization ||
		mgr.cr.Status.ImageUpgrade.SourceImage != "splunk/splunk:9.4.0" ||
		mgr.cr.Status.ImageUpgrade.TargetImage != "splunk/splunk:10.0.0" {
		t.Fatalf("new image workflow = %#v", mgr.cr.Status.ImageUpgrade)
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

func TestRollingUpdateScaleUpRevisionSkewWaitsForAdditiveOrdinalThenRollsExistingMembers(
	t *testing.T,
) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1", "revision-2"},
	)
	stable := int32(3)
	mgr.cr.Status.LastStableReplicas = &stable
	statefulSet.Status.ReadyReplicas = 3
	setRollingUpdateFixturePodReady(
		t,
		client,
		statefulSet,
		3,
		false,
	)
	client.ResetCalls()

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		4,
	)
	if err != nil {
		t.Fatalf("wait for additive scale-up ordinal: %v", err)
	}
	if phase != enterpriseApi.PhaseScalingUp {
		t.Fatalf(
			"additive ordinal phase = %q, want %q",
			phase,
			enterpriseApi.PhaseScalingUp,
		)
	}
	if mgr.cr.Status.LifecycleOperation != nil {
		t.Fatalf(
			"unready additive ordinal started replacement lifecycle: %#v",
			mgr.cr.Status.LifecycleOperation,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	assertNoRollingUpdatePodDelete(t, client)

	statefulSet.Status.ReadyReplicas = 4
	setRollingUpdateFixturePodReady(
		t,
		client,
		statefulSet,
		3,
		true,
	)
	client.ResetCalls()
	phase, err = mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		4,
	)
	if err != nil {
		t.Fatalf("continue after additive scale-up ordinal recovered: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf(
			"post-scale rollout phase = %q, want %q",
			phase,
			enterpriseApi.PhaseUpdating,
		)
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != 2 ||
		operation.DesiredRevision != "revision-2" {
		t.Fatalf(
			"post-scale lifecycle = %#v, want ordinal 2 at revision-2",
			operation,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	assertNoRollingUpdatePodDelete(t, client)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf(
			"post-scale preparation changed Kubernetes state: %v",
			client.Calls["Update"],
		)
	}
}

func TestRollingUpdateControllerHoldsAuthorizedTargetDuringCaptainNodeLoss(
	t *testing.T,
) {
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
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:     "pod-update-2",
			Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision: "revision-2",
			TargetPod:       statefulSet.GetName() + "-2",
			TargetOrdinal:   &target,
			Stage: enterpriseApi.
				SearchHeadClusterLifecycleStageAuthorizingReplacement,
			ReplacementAuthorizedAt: &authorizedAt,
		}

	captainPod := &corev1.Pod{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      statefulSet.GetName() + "-0",
	}, captainPod); err != nil {
		t.Fatalf("get captain Pod: %v", err)
	}
	if err := client.Delete(context.Background(), captainPod); err != nil {
		t.Fatalf("simulate captain node loss: %v", err)
	}
	mgr.cr.Status.Captain = ""
	mgr.cr.Status.CaptainReady = false
	client.ResetCalls()

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)

	if err != nil {
		t.Fatalf("captain node-loss wait: %v", err)
	}
	if phase != enterpriseApi.PhasePending {
		t.Fatalf(
			"captain node-loss phase = %q, want %q",
			phase,
			enterpriseApi.PhasePending,
		)
	}
	if !strings.Contains(
		mgr.cr.Status.Message,
		string(upgrade.SHCRolloutReasonCaptainUnavailable),
	) {
		t.Fatalf(
			"captain node-loss status = %q, want %s",
			mgr.cr.Status.Message,
			upgrade.SHCRolloutReasonCaptainUnavailable,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("captain node loss changed Kubernetes state: %v", client.Calls["Update"])
	}
	assertNoRollingUpdatePodDelete(t, client)
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != target ||
		operation.ReplacementAuthorizedAt == nil {
		t.Fatalf(
			"captain node loss replaced durable operation: %#v",
			operation,
		)
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
	if len(recorder.events) != 0 {
		t.Fatalf(
			"observing a durable lifecycle failure emitted %d duplicate events",
			len(recorder.events),
		)
	}
	if !strings.Contains(
		mgr.cr.Status.Message,
		string(
			enterpriseApi.
				SearchHeadClusterLifecycleReasonSplunkStartupFailed,
		),
	) {
		t.Fatalf(
			"blocked status = %q, want durable lifecycle reason",
			mgr.cr.Status.Message,
		)
	}

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

func TestRollingUpdateControllerBlocksKubernetesReadyUnplannedMemberRecovery(
	t *testing.T,
) {
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
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:     "pod-update-2",
			Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision: "revision-2",
			TargetPod:       statefulSet.GetName() + "-2",
			TargetOrdinal:   &target,
			Stage: enterpriseApi.
				SearchHeadClusterLifecycleStageAuthorizingReplacement,
			ReplacementAuthorizedAt: &authorizedAt,
		}
	mgr.cr.Status.Members[1].Registered = false
	mgr.cr.Status.Members[1].Status = ""
	client.ResetCalls()

	phase, err := mgr.updateRollingStatefulSetPods(
		context.Background(),
		statefulSet,
		3,
	)

	if err == nil ||
		!strings.Contains(
			err.Error(),
			string(upgrade.SHCRolloutReasonMemberRecoveryPending),
		) {
		t.Fatalf("member-recovery error = %v", err)
	}
	if phase != enterpriseApi.PhaseError {
		t.Fatalf(
			"member-recovery phase = %q, want %q",
			phase,
			enterpriseApi.PhaseError,
		)
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 3)
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf(
			"Kubernetes-ready member recovery advanced partition: %v",
			client.Calls["Update"],
		)
	}
	assertNoRollingUpdatePodDelete(t, client)
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

func TestLifecycleRecoveryDoesNotRequirePartitionForScaleDownCancellation(
	t *testing.T,
) {
	target := int32(3)
	partition := int32(4)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
		TargetOrdinal: &target,
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageValidatingRecovery,
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

	if !lifecycleRecoveryActiveForStatefulSet(statefulSet, operation) {
		t.Fatal("cancelled scale down incorrectly waited for partition advancement")
	}
}

func TestLifecycleRecoveryDoesNotRequirePartitionForPodUpdateCancellation(
	t *testing.T,
) {
	target := int32(2)
	partition := int32(3)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		TargetOrdinal: &target,
		TargetPodUID:  "original-pod-uid",
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageValidatingRecovery,
		Reason: enterpriseApi.
			SearchHeadClusterLifecycleReasonPodUpdateCancelled,
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

	if !lifecycleRecoveryActiveForStatefulSet(statefulSet, operation) {
		t.Fatal("cancelled Pod update incorrectly waited for partition advancement")
	}
}

func TestRollingUpdateObservationDoesNotRepeatDurableLifecycleBlockedEvent(
	t *testing.T,
) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, _ := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	target := int32(2)
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:     "PodUpdate:example-search-head-2:revision-2:2",
			Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision: "revision-2",
			TargetPod:       statefulSet.GetName() + "-2",
			TargetOrdinal:   &target,
			TargetPodUID:    "original-pod-uid",
			Stage: enterpriseApi.
				SearchHeadClusterLifecycleStageBlocked,
			Reason: enterpriseApi.
				SearchHeadClusterLifecycleReasonSearchDrainTimedOut,
			Message: "search drain timed out: historical=0 realtime=1",
		}
	recorder := &mockEventRecorder{}
	ctx := context.WithValue(
		context.Background(),
		splcommon.EventPublisherKey,
		&K8EventPublisher{recorder: recorder, instance: mgr.cr},
	)
	state, err := mgr.observeRollingStatefulSet(ctx, statefulSet)
	if err != nil {
		t.Fatalf("observe StatefulSet: %v", err)
	}
	decision := upgrade.EvaluateSHCRollout(state)
	if decision.Action != upgrade.SHCRolloutActionBlock {
		t.Fatalf(
			"action = %q reason = %q, want Block",
			decision.Action,
			decision.Reason,
		)
	}

	mgr.recordRollingUpdateDecision(ctx, state, decision)
	if len(recorder.events) != 0 {
		t.Fatalf(
			"durable lifecycle block observation emitted %d duplicate events",
			len(recorder.events),
		)
	}
	if !strings.Contains(
		mgr.cr.Status.Message,
		string(
			enterpriseApi.
				SearchHeadClusterLifecycleReasonSearchDrainTimedOut,
		),
	) {
		t.Fatalf(
			"status message = %q, want durable lifecycle reason",
			mgr.cr.Status.Message,
		)
	}
}

func TestSearchDrainContinuationApprovalIsAppliedAndEmittedOnce(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, _ := rollingUpdateControllerFixture(
		t,
		3,
		"revision-1",
		"revision-2",
		[]string{"revision-1", "revision-1", "revision-1"},
	)
	mgr.cr.Generation = 7
	target := int32(2)
	token := strings.Repeat("a", 64)
	targetPod := statefulSet.GetName() + "-2"
	mgr.cr.Spec.LifecycleApproval =
		&enterpriseApi.SearchHeadClusterLifecycleApproval{
			OperationID: "PodUpdate:example-search-head-2:revision-2:2",
			Token:       token,
			Action: enterpriseApi.
				SearchHeadClusterLifecycleApprovalActionContinueAfterSearchDrainTimeout,
		}
	mgr.cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:     mgr.cr.Spec.LifecycleApproval.OperationID,
			Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
			DesiredRevision: "revision-2",
			TargetPod:       targetPod,
			TargetOrdinal:   &target,
			TargetPodUID:    "original-pod-uid",
			Stage: enterpriseApi.
				SearchHeadClusterLifecycleStageBlocked,
			Reason: enterpriseApi.
				SearchHeadClusterLifecycleReasonSearchDrainTimedOut,
			SearchDrainContinuationToken: token,
		}
	mgr.cr.Status.Members = nil

	recorder := &mockEventRecorder{}
	publisher := &K8EventPublisher{recorder: recorder, instance: mgr.cr}
	metricBefore := testutil.ToFloat64(
		splmetrics.SHCSearchDrainContinuationApprovalCounter,
	)
	if mgr.reconcileSearchDrainContinuationApproval(
		context.Background(),
		publisher,
	) {
		t.Fatal("approval was applied without a refreshed target-member observation")
	}
	if got := testutil.ToFloat64(
		splmetrics.SHCSearchDrainContinuationApprovalCounter,
	); got != metricBefore {
		t.Fatalf("missing target-member observation changed approval metric to %v", got)
	}
	if len(recorder.events) != 0 {
		t.Fatalf(
			"missing target-member observation emitted %d events",
			len(recorder.events),
		)
	}

	mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{{
		Name:                        targetPod,
		Status:                      "ManualDetention",
		ActiveHistoricalSearchCount: 2,
		ActiveRealtimeSearchCount:   1,
	}}

	if !mgr.reconcileSearchDrainContinuationApproval(
		context.Background(),
		publisher,
	) {
		t.Fatal("matching continuation approval was not applied")
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches ||
		operation.Reason !=
			enterpriseApi.
				SearchHeadClusterLifecycleReasonSearchDrainContinuationApproved ||
		operation.SearchDrainContinuationApprovalGeneration != 7 ||
		operation.ApprovedActiveHistoricalSearches != 2 ||
		operation.ApprovedActiveRealtimeSearches != 1 {
		t.Fatalf("approved operation = %#v", operation)
	}
	if got := testutil.ToFloat64(
		splmetrics.SHCSearchDrainContinuationApprovalCounter,
	); got != metricBefore+1 {
		t.Fatalf("approval metric = %v, want %v", got, metricBefore+1)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("approval emitted %d events, want one", len(recorder.events))
	}
	assertRolloutEvent(
		t,
		recorder,
		EventReasonSHCSearchDrainContinuationApproved,
		corev1.EventTypeNormal,
	)

	if mgr.reconcileSearchDrainContinuationApproval(
		context.Background(),
		publisher,
	) {
		t.Fatal("persisted approval was applied more than once")
	}
	if got := testutil.ToFloat64(
		splmetrics.SHCSearchDrainContinuationApprovalCounter,
	); got != metricBefore+1 {
		t.Fatalf("duplicate reconcile changed approval metric to %v", got)
	}
	if len(recorder.events) != 1 {
		t.Fatalf("duplicate reconcile emitted %d events", len(recorder.events))
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

	eventsBefore := len(recorder.events)
	phase, err = mgr.updateRollingStatefulSetPods(ctx, statefulSet, 3)
	if err == nil || phase != enterpriseApi.PhaseError {
		t.Fatalf(
			"repeated blocked decision phase=%q error=%v, want PhaseError",
			phase,
			err,
		)
	}
	if len(recorder.events) != eventsBefore {
		t.Fatalf(
			"repeated blocked decision emitted %d additional events",
			len(recorder.events)-eventsBefore,
		)
	}
	if got := testutil.ToFloat64(decisionMetric); got != metricBefore+1 {
		t.Fatalf(
			"repeated blocked observation changed transition metric to %f, want %f",
			got,
			metricBefore+1,
		)
	}
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

func TestRollingUpdateControllerContinuesRollbackOfSupersededOrdinal(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	mgr, statefulSet, client := rollingUpdateControllerFixture(
		t,
		2,
		"revision-1",
		"revision-1",
		[]string{"revision-1", "revision-2", "revision-1"},
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
		t.Fatalf("continue rollback: %v", err)
	}
	if phase != enterpriseApi.PhaseUpdating {
		t.Fatalf("rollback phase = %q, want %q", phase, enterpriseApi.PhaseUpdating)
	}
	operation := mgr.cr.Status.LifecycleOperation
	if operation == nil ||
		operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != 1 ||
		operation.DesiredRevision != "revision-1" ||
		operation.ReplacementAuthorizedAt != nil {
		t.Fatalf(
			"continued rollback operation = %#v, want unapproved ordinal 1",
			operation,
		)
	}
	if len(client.Calls["Update"]) != 0 {
		t.Fatalf("continued rollback changed partition: %v", client.Calls["Update"])
	}
	assertRollingUpdatePartition(t, statefulSet.Spec.UpdateStrategy, 2)
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
	oldGetKVStoreStatus := getSearchHeadKVStoreStatus
	t.Cleanup(func() {
		getSearchHeadKVStoreStatus = oldGetKVStoreStatus
	})
	getSearchHeadKVStoreStatus = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (string, error) {
		return "ready", nil
	}
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
			Captain:        "splunk-stack1-search-head-0",
			CaptainReady:   true,
			Members: make([]enterpriseApi.SearchHeadClusterMemberStatus,
				replicas),
		},
	}
	for ordinal := range cr.Status.Members {
		cr.Status.Members[ordinal] =
			enterpriseApi.SearchHeadClusterMemberStatus{
				Name: fmt.Sprintf(
					"splunk-stack1-search-head-%d",
					ordinal,
				),
				Status:     "Up",
				Registered: true,
			}
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
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "splunk", Image: "splunk/splunk:9.4.0"},
					},
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
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "splunk", Image: "splunk/splunk:9.4.0"},
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

func restartRollingUpdateController(
	mgr *searchHeadClusterPodManager,
	statefulSet *appsv1.StatefulSet,
	client *spltest.MockClient,
) *searchHeadClusterPodManager {
	return &searchHeadClusterPodManager{
		c:           client,
		cr:          mgr.cr.DeepCopy(),
		statefulSet: statefulSet,
	}
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

func setRollingUpdateFixturePodImage(
	t *testing.T,
	client *spltest.MockClient,
	statefulSet *appsv1.StatefulSet,
	ordinal int32,
	image string,
) {
	t.Helper()
	pod := &corev1.Pod{}
	if err := client.Get(context.Background(), types.NamespacedName{
		Namespace: statefulSet.GetNamespace(),
		Name:      fmt.Sprintf("%s-%d", statefulSet.GetName(), ordinal),
	}, pod); err != nil {
		t.Fatalf("get Pod %d: %v", ordinal, err)
	}
	for index := range pod.Spec.Containers {
		if pod.Spec.Containers[index].Name == "splunk" {
			pod.Spec.Containers[index].Image = image
		}
	}
	if err := client.Update(context.Background(), pod); err != nil {
		t.Fatalf("update Pod %d image: %v", ordinal, err)
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
