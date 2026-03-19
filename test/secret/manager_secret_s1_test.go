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
package secret

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Secret Test for SVA S1", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	ctx := context.TODO()
	var deployment *testenv.Deployment

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")

		// Validate test prerequisites early to fail fast
		err := testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Standalone deployment (S1) with LM and MC", func() {
		It("managersecret, integration, s1: Secret update on a standalone instance with LM and MC", func() {
			config := NewSecretTestConfigV4()
			RunS1SecretUpdateTest(ctx, deployment, testcaseEnvInst, config)
		})
	})

	Context("Standalone deployment (S1) with LM amd MC", func() {
		It("managersecret, integration, s1: Secret Object is recreated on delete and new secrets are applied to Splunk Pods", func() {
			config := NewSecretTestConfigV4()
			RunS1SecretDeleteTest(ctx, deployment, testcaseEnvInst, config)
		})
	})

	Context("Standalone deployment (S1)", func() {
		It("managersecret, smoke, s1: Secret Object data is repopulated in secret object on passing empty Data map and new secrets are applied to Splunk Pods", func() {
			config := NewSecretTestConfigV4()
			RunS1SecretDeleteWithMCRefTest(ctx, deployment, testcaseEnvInst, config)
		})
	})
})
