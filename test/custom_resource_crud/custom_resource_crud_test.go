// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Crcrud test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string
	var verificationTimeout time.Duration

	ctx := context.TODO()

	// S1 test — single variant (manager, V4)
	Context("Standalone deployment (S1)", func() {
		BeforeEach(func() {
			testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")
			defaultCPULimits = DefaultCPULimits
			newCPULimits = UpdatedCPULimits
		})

		AfterEach(func() {
			testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
		})

		It("managercrcrud, integration, s1: can deploy a standalone instance, change its CR, update the instance", func() {
			RunS1CPUUpdateTest(ctx, deployment, testcaseEnvInst, defaultCPULimits, newCPULimits)
		})
	})

	// C3 tests — V3 (master) and V4 (manager) variants
	c3CrudConfigs := []struct {
		namePrefix string
		label      string
		newConfig  func() *testenv.ClusterReadinessConfig
	}{
		{"master", "mastercrcrud", testenv.NewClusterReadinessConfigV3},
		{"", "managercrcrud", testenv.NewClusterReadinessConfigV4},
	}

	for _, tc := range c3CrudConfigs {
		tc := tc
		Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
			BeforeEach(func() {
				defaultCPULimits = DefaultCPULimits
				newCPULimits = UpdatedCPULimits
				verificationTimeout = DefaultVerificationTimeout
				testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, tc.namePrefix)
			})

			AfterEach(func() {
				testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
			})

			It(tc.label+", integration, c3: can deploy indexer and search head cluster, change their CR, update the instances", func() {
				config := tc.newConfig()
				RunC3CPUUpdateTest(ctx, deployment, testcaseEnvInst, config, defaultCPULimits, newCPULimits)
			})

			It(tc.label+", integration, c3: can verify IDXC, CM and SHC PVCs are correctly deleted after the CRs deletion", func() {
				config := tc.newConfig()
				RunC3PVCDeletionTest(ctx, deployment, testcaseEnvInst, config, verificationTimeout)
			})
		})
	}

	// CSPL-3256 - SHC deployer resource spec test (IDXC is irrelevant for this test case)
	Context("Search Head Cluster", func() {
		BeforeEach(func() {
			defaultCPULimits = DefaultCPULimits
			testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")
		})

		AfterEach(func() {
			testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
		})

		It("managercrcrud, integration, shc: can deploy Search Head Cluster with Deployer resource spec configured", func() {
			RunSHCDeployerResourceSpecTest(ctx, deployment, testcaseEnvInst, defaultCPULimits)
		})
	})

	// M4 tests — V3 (master) and V4 (manager) variants
	m4CrudConfigs := []struct {
		namePrefix string
		label      string
		newConfig  func() *testenv.ClusterReadinessConfig
	}{
		{"master", "mastercrcrud", testenv.NewClusterReadinessConfigV3},
		{"", "managercrcrud", testenv.NewClusterReadinessConfigV4},
	}

	for _, tc := range m4CrudConfigs {
		tc := tc
		Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster)", func() {
			BeforeEach(func() {
				defaultCPULimits = DefaultCPULimits
				newCPULimits = UpdatedCPULimits
				testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, tc.namePrefix)
			})

			AfterEach(func() {
				testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
			})

			It(tc.label+", integration, m4: can deploy multisite indexer and search head clusters, change their CR, update the instances", func() {
				config := tc.newConfig()
				RunM4CPUUpdateTest(ctx, deployment, testcaseEnvInst, config, defaultCPULimits, newCPULimits)
			})
		})
	}
})
