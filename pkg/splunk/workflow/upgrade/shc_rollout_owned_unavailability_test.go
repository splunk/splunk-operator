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

package upgrade

import (
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
)

func TestSHCRolloutContinuesLifecycleAfterOwnedTargetWithdrawal(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Pods[2].Ready = false
	state.Lifecycle = lifecycleForOrdinal(
		2,
		enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget,
		false,
	)

	decision := EvaluateSHCRollout(state)

	assertSHCRolloutDecision(
		t,
		decision,
		SHCRolloutActionPrepareTarget,
		SHCRolloutReasonPrepareTarget,
		2,
	)
}

func TestSHCRolloutBlocksUnrelatedUnavailablePodDuringTargetWithdrawal(
	t *testing.T,
) {
	state := pendingSHCRolloutState()
	state.Pods[2].Ready = false
	state.Pods[1].Ready = false
	state.Lifecycle = lifecycleForOrdinal(
		2,
		enterpriseApi.SearchHeadClusterLifecycleStageDetainingTarget,
		false,
	)

	decision := EvaluateSHCRollout(state)

	assertSHCRolloutDecision(
		t,
		decision,
		SHCRolloutActionBlock,
		SHCRolloutReasonExistingUnavailablePod,
		1,
	)
}

func TestSHCRolloutDoesNotOwnUnavailablePodWithoutActiveLifecycle(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Pods[2].Ready = false
	state.Lifecycle = lifecycleForOrdinal(
		2,
		enterpriseApi.SearchHeadClusterLifecycleStageCompleted,
		true,
	)

	decision := EvaluateSHCRollout(state)

	assertSHCRolloutDecision(
		t,
		decision,
		SHCRolloutActionBlock,
		SHCRolloutReasonExistingUnavailablePod,
		2,
	)
}

func TestSHCRolloutReportsBlockedOwnedLifecycleInsteadOfAvailability(t *testing.T) {
	state := pendingSHCRolloutState()
	state.Pods[2].Ready = false
	state.Lifecycle = lifecycleForOrdinal(
		2,
		enterpriseApi.SearchHeadClusterLifecycleStageBlocked,
		false,
	)

	decision := EvaluateSHCRollout(state)

	assertSHCRolloutDecision(
		t,
		decision,
		SHCRolloutActionBlock,
		SHCRolloutReasonLifecycleBlocked,
		2,
	)
}
