// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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

package cert

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

// Cert tests are manager (V4) only — ClusterMaster/LicenseMaster are deprecated.
// Labeled with both tier:e2e-pr and tier:e2e-full so the same suite is picked
// up by the PR-tier smoke job and the nightly full-integration job.
var _ = Describe("Topology cert mounting", Label("tier:e2e-pr", "tier:e2e-full", "variant:manager", "feature:cert"), func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	ctx := context.TODO()

	Context("Standalone deployment (S1) — cert mounting", func() {
		BeforeEach(func() {
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
		})

		AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
			Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("standalone mounts a CA-only server-role cert, a tls-only no-role cert, and an auto-generated no-role cert, and detects rotation", Label("sva:s1"), func() {
			RunS1CertTest(ctx, deployment, testcaseEnvInst, testenvInstance)
		})
	})

	Context("Clustered deployment (C3) — cert mounting", func() {
		BeforeEach(func() {
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
		})

		AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
			Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("ClusterManager/IndexerCluster/SearchHeadCluster mount a CA-only server-role cert, a tls-only no-role cert, and an auto-generated no-role cert, and detect rotation", Label("sva:c3"), func() {
			RunC3CertTest(ctx, deployment, testcaseEnvInst, testenvInstance)
		})
	})

	Context("IngestorCluster deployment (topology) — cert mounting", func() {
		BeforeEach(func() {
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
		})

		AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
			Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("IngestorCluster mounts an auto-generated Role:server cert with explicit DNSNames and presents a valid TLS cert on port 8089", Label("sva:ingestor"), func() {
			RunICCertTestTopology(ctx, deployment, testcaseEnvInst, testenvInstance)
		})
	})

	Context("Standalone deployment (S1) — cert garbage collection on CR deletion", func() {
		BeforeEach(func() {
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
		})

		AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
			Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("deleting the CR removes the auto-generated Certificate but leaves both the auto-generated and customer-provided Secrets in place", Label("sva:s1"), func() {
			RunS1CertGCOnDeleteTest(ctx, deployment, testcaseEnvInst, testenvInstance)
		})
	})
})
