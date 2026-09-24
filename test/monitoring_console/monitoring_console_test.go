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
package monitoringconsole

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
)

// masterManagerMCConfigs defines the V3 (master) and V4 (manager) variants
// shared by the C3 reconfig and M4 MC reconfig test tables.
var masterManagerMCConfigs = []testenv.MCVersionConfig{
	{
		MCReconfigParams: testenv.MCReconfigParams{CMServiceNameFmt: testenv.ClusterMasterServiceName, CMURLKey: "SPLUNK_CLUSTER_MASTER_URL"},
		NamePrefix:       "master",
		Label:            "master",
		DeployC3WithMC: func(ctx context.Context, d *testenv.Deployment, name string, replicas int, shc bool, mcRef string) error {
			return d.DeploySingleSiteClusterMasterWithGivenMonitoringConsole(ctx, name, replicas, shc, mcRef)
		},
		DeployM4WithMC: func(ctx context.Context, d *testenv.Deployment, name string, replicas int, siteCount int, mcRef string, shc bool) error {
			return d.DeployMultisiteClusterMasterWithMonitoringConsole(ctx, name, replicas, siteCount, mcRef, shc)
		},
		NewCMObject: func() client.Object { return &enterpriseApiV3.ClusterMaster{} },
		VerifyCMReady: func(ctx context.Context, d *testenv.Deployment, te *testenv.TestCaseEnv) error {
			return te.VerifyClusterMasterReady(ctx, d)
		},
		SHCReconfigTimeout:       0,
		VerifyMCTwoReadyAfterSHC: true,
	},
	{
		MCReconfigParams: testenv.MCReconfigParams{CMServiceNameFmt: testenv.ClusterManagerServiceName, CMURLKey: splcommon.ClusterManagerURL},
		NamePrefix:       "",
		Label:            "manager",
		DeployC3WithMC: func(ctx context.Context, d *testenv.Deployment, name string, replicas int, shc bool, mcRef string) error {
			return d.DeploySingleSiteClusterWithGivenMonitoringConsole(ctx, name, replicas, shc, mcRef)
		},
		DeployM4WithMC: func(ctx context.Context, d *testenv.Deployment, name string, replicas int, siteCount int, mcRef string, shc bool) error {
			return d.DeployMultisiteClusterWithMonitoringConsole(ctx, name, replicas, siteCount, mcRef, shc)
		},
		NewCMObject: func() client.Object { return &enterpriseApi.ClusterManager{} },
		VerifyCMReady: func(ctx context.Context, d *testenv.Deployment, te *testenv.TestCaseEnv) error {
			return te.VerifyClusterManagerReady(ctx, d)
		},
		SHCReconfigTimeout:       5 * time.Minute,
		VerifyMCTwoReadyAfterSHC: false,
	},
}

// C3 scale-up tests — V3 (master) and V4 (manager) variants
var _ = Describe("Monitoring Console C3 scale-up tests", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	for _, cfg := range masterManagerMCConfigs {
		cfg := cfg
		Context("Clustered deployment C3 scale-up ("+cfg.Label+")", func() {
			BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, cfg.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It("MC can configure SHC, indexer instances after scale up and standalone in a namespace", Label("tier:e2e-pr", "sva:c3", "cloud:aws", "variant:"+cfg.Label, "feature:monitoringconsole"), NodeTimeout(testenv.LongTimeout), func(ctx SpecContext) {
				RunC3MCScaleUpTest(ctx, deployment, testcaseEnvInst, cfg)
			})
		})
	}
})

// Manager (V4) Monitoring Console tests
var _ = Describe("Monitoring Console test (manager)", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	Context("Deploy Monitoring Console", func() {
		It("can deploy MC CR and can be configured standalone", Label("tier:e2e-pr", "cloud:aws", "feature:monitoringconsole"), NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
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
			Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed(), "MC version not changed or not ready")

			// Check Standalone is configured in MC
			testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
			Expect(testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, standaloneOneName, mcName, true)).To(Succeed(), "Standalone not configured in MC")

			// #########################  RECONFIGURE STANDALONE WITH SECOND MC #######################################

			// Reconfig S1 with 2nd Monitoring Console Name
			mcTwoName := deployment.GetName() + "-two"
			Expect(deployment.GetInstance(ctx, standaloneOneName, standaloneOne)).To(Succeed(), "Unable to get instance of Standalone")
			standaloneOne.Spec.MonitoringConsoleRef.Name = mcTwoName

			// Update Standalone with 2nd MC
			err = deployment.UpdateCR(ctx, standaloneOne)
			Expect(err).To(Succeed(), "Unable to update Standalone with new MC Name")

			// Deploy 2nd MC Pod
			_, err = testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console Two")

			// Check Standalone is configured in MC Two
			testcaseEnvInst.Log.Info("Checking for Standalone on SECOND MC after Standalone RECONFIG")
			Expect(testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, standaloneOneName, mcTwoName, true)).To(Succeed(), "Standalone not configured in MC Two after reconfig")

			// Verify Monitoring Console One is Ready and stays in ready state
			Expect(testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)).To(Succeed(), "MC One not ready after reconfig")

			// Check Standalone is NOT configured in MC One
			testcaseEnvInst.Log.Info("Checking for Standalone NOT ON FIRST MC after Standalone RECONFIG")
			Expect(testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, standaloneOneName, mcName, false)).To(Succeed(), "Standalone still configured in MC One after reconfig")

			Expect(testcaseEnvInst.VerifyStandaloneConditionReady(ctx, deployment, standaloneOne)).To(Succeed(), "Standalone Ready condition not met")
		})

	})

	Context("Standalone deployment (S1)", func() {
		It("can deploy a MC with standalone instance and update MC with new standalone deployment", Label("tier:e2e-full", "sva:s1", "cloud:aws", "variant:manager", "feature:monitoringconsole", "suite:mc1"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {
			RunS1StandaloneAddDeleteMCTest(ctx, deployment, testcaseEnvInst, deployment.GetName(), "standalone-"+testenv.RandomDNSName(3))
		})
	})

	Context("Standalone deployment with Scale up", func() {
		It("can deploy a MC with standalone instance and update MC when standalone is scaled up", Label("tier:e2e-full", "sva:s1", "cloud:aws", "variant:manager", "feature:monitoringconsole", "suite:mc1"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {
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
			Expect(testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, standaloneName, mcName, true)).To(Succeed(), "Standalone not configured in MC")

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Scale Standalone instance
			testcaseEnvInst.Log.Info("Scaling Standalone CR")
			scaledReplicaCount := 2
			standalone = &enterpriseApi.Standalone{}
			Expect(deployment.GetInstance(ctx, deployment.GetName(), standalone)).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale Standalone")

			// Ensure standalone reaches ScalingUp phase and returns to Ready
			Expect(testcaseEnvInst.VerifyStandalonePhaseAndReady(ctx, deployment, enterpriseApi.PhaseScalingUp, standalone)).To(Succeed(), "Standalone did not reach ScalingUp phase or not ready after scale up")

			// wait for custom resource resource version to change and verify MC is ready
			Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed(), "MC version not changed or not ready after scale up")

			standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneName, 2, false, 0)

			// Check both Standalone pods are configured in MC after scale up
			testcaseEnvInst.Log.Info("Checking for Standalone Pods on MC after scale up")
			Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)).To(Succeed(), "Standalone pods not found in MC after scale up")

			Expect(testcaseEnvInst.VerifyStandaloneConditionReady(ctx, deployment, standalone)).To(Succeed(), "Standalone Ready condition not met")
		})
	})

	Context("Standalone deployment (S1)", func() {
		It("can deploy a MC with standalone instance and update MC with new standalone deployment of similar names", Label("tier:e2e-full", "sva:s1", "cloud:aws", "variant:manager", "feature:monitoringconsole", "suite:mc2"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {
			RunS1StandaloneAddDeleteMCTest(ctx, deployment, testcaseEnvInst, "search-head-adhoc", "search-head")
		})
	})

})

// C3 reconfig and M4 tests — V3 (master) and V4 (manager) variants
var _ = Describe("Monitoring Console reconfig tests", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	// C3 reconfig tests
	for _, cfg := range masterManagerMCConfigs {
		cfg := cfg
		Context("Clustered deployment C3 reconfig ("+cfg.Label+")", func() {
			BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, cfg.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It("MC can configure SHC, indexer instances and reconfigure to new MC", Label("tier:e2e-full", "sva:c3", "cloud:aws", "variant:"+cfg.Label, "feature:monitoringconsole"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {
				RunC3MCReconfigTest(ctx, deployment, testcaseEnvInst, cfg)
			})
		})
	}

	// M4 reconfig tests
	for _, cfg := range masterManagerMCConfigs {
		cfg := cfg
		Context("Multisite Clustered deployment M4 reconfig ("+cfg.Label+")", func() {
			BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, cfg.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
				Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It("MC can configure SHC, indexer instances and reconfigure Cluster Manager to new Monitoring Console", Label("tier:e2e-full", "sva:m4", "cloud:aws", "variant:"+cfg.Label, "feature:monitoringconsole"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {
				RunM4MCReconfigTest(ctx, deployment, testcaseEnvInst, cfg)
			})
		})
	}
})
