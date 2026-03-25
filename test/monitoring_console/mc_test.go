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
package monitoringconsoletest

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
)

// Master (V3) Monitoring Console tests
var _ = Describe("Monitoring Console test (master)", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("mastermc, smoke: MC can configure SHC, indexer instances after scale up and standalone in a namespace", func() {
			/*
				Test Steps
				1. Deploy Single Site Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. VerifyMonitoring Console Pod has Search Heads in Peer strings
				6. Verify Monitoring Console Pod has peers(indexers) in Peer string
				7. Scale SH Cluster
				8. Scale Indexer Count
				9. Add a standalone
				10. Verify Standalone is configured in MC Config Map and Peer String
				11. Verify SH are configured in MC Config Map and Peers String
				12. Verify Indexers are configured in Peer String
			*/

			defaultSHReplicas := 3
			defaultIndexerReplicas := 3
			mcName := deployment.GetName()

			// Deploy and verify Monitoring Console
			mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")

			// Deploy and verify C3 cluster with MC
			testcaseEnvInst.DeployAndVerifyC3WithMC(ctx, deployment, deployment.GetName(), defaultIndexerReplicas, mcName)

			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			// Wait for Cluster Master to appear in Monitoring Console Config Map
			err := testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, true, 2*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for Cluster Master in MC ConfigMap")

			// Verify MC configuration for C3 cluster
			testcaseEnvInst.VerifyMCConfigForC3Cluster(ctx, deployment, deployment.GetName(), mcName, defaultSHReplicas, defaultIndexerReplicas, true)

			// Scale Search Head Cluster
			scaledSHReplicas := defaultSHReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)
			testcaseEnvInst.ScaleSearchHeadCluster(ctx, deployment, deployment.GetName(), scaledSHReplicas)

			// Scale indexers
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)
			testcaseEnvInst.ScaleIndexerCluster(ctx, deployment, deployment.GetName(), scaledIndexerReplicas)

			// get revision number of the resource
			resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Deploy Standalone with MC reference
			testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, deployment.GetName(), mcName)

			// Ensure Indexer cluster go to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// Ensure Search Head Cluster go to Ready Phase
			// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

			// Wait for MC to go to PENDING Phase
			//testcaseEnvInst.VerifyMonitoringConsolePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhasePending)

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Verify Standalone configured on Monitoring Console
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, deployment.GetName(), mcName, true)

			// Verify MC configuration after scale up
			testcaseEnvInst.Log.Info("Verify MC configuration after Scale Up")
			testcaseEnvInst.VerifyMCConfigForC3Cluster(ctx, deployment, deployment.GetName(), mcName, scaledSHReplicas, scaledIndexerReplicas, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("mastermc, integration: MC can configure SHC, indexer instances and reconfigure to new MC", func() {
			/*
				Test Steps
				1. Deploy Single Site Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. Verify SH are configured in peer strings on MC Pod
				6. Verify Monitoring Console Pod has peers in Peer string on MC Pod
				--------------- RECONFIG CLUSTER MANAGER WITH NEW MC --------------------------
				13. Reconfigure CM with Second MC
				14. Verify CM in config map of Second MC
				15. Create Second MC Pod
				16. Verify Indexers in second MC Pod
				17. Verify SHC not in second MC
				18. Verify SHC still present in first MC
				19. Verify CM and Indexers not in first MC Pod
				---------------- RECONFIG SHC WITH NEW MC
				20. Configure SHC with Second MC
				21. Verify CM, DEPLOYER, Search Heads in config map of second MC
				22. Verify SHC in second MC pod
				23. Verify Indexers in Second MC Pod
				24. Verify CM and Deployer not in first MC CONFIG MAP
				24. Verify SHC, Indexers not in first MC Pod
			*/

			defaultSHReplicas := 3
			defaultIndexerReplicas := 3
			mcName := deployment.GetName()

			// Deploy Monitoring Console Pod
			mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")

			err := deployment.DeploySingleSiteClusterMasterWithGivenMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterMasterReady(ctx, deployment)
			testcaseEnvInst.VerifyC3ComponentsReady(ctx, deployment)

			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			// Verify MC configuration for C3 cluster
			testcaseEnvInst.VerifyMCConfigForC3Cluster(ctx, deployment, deployment.GetName(), mcName, defaultSHReplicas, defaultIndexerReplicas, true)

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Generate pod name slices for later verification
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
			indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)

			// #################  Update Monitoring Console In Cluster Master CR ##################################

			mcTwoName := deployment.GetName() + "-two"
			cm := &enterpriseApiV3.ClusterMaster{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to get instance of Cluster Manager")

			// get revision number of the resource
			resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, cm)

			cm.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(ctx, cm)

			Expect(err).To(Succeed(), "Failed to update mcRef in Cluster Manager")

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, cm, resourceVersion)

			// Ensure Cluster Master Goes to Updating Phase
			//testcaseEnvInst.VerifyClusterMasterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterMasterReady(ctx, deployment)

			// Ensure indexers go to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// Deploy and verify Monitoring Console Two
			mcTwo := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")

			// ###########   VERIFY MONITORING CONSOLE TWO AFTER CLUSTER MANAGER RECONFIG  ###################################
			masterParams := MCReconfigParams{CMServiceNameFmt: testenv.ClusterMasterServiceName, CMURLKey: "SPLUNK_CLUSTER_MASTER_URL"}
			VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, masterParams, mcTwoName, shPods, indexerPods, true)

			// ##############  VERIFY MONITORING CONSOLE ONE AFTER CLUSTER MANAGER RECONFIG #######################
			VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, masterParams, mcName, mc, shPods, false)

			// #################  Update Monitoring Console In SHC CR ##################################

			// Update SHC to use 2nd Monitoring Console
			shc := &enterpriseApi.SearchHeadCluster{}
			shcName := deployment.GetName() + "-shc"
			testenv.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, testcaseEnvInst, shc, shcName, mcTwoName)

			// Ensure Search Head Cluster go to Ready Phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Verify MC is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo)

			// ############################  VERIFICATOIN FOR MONITORING CONSOLE TWO POST SHC RECONFIG ###############################
			VerifyMCTwoAfterSHCReconfig(ctx, deployment, testcaseEnvInst, masterParams, mcTwoName, shPods, indexerPods, 0)

			// ############################  VERIFICATOIN FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################
			VerifyMCOneAfterSHCReconfig(ctx, deployment, testcaseEnvInst, masterParams, mcName, mc, shPods, 0)
		})
	})

	Context("Multisite Clustered deployment (M4 - 3 Site clustered indexer, search head cluster)", func() {
		It("mastermc, integration: MC can configure SHC, indexer instances and reconfigure Cluster Manager to new Monitoring Console", func() {
			/*
				Test Steps
				1. Deploy Multisite Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. Verify SH are configured in peer strings on MC Pod
				6. Verify Indexers are configured in MC Config Map
				7. Verify Monitoring Console Pod has peers in Peer string on MC Pod
				############ CLUSTER MANAGER MC RECONFIG #################################
				8.  Configure Cluster Master to use 2nd Monitoring Console
				9.  Verify Cluster Master is configured Config Maps of Second MC
				10. Deploy 2nd MC pod
				11. Verify Indexers in 2nd MC Pod
				12. Verify SHC not in 2nd MC CM
				13. Verify SHC not in 2nd MC Pod
				14. Verify Cluster Master not 1st MC Config Map
				15. Verify Indexers not in 1st MC Pod
			*/
			defaultSHReplicas := 3
			defaultIndexerReplicas := 1
			siteCount := 3
			mcName := deployment.GetName()
			err := deployment.DeployMultisiteClusterMasterWithMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, siteCount, mcName, true)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster with SHC")

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterMasterReady(ctx, deployment)
			testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)

			// Deploy and verify Monitoring Console
			mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

			// Verify MC configuration for M4 cluster
			testcaseEnvInst.VerifyMCConfigForM4Cluster(ctx, deployment, deployment.GetName(), mcName, defaultSHReplicas, defaultIndexerReplicas, siteCount, true)

			// Generate pod name slices for later verification
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
			indexerPods := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), defaultIndexerReplicas, true, siteCount)

			// ############ CLUSTER MANAGER MC RECONFIG #################################
			mcTwoName := deployment.GetName() + "-two"
			// Update Cluster Manager to use 2nd Monitoring Console
			cm := &enterpriseApiV3.ClusterMaster{}
			testenv.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, testcaseEnvInst, cm, deployment.GetName(), mcTwoName)

			// Ensure Cluster Master Goes to Updating Phase
			//testcaseEnvInst.VerifyClusterMasterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterMasterReady(ctx, deployment)

			// Deploy and verify Monitoring Console Two
			mcTwo := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")

			// Verify Monitoring Console TWO is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo)

			masterParams := MCReconfigParams{CMServiceNameFmt: testenv.ClusterMasterServiceName, CMURLKey: "SPLUNK_CLUSTER_MASTER_URL"}
			VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, masterParams, mcTwoName, shPods, indexerPods, false)
			VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, masterParams, mcName, mc, shPods, true)

		})
	})
})

// Manager (V4) Monitoring Console tests
var _ = Describe("Monitoring Console test (manager)", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Deploy Monitoring Console", func() {
		It("smoke, monitoringconsole: can deploy MC CR and can be configured standalone", func() {
			/*
				Test Steps
				1. Deploy Monitoring Console
				2. Deploy Standalone
				3. Wait for Monitoring Console status to go back to READY
				4. Verify Standalone configured in Monitoring Console Config Map
				5. Verify Monitoring Console Pod has correct peers in Peer List
				--------------- RECONFIG WITH NEW MC --------------------------
				6.  Reconfig S1 with 2nd Monitoring Console Name
				7.  Check 2nd Monitoring Console Config Map to verify s1
				8.  Deploy 2nd Monitoring Console Pod
				9.  Verify Standalone pod is configured on Monitoring Console Pod
				10. Verify 1st Monitoring Console Config Map is not configured with S1
				11. Verify 1st Monitoring Console Pod is not configured with S1
			*/

			// Deploy Monitoring Console CRD
			mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")

			// Create Standalone Spec and apply
			standaloneOneName := deployment.GetName()
			mcName := deployment.GetName()
			spec := testenv.NewStandaloneSpecWithMCRef(testcaseEnvInst.GetSplunkImage(), mcName)
			standaloneOne, err := deployment.DeployStandaloneWithGivenSpec(ctx, standaloneOneName, spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for standalone to be in READY Status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standaloneOne)

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)
			//testcaseEnvInst.VerifyMonitoringConsolePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)

			// Verify MC is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Check Standalone is configured in MC
			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

			// #########################  RECONFIGURE STANDALONE WITH SECOND MC #######################################

			// Reconfig S1 with 2nd Monitoring Console Name
			mcTwoName := deployment.GetName() + "-two"
			err = deployment.GetInstance(ctx, standaloneOneName, standaloneOne)
			Expect(err).To(Succeed(), "Unable to get instance of Standalone")
			standaloneOne.Spec.MonitoringConsoleRef.Name = mcTwoName

			// Update Standalone with 2nd MC
			err = deployment.UpdateCR(ctx, standaloneOne)
			Expect(err).To(Succeed(), "Unable to update Standalone with new MC Name")

			// Deploy 2nd MC Pod
			testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")

			// Check Standalone is configured in MC Two
			testcaseEnvInst.Log.Info("Checking for Standalone on SECOND MC after Standalone RECONFIG")
			verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcTwoName, true)

			// Verify Monitoring Console One is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Check Standalone is NOT configured in MC One
			testcaseEnvInst.Log.Info("Checking for Standalone NOT ON FIRST MC after Standalone RECONFIG")
			verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, false)

		})
	})

	Context("Standalone deployment (S1)", func() {
		It("managermc1, integration: can deploy a MC with standalone instance and update MC with new standalone deployment", func() {
			RunS1StandaloneAddDeleteMCTest(ctx, deployment, testcaseEnvInst, deployment.GetName(), "standalone-"+testenv.RandomDNSName(3))
		})
	})

	Context("Standalone deployment with Scale up", func() {
		It("managermc1, integration: can deploy a MC with standalone instance and update MC when standalone is scaled up", func() {
			/*
				Test Steps
				1.  Deploy Standalone
				2.  Wait for Standalone to go to READY
				3.  Deploy Monitoring Console
				4.  Wait for Monitoring Console status to be READY
				5.  Verify Standalone configured in Monitoring Console Config Map
				6.  Verify Monitoring Console Pod has correct peers in Peer List
				7.  Scale Standalone to 2 REPLICAS
				8.  Wait for Second Standalone POD to come up and PHASE to be READY
				9.  Wait for Monitoring Console status to go UPDATING then READY
				10. Verify both Standalone PODS configured in Monitoring Console Config Map
				11. Verify both Standalone configured in Monitoring Console Pod Peers String
			*/

			standaloneName := deployment.GetName()
			mcName := deployment.GetName()
			spec := testenv.NewStandaloneSpecWithMCRef(testcaseEnvInst.GetSplunkImage(), mcName)

			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, standaloneName, spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for standalone to be in READY Status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

			// Deploy MC and wait for MC to be READY
			mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

			// Check Standalone is configured in MC
			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneName, 1, false, 0)
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Scale Standalone instance
			testcaseEnvInst.Log.Info("Scaling Standalone CR")
			scaledReplicaCount := 2
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(ctx, deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale Standalone")

			// Ensure standalone is scaling up
			testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

			// Wait for MC to go to Updating Phase
			//testcaseEnvInst.VerifyMonitoringConsolePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)

			// Verify MC is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneName, 2, false, 0)

			// Check both Standalone pods are configured in MC after scale up
			testcaseEnvInst.Log.Info("Checking for Standalone Pods on MC after scale up")
			verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("managermc, smoke: MC can configure SHC, indexer instances after scale up and standalone in a namespace", func() {
			/*
				Test Steps
				1. Deploy Single Site Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. VerifyMonitoring Console Pod has Search Heads in Peer strings
				6. Verify Monitoring Console Pod has peers(indexers) in Peer string
				7. Scale SH Cluster
				8. Scale Indexer Count
				9. Add a standalone
				10. Verify Standalone is configured in MC Config Map and Peer String
				11. Verify SH are configured in MC Config Map and Peers String
				12. Verify Indexers are configured in Peer String
			*/

			defaultSHReplicas := 3
			defaultIndexerReplicas := 3
			mcName := deployment.GetName()

			// Deploy Monitoring Console Pod
			mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")

			err := deployment.DeploySingleSiteClusterWithGivenMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)
			testcaseEnvInst.VerifyC3ComponentsReady(ctx, deployment)

			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			// Wait for Cluster Manager to appear in Monitoring Console Config Map
			err = testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcName, true, 2*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for Cluster Manager in MC ConfigMap")

			// Check Deployer in Monitoring Console Config Map
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Search Head Pods in Monitoring Console Config Map
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			// Wait for Monitoring console Pod to be configured with all search head
			err = testcaseEnvInst.WaitForPodsInMCConfigString(ctx, deployment, shPods, mcName, true, false, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for search heads in MC config")

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, true, true)

			// Scale Search Head Cluster
			scaledSHReplicas := defaultSHReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)
			testcaseEnvInst.ScaleSearchHeadCluster(ctx, deployment, deployment.GetName(), scaledSHReplicas)

			// Scale indexers
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)
			testcaseEnvInst.ScaleIndexerCluster(ctx, deployment, deployment.GetName(), scaledIndexerReplicas)

			// get revision number of the resource
			resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Deploy Standalone Pod
			spec := testenv.NewStandaloneSpecWithMCRef(testcaseEnvInst.GetSplunkImage(), mcName)
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for Standalone to be in READY status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

			// Ensure Indexer cluster go to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// Ensure Search Head Cluster go to Ready Phase
			// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

			// Wait for MC to go to PENDING Phase
			//testcaseEnvInst.VerifyMonitoringConsolePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhasePending)

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Check Standalone configured on Monitoring Console
			standalonePods := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

			// Verify all Search Head Members are configured on Monitoring Console
			shPods = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), scaledSHReplicas, false, 0)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Config Map after Scale Up")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Pod after Scale Up")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			testcaseEnvInst.Log.Info("Checking for Indexer Pod on MC after Scale Up")
			indexerPods = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas, false, 0)
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, true, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("managermc1, integration: MC can configure SHC, indexer instances and reconfigure to new MC", func() {
			/*
				Test Steps
				1. Deploy Single Site Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. Verify SH are configured in peer strings on MC Pod
				6. Verify Monitoring Console Pod has peers in Peer string on MC Pod
				--------------- RECONFIG CLUSTER MANAGER WITH NEW MC --------------------------
				13. Reconfigure CM with Second MC
				14. Verify CM in config map of Second MC
				15. Create Second MC Pod
				16. Verify Indexers in second MC Pod
				17. Verify SHC not in second MC
				18. Verify SHC still present in first MC
				19. Verify CM and Indexers not in first MC Pod
				---------------- RECONFIG SHC WITH NEW MC
				20. Configure SHC with Second MC
				21. Verify CM, DEPLOYER, Search Heads in config map of second MC
				22. Verify SHC in second MC pod
				23. Verify Indexers in Second MC Pod
				24. Verify CM and Deployer not in first MC CONFIG MAP
				24. Verify SHC, Indexers not in first MC Pod
			*/

			defaultSHReplicas := 3
			defaultIndexerReplicas := 3
			mcName := deployment.GetName()

			// Deploy Monitoring Console Pod
			mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")

			err := deployment.DeploySingleSiteClusterWithGivenMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)
			testcaseEnvInst.VerifyC3ComponentsReady(ctx, deployment)

			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			// Check Cluster Manager in Monitoring Console Config Map
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcName, true)

			// Check Deployer in Monitoring Console Config Map
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Search Head Pods in Monitoring Console Config Map
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Wait for Monitoring console Pod to be configured with all search head
			err = testcaseEnvInst.WaitForPodsInMCConfigString(ctx, deployment, shPods, mcName, true, false, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for search heads in MC config")

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, true, true)

			// #################  Update Monitoring Console In Cluster Manager CR ##################################

			mcTwoName := deployment.GetName() + "-two"
			cm := &enterpriseApi.ClusterManager{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to get instance of Cluster Manager")

			// get revision number of the resource
			resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, cm)

			cm.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(ctx, cm)

			Expect(err).To(Succeed(), "Failed to update mcRef in Cluster Manager")

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, cm, resourceVersion)

			// Ensure Cluster Manager Goes to Updating Phase
			//testcaseEnvInst.VerifyClusterManagerPhase(ctx, deployment, enterpriseApi.PhaseUpdating)

			// Ensure that the cluster-manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Ensure indexers go to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// Deploy Monitoring Console Pod
			testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")

			// ###########   VERIFY MONITORING CONSOLE TWO AFTER CLUSTER MANAGER RECONFIG  ###################################
			managerParams := MCReconfigParams{CMServiceNameFmt: testenv.ClusterManagerServiceName, CMURLKey: splcommon.ClusterManagerURL}
			VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, managerParams, mcTwoName, shPods, indexerPods, true)

			// ##############  VERIFY MONITORING CONSOLE ONE AFTER CLUSTER MANAGER RECONFIG #######################
			VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, managerParams, mcName, mc, shPods, false)

			// #################  Update Monitoring Console In SHC CR ##################################

			// Get instance of current SHC CR with latest config
			shc := &enterpriseApi.SearchHeadCluster{}
			shcName := deployment.GetName() + "-shc"
			err = deployment.GetInstance(ctx, shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Update SHC to use 2nd Monitoring Console
			shc.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to update Monitoring Console in Search Head Cluster CRD")

			// Ensure Search Head Cluster go to Ready Phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Verify MC is Ready and stays in ready state
			// testenv.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo, testcaseEnvInst)

			// ############################  VERIFICATION FOR MONITORING CONSOLE TWO POST SHC RECONFIG ###############################
			VerifyMCTwoAfterSHCReconfig(ctx, deployment, testcaseEnvInst, managerParams, mcTwoName, shPods, indexerPods, 5*time.Minute)

			// ############################  VERIFICATION FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################
			VerifyMCOneAfterSHCReconfig(ctx, deployment, testcaseEnvInst, managerParams, mcName, mc, shPods, 5*time.Minute)
		})
	})

	Context("Multisite Clustered deployment (M4 - 3 Site clustered indexer, search head cluster)", func() {
		It("managermc2, integration: MC can configure SHC, indexer instances and reconfigure Cluster Manager to new Monitoring Console", func() {
			/*
				Test Steps
				1. Deploy Multisite Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. Verify SH are configured in peer strings on MC Pod
				6. Verify Indexers are configured in MC Config Map
				7. Verify Monitoring Console Pod has peers in Peer string on MC Pod
				############ CLUSTER MANAGER MC RECONFIG #################################
				8.  Configure Cluster Manager to use 2nd Monitoring Console
				9.  Verify Cluster Manager is configured Config Maps of Second MC
				10. Deploy 2nd MC pod
				11. Verify Indexers in 2nd MC Pod
				12. Verify SHC not in 2nd MC CM
				13. Verify SHC not in 2nd MC Pod
				14. Verify Cluster Manager not 1st MC Config Map
				15. Verify Indexers not in 1st MC Pod
			*/
			defaultSHReplicas := 3
			defaultIndexerReplicas := 1
			siteCount := 3
			mcName := deployment.GetName()
			err := deployment.DeployMultisiteClusterWithMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, siteCount, mcName, true)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Ensure indexers go to Ready phase
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Ensure indexer clustered is configured as multisite
			testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

			// Ensure search head cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Check Cluster Manager in Monitoring Console Config Map
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcName, true)

			// Check Deployer in Monitoring Console Config Map
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Deploy Monitoring Console Pod
			mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

			// Check Monitoring console is configured with all search head instances in namespace
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)

			testcaseEnvInst.Log.Info("Checking for Search Head on MC Config Map")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Checking for Search Head on MC Pod")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, 3)
			testcaseEnvInst.Log.Info("Checking for Indexer Pods on MC POD")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, true, true)

			// ############ CLUSTER MANAGER MC RECONFIG #################################
			mcTwoName := deployment.GetName() + "-two"
			cm := &enterpriseApi.ClusterManager{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to get instance of Cluster Manager")

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, cm)

			cm.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(ctx, cm)
			Expect(err).To(Succeed(), "Failed to update mcRef in Cluster Manager")

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, cm, resourceVersion)

			// Ensure Cluster Manager Goes to Updating Phase
			//testcaseEnvInst.VerifyClusterManagerPhase(ctx, deployment, enterpriseApi.PhaseUpdating)

			// Ensure that the cluster-manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Deploy Monitoring Console Pod
			testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")

			managerParams := MCReconfigParams{CMServiceNameFmt: testenv.ClusterManagerServiceName, CMURLKey: splcommon.ClusterManagerURL}
			VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, managerParams, mcTwoName, shPods, indexerPods, false)
			VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, managerParams, mcName, mc, shPods, true)

		})
	})

	Context("Standalone deployment (S1)", func() {
		It("managermc2, integration: can deploy a MC with standalone instance and update MC with new standalone deployment of similar names", func() {
			RunS1StandaloneAddDeleteMCTest(ctx, deployment, testcaseEnvInst, "search-head-adhoc", "search-head")
		})
	})

})
