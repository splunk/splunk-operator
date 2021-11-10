// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
package secret

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Secret Test for M4 SVA", func() {

	var deployment *testenv.Deployment

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testenvInstance.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
	})

	Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster)", func() {
		It("secret, integration, m4: secret update on multisite indexers and search head cluster", func() {

			/* Test Scenario
			1. Update Secrets Data
			2. Verify New versioned secret are created with correct value.
			3. Verify new secrets are mounted on pods.
			4. Verify New Secrets are present in server.conf (Pass4SymmKey)
			5. Verify New Secrets via api access (password)*/

			// Download License File
			licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			siteCount := 3
			mcName := deployment.GetName()
			err = deployment.DeployMultisiteClusterWithSearchHead(deployment.GetName(), 1, siteCount, mcName)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(deployment, testenvInstance)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(deployment.GetName(), deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenvInstance.Log.Info("Checkin RF SF before secret change")
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Get Current Secrets Struct
			namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testenvInstance.GetName())
			secretStruct, err := testenv.GetSecretStruct(deployment, testenvInstance.GetName(), namespaceScopedSecretName)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			// Test 1
			// Update Secrets Data and
			// Verify New versioned secret are created with correct value.
			// Verify new secrets are mounted on pods.
			// Verify New Secrets are present in server.conf (Pass4SymmKey)
			// Verify New Secrets via api access (password)*/

			// Update Secret Value on Secret Object
			testenvInstance.Log.Info("Data in secret object", "data", secretStruct.Data)
			modifiedHecToken := testenv.GetRandomeHECToken()
			modifedValue := testenv.RandomDNSName(10)
			updatedSecretData := testenv.GetSecretDataMap(modifiedHecToken, modifedValue, modifedValue, modifedValue, modifedValue)

			err = testenv.ModifySecretObject(deployment, testenvInstance.GetName(), namespaceScopedSecretName, updatedSecretData)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// Ensure that Cluster Manager goes to update phase
			testenv.VerifyClusterManagerPhase(deployment, testenvInstance, splcommon.PhaseUpdating)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenvInstance.Log.Info("Checkin RF SF after secret change")
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testenvInstance.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testenv.VerifySecretsOnSecretObjects(deployment, testenvInstance, secretObjectNames, updatedSecretData, true)

			// Once Pods are READY check each versioned secret for updated secret keys
			verificationPods := testenv.DumpGetPods(testenvInstance.GetName())

			// Verify secrets on pods
			testenv.VerifySecretsOnPods(deployment, testenvInstance, verificationPods, updatedSecretData, true)

			// Verify Pass4SymmKey Secrets on ServerConf on MC, LM Pods
			testenv.VerifySplunkServerConfSecrets(deployment, testenvInstance, verificationPods, updatedSecretData, true)

			// Verify Hec token on InputConf on Pod
			testenv.VerifySplunkInputConfSecrets(deployment, testenvInstance, verificationPods, updatedSecretData, true)

			// Verify Secrets via api access on Pod
			testenv.VerifySplunkSecretViaAPI(deployment, testenvInstance, verificationPods, updatedSecretData, true)
		})
	})
})
