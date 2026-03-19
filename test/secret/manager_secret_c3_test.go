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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Secret Test for SVA C3", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")

		// Validate test prerequisites early to fail fast
		err := testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("managersecret, smoke, c3: secret update on indexers and search head cluster", func() {

			// Test Scenario
			// 1. Update Secrets Data
			// 2. Verify New versioned secret are created with correct value.
			// 3. Verify new secrets are mounted on pods.
			// 4. Verify New Secrets are present in server.conf (Pass4SymmKey)
			// 5. Verify New Secrets via api access (password)

			// Download License File and create config map
			testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

			mcRef := deployment.GetName()
			err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Wait for License Manager to be in READY status
			testcaseEnvInst.VerifyLicenseManagerReady(ctx, deployment)

			// Ensure that the cluster-manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Ensure Search Head Cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Ensure Indexers go to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

			// Verify RF SF is met
			testcaseEnvInst.Log.Info("Checkin RF SF before secret change")
			testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

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
			testcaseEnvInst.VerifyClusterManagerPhase(ctx, deployment, enterpriseApi.PhaseUpdating)

			// Wait for License Manager to be in READY status
			testcaseEnvInst.VerifyLicenseManagerReady(ctx, deployment)

			// Ensure that the cluster-manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Ensure Search Head Cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Ensure Indexers go to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// wait for custom resource resource version to change
			testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Verify RF SF is met
			testcaseEnvInst.Log.Info("Checkin RF SF after secret change")
			testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

			// Once Pods are READY check each versioned secret for updated secret keys
			secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

			// Verify Secrets on versioned secret objects
			testcaseEnvInst.VerifySecretsOnSecretObjects(ctx, deployment, secretObjectNames, updatedSecretData, true)

			// Once Pods are READY check each pod for updated secret keys
			verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

			// Verify secrets on pods
			testcaseEnvInst.VerifySecretsOnPods(ctx, deployment, verificationPods, updatedSecretData, true)

			// Verify Pass4SymmKey Secrets on ServerConf on MC, LM Pods
			testcaseEnvInst.VerifySplunkServerConfSecrets(ctx, deployment, verificationPods, updatedSecretData, true)

			// Verify Hec token on InputConf on Pod
			testcaseEnvInst.VerifySplunkInputConfSecrets(deployment, verificationPods, updatedSecretData, true)

			// Verify Secrets via api access on Pod
			testcaseEnvInst.VerifySplunkSecretViaAPI(ctx, deployment, verificationPods, updatedSecretData, true)
		})
	})
})
