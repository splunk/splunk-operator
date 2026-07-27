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
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestSearchHeadServingReadinessGateWiring(t *testing.T) {
	setLifecyclePolicyTestGates(t, true, true)
	spec := &corev1.PodSpec{}
	applySearchHeadServingReadinessGate(spec, SplunkSearchHead)
	require.Equal(t, []corev1.PodReadinessGate{
		{ConditionType: searchHeadServingCondition},
	}, spec.ReadinessGates)
	applySearchHeadServingReadinessGate(spec, SplunkSearchHead)
	require.Len(t, spec.ReadinessGates, 1)

	nonSHC := &corev1.PodSpec{}
	applySearchHeadServingReadinessGate(nonSHC, SplunkDeployer)
	require.Empty(t, nonSHC.ReadinessGates)
}

func TestDesiredSearchHeadServingCondition(t *testing.T) {
	ordinal := int32(0)
	mgr := &searchHeadClusterPodManager{
		cr: &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				Initialized:    true,
				MinPeersJoined: true,
				CaptainReady:   true,
				Members: []enterpriseApi.SearchHeadClusterMemberStatus{{
					Name:       "splunk-example-search-head-0",
					Status:     "Up",
					Registered: true,
				}},
			},
		},
	}
	pod := &corev1.Pod{Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{
		Type:   corev1.ContainersReady,
		Status: corev1.ConditionTrue,
	}}}}

	status, reason, _ := mgr.desiredSearchHeadServingCondition(pod, ordinal)
	require.Equal(t, corev1.ConditionTrue, status)
	require.Equal(t, "MemberServing", reason)

	mgr.cr.Status.Members[0].Status = "ManualDetention"
	status, reason, _ = mgr.desiredSearchHeadServingCondition(pod, ordinal)
	require.Equal(t, corev1.ConditionFalse, status)
	require.Equal(t, "MemberNotUp", reason)

	mgr.cr.Status.Members[0].Status = "Up"
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		TargetOrdinal: &ordinal,
		Stage:         enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget,
	}
	status, reason, _ = mgr.desiredSearchHeadServingCondition(pod, ordinal)
	require.Equal(t, corev1.ConditionFalse, status)
	require.Equal(t, "LifecycleOperationActive", reason)

	peerOrdinal := int32(1)
	mgr.cr.Status.Members = append(
		mgr.cr.Status.Members,
		enterpriseApi.SearchHeadClusterMemberStatus{
			Name:       "splunk-example-search-head-1",
			Status:     "Up",
			Registered: true,
		},
	)
	mgr.cr.Status.CaptainReady = false
	status, reason, _ = mgr.desiredSearchHeadServingCondition(pod, peerOrdinal)
	require.Equal(t, corev1.ConditionTrue, status)
	require.Equal(t, "PeerServingDuringLifecycle", reason)

	mgr.cr.Status.LifecycleOperation = nil
	status, reason, _ = mgr.desiredSearchHeadServingCondition(pod, peerOrdinal)
	require.Equal(t, corev1.ConditionFalse, status)
	require.Equal(t, "ClusterNotReady", reason)

	mgr.cr.Status.CaptainReady = true
	mgr.cr.Status.LifecycleOperation = &enterpriseApi.SearchHeadClusterLifecycleOperationStatus{
		TargetOrdinal: &ordinal,
		Stage:         enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget,
	}
	mgr.cr.Status.LifecycleOperation.Stage =
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted
	status, _, _ = mgr.desiredSearchHeadServingCondition(pod, ordinal)
	require.Equal(t, corev1.ConditionTrue, status)

	now := metav1.Now()
	pod.DeletionTimestamp = &now
	status, reason, _ = mgr.desiredSearchHeadServingCondition(pod, ordinal)
	require.Equal(t, corev1.ConditionFalse, status)
	require.Equal(t, "PodTerminating", reason)
}

func TestEndpointSlicesRoutePod(t *testing.T) {
	ready := true
	notReady := false
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "splunk-example-search-head-0",
		UID:  types.UID("current-pod"),
	}}
	endpointFor := func(uid types.UID, ready *bool) discoveryv1.Endpoint {
		return discoveryv1.Endpoint{
			Conditions: discoveryv1.EndpointConditions{Ready: ready},
			TargetRef: &corev1.ObjectReference{
				Kind: "Pod",
				Name: pod.Name,
				UID:  uid,
			},
		}
	}

	require.True(t, endpointSlicesRoutePod([]discoveryv1.EndpointSlice{{
		Endpoints: []discoveryv1.Endpoint{endpointFor(pod.UID, &ready)},
	}}, pod))
	require.True(t, endpointSlicesRoutePod([]discoveryv1.EndpointSlice{{
		Endpoints: []discoveryv1.Endpoint{endpointFor(pod.UID, nil)},
	}}, pod), "unknown readiness must fail closed")
	require.False(t, endpointSlicesRoutePod([]discoveryv1.EndpointSlice{{
		Endpoints: []discoveryv1.Endpoint{endpointFor(pod.UID, &notReady)},
	}}, pod))
	require.False(t, endpointSlicesRoutePod([]discoveryv1.EndpointSlice{{
		Endpoints: []discoveryv1.Endpoint{endpointFor(types.UID("replaced-pod"), &ready)},
	}}, pod), "an endpoint for an old Pod UID must not block the replacement")
	require.False(t, endpointSlicesRoutePod(nil, pod))
}

func TestSearchHeadServingWithdrawalWaitsForClientServiceEndpointSlice(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	const namespace = "test"
	cr := &enterpriseApi.SearchHeadCluster{ObjectMeta: metav1.ObjectMeta{
		Name:      "example",
		Namespace: namespace,
	}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-example-search-head-0",
			Namespace: namespace,
			UID:       types.UID("current-pod"),
		},
		Status: corev1.PodStatus{Conditions: []corev1.PodCondition{
			{Type: searchHeadServingCondition, Status: corev1.ConditionFalse},
			{Type: corev1.PodReady, Status: corev1.ConditionFalse},
		}},
	}
	ready := true
	endpointSlice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-example-search-head",
			Namespace: namespace,
			Labels: map[string]string{
				discoveryv1.LabelServiceName: splcommon.GetSplunkServiceName(
					SplunkSearchHead,
					cr.Name,
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
	k8sClient := newFakeClientBuilder(scheme).
		WithObjects(pod, endpointSlice).
		Build()
	mgr := &searchHeadClusterPodManager{
		c:                       k8sClient,
		cr:                      cr,
		servingConditionChanged: map[int32]bool{},
	}

	withdrawn, err := mgr.searchHeadServingWithdrawalObserved(context.Background(), 0)
	require.NoError(t, err)
	require.False(t, withdrawn)

	ready = false
	endpointSlice.Endpoints[0].Conditions.Ready = &ready
	require.NoError(t, k8sClient.Update(context.Background(), endpointSlice))
	withdrawn, err = mgr.searchHeadServingWithdrawalObserved(context.Background(), 0)
	require.NoError(t, err)
	require.True(t, withdrawn)
}
