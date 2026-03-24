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

var _ = Describe("Crcrud test for SVA C3", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string
	var verificationTimeout time.Duration

	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")

		defaultCPULimits = DefaultCPULimits
		newCPULimits = UpdatedCPULimits
		verificationTimeout = DefaultVerificationTimeout
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("mastercrcrud, integration, c3: can deploy indexer and search head cluster, change their CR, update the instances", func() {
			config := testenv.NewClusterReadinessConfigV3()
			RunC3CPUUpdateTest(ctx, deployment, testcaseEnvInst, config, defaultCPULimits, newCPULimits)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("mastercrcrud, integration, c3: can verify IDXC, CM and SHC PVCs are correctly deleted after the CRs deletion", func() {
			config := testenv.NewClusterReadinessConfigV3()
			RunC3PVCDeletionTest(ctx, deployment, testcaseEnvInst, config, verificationTimeout)
		})
	})
})
