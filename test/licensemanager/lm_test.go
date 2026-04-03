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
var masterManagerLMConfigs = []struct {
	NamePrefix string
	Label      string
	NewConfig  func() *testenv.LicenseTestConfig
}{
	{"master", "licensemaster", testenv.NewLicenseMasterConfig},
	{"", "licensemanager", testenv.NewLicenseManagerConfig},
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

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", smoke, s1: Splunk Operator can configure LM with Standalone in S1 SVA", func() {
				RunLMS1Test(ctx, deployment, testcaseEnvInst, tc.NewConfig())
			})
		})

		Context("Clustered deployment (C3) with "+tc.Label, func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", integration, c3: Splunk Operator can configure LM with Indexers and Search Heads in C3 SVA", func() {
				RunLMC3Test(ctx, deployment, testcaseEnvInst, tc.NewConfig())
			})
		})

		Context("Multisite cluster deployment (M4) with "+tc.Label, func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", integration, m4: Splunk Operator can configure LM with Indexers and Search Heads in M4 SVA", func() {
				RunLMM4Test(ctx, deployment, testcaseEnvInst, tc.NewConfig())
			})
		})
	}
})
