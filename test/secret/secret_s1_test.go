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
package secret

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Secret Test for SVA S1", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	ctx := context.TODO()
	var deployment *testenv.Deployment

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
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

	Context("Standalone deployment (S1) with LM and MC", func() {
		It("secret, integration, s1: Secret update on a standalone instance with LM and MC", func() {

			//  Test Scenario
			// 1. Update Secrets Data
			// 2. Verify New versioned secret are created with correct value.
			// 3. Verify new secrets are mounted on pods.
			// 4. Verify New Secrets are present in server.conf (Pass4SymmKey)
			// 5. Verify New Secrets via api access (password)

			// Download License File
			licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath)

			// Create standalone Deployment with License Manager
			mcName := deployment.GetName()
			standalone, err := deployment.DeployStandaloneWithLM(ctx, deployment.GetName(), mcName)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

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

			// Ensure standalone is updating
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), splcommon.PhaseUpdating)

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testenv.VerifySecretsOnSecretObjects(ctx, deployment, testcaseEnvInst, secretObjectNames, updatedSecretData, true)

			// Once Pods are READY check each pod for updated secret keys
			verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

			// Verify secrets on pods
			testenv.VerifySecretsOnPods(ctx, deployment, testcaseEnvInst, verificationPods, updatedSecretData, true)

			// Verify Secrets on ServerConf on Pod
			testenv.VerifySplunkServerConfSecrets(ctx, deployment, testcaseEnvInst, verificationPods, updatedSecretData, true)

			// Verify Hec token on InputConf on Pod
			testenv.VerifySplunkInputConfSecrets(deployment, testcaseEnvInst, verificationPods, updatedSecretData, true)

			// Verify Secrets via api access on Pod
			testenv.VerifySplunkSecretViaAPI(ctx, deployment, testcaseEnvInst, verificationPods, updatedSecretData, true)

		})
	})

	Context("Standalone deployment (S1) with LM amd MC", func() {
		It("secret, integration, s1: Secret Object is recreated on delete and new secrets are applied to Splunk Pods", func() {

			// Test Scenario
			//1. Delete Secret Object
			//2. Verify New versioned secret are created with new values.
			//3. Verify New secrets are mounted on pods.
			//4. Verify New Secrets are present in server.conf (Pass4SymmKey)
			//5. Verify New Secrets via api access (password)

			// Download License File
			licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath)

			// Create standalone Deployment with License Manager
			mcName := deployment.GetName()
			standalone, err := deployment.DeployStandaloneWithLM(ctx, deployment.GetName(), mcName)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Get Current Secrets Struct
			namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
			secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
			testcaseEnvInst.Log.Info("Data in secret object", "data", secretStruct.Data)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			// Delete secret Object
			err = testenv.DeleteSecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
			Expect(err).To(Succeed(), "Unable to delete secret Object")

			// Ensure standalone is updating
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), splcommon.PhaseUpdating)

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testenv.VerifySecretsOnSecretObjects(ctx, deployment, testcaseEnvInst, secretObjectNames, secretStruct.Data, false)

			// Once Pods are READY check each pod for updated secret keys
			verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

			// Verify secrets on pods
			testenv.VerifySecretsOnPods(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Verify Secrets on ServerConf on Pod
			testenv.VerifySplunkServerConfSecrets(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Verify Hec token on InputConf on Pod
			testenv.VerifySplunkInputConfSecrets(deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Verify Secrets via api access on Pod
			testenv.VerifySplunkSecretViaAPI(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)
		})
	})

	Context("Standalone deployment (S1)", func() {
		It("secret, smoke, s1: Secret Object data is repopulated in secret object on passing empty Data map and new secrets are applied to Splunk Pods", func() {

			// Test Scenario
			// 1. Delete Secret Passing Empty Data Map to secret Object
			// 2. Verify New versioned secret are created with new values.
			// 3. Verify New secrets are mounted on pods.
			// 4. Verify New Secrets are present in server.conf (Pass4SymmKey)
			// 5. Verify New Secrets via api access (password)

			// Create standalone Deployment with MonitoringConsoleRef
			mcName := deployment.GetName()
			standaloneSpec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
			}
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), standaloneSpec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with MonitoringConsoleRef")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Get Current Secrets Struct
			namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
			secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
			testcaseEnvInst.Log.Info("Data in secret object", "data", secretStruct.Data)
			Expect(err).To(Succeed(), "Unable to get secret struct")

			// Delete secret by passing empty Data Map
			err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, map[string][]byte{})
			Expect(err).To(Succeed(), "Unable to delete secret Object")

			// Ensure standalone is updating
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), splcommon.PhaseUpdating)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testenv.VerifySecretsOnSecretObjects(ctx, deployment, testcaseEnvInst, secretObjectNames, secretStruct.Data, false)

			// Once Pods are READY check each pod for updated secret keys
			verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

			// Verify secrets on pods
			testenv.VerifySecretsOnPods(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Verify Secrets on ServerConf on Pod
			testenv.VerifySplunkServerConfSecrets(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Verify Hec token on InputConf on Pod
			testenv.VerifySplunkInputConfSecrets(deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)

			// Verify Secrets via api access on Pod
			testenv.VerifySplunkSecretViaAPI(ctx, deployment, testcaseEnvInst, verificationPods, secretStruct.Data, false)
		})
	})
})
