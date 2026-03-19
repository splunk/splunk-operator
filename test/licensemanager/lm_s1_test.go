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
package licensemanager

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Licensemanager test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var config *LicenseTestConfig
	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")

		config = NewLicenseManagerConfig()

		// Validate test prerequisites early to fail fast
		err := testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Standalone deployment (S1) with License Manager", func() {
		It("licensemanager, smoke, s1: Splunk Operator can configure License Manager with Standalone in S1 SVA", func() {
			RunLMS1Test(ctx, deployment, testcaseEnvInst, config)
		})
	})
})
