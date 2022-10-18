// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
package licensemanager

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Licensemanager test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Standalone deployment (S1) with LM", func() {
		It("licensemanager, smoke, s1: Splunk Operator can configure License Manager with Standalone in S1 SVA", func() {

			// Download License File
			downloadDir := "licenseFolder"
			switch testenv.ClusterProvider {
			case "eks":
				licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
				Expect(err).To(Succeed(), "Unable to download license file from S3")
				// Create License Config Map
				testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath)
			case "azure":
				licenseFilePath, err := testenv.DownloadLicenseFromAzure(ctx, downloadDir)
				Expect(err).To(Succeed(), "Unable to download license file from Azure")
				// Create License Config Map
				testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath)
			default:
				fmt.Printf("Unable to download license file")
				testcaseEnvInst.Log.Info(fmt.Sprintf("Unable to download license file with Cluster Provider set as %v", testenv.ClusterProvider))
			}

			// Create standalone Deployment with License Manager
			mcRef := deployment.GetName()
			standalone, err := deployment.DeployStandaloneWithLM(ctx, deployment.GetName(), mcRef)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

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
