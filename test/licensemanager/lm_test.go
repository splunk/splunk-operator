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
package licensemanager

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

// masterManagerLMConfigs defines the V3 (master) and V4 (manager) variants
// for the license manager tests.
var masterManagerLMConfigs = []testenv.MasterManagerLMTestConfig{
	{NamePrefix: "master", Label: "master", NewConfig: testenv.NewLicenseMasterConfig},
	{NamePrefix: "", Label: "manager", NewConfig: testenv.NewLicenseManagerConfig},
}

var _ = Describe("License Manager test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	for _, tc := range masterManagerLMConfigs {
		tc := tc

		Context("Standalone deployment (S1) with "+tc.Label, func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It("Splunk Operator can configure LM with Standalone in S1 SVA", Label("tier:e2e-pr", "sva:s1", "cloud:aws", "variant:"+tc.Label, "feature:licensemanager"), func() {
				RunLMS1Test(ctx, deployment, testcaseEnvInst, tc.NewConfig())
			})
		})

		Context("Clustered deployment (C3) with "+tc.Label, func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It("Splunk Operator can configure LM with Indexers and Search Heads in C3 SVA", Label("tier:e2e-full", "sva:c3", "cloud:aws", "variant:"+tc.Label, "feature:licensemanager"), func() {
				RunLMC3Test(ctx, deployment, testcaseEnvInst, tc.NewConfig())
			})
		})

		Context("Multisite cluster deployment (M4) with "+tc.Label, func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It("Splunk Operator can configure LM with Indexers and Search Heads in M4 SVA", Label("tier:e2e-full", "sva:m4", "cloud:aws", "variant:"+tc.Label, "feature:licensemanager"), func() {
				RunLMM4Test(ctx, deployment, testcaseEnvInst, tc.NewConfig())
			})
		})

		Context("Clustered deployment (C3) with "+tc.Label+" App Framework", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It("Splunk Operator can configure a C3 SVA and have apps installed locally on LM", Label("tier:e2e-full", "sva:c3", "cloud:aws", "variant:"+tc.Label, "feature:licensemanager"), func() {
				RunLMC3AppFrameworkTest(ctx, deployment, testcaseEnvInst, testenvInstance, tc.NewConfig())
			})
		})
	}
})
