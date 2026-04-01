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

	// S1 tests — both label variants use identical V4 config
	s1SecretLabels := []string{"mastersecret", "managersecret"}

	for _, label := range s1SecretLabels {
		label := label
		Context("Standalone deployment (S1) with LM and MC", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed())
			})

			It(label+", integration, s1: Secret update on a standalone instance with LM and MC", func() {
				config := testenv.NewClusterReadinessConfigV4()
				RunS1SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
			})

			It(label+", integration, s1: Secret Object is recreated on delete and new secrets are applied to Splunk Pods", func() {
				config := testenv.NewClusterReadinessConfigV4()
				RunS1SecretDeleteTest(ctx, deployment, testcaseEnvInst, config)
			})

			It(label+", smoke, s1: Secret Object data is repopulated in secret object on passing empty Data map and new secrets are applied to Splunk Pods", func() {
				config := testenv.NewClusterReadinessConfigV4()
				RunS1SecretDeleteWithMCRefTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}

	// C3 tests — V3 (master) and V4 (manager) variants
	for _, tc := range masterManagerConfigs {
		tc := tc
		Context("Clustered deployment (C3 - Clustered Indexer, Search Head Cluster)", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed())
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
				testenv.SpecifiedTestTimeout = 40000
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed())
			})

			It(tc.Label+", integration, m4: secret update on multisite Indexers and Search Head Cluster", func() {
				config := tc.NewConfig()
				RunM4SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}
})
