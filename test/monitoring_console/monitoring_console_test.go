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
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Monitoring Console test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")

		// Validate test prerequisites early to fail fast
		err := testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")
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
			mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Deploy and verify C3 cluster with MC
			testcaseEnvInst.DeployAndVerifyC3WithMC(ctx, deployment, deployment.GetName(), defaultIndexerReplicas, mcName)

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

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
				18. Verify SHC still present in first MC d
				19. Veirfy CM and Indexers not in first MC Pod
				---------------- RECONFIG SHC WITH NEW MC
				20. Configure SHC with Second MC
				21. Verify CM, DEPLOYER, Search Heads in config map of seconds MC
				22. Verify SHC in second MC pod
				23. Verify Indexers in Second MC Pod
				24. Verify CM and Deployer not in first MC CONFIG MAP
				24. Verify SHC, Indexers not in first MC Pod
			*/

			defaultSHReplicas := 3
			defaultIndexerReplicas := 3
			mcName := deployment.GetName()

			// Deploy Monitoring Console Pod
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			err = deployment.DeploySingleSiteClusterMasterWithGivenMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterMasterReady(ctx, deployment)

			// Ensure Search Head Cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Ensure Indexers go to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

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

			// Check Cluster Master in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager in Monitoring Console Two Config Map after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Monitoring console Two is configured with all Indexer in Name Space
			testcaseEnvInst.Log.Info("Verify Indexers in Monitoring Console Pod TWO Config String after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcTwoName, true, true)

			// Check Deployer NOT in Monitoring Console TWO Config Map
			testcaseEnvInst.Log.Info("Verify DEPLOYER NOT on Monitoring Console TWO Config Map after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, false)

			// Check Monitoring Console TWO is NOT configured with Search Head in namespace
			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console TWO Config Map after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, false)

			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console TWO Pod after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcTwoName, false, false)

			// ##############  VERIFY MONITORING CONSOLE ONE AFTER CLUSTER MANAGER RECONFIG #######################

			// Verify Monitoring Console One Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Check Cluster Master Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager NOT in Monitoring Console One Config Map after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Verify Indexers NOT in Monitoring Console One Pod Config String after Cluster Master Reconfig")
			// testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, false, true)

			// Check Monitoring Console One is still configured with Search Head in namespace
			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console One Config Map after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Pod after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, true, false)

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

			// Check Cluster Master in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager on Monitoring Console Two Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Deployer in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Deployer on Monitoring Console Two Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, true)

			// Verify all Search Head Members are configured on Monitoring Console Two
			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Two Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, true)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Pod after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcTwoName, true, false)

			// Check Monitoring console Two is configured with all Indexer in Name Space
			testcaseEnvInst.Log.Info("Checking for Indexer Pod on MC TWO after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcTwoName, true, true)

			// ############################  VERIFICATOIN FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################

			// Verify MC ONE is Ready and stays in ready state before running verfications
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

			// Check Cluster Master Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager NOT in Monitoring Console One Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check DEPLOYER Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify DEPLOYER NOT in Monitoring Console One Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, false)

			// Verify all Search Head Members are Not configured on Monitoring Console One
			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console ONE Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, false)

			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console ONE Pod after Search Head Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, false, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Checking for Indexer Pod NOT on MC One after SHC Reconfig")
			// testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, false, true)
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

			// Ensure indexers go to Ready phase
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Ensure indexer clustered is configured as multisite
			testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

			// Ensure search head cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

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

			// Check Cluster Master in Monitoring Console Config Map
			testcaseEnvInst.Log.Info("Checking for Cluster Manager on MC TWO CONFIG MAP after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Monitoring Console TWO is configured with all Indexers in Name Space
			testcaseEnvInst.Log.Info("Checking for Indexer Pods on MC TWO POD after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcTwoName, true, true)

			// Check Monitoring console Two is NOT configured with all search head instances in namespace
			testcaseEnvInst.Log.Info("Checking for Search Head NOT CONFIGURED on MC TWO Config Map after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, false)

			testcaseEnvInst.Log.Info("Checking for Search Head NOT CONFIGURED on MC TWO Pod after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcTwoName, false, false)

			// Verify Monitoring Console One is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Check Cluster Master NOT configured on  Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Checking for Cluster Manager NOT in MC One Config Map after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Checking for Indexer Pods Not on MC one POD after Cluster Master RECONFIG")
			//testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, false, true)

			// Check Deployer in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Checking for Deployer in MC One Config Map after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Monitoring console  One is configured with all search head instances in namespace
			testcaseEnvInst.Log.Info("Checking for Search Head on MC ONE Config Map after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Checking for Search Head on MC ONE Pod after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, true, false)

		})
	})
})
