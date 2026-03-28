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

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// RunS1StandaloneAddDeleteMCTest deploys two standalone instances with a Monitoring Console,
// verifies both are registered, then deletes the second standalone and verifies the MC
// config map and peer list are updated correctly.
func RunS1StandaloneAddDeleteMCTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, standaloneOneName, standaloneTwoName string) {
	mcName := deployment.GetName()

	// Deploy standalone one with MCRef
	spec := testenv.NewStandaloneSpecWithMCRef(testcaseEnvInst.GetSplunkImage(), mcName)
	standaloneOne, err := deployment.DeployStandaloneWithGivenSpec(ctx, standaloneOneName, spec)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	// Wait for standalone to be in READY Status
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, standaloneOneName, standaloneOne)

	// Deploy MC and wait for MC to be READY
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

	// Check Standalone is configured in MC Config Map
	standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
	verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

	// get revision number of the resource
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
	verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

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
	verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, true)

	// Check Standalone Two NOT configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneTwoName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Two Pod NOT in MC Config Map after deleting second standalone")
	verifyStandaloneInMC(ctx, deployment, testcaseEnvInst, standalonePods, mcName, false)
}

func verifyStandaloneInMC(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, pods []string, mcName string, shouldExist bool) {
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, pods, "SPLUNK_STANDALONE_URL", mcName, shouldExist)
	testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, pods, mcName, shouldExist, false)
}

// MCReconfigParams holds the service name and URL parameters that differ between
// V3 (master) and V4 (manager) monitoring console tests.
type MCReconfigParams struct {
	CMServiceNameFmt string // format string for CM service name (e.g., testenv.ClusterMasterServiceName)
	CMURLKey         string // config map URL key (e.g., "SPLUNK_CLUSTER_MASTER_URL" or splcommon.ClusterManagerURL)
}

// VerifyMCTwoAfterCMReconfig verifies that MC Two is correctly configured after the Cluster Manager
// has been reconfigured to point to it: CM and indexers should be present, SH should be absent.
// If checkDeployerAbsent is true, also verifies deployer is absent on MC Two (used in C3 tests).
func VerifyMCTwoAfterCMReconfig(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv,
	params MCReconfigParams, mcTwoName string, shPods, indexerPods []string, checkDeployerAbsent bool) {

	testcaseEnvInst.Log.Info("Verify CM in MC Two Config Map after CM Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}, params.CMURLKey, mcTwoName, true)

	testcaseEnvInst.Log.Info("Verify Indexers in MC Two Config String after CM Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcTwoName, true, true)

	if checkDeployerAbsent {
		testcaseEnvInst.Log.Info("Verify Deployer NOT in MC Two Config Map after CM Reconfig")
		testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
			[]string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, false)
	}

	testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC Two Config Map after CM Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, false)

	testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC Two Config String after CM Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcTwoName, false, false)
}

// VerifyMCOneAfterCMReconfig verifies that MC One is correctly configured after the Cluster Manager
// has been reconfigured away from it: CM should be absent, SH should still be present.
// If checkDeployerPresent is true, also verifies deployer is still present on MC One (used in M4 tests).
func VerifyMCOneAfterCMReconfig(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv,
	params MCReconfigParams, mcName string, mc *enterpriseApi.MonitoringConsole, shPods []string, checkDeployerPresent bool) {

	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

	testcaseEnvInst.Log.Info("Verify CM NOT in MC One Config Map after CM Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}, params.CMURLKey, mcName, false)

	// CSPL-619: Indexer verification on MC One is commented out in all test variants

	if checkDeployerPresent {
		testcaseEnvInst.Log.Info("Verify Deployer still in MC One Config Map after CM Reconfig")
		testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
			[]string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true)
	}

	testcaseEnvInst.Log.Info("Verify SH Pods still in MC One Config Map after CM Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true)

	testcaseEnvInst.Log.Info("Verify SH Pods still in MC One Config String after CM Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, true, false)
}

// VerifyMCTwoAfterSHCReconfig verifies that MC Two has all components (CM, deployer, SH, indexers)
// after the SHC has been reconfigured to point to it.
// If timeout > 0, uses WaitForPodsInMCConfigString; otherwise uses direct VerifyPodsInMCConfigString.
func VerifyMCTwoAfterSHCReconfig(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv,
	params MCReconfigParams, mcTwoName string, shPods, indexerPods []string, timeout time.Duration) {

	testcaseEnvInst.Log.Info("Verify CM in MC Two Config Map after SHC Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}, params.CMURLKey, mcTwoName, true)

	testcaseEnvInst.Log.Info("Verify Deployer in MC Two Config Map after SHC Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(testenv.DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, true)

	testcaseEnvInst.Log.Info("Verify SH Pods in MC Two Config Map after SHC Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, true)

	if timeout > 0 {
		testcaseEnvInst.Log.Info("Verify SH Pods in MC Two Config String after SHC Reconfig (with wait)")
		err := testcaseEnvInst.WaitForPodsInMCConfigString(ctx, deployment, shPods, mcTwoName, true, false, timeout)
		Expect(err).To(Succeed(), "Timed out waiting for search heads in MC two config after SHC reconfig")

		testcaseEnvInst.Log.Info("Verify Indexers in MC Two Config String after SHC Reconfig (with wait)")
		err = testcaseEnvInst.WaitForPodsInMCConfigString(ctx, deployment, indexerPods, mcTwoName, true, true, timeout)
		Expect(err).To(Succeed(), "Timed out waiting for indexers in MC two config after SHC reconfig")
	} else {
		testcaseEnvInst.Log.Info("Verify SH Pods in MC Two Config String after SHC Reconfig")
		testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcTwoName, true, false)

		testcaseEnvInst.Log.Info("Verify Indexers in MC Two Config String after SHC Reconfig")
		testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcTwoName, true, true)
	}
}

// VerifyMCOneAfterSHCReconfig verifies that MC One has lost all components (CM, deployer, SH)
// after the SHC has been reconfigured away from it.
// If timeout > 0, uses WaitForPodsInMCConfigString; otherwise uses direct VerifyPodsInMCConfigString.
func VerifyMCOneAfterSHCReconfig(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv,
	params MCReconfigParams, mcName string, mc *enterpriseApi.MonitoringConsole, shPods []string, timeout time.Duration) {

	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

	testcaseEnvInst.Log.Info("Verify CM NOT in MC One Config Map after SHC Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}, params.CMURLKey, mcName, false)

	testcaseEnvInst.Log.Info("Verify Deployer NOT in MC One Config Map after SHC Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, false)

	testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC One Config Map after SHC Reconfig")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, false)

	if timeout > 0 {
		testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC One Config String after SHC Reconfig (with wait)")
		err := testcaseEnvInst.WaitForPodsInMCConfigString(ctx, deployment, shPods, mcName, false, false, timeout)
		Expect(err).To(Succeed(), "Timed out waiting for search heads to be removed from MC one config after SHC reconfig")
	} else {
		testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC One Config String after SHC Reconfig")
		testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, false, false)
	}

	// CSPL-619: Indexer verification on MC One is commented out in all test variants
}
