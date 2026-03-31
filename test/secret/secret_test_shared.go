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
package secret

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// verifySecretsPropagated checks that the given secret data has been propagated to all
// versioned secret objects, pods, server config, input config, and via the API.
func verifySecretsPropagated(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, secretData map[string][]byte, updated bool) {
	// Once Pods are READY check each versioned secret for updated secret keys
	secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

	// Verify Secrets on versioned secret objects
	testcaseEnvInst.VerifySecretsOnSecretObjects(ctx, deployment, secretObjectNames, secretData, updated)

	// Once Pods are READY check each pod for updated secret keys
	verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

	// Verify secrets on pods
	testcaseEnvInst.VerifySecretsOnPods(ctx, deployment, verificationPods, secretData, updated)

	// Verify Secrets on ServerConf on Pod
	testcaseEnvInst.VerifySplunkServerConfSecrets(ctx, deployment, verificationPods, secretData, updated)

	// Verify Hec token on InputConf on Pod
	testcaseEnvInst.VerifySplunkInputConfSecrets(deployment, verificationPods, secretData, updated)

	// Verify Secrets via api access on Pod
	testcaseEnvInst.VerifySplunkSecretViaAPI(ctx, deployment, verificationPods, secretData, updated)
}

// verifyLMAndStandaloneReady waits for License Manager then Standalone to reach READY status.
func verifyLMAndStandaloneReady(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, standalone *enterpriseApi.Standalone) {
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)
}

// verifyLMAndClusterManagerReady waits for License Manager then Cluster Manager to reach READY status.
func verifyLMAndClusterManagerReady(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)
}

// s1WithLMSetup holds the resources created by setupS1WithLMAndMC so that
// individual test functions can operate on them without repeating the setup.
type s1WithLMSetup struct {
	standalone                *enterpriseApi.Standalone
	mc                        *enterpriseApi.MonitoringConsole
	resourceVersion           string
	namespaceScopedSecretName string
}

// setupS1WithLMAndMC performs the common S1 setup shared by the secret-update
// and secret-delete tests: license config map, standalone with LM, MC, and
// initial secret verification.
func setupS1WithLMAndMC(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) s1WithLMSetup {
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	mcRef := deployment.GetName()
	standalone, err := config.DeployStandaloneWithLM(ctx, deployment, deployment.GetName(), mcRef)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

	verifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)

	mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), deployment.GetName())

	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err = testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	return s1WithLMSetup{
		standalone:                standalone,
		mc:                        mc,
		resourceVersion:           resourceVersion,
		namespaceScopedSecretName: namespaceScopedSecretName,
	}
}

// verifyS1SecretChangeApplied verifies that a secret change (update or delete)
// has been applied to the S1 stack: standalone enters Updating phase, LM and
// standalone return to Ready, MC version changes, and secrets are propagated.
func verifyS1SecretChangeApplied(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, setup s1WithLMSetup, secretData map[string][]byte, updated bool) {
	testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)
	verifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, setup.standalone)
	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, setup.mc, setup.resourceVersion)
	verifySecretsPropagated(ctx, deployment, testcaseEnvInst, secretData, updated)
}

// generateAndApplySecretUpdate creates randomized secret data and applies it to the namespace-scoped
// secret object, returning the updated data map for subsequent verification.
func generateAndApplySecretUpdate(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, namespaceScopedSecretName string) map[string][]byte {
	modifiedHecToken := testenv.GetRandomHECToken()
	modifiedValue := testenv.RandomDNSName(10)
	updatedSecretData := testenv.GetSecretDataMap(modifiedHecToken, modifiedValue, modifiedValue, modifiedValue, modifiedValue)
	err := testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, updatedSecretData)
	Expect(err).To(Succeed(), "Unable to update secret Object")
	return updatedSecretData
}

// RunS1SecretUpdateTest runs the standard S1 secret update test workflow
func RunS1SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	setup := setupS1WithLMAndMC(ctx, deployment, testcaseEnvInst, config)

	// Update Secret Value on Secret Object
	updatedSecretData := generateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, setup.namespaceScopedSecretName)

	verifyS1SecretChangeApplied(ctx, deployment, testcaseEnvInst, config, setup, updatedSecretData, true)
}

// RunS1SecretDeleteTest runs the standard S1 secret delete test workflow
func RunS1SecretDeleteTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	setup := setupS1WithLMAndMC(ctx, deployment, testcaseEnvInst, config)

	// Re-fetch secret struct so we can verify its data is restored after deletion
	secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), setup.namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Delete Secret Object
	err = testenv.DeleteSecretObject(ctx, deployment, testcaseEnvInst.GetName(), setup.namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to delete secret Object")

	verifyS1SecretChangeApplied(ctx, deployment, testcaseEnvInst, config, setup, secretStruct.Data, false)
}

// RunS1SecretDeleteWithMCRefTest runs the S1 secret delete test with MC reference workflow
func RunS1SecretDeleteWithMCRefTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	// Create standalone Deployment with MonitoringConsoleRef
	mcName := deployment.GetName()
	standalone := testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, deployment.GetName(), mcName)

	// Deploy and verify Monitoring Console
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

	// Get revision number of the resource
	resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Get Current Secrets Struct
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	testcaseEnvInst.Log.Info("Data in secret object", "data", secretStruct.Data)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Delete secret by passing empty Data Map
	err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, map[string][]byte{})
	Expect(err).To(Succeed(), "Unable to delete secret Object")

	// Ensure standalone is updating
	testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)

	// Wait for Standalone to be in READY status
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	verifySecretsPropagated(ctx, deployment, testcaseEnvInst, secretStruct.Data, false)
}

// RunC3SecretUpdateTest runs the standard C3 secret update test workflow
func RunC3SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	// Set up license config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	mcRef := deployment.GetName()
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)
	config.VerifyC3ClusterReady(ctx, deployment, testcaseEnvInst)

	mc, resourceVersion, updatedSecretData := applySecretUpdateAndVerifyCMUpdating(ctx, deployment, testcaseEnvInst, config)

	verifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)

	// Ensure Search Head Cluster goes to Ready phase
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	// Wait for PasswordSyncCompleted event on SearchHeadCluster
	shcName := deployment.GetName() + "-shc"
	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), shcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on SearchHeadCluster")

	// Ensure Indexers go to Ready phase
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

	// Wait for PasswordSyncCompleted event on IndexerCluster
	idxcName := deployment.GetName() + "-idxc"
	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), idxcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on IndexerCluster")

	verifyPostSecretChangeCluster(ctx, deployment, testcaseEnvInst, mc, resourceVersion, updatedSecretData)
}

// verifyPostSecretChangeCluster performs the common tail verification after a
// secret change on a clustered deployment: MC version changed, RF/SF met, and
// secrets propagated to all pods.
func verifyPostSecretChangeCluster(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, mc *enterpriseApi.MonitoringConsole, resourceVersion string, updatedSecretData map[string][]byte) {
	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	testcaseEnvInst.Log.Info("Checking RF SF after secret change")
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	verifySecretsPropagated(ctx, deployment, testcaseEnvInst, updatedSecretData, true)
}

// applySecretUpdateAndVerifyCMUpdating deploys MC, verifies RF/SF and initial secret state,
// applies a secret update, and confirms the Cluster Manager enters the Updating phase.
// Returns the MC, its resource version, and the updated secret data for post-change verification.
func applySecretUpdateAndVerifyCMUpdating(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) (*enterpriseApi.MonitoringConsole, string, map[string][]byte) {
	mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), deployment.GetName())
	testcaseEnvInst.Log.Info("Checking RF SF before secret change")
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")
	updatedSecretData := generateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, namespaceScopedSecretName)
	config.VerifyClusterManagerPhaseUpdating(ctx, deployment, testcaseEnvInst)
	return mc, resourceVersion, updatedSecretData
}

// RunM4SecretUpdateTest runs the standard M4 secret update test workflow
func RunM4SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	// Set up license config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	siteCount := 3
	mcName := deployment.GetName()

	err := config.DeployMultisiteCluster(ctx, deployment, deployment.GetName(), 1, siteCount, mcName)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	verifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)
	testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)

	mc, resourceVersion, updatedSecretData := applySecretUpdateAndVerifyCMUpdating(ctx, deployment, testcaseEnvInst, config)

	verifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)
	testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)

	verifyPostSecretChangeCluster(ctx, deployment, testcaseEnvInst, mc, resourceVersion, updatedSecretData)
}
