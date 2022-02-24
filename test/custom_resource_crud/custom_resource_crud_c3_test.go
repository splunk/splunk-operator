// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Crcrud test for SVA C3", func() {

	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string
	var verificationTimeout time.Duration

	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
		Expect(err).ToNot(HaveOccurred())

		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		defaultCPULimits = "4"
		newCPULimits = "2"
		verificationTimeout = 150 * time.Second
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testenvInstance.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
		if testenvInstance != nil {
			Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("crcrud, integration, c3: can deploy indexer and search head cluster, change their CR, update the instances", func() {

			// Deploy Single site Cluster and Search Head Clusters
			mcRef := deployment.GetName()
			err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true /*shc*/, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testenvInstance)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testenvInstance)

			// Verify CPU limits on Indexers before updating the CR
			indexerCount := 3
			for i := 0; i < indexerCount; i++ {
				indexerPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), indexerPodName, defaultCPULimits)
			}

			// Change CPU limits to trigger CR update
			idxc := &enterpriseApi.IndexerCluster{}
			instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
			err = deployment.GetInstance(ctx, instanceName, idxc)
			Expect(err).To(Succeed(), "Unable to get instance of indexer cluster")
			idxc.Spec.Resources.Limits = corev1.ResourceList{
				"cpu": resource.MustParse(newCPULimits),
			}
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster with updated CR")

			// Verify Indexer Cluster is updating
			idxcName := deployment.GetName() + "-idxc"
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testenvInstance, splcommon.PhaseUpdating, idxcName)

			// Verify Indexers go to ready state
			testenv.SingleSiteIndexersReady(ctx, deployment, testenvInstance)

			// Verify CPU limits on Indexers after updating the CR
			for i := 0; i < indexerCount; i++ {
				indexerPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), indexerPodName, newCPULimits)
			}

			// Verify CPU limits on Search Heads before updating the CR
			searchHeadCount := 3
			for i := 0; i < searchHeadCount; i++ {
				SearchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, defaultCPULimits)
			}

			// Change CPU limits to trigger CR update
			shc := &enterpriseApi.SearchHeadCluster{}
			instanceName = fmt.Sprintf("%s-shc", deployment.GetName())
			err = deployment.GetInstance(ctx, instanceName, shc)
			Expect(err).To(Succeed(), "Unable to fetch Search Head Cluster deployment")

			shc.Spec.Resources.Limits = corev1.ResourceList{
				"cpu": resource.MustParse(newCPULimits),
			}
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster with updated CR")

			// Verify Search Head Cluster is updating
			testenv.VerifySearchHeadClusterPhase(ctx, deployment, testenvInstance, splcommon.PhaseUpdating)

			// Verify Search Head go to ready state
			testenv.SearchHeadClusterReady(ctx, deployment, testenvInstance)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testenvInstance)

			// Verify CPU limits on Search Heads after updating the CR
			for i := 0; i < searchHeadCount; i++ {
				SearchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				testenv.VerifyCPULimits(deployment, testenvInstance.GetName(), SearchHeadPodName, newCPULimits)
			}
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("crcrud, integration, c3: can verify IDXC, CM and SHC PVCs are correctly deleted after the CRs deletion", func() {

			// Deploy Single site Cluster and Search Head Clusters
			mcRef := deployment.GetName()
			err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true /*shc*/, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testenvInstance)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testenvInstance)

			// Verify Search Heads PVCs (etc and var) exists
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, "shc-search-head", 3, true, verificationTimeout)

			// Verify Deployer PVCs (etc and var) exists
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, "shc-deployer", 1, true, verificationTimeout)

			// Verify Indexers PVCs (etc and var) exists
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, "idxc-indexer", 3, true, verificationTimeout)

			// Verify Cluster Manager PVCs (etc and var) exists
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, splcommon.ClusterManager, 1, true, verificationTimeout)

			// Delete the Search Head Cluster
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)
			Expect(err).To(Succeed(), "Unable to GET SHC instance", "SHC Name", shc)
			err = deployment.DeleteCR(ctx, shc)
			Expect(err).To(Succeed(), "Unable to delete SHC instance", "SHC Name", shc)

			// Delete the Indexer Cluster
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", idxc)
			Expect(err).To(Succeed(), "Unable to GET IDXC instance", "IDXC Name", idxc)
			err = deployment.DeleteCR(ctx, idxc)
			Expect(err).To(Succeed(), "Unable to delete IDXC instance", "IDXC Name", idxc)

			// Delete the Cluster Manager
			cm := &enterpriseApi.ClusterMaster{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Unable to GET Cluster Manager instance", "Cluster Manager Name", cm)
			err = deployment.DeleteCR(ctx, cm)
			Expect(err).To(Succeed(), "Unable to delete Cluster Manager instance", "Cluster Manger Name", cm)

			// Delete Monitoring Console
			err = deployment.GetInstance(ctx, mcRef, mc)
			Expect(err).To(Succeed(), "Unable to GET Monitoring Console instance", "Monitoring Console Name", mcRef)
			err = deployment.DeleteCR(ctx, mc)
			Expect(err).To(Succeed(), "Unable to delete Monitoring Console instance", "Monitoring Console Name", mcRef)

			// Verify Search Heads PVCs (etc and var) have been deleted
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, "shc-search-head", 3, false, verificationTimeout)

			// Verify Deployer PVCs (etc and var) have been deleted
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, "shc-deployer", 1, false, verificationTimeout)

			// Verify Indexers PVCs (etc and var) have been deleted
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, "idxc-indexer", 3, false, verificationTimeout)

			// Verify Cluster Manager PVCs (etc and var) have been deleted
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, splcommon.ClusterManager, 1, false, verificationTimeout)

			// Verify Monitoring Console PVCs (etc and var) have been deleted
			testenv.VerifyPVCsPerDeployment(deployment, testenvInstance, "monitoring-console", 1, false, verificationTimeout)
		})
	})
})
