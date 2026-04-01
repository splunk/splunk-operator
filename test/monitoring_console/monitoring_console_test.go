// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

// masterManagerMCConfigs defines the V3 (master) and V4 (manager) variants
// shared by the C3 reconfig and M4 MC reconfig test tables.
var masterManagerMCConfigs = []testenv.MCVersionConfig{
	{
		MCReconfigParams: testenv.MCReconfigParams{CMServiceNameFmt: testenv.ClusterMasterServiceName, CMURLKey: "SPLUNK_CLUSTER_MASTER_URL"},
		NamePrefix:       "master",
		Label:            "mastermc",
		DeployC3WithMC: func(ctx context.Context, d *testenv.Deployment, name string, replicas int, shc bool, mcRef string) error {
			return d.DeploySingleSiteClusterMasterWithGivenMonitoringConsole(ctx, name, replicas, shc, mcRef)
		},
		DeployM4WithMC: func(ctx context.Context, d *testenv.Deployment, name string, replicas int, siteCount int, mcRef string, shc bool) error {
			return d.DeployMultisiteClusterMasterWithMonitoringConsole(ctx, name, replicas, siteCount, mcRef, shc)
		},
		NewCMObject: func() interface{} { return &enterpriseApiV3.ClusterMaster{} },
		VerifyCMReady: func(ctx context.Context, te *testenv.TestCaseEnv, d *testenv.Deployment) {
			te.VerifyClusterMasterReady(ctx, d)
		},
		SHCReconfigTimeout:       0,
		VerifyMCTwoReadyAfterSHC: true,
	},
	{
		MCReconfigParams: testenv.MCReconfigParams{CMServiceNameFmt: testenv.ClusterManagerServiceName, CMURLKey: splcommon.ClusterManagerURL},
		NamePrefix:       "",
		Label:            "managermc",
		DeployC3WithMC: func(ctx context.Context, d *testenv.Deployment, name string, replicas int, shc bool, mcRef string) error {
			return d.DeploySingleSiteClusterWithGivenMonitoringConsole(ctx, name, replicas, shc, mcRef)
		},
		DeployM4WithMC: func(ctx context.Context, d *testenv.Deployment, name string, replicas int, siteCount int, mcRef string, shc bool) error {
			return d.DeployMultisiteClusterWithMonitoringConsole(ctx, name, replicas, siteCount, mcRef, shc)
		},
		NewCMObject: func() interface{} { return &enterpriseApi.ClusterManager{} },
		VerifyCMReady: func(ctx context.Context, te *testenv.TestCaseEnv, d *testenv.Deployment) {
			te.VerifyClusterManagerReady(ctx, d)
		},
		SHCReconfigTimeout:       5 * time.Minute,
		VerifyMCTwoReadyAfterSHC: false,
	},
}

// Master (V3) Monitoring Console tests
var _ = Describe("Monitoring Console test (master)", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "master")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed())
	})

	Context("Clustered deployment (C3 - Clustered Indexer, Search Head Cluster)", func() {
		It("mastermc, smoke: MC can configure SHC, indexer instances after scale up and standalone in a namespace", func() {
			/*
				Test Steps
				1. Deploy Single Site Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. Verify Monitoring Console Pod has Search Heads in Peer strings
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
			mc, resourceVersion, err := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Deploy and verify C3 cluster with MC
			testcaseEnvInst.DeployAndVerifyC3WithMC(ctx, deployment, deployment.GetName(), defaultIndexerReplicas, mcName)

			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			// Wait for Cluster Master to appear in Monitoring Console Config Map
			err = testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(testenv.ClusterMasterServiceName, deployment.GetName())}, "SPLUNK_CLUSTER_MASTER_URL", mcName, true, 2*time.Minute)
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

			// Ensure Indexer Cluster goes to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// Ensure Search Head Cluster goes to Ready Phase
			// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// wait for custom resource resource version to change and verify MC is ready
			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			// Verify Standalone configured on Monitoring Console
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, deployment.GetName(), mcName, true)

			// Verify MC configuration after scale up
			testcaseEnvInst.Log.Info("Verify MC configuration after Scale Up")
			testcaseEnvInst.VerifyMCConfigForC3Cluster(ctx, deployment, deployment.GetName(), mcName, scaledSHReplicas, scaledIndexerReplicas, true)
		})
	})
})

// Manager (V4) Monitoring Console tests
var _ = Describe("Monitoring Console test (manager)", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed())
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
			mc, resourceVersion, err := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Create Standalone Spec and apply
			standaloneOneName := deployment.GetName()
			mcName := deployment.GetName()
			standaloneOne, err := testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, standaloneOneName, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Standalone with MC reference")

			// wait for custom resource resource version to change and verify MC is ready
			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			// Check Standalone is configured in MC
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, standaloneOneName, mcName, true)

			// #########################  RECONFIGURE STANDALONE WITH SECOND MC #######################################

			// Reconfig S1 with 2nd Monitoring Console Name
			mcTwoName := deployment.GetName() + "-two"
			testenv.GetInstanceWithExpect(ctx, deployment, standaloneOne, standaloneOneName, "Unable to get instance of Standalone")
			standaloneOne.Spec.MonitoringConsoleRef.Name = mcTwoName

			// Update Standalone with 2nd MC
			err = deployment.UpdateCR(ctx, standaloneOne)
			Expect(err).To(Succeed(), "Unable to update Standalone with new MC Name")

			// Deploy 2nd MC Pod
			_, err = testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console Two")

			// Check Standalone is configured in MC Two
			testcaseEnvInst.Log.Info("Checking for Standalone on SECOND MC after Standalone RECONFIG")
			testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, standaloneOneName, mcTwoName, true)

			// Verify Monitoring Console One is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Check Standalone is NOT configured in MC One
			testcaseEnvInst.Log.Info("Checking for Standalone NOT ON FIRST MC after Standalone RECONFIG")
			testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, standaloneOneName, mcName, false)

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
			standalone, err := testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, standaloneName, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Standalone with MC reference")

			// Deploy MC and wait for MC to be READY
			mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Check Standalone is configured in MC
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, standaloneName, mcName, true)

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Scale Standalone instance
			testcaseEnvInst.Log.Info("Scaling Standalone CR")
			scaledReplicaCount := 2
			standalone = &enterpriseApi.Standalone{}
			testenv.GetInstanceWithExpect(ctx, deployment, standalone, deployment.GetName(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale Standalone")

			// Ensure standalone is scaling up
			testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

			// wait for custom resource resource version to change and verify MC is ready
			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneName, 2, false, 0)

			// Check both Standalone pods are configured in MC after scale up
			testcaseEnvInst.Log.Info("Checking for Standalone Pods on MC after scale up")
			testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)
		})
	})

	Context("Clustered deployment (C3 - Clustered Indexer, Search Head Cluster)", func() {
		It("managermc, smoke: MC can configure SHC, indexer instances after scale up and standalone in a namespace", func() {
			/*
				Test Steps
				1. Deploy Single Site Indexer Cluster
				2. Deploy Monitoring Console
				3. Wait for Monitoring Console status to go back to READY
				4. Verify SH are configured in MC Config Map
				5. Verify Monitoring Console Pod has Search Heads in Peer strings
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
			mc, resourceVersion, err := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			err = deployment.DeploySingleSiteClusterWithGivenMonitoringConsole(ctx, deployment.GetName(), defaultIndexerReplicas, true, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Ensure C3 cluster is ready
			testcaseEnvInst.VerifyC3ClusterReady(ctx, deployment, testcaseEnvInst.VerifyClusterManagerReady)

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
			testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, deployment.GetName(), mcName)

			// Ensure Indexer Cluster goes to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// Ensure Search Head Cluster goes to Ready Phase
			// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// wait for custom resource resource version to change and verify MC is ready
			testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

			// Check Standalone configured on Monitoring Console
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, deployment.GetName(), mcName, true)

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

	Context("Standalone deployment (S1)", func() {
		It("managermc2, integration: can deploy a MC with standalone instance and update MC with new standalone deployment of similar names", func() {
			RunS1StandaloneAddDeleteMCTest(ctx, deployment, testcaseEnvInst, "search-head-adhoc", "search-head")
		})
	})

})

// C3 reconfig and M4 tests — V3 (master) and V4 (manager) variants
var _ = Describe("Monitoring Console reconfig tests", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	// C3 reconfig tests
	for _, cfg := range masterManagerMCConfigs {
		cfg := cfg
		Context("Clustered deployment C3 reconfig ("+cfg.Label+")", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, cfg.NamePrefix)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed())
			})

			It(cfg.Label+", integration: MC can configure SHC, indexer instances and reconfigure to new MC", func() {
				RunC3MCReconfigTest(ctx, deployment, testcaseEnvInst, cfg)
			})
		})
	}

	// M4 reconfig tests
	for _, cfg := range masterManagerMCConfigs {
		cfg := cfg
		Context("Multisite Clustered deployment M4 reconfig ("+cfg.Label+")", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, cfg.NamePrefix)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed())
			})

			It(cfg.Label+", integration: MC can configure SHC, indexer instances and reconfigure Cluster Manager to new Monitoring Console", func() {
				RunM4MCReconfigTest(ctx, deployment, testcaseEnvInst, cfg)
			})
		})
	}
})
