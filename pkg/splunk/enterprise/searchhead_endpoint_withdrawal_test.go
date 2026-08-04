// Copyright (c) 2026 Splunk Inc. All rights reserved.

package enterprise

import (
	"context"
	"testing"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestSearchHeadEndpointWithdrawalPersistsDelayAndInvalidation(t *testing.T) {
	mgr, endpointSlice := searchHeadEndpointWithdrawalFixture(t)
	base := time.Date(2026, 8, 4, 13, 40, 32, 0, time.UTC)
	currentTime := base
	oldNow := searchHeadEndpointWithdrawalNow
	searchHeadEndpointWithdrawalNow = func() time.Time { return currentTime }
	t.Cleanup(func() { searchHeadEndpointWithdrawalNow = oldNow })

	elapsed, err := mgr.ensureSearchHeadEndpointWithdrawalBarrier(
		context.Background(),
		0,
		30*time.Second,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	operation := mgr.cr.Status.LifecycleOperation
	require.NotNil(t, operation.EndpointWithdrawalObservedAt)
	require.NotNil(t, operation.EndpointWithdrawalDeadline)
	require.Equal(t, base, operation.EndpointWithdrawalObservedAt.Time)
	require.Equal(t, base.Add(30*time.Second), operation.EndpointWithdrawalDeadline.Time)
	require.Equal(t, operation.TargetPodUID, operation.EndpointWithdrawalPodUID)
	require.Equal(t, int64(1), operation.EndpointWithdrawalSequence)
	require.Equal(
		t,
		enterpriseApi.SearchHeadClusterLifecycleReasonEndpointWithdrawalObserved,
		operation.Reason,
	)

	currentTime = base.Add(5 * time.Second)
	elapsed, err = mgr.ensureSearchHeadEndpointWithdrawalBarrier(
		context.Background(),
		0,
		time.Second,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	require.Equal(t, base.Add(30*time.Second), operation.EndpointWithdrawalDeadline.Time)
	require.Equal(
		t,
		enterpriseApi.SearchHeadClusterLifecycleReasonEndpointWithdrawalPending,
		operation.Reason,
	)

	ready := true
	endpointSlice.Endpoints[0].Conditions.Ready = &ready
	require.NoError(t, mgr.c.Update(context.Background(), endpointSlice))
	elapsed, err = mgr.ensureSearchHeadEndpointWithdrawalBarrier(
		context.Background(),
		0,
		30*time.Second,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	require.Equal(t, int64(1), operation.EndpointWithdrawalInvalidatedSequence)
	require.Equal(
		t,
		enterpriseApi.SearchHeadClusterLifecycleReasonEndpointWithdrawalInvalidated,
		operation.Reason,
	)

	ready = false
	endpointSlice.Endpoints[0].Conditions.Ready = &ready
	require.NoError(t, mgr.c.Update(context.Background(), endpointSlice))
	elapsed, err = mgr.ensureSearchHeadEndpointWithdrawalBarrier(
		context.Background(),
		0,
		30*time.Second,
	)
	require.NoError(t, err)
	require.False(t, elapsed)
	require.Equal(t, int64(2), operation.EndpointWithdrawalSequence)
	require.Equal(t, base.Add(35*time.Second), operation.EndpointWithdrawalDeadline.Time)

	currentTime = base.Add(36 * time.Second)
	elapsed, err = mgr.ensureSearchHeadEndpointWithdrawalBarrier(
		context.Background(),
		0,
		30*time.Second,
	)
	require.NoError(t, err)
	require.True(t, elapsed)
}

func TestSearchHeadEndpointWithdrawalRejectsWrongTarget(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*enterpriseApi.SearchHeadClusterLifecycleOperationStatus)
	}{
		{
			name: "replacement UID",
			mutate: func(operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus) {
				operation.TargetPodUID = "replacement-uid"
			},
		},
		{
			name: "different ordinal",
			mutate: func(operation *enterpriseApi.SearchHeadClusterLifecycleOperationStatus) {
				ordinal := int32(1)
				operation.TargetOrdinal = &ordinal
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			mgr, _ := searchHeadEndpointWithdrawalFixture(t)
			testCase.mutate(mgr.cr.Status.LifecycleOperation)
			elapsed, err := mgr.ensureSearchHeadEndpointWithdrawalBarrier(
				context.Background(),
				0,
				30*time.Second,
			)
			require.Error(t, err)
			require.False(t, elapsed)
		})
	}
}

func TestSearchHeadEndpointWithdrawalPrecedesDetentionForReplacementIntents(
	t *testing.T,
) {
	for _, intent := range []enterpriseApi.SearchHeadClusterLifecycleIntent{
		enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
		enterpriseApi.SearchHeadClusterLifecycleIntentScaleDown,
	} {
		t.Run(string(intent), func(t *testing.T) {
			setLifecyclePolicyTestGates(t, true, true)
			mgr, _ := searchHeadEndpointWithdrawalFixture(t)
			mgr.cr.Status.Initialized = true
			mgr.cr.Status.MinPeersJoined = true
			mgr.cr.Status.CaptainReady = true
			mgr.cr.Status.Captain = "splunk-example-search-head-1"
			mgr.cr.Status.Members = []enterpriseApi.SearchHeadClusterMemberStatus{
				{Name: "splunk-example-search-head-0", Status: "Up", Registered: true},
				{Name: "splunk-example-search-head-1", Status: "Up", Registered: true},
				{Name: "splunk-example-search-head-2", Status: "Up", Registered: true},
			}
			delaySeconds := int64(30)
			mgr.cr.Spec.LifecyclePolicy = &enterpriseApi.SearchHeadClusterLifecyclePolicy{
				EndpointWithdrawalDelaySeconds: &delaySeconds,
			}
			mgr.statefulSet = &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{ReadinessGates: []corev1.PodReadinessGate{{
						ConditionType: searchHeadServingCondition,
					}}},
				}},
				Status: appsv1.StatefulSetStatus{UpdateRevision: "revision-2"},
			}
			operation := mgr.cr.Status.LifecycleOperation
			operation.Intent = intent
			if intent == enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate {
				operation.DesiredRevision = "revision-2"
			}

			oldGetMembers := getSearchHeadCaptainMembers
			oldRequestDetention := requestSearchHeadDetention
			oldLifecycleNow := searchHeadClusterLifecycleNow
			oldWithdrawalNow := searchHeadEndpointWithdrawalNow
			t.Cleanup(func() {
				getSearchHeadCaptainMembers = oldGetMembers
				requestSearchHeadDetention = oldRequestDetention
				searchHeadClusterLifecycleNow = oldLifecycleNow
				searchHeadEndpointWithdrawalNow = oldWithdrawalNow
			})
			getSearchHeadCaptainMembers = func(
				context.Context,
				*searchHeadClusterPodManager,
				int32,
			) (map[string]splclient.SearchHeadCaptainMemberInfo, error) {
				return map[string]splclient.SearchHeadCaptainMemberInfo{
					"splunk-example-search-head-0": {
						Identifier:    "member-0",
						Label:         "splunk-example-search-head-0",
						Status:        "Up",
						ManagementURI: "https://splunk-example-search-head-0:8089",
					},
					"splunk-example-search-head-1": {
						Identifier:    "member-1",
						Label:         "splunk-example-search-head-1",
						Status:        "Up",
						Captain:       true,
						ManagementURI: "https://splunk-example-search-head-1:8089",
					},
					"splunk-example-search-head-2": {
						Identifier:    "member-2",
						Label:         "splunk-example-search-head-2",
						Status:        "Up",
						ManagementURI: "https://splunk-example-search-head-2:8089",
					},
				}, nil
			}
			detentionCalls := 0
			requestSearchHeadDetention = func(
				context.Context,
				*searchHeadClusterPodManager,
				int32,
			) error {
				detentionCalls++
				return nil
			}
			base := time.Date(2026, 8, 4, 13, 40, 32, 0, time.UTC)
			currentTime := base
			searchHeadClusterLifecycleNow = func() time.Time { return currentTime }
			searchHeadEndpointWithdrawalNow = func() time.Time { return currentTime }

			ready, err := mgr.prepareLifecycleReplacement(
				context.Background(),
				0,
				intent,
			)
			require.NoError(t, err)
			require.False(t, ready)
			require.Zero(t, detentionCalls)
			require.NotNil(
				t,
				mgr.cr.Status.LifecycleOperation.EndpointWithdrawalDeadline,
			)

			currentTime = base.Add(29 * time.Second)
			ready, err = mgr.prepareLifecycleReplacement(
				context.Background(),
				0,
				intent,
			)
			require.NoError(t, err)
			require.False(t, ready)
			require.Zero(t, detentionCalls)

			currentTime = base.Add(30 * time.Second)
			ready, err = mgr.prepareLifecycleReplacement(
				context.Background(),
				0,
				intent,
			)
			require.NoError(t, err)
			require.False(t, ready)
			require.Equal(t, 1, detentionCalls)
		})
	}
}

func searchHeadEndpointWithdrawalFixture(
	t *testing.T,
) (*searchHeadClusterPodManager, *discoveryv1.EndpointSlice) {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	const namespace = "test"
	const name = "example"
	ordinal := int32(0)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-example-search-head-0",
			Namespace: namespace,
			UID:       types.UID("target-uid"),
		},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
			{Type: searchHeadServingCondition, Status: corev1.ConditionFalse},
			{Type: corev1.PodReady, Status: corev1.ConditionFalse},
		}},
	}
	ready := false
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-example-search-head",
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: splcommon.GetSplunkServiceName(
					SplunkSearchHead,
					name,
					false,
				),
			},
		},
		Endpoints: []discoveryv1.Endpoint{{
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			TargetRef: &corev1.ObjectReference{
				Kind: "Pod",
				Name: pod.Name,
				UID:  pod.UID,
			},
		}},
	}
	cr := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Status: enterpriseApi.SearchHeadClusterStatus{
			LifecycleOperation: &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
				OperationID:   "operation",
				Intent:        enterpriseApi.SearchHeadClusterLifecycleIntentPodUpdate,
				TargetPod:     pod.Name,
				TargetOrdinal: &ordinal,
				TargetPodUID:  string(pod.UID),
				Stage:         enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget,
			},
		},
	}
	k8sClient := newFakeClientBuilder(scheme).
		WithObjects(pod, endpointSlice).
		Build()
	return &searchHeadClusterPodManager{
		c:                       k8sClient,
		cr:                      cr,
		servingConditionChanged: map[int32]bool{},
	}, endpointSlice
}
