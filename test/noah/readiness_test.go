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
		// Readiness is meaningful for this workflow only when the C3 is wired to
		// Kraken's external S3/SQS resources and no chart MinIO workload is active.
		It(
			"reports generation-current readiness for every indexer and search head",
			NodeTimeout(testenv.MediumLongTimeout),
			func(ctx SpecContext) {
				_, err := checkKrakenExternalStorage(ctx)
				Expect(err).NotTo(HaveOccurred(), "Kraken external storage is not wired as expected")
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
