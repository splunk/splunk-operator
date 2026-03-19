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
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Crcrud test for SVA M4", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string
	var ctx context.Context

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")

		// Validate test prerequisites early to fail fast
		err := testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")

		defaultCPULimits = "4"
		newCPULimits = "2"
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster)", func() {
		It("mastercrcrud, integration, m4: can deploy can deploy multisite indexer and search head clusters, change their CR, update the instances", func() {

			// Deploy Multisite Cluster and Search Head Clusters
			mcRef := deployment.GetName()
			prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
			siteCount := 3
			err := deployment.DeployMultisiteClusterMasterWithSearchHead(ctx, deployment.GetName(), 1, siteCount, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterMasterReady(ctx, deployment)

			// Ensure the indexers of all sites go to Ready phase
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Ensure cluster configured as multisite
			testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

			// Ensure search head cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Verify telemetry
			testcaseEnvInst.TriggerTelemetrySubmission(ctx, deployment)
			testcaseEnvInst.VerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Verify RF SF is met
			testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

			// Verify CPU limits on Indexers before updating the CR
			for i := 1; i <= siteCount; i++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), i, 0)
				testcaseEnvInst.VerifyCPULimits(deployment, podName, defaultCPULimits)
			}

			// Change CPU limits to trigger CR update
			idxc := &enterpriseApi.IndexerCluster{}
			for i := 1; i <= siteCount; i++ {
				siteName := fmt.Sprintf("site%d", i)
				instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
				err = deployment.GetInstance(ctx, instanceName, idxc)
				Expect(err).To(Succeed(), "Unable to fetch Indexer Cluster deployment")
				idxc.Spec.Resources.Limits = corev1.ResourceList{
					"cpu": resource.MustParse(newCPULimits),
				}
				err = deployment.UpdateCR(ctx, idxc)
				Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster with updated CR")
			}

			// Verify Indexer Cluster is updating
			idxcName := deployment.GetName() + "-" + "site1"
			testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating, idxcName)

			// Verify Indexers go to ready state
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Verify RF SF is met
			testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

			// Verify CPU limits after updating the CR
			for i := 1; i <= siteCount; i++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), i, 0)
				testcaseEnvInst.VerifyCPULimits(deployment, podName, newCPULimits)
			}
		})
	})
})
