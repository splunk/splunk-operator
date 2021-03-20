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
	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("crcrud test", func() {

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

	Context("Standalone deployment (S1)", func() {
		It("crcrud: can deploy a standalone instance, change its CR, update the instance", func() {

			// Deploy Standalone
			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Verify Standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify CPU limits before updating the CR
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), standalonePodName, defaultCPULimits)

			// Change CPU limits to trigger CR update
			standalone.Spec.Resources.Limits = corev1.ResourceList{
				"cpu": resource.MustParse(newCPULimits),
			}
			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with updated CR ")

			// Verify Standalone is updating
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Verify Standalone goes to ready state
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseReady)

			// Verify CPU limits after updating the CR
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), standalonePodName, newCPULimits)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("crcrud: can deploy indexer and search head cluster, change their CR, update the instances", func() {

			// Deploy Single site Cluster and Search Head Clusters
			err := deployment.DeploySingleSiteCluster(deployment.GetName(), 3, true /*shc*/)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify CPU limits on Indexers before updating the CR
			indexerPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), indexerPodName, defaultCPULimits)
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 1)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), indexerPodName, defaultCPULimits)
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 2)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), indexerPodName, defaultCPULimits)

			// Change CPU limits to trigger CR update
			idxc := &enterprisev1.IndexerCluster{}
			instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
			err = deployment.GetInstance(instanceName, idxc)
			if err != nil {
				testenvInstance.Log.Error(err, "Unable to fetch Indexer Cluster deployment")
			}
			idxc.Spec.Resources.Limits = corev1.ResourceList{
				"cpu": resource.MustParse(newCPULimits),
			}
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster with updated CR")

			// Verify Indexer Cluster is updating
			idxcName := deployment.GetName() + "-idxc"
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseUpdating, idxcName)

			// Verify Indexers go to ready state
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseReady, idxcName)

			// Verify CPU limits after updating the CR
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), indexerPodName, newCPULimits)
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 1)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), indexerPodName, newCPULimits)
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 2)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), indexerPodName, newCPULimits)

			// Verify CPU limits on Search Heads before updating the CR
			SearchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, defaultCPULimits)
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, defaultCPULimits)
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, defaultCPULimits)

			// Change CPU limits to trigger CR update
			shc := &enterprisev1.SearchHeadCluster{}
			instanceName = fmt.Sprintf("%s-shc", deployment.GetName())
			err = deployment.GetInstance(instanceName, shc)
			if err != nil {
				testenvInstance.Log.Error(err, "Unable to fetch Search Head Cluster deployment")
			}

			shc.Spec.Resources.Limits = corev1.ResourceList{
				"cpu": resource.MustParse(newCPULimits),
			}
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster with updated CR")

			// Verify Search Head Cluster is updating
			testenv.VerifySearchHeadClusterPhase(deployment, testenvInstance, splcommon.PhaseUpdating)

			// Verify Search Head go to ready state
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify CPU limits after updating the CR
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, newCPULimits)
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, newCPULimits)
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, newCPULimits)
		})
	})

	Context("Multisite cluster deployment (M13 - Multisite indexer cluster, Search head cluster)", func() {
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
				siteName := fmt.Sprintf("site%d", i)
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteName, 0)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), podName, defaultCPULimits)
			}

			// Change CPU limits to trigger CR update
			idxc := &enterprisev1.IndexerCluster{}
			for i := 1; i <= siteCount; i++ {
				siteName := fmt.Sprintf("site%d", i)
				instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
				err = deployment.GetInstance(instanceName, idxc)
				if err != nil {
					testenvInstance.Log.Error(err, "Unable to fetch Indexer Cluster deployment")
				}
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
				siteName := fmt.Sprintf("site%d", i)
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteName, 0)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), podName, newCPULimits)
			}

			// Verify CPU limits on Search Heads before updating the CR
			SearchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, defaultCPULimits)
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, defaultCPULimits)
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, defaultCPULimits)

			// Change CPU limits to trigger CR update
			shc := &enterprisev1.SearchHeadCluster{}
			instanceName := fmt.Sprintf("%s-shc", deployment.GetName())
			err = deployment.GetInstance(instanceName, shc)
			if err != nil {
				testenvInstance.Log.Error(err, "Unable to fetch Search Head Cluster deployment")
			}

			shc.Spec.Resources.Limits = corev1.ResourceList{
				"cpu": resource.MustParse(newCPULimits),
			}
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster with updated CR")

			// Verify Search Head Cluster is updating
			testenv.VerifySearchHeadClusterPhase(deployment, testenvInstance, splcommon.PhaseUpdating)

			// Verify Search Head go to ready state
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify CPU limits after updating the CR
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, newCPULimits)
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, newCPULimits)
			SearchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, newCPULimits)
		})
	})
})
