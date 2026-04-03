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

var _ = Describe("License Manager App Framework test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	for _, tc := range masterManagerLMConfigs {
		tc := tc

		Context("Clustered deployment (C3) with "+tc.Label+" App Framework", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", integration, c3: Splunk Operator can configure a C3 SVA and have apps installed locally on LM", func() {
				RunLMC3AppFrameworkTest(ctx, deployment, testcaseEnvInst, testenvInstance, tc.NewConfig())
			})
		})
	}
})
