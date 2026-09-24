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
package shc_detention

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("SHC Detention Timeout", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(),
			"Failed to teardown test case environment")
	})

	// Scenario 3: No regression: rolling update completes without timeout when no active searches
	It("shcdetention, tier:e2e-full, sva:shc: rolling update completes without timeout when no active searches",
		Label("tier:e2e-full", "sva:shc", "cloud:aws", "variant:manager", "feature:detention"),
		NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

			shc, err := deployment.DeploySearchHeadCluster(ctx, deployment.GetName(), "", "", "")
			Expect(err).To(Succeed(), "Failed to deploy SHC")

			err = testcaseEnvInst.WatchForSearchHeadClusterPhase(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				enterpriseApi.PhaseReady, testenv.DefaultTimeout)
			Expect(err).To(Succeed(), "SHC did not reach Ready phase")

			// Trigger rolling update
			shc.Spec.Image = testenvInstance.GetSplunkUpgradeImage()
			Expect(deployment.UpdateCR(ctx, shc)).To(Succeed(), "Failed to patch SHC image")

			// Wait for the operator to observe the spec change before asserting Ready.
			// Without this, WatchForSearchHeadClusterPhase can return immediately using
			// the pre-update Ready status before the rolling update has started.
			Expect(testcaseEnvInst.WatchForSHCObservedGeneration(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				shc.Generation+1, testenv.DefaultTimeout)).To(Succeed(),
				"SHC operator did not observe spec update")

			// Assert SHC returns to Ready — all members drained naturally
			err = testcaseEnvInst.WatchForSearchHeadClusterPhase(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				enterpriseApi.PhaseReady, testenv.DefaultTimeout)
			Expect(err).To(Succeed(), "SHC did not return to Ready after rolling update")

			// Assert no DetentionTimeoutForced events were emitted
			err = testcaseEnvInst.WatchForEventWithReason(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				"DetentionTimeoutForced", 10*time.Second)
			Expect(err).NotTo(Succeed(), "Expected no DetentionTimeoutForced events on normal drain")
		})

	// Scenario 2: Forced timeout: member is recycled when searches never drain
	It("shcdetention, tier:e2e-full, sva:shc: forced timeout recycles member when searches never drain",
		Label("tier:e2e-full", "sva:shc", "cloud:aws", "variant:manager", "feature:detention"),
		NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

			// Deploy with short detention timeout so test completes in ~3 minutes
			shcSpec := enterpriseApi.SearchHeadClusterSpec{}
			shcSpec.Replicas = 3
			shcSpec.Image = testenvInstance.GetSplunkImage()
			shcSpec.DetentionTimeoutSeconds = 120
			shc, err := deployment.DeploySearchHeadClusterWithGivenSpec(ctx, deployment.GetName(), shcSpec)
			Expect(err).To(Succeed(), "Failed to deploy SHC")

			err = testcaseEnvInst.WatchForSearchHeadClusterPhase(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				enterpriseApi.PhaseReady, testenv.DefaultTimeout)
			Expect(err).To(Succeed(), "SHC did not reach Ready phase")

			// Start a never-ending real-time search on search-head-2 to block drain.
			// Pods recycle in descending order (2→1→0), so pod 2 enters detention first.
			// Targeting pod 0 would require waiting for pods 2 and 1 to recycle first
			// (~40s each), consuming 80s of the 3-minute timeout before detention even starts.
			podName := testenv.SearchHeadPodName(deployment.GetName(), 2)
			Expect(testenv.StartRealtimeSearch(ctx, deployment, podName)).To(Succeed(),
				"Failed to start real-time search on search-head-2")

			// Trigger rolling update
			shc.Spec.Image = testenvInstance.GetSplunkUpgradeImage()
			Expect(deployment.UpdateCR(ctx, shc)).To(Succeed(), "Failed to patch SHC image")

			// Wait for the operator to observe the spec change before asserting detention behavior.
			Expect(testcaseEnvInst.WatchForSHCObservedGeneration(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				shc.Generation+1, testenv.DefaultTimeout)).To(Succeed(),
				"SHC operator did not observe spec update")

			// Assert DetentionTimeoutForced event appears within timeout + overhead margin
			err = testcaseEnvInst.WatchForEventWithReason(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				"DetentionTimeoutForced", testenv.DetentionTimeoutEventBudget)
			Expect(err).To(Succeed(), "Expected DetentionTimeoutForced event within budget")

			// Assert rolling update completes and SHC recovers to Ready
			err = testcaseEnvInst.WatchForSearchHeadClusterPhase(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				enterpriseApi.PhaseReady, testenv.DefaultTimeout)
			Expect(err).To(Succeed(), "SHC did not recover to Ready after forced timeout recycle")
		})

	// Scenario 1: Normal drain: rolling update completes when bounded searches drain before timeout
	It("shcdetention, tier:e2e-full, sva:shc: rolling update completes when searches drain before timeout",
		Label("tier:e2e-full", "sva:shc", "cloud:aws", "variant:manager", "feature:detention"),
		NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

			shc, err := deployment.DeploySearchHeadCluster(ctx, deployment.GetName(), "", "", "")
			Expect(err).To(Succeed(), "Failed to deploy SHC")

			err = testcaseEnvInst.WatchForSearchHeadClusterPhase(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				enterpriseApi.PhaseReady, testenv.DefaultTimeout)
			Expect(err).To(Succeed(), "SHC did not reach Ready phase")

			// Start a bounded historical search on search-head-2 — the first pod to enter detention.
			// Pods recycle in descending order (2→1→0); targeting pod 2 ensures the search is
			// active when detention starts rather than having already completed during pods 2 and 1's
			// recycle cycles, which would make the drain assertion trivially true.
			podName := testenv.SearchHeadPodName(deployment.GetName(), 2)
			Expect(testenv.StartHistoricalSearch(ctx, deployment, podName)).To(Succeed(),
				"Failed to start historical search on search-head-2")

			// Trigger rolling update
			shc.Spec.Image = testenvInstance.GetSplunkUpgradeImage()
			Expect(deployment.UpdateCR(ctx, shc)).To(Succeed(), "Failed to patch SHC image")

			// Wait for the operator to observe the spec change before asserting Ready.
			Expect(testcaseEnvInst.WatchForSHCObservedGeneration(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				shc.Generation+1, testenv.DefaultTimeout)).To(Succeed(),
				"SHC operator did not observe spec update")

			// Assert SHC returns to Ready — searches drained naturally before timeout
			err = testcaseEnvInst.WatchForSearchHeadClusterPhase(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				enterpriseApi.PhaseReady, testenv.DefaultTimeout)
			Expect(err).To(Succeed(), "SHC did not return to Ready after rolling update")

			// Assert no forced timeout events
			err = testcaseEnvInst.WatchForEventWithReason(ctx, deployment,
				testcaseEnvInst.GetName(), deployment.GetName(),
				"DetentionTimeoutForced", 10*time.Second)
			Expect(err).NotTo(Succeed(), "Expected no DetentionTimeoutForced events when searches drain naturally")
		})
})
