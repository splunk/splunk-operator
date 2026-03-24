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

// RunS1StandaloneAddDeleteMCTest deploys two standalone instances with a Monitoring Console,
// verifies both are registered, then deletes the second standalone and verifies the MC
// config map and peer list are updated correctly.
func RunS1StandaloneAddDeleteMCTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, standaloneOneName, standaloneTwoName string) {
	mcName := deployment.GetName()

	// Deploy standalone one with MCRef
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
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, standaloneOneName, standaloneOne)

	// Deploy MC and wait for MC to be READY
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

	// Check Standalone is configured in MC Config Map
	standalonePods := testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

	// Check Standalone Pod in MC Peer List
	testcaseEnvInst.Log.Info("Check standalone instance in MC Peer list")
	testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcName, true, false)

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
	Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

	// Wait for standalone two to be in READY status
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, standaloneTwoName, standaloneTwo)

	// wait for custom resource resource version to change
	testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

	// Verify MC is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Check both standalones are configured in MC Config Map
	standalonePods = append(standalonePods, fmt.Sprintf(testenv.StandalonePod, standaloneTwoName, 0))

	testcaseEnvInst.Log.Info("Checking for Standalone Pod on MC Config Map after adding new standalone")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

	// Check Standalone Pod in MC Peer List
	testcaseEnvInst.Log.Info("Check standalone instance in MC Peer list after adding new standalone")
	testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcName, true, false)

	// get revision number of the resource
	resourceVersion = testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Delete standalone two and ensure MC is updated
	testcaseEnvInst.Log.Info("Deleting second standalone deployment to namespace", "Standalone Name", standaloneTwoName)
	deployment.GetInstance(ctx, standaloneTwoName, standaloneTwo)
	err = deployment.DeleteCR(ctx, standaloneTwo)
	Expect(err).To(Succeed(), "Unable to delete standalone instance", "Standalone Name", standaloneTwo)

	// wait for custom resource resource version to change
	testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

	// Verify MC is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Check standalone one is still configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneOneName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone One Pod in MC Config Map after deleting second standalone")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcName, true)

	// Check Standalone Pod in MC Peer List
	testcaseEnvInst.Log.Info("Check Standalone One Pod in MC Peer list after deleting second standalone")
	testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcName, true, false)

	// Check Standalone Two NOT configured in MC Config Map
	standalonePods = testenv.GeneratePodNameSlice(testenv.StandalonePod, standaloneTwoName, 1, false, 0)

	testcaseEnvInst.Log.Info("Checking for Standalone Two Pod NOT in MC Config Map after deleting second standalone")
	testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, standalonePods, "SPLUNK_STANDALONE_URL", mcName, false)

	// Check Standalone Pod TWO NOT configured in MC Peer List
	testcaseEnvInst.Log.Info("Check Standalone Two Pod NOT in MC Peer list after deleting second standalone")
	testcaseEnvInst.VerifyPodsInMCConfigString(ctx, deployment, standalonePods, mcName, false, false)
}
