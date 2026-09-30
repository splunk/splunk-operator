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

package noah

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe(
	"Noah-backed C3 readiness",
	Serial,
	Label("tier:noah-e2e", "sva:c3", "cloud:kraken", "variant:noah", "feature:noah", "scenario:readiness"),
	func() {
		It(
			"reports generation-current readiness for every indexer and search head",
			NodeTimeout(testenv.MediumLongTimeout),
			func(ctx SpecContext) {
				topology := waitForC3Ready(ctx)
				Expect(topology.LicenseManagerPod).NotTo(BeEmpty())
				Expect(topology.IndexerPods).NotTo(BeEmpty())
				Expect(topology.SearchHeadPods).NotTo(BeEmpty())

				for _, podName := range topology.splunkPods() {
					By("checking that splunkd is running on " + podName)
					_, err := testenv.WaitForPodExecSuccess(
						ctx,
						deployment,
						podName,
						[]string{"/bin/sh"},
						`/opt/splunk/bin/splunk status | grep -q "splunkd is running"`,
						2*time.Minute,
					)
					Expect(err).To(Succeed(), "splunkd did not become ready on %s", podName)
				}
			},
		)
	},
)

func waitForC3Ready(ctx context.Context) *c3Topology {
	GinkgoHelper()

	readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	var topology *c3Topology
	Eventually(func() error {
		current, err := inspectC3Ready(
			readyCtx,
			testcaseEnvInstance.GetKubeClient(),
			operatorNamespace,
			clusterName,
			operatorName,
			noahDeployment,
		)
		if isTerminalReadinessError(err) {
			StopTrying("Noah C3 reported a terminal readiness condition").Wrap(err).Now()
		}
		if err == nil {
			topology = current
		}
		return err
	}).
		WithContext(readyCtx).
		WithTimeout(readyTimeout).
		WithPolling(testenv.PollInterval).
		Should(Succeed())
	return topology
}
