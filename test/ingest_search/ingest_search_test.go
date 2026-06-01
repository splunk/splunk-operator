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
package ingestsearch

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Ingest and Search test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	Context("Standalone deployment (S1)", func() {
		It("can search internal logs for standalone instance", Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:ingestsearch"), NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			RunS1InternalLogSearchTest(ctx, deployment, testcaseEnvInst)
		})

		It("can ingest custom data to new index and search", Label("tier:e2e-full", "sva:s1", "cloud:aws", "feature:ingestsearch"), NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			RunS1IngestAndSearchTest(ctx, deployment, testcaseEnvInst)
		})
	})
})
