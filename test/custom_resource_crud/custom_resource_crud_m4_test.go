// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Crcrud test for SVA M4", func() {

	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string
	var ctx context.Context

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		defaultCPULimits = "4"
		newCPULimits = "2"
		ctx = context.TODO()
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testenvInstance.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
	})

	Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster)", func() {
		It("crcrud, integration, m4: can deploy can deploy multisite indexer and search head clusters, change their CR, update the instances", func() {

			// Deploy Multisite Cluster and Search Head Clusters
			mcRef := deployment.GetName()
			siteCount := 3
			err := deployment.DeployMultisiteClusterWithSearchHead(ctx, deployment.GetName(), 1, siteCount, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testenvInstance)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testenvInstance)

			// Verify CPU limits on Indexers before updating the CR
			for i := 1; i <= siteCount; i++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), i, 0)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), podName, defaultCPULimits)
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
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testenvInstance, splcommon.PhaseUpdating, idxcName)

			// Verify Indexers go to ready state
			testenv.IndexersReady(ctx, deployment, testenvInstance, siteCount)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testenvInstance)

			// Verify CPU limits after updating the CR
			for i := 1; i <= siteCount; i++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), i, 0)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), podName, newCPULimits)
			}
		})
	})
})
