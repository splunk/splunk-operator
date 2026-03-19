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

var _ = Describe("Secret Test for M4 SVA", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")

		testenv.SpecifiedTestTimeout = 40000

		// Validate test prerequisites early to fail fast
		err := testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster)", func() {
		It("mastersecret, integration, m4: secret update on multisite indexers and search head cluster", func() {

			// Test Scenario
			// 1. Update Secrets Data
			// 2. Verify New versioned secret are created with correct value.
			// 3. Verify new secrets are mounted on pods.
			// 4. Verify New Secrets are present in server.conf (Pass4SymmKey)
			// 5. Verify New Secrets via api access (password)

			// Download License File and create config map
			testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

			siteCount := 3
			mcName := deployment.GetName()
			err := deployment.DeployMultisiteClusterMasterWithSearchHead(ctx, deployment.GetName(), 1, siteCount, mcName)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Wait for License Master to be in READY status
			testcaseEnvInst.VerifyLicenseMasterReady(ctx, deployment)

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterMasterReady(ctx, deployment)

			// Ensure the indexers of all sites go to Ready phase
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Ensure search head cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Ensure cluster configured as multisite
			testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

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

			// Test 1
			// Update Secrets Data and
			// Verify New versioned secret are created with correct value.
			// Verify new secrets are mounted on pods.
			// Verify New Secrets are present in server.conf (Pass4SymmKey)
			// Verify New Secrets via api access (password)

			// Update Secret Value on Secret Object
			testcaseEnvInst.Log.Info("Data in secret object", "data", secretStruct.Data)
			modifiedHecToken := testenv.GetRandomeHECToken()
			modifedValue := testenv.RandomDNSName(10)
			updatedSecretData := testenv.GetSecretDataMap(modifiedHecToken, modifedValue, modifedValue, modifedValue, modifedValue)

			err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, updatedSecretData)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// Ensure that Cluster Master goes to update phase
			testcaseEnvInst.VerifyClusterMasterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterMasterReady(ctx, deployment)

			// Wait for License Master to be in READY status
			testcaseEnvInst.VerifyLicenseMasterReady(ctx, deployment)

			// Ensure the indexers of all sites go to Ready phase
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Ensure search head cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

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

			// Once Pods are READY check each versioned secret for updated secret keys
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
