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
package secret

import (
	"context"
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Secret Test for SVA C3", func() {

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

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("managersecret, smoke, c3: secret update on indexers and search head cluster", func() {

			// Test Scenario
			// 1. Update Secrets Data
			// 2. Verify New versioned secret are created with correct value.
			// 3. Verify new secrets are mounted on pods.
			// 4. Verify New Secrets are present in server.conf (Pass4SymmKey)
			// 5. Verify New Secrets via api access (password)

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

			mcRef := deployment.GetName()
			err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Verify RF SF is met
			testcaseEnvInst.Log.Info("Checkin RF SF before secret change")
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Current Secrets Struct
			namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
			secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			// Update Secret Value on Secret Object
			testcaseEnvInst.Log.Info("Data in secret object", "data", secretStruct.Data)
			modifiedHecToken := testenv.GetRandomeHECToken()
			modifedValue := testenv.RandomDNSName(10)
			updatedSecretData := testenv.GetSecretDataMap(modifiedHecToken, modifedValue, modifedValue, modifedValue, modifedValue)

			err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, updatedSecretData)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// Ensure that Cluster Manager goes to update phase
			testenv.VerifyClusterManagerPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseUpdating)

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Verify RF SF is met
			testcaseEnvInst.Log.Info("Checkin RF SF after secret change")
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testenv.VerifySecretsOnSecretObjects(ctx, deployment, testcaseEnvInst, secretObjectNames, updatedSecretData, true)

			// Once Pods are READY check each pod for updated secret keys
			verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

			// Verify secrets on pods
			testenv.VerifySecretsOnPods(ctx, deployment, testcaseEnvInst, verificationPods, updatedSecretData, true)

			// Verify Pass4SymmKey Secrets on ServerConf on MC, LM Pods
			testenv.VerifySplunkServerConfSecrets(ctx, deployment, testcaseEnvInst, verificationPods, updatedSecretData, true)

			// Verify Hec token on InputConf on Pod
			testenv.VerifySplunkInputConfSecrets(deployment, testcaseEnvInst, verificationPods, updatedSecretData, true)

			// Verify Secrets via api access on Pod
			testenv.VerifySplunkSecretViaAPI(ctx, deployment, testcaseEnvInst, verificationPods, updatedSecretData, true)
		})
	})
})
