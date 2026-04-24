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
package secret

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

// masterManagerConfigs defines the V3 (master) and V4 (manager) variants
// shared by the C3 and M4 secret test tables.
var masterManagerConfigs = []testenv.MasterManagerTestConfig{
	{NamePrefix: "master", Label: "mastersecret", NewConfig: testenv.NewClusterReadinessConfigV3},
	{NamePrefix: "", Label: "managersecret", NewConfig: testenv.NewClusterReadinessConfigV4},
}

var _ = Describe("Secret test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	// S1 tests — V3/V4 distinction is irrelevant for standalone secret tests (always V4)
	Context("Standalone deployment (S1) with LM and MC", func() {
		BeforeEach(func() {
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
		})

		AfterEach(func() {
			Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("managersecret, integration, s1: Secret update on a standalone instance with LM and MC", func() {
			config := testenv.NewClusterReadinessConfigV4()
			RunS1SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
		})

		It("managersecret, integration, s1: Secret Object is recreated on delete and new secrets are applied to Splunk Pods", func() {
			config := testenv.NewClusterReadinessConfigV4()
			RunS1SecretDeleteTest(ctx, deployment, testcaseEnvInst, config)
		})

		It("managersecret, smoke, s1: Secret Object data is repopulated in secret object on passing empty Data map and new secrets are applied to Splunk Pods", func() {
			config := testenv.NewClusterReadinessConfigV4()
			RunS1SecretDeleteWithMCRefTest(ctx, deployment, testcaseEnvInst, config)
		})
	})

	// C3 tests — V3 (master) and V4 (manager) variants
	for _, tc := range masterManagerConfigs {
		tc := tc
		Context("Clustered deployment (C3 - Clustered Indexer, Search Head Cluster)", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", smoke, c3: secret update on Indexers and Search Head Cluster", func() {
				config := tc.NewConfig()
				RunC3SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}

	// M4 tests — V3 (master) and V4 (manager) variants
	for _, tc := range masterManagerConfigs {
		tc := tc
		Context("Multisite cluster deployment (M4 - Multisite Indexer Cluster, Search Head Cluster)", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix, testenv.WithTimeout(40000))
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", integration, m4: secret update on multisite Indexers and Search Head Cluster", func() {
				config := tc.NewConfig()
				RunM4SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}
})
