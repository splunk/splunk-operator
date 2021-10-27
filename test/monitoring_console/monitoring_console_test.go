// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package monitoringconsoletest

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Monitoring Console test", func() {

	var deployment *testenv.Deployment

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
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
			mc, err := deployment.DeployMonitoringConsole(deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Create Standalone Spec and apply
			standaloneOneName := deployment.GetName()
			mcName := deployment.GetName()
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}
			standaloneOne, err := deployment.DeployStandaloneWithGivenSpec(standaloneOneName, spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for standalone to be in READY Status
			testenv.StandaloneReady(deployment, deployment.GetName(), standaloneOne, testenvInstance)

			// Wait for MC to go to Updating Phase
			testenv.VerifyMonitoringConsolePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Standalone is configure in MC Config Map
			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

			testenvInstance.Log.Info("Checking for Standalone Pod on MC Config Map")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testenvInstance.Log.Info("Check standalone  instance in MC Peer list")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, true, false)

			// #########################  RECONFIGURE STANDALONE WITH SECOND MC #######################################

			// Reconfig S1 with 2nd Monitoring Console Name
			mcTwoName := deployment.GetName() + "-two"
			err = deployment.GetInstance(standaloneOneName, standaloneOne)
			Expect(err).To(Succeed(), "Unable to get instance of Standalone")
			standaloneOne.Spec.MonitoringConsoleRef.Name = mcTwoName

			// Update Standalone with 2nd MC
			err = deployment.UpdateCR(standaloneOne)
			Expect(err).To(Succeed(), "Unable to update Standalone with new MC Name")

			// Deploy 2nd MC Pod
			mcTwo, err := deployment.DeployMonitoringConsole(mcTwoName, "")
			Expect(err).To(Succeed(), "Unable to deploy Second Monitoring Console Pod")

			// Verify 2nd Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, mcTwoName, mcTwo, testenvInstance)

			// Check Standalone is configure in MC Config Map
			testenvInstance.Log.Info("Checking for Standalone Pod on SECOND MC Config Map after Standalone RECONFIG")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcTwoName, true)

			// Check Standalone Pod in MC Peer List
			testenvInstance.Log.Info("Check standalone  instance in SECOND MC Peer list after Standalone RECONFIG")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcTwoName, true, false)

			// Verify Monitoring Console One is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Standalone is not configured in MC ONE Config Map
			testenvInstance.Log.Info("Checking for Standalone Pod NOT ON FIRST MC Config Map after Standalone RECONFIG")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcName, false)

			// Check Standalone Pod Not in MC ONE Peer List
			testenvInstance.Log.Info("Check standalone NOT ON FIRST MC Peer list after Standalone RECONFIG")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, false, false)

		})
	})

	Context("Standalone deployment (S1)", func() {
		It("monitoringconsole, integration: can deploy a MC with standalone instance and update MC with new standalone deployment", func() {
			/*
				Test Steps
				1.  Deploy Standalone
				2.  Wait for Standalone to go to READY
				3.  Deploy Monitoring Console
				4.  Wait for Monitoring Console status to be READY
				5.  Verify Standalone configured in Monitoring Console Config Map
				6.  Verify Monitoring Console Pod has correct peers in Peer List
				7.  Deploy 2nd Standalone
				8.  Wait for Second Standalone to be READY
				9.  Wait for Monitoring Console status to go UPDATING then READY
				10. Verify both Standalone configured in Monitoring Console Config Map
				11. Verify both Standalone configured in Monitoring Console Pod Peers String
				12. Delete 2nd Standalone
				13. Wait for Monitoring Console to go to UPDATING then READY
				14. Verify only first Standalone configured in Monitoring Console Config Map
				15. Verify only first Standalone configured in Monitoring Console Pod Peers String
			*/

			standaloneOneName := deployment.GetName()
			mcName := deployment.GetName()
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}
			standaloneOne, err := deployment.DeployStandaloneWithGivenSpec(standaloneOneName, spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for standalone to be in READY Status
			testenv.StandaloneReady(deployment, deployment.GetName(), standaloneOne, testenvInstance)

			// Deploy MC and wait for MC to be READY
			mc, err := deployment.DeployMonitoringConsole(deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Standalone is configure in MC Config Map
			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

			testenvInstance.Log.Info("Checking for Standalone Pod on MC Config Map")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testenvInstance.Log.Info("Check standalone  instance in MC Peer list")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, true, false)

			// Check Standalone Pod in MC Peer List
			testenvInstance.Log.Info("Check standalone  instance in MC Peer list")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, true, false)
			// Add another standalone instance in namespace
			testenvInstance.Log.Info("Adding second standalone deployment to namespace")
			// CSPL-901 standaloneTwoName := deployment.GetName() + "-two"
			standaloneTwoName := "standalone-" + testenv.RandomDNSName(3)
			// Configure Resources on second standalone CSPL-555
			standaloneTwoSpec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"cpu":    resource.MustParse("2"),
								"memory": resource.MustParse("4Gi"),
							},
							Requests: corev1.ResourceList{
								"cpu":    resource.MustParse("0.2"),
								"memory": resource.MustParse("256Mi"),
							},
						},
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}
			standaloneTwo, err := deployment.DeployStandaloneWithGivenSpec(standaloneTwoName, standaloneTwoSpec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for standalone two to be in READY status
			testenv.StandaloneReady(deployment, standaloneTwoName, standaloneTwo, testenvInstance)

			// Wait for MC to go to Updating Phase
			testenv.VerifyMonitoringConsolePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Standalone is configure in MC Config Map
			standalonePods = append(standalonePods, fmt.Sprintf(testenv.StandalonePod, standaloneTwoName, 0))

			testenvInstance.Log.Info("Checking for Standalone Pod on MC Config Map after adding new standalone")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testenvInstance.Log.Info("Check standalone  instance in MC Peer list after adding new standalone")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, true, false)

			// Delete Standlone TWO of the standalone and ensure MC is updated
			testenvInstance.Log.Info("Deleting second standalone deployment to namespace", "Standalone Name", standaloneTwoName)
			deployment.GetInstance(standaloneTwoName, standaloneTwo)
			err = deployment.DeleteCR(standaloneTwo)
			Expect(err).To(Succeed(), "Unable to delete standalone instance", "Standalone Name", standaloneTwo)

			// Wait for MC to go to Updating Phase
			testenv.VerifyMonitoringConsolePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Standalone is configure in MC Config Map
			standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

			testenvInstance.Log.Info("Checking for Standalone One Pod in MC Config Map after deleting second standalone")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testenvInstance.Log.Info("Check Standalone One Pod in MC Peer list after deleting second standalone")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, true, false)

			// Check Standalone TWO NOT configured in MC Config Map
			standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneTwoName, 1, false, 0)

			testenvInstance.Log.Info("Checking for Standalone Two Pod NOT in MC Config Map after deleting second standalone")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcName, false)

			// Check Standalone Pod TWO NOT configured MC Peer List
			testenvInstance.Log.Info("Check Standalone Two Pod NOT in MC Peer list after deleting second standalone")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, false, false)

		})
	})

	Context("Standalone deployment with Scale up", func() {
		It("monitoringconsole, integration: can deploy a MC with standalone instance and update MC when standalone is scaled up", func() {
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
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}

			standalone, err := deployment.DeployStandaloneWithGivenSpec(standaloneName, spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for standalone to be in READY Status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Deploy MC and wait for MC to be READY
			mc, err := deployment.DeployMonitoringConsole(deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Standalone is configure in MC Config Map
			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneName, 1, false, 0)
			testenvInstance.Log.Info("Checking for Standalone Pod on MC Config Map")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testenvInstance.Log.Info("Check standalone  instance in MC Peer list")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, true, false)

			// Scale Standalone instance
			testenvInstance.Log.Info("Scaling Standalone CR")
			scaledReplicaCount := 2
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Failed to scale Standalone")

			// Ensure standalone is scaling up
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for MC to go to Updating Phase
			testenv.VerifyMonitoringConsolePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneName, 2, false, 0)

			// Check Standalone is configure in MC Config Map
			testenvInstance.Log.Info("Checking for Standalone Pod on MC Config Map")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

			// Check Standalone Pod in MC Peer List
			testenvInstance.Log.Info("Check standalone  instance in MC Peer list")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, standalonePods, mcName, true, false)
		})
	})

	// TO BE READDED WHEN CSPL-1379 fixed
	XContext("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("monitoringconsole, smoke: MC can configure SHC, indexer instances after scale up and standalone in a namespace", func() {
			/*
				Test Steps
				1. Deploy Single Site Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. Verify SH are configured in peer strings on MC Pod
				6. Verify Monitoring Console Pod has peers in Peer string on MC Pod
				7. Scale SH Cluster
				8. Scale Indexer Count on One Site
				9. Add a standalone
				10. Verify Standalone is configured in MC Config Map
				11. Verify SH are configured in MC Config Map and Peers String
				12. Verify Indexers are configured in Peer String
			*/

			defaultSHReplicas := 3
			defaultIndexerReplicas := 3
			mcName := deployment.GetName()
			err := deployment.DeploySingleSiteClusterWithGivenMonitoringConsole(deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Check Cluster Manager in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, true)

			// Check Deployer in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Search Head Pods in Monitoring Console Config Map
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			// Deploy Monitoring Console Pod
			mc, err := deployment.DeployMonitoringConsole(deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Monitoring console Pod is configured with all search head
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcName, true, true)

			// Scale Search Head Cluster
			scaledSHReplicas := defaultSHReplicas + 1
			testenvInstance.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)
			shcName := deployment.GetName() + "-shc"

			// Get instance of current SHC CR with latest config
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Update Replicas of SHC
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Failed to scale Search Head Cluster")

			// Ensure Search Head cluster scales up and go to ScalingUp phase
			testenv.VerifySearchHeadClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp)

			// Scale indexers
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testenvInstance.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)
			idxcName := deployment.GetName() + "-idxc"

			// Get instance of current Indexer CR with latest config
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Failed to scale Indxer Cluster")

			// Ensure Indxer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp, idxcName)

			// Deploy Standalone Pod
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}
			standalone, err := deployment.DeployStandaloneWithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Ensure Indexer cluster go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready Phase
			// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Wait for MC to go to Updating Phase
			testenv.VerifyMonitoringConsolePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Standalone configured on Monitoring Console
			testenvInstance.Log.Info("Checking for Standalone Pod on MC Config Map")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}, "SPLUNK_STANDALONE_URL", mcName, true)

			testenvInstance.Log.Info("Check standalone instance in MC Peer list")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}, mcName, true, false)

			// Verify all Search Head Members are configured on Monitoring Console
			shPods = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), scaledSHReplicas, false, 0)

			testenvInstance.Log.Info("Verify Search Head Pods on Monitoring Console Config Map after Scale Up")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testenvInstance.Log.Info("Verify Search Head Pods on Monitoring Console Pod after Scale Up")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			testenvInstance.Log.Info("Checking for Indexer Pod on MC after Scale Up")
			indexerPods = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas, false, 0)
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcName, true, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("monitoringconsole, integration: MC can configure SHC, indexer instances and reconfigure to new MC", func() {
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
			err := deployment.DeploySingleSiteClusterWithGivenMonitoringConsole(deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Check Cluster Manager in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, true)

			// Check Deployer in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Search Head Pods in Monitoring Console Config Map
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			// Deploy Monitoring Console Pod
			mc, err := deployment.DeployMonitoringConsole(deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Monitoring console Pod is configured with all search head
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcName, true, true)

			// #################  Update Monitoring Console In Cluster Manager CR ##################################

			mcTwoName := deployment.GetName() + "-two"
			cm := &enterpriseApi.ClusterMaster{}
			err = deployment.GetInstance(deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to get instance of Cluster Manager")

			cm.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(cm)
			Expect(err).To(Succeed(), "Failed to update mcRef in Cluster Manager")

			// Ensure Cluster Manager Goes to Updating Phase
			testenv.VerifyClusterManagerPhase(deployment, testenvInstance, splcommon.PhaseUpdating)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Deploy Monitoring Console Pod
			mcTwo, err := deployment.DeployMonitoringConsole(mcTwoName, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console TWO is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, mcTwoName, mcTwo, testenvInstance)

			// ###########   VERIFY MONITORING CONSOLE TWO AFTER CLUSTER MANAGER RECONFIG  ###################################

			// Check Cluster Manager in Monitoring Console Two Config Map
			testenvInstance.Log.Info("Verify Cluster Manager in Monitoring Console Two Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Monitoring console Two is configured with all Indexer in Name Space
			testenvInstance.Log.Info("Verify Indexers in Monitoring Console Pod TWO Config String after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcTwoName, true, true)

			// Check Deployer NOT in Monitoring Console TWO Config Map
			testenvInstance.Log.Info("Verify DEPLOYER NOT on Monitoring Console TWO Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, false)

			// Check Monitoring Console TWO is NOT configured with Search Head in namespace
			testenvInstance.Log.Info("Verify Search Head Pods NOT on Monitoring Console TWO Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, false)

			testenvInstance.Log.Info("Verify Search Head Pods NOT on Monitoring Console TWO Pod after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcTwoName, false, false)

			// ##############  VERIFY MONITORING CONSOLE ONE AFTER CLUSTER MANAGER RECONFIG #######################

			// Verify Monitoring Console One Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Cluster Manager Not in Monitoring Console One Config Map
			testenvInstance.Log.Info("Verify Cluster Manager NOT in Monitoring Console One Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check Monitoring console One is NOT configured with all Indexer in Name Space
			testenvInstance.Log.Info("Verify Indexers NOT in Monitoring Console One Pod Config String after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcName, false, true)

			// Check Monitoring Console One is still configured with Search Head in namespace
			testenvInstance.Log.Info("Verify Search Head Pods on Monitoring Console One Config Map after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testenvInstance.Log.Info("Verify Search Head Pods on Monitoring Console Pod after Cluster Manager Reconfig")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcName, true, false)

			// #################  Update Monitoring Console In SHC CR ##################################

			// Get instance of current SHC CR with latest config
			shc := &enterpriseApi.SearchHeadCluster{}
			shcName := deployment.GetName() + "-shc"
			err = deployment.GetInstance(shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Update SHC to use 2nd Montioring Console
			shc.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Failed to get update Monitoring Console in Search Head Cluster CRD")

			// Ensure Search Head Cluster go to Ready Phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, mcTwoName, mcTwo, testenvInstance)

			// ############################  VERIFICATOIN FOR MONITORING CONSOLE TWO POST SHC RECONFIG ###############################

			// Check Cluster Manager in Monitoring Console Two Config Map
			testenvInstance.Log.Info("Verify Cluster Manager on Monitoring Console Two Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Deployer in Monitoring Console Two Config Map
			testenvInstance.Log.Info("Verify Deployer on Monitoring Console Two Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, true)

			// Verify all Search Head Members are configured on Monitoring Console Two
			testenvInstance.Log.Info("Verify Search Head Pods on Monitoring Console Two Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, true)

			testenvInstance.Log.Info("Verify Search Head Pods on Monitoring Console Pod after SHC Reconfig")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcTwoName, true, false)

			// Check Monitoring console Two is configured with all Indexer in Name Space
			testenvInstance.Log.Info("Checking for Indexer Pod on MC TWO after SHC Reconfig")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcTwoName, true, true)

			// ############################  VERIFICATOIN FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################

			// Verify MC ONE is Ready and stays in ready state before running verfications
			testenv.VerifyMonitoringConsoleReady(deployment, mcName, mc, testenvInstance)

			// Check Cluster Manager Not in Monitoring Console One Config Map
			testenvInstance.Log.Info("Verify Cluster Manager NOT in Monitoring Console One Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check DEPLOYER Not in Monitoring Console One Config Map
			testenvInstance.Log.Info("Verify DEPLOYER NOT in Monitoring Console One Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, false)

			// Verify all Search Head Members are Not configured on Monitoring Console One
			testenvInstance.Log.Info("Verify Search Head Pods NOT on Monitoring Console ONE Config Map after SHC Reconfig")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, false)

			testenvInstance.Log.Info("Verify Search Head Pods NOT on Monitoring Console ONE Pod after Search Head Reconfig")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcName, false, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testenvInstance.Log.Info("Checking for Indexer Pod NOT on MC One after SHC Reconfig")
			// testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcName, false, true)
		})
	})

	Context("Multisite Clustered deployment (M4 - 3 Site clustered indexer, search head cluster)", func() {
		It("monitoring_console, integration: MC can configure SHC, indexer instances and reconfigure Cluster Manager to new Monitoring Console", func() {
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
			err := deployment.DeployMultisiteClusterWithMonitoringConsole(deployment.GetName(), defaultIndexerReplicas, siteCount, mcName, true)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure indexer clustered is configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Check Cluster Manager in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, true)

			// Check Deployer in Monitoring Console Config Map
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Deploy Monitoring Console Pod
			mc, err := deployment.DeployMonitoringConsole(deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Monitoring console is configured with all search head instances in namespace
			shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)

			testenvInstance.Log.Info("Checking for Search Head on MC Config Map")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testenvInstance.Log.Info("Checking for Search Head on MC Pod")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcName, true, false)

			// Check Monitoring console is configured with all Indexer in Name Space
			indexerPods := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, 3)
			testenvInstance.Log.Info("Checking for Indexer Pods on MC POD")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcName, true, true)

			// ############ CLUSTER MANAGER MC RECONFIG #################################
			mcTwoName := deployment.GetName() + "-two"
			cm := &enterpriseApi.ClusterMaster{}
			err = deployment.GetInstance(deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to get instance of Cluster Manager")

			cm.Spec.MonitoringConsoleRef.Name = mcTwoName
			err = deployment.UpdateCR(cm)
			Expect(err).To(Succeed(), "Failed to update mcRef in Cluster Manager")

			// Ensure Cluster Manager Goes to Updating Phase
			testenv.VerifyClusterManagerPhase(deployment, testenvInstance, splcommon.PhaseUpdating)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Deploy Monitoring Console Pod
			mcTwo, err := deployment.DeployMonitoringConsole(mcTwoName, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console Two instance")

			// Verify Monitoring Console TWO is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, mcTwoName, mcTwo, testenvInstance)

			// Check Cluster Manager in Monitoring Console Config Map
			testenvInstance.Log.Info("Checking for Cluster Manager on MC TWO CONFIG MAP after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcTwoName, true)

			// Check Monitoring Console TWO is configured with all Indexers in Name Space
			testenvInstance.Log.Info("Checking for Indexer Pods on MC TWO POD after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcTwoName, true, true)

			// Check Monitoring console Two is NOT configured with all search head instances in namespace
			testenvInstance.Log.Info("Checking for Search Head NOT CONFIGURED on MC TWO Config Map after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, false)

			testenvInstance.Log.Info("Checking for Search Head NOT CONFIGURED on MC TWO Pod after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcTwoName, false, false)

			// Verify Monitoring Console One is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Check Cluster Manager NOT configured on  Monitoring Console One Config Map
			testenvInstance.Log.Info("Checking for Cluster Manager NOT in MC One Config Map after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, false)

			// Check Monitoring console One is Not configured with all Indexer in Name Space
			// CSPL-619
			// testenvInstance.Log.Info("Checking for Indexer Pods Not on MC one POD after Cluster Manager RECONFIG")
			//testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, indexerPods, mcName, false, true)

			// Check Deployer in Monitoring Console One Config Map
			testenvInstance.Log.Info("Checking for Deployer in MC One Config Map after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, []string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)

			// Check Monitoring console  One is configured with all search head instances in namespace
			testenvInstance.Log.Info("Checking for Search Head on MC ONE Config Map after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigMap(deployment, testenvInstance, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

			testenvInstance.Log.Info("Checking for Search Head on MC ONE Pod after Cluster Manager RECONFIG")
			testenv.VerifyPodsInMCConfigString(deployment, testenvInstance, shPods, mcName, true, false)

		})
	})
})
