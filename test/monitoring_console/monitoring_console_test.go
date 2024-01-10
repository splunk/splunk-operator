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
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Monitoring Console test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", "master"+testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testcaseEnvInst.SkipTeardown = true
		}

		if deployment != nil {
			deployment.Teardown()
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
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

			// Deploy Monitoring Console Pod
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			err = deployment.DeploySingleSiteClusterMasterWithGivenMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			time.Sleep(60 * time.Second)

			// Check Cluster Master in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, true)

			// Check Deployer in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Search Head Pods in Monitoring Console Config Map
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)
			// Check Monitoring console Pod is configured with all search head
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcName, true, true)

			// Scale Search Head Cluster
			scaledSHReplicas := defaultSHReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)
			shcName := deployment.GetName() + "-shc"

			// Get instance of current SHC CR with latest config
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(ctx, shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Update Replicas of SHC
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to scale Search Head Cluster")

			// Ensure Search Head cluster scales up and go to ScalingUp phase
			testenv.VerifySearchHeadClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingUp)

			// Scale indexers
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)
			idxcName := deployment.GetName() + "-idxc"

			// Get instance of current Indexer CR with latest config
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance  of Indexer Cluster")

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to scale Indxer Cluster")

			// Ensure Indxer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingUp, idxcName)

			// get revision number of the resource
			resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Deploy Standalone Pod
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Ensure Indexer cluster go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready Phase
			// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Wait for MC to go to PENDING Phase
			//testenv.VerifyMonitoringConsolePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhasePending)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Check Standalone configured on Monitoring Console
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}, "SPLUNK_STANDALONE_URL", mcName, true)

			testcaseEnvInst.Log.Info("Check standalone instance in MC Peer list")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}, mcName, true, false)

			//time.Sleep(2 * time.Second)

			// Verify all Search Head Members are configured on Monitoring Console
			shPods = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), scaledSHReplicas, false, 0)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Config Map after Scale Up")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Pod after Scale Up")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			testcaseEnvInst.Log.Info("Checking for Indexer Pod on MC after Scale Up")
			indexerPods = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas, false, 0)
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcName, true, true)
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
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			err = deployment.DeploySingleSiteClusterMasterWithGivenMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Check Cluster Master in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, true)

			// Check Deployer in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Search Head Pods in Monitoring Console Config Map
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Check Monitoring console Pod is configured with all search head
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcName, true, true)

			// #################  Update Monitoring Console In Cluster Master CR ##################################

			mcTwoName := deployment.GetName() + "-two"
			cm := &enterpriseApiV3.ClusterMaster{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to get instance of Cluster Manager")

			// get revision number of the resource
			resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, cm)

			cm.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(ctx, cm)

			Expect(err).To(Succeed(), "Failed to update mcRef in Cluster Manager")

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, cm, resourceVersion)

			// Ensure Cluster Master Goes to Updating Phase
			//testenv.VerifyClusterMasterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseUpdating)

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Deploy Monitoring Console Pod
			mcTwo, err := deployment.DeployMonitoringConsole(ctx, mcTwoName, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console TWO is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo, testcaseEnvInst)

			// ###########   VERIFY MONITORING CONSOLE TWO AFTER CLUSTER MANAGER RECONFIG  ###################################

			// Check Cluster Master in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager in Monitoring Console Two Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Monitoring console Two is configured with all Indexer in Name Space
			testcaseEnvInst.Log.Info("Verify Indexers in Monitoring Console Pod TWO Config String after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcTwoName, true, true)

			// Check Deployer NOT in Monitoring Console TWO Config Map
			testcaseEnvInst.Log.Info("Verify DEPLOYER NOT on Monitoring Console TWO Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, false)

			// Check Monitoring Console TWO is NOT configured with Search Head in namespace
			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console TWO Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, false)

			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console TWO Pod after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcTwoName, false, false)

			// ##############  VERIFY MONITORING CONSOLE ONE AFTER CLUSTER MANAGER RECONFIG #######################

			// Verify Monitoring Console One Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Check Cluster Master Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager NOT in Monitoring Console One Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Verify Indexers NOT in Monitoring Console One Pod Config String after Cluster Master Reconfig")
			// testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcName, false, true)

			// Check Monitoring Console One is still configured with Search Head in namespace
			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console One Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Pod after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcName, true, false)

			// #################  Update Monitoring Console In SHC CR ##################################

			// Get instance of current SHC CR with latest config
			shc := &enterpriseApi.SearchHeadCluster{}
			shcName := deployment.GetName() + "-shc"
			err = deployment.GetInstance(ctx, shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Update SHC to use 2nd Montioring Console
			shc.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to get update Monitoring Console in Search Head Cluster CRD")

			// Ensure Search Head Cluster go to Ready Phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo, testcaseEnvInst)

			// ############################  VERIFICATOIN FOR MONITORING CONSOLE TWO POST SHC RECONFIG ###############################

			// Check Cluster Master in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager on Monitoring Console Two Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Deployer in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Deployer on Monitoring Console Two Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, true)

			// Verify all Search Head Members are configured on Monitoring Console Two
			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Two Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, true)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Pod after SHC Reconfig")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcTwoName, true, false)

			// Check Monitoring console Two is configured with all Indexer in Name Space
			testcaseEnvInst.Log.Info("Checking for Indexer Pod on MC TWO after SHC Reconfig")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcTwoName, true, true)

			// ############################  VERIFICATOIN FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################

			// Verify MC ONE is Ready and stays in ready state before running verfications
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc, testcaseEnvInst)

			// Check Cluster Master Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager NOT in Monitoring Console One Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check DEPLOYER Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify DEPLOYER NOT in Monitoring Console One Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, false)

			// Verify all Search Head Members are Not configured on Monitoring Console One
			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console ONE Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, false)

			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console ONE Pod after Search Head Reconfig")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcName, false, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Checking for Indexer Pod NOT on MC One after SHC Reconfig")
			// testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcName, false, true)
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
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure indexers go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure indexer clustered is configured as multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Check Cluster Master in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, true)

			// Check Deployer in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Deploy Monitoring Console Pod
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Check Monitoring console is configured with all search head instances in namespace
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)

			testcaseEnvInst.Log.Info("Checking for Search Head on MC Config Map")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Checking for Search Head on MC Pod")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, 3)
			testcaseEnvInst.Log.Info("Checking for Indexer Pods on MC POD")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcName, true, true)

			// ############ CLUSTER MANAGER MC RECONFIG #################################
			mcTwoName := deployment.GetName() + "-two"
			cm := &enterpriseApiV3.ClusterMaster{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to get instance of Cluster Manager")

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, cm)

			cm.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(ctx, cm)
			Expect(err).To(Succeed(), "Failed to update mcRef in Cluster Manager")

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, cm, resourceVersion)

			// Ensure Cluster Master Goes to Updating Phase
			//testenv.VerifyClusterMasterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseUpdating)

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Deploy Monitoring Console Pod
			mcTwo, err := deployment.DeployMonitoringConsole(ctx, mcTwoName, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console Two instance")

			// Verify Monitoring Console TWO is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo, testcaseEnvInst)

			// Check Cluster Master in Monitoring Console Config Map
			testcaseEnvInst.Log.Info("Checking for Cluster Manager on MC TWO CONFIG MAP after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Monitoring Console TWO is configured with all Indexers in Name Space
			testcaseEnvInst.Log.Info("Checking for Indexer Pods on MC TWO POD after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcTwoName, true, true)

			// Check Monitoring console Two is NOT configured with all search head instances in namespace
			testcaseEnvInst.Log.Info("Checking for Search Head NOT CONFIGURED on MC TWO Config Map after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, false)

			testcaseEnvInst.Log.Info("Checking for Search Head NOT CONFIGURED on MC TWO Pod after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcTwoName, false, false)

			// Verify Monitoring Console One is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Check Cluster Master NOT configured on  Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Checking for Cluster Manager NOT in MC One Config Map after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Checking for Indexer Pods Not on MC one POD after Cluster Master RECONFIG")
			//testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, indexerPods, mcName, false, true)

			// Check Deployer in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Checking for Deployer in MC One Config Map after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Monitoring console  One is configured with all search head instances in namespace
			testcaseEnvInst.Log.Info("Checking for Search Head on MC ONE Config Map after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(ctx, deployment, testcaseEnvInst, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Checking for Search Head on MC ONE Pod after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigString(ctx, deployment, testcaseEnvInst, shPods, mcName, true, false)

		})
	})
})
