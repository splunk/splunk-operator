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
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	shcworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/shc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestLifecycleBlockedErrorIsTerminalAndEmitsOnce(t *testing.T) {
	target := int32(2)
	cr := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				OperationID:   "pod-update-2",
				TargetPod:     "splunk-example-search-head-2",
				TargetOrdinal: &target,
				Stage: enterpriseApi.
					SearchHeadClusterLifecycleStageBlocked,
				Reason: enterpriseApi.
					SearchHeadClusterLifecycleReasonSplunkStartupFailed,
				Message: "replacement Pod startup timed out",
			},
		},
	}
	recorder := &mockEventRecorder{}
	ctx := context.WithValue(
		context.Background(),
		splcommon.EventPublisherKey,
		&K8EventPublisher{recorder: recorder, instance: cr},
	)
	mgr := &searchHeadClusterPodManager{cr: cr}

	err := mgr.lifecycleBlockedError(
		ctx,
		enterpriseApi.SearchHeadClusterLifecycleStageWaitingForContainer,
	)
	message, terminal := splcommon.TerminalMessage(err)
	if !terminal || message != "replacement Pod startup timed out" {
		t.Fatalf(
			"terminal error message=%q terminal=%t error=%v",
			message,
			terminal,
			err,
		)
	}
	reason, terminal := splcommon.TerminalReason(err)
	if !terminal ||
		reason != string(
			enterpriseApi.
				SearchHeadClusterLifecycleReasonSplunkStartupFailed,
		) {
		t.Fatalf(
			"terminal error reason=%q terminal=%t error=%v",
			reason,
			terminal,
			err,
		)
	}
	if !strings.Contains(
		cr.Status.Message,
		string(
			enterpriseApi.
				SearchHeadClusterLifecycleReasonSplunkStartupFailed,
		),
	) {
		t.Fatalf("status message = %q, want startup reason", cr.Status.Message)
	}
	assertRolloutEvent(
		t,
		recorder,
		EventReasonSHCRolloutBlocked,
		corev1.EventTypeWarning,
	)

	eventsBefore := len(recorder.events)
	err = mgr.lifecycleBlockedError(
		ctx,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
	)
	if _, terminal = splcommon.TerminalMessage(err); !terminal {
		t.Fatalf("persisted blocked operation lost terminal error: %v", err)
	}
	if len(recorder.events) != eventsBefore {
		t.Fatalf(
			"persisted blocked operation emitted %d additional events",
			len(recorder.events)-eventsBefore,
		)
	}
}

func TestNormalizeSearchHeadClusterPodUpdatePhase(t *testing.T) {
	target := int32(2)
	active := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		Intent: enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageWaitingForContainer,
		TargetOrdinal: &target,
	}
	completed := active.DeepCopy()
	completed.Stage = enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	scaleDown := active.DeepCopy()
	scaleDown.Intent = enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown

	tests := []struct {
		name              string
		phase             enterpriseApi.Phase
		operation         *enterpriseApi.SearchHeadClusterLifecycleOperationStatus
		recoveryCompleted bool
		want              enterpriseApi.Phase
	}{
		{
			name:      "active pod update is not scale up",
			phase:     enterpriseApi.PhaseScalingUp,
			operation: active,
			want:      enterpriseApi.PhaseUpdating,
		},
		{
			name:              "recovery completed in this reconcile is still updating",
			phase:             enterpriseApi.PhaseScalingUp,
			operation:         completed,
			recoveryCompleted: true,
			want:              enterpriseApi.PhaseUpdating,
		},
		{
			name:      "stale completed operation does not hide later scale up",
			phase:     enterpriseApi.PhaseScalingUp,
			operation: completed,
			want:      enterpriseApi.PhaseScalingUp,
		},
		{
			name:      "scale down lifecycle does not change scale up phase",
			phase:     enterpriseApi.PhaseScalingUp,
			operation: scaleDown,
			want:      enterpriseApi.PhaseScalingUp,
		},
		{
			name:      "ready phase remains ready",
			phase:     enterpriseApi.PhaseReady,
			operation: active,
			want:      enterpriseApi.PhaseReady,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalizeSearchHeadClusterPodUpdatePhase(
				test.phase,
				test.operation,
				test.recoveryCompleted,
			)
			if got != test.want {
				t.Fatalf("phase = %s, want %s", got, test.want)
			}
		})
	}
}

func TestRecordStableReplicaCountEmitsOnlyDesiredScaleEvents(t *testing.T) {
	newManager := func(
		stable *int32,
		ready int32,
	) (*searchHeadClusterPodManager, *mockEventRecorder, context.Context) {
		cr := &enterpriseApi.SearchHeadCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example",
				Namespace: "test",
			},
			Status: enterpriseApi.SearchHeadClusterStatus{
				ReadyReplicas:      ready,
				LastStableReplicas: stable,
			},
		}
		recorder := &mockEventRecorder{}
		publisher := &K8EventPublisher{recorder: recorder, instance: cr}
		ctx := context.WithValue(
			context.Background(),
			splcommon.EventPublisherKey,
			publisher,
		)
		return &searchHeadClusterPodManager{cr: cr}, recorder, ctx
	}

	t.Run("initializes migration baseline without scale event", func(t *testing.T) {
		mgr, recorder, ctx := newManager(nil, 3)
		mgr.recordStableReplicaCount(
			ctx,
			GetEventPublisher(ctx, mgr.cr),
			enterpriseApi.PhaseReady,
			3,
		)
		if mgr.cr.Status.LastStableReplicas == nil ||
			*mgr.cr.Status.LastStableReplicas != 3 {
			t.Fatalf(
				"last stable replicas = %v, want 3",
				mgr.cr.Status.LastStableReplicas,
			)
		}
		if len(recorder.events) != 0 {
			t.Fatalf("migration baseline emitted %d events", len(recorder.events))
		}
	})

	t.Run("pod readiness recovery does not emit scale event", func(t *testing.T) {
		stable := int32(3)
		mgr, recorder, ctx := newManager(&stable, 3)
		mgr.recordStableReplicaCount(
			ctx,
			GetEventPublisher(ctx, mgr.cr),
			enterpriseApi.PhaseReady,
			3,
		)
		if len(recorder.events) != 0 {
			t.Fatalf("readiness recovery emitted %d events", len(recorder.events))
		}
	})

	t.Run("desired scale up emits once and advances baseline", func(t *testing.T) {
		stable := int32(3)
		mgr, recorder, ctx := newManager(&stable, 5)
		mgr.recordStableReplicaCount(
			ctx,
			GetEventPublisher(ctx, mgr.cr),
			enterpriseApi.PhaseReady,
			5,
		)
		assertRolloutEvent(
			t,
			recorder,
			EventReasonScaledUp,
			corev1.EventTypeNormal,
		)
		if *mgr.cr.Status.LastStableReplicas != 5 {
			t.Fatalf(
				"last stable replicas = %d, want 5",
				*mgr.cr.Status.LastStableReplicas,
			)
		}
	})

	t.Run("desired scale down emits once and advances baseline", func(t *testing.T) {
		stable := int32(5)
		mgr, recorder, ctx := newManager(&stable, 3)
		mgr.recordStableReplicaCount(
			ctx,
			GetEventPublisher(ctx, mgr.cr),
			enterpriseApi.PhaseReady,
			3,
		)
		assertRolloutEvent(
			t,
			recorder,
			EventReasonScaledDown,
			corev1.EventTypeNormal,
		)
		if *mgr.cr.Status.LastStableReplicas != 3 {
			t.Fatalf(
				"last stable replicas = %d, want 3",
				*mgr.cr.Status.LastStableReplicas,
			)
		}
	})

	t.Run("non-ready phase preserves baseline", func(t *testing.T) {
		stable := int32(3)
		mgr, recorder, ctx := newManager(&stable, 2)
		mgr.recordStableReplicaCount(
			ctx,
			GetEventPublisher(ctx, mgr.cr),
			enterpriseApi.PhaseUpdating,
			3,
		)
		if *mgr.cr.Status.LastStableReplicas != 3 {
			t.Fatalf(
				"last stable replicas = %d, want 3",
				*mgr.cr.Status.LastStableReplicas,
			)
		}
		if len(recorder.events) != 0 {
			t.Fatalf("non-ready phase emitted %d events", len(recorder.events))
		}
	})
}

func TestLifecycleMemberObservationExpectedUnavailable(t *testing.T) {
	target := int32(1)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		TargetOrdinal: &target,
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageWaitingForContainer,
	}
	if !lifecycleMemberObservationExpectedUnavailable(operation, target) {
		t.Fatal("target container wait should be expected unavailable")
	}
	if lifecycleMemberObservationExpectedUnavailable(operation, 0) {
		t.Fatal("non-target unavailability must remain unexpected")
	}
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget
	if lifecycleMemberObservationExpectedUnavailable(operation, target) {
		t.Fatal("detention-stage target must still be observable")
	}
	operation.Stage = enterpriseApi.
		SearchHeadClusterLifecycleStageValidatingRecovery
	if !lifecycleMemberObservationExpectedUnavailable(operation, target) {
		t.Fatal("recovery validation can observe bounded target unavailability")
	}
	operation.Intent =
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown
	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement
	requestedAt := metav1.Now()
	operation.MembershipRemovalRequestedAt = &requestedAt
	if !lifecycleMemberObservationExpectedUnavailable(operation, target) {
		t.Fatal("removed scale-down target should be expected unavailable")
	}
	operation.MembershipRemovalRequestedAt = nil
	if lifecycleMemberObservationExpectedUnavailable(operation, target) {
		t.Fatal("scale-down target must remain observable before membership removal")
	}
}

func TestSearchHeadClusterMemberObservationCountPreservesHigherOrdinals(
	t *testing.T,
) {
	specReplicas := int32(3)
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{Replicas: &specReplicas},
		Status: appsv1.StatefulSetStatus{
			Replicas: 2,
		},
	}
	target := int32(1)
	operation := &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		TargetOrdinal: &target,
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageWaitingForTermination,
	}

	if got := searchHeadClusterMemberObservationCount(
		statefulSet,
		operation,
	); got != 3 {
		t.Fatalf(
			"active Pod-update observation count = %d, want all 3 desired ordinals",
			got,
		)
	}

	operation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	if got := searchHeadClusterMemberObservationCount(
		statefulSet,
		operation,
	); got != 2 {
		t.Fatalf(
			"completed operation observation count = %d, want observed count 2",
			got,
		)
	}

	statefulSet.Status.Replicas = 4
	if got := searchHeadClusterMemberObservationCount(
		statefulSet,
		operation,
	); got != 4 {
		t.Fatalf(
			"scale-down observation count = %d, want observed count 4",
			got,
		)
	}
}

func TestUpdateStatusPreservesHigherOrdinalDuringLowerOrdinalReplacement(
	t *testing.T,
) {
	oldGetMemberInfo := GetSearchHeadClusterMemberInfo
	oldGetCaptainInfo := GetSearchHeadCaptainInfo
	t.Cleanup(func() {
		GetSearchHeadClusterMemberInfo = oldGetMemberInfo
		GetSearchHeadCaptainInfo = oldGetCaptainInfo
	})

	target := int32(1)
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "example"},
		Status: enterpriseApi.SearchHeadClusterStatus{
			Members: []enterpriseApi.SearchHeadClusterMemberStatus{
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
			},
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				TargetOrdinal: &target,
				Stage: enterpriseApi.
					SearchHeadClusterLifecycleStageWaitingForTermination,
			},
		},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}
	observedOrdinals := make(map[int32]bool)
	GetSearchHeadClusterMemberInfo = func(
		_ context.Context,
		_ *searchHeadClusterPodManager,
		ordinal int32,
	) (*splclient.SearchHeadClusterMemberInfo, error) {
		observedOrdinals[ordinal] = true
		if ordinal == target {
			return nil, errors.New("target is terminating")
		}
		return &splclient.SearchHeadClusterMemberInfo{
			Status:     "Up",
			Registered: true,
		}, nil
	}
	GetSearchHeadCaptainInfo = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (*splclient.SearchHeadCaptainInfo, error) {
		return &splclient.SearchHeadCaptainInfo{
			Label:          "splunk-example-search-head-0",
			ServiceReady:   true,
			Initialized:    true,
			MinPeersJoined: true,
		}, nil
	}

	specReplicas := int32(3)
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{Replicas: &specReplicas},
		Status: appsv1.StatefulSetStatus{
			Replicas:      2,
			ReadyReplicas: 2,
		},
	}
	if err := mgr.updateStatus(context.Background(), statefulSet); err != nil {
		t.Fatalf("update status: %v", err)
	}

	if len(cr.Status.Members) != 3 ||
		!observedOrdinals[2] ||
		cr.Status.Members[2].Status != "Up" ||
		!cr.Status.Members[2].Registered {
		t.Fatalf(
			"higher ordinal was not preserved: members=%#v observed=%v",
			cr.Status.Members,
			observedOrdinals,
		)
	}
}

func TestScaleUpMemberObservationExpectedUnavailable(t *testing.T) {
	stable := int32(3)
	replicas := int32(4)
	statefulSet := &appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{Replicas: &replicas},
		Status: appsv1.StatefulSetStatus{
			Replicas:      4,
			ReadyReplicas: 3,
		},
	}
	if !scaleUpMemberObservationExpectedUnavailable(
		&stable,
		statefulSet,
		3,
	) {
		t.Fatal("starting additive ordinal should be expected unavailable")
	}
	if scaleUpMemberObservationExpectedUnavailable(
		&stable,
		statefulSet,
		2,
	) {
		t.Fatal("existing member unavailability must remain unexpected")
	}
	statefulSet.Status.ReadyReplicas = 4
	if scaleUpMemberObservationExpectedUnavailable(
		&stable,
		statefulSet,
		3,
	) {
		t.Fatal("stable additive member unavailability must be unexpected")
	}
}

func TestRollingUpdateOwnsClusterUpgradeLifecycle(t *testing.T) {
	target := int32(2)
	mgr := &searchHeadClusterPodManager{
		statefulSet: &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
					Type: appsv1.RollingUpdateStatefulSetStrategyType,
				},
			},
		},
		cr: &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
					Intent:          enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
					DesiredRevision: "revision-2",
					TargetOrdinal:   &target,
				},
			},
		},
	}

	if !shcRollingUpdateOwnsClusterUpgradeLifecycle(mgr) {
		t.Fatal("RollingUpdate did not own its cluster upgrade lifecycle")
	}

	tests := []struct {
		name   string
		mutate func(*searchHeadClusterPodManager)
	}{
		{
			name: "missing StatefulSet",
			mutate: func(mgr *searchHeadClusterPodManager) {
				mgr.statefulSet = nil
			},
		},
		{
			name: "OnDelete compatibility",
			mutate: func(mgr *searchHeadClusterPodManager) {
				mgr.statefulSet.Spec.UpdateStrategy.Type =
					appsv1.OnDeleteStatefulSetStrategyType
			},
		},
		{
			name: "missing lifecycle",
			mutate: func(mgr *searchHeadClusterPodManager) {
				mgr.cr.Status.LifecycleOperation = nil
			},
		},
		{
			name: "member intent differs",
			mutate: func(mgr *searchHeadClusterPodManager) {
				mgr.cr.Status.LifecycleOperation.Intent =
					enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copy := mgr.cr.DeepCopy()
			testManager := &searchHeadClusterPodManager{
				cr:          copy,
				statefulSet: mgr.statefulSet.DeepCopy(),
			}
			test.mutate(testManager)
			if shcRollingUpdateOwnsClusterUpgradeLifecycle(testManager) {
				t.Fatal("legacy path was incorrectly treated as RollingUpdate-owned")
			}
		})
	}
}

func TestOnDeleteUpgradeInitializationUsesObservedCaptain(t *testing.T) {
	target := int32(2)
	mgr := &searchHeadClusterPodManager{
		statefulSet: &appsv1.StatefulSet{
			Spec: appsv1.StatefulSetSpec{
				UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
					Type: appsv1.OnDeleteStatefulSetStrategyType,
				},
			},
		},
		cr: &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				Captain:      "splunk-example-search-head-1",
				CaptainReady: true,
				Members: []enterpriseApi.SearchHeadClusterMemberStatus{
					{Name: "splunk-example-search-head-0", Status: "Up", Registered: true},
					{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
					{Name: "splunk-example-search-head-2", Status: "Up", Registered: true},
				},
				LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
					Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
					TargetOrdinal: &target,
				},
			},
		},
	}

	oldInitiateUpgrade := initiateSearchHeadClusterUpgrade
	oldSetDetention := setSearchHeadDetention
	t.Cleanup(func() {
		initiateSearchHeadClusterUpgrade = oldInitiateUpgrade
		setSearchHeadDetention = oldSetDetention
	})

	initOrdinal := int32(-1)
	initiateSearchHeadClusterUpgrade = func(
		_ context.Context,
		_ *searchHeadClusterPodManager,
		ordinal int32,
	) error {
		initOrdinal = ordinal
		return nil
	}
	detentionOrdinal := int32(-1)
	detain := false
	setSearchHeadDetention = func(
		_ context.Context,
		_ *searchHeadClusterPodManager,
		ordinal int32,
		requested bool,
	) error {
		detentionOrdinal = ordinal
		detain = requested
		return nil
	}

	if err := requestSearchHeadDetention(
		context.Background(),
		mgr,
		target,
	); err != nil {
		t.Fatalf("request detention: %v", err)
	}
	if initOrdinal != 1 {
		t.Fatalf("upgrade initialization ordinal = %d, want captain ordinal 1", initOrdinal)
	}
	if detentionOrdinal != target || !detain {
		t.Fatalf(
			"detention target ordinal = %d detain = %t, want ordinal %d on",
			detentionOrdinal,
			detain,
			target,
		)
	}
}

func TestLifecycleAdapterPersistsStagesBeforeActions(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterLifecycleNow
	oldGetMembers := getSearchHeadCaptainMembers
	oldGetKVStoreStatus := getSearchHeadKVStoreStatus
	oldRequestDetention := requestSearchHeadDetention
	oldTransferCaptain := transferSearchHeadCaptain
	oldGetLifecyclePod := getSearchHeadLifecyclePod
	t.Cleanup(func() {
		searchHeadClusterLifecycleNow = oldNow
		getSearchHeadCaptainMembers = oldGetMembers
		getSearchHeadKVStoreStatus = oldGetKVStoreStatus
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
	getSearchHeadKVStoreStatus = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (string, error) {
		return "ready", nil
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

	// Reconcile 2 persists the original Pod identity as its own barrier;
	// detention is still not called.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.Stage != enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster {
		t.Fatalf("stage = %q, want ValidatingCluster", cr.Status.LifecycleOperation.Stage)
	}
	if cr.Status.LifecycleOperation.TargetPodUID != "original-pod-uid" {
		t.Fatalf(
			"target Pod UID = %q, want original identity captured before detention",
			cr.Status.LifecycleOperation.TargetPodUID,
		)
	}
	if detentionCalls != 0 {
		t.Fatal("detention executed in the same reconcile as its stage transition")
	}

	// Reconcile 3 persists DetainingTarget; detention is still not called.
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

	// Reconcile 4 observes the persisted stage and may request detention.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		2,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if detentionCalls != 1 {
		t.Fatalf("detention calls = %d, want 1", detentionCalls)
	}
	if cr.Status.LifecycleOperation.DetentionRequestedAt == nil ||
		cr.Status.LifecycleOperation.DetentionRequestAttemptCount != 1 {
		t.Fatalf(
			"detention request status = %#v, want first attempt recorded",
			cr.Status.LifecycleOperation,
		)
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

func TestLifecycleAdapterRequiresKVStorePreflightBeforeDetention(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	oldGetMembers := getSearchHeadCaptainMembers
	oldGetKVStoreStatus := getSearchHeadKVStoreStatus
	t.Cleanup(func() {
		getSearchHeadCaptainMembers = oldGetMembers
		getSearchHeadKVStoreStatus = oldGetKVStoreStatus
	})

	target := int32(2)
	targetPod := "splunk-example-search-head-2"
	cr := &enterpriseApi.SearchHeadCluster{}
	cr.Name = "example"
	cr.Spec.Replicas = 3
	cr.Status.Initialized = true
	cr.Status.MinPeersJoined = true
	cr.Status.CaptainReady = true
	cr.Status.Captain = "splunk-example-search-head-0"
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-example-search-head-0", Status: "Up", Registered: true},
		{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
		{Name: targetPod, Status: "Up", Registered: true},
	}
	cr.Status.LifecycleOperation = shcworkflow.StartReplacement(
		"operation-1",
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		"revision-2",
		targetPod,
		target,
		now,
	)
	mgr := &searchHeadClusterPodManager{cr: cr}

	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			"splunk-example-search-head-0": {
				Identifier: "member-guid-0",
				Label:      "splunk-example-search-head-0",
				Status:     "Up",
				Captain:    true,
			},
			"splunk-example-search-head-1": {
				Identifier: "member-guid-1",
				Label:      "splunk-example-search-head-1",
				Status:     "Up",
			},
			targetPod: {
				Identifier: "member-guid-2",
				Label:      targetPod,
				Status:     "Up",
			},
		}, nil
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

	observation := mgr.observeLifecycleReplacement(
		context.Background(),
		target,
		now,
	)
	decision := shcworkflow.EvaluateReplacement(
		cr.Status.LifecycleOperation,
		observation,
		shcworkflow.ReplacementPolicy{
			DetentionTimeout:       30 * time.Second,
			SearchDrainTimeout:     30 * time.Second,
			CaptainTransferTimeout: 30 * time.Second,
		},
		now,
	)

	if decision.Action.Type != shcworkflow.ActionObserveCluster ||
		decision.Operation.Stage !=
			enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster ||
		decision.Operation.Reason !=
			enterpriseApi.SearchHeadClusterLifecycleReasonKVStoreNotReady ||
		!reflect.DeepEqual(
			decision.Operation.KVStoreNotReadyMembers,
			[]string{"splunk-example-search-head-1=starting"},
		) {
		t.Fatalf(
			"KV Store preflight decision=%#v observation=%#v",
			decision,
			observation,
		)
	}
}

func TestScaleDownCaptainTransferPrecedesMembershipRemoval(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	stubReadySearchHeadKVStore(t)

	now := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	target := int32(2)
	targetPod := "splunk-example-search-head-2"
	preferredCaptain := "splunk-example-search-head-1"
	stageStartedAt := metav1.NewTime(now)
	cr := &enterpriseApi.SearchHeadCluster{}
	cr.Name = "example"
	cr.Status.Initialized = true
	cr.Status.MinPeersJoined = true
	cr.Status.CaptainReady = true
	cr.Status.Captain = targetPod
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-example-search-head-0", Status: "Up", Registered: true},
		{Name: preferredCaptain, Status: "Up", Registered: true},
		{Name: targetPod, Status: "ManualDetention", Registered: true},
	}
	cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:    "ScaleDown:splunk-example-search-head-2:",
		Intent:         enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
		TargetPod:      targetPod,
		TargetOrdinal:  &target,
		TargetMemberID: "member-guid-2",
		Stage: enterpriseApi.
			SearchHeadClusterLifecycleStageTransferringCaptain,
		StageStartedAt:     &stageStartedAt,
		LastTransitionTime: &stageStartedAt,
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldNow := searchHeadClusterLifecycleNow
	oldGetMembers := getSearchHeadCaptainMembers
	oldTransferCaptain := transferSearchHeadCaptain
	oldGetLifecyclePod := getSearchHeadLifecyclePod
	oldRemoveMember := removeSearchHeadClusterMember
	t.Cleanup(func() {
		searchHeadClusterLifecycleNow = oldNow
		getSearchHeadCaptainMembers = oldGetMembers
		transferSearchHeadCaptain = oldTransferCaptain
		getSearchHeadLifecyclePod = oldGetLifecyclePod
		removeSearchHeadClusterMember = oldRemoveMember
	})
	searchHeadClusterLifecycleNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}

	captainMembers := map[string]splclient.SearchHeadCaptainMemberInfo{
		"splunk-example-search-head-0": {
			Identifier:    "member-guid-0",
			Label:         "splunk-example-search-head-0",
			Status:        "Up",
			ManagementURI: "https://splunk-example-search-head-0:8089",
		},
		preferredCaptain: {
			Identifier:       "member-guid-1",
			Label:            preferredCaptain,
			Status:           "Up",
			ManagementURI:    "https://splunk-example-search-head-1:8089",
			PreferredCaptain: true,
		},
		targetPod: {
			Identifier:    "member-guid-2",
			Label:         targetPod,
			Status:        "ManualDetention",
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
			ObjectMeta: metav1.ObjectMeta{UID: types.UID("scale-down-pod-uid")},
		}, nil
	}
	removeCalls := 0
	removeSearchHeadClusterMember = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		removeCalls++
		return nil
	}

	// First persist the original Pod identity. The transfer stage cannot
	// submit its side effect in the same reconciliation.
	ready, err := mgr.PrepareScaleDown(context.Background(), target)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.TargetPodUID != "scale-down-pod-uid" {
		t.Fatalf(
			"scale-down target UID = %q, want durable original identity",
			cr.Status.LifecycleOperation.TargetPodUID,
		)
	}
	if transferCalls != 0 || removeCalls != 0 {
		t.Fatalf(
			"calls during identity barrier: transfer=%d removal=%d; want 0 and 0",
			transferCalls,
			removeCalls,
		)
	}

	// A persisted transfer stage now submits exactly one transfer request. It
	// cannot remove membership while the target remains the observed captain.
	ready, err = mgr.PrepareScaleDown(context.Background(), target)
	assertLifecycleAdapterResult(t, ready, err, false)
	if transferCalls != 1 ||
		transferTarget != "https://splunk-example-search-head-1:8089" {
		t.Fatalf(
			"transfer calls = %d, target = %q; want one request to preferred candidate",
			transferCalls,
			transferTarget,
		)
	}
	if removeCalls != 0 {
		t.Fatalf("membership removal calls before transfer confirmation = %d, want 0", removeCalls)
	}

	// A resumed reconcile only observes the submitted request.
	ready, err = mgr.PrepareScaleDown(context.Background(), target)
	assertLifecycleAdapterResult(t, ready, err, false)
	if transferCalls != 1 || removeCalls != 0 {
		t.Fatalf(
			"calls while target remains captain: transfer=%d removal=%d; want 1 and 0",
			transferCalls,
			removeCalls,
		)
	}

	// Authoritative agreement on a different ready captain persists the
	// authorization stage without removing the member in the same reconcile.
	cr.Status.Captain = preferredCaptain
	targetInfo := captainMembers[targetPod]
	targetInfo.Captain = false
	captainMembers[targetPod] = targetInfo
	preferredInfo := captainMembers[preferredCaptain]
	preferredInfo.Captain = true
	captainMembers[preferredCaptain] = preferredInfo

	ready, err = mgr.PrepareScaleDown(context.Background(), target)
	assertLifecycleAdapterResult(t, ready, err, false)
	if cr.Status.LifecycleOperation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement {
		t.Fatalf(
			"stage = %q, want AuthorizingReplacement",
			cr.Status.LifecycleOperation.Stage,
		)
	}
	if removeCalls != 0 {
		t.Fatalf("membership removal calls during authorization transition = %d, want 0", removeCalls)
	}

	// A later reconcile authorizes replacement and requests membership
	// removal, then persists another barrier before replica reduction.
	ready, err = mgr.PrepareScaleDown(context.Background(), target)
	assertLifecycleAdapterResult(t, ready, err, false)
	if removeCalls != 1 ||
		cr.Status.LifecycleOperation.MembershipRemovalRequestedAt == nil {
		t.Fatalf(
			"membership removal calls = %d, requestedAt = %v; want one durable request",
			removeCalls,
			cr.Status.LifecycleOperation.MembershipRemovalRequestedAt,
		)
	}
	if cr.Status.LifecycleOperation.TargetPodUID != "scale-down-pod-uid" ||
		cr.Status.LifecycleOperation.ReplacementAuthorizedAt == nil {
		t.Fatalf(
			"scale-down authorization = %#v, want captured Pod UID and timestamp",
			cr.Status.LifecycleOperation,
		)
	}

	ready, err = mgr.PrepareScaleDown(context.Background(), target)
	assertLifecycleAdapterResult(t, ready, err, true)
	if transferCalls != 1 || removeCalls != 1 {
		t.Fatalf(
			"calls after durable resume: transfer=%d removal=%d; want 1 and 1",
			transferCalls,
			removeCalls,
		)
	}
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

func TestLifecycleStartsNewScaleDownAfterCompletedCancellation(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	now := time.Date(2026, 7, 28, 5, 30, 0, 0, time.UTC)
	oldNow := searchHeadClusterLifecycleNow
	t.Cleanup(func() { searchHeadClusterLifecycleNow = oldNow })
	searchHeadClusterLifecycleNow = func() time.Time { return now }

	target := int32(3)
	oldStartedAt := metav1.NewTime(now.Add(-time.Hour))
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "example",
			Generation: 42,
		},
	}
	cr.Status.LifecycleOperation =
		&enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
			OperationID:           "ScaleDown:splunk-example-search-head-3:",
			Intent:                enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
			TargetPod:             "splunk-example-search-head-3",
			TargetOrdinal:         &target,
			TargetPodUID:          "original-pod-uid",
			Stage:                 enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
			Reason:                enterpriseApi.SearchHeadClusterLifecycleReasonOperationCompleted,
			StartedAt:             &oldStartedAt,
			MemberRejoinStartedAt: &oldStartedAt,
		}
	mgr := &searchHeadClusterPodManager{cr: cr}

	ready, err := mgr.PrepareScaleDown(context.Background(), target)
	assertLifecycleAdapterResult(t, ready, err, false)

	operation := cr.Status.LifecycleOperation
	if operation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageValidatingCluster {
		t.Fatalf(
			"stage = %q, want a new ValidatingCluster operation",
			operation.Stage,
		)
	}
	if operation.OperationID !=
		"ScaleDown:splunk-example-search-head-3::42" {
		t.Fatalf(
			"operation ID = %q, want generation-scoped identity",
			operation.OperationID,
		)
	}
	if operation.StartedAt == nil ||
		!operation.StartedAt.Time.Equal(now) {
		t.Fatalf("startedAt = %v, want %v", operation.StartedAt, now)
	}
	if operation.TargetPodUID != "" ||
		operation.MemberRejoinStartedAt != nil ||
		operation.MembershipRemovalRequestedAt != nil {
		t.Fatalf(
			"new scale-down retained historical state: %#v",
			operation,
		)
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

func TestLifecycleAdapterClassifiesNonzeroContainerTermination(t *testing.T) {
	observation := observeLifecyclePodWithContainerStatus(
		t,
		corev1.ContainerStatus{
			Name: "splunk",
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 2,
					Reason:   "Error",
				},
			},
		},
	)
	if !observation.ContainerStartupFailed ||
		observation.ContainerFailureTerminal {
		t.Fatalf(
			"nonzero current termination observation = %#v",
			observation,
		)
	}

	observation = observeLifecyclePodWithContainerStatus(
		t,
		corev1.ContainerStatus{
			Name:         "splunk",
			RestartCount: 4,
			LastTerminationState: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 1,
					Reason:   "Error",
				},
			},
		},
	)
	if !observation.ContainerStartupFailed ||
		observation.ContainerFailureTerminal {
		t.Fatalf(
			"nonzero previous termination observation = %#v",
			observation,
		)
	}
}

func TestLifecycleAdapterObservesContainersReadyBeforePodReady(t *testing.T) {
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
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
					{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionFalse,
						Reason: "ReadinessGatesNotReady",
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
		t.Fatalf("observe replacement readiness conditions: %v", err)
	}
	if !observation.PodExists ||
		!observation.PodScheduled ||
		!observation.ContainersReady ||
		observation.PodReady {
		t.Fatalf("replacement readiness observation = %#v", observation)
	}
}

func TestLifecycleAdapterRequiresCaptainToObserveReplacementMember(t *testing.T) {
	target := int32(2)
	targetPod := "splunk-example-search-head-2"
	captainPod := "splunk-example-search-head-0"
	cr := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			Captain:      captainPod,
			CaptainReady: true,
			Members: []enterpriseApi.SearchHeadClusterMemberStatus{
				{Name: captainPod, Status: "Up", Registered: true},
				{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
				{Name: targetPod, Status: "Up", Registered: true},
			},
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				TargetPod:     targetPod,
				TargetOrdinal: &target,
			},
		},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldGetLifecyclePod := getSearchHeadLifecyclePod
	oldGetMembers := getSearchHeadCaptainMembers
	t.Cleanup(func() {
		getSearchHeadLifecyclePod = oldGetLifecyclePod
		getSearchHeadCaptainMembers = oldGetMembers
	})
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
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}, nil
	}
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			captainPod: {
				Identifier: "member-guid-0",
				Label:      captainPod,
				Status:     "Up",
				Captain:    true,
			},
			"splunk-example-search-head-1": {
				Identifier: "member-guid-1",
				Label:      "splunk-example-search-head-1",
				Status:     "Up",
			},
		}, nil
	}

	observation, err := mgr.observeLifecycleRecovery(
		context.Background(),
		target,
	)
	if err != nil {
		t.Fatalf("observe member missing from captain view: %v", err)
	}
	if !observation.PodReady ||
		!observation.MemberObserved ||
		!observation.MemberRegistered ||
		!observation.AuthoritativeCaptain ||
		!observation.CaptainReady {
		t.Fatalf("local and captain availability observation = %#v", observation)
	}
	if observation.CaptainMemberObserved ||
		observation.CaptainMemberID != "" ||
		observation.CaptainMemberStatus != "" {
		t.Fatalf("target unexpectedly appeared in captain view: %#v", observation)
	}
}

func TestLifecycleAdapterPreservesNonUpMemberViews(t *testing.T) {
	target := int32(2)
	targetPod := "splunk-example-search-head-2"
	captainPod := "splunk-example-search-head-0"
	cr := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			Captain:      captainPod,
			CaptainReady: true,
			Members: []enterpriseApi.SearchHeadClusterMemberStatus{
				{Name: captainPod, Status: "Up", Registered: true},
				{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
				{Name: targetPod, Status: "Joining", Registered: true},
			},
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				TargetPod:     targetPod,
				TargetOrdinal: &target,
			},
		},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldGetLifecyclePod := getSearchHeadLifecyclePod
	oldGetMembers := getSearchHeadCaptainMembers
	t.Cleanup(func() {
		getSearchHeadLifecyclePod = oldGetLifecyclePod
		getSearchHeadCaptainMembers = oldGetMembers
	})
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
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}, nil
	}
	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			captainPod: {
				Identifier: "member-guid-0",
				Label:      captainPod,
				Status:     "Up",
				Captain:    true,
			},
			targetPod: {
				Identifier: "member-guid-2",
				Label:      targetPod,
				Status:     "Syncing",
			},
		}, nil
	}

	observation, err := mgr.observeLifecycleRecovery(
		context.Background(),
		target,
	)
	if err != nil {
		t.Fatalf("observe non-Up replacement: %v", err)
	}
	if !observation.PodReady ||
		!observation.MemberObserved ||
		!observation.MemberRegistered ||
		!observation.CaptainMemberObserved ||
		!observation.AuthoritativeCaptain {
		t.Fatalf("non-Up replacement observation = %#v", observation)
	}
	if observation.MemberStatus != "Joining" ||
		observation.CaptainMemberStatus != "Syncing" {
		t.Fatalf(
			"member statuses = local %q, captain %q",
			observation.MemberStatus,
			observation.CaptainMemberStatus,
		)
	}
}

func TestLifecycleAdapterTreatsOrdinalZeroAsNonCaptainWhenObservedElsewhere(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	stubReadySearchHeadKVStore(t)

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

	// Persist the original Pod identity before evaluating the durable drain
	// stage or returning permission to replace ordinal zero.
	ready, err := mgr.prepareLifecycleReplacement(
		context.Background(),
		target,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	operation := cr.Status.LifecycleOperation
	if operation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageDrainingSearches {
		t.Fatalf("stage = %q, want DrainingSearches", operation.Stage)
	}
	if operation.TargetPodUID != "ordinal-zero-pod-uid" ||
		operation.ReplacementAuthorizedAt != nil {
		t.Fatalf(
			"ordinal-zero identity barrier = %#v, want captured but unauthorized target",
			operation,
		)
	}
	if transferCalls != 0 {
		t.Fatalf("captain transfer calls = %d, want zero", transferCalls)
	}

	// A later reconciliation persists replacement authorization as a separate
	// stage, without returning replacement permission.
	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		target,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if transferCalls != 0 {
		t.Fatalf("captain transfer calls after authorization stage = %d, want zero", transferCalls)
	}
	operation = cr.Status.LifecycleOperation
	if operation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement ||
		operation.ReplacementAuthorizedAt != nil {
		t.Fatalf(
			"ordinal-zero authorization barrier = %#v, want staged but unauthorized target",
			operation,
		)
	}
	if operation.Captain != captainPod {
		t.Fatalf("observed captain = %q, want %q", operation.Captain, captainPod)
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
		{
			Name:                        "splunk-example-search-head-2",
			Status:                      "ManualDetention",
			Registered:                  true,
			ActiveHistoricalSearchCount: 1,
			ActiveRealtimeSearchCount:   1,
		},
	}
	cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		OperationID:              "operation-1",
		Intent:                   enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		DesiredRevision:          "revision-2",
		TargetPod:                "splunk-example-search-head-2",
		TargetOrdinal:            &ordinal,
		Stage:                    enterpriseApi.SearchHeadClusterLifecycleStageAuthorizingReplacement,
		TargetPodUID:             "old-pod-uid",
		TargetMemberID:           "member-guid-2",
		ReplacementAuthorizedAt:  &authorizedAt,
		ActiveHistoricalSearches: 2,
		ActiveRealtimeSearches:   2,
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
		if releaseCalls == 1 {
			return &url.Error{
				Op:  "Post",
				URL: "https://splunk-example-search-head-2:8089/detention",
				Err: context.DeadlineExceeded,
			}
		}
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
	if cr.Status.LifecycleOperation.ActiveHistoricalSearches != 1 ||
		cr.Status.LifecycleOperation.ActiveRealtimeSearches != 1 {
		t.Fatalf(
			"adapter recovery counts = historical %d realtime %d, want current 1 and 1",
			cr.Status.LifecycleOperation.ActiveHistoricalSearches,
			cr.Status.LifecycleOperation.ActiveRealtimeSearches,
		)
	}

	complete, err = mgr.resumeLifecycleRecovery(context.Background(), ordinal)
	assertLifecycleAdapterResult(t, complete, err, false)
	if releaseCalls != 1 || cr.Status.LifecycleOperation.DetentionReleaseRequestedAt == nil {
		t.Fatalf("release calls = %d, requestedAt = %v; want one durable unknown-outcome request",
			releaseCalls,
			cr.Status.LifecycleOperation.DetentionReleaseRequestedAt,
		)
	}
	if cr.Status.LifecycleOperation.Reason !=
		enterpriseApi.SearchHeadClusterLifecycleReasonDetentionReleasePending ||
		cr.Status.LifecycleOperation.RetryCount != 1 {
		t.Fatalf(
			"unknown-outcome status = %#v, want pending with one attempt",
			cr.Status.LifecycleOperation,
		)
	}

	// A restart/resume retries the idempotent desired state while both Splunk
	// views still report ManualDetention.
	complete, err = mgr.resumeLifecycleRecovery(context.Background(), ordinal)
	assertLifecycleAdapterResult(t, complete, err, false)
	if releaseCalls != 2 ||
		cr.Status.LifecycleOperation.RetryCount != 2 {
		t.Fatalf(
			"release calls after resume = %d, retry count = %d; want 2",
			releaseCalls,
			cr.Status.LifecycleOperation.RetryCount,
		)
	}

	cr.Status.Members[2].Status = "Up"
	cr.Status.Members[2].ActiveHistoricalSearchCount = 0
	cr.Status.Members[2].ActiveRealtimeSearchCount = 0
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
	if cr.Status.LifecycleOperation.ActiveHistoricalSearches != 0 ||
		cr.Status.LifecycleOperation.ActiveRealtimeSearches != 0 {
		t.Fatalf(
			"completed adapter recovery retained stale search counts: historical %d realtime %d",
			cr.Status.LifecycleOperation.ActiveHistoricalSearches,
			cr.Status.LifecycleOperation.ActiveRealtimeSearches,
		)
	}
}

func TestLifecycleAdapterRetriesUnknownDetentionOutcome(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)

	now := time.Date(2026, 7, 24, 13, 0, 0, 0, time.UTC)
	oldNow := searchHeadClusterLifecycleNow
	oldGetMembers := getSearchHeadCaptainMembers
	oldRequestDetention := requestSearchHeadDetention
	t.Cleanup(func() {
		searchHeadClusterLifecycleNow = oldNow
		getSearchHeadCaptainMembers = oldGetMembers
		requestSearchHeadDetention = oldRequestDetention
	})
	searchHeadClusterLifecycleNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}

	ordinal := int32(2)
	targetPod := "splunk-example-search-head-2"
	operation := shcworkflow.StartReplacement(
		"operation-1",
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		"revision-2",
		targetPod,
		ordinal,
		now,
	)
	operation.Stage = enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget
	operation.TargetPodUID = "original-pod-uid"
	stageStartedAt := metav1.NewTime(now)
	operation.StageStartedAt = &stageStartedAt

	cr := &enterpriseApi.SearchHeadCluster{}
	cr.Name = "example"
	cr.Status.Initialized = true
	cr.Status.MinPeersJoined = true
	cr.Status.CaptainReady = true
	cr.Status.Captain = "splunk-example-search-head-0"
	cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
		{Name: "splunk-example-search-head-0", Status: "Up", Registered: true},
		{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
		{Name: targetPod, Status: "Up", Registered: true},
	}
	cr.Status.LifecycleOperation = operation
	mgr := &searchHeadClusterPodManager{
		cr: cr,
		statefulSet: &appsv1.StatefulSet{
			Status: appsv1.StatefulSetStatus{UpdateRevision: "revision-2"},
		},
	}

	getSearchHeadCaptainMembers = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
		return map[string]splclient.SearchHeadCaptainMemberInfo{
			"splunk-example-search-head-0": {
				Identifier:    "member-guid-0",
				Label:         "splunk-example-search-head-0",
				Status:        "Up",
				Captain:       true,
				ManagementURI: "https://splunk-example-search-head-0:8089",
			},
			targetPod: {
				Identifier:    "member-guid-2",
				Label:         targetPod,
				Status:        "Up",
				ManagementURI: "https://splunk-example-search-head-2:8089",
			},
		}, nil
	}

	detentionCalls := 0
	requestSearchHeadDetention = func(context.Context, *searchHeadClusterPodManager, int32) error {
		detentionCalls++
		if detentionCalls == 1 {
			return &url.Error{
				Op:  "Post",
				URL: "https://splunk-example-search-head-2:8089/detention",
				Err: context.DeadlineExceeded,
			}
		}
		return nil
	}

	ready, err := mgr.prepareLifecycleReplacement(
		context.Background(),
		ordinal,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if detentionCalls != 1 ||
		cr.Status.LifecycleOperation.DetentionRequestedAt == nil ||
		cr.Status.LifecycleOperation.DetentionRequestAttemptCount != 1 {
		t.Fatalf(
			"first uncertain attempt calls=%d status=%#v",
			detentionCalls,
			cr.Status.LifecycleOperation,
		)
	}

	ready, err = mgr.prepareLifecycleReplacement(
		context.Background(),
		ordinal,
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if detentionCalls != 2 ||
		cr.Status.LifecycleOperation.DetentionRequestAttemptCount != 2 {
		t.Fatalf(
			"retry calls=%d attemptCount=%d, want 2",
			detentionCalls,
			cr.Status.LifecycleOperation.DetentionRequestAttemptCount,
		)
	}
	if cr.Status.LifecycleOperation.Stage !=
		enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget {
		t.Fatalf(
			"stage = %q, want DetainingTarget",
			cr.Status.LifecycleOperation.Stage,
		)
	}
}

func TestDetentionOutcomeUnknownClassification(t *testing.T) {
	timeout := &url.Error{
		Op:  "Post",
		URL: "https://splunk-example-search-head-2:8089/detention",
		Err: context.DeadlineExceeded,
	}
	if !detentionOutcomeUnknown(timeout) {
		t.Fatal("transport timeout must have unknown detention outcome")
	}
	if detentionOutcomeUnknown(errors.New("known response failure")) {
		t.Fatal("non-transport failure must remain an adapter error")
	}
	if detentionOutcomeUnknown(nil) {
		t.Fatal("nil error cannot have an unknown outcome")
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

func TestScaleDownMembershipRemovalHasDurableBarrier(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	now := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	target := int32(2)
	cr := &enterpriseApi.SearchHeadCluster{
		Status: enterpriseApi.SearchHeadClusterStatus{
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				OperationID:   "scale-down:search-head-2",
				Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
				TargetPod:     "splunk-example-search-head-2",
				TargetOrdinal: &target,
				Stage: enterpriseApi.
					SearchHeadClusterLifecycleStageAuthorizingReplacement,
			},
		},
	}
	mgr := &searchHeadClusterPodManager{cr: cr}

	oldNow := searchHeadClusterLifecycleNow
	oldRemoveMember := removeSearchHeadClusterMember
	t.Cleanup(func() {
		searchHeadClusterLifecycleNow = oldNow
		removeSearchHeadClusterMember = oldRemoveMember
	})
	searchHeadClusterLifecycleNow = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	removeCalls := 0
	removeSearchHeadClusterMember = func(
		context.Context,
		*searchHeadClusterPodManager,
		int32,
	) error {
		removeCalls++
		return nil
	}

	ready, err := mgr.requestScaleDownMembershipRemoval(
		context.Background(),
		target,
	)
	assertLifecycleAdapterResult(t, ready, err, false)
	if removeCalls != 1 ||
		cr.Status.LifecycleOperation.MembershipRemovalRequestedAt == nil {
		t.Fatalf(
			"first removal calls = %d, requestedAt = %v",
			removeCalls,
			cr.Status.LifecycleOperation.MembershipRemovalRequestedAt,
		)
	}

	ready, err = mgr.requestScaleDownMembershipRemoval(
		context.Background(),
		target,
	)
	assertLifecycleAdapterResult(t, ready, err, true)
	if removeCalls != 1 {
		t.Fatalf("removal calls after durable resume = %d, want 1", removeCalls)
	}
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

func stubReadySearchHeadKVStore(t *testing.T) {
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
}

func observeWaitingLifecyclePod(
	t *testing.T,
	reason string,
) shcworkflow.RecoveryObservation {
	return observeLifecyclePodWithContainerStatus(
		t,
		corev1.ContainerStatus{
			Name: "splunk",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  reason,
					Message: "container startup failed",
				},
			},
		},
	)
}

func observeLifecyclePodWithContainerStatus(
	t *testing.T,
	containerStatus corev1.ContainerStatus,
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
					containerStatus,
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
