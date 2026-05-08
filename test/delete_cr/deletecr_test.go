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
package deletecr

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Delete CR test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	Context("Standalone deployment (S1)", func() {
		It("integration, managerdeletecr: can deploy standalone and delete", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			Expect(testcaseEnvInst.RunDeleteStandaloneWorkflow(ctx, deployment)).To(Succeed(), "Unable to run delete Standalone workflow")
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3)", func() {
		It("integration, managerdeletecr: can deploy C3 and delete search head, clustermanager", NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {
			Expect(testcaseEnvInst.RunDeleteC3Workflow(ctx, deployment, 3)).To(Succeed(), "Unable to run delete C3 workflow")
		})
	})
})
