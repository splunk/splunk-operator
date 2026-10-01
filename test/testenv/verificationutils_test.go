// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testenv

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

// shcFixture returns a fully-converged SearchHeadCluster for tests to perturb one field at a time.
func shcFixture(replicas int32) *enterpriseApi.SearchHeadCluster {
	shc := &enterpriseApi.SearchHeadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-shc", Generation: 2},
	}
	shc.Spec.Replicas = replicas
	shc.Status.ObservedGeneration = 2
	shc.Status.Replicas = replicas
	shc.Status.ReadyReplicas = replicas
	shc.Status.DeployerPhase = enterpriseApi.PhaseReady
	shc.Status.CaptainReady = true
	shc.Status.Phase = enterpriseApi.PhaseUpdating
	return shc
}

func TestIsSHCMemberJoinStalled(t *testing.T) {
	const replicas int32 = 4

	tests := []struct {
		name    string
		mutate  func(*enterpriseApi.SearchHeadCluster)
		stalled bool
	}{
		{
			name:    "all ready except phase is the stall",
			mutate:  func(*enterpriseApi.SearchHeadCluster) {},
			stalled: true,
		},
		{
			name:    "phase Ready is convergence, not a stall",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Status.Phase = enterpriseApi.PhaseReady },
			stalled: false,
		},
		{
			name:    "pods still becoming ready is ordinary progress",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Status.ReadyReplicas = replicas - 1 },
			stalled: false,
		},
		{
			name:    "statefulset not yet scaled is ordinary progress",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Status.Replicas = replicas - 1 },
			stalled: false,
		},
		{
			name:    "operator has not observed the spec yet",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Status.ObservedGeneration = 1 },
			stalled: false,
		},
		{
			name:    "captain not yet elected is ordinary progress",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Status.CaptainReady = false },
			stalled: false,
		},
		{
			name:    "deployer not ready is ordinary progress",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Status.DeployerPhase = enterpriseApi.PhaseUpdating },
			stalled: false,
		},
		{
			name:    "spec replicas not yet the requested count",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Spec.Replicas = replicas - 1 },
			stalled: false,
		},
		{
			name:    "observedGeneration ahead of generation still counts as observed",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Status.ObservedGeneration = 3 },
			stalled: true,
		},
		{
			name:    "error phase with everything else ready is a controller error, not a stall",
			mutate:  func(s *enterpriseApi.SearchHeadCluster) { s.Status.Phase = enterpriseApi.PhaseError },
			stalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shc := shcFixture(replicas)
			tt.mutate(shc)
			if got := isSHCMemberJoinStalled(shc, replicas); got != tt.stalled {
				t.Errorf("isSHCMemberJoinStalled() = %v, want %v", got, tt.stalled)
			}
		})
	}
}

// TestSHCMemberJoinStallTimeoutBounds pins the threshold between the benign and pathological windows.
func TestSHCMemberJoinStallTimeoutBounds(t *testing.T) {
	const widestBenignWindowSeconds = 54

	if SHCMemberJoinStallTimeout.Seconds() < 5*widestBenignWindowSeconds {
		t.Errorf("SHCMemberJoinStallTimeout %v leaves too little margin over the widest benign window (%ds); false failures likely",
			SHCMemberJoinStallTimeout, widestBenignWindowSeconds)
	}
	if SHCMemberJoinStallTimeout >= MediumTimeout {
		t.Errorf("SHCMemberJoinStallTimeout %v is not meaningfully shorter than the smallest spec NodeTimeout that uses it (%v); it would never fire before the node timeout",
			SHCMemberJoinStallTimeout, MediumTimeout)
	}
}
