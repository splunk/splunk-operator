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
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestLifecycleAdapterPersistsStagesBeforeActions(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterLifecycleNow
	oldGetMembers := getSearchHeadCaptainMembers
	oldRequestDetention := requestSearchHeadDetention
	oldTransferCaptain := transferSearchHeadCaptain
	oldGetLifecyclePod := getSearchHeadLifecyclePod
	t.Cleanup(func() {
		searchHeadClusterLifecycleNow = oldNow
		getSearchHeadCaptainMembers = oldGetMembers
		requestSearchHeadDetention = oldRequestDetention
		transferSearchHeadCaptain = oldTransferCaptain
		getSearchHeadLifecyclePod = oldGetLifecyclePod
	})
	searchHeadClusterLifecycleNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}

	cr := &enterpriseApi.SearchHeadCluster{}
	cr.Name = "example"
	cr.Status.Initialized = true
	cr.Status.MinPeersJoined = true
	cr.Status.CaptainReady = true
	cr.Status.Captain = "splunk-example-search-head-2"
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{
			Name:       "splunk-example-search-head-0",
			Status:     "Up",
			Registered: true,
		},
		{
			Name:       "splunk-example-search-head-1",
			Status:     "Up",
			Registered: true,
		},
		{
			Name:       "splunk-example-search-head-2",
			Status:     "Up",
			Registered: true,
		},
	}
	mgr := &searchHeadClusterPodManager{
		cr: cr,
		statefulSet: &appsv1.StatefulSet{
			Status: appsv1.StatefulSetStatus{UpdateRevision: "revision-2"},
		},
	}

	captainMembers := map[string]splclient.SearchHeadCaptainMemberInfo{
		"splunk-example-search-head-0": {
			Identifier:    "member-guid-0",
			Label:         "splunk-example-search-head-0",
			Status:        "Up",
			ManagementURI: "https://splunk-example-search-head-0:8089",
		},
		"splunk-example-search-head-1": {
			Identifier:       "member-guid-1",
			Label:            "splunk-example-search-head-1",
			Status:           "Up",
			ManagementURI:    "https://splunk-example-search-head-1:8089",
			PreferredCaptain: true,
		},
		"splunk-example-search-head-2": {
			Identifier:    "member-guid-2",
			Label:         "splunk-example-search-head-2",
			Status:        "Up",
			Captain:       true,
			ManagementURI: "https://splunk-example-search-head-2:8089",
		},
	}
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return captainMembers, nil
	}

	detentionCalls := 0
	requestSearchHeadDetention = func(context.Context, *searchHeadClusterPodManager, int32) error {
		detentionCalls++
		return nil
	}
	transferCalls := 0
	transferTarget := ""
	transferSearchHeadCaptain = func(
		_ context.Context,
		_ *searchHeadClusterPodManager,
		_ int32,
		managementURI string,
	) error {
		transferCalls++
		transferTarget = managementURI
		return nil
	}
	getSearchHeadLifecyclePod = func(
		context.Context,
		*searchHeadClusterPodManager,
		string,
	) (*corev1.Pod, error) {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID: types.UID("original-pod-uid"),
			},
		}, nil
	}

	// Reconcile 1 persists operation identity; no action is allowed.
	ready, err := mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if detentionCalls != 0 || transferCalls != 0 {
		t.Fatal("adapter executed an action before operation identity was persisted")
	}

	// Reconcile 2 persists DetainingTarget; detention is still not called.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget {
		t.Fatalf("stage = %q, want DetainingTarget", cr.Status.LifecycleOperation.Stage)
	}
	if detentionCalls != 0 {
		t.Fatal("detention executed in the same reconcile as its stage transition")
	}

	// Reconcile 3 observes the persisted stage and may request detention.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if detentionCalls != 1 {
		t.Fatalf("detention calls = %d, want 1", detentionCalls)
	}

	// Once detained and drained, the next reconcile persists
	// TransferringCaptain but does not yet call the transfer endpoint.
	cr.Status.Members[2].Status = "ManualDetention"
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageTransferringCaptain {
		t.Fatalf("stage = %q, want TransferringCaptain", cr.Status.LifecycleOperation.Stage)
	}
	if transferCalls != 0 {
		t.Fatal("captain transfer executed in the same reconcile as its stage transition")
	}

	// The persisted transfer stage authorizes one transfer request.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if transferCalls != 1 {
		t.Fatalf("transfer calls = %d, want 1", transferCalls)
	}
	if transferTarget != "https://splunk-example-search-head-1:8089" {
		t.Fatalf("transfer target = %q, want preferred captain candidate", transferTarget)
	}
	if cr.Status.LifecycleOperation.CaptainTransferRequestedAt == nil {
		t.Fatal("successful captain transfer submission was not recorded")
	}

	// Restart/resume with the submitted operation only observes; it does not
	// submit the non-idempotent transfer request again.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if transferCalls != 1 {
		t.Fatalf("transfer calls after resume = %d, want 1", transferCalls)
	}

	// A fresh observation of a different ready captain persists replacement
	// authorization, but cannot authorize deletion in the same reconcile.
	cr.Status.Captain = "splunk-example-search-head-0"
	captainMembers["splunk-example-search-head-2"] = splclient.SearchHeadCaptainMemberInfo{
		Identifier:    "member-guid-2",
		Label:         "splunk-example-search-head-2",
		Status:        "ManualDetention",
		ManagementURI: "https://splunk-example-search-head-2:8089",
	}
	captainMembers["splunk-example-search-head-0"] = splclient.SearchHeadCaptainMemberInfo{
		Identifier:    "member-guid-0",
		Label:         "splunk-example-search-head-0",
		Status:        "Up",
		Captain:       true,
		ManagementURI: "https://splunk-example-search-head-0:8089",
	}
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement {
		t.Fatalf("stage = %q, want AuthorizingReplacement", cr.Status.LifecycleOperation.Stage)
	}

	// Only a later reconcile observing the durable authorization returns true
	// to the existing Pod manager.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, true)
}

func TestLifecycleObservationRejectsCaptainDisagreement(t *testing.T) {
	cr := &enterpriseApi.SearchHeadCluster{}
	cr.Name = "example"
	cr.Status.Initialized = true
	cr.Status.MinPeersJoined = true
	cr.Status.CaptainReady = true
	cr.Status.Captain = "splunk-example-search-head-0"
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-example-search-head-0", Status: "Up", Registered: true},
		{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldGetMembers := getSearchHeadCaptainMembers
	t.Cleanup(func() { getSearchHeadCaptainMembers = oldGetMembers })
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			"splunk-example-search-head-0": {
				Label:  "splunk-example-search-head-0",
				Status: "Up",
			},
			"splunk-example-search-head-1": {
				Label:   "splunk-example-search-head-1",
				Status:  "Up",
				Captain: true,
			},
		}, nil
	}

	observation := mgr.observeLifecycleReplacement(context.Background(), 1, time.Now())
	if !observation.Available || !observation.Fresh {
		t.Fatal("expected a fresh observation")
	}
	if !observation.ConflictingCaptain {
		t.Fatal("expected disagreement between captain info and captain member view")
	}
}

func TestLifecycleAdapterObservesUnschedulableReplacementPod(t *testing.T) {
	target := int32(2)
	targetPod := "splunk-example-search-head-2"
	cr := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				TargetPod:     targetPod,
				TargetOrdinal: &target,
			},
		},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldGetLifecyclePod := getSearchHeadLifecyclePod
	t.Cleanup(func() { getSearchHeadLifecyclePod = oldGetLifecyclePod })
	getSearchHeadLifecyclePod = func(
		context.Context,
		*searchHeadClusterPodManager,
		string,
	) (*corev1.Pod, error) {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:    types.UID("new-pod-uid"),
				Labels: map[string]string{"controller-revision-hash": "revision-2"},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodScheduled,
						Status: corev1.ConditionFalse,
						Reason: corev1.PodReasonUnschedulable,
					},
				},
			},
		}, nil
	}

	observation, err := mgr.observeLifecycleRecovery(
		context.Background(),
		target,
	)
	if err != nil {
		t.Fatalf("observe unschedulable replacement: %v", err)
	}
	if !observation.PodExists ||
		observation.PodUID != "new-pod-uid" ||
		observation.PodRevision != "revision-2" {
		t.Fatalf("replacement identity observation = %#v", observation)
	}
	if observation.PodScheduled || !observation.PodUnschedulable {
		t.Fatalf(
			"scheduling observation = scheduled %t, unschedulable %t",
			observation.PodScheduled,
			observation.PodUnschedulable,
		)
	}
	if observation.MemberObserved ||
		observation.CaptainMemberObserved ||
		observation.MemberRegistered {
		t.Fatalf("unscheduled Pod produced Splunk observations: %#v", observation)
	}
}

func TestLifecycleAdapterObservesReplacementWaitingForStorage(t *testing.T) {
	target := int32(2)
	targetPod := "splunk-example-search-head-2"
	cr := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				TargetPod:     targetPod,
				TargetOrdinal: &target,
			},
		},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldGetLifecyclePod := getSearchHeadLifecyclePod
	t.Cleanup(func() { getSearchHeadLifecyclePod = oldGetLifecyclePod })
	getSearchHeadLifecyclePod = func(
		context.Context,
		*searchHeadClusterPodManager,
		string,
	) (*corev1.Pod, error) {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID:    types.UID("new-pod-uid"),
				Labels: map[string]string{"controller-revision-hash": "revision-2"},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodScheduled,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
					},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "splunk",
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason:  "ContainerCreating",
								Message: "MountVolume.SetUp failed for volume \"etc\"",
							},
						},
					},
				},
			},
		}, nil
	}

	observation, err := mgr.observeLifecycleRecovery(
		context.Background(),
		target,
	)
	if err != nil {
		t.Fatalf("observe storage-pending replacement: %v", err)
	}
	if !observation.PodExists ||
		!observation.PodScheduled ||
		observation.PodReady {
		t.Fatalf("replacement Pod observation = %#v", observation)
	}
	if !observation.StoragePending {
		t.Fatalf("storage wait was not classified: %#v", observation)
	}
	if observation.ImagePullFailed ||
		observation.ContainerStartupFailed ||
		observation.MemberObserved {
		t.Fatalf("storage wait was misclassified: %#v", observation)
	}
}

func TestLifecycleAdapterObservesTerminalImagePullFailure(t *testing.T) {
	for _, reason := range []string{
		"ErrImagePull",
		"ImagePullBackOff",
		"InvalidImageName",
		"ErrInvalidImage",
	} {
		t.Run(reason, func(t *testing.T) {
			observation := observeWaitingLifecyclePod(t, reason)
			if !observation.PodExists ||
				!observation.PodScheduled ||
				!observation.ImagePullFailed {
				t.Fatalf("image-pull observation = %#v", observation)
			}
			if observation.StoragePending ||
				observation.ContainerStartupFailed ||
				observation.MemberObserved {
				t.Fatalf("image-pull failure was misclassified: %#v", observation)
			}
		})
	}
}

func TestLifecycleAdapterClassifiesContainerStartupFailures(t *testing.T) {
	tests := []struct {
		reason   string
		terminal bool
	}{
		{reason: "CrashLoopBackOff"},
		{reason: "CreateContainerConfigError", terminal: true},
		{reason: "CreateContainerError", terminal: true},
		{reason: "RunContainerError", terminal: true},
	}
	for _, test := range tests {
		t.Run(test.reason, func(t *testing.T) {
			observation := observeWaitingLifecyclePod(t, test.reason)
			if !observation.PodExists ||
				!observation.PodScheduled ||
				!observation.ContainerStartupFailed {
				t.Fatalf("container startup observation = %#v", observation)
			}
			if observation.ContainerFailureTerminal != test.terminal {
				t.Fatalf(
					"terminal = %t, want %t: %#v",
					observation.ContainerFailureTerminal,
					test.terminal,
					observation,
				)
			}
			if observation.StoragePending ||
				observation.ImagePullFailed ||
				observation.MemberObserved {
				t.Fatalf("container startup failure was misclassified: %#v", observation)
			}
		})
	}
}

func TestLifecycleAdapterTreatsOrdinalZeroAsNonCaptainWhenObservedElsewhere(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	stageStartedAt := metav1.NewTime(now)
	target := int32(0)
	targetPod := "splunk-example-search-head-0"
	captainPod := "splunk-example-search-head-1"
	drainingStage :=
		enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "example"},
		Status: enterpriseApi.SearchHeadClusterStatus{
			Initialized:    true,
			MinPeersJoined: true,
			Captain:        captainPod,
			CaptainReady:   true,
			Members: []enterpriseApi.SearchHeadClusterMemberStatus{
				{
					Name:       targetPod,
					Status:     "ManualDetention",
					Registered: true,
				},
				{
					Name:       captainPod,
					Status:     "Up",
					Registered: true,
				},
				{
					Name:       "splunk-example-search-head-2",
					Status:     "Up",
					Registered: true,
				},
			},
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				OperationID:        "pod-update-0",
				Intent:             enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				DesiredRevision:    "revision-2",
				TargetPod:          targetPod,
				TargetOrdinal:      &target,
				Stage:              drainingStage,
				StartedAt:          &stageStartedAt,
				StageStartedAt:     &stageStartedAt,
				LastTransitionTime: &stageStartedAt,
			},
		},
	}
	mgr := &searchHeadClusterPodManager{
		cr: cr,
		statefulSet: &appsv1.StatefulSet{
			Status: appsv1.StatefulSetStatus{UpdateRevision: "revision-2"},
		},
	}

	oldNow := searchHeadClusterLifecycleNow
	oldGetMembers := getSearchHeadCaptainMembers
	oldTransferCaptain := transferSearchHeadCaptain
	oldGetLifecyclePod := getSearchHeadLifecyclePod
	t.Cleanup(func() {
		searchHeadClusterLifecycleNow = oldNow
		getSearchHeadCaptainMembers = oldGetMembers
		transferSearchHeadCaptain = oldTransferCaptain
		getSearchHeadLifecyclePod = oldGetLifecyclePod
	})
	searchHeadClusterLifecycleNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			targetPod: {
				Identifier: "member-guid-0",
				Label:      targetPod,
				Status:     "ManualDetention",
			},
			captainPod: {
				Identifier: "member-guid-1",
				Label:      captainPod,
				Status:     "Up",
				Captain:    true,
			},
			"splunk-example-search-head-2": {
				Identifier: "member-guid-2",
				Label:      "splunk-example-search-head-2",
				Status:     "Up",
			},
		}, nil
	}
	transferCalls := 0
	transferSearchHeadCaptain = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
		string,
	) error {
		transferCalls++
		return nil
	}
	getSearchHeadLifecyclePod = func(
		context.Context,
		*searchHeadClusterPodManager,
		string,
	) (*corev1.Pod, error) {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: targetPod,
				UID:  types.UID("ordinal-zero-pod-uid"),
			},
		}, nil
	}

	// Persist authorization as a separate stage before returning permission
	// to replace ordinal zero.
	ready, err := mgr.prepareLifecycleReplacement(
		context.Background(),
		target,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	operation := cr.Status.LifecycleOperation
	if operation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement {
		t.Fatalf("stage = %q, want AuthorizingReplacement", operation.Stage)
	}
	if operation.Captain != captainPod {
		t.Fatalf("observed captain = %q, want %q", operation.Captain, captainPod)
	}
	if transferCalls != 0 {
		t.Fatalf("captain transfer calls = %d, want zero", transferCalls)
	}

	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		target,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, true)
	if transferCalls != 0 {
		t.Fatalf("captain transfer calls after authorization = %d, want zero", transferCalls)
	}
	operation = cr.Status.LifecycleOperation
	if operation.TargetOrdinal == nil ||
		*operation.TargetOrdinal != target ||
		operation.TargetPodUID != "ordinal-zero-pod-uid" ||
		operation.ReplacementAuthorizedAt == nil {
		t.Fatalf(
			"ordinal-zero authorization = %#v, want authorized target with captured UID",
			operation,
		)
	}
}

func TestLifecycleRecoveryAdapterReleasesDetentionAndCompletes(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	now := time.Date(2026, 7, 24, 14, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterLifecycleNow
	oldGetMembers := getSearchHeadCaptainMembers
	oldGetLifecyclePod := getSearchHeadLifecyclePod
	oldReleaseDetention := releaseSearchHeadDetention
	t.Cleanup(func() {
		searchHeadClusterLifecycleNow = oldNow
		getSearchHeadCaptainMembers = oldGetMembers
		getSearchHeadLifecyclePod = oldGetLifecyclePod
		releaseSearchHeadDetention = oldReleaseDetention
	})
	searchHeadClusterLifecycleNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}

	ordinal := int32(2)
	authorizedAt := metav1.NewTime(now)
	cr := &enterpriseApi.SearchHeadCluster{}
	cr.Name = "example"
	cr.Status.Captain = "splunk-example-search-head-0"
	cr.Status.CaptainReady = true
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-example-search-head-0", Status: "Up", Registered: true},
		{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
		{Name: "splunk-example-search-head-2", Status: "ManualDetention", Registered: true},
	}
	cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:             "operation-1",
		Intent:                  enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:         "revision-2",
		TargetPod:               "splunk-example-search-head-2",
		TargetOrdinal:           &ordinal,
		Stage:                   enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		TargetPodUID:            "old-pod-uid",
		TargetMemberID:          "member-guid-2",
		ReplacementAuthorizedAt: &authorizedAt,
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	getSearchHeadLifecyclePod = func(
		context.Context,
		*searchHeadClusterPodManager,
		string,
	) (*corev1.Pod, error) {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "splunk-example-search-head-2",
				UID:  types.UID("new-pod-uid"),
				Labels: map[string]string{
					"controller-revision-hash": "revision-2",
				},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}, nil
	}
	captainMembers := map[string]splclient.SearchHeadCaptainMemberInfo{
		"splunk-example-search-head-0": {
			Identifier: "member-guid-0",
			Label:      "splunk-example-search-head-0",
			Status:     "Up",
			Captain:    true,
		},
		"splunk-example-search-head-2": {
			Identifier: "member-guid-2",
			Label:      "splunk-example-search-head-2",
			Status:     "ManualDetention",
		},
	}
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return captainMembers, nil
	}
	releaseCalls := 0
	releaseSearchHeadDetention = func(context.Context, *searchHeadClusterPodManager, int32) error {
		releaseCalls++
		return nil
	}

	// Persist ValidatingRecovery before releasing detention.
	complete, err := mgr.resumeLifecycleRecovery(context.Background(), ordinal)
	assertLifecycleAdapterResult(t, complete, err, false)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery {
		t.Fatalf("stage = %q, want ValidatingRecovery", cr.Status.LifecycleOperation.Stage)
	}
	if releaseCalls != 0 {
		t.Fatal("detention release ran in the same reconcile as its stage transition")
	}

	complete, err = mgr.resumeLifecycleRecovery(context.Background(), ordinal)
	assertLifecycleAdapterResult(t, complete, err, false)
	if releaseCalls != 1 || cr.Status.LifecycleOperation.DetentionReleaseRequestedAt == nil {
		t.Fatalf("release calls = %d, requestedAt = %v; want one durable request",
			releaseCalls,
			cr.Status.LifecycleOperation.DetentionReleaseRequestedAt,
		)
	}

	// Restart/resume observes the durable request without calling it again.
	complete, err = mgr.resumeLifecycleRecovery(context.Background(), ordinal)
	assertLifecycleAdapterResult(t, complete, err, false)
	if releaseCalls != 1 {
		t.Fatalf("release calls after resume = %d, want 1", releaseCalls)
	}

	cr.Status.Members[2].Status = "Up"
	captainMembers["splunk-example-search-head-2"] = splclient.SearchHeadCaptainMemberInfo{
		Identifier: "member-guid-2",
		Label:      "splunk-example-search-head-2",
		Status:     "Up",
	}
	complete, err = mgr.resumeLifecycleRecovery(context.Background(), ordinal)
	assertLifecycleAdapterResult(t, complete, err, true)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageCompleted {
		t.Fatalf("stage = %q, want Completed", cr.Status.LifecycleOperation.Stage)
	}
}

func TestLifecycleFinishRecycleDoesNotBlockCompletedHigherOrdinal(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	targetOrdinal := int32(1)
	mgr := &searchHeadClusterPodManager{
		cr: &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
					TargetOrdinal: &targetOrdinal,
					Stage:         enterpriseApi.SearchHeadClusterLifecycleStageValidatingRecovery,
				},
			},
		},
	}

	complete, err := mgr.FinishRecycle(context.Background(), 2)
	assertLifecycleAdapterResult(t, complete, err, true)

	complete, err = mgr.FinishRecycle(context.Background(), targetOrdinal)
	assertLifecycleAdapterResult(t, complete, err, false)

	mgr.cr.Status.LifecycleOperation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	complete, err = mgr.FinishRecycle(context.Background(), targetOrdinal)
	assertLifecycleAdapterResult(t, complete, err, true)
}

func assertLifecycleAdapterResult(t *testing.T, got bool, err error, want bool) {
	t.Helper()
	if err != nil {
		t.Fatalf("prepare lifecycle replacement: %v", err)
	}
	if got != want {
		t.Fatalf("ready = %t, want %t", got, want)
	}
}

func observeWaitingLifecyclePod(
	t *testing.T,
	reason string,
) shcworkflow.RecoveryObservation {
	t.Helper()
	target := int32(2)
	targetPod := "splunk-example-search-head-2"
	cr := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				TargetPod:     targetPod,
				TargetOrdinal: &target,
			},
		},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldGetLifecyclePod := getSearchHeadLifecyclePod
	t.Cleanup(func() { getSearchHeadLifecyclePod = oldGetLifecyclePod })
	getSearchHeadLifecyclePod = func(
		context.Context,
		*searchHeadClusterPodManager,
		string,
	) (*corev1.Pod, error) {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				UID: types.UID("new-pod-uid"),
				Labels: map[string]string{
					"controller-revision-hash": "revision-2",
				},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodScheduled,
						Status: corev1.ConditionTrue,
					},
				},
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "splunk",
						State: corev1.ContainerState{
							Waiting: &corev1.ContainerStateWaiting{
								Reason:  reason,
								Message: "container startup failed",
							},
						},
					},
				},
			},
		}, nil
	}

	observation, err := mgr.observeLifecycleRecovery(
		context.Background(),
		target,
	)
	if err != nil {
		t.Fatalf("observe waiting container: %v", err)
	}
	return observation
}
