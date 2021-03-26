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

	Context("Multisite cluster deployment (M13 - Multisite indexer cluster, Search head cluster)", func() {
		It("secret: secret update on multisite indexers and search head cluster", func() {

			/* Test Scenario
			1. Update Secrets Data
			2. Verify New versioned secret are created with correct value.
			3. Verify new secrets are mounted on pods.
			4. Verify New Secrets are present in server.conf (Pass4SymmKey) */

			// Download License File
			licenseFilePath, err := testenv.DownloadFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			siteCount := 3
			err = deployment.DeployMultisiteClusterWithSearchHead(deployment.GetName(), 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Get Current Secrets Struct
			namespaceScopedSecretName := splcommon.GetNamespaceScopedSecretName(testenvInstance.GetName())
			secretStruct, err := testenv.GetSecretStruct(deployment, testenvInstance.GetName(), namespaceScopedSecretName)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			// Test 1
			// Update Secrets Data and
			// Verify New versioned secret are created with correct value.
			// Verify new secrets are mounted on pods.
			// Verify New Secrets are present in server.conf (Pass4SymmKey)

			// Update Secret Value on Secret Object
			testenvInstance.Log.Info("Data in secret object", "data", secretStruct.Data)
			modifiedHecToken := testenv.GetRandomeHECToken()
			modifedValue := testenv.RandomDNSName(10)
			updatedSecretData := map[string][]byte{
				"hec_token":    []byte(modifiedHecToken),
				"password":     []byte(modifedValue),
				"pass4SymmKey": []byte(modifedValue),
				"idxc_secret":  []byte(modifedValue),
				"shc_secret":   []byte(modifedValue),
			}

			err = testenv.ModifySecretObject(deployment, testenvInstance.GetName(), namespaceScopedSecretName, updatedSecretData)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// Ensure that Cluster Master goes to update phase
			testenv.VerifyClusterMasterPhase(deployment, testenvInstance, splcommon.PhaseUpdating)

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

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
		})
	})
})
