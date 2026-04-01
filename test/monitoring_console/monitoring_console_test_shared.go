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
	cfg.VerifyCMReady(ctx, testcaseEnvInst, deployment)
	testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)

	// Deploy and verify Monitoring Console
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

	// Generate pod name slices for verification
	shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
	indexerPods := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), defaultIndexerReplicas, true, siteCount)

	// Verify MC configuration for M4 cluster
	testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, mcName, shPods, indexerPods)

	// ############ CLUSTER MANAGER MC RECONFIG #################################
	mcTwoName := deployment.GetName() + "-two"
	cm := cfg.NewCMObject()
	testenv.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, testcaseEnvInst, cm, deployment.GetName(), mcTwoName)

	cfg.VerifyCMReady(ctx, testcaseEnvInst, deployment)

	// Deploy and verify Monitoring Console Two
	testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")

	testenv.VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, shPods, indexerPods, false)
	testenv.VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcName, mc, shPods, true)
}

// RunC3MCReconfigTest deploys a C3 single-site cluster with a Monitoring Console,
// verifies the MC configuration, then reconfigures the Cluster Manager and SHC
// to point to a second MC and verifies both MCs are updated correctly.
func RunC3MCReconfigTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, cfg testenv.MCVersionConfig) {
	defaultSHReplicas := 3
	defaultIndexerReplicas := 3
	mcName := deployment.GetName()

	// Deploy Monitoring Console Pod
	mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), "")

	err := cfg.DeployC3WithMC(ctx, deployment, deployment.GetName(), defaultIndexerReplicas, true, mcName)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure C3 cluster is ready
	testcaseEnvInst.VerifyC3ClusterReady(ctx, deployment, func(ctx2 context.Context, d *testenv.Deployment) {
		cfg.VerifyCMReady(ctx2, testcaseEnvInst, d)
	})

	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	// Generate pod name slices for verification
	shPods := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), defaultSHReplicas, false, 0)
	indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), defaultIndexerReplicas, false, 0)

	// Verify MC configuration for C3 cluster
	testenv.VerifyMCConfigForCluster(ctx, deployment, testcaseEnvInst, cfg, mcName, shPods, indexerPods)

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// #################  Update Monitoring Console In Cluster Manager CR ##################################

	mcTwoName := deployment.GetName() + "-two"
	cm := cfg.NewCMObject()
	testenv.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, testcaseEnvInst, cm, deployment.GetName(), mcTwoName)

	cfg.VerifyCMReady(ctx, testcaseEnvInst, deployment)
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

	// Deploy and verify Monitoring Console Two
	mcTwo := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")

	// ###########   VERIFY MONITORING CONSOLE TWO AFTER CLUSTER MANAGER RECONFIG  ###################################
	testenv.VerifyMCTwoAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, shPods, indexerPods, true)

	// ##############  VERIFY MONITORING CONSOLE ONE AFTER CLUSTER MANAGER RECONFIG #######################
	testenv.VerifyMCOneAfterCMReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcName, mc, shPods, false)

	// #################  Update Monitoring Console In SHC CR ##################################

	shc := &enterpriseApi.SearchHeadCluster{}
	shcName := deployment.GetName() + "-shc"
	testenv.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, testcaseEnvInst, shc, shcName, mcTwoName)

	// Ensure Search Head Cluster goes to Ready Phase
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	if cfg.VerifyMCTwoReadyAfterSHC {
		testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcTwoName, mcTwo)
	}

	// ############################  VERIFICATION FOR MONITORING CONSOLE TWO POST SHC RECONFIG ###############################
	testenv.VerifyMCTwoAfterSHCReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcTwoName, shPods, indexerPods, cfg.SHCReconfigTimeout)

	// ############################  VERIFICATION FOR MONITORING CONSOLE ONE POST SHC RECONFIG ###############################
	testenv.VerifyMCOneAfterSHCReconfig(ctx, deployment, testcaseEnvInst, cfg.MCReconfigParams, mcName, mc, shPods, cfg.SHCReconfigTimeout)
}

// RunS1StandaloneAddDeleteMCTest deploys two standalone instances with a Monitoring Console,
// verifies both are registered, then deletes the second standalone and verifies the MC
// config map and peer list are updated correctly.
func RunS1StandaloneAddDeleteMCTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, standaloneOneName, standaloneTwoName string) {
	mcName := deployment.GetName()

	// Deploy Standalone one with MCRef
	testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, standaloneOneName, mcName)

	// Deploy MC and wait for MC to be READY
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

	// Check Standalone is configured in MC Config Map
	standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
	testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

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
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, standaloneTwoName, standaloneTwo)

	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	// Check both standalones are configured in MC Config Map
	standalonePods = append(standalonePods, fmt.Sprintf(testenv.StandalonePod, standaloneTwoName, 0))

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map after adding new standalone")
	testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

	// get revision number of the resource
	resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Delete standalone two and ensure MC is updated
	testcaseEnvInst.Log.Info("Deleting second standalone deployment from namespace", "Standalone Name", standaloneTwoName)
	deployment.GetInstance(ctx, standaloneTwoName, standaloneTwo)
	err = deployment.DeleteCR(ctx, standaloneTwo)
	Expect(err).To(Succeed(), "Unable to delete standalone instance", "Standalone Name", standaloneTwo)

	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	// Check standalone one is still configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone One Pod in MC Config Map after deleting second standalone")
	testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

	// Check Standalone Two NOT configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneTwoName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Two Pod NOT in MC Config Map after deleting second standalone")
	testenv.VerifyStandalonePodsInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, false)
}
