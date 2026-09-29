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

package searchheadcluster

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func readyFalse(phase enterpriseApi.Phase, message string) []metav1.Condition {
	return []metav1.Condition{{
		Type:    string(enterpriseApi.ConditionReady),
		Status:  metav1.ConditionFalse,
		Reason:  string(enterpriseApi.ReasonReplicasNotReady),
		Message: message,
	}}
}

func TestReadyCause(t *testing.T) {
	cases := []struct {
		name       string
		cr         *enterpriseApi.SearchHeadCluster
		wantOK     bool
		wantReason enterpriseApi.ConditionReason
	}{
		{
			name:   "no cause: everything healthy",
			cr:     &enterpriseApi.SearchHeadCluster{Status: enterpriseApi.SearchHeadClusterStatus{Captain: "sh-0", CaptainReady: true, MinPeersJoined: true, DeployerPhase: enterpriseApi.PhaseReady}},
			wantOK: false,
		},
		{
			name:       "no captain elected",
			cr:         &enterpriseApi.SearchHeadCluster{Status: enterpriseApi.SearchHeadClusterStatus{Captain: "", CaptainReady: false}},
			wantOK:     true,
			wantReason: enterpriseApi.ReasonNoCaptainElected,
		},
		{
			name:       "captain label present but not ready",
			cr:         &enterpriseApi.SearchHeadCluster{Status: enterpriseApi.SearchHeadClusterStatus{Captain: "sh-0", CaptainReady: false}},
			wantOK:     true,
			wantReason: enterpriseApi.ReasonNoCaptainElected,
		},
		{
			name:       "below minimum peers",
			cr:         &enterpriseApi.SearchHeadCluster{Status: enterpriseApi.SearchHeadClusterStatus{Captain: "sh-0", CaptainReady: true, MinPeersJoined: false}},
			wantOK:     true,
			wantReason: enterpriseApi.ReasonBelowMinimumPeers,
		},
		{
			name: "classic mode deployer not ready",
			cr: &enterpriseApi.SearchHeadCluster{
				Status: enterpriseApi.SearchHeadClusterStatus{Captain: "sh-0", CaptainReady: true, MinPeersJoined: true, DeployerPhase: enterpriseApi.PhasePending},
			},
			wantOK:     true,
			wantReason: enterpriseApi.ReasonDeployerNotReady,
		},
		{
			name: "noah mode: unresolved dependency wins over captain/peers",
			cr: &enterpriseApi.SearchHeadCluster{
				Spec:   enterpriseApi.SearchHeadClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				Status: enterpriseApi.SearchHeadClusterStatus{Captain: "", CaptainReady: false, Conditions: []metav1.Condition{{Type: string(enterpriseApi.ConditionNoahDependencyResolved), Status: metav1.ConditionFalse, Reason: string(enterpriseApi.ReasonNoahDependencyMissing), Message: "Noah cluster not found"}}},
			},
			wantOK:     true,
			wantReason: enterpriseApi.ReasonNoahDependencyMissing,
		},
		{
			name: "noah mode: deployer readiness is never a cause (no deployer exists)",
			cr: &enterpriseApi.SearchHeadCluster{
				Spec:   enterpriseApi.SearchHeadClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}},
				Status: enterpriseApi.SearchHeadClusterStatus{Captain: "sh-0", CaptainReady: true, MinPeersJoined: true, DeployerPhase: enterpriseApi.PhasePending},
			},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, message, ok := readyCause(tc.cr)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (reason=%q message=%q)", ok, tc.wantOK, reason, message)
			}
			if ok && reason != string(tc.wantReason) {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
			if ok && message == "" {
				t.Errorf("expected a non-empty message")
			}
		})
	}
}

func TestRefineReadyReason(t *testing.T) {
	t.Run("promotes cause when message is still generic", func(t *testing.T) {
		cr := &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				Phase:      enterpriseApi.PhasePending,
				Captain:    "",
				Conditions: readyFalse(enterpriseApi.PhasePending, "Resource is pending initialization"),
			},
		}
		refineReadyReason(cr)
		ready := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionReady)
		if ready == nil || ready.Reason != string(enterpriseApi.ReasonNoCaptainElected) {
			t.Fatalf("expected promoted reason %q, got %+v", enterpriseApi.ReasonNoCaptainElected, ready)
		}
	})

	t.Run("does not clobber an already-specific message", func(t *testing.T) {
		cr := &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				Phase:      enterpriseApi.PhasePending,
				Captain:    "",
				Conditions: readyFalse(enterpriseApi.PhasePending, "Noah dependency reconcile failed: boom"),
			},
		}
		refineReadyReason(cr)
		ready := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionReady)
		if ready.Reason != string(enterpriseApi.ReasonReplicasNotReady) {
			t.Errorf("expected the original reason left untouched, got %q", ready.Reason)
		}
	})

	t.Run("no-op when Ready is True", func(t *testing.T) {
		cr := &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				Phase: enterpriseApi.PhaseReady,
				Conditions: []metav1.Condition{{
					Type: string(enterpriseApi.ConditionReady), Status: metav1.ConditionTrue,
					Reason: string(enterpriseApi.ReasonAllReplicasReady), Message: "All replicas are ready",
				}},
			},
		}
		refineReadyReason(cr)
		ready := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionReady)
		if ready.Reason != string(enterpriseApi.ReasonAllReplicasReady) {
			t.Errorf("expected no change, got %q", ready.Reason)
		}
	})
}

func TestRefineProgressingReason(t *testing.T) {
	t.Run("promotes the member in an in-flight lifecycle operation", func(t *testing.T) {
		cr := &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				Members: []enterpriseApi.SearchHeadClusterMemberStatus{
					{Name: "sh-0"},
					{Name: "sh-1", CurrentOperation: enterpriseApi.MemberOperationDraining},
				},
				Conditions: []metav1.Condition{{
					Type: string(enterpriseApi.ConditionProgressing), Status: metav1.ConditionTrue,
					Reason: string(enterpriseApi.ReasonUpgrading), Message: "Resource is being updated",
				}},
			},
		}
		refineProgressingReason(cr)
		progressing := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionProgressing)
		if progressing == nil || progressing.Reason != string(enterpriseApi.ReasonMemberOperationInProgress) {
			t.Fatalf("expected promoted reason %q, got %+v", enterpriseApi.ReasonMemberOperationInProgress, progressing)
		}
		wantMessage := "sh-1 is Draining"
		if progressing.Message != wantMessage {
			t.Errorf("message = %q, want %q", progressing.Message, wantMessage)
		}
	})

	t.Run("promotes during PhasePending initialization, not just Updating/ScalingUp/ScalingDown", func(t *testing.T) {
		cr := &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				Members: []enterpriseApi.SearchHeadClusterMemberStatus{
					{Name: "sh-0", CurrentOperation: enterpriseApi.MemberOperationRecycling},
				},
				Conditions: []metav1.Condition{{
					Type: string(enterpriseApi.ConditionProgressing), Status: metav1.ConditionTrue,
					Reason: string(enterpriseApi.ReasonScaling), Message: "Resource is being initialized",
				}},
			},
		}
		refineProgressingReason(cr)
		progressing := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionProgressing)
		if progressing == nil || progressing.Reason != string(enterpriseApi.ReasonMemberOperationInProgress) {
			t.Fatalf("expected promoted reason %q, got %+v", enterpriseApi.ReasonMemberOperationInProgress, progressing)
		}
		wantMessage := "sh-0 is Recycling"
		if progressing.Message != wantMessage {
			t.Errorf("message = %q, want %q", progressing.Message, wantMessage)
		}
	})

	t.Run("no-op when no member has an in-flight operation", func(t *testing.T) {
		cr := &enterpriseApi.SearchHeadCluster{
			Status: enterpriseApi.SearchHeadClusterStatus{
				Members: []enterpriseApi.SearchHeadClusterMemberStatus{{Name: "sh-0"}},
				Conditions: []metav1.Condition{{
					Type: string(enterpriseApi.ConditionProgressing), Status: metav1.ConditionTrue,
					Reason: string(enterpriseApi.ReasonUpgrading), Message: "Resource is being updated",
				}},
			},
		}
		refineProgressingReason(cr)
		progressing := splcommon.GetCondition(cr.Status.Conditions, enterpriseApi.ConditionProgressing)
		if progressing.Reason != string(enterpriseApi.ReasonUpgrading) {
			t.Errorf("expected no change, got %q", progressing.Reason)
		}
	})
}
