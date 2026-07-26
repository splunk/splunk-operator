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
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
