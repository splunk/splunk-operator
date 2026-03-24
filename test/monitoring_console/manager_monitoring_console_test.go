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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Monitoring Console test", func() {

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
			mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Create Standalone Spec and apply
			standaloneOneName := deployment.GetName()
			mcName := deployment.GetName()
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "IfNotPresent",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}
			standaloneOne, err := deployment.DeployStandaloneWithGivenSpec(ctx, standaloneOneName, spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for standalone to be in READY Status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standaloneOne)

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)
			//testcaseEnvInst.VerifyMonitoringConsolePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)

			// Verify MC is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Check Standalone is configure in MC Config Map
			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testcaseEnvInst.Log.Info("Check standalone  instance in MC Peer list")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcName, true, false)

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

			// Check Standalone is configure in MC Config Map
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on SECOND MC Config Map after Standalone RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcTwoName, true)

			// Check Standalone Pod in MC Peer List
			testcaseEnvInst.Log.Info("Check standalone  instance in SECOND MC Peer list after Standalone RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcTwoName, true, false)

			// Verify Monitoring Console One is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Check Standalone is not configured in MC ONE Config Map
			testcaseEnvInst.Log.Info("Checking for Standalone Pod NOT ON FIRST MC Config Map after Standalone RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcName, false)

			// Check Standalone Pod Not in MC ONE Peer List
			testcaseEnvInst.Log.Info("Check standalone NOT ON FIRST MC Peer list after Standalone RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcName, false, false)

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
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "IfNotPresent",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}

			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, standaloneName, spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for standalone to be in READY Status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

			// Deploy MC and wait for MC to be READY
			mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

			// Check Standalone is configure in MC Config Map
			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneName, 1, false, 0)
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testcaseEnvInst.Log.Info("Check standalone  instance in MC Peer list")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcName, true, false)

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

			// Check Standalone is configure in MC Config Map
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testcaseEnvInst.Log.Info("Check standalone  instance in MC Peer list")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcName, true, false)
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
			mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

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
			testcaseEnvInst.VerifySearchHeadClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp)

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
			testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp, idxcName)

			// get revision number of the resource
			resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Deploy Standalone Pod
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "IfNotPresent",
						Image:           testcaseEnvInst.GetSplunkImage(),
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
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}, "SPLUNK_STANDALONE_URL", mcName, true)

			testcaseEnvInst.Log.Info("Check standalone instance in MC Peer list")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}, mcName, true, false)

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
			mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

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

			// Check Cluster Manager in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager in Monitoring Console Two Config Map after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcTwoName, true)

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

			// Check Cluster Manager Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager NOT in Monitoring Console One Config Map after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcName, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Verify Indexers NOT in Monitoring Console One Pod Config String after Cluster Manager Reconfig")
			// testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, false, true)

			// Check Monitoring Console One is still configured with Search Head in namespace
			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console One Config Map after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Pod after Cluster Manager Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, true, false)

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
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Verify MC is Ready and stays in ready state
			// testenv.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo, testcaseEnvInst)

			// ############################  VERIFICATION FOR MONITORING CONSOLE TWO POST SHC RECONFIG ###############################

			// Check Cluster Manager in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager on Monitoring Console Two Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcTwoName, true)

			// Check Deployer in Monitoring Console Two Config Map
			testcaseEnvInst.Log.Info("Verify Deployer on Monitoring Console Two Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, true)

			// Verify all Search Head Members are configured on Monitoring Console Two
			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Two Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, true)

			testcaseEnvInst.Log.Info("Verify Search Head Pods on Monitoring Console Pod after SHC Reconfig")
			err = testcaseEnvInst.WaitForPodsInMCConfigString(ctx, deployment, shPods, mcTwoName, true, false, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for search heads in MC two config after SHC reconfig")

			// Check Monitoring console Two is configured with all Indexer in Name Space
			testcaseEnvInst.Log.Info("Checking for Indexer Pod on MC TWO after SHC Reconfig")
			err = testcaseEnvInst.WaitForPodsInMCConfigString(ctx, deployment, indexerPods, mcTwoName, true, true, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for indexers in MC two config after SHC reconfig")

			// ############################  VERIFICATION FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################

			// Verify MC ONE is Ready and stays in ready state before running verfications
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

			// Check Cluster Manager Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify Cluster Manager NOT in Monitoring Console One Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcName, false)

			// Check DEPLOYER Not in Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Verify DEPLOYER NOT in Monitoring Console One Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, false)

			// Verify all Search Head Members are Not configured on Monitoring Console One
			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console ONE Config Map after SHC Reconfig")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, false)

			testcaseEnvInst.Log.Info("Verify Search Head Pods NOT on Monitoring Console ONE Pod after Search Head Reconfig")
			err = testcaseEnvInst.WaitForPodsInMCConfigString(ctx, deployment, shPods, mcName, false, false, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for search heads to be removed from MC one config after SHC reconfig")

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Checking for Indexer Pod NOT on MC One after SHC Reconfig")
			// testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, false, true)
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

			// Check Cluster Manager in Monitoring Console Config Map
			testcaseEnvInst.Log.Info("Checking for Cluster Manager on MC TWO CONFIG MAP after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcTwoName, true)

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

			// Check Cluster Manager NOT configured on  Monitoring Console One Config Map
			testcaseEnvInst.Log.Info("Checking for Cluster Manager NOT in MC One Config Map after Cluster Manager RECONFIG")
			testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterManagerServiceName, deployment.GetName())}, splcommon.ClusterManagerURL, mcName, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testcaseEnvInst.Log.Info("Checking for Indexer Pods Not on MC one POD after Cluster Manager RECONFIG")
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

	Context("Standalone deployment (S1)", func() {
		It("managermc2, integration: can deploy a MC with standalone instance and update MC with new standalone deployment of similar names", func() {
			RunS1StandaloneAddDeleteMCTest(ctx, deployment, testcaseEnvInst, "search-head-adhoc", "search-head")
		})
	})

})
