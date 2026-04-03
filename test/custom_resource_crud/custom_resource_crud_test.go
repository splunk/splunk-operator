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
package crcrud

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

// masterManagerCrudConfigs defines the V3 (master) and V4 (manager) variants
// shared by the C3 and M4 CRUD test tables.
var masterManagerCrudConfigs = []testenv.MasterManagerTestConfig{
	{NamePrefix: "master", Label: "mastercrcrud", NewConfig: testenv.NewClusterReadinessConfigV3},
	{NamePrefix: "", Label: "managercrcrud", NewConfig: testenv.NewClusterReadinessConfigV4},
}

var _ = Describe("Custom Resource CRUD test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string
	var verificationTimeout time.Duration

	ctx := context.TODO()

	// S1 test — single variant (manager, V4)
	Context("Standalone deployment (S1)", func() {
		BeforeEach(func() {
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
			defaultCPULimits = DefaultCPULimits
			newCPULimits = UpdatedCPULimits
		})

		AfterEach(func() {
			Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("managercrcrud, integration, s1: can deploy a standalone instance, change its CR, update the instance", func() {
			RunS1CPUUpdateTest(ctx, deployment, testcaseEnvInst, defaultCPULimits, newCPULimits)
		})
	})

	// C3 tests — V3 (master) and V4 (manager) variants
	for _, tc := range masterManagerCrudConfigs {
		tc := tc
		Context("Clustered deployment (C3 - Clustered Indexer, Search Head Cluster)", func() {
			BeforeEach(func() {
				defaultCPULimits = DefaultCPULimits
				newCPULimits = UpdatedCPULimits
				verificationTimeout = DefaultVerificationTimeout
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", integration, c3: can deploy Indexer and Search Head Cluster, change their CR, update the instances", func() {
				config := tc.NewConfig()
				RunC3CPUUpdateTest(ctx, deployment, testcaseEnvInst, config, defaultCPULimits, newCPULimits)
			})

			It(tc.Label+", integration, c3: can verify IDXC, CM and SHC PVCs are correctly deleted after the CRs deletion", func() {
				config := tc.NewConfig()
				RunC3PVCDeletionTest(ctx, deployment, testcaseEnvInst, config, verificationTimeout)
			})
		})
	}

	// CSPL-3256 - SHC deployer resource spec test (IDXC is irrelevant for this test case)
	Context("Search Head Cluster", func() {
		BeforeEach(func() {
			defaultCPULimits = DefaultCPULimits
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
		})

		AfterEach(func() {
			Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("managercrcrud, integration, shc: can deploy Search Head Cluster with Deployer resource spec configured", func() {
			RunSHCDeployerResourceSpecTest(ctx, deployment, testcaseEnvInst, defaultCPULimits)
		})
	})

	// M4 tests — V3 (master) and V4 (manager) variants
	for _, tc := range masterManagerCrudConfigs {
		tc := tc
		Context("Multisite cluster deployment (M4 - Multisite Indexer Cluster, Search Head Cluster)", func() {
			BeforeEach(func() {
				defaultCPULimits = DefaultCPULimits
				newCPULimits = UpdatedCPULimits
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", integration, m4: can deploy multisite Indexer and Search Head Clusters, change their CR, update the instances", func() {
				config := tc.NewConfig()
				RunM4CPUUpdateTest(ctx, deployment, testcaseEnvInst, config, defaultCPULimits, newCPULimits)
			})
		})
	}
})
