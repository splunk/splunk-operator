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
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
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
		if !testenvInstance.SkipTeardown {
			testenv.DeleteMCPod(testenvInstance.GetName())
		}
		if deployment != nil {
			deployment.Teardown()
		}
	})

	Context("Standalone deployment (S1)", func() {
		It("monitoring_console: can deploy a MC with standalone instance and update MC with new standalone deployment", func() {

			standaloneOneName := deployment.GetName()
			standaloneOne, err := deployment.DeployStandalone(standaloneOneName)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Wait for standalone to be in READY Status
			testenv.StandaloneReady(deployment, deployment.GetName(), standaloneOne, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all standalone instances in namespace
			peerList := testenv.GetConfiguredPeers(testenvInstance.GetName())
			testenvInstance.Log.Info("Peer List", "instance", peerList)

			// Only 1 peer expected in MC peer list
			Expect(len(peerList)).To(Equal(1))

			podName := fmt.Sprintf(testenv.StandalonePod, standaloneOneName, 0)
			testenvInstance.Log.Info("Check standalone instance in MC Peer list", "Standalone Pod", podName, "Peer in peer list", peerList[0])
			Expect(strings.Contains(peerList[0], podName)).To(Equal(true))

			// Add another standalone instance in namespace
			testenvInstance.Log.Info("Adding second standalone deployment to namespace")
			// CSPL-901 standaloneTwoName := deployment.GetName() + "-two"
			standaloneTwoName := "standalone-" + testenv.RandomDNSName(3)
			// Configure Resources on second standalone CSPL-555
			standaloneTwoSpec := enterprisev1.StandaloneSpec{
				CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
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
				},
			}
			standaloneTwo, err := deployment.DeployStandalonewithGivenSpec(standaloneTwoName, standaloneTwoSpec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for standalone two to be in READY status
			testenv.StandaloneReady(deployment, standaloneTwoName, standaloneTwo, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all standalone instances in namespace
			peerList = testenv.GetConfiguredPeers(testenvInstance.GetName())
			testenvInstance.Log.Info("Peer List", "instance", peerList)

			// Only 2 peers expected in MC peer list
			Expect(len(peerList)).To(Equal(2))

			// Verify Pod Name in Peer List
			podNameOne := fmt.Sprintf(testenv.StandalonePod, standaloneOneName, 0)
			podNameTwo := fmt.Sprintf(testenv.StandalonePod, standaloneTwoName, 0)
			testenvInstance.Log.Info("Checking Standalone on MC", "Standalone POD Name", podNameOne)
			Expect(testenv.CheckPodNameOnMC(testenvInstance.GetName(), podNameOne), true)
			testenvInstance.Log.Info("Checking Standalone on MC", "Standalone POD Name", podNameTwo)
			Expect(testenv.CheckPodNameOnMC(testenvInstance.GetName(), podNameTwo), true)

			// Delete Standlone TWO of the standalone and ensure MC is updated
			testenvInstance.Log.Info("Deleting second standalone deployment to namespace", "Standalone Name", standaloneTwoName)
			deployment.GetInstance(standaloneTwoName, standaloneTwo)
			err = deployment.DeleteCR(standaloneTwo)
			Expect(err).To(Succeed(), "Unable to delete standalone instance", "Standalone Name", standaloneTwo)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all standalone instances in namespace
			peerList = testenv.GetConfiguredPeers(testenvInstance.GetName())
			testenvInstance.Log.Info("Peer List", "instance", peerList)

			// Only 1 peer expected in MC peer list
			Expect(len(peerList)).To(Equal(1))

			podName = fmt.Sprintf(testenv.StandalonePod, standaloneOneName, 0)
			testenvInstance.Log.Info("Check standalone instance in MC Peer list", "Standalone Pod", podName, "Peer in peer list", peerList[0])
			Expect(strings.Contains(peerList[0], podName)).To(Equal(true))
		})
	})

	Context("Standalone deployment with Scale up", func() {
		It("monitoring_console: can deploy a MC with standalone instance and update MC when standalone is scaled up", func() {

			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all standalone instances in namespace
			peerList := testenv.GetConfiguredPeers(testenvInstance.GetName())
			testenvInstance.Log.Info("Peer List", "instance", peerList)

			// Only 1 peer expected in MC peer list
			Expect(len(peerList)).To(Equal(1))

			// Check spluk standlone pods are configured in MC peer list
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenvInstance.Log.Info("Check standalone instance in MC Peer list", "Standalone Pod", podName, "Peer in peer list", peerList[0])
			Expect(testenv.CheckPodNameOnMC(testenvInstance.GetName(), podName)).To(Equal(true))

			// Scale Standalone instance
			testenvInstance.Log.Info("Scaling Standalone CR")
			scaledReplicaCount := 2
			standalone = &enterprisev1.Standalone{}
			err = deployment.GetInstance(deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Failed to scale Standalone")

			// Ensure standalone is scaling up
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Only 2 peer expected in MC peer list
			peerList = testenv.GetConfiguredPeers(testenvInstance.GetName())
			testenvInstance.Log.Info("Peers in configuredPeer List", "count", len(peerList))
			Expect(len(peerList)).To(Equal(2))

			// Verify Pod Name in Peer List
			podNameTwo := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 1)
			testenvInstance.Log.Info("Checking Standalone on MC", "Standalone POD Name", podName)
			Expect(testenv.CheckPodNameOnMC(testenvInstance.GetName(), podName), true)
			testenvInstance.Log.Info("Checking Standalone on MC", "Standalone POD Name", podNameTwo)
			Expect(testenv.CheckPodNameOnMC(testenvInstance.GetName(), podNameTwo), true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("monitoring_console: MC can configure SHC, indexer instances after scale up and standalone in a namespace", func() {

			defaultSHReplicas := 3
			defaultIndexerReplicas := 3
			err := deployment.DeploySingleSiteCluster(deployment.GetName(), defaultIndexerReplicas, true)
			Expect(err).To(Succeed(), "Unable to deploy search head cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all search head instances in namespace
			for i := 0; i < defaultSHReplicas; i++ {
				podName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				testenvInstance.Log.Info("Checking for Search Head on MC", "Search Head Name", podName)
				found := testenv.CheckPodNameOnMC(testenvInstance.GetName(), podName)
				Expect(found).To(Equal(true))
			}

			// Check Monitoring console is configured with all Indexer in Name Space
			for i := 0; i < defaultIndexerReplicas; i++ {
				podName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				podIP := testenv.GetPodIP(testenvInstance.GetName(), podName)
				testenvInstance.Log.Info("Checking for Indexer Pod on MC", "Search Head Name", podName, "IP Address", podIP)
				found := testenv.CheckPodNameOnMC(testenvInstance.GetName(), podIP)
				Expect(found).To(Equal(true))
			}

			// Scale Search Head Cluster
			scaledSHReplicas := defaultSHReplicas + 1
			testenvInstance.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)
			shcName := deployment.GetName() + "-shc"

			// Get instance of current SHC CR with latest config
			shc := &enterprisev1.SearchHeadCluster{}
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
			idxc := &enterprisev1.IndexerCluster{}
			err = deployment.GetInstance(idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Failed to scale Indxer Cluster")

			// Ensure Indxer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp, idxcName)

			// Deploy Standalone
			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Ensure Indexer cluster go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready Phase
			// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Standalone configured on Monitoring Console
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenvInstance.Log.Info("Check standalone instance in MC Peer list", "Standalone Pod", podName)
			Expect(testenv.CheckPodNameOnMC(testenvInstance.GetName(), podName)).To(Equal(true))

			// Verify all Search Head Members are configured on Monitoring Console
			for i := 0; i < scaledSHReplicas; i++ {
				podName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				testenvInstance.Log.Info("Checking for Search Head on MC after adding Standalone", "Search Head Name", podName)
				found := testenv.CheckPodNameOnMC(testenvInstance.GetName(), podName)
				Expect(found).To(Equal(true))
			}

			// Check Monitoring console is configured with all Indexer in Name Space
			for i := 0; i < scaledIndexerReplicas; i++ {
				podName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				podIP := testenv.GetPodIP(testenvInstance.GetName(), podName)
				testenvInstance.Log.Info("Checking for Indexer Pod on MC", "Search Head Name", podName, "IP Address", podIP)
				found := testenv.CheckPodNameOnMC(testenvInstance.GetName(), podIP)
				Expect(found).To(Equal(true))
			}
		})
	})
})
