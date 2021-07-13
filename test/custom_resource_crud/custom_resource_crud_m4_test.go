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
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/latest"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Crcrud test for SVA M4", func() {

	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		defaultCPULimits = "4"
		newCPULimits = "2"
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
		It("crcrud : can deploy can deploy multisite indexer and search head clusters, change their CR, update the instances", func() {

			// Deploy Multisite Cluster and Search Head Clusters
			siteCount := 3
			err := deployment.DeployMultisiteClusterWithSearchHead(deployment.GetName(), 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

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
				err = deployment.GetInstance(instanceName, idxc)
				Expect(err).To(Succeed(), "Unable to fetch Indexer Cluster deployment")
				idxc.Spec.Resources.Limits = corev1.ResourceList{
					"cpu": resource.MustParse(newCPULimits),
				}
				err = deployment.UpdateCR(idxc)
				Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster with updated CR")
			}

			// Verify Indexer Cluster is updating
			idxcName := deployment.GetName() + "-" + "site1"
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseUpdating, idxcName)

			// Verify Indexers go to ready state
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Verify CPU limits after updating the CR
			for i := 1; i <= siteCount; i++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), i, 0)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), podName, newCPULimits)
			}
		})
	})
})
