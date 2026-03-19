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
package licensemaster

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Licensemanager test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Standalone deployment (S1) with LM", func() {
		It("licensemaster, smoke, s1: Splunk Operator can configure License Manager with Standalone in S1 SVA", func() {

			// Download License File and create config map
			testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

			// Create standalone Deployment with License Master
			mcRef := deployment.GetName()
			standalone, err := deployment.DeployStandaloneWithLMaster(ctx, deployment.GetName(), mcRef)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(ctx, deployment, testcaseEnvInst)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Deploy Monitoring Console
			mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// ############ Verify livenessProbe and readinessProbe config object and scripts############
			testcaseEnvInst.Log.Info("Get config map for livenessProbe and readinessProbe")
			ConfigMapName := enterprise.GetProbeConfigMapName(testcaseEnvInst.GetName())
			_, err = testenv.GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
			Expect(err).To(Succeed(), "Unable to get config map for livenessProbe and readinessProbe", "ConfigMap name", ConfigMapName)
			scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName()}
			allPods := testenv.DumpGetPods(testcaseEnvInst.GetName())
			testenv.VerifyFilesInDirectoryOnPod(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)

			// Verify LM is configured on standalone instance
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, standalonePodName)

			// Verify LM Configured on Monitoring Console
			monitoringConsolePodName := fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, monitoringConsolePodName)

		})
	})
})
