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
	"fmt"

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// RunM4MCReconfigTest deploys an M4 multisite cluster with a Monitoring Console,
// verifies the MC configuration, then reconfigures the Cluster Manager to point
// to a second MC and verifies both MCs are updated correctly.
func RunM4MCReconfigTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, cfg testenv.MCVersionConfig) {
	defaultSHReplicas := 3
	defaultIndexerReplicas := 1
	siteCount := 3
	mcName := deployment.GetName()

	err := cfg.DeployM4WithMC(ctx, deployment, deployment.GetName(), defaultIndexerReplicas, siteCount, mcName, true)
	Expect(err).To(Succeed(), "Unable to deploy multisite cluster")

	// Ensure cluster coordinator and all M4 components are ready
	Expect(testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount, func() error {
		return cfg.VerifyCMReady(ctx, deployment, testcaseEnvInst)
	})).To(Succeed(), "M4 components not ready")

	// Deploy and verify Monitoring Console
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Generate pod name slices for verification
	shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
	indexerPods := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), defaultIndexerReplicas, true, siteCount)

	// Verify MC configuration for M4 cluster
	Expect(testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, mcName, shPods, indexerPods)).To(Succeed(), "MC config verification failed for M4 cluster")

	// ############ CLUSTER MANAGER MC RECONFIG #################################
	mcTwoName, _, err := testenv.ReconfigCMWithNewMC(ctx, deployment, testcaseEnvInst, cfg)
	Expect(err).To(Succeed(), "Unable to reconfig CM with new MC")

	Expect(testenv.VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, shPods, indexerPods, false)).To(Succeed(), "MC Two verification failed after CM reconfig")
	Expect(testenv.VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcName, mc, shPods, true)).To(Succeed(), "MC One verification failed after CM reconfig")

	Expect(testcaseEnvInst.VerifyM4ConditionsReady(ctx, deployment, siteCount)).To(Succeed(), "M4 Ready conditions not met")
}

// c3MCSetupResult holds the common state produced by deployAndVerifyC3WithMC.
type c3MCSetupResult struct {
	mc              *enterpriseApi.MonitoringConsole
	mcName          string
	shPods          []string
	indexerPods     []string
	shReplicas      int
	indexerReplicas int
}

// deployAndVerifyC3WithMC deploys a C3 cluster with a Monitoring Console, waits
// for everything to be ready, and verifies the MC configuration. This is the
// common setup shared by RunC3MCReconfigTest and RunC3MCScaleUpTest.
func deployAndVerifyC3WithMC(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, cfg testenv.MCVersionConfig) c3MCSetupResult {
	shReplicas := 3
	indexerReplicas := 3
	mcName := deployment.GetName()

	mc, resourceVersion, err := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	err = cfg.DeployC3WithMC(ctx, deployment, deployment.GetName(), indexerReplicas, true, mcName)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	Expect(testcaseEnvInst.VerifyC3ClusterReady(ctx, deployment, func(ctx2 context.Context, d *testenv.Deployment) error {
		return cfg.VerifyCMReady(ctx2, d, testcaseEnvInst)
	})).To(Succeed(), "C3 cluster not ready")

	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed(), "MC version not changed or not ready")

	shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 0)
	indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 0)
	Expect(testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, mcName, shPods, indexerPods)).To(Succeed(), "MC config verification failed for C3 cluster")

	return c3MCSetupResult{mc: mc, mcName: mcName, shPods: shPods, indexerPods: indexerPods, shReplicas: shReplicas, indexerReplicas: indexerReplicas}
}

// RunC3MCReconfigTest deploys a C3 single-site cluster with a Monitoring Console,
// verifies the MC configuration, then reconfigures the Cluster Manager and SHC
// to point to a second MC and verifies both MCs are updated correctly.
func RunC3MCReconfigTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, cfg testenv.MCVersionConfig) {
	setup := deployAndVerifyC3WithMC(ctx, deployment, testcaseEnvInst, cfg)

	// Verify Monitoring Console is Ready and stays in ready state
	Expect(testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), setup.mc)).To(Succeed(), "Monitoring Console not ready")

	// #################  Update Monitoring Console In Cluster Manager CR ##################################

	mcTwoName, mcTwo, err := testenv.ReconfigCMWithNewMC(ctx, deployment, testcaseEnvInst, cfg)
	Expect(err).To(Succeed(), "Unable to reconfig CM with new MC")
	Expect(testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)).To(Succeed(), "Indexers not ready after CM MC reconfig")

	// ###########   VERIFY MONITORING CONSOLE TWO AFTER CLUSTER MANAGER RECONFIG  ###################################
	Expect(testenv.VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, setup.shPods, setup.indexerPods, true)).To(Succeed(), "MC Two verification failed after CM reconfig")

	// ##############  VERIFY MONITORING CONSOLE ONE AFTER CLUSTER MANAGER RECONFIG #######################
	Expect(testenv.VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, setup.mcName, setup.mc, setup.shPods, false)).To(Succeed(), "MC One verification failed after CM reconfig")

	// #################  Update Monitoring Console In SHC CR ##################################

	shc := &enterpriseApi.SearchHeadCluster{}
	shcName := deployment.GetName() + "-shc"
	Expect(testcaseEnvInst.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, shc, shcName, mcTwoName)).To(Succeed(), "Unable to update SHC MC ref")

	// Ensure Search Head Cluster goes to Ready Phase
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed(), "Search Head Cluster not ready after SHC MC reconfig")

	if cfg.VerifyMCTwoReadyAfterSHC {
		Expect(testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo)).To(Succeed(), "MC Two not ready after SHC reconfig")
	}

	// ############################  VERIFICATION FOR MONITORING CONSOLE TWO POST SHC RECONFIG ###############################
	Expect(testenv.VerifyMCTwoAfterSHCReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, setup.shPods, setup.indexerPods, cfg.SHCReconfigTimeout)).To(Succeed(), "MC Two verification failed after SHC reconfig")

	// ############################  VERIFICATION FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################
	Expect(testenv.VerifyMCOneAfterSHCReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, setup.mcName, setup.mc, setup.shPods, cfg.SHCReconfigTimeout)).To(Succeed(), "MC One verification failed after SHC reconfig")

	Expect(testcaseEnvInst.VerifyC3ConditionsReady(ctx, deployment)).To(Succeed(), "C3 Ready conditions not met")
}

// RunC3MCScaleUpTest deploys a C3 cluster with a Monitoring Console, verifies MC
// configuration, scales SHC and indexers, adds a standalone, and verifies the MC
// is updated correctly after scale up. Works for both V3 (master) and V4 (manager).
func RunC3MCScaleUpTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, cfg testenv.MCVersionConfig) {
	setup := deployAndVerifyC3WithMC(ctx, deployment, testcaseEnvInst, cfg)

	// Scale Search Head Cluster
	scaledSHReplicas := setup.shReplicas + 1
	testcaseEnvInst.Log.Info("Scaling up Search Head Cluster", "Current Replicas", setup.shReplicas, "New Replicas", scaledSHReplicas)
	Expect(testcaseEnvInst.ScaleSearchHeadCluster(ctx, deployment, scaledSHReplicas)).To(Succeed(), "Unable to scale Search Head Cluster")

	// Scale indexers
	scaledIndexerReplicas := setup.indexerReplicas + 1
	testcaseEnvInst.Log.Info("Scaling up Indexer Cluster", "Current Replicas", setup.indexerReplicas, "New Replicas", scaledIndexerReplicas)
	Expect(testcaseEnvInst.ScaleIndexerCluster(ctx, deployment, scaledIndexerReplicas)).To(Succeed(), "Unable to scale Indexer Cluster")

	// Get revision number of the resource
	resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, setup.mc)

	// Deploy Standalone with MC reference
	_, err := testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, deployment.GetName(), setup.mcName)
	Expect(err).To(Succeed(), "Unable to deploy Standalone with MC reference")

	// Ensure Indexer Cluster goes to Ready phase
	Expect(testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)).To(Succeed(), "Indexers not ready after scale up")

	// Ensure Search Head Cluster goes to Ready Phase
	// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed(), "Search Head Cluster not ready after scale up")

	// Wait for custom resource resource version to change and verify MC is ready
	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, setup.mc, resourceVersion)).To(Succeed(), "MC version not changed or not ready after scale up")

	// Verify Standalone configured on Monitoring Console
	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
	Expect(testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, deployment.GetName(), setup.mcName, true)).To(Succeed(), "Standalone not configured in MC")

	// Verify MC configuration after scale up
	testcaseEnvInst.Log.Info("Verify MC configuration after Scale Up")
	shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), scaledSHReplicas, false, 0)
	indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas, false, 0)
	Expect(testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, setup.mcName, shPods, indexerPods)).To(Succeed(), "MC config verification failed after scale up")

	Expect(testcaseEnvInst.VerifyC3ConditionsReady(ctx, deployment)).To(Succeed(), "C3 Ready conditions not met")
}

// RunS1StandaloneAddDeleteMCTest deploys two standalone instances with a Monitoring Console,
// verifies both are registered, then deletes the second standalone and verifies the MC
// config map and peer list are updated correctly.
func RunS1StandaloneAddDeleteMCTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, standaloneOneName, standaloneTwoName string) {
	mcName := deployment.GetName()

	// Deploy Standalone one with MCRef
	_, err := testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, standaloneOneName, mcName)
	Expect(err).To(Succeed(), "Unable to deploy Standalone with MC reference")

	// Deploy MC and wait for MC to be READY
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Check Standalone is configured in MC Config Map
	standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
	Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)).To(Succeed(), "Standalone pods not found in MC config")

	// Get revision number of the resource
	resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Add another standalone instance in namespace
	testcaseEnvInst.Log.Info("Adding second standalone deployment to namespace")
	standaloneTwoSpec := testenv.NewStandaloneSpecWithMCRefAndResources(
		testcaseEnvInst.GetSplunkImage(), mcName,
		corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				"cpu":    resource.MustParse("2"),
				"memory": resource.MustParse("4Gi"),
			},
			Requests: corev1.ResourceList{
				"cpu":    resource.MustParse("0.2"),
				"memory": resource.MustParse("256Mi"),
			},
		},
	)
	standaloneTwo, err := deployment.DeployStandaloneWithGivenSpec(ctx, standaloneTwoName, standaloneTwoSpec)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	// Wait for standalone two to be in READY status
	Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, standaloneTwoName, standaloneTwo)).To(Succeed(), "Standalone Two not ready")

	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed(), "MC version not changed or not ready")

	// Check both standalones are configured in MC Config Map
	standalonePods = append(standalonePods, fmt.Sprintf(testenv.StandalonePod, standaloneTwoName, 0))

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map after adding new standalone")
	Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)).To(Succeed(), "Standalone pods not found in MC config after adding second standalone")

	// get revision number of the resource
	resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Delete standalone two and ensure MC is updated
	testcaseEnvInst.Log.Info("Deleting second standalone deployment from namespace", "Standalone Name", standaloneTwoName)
	Expect(deployment.GetInstance(ctx, standaloneTwoName, standaloneTwo)).To(Succeed(), "Unable to get standalone instance")
	err = deployment.DeleteCR(ctx, standaloneTwo)
	Expect(err).To(Succeed(), "Unable to delete standalone instance", "Standalone Name", standaloneTwo)

	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed(), "MC version not changed or not ready after standalone deletion")

	// Check standalone one is still configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone One Pod in MC Config Map after deleting second standalone")
	Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)).To(Succeed(), "Standalone One not found in MC config after deleting second standalone")

	// Check Standalone Two NOT configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneTwoName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Two Pod NOT in MC Config Map after deleting second standalone")
	Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, false)).To(Succeed(), "Standalone Two still found in MC config after deletion")

	standaloneCR := &enterpriseApi.Standalone{}
	Expect(deployment.GetInstance(ctx, standaloneOneName, standaloneCR)).To(Succeed(), "Failed to get Standalone instance")
	Expect(testcaseEnvInst.VerifyStandaloneConditionReady(ctx, deployment, standaloneCR)).To(Succeed(), "Standalone Ready condition not met")
}
