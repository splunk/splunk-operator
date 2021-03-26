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

var _ = Describe("Secret Test for SVA S1", func() {

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

	Context("Standalone deployment (S1) with LM", func() {
		It("secret: Secret update on a standalone instance", func() {

			/* Test Scenario
			1. Update Secrets Data
			2. Verify New versioned secret are created with correct value.
			3. Verify new secrets are mounted on pods.
			4.  Verify New Secrets are present in server.conf (Pass4SymmKey) */

			// Download License File
			licenseFilePath, err := testenv.DownloadFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			// Create standalone Deployment with License Master
			standalone, err := deployment.DeployStandaloneWithLM(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Get Current Secrets Struct
			namespaceScopedSecretName := splcommon.GetNamespaceScopedSecretName(testenvInstance.GetName())
			secretStruct, err := testenv.GetSecretStruct(deployment, testenvInstance.GetName(), namespaceScopedSecretName)
			Expect(err).To(Succeed(), "Unable to get secret struct")

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

			// Ensure standalone is updating
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testenvInstance.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testenv.VerifySecretsOnSecretObjects(deployment, testenvInstance, secretObjectNames, updatedSecretData, true)

			// Once Pods are READY check each pod for updated secret keys
			verificationPods := testenv.DumpGetPods(testenvInstance.GetName())

			// Verify secrets on pods
			testenv.VerifySecretsOnPods(deployment, testenvInstance, verificationPods, updatedSecretData, true)

			// Verify Secrets on ServerConf on Pod
			testenv.VerifySplunkServerConfSecrets(deployment, testenvInstance, verificationPods, updatedSecretData, true)

		})
	})

	Context("Standalone deployment (S1) with LM", func() {
		It("secret: Secret Object is recreated on delete and new secrets are applied to Splunk Pods", func() {

			/* Test Scenario
			1. Delete Secret Object
			2. Verify New versioned secret are created with new values.
			3. Verify New secrets are mounted on pods.
			4.  Verify New Secrets are present in server.conf (Pass4SymmKey) */

			// Download License File
			licenseFilePath, err := testenv.DownloadFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			// Create standalone Deployment with License Master
			standalone, err := deployment.DeployStandaloneWithLM(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Get Current Secrets Struct
			namespaceScopedSecretName := splcommon.GetNamespaceScopedSecretName(testenvInstance.GetName())
			secretStruct, err := testenv.GetSecretStruct(deployment, testenvInstance.GetName(), namespaceScopedSecretName)
			testenvInstance.Log.Info("Data in secret object", "data", secretStruct.Data)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			// Delete secret Object
			err = testenv.DeleteSecretObject(deployment, testenvInstance.GetName(), namespaceScopedSecretName)
			Expect(err).To(Succeed(), "Unable to delete secret Object")

			// Ensure standalone is updating
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testenvInstance.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testenv.VerifySecretsOnSecretObjects(deployment, testenvInstance, secretObjectNames, secretStruct.Data, false)

			// Once Pods are READY check each pod for updated secret keys
			verificationPods := testenv.DumpGetPods(testenvInstance.GetName())

			// Verify secrets on pods
			testenv.VerifySecretsOnPods(deployment, testenvInstance, verificationPods, secretStruct.Data, false)

			// Verify Secrets on ServerConf on Pod
			testenv.VerifySplunkServerConfSecrets(deployment, testenvInstance, verificationPods, secretStruct.Data, false)
		})
	})

	Context("Standalone deployment (S1)", func() {
		It("secret,smoke: Secret Object data is repopulated in secret object on passing empty Data map and new secrets are applied to Splunk Pods", func() {

			/* Test Scenario
			1. Delete Secret Passing Empty Data Map to secret Object
			2. Verify New versioned secret are created with new values.
			3. Verify New secrets are mounted on pods.
			4. Verify New Secrets are present in server.conf (Pass4SymmKey) */

			// Create standalone Deployment with License Master
			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Get Current Secrets Struct
			namespaceScopedSecretName := splcommon.GetNamespaceScopedSecretName(testenvInstance.GetName())
			secretStruct, err := testenv.GetSecretStruct(deployment, testenvInstance.GetName(), namespaceScopedSecretName)
			testenvInstance.Log.Info("Data in secret object", "data", secretStruct.Data)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			// Delete secret by passing empty Data Map
			err = testenv.ModifySecretObject(deployment, testenvInstance.GetName(), namespaceScopedSecretName, map[string][]byte{})
			Expect(err).To(Succeed(), "Unable to delete secret Object")

			// Ensure standalone is updating
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testenvInstance.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testenv.VerifySecretsOnSecretObjects(deployment, testenvInstance, secretObjectNames, secretStruct.Data, false)

			// Once Pods are READY check each pod for updated secret keys
			verificationPods := testenv.DumpGetPods(testenvInstance.GetName())

			// Verify secrets on pods
			testenv.VerifySecretsOnPods(deployment, testenvInstance, verificationPods, secretStruct.Data, false)

			// Verify Secrets on ServerConf on Pod
			testenv.VerifySplunkServerConfSecrets(deployment, testenvInstance, verificationPods, secretStruct.Data, false)

		})
	})
})
