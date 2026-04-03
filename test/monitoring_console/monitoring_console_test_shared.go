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

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
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
	Expect(cfg.VerifyCMReady(ctx, deployment, testcaseEnvInst)).To(Succeed())
	Expect(testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)).To(Succeed())

	// Deploy and verify Monitoring Console
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Generate pod name slices for verification
	shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
	indexerPods := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), defaultIndexerReplicas, true, siteCount)

	// Verify MC configuration for M4 cluster
	Expect(testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, mcName, shPods, indexerPods)).To(Succeed())

	// ############ CLUSTER MANAGER MC RECONFIG #################################
	mcTwoName := deployment.GetName() + "-two"
	cm := cfg.NewCMObject()
	Expect(testenv.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, testcaseEnvInst, cm, deployment.GetName(), mcTwoName)).To(Succeed())

	Expect(cfg.VerifyCMReady(ctx, deployment, testcaseEnvInst)).To(Succeed())

	// Deploy and verify Monitoring Console Two
	testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")

	Expect(testenv.VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, shPods, indexerPods, false)).To(Succeed())
	Expect(testenv.VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcName, mc, shPods, true)).To(Succeed())
}

// RunC3MCReconfigTest deploys a C3 single-site cluster with a Monitoring Console,
// verifies the MC configuration, then reconfigures the Cluster Manager and SHC
// to point to a second MC and verifies both MCs are updated correctly.
func RunC3MCReconfigTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, cfg testenv.MCVersionConfig) {
	defaultSHReplicas := 3
	defaultIndexerReplicas := 3
	mcName := deployment.GetName()

	// Deploy Monitoring Console Pod
	mc, resourceVersion, err := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	err = cfg.DeployC3WithMC(ctx, deployment, deployment.GetName(), defaultIndexerReplicas, true, mcName)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure C3 cluster is ready
	Expect(testcaseEnvInst.VerifyC3ClusterReady(ctx, deployment, func(ctx2 context.Context, d *testenv.Deployment) error {
		return cfg.VerifyCMReady(ctx2, d, testcaseEnvInst)
	})).To(Succeed())

	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed())

	// Generate pod name slices for verification
	shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
	indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)

	// Verify MC configuration for C3 cluster
	Expect(testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, mcName, shPods, indexerPods)).To(Succeed())

	// Verify Monitoring Console is Ready and stays in ready state
	Expect(testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)).To(Succeed())

	// #################  Update Monitoring Console In Cluster Manager CR ##################################

	mcTwoName := deployment.GetName() + "-two"
	cm := cfg.NewCMObject()
	Expect(testenv.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, testcaseEnvInst, cm, deployment.GetName(), mcTwoName)).To(Succeed())

	Expect(cfg.VerifyCMReady(ctx, deployment, testcaseEnvInst)).To(Succeed())
	Expect(testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)).To(Succeed())

	// Deploy and verify Monitoring Console Two
	mcTwo, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console Two")

	// ###########   VERIFY MONITORING CONSOLE TWO AFTER CLUSTER MANAGER RECONFIG  ###################################
	Expect(testenv.VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, shPods, indexerPods, true)).To(Succeed())

	// ##############  VERIFY MONITORING CONSOLE ONE AFTER CLUSTER MANAGER RECONFIG #######################
	Expect(testenv.VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcName, mc, shPods, false)).To(Succeed())

	// #################  Update Monitoring Console In SHC CR ##################################

	shc := &enterpriseApi.SearchHeadCluster{}
	shcName := deployment.GetName() + "-shc"
	Expect(testenv.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, testcaseEnvInst, shc, shcName, mcTwoName)).To(Succeed())

	// Ensure Search Head Cluster goes to Ready Phase
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed())

	if cfg.VerifyMCTwoReadyAfterSHC {
		Expect(testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo)).To(Succeed())
	}

	// ############################  VERIFICATION FOR MONITORING CONSOLE TWO POST SHC RECONFIG ###############################
	Expect(testenv.VerifyMCTwoAfterSHCReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, shPods, indexerPods, cfg.SHCReconfigTimeout)).To(Succeed())

	// ############################  VERIFICATION FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################
	Expect(testenv.VerifyMCOneAfterSHCReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcName, mc, shPods, cfg.SHCReconfigTimeout)).To(Succeed())
}

// RunC3MCScaleUpTest deploys a C3 cluster with a Monitoring Console, verifies MC
// configuration, scales SHC and indexers, adds a standalone, and verifies the MC
// is updated correctly after scale up. Works for both V3 (master) and V4 (manager).
func RunC3MCScaleUpTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, cfg testenv.MCVersionConfig) {
	defaultSHReplicas := 3
	defaultIndexerReplicas := 3
	mcName := deployment.GetName()

	// Deploy and verify Monitoring Console
	mc, resourceVersion, err := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Deploy C3 cluster with MC
	err = cfg.DeployC3WithMC(ctx, deployment, deployment.GetName(), defaultIndexerReplicas, true, mcName)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure C3 cluster is ready
	Expect(testcaseEnvInst.VerifyC3ClusterReady(ctx, deployment, func(ctx2 context.Context, d *testenv.Deployment) error {
		return cfg.VerifyCMReady(ctx2, d, testcaseEnvInst)
	})).To(Succeed())

	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed())

	// Verify MC configuration for C3 cluster
	shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
	indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)
	Expect(testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, mcName, shPods, indexerPods)).To(Succeed())

	// Scale Search Head Cluster
	scaledSHReplicas := defaultSHReplicas + 1
	testcaseEnvInst.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)
	testcaseEnvInst.ScaleSearchHeadCluster(ctx, deployment, deployment.GetName(), scaledSHReplicas)

	// Scale indexers
	scaledIndexerReplicas := defaultIndexerReplicas + 1
	testcaseEnvInst.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)
	testcaseEnvInst.ScaleIndexerCluster(ctx, deployment, deployment.GetName(), scaledIndexerReplicas)

	// Get revision number of the resource
	resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Deploy Standalone with MC reference
	testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, deployment.GetName(), mcName)

	// Ensure Indexer Cluster goes to Ready phase
	Expect(testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)).To(Succeed())

	// Ensure Search Head Cluster goes to Ready Phase
	// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed())

	// Wait for custom resource resource version to change and verify MC is ready
	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed())

	// Verify Standalone configured on Monitoring Console
	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC")
	Expect(testcaseEnvInst.VerifyStandaloneInMC(ctx, deployment, deployment.GetName(), mcName, true)).To(Succeed())

	// Verify MC configuration after scale up
	testcaseEnvInst.Log.Info("Verify MC configuration after Scale Up")
	shPods = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), scaledSHReplicas, false, 0)
	indexerPods = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas, false, 0)
	Expect(testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, mcName, shPods, indexerPods)).To(Succeed())
}

// RunS1StandaloneAddDeleteMCTest deploys two standalone instances with a Monitoring Console,
// verifies both are registered, then deletes the second standalone and verifies the MC
// config map and peer list are updated correctly.
func RunS1StandaloneAddDeleteMCTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, standaloneOneName, standaloneTwoName string) {
	mcName := deployment.GetName()

	// Deploy Standalone one with MCRef
	testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, standaloneOneName, mcName)

	// Deploy MC and wait for MC to be READY
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Check Standalone is configured in MC Config Map
	standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
	Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)).To(Succeed())

	// Get revision number of the resource
	resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Add another standalone instance in namespace
	testcaseEnvInst.Log.Info("Adding second standalone deployment to namespace")
	standaloneTwoSpec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				ImagePullPolicy: "IfNotPresent",
				Image:           testcaseEnvInst.GetSplunkImage(),
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
	standaloneTwo, err := deployment.DeployStandaloneWithGivenSpec(ctx, standaloneTwoName, standaloneTwoSpec)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	// Wait for standalone two to be in READY status
	Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, standaloneTwoName, standaloneTwo)).To(Succeed())

	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed())

	// Check both standalones are configured in MC Config Map
	standalonePods = append(standalonePods, fmt.Sprintf(testenv.StandalonePod, standaloneTwoName, 0))

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map after adding new standalone")
	Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)).To(Succeed())

	// get revision number of the resource
	resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Delete standalone two and ensure MC is updated
	testcaseEnvInst.Log.Info("Deleting second standalone deployment from namespace", "Standalone Name", standaloneTwoName)
	deployment.GetInstance(ctx, standaloneTwoName, standaloneTwo)
	err = deployment.DeleteCR(ctx, standaloneTwo)
	Expect(err).To(Succeed(), "Unable to delete standalone instance", "Standalone Name", standaloneTwo)

	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed())

	// Check standalone one is still configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone One Pod in MC Config Map after deleting second standalone")
	Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)).To(Succeed())

	// Check Standalone Two NOT configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneTwoName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Two Pod NOT in MC Config Map after deleting second standalone")
	Expect(testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, false)).To(Succeed())
}
