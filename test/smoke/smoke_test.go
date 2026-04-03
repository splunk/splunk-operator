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
package smoke

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Smoke test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(func() {
		Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	Context("Standalone deployment (S1)", func() {
		It("smoke, basic, s1: can deploy a standalone instance", func() {
			_, err := testcaseEnvInst.RunStandaloneDeploymentWorkflow(ctx, deployment)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")
		})
	})

	Context("Clustered deployment (C3 - Clustered Indexer, Search Head Cluster)", func() {
		It("smoke, basic, c3: can deploy indexers and search head cluster", func() {
			testcaseEnvInst.RunC3DeploymentWorkflow(ctx, deployment, 3, "")
		})
	})

	Context("Multisite cluster deployment (M4 - Multisite Indexer Cluster, Search Head Cluster)", func() {
		It("smoke, basic, m4: can deploy indexers and search head cluster", func() {
			testcaseEnvInst.RunM4DeploymentWorkflow(ctx, deployment, 1, 3, "")
		})
	})

	Context("Multisite cluster deployment (M1 - Multisite Indexer Cluster)", func() {
		It("smoke, basic: can deploy multisite indexers cluster", func() {
			testcaseEnvInst.RunM1DeploymentWorkflow(ctx, deployment, 1, 3)
		})
	})

	Context("Standalone deployment (S1) with Service Account", func() {
		It("smoke, basic, s1: can deploy a standalone instance attached to a service account", func() {
			serviceAccountName := "smoke-service-account"
			testcaseEnvInst.RunStandaloneWithServiceAccountWorkflow(ctx, deployment, serviceAccountName)
		})
	})
})
