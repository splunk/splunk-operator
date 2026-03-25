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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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

// generateAndApplySecretUpdate creates randomized secret data and applies it to the namespace-scoped
// secret object, returning the updated data map for subsequent verification.
func generateAndApplySecretUpdate(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, namespaceScopedSecretName string) map[string][]byte {
	modifiedHecToken := testenv.GetRandomeHECToken()
	modifiedValue := testenv.RandomDNSName(10)
	updatedSecretData := testenv.GetSecretDataMap(modifiedHecToken, modifiedValue, modifiedValue, modifiedValue, modifiedValue)
	err := testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, updatedSecretData)
	Expect(err).To(Succeed(), "Unable to update secret Object")
	return updatedSecretData
}

// RunS1SecretUpdateTest runs the standard S1 secret update test workflow
func RunS1SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	// Download License File and create config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	// Create standalone Deployment with License Manager
	mcRef := deployment.GetName()
	var standalone *enterpriseApi.Standalone
	var err error

	standalone, err = config.DeployStandaloneWithLM(ctx, deployment, deployment.GetName(), mcRef)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

	verifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)

	// Deploy and verify Monitoring Console
	mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), deployment.GetName())

	// Get Current Secrets Struct
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err = testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Update Secret Value on Secret Object
	updatedSecretData := generateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, namespaceScopedSecretName)

	// Ensure standalone is updating
	testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)

	verifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)

	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	verifySecretsPropagated(ctx, deployment, testcaseEnvInst, updatedSecretData, true)
}

// RunS1SecretDeleteTest runs the standard S1 secret delete test workflow
func RunS1SecretDeleteTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	// Download License File and create config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	// Create standalone Deployment with License Manager
	mcRef := deployment.GetName()
	var standalone *enterpriseApi.Standalone
	var err error

	standalone, err = config.DeployStandaloneWithLM(ctx, deployment, deployment.GetName(), mcRef)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

	verifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)

	// Deploy and verify Monitoring Console
	mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), deployment.GetName())

	// Get Current Secrets Struct
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Delete Secret Object
	err = testenv.DeleteSecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to delete secret Object")

	// Ensure standalone is updating
	testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)

	verifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)

	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	verifySecretsPropagated(ctx, deployment, testcaseEnvInst, secretStruct.Data, false)
}

// RunS1SecretDeleteWithMCRefTest runs the S1 secret delete test with MC reference workflow
func RunS1SecretDeleteWithMCRefTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	// Create standalone Deployment with MonitoringConsoleRef
	var standalone *enterpriseApi.Standalone
	var err error

	mcName := deployment.GetName()
	standaloneSpec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				ImagePullPolicy: "IfNotPresent",
				Image:           testcaseEnvInst.GetSplunkImage(),
			},
			Volumes: []corev1.Volume{},
			MonitoringConsoleRef: corev1.ObjectReference{
				Name: mcName,
			},
		},
	}
	standalone, err = deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), standaloneSpec)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with MonitoringConsoleRef")

	// Wait for Standalone to be in READY status
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

	// Deploy and verify Monitoring Console
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Get revision number of the resource
	resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Get Current Secrets Struct
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Delete Secret Object
	err = testenv.DeleteSecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
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
	// Download License File and create config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	mcRef := deployment.GetName()
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	verifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)
	testcaseEnvInst.VerifyC3ComponentsReady(ctx, deployment)

	// Wait for ClusterInitialized event to confirm cluster is fully initialized
	idxcName := deployment.GetName() + "-idxc"
	err = testcaseEnvInst.WaitForClusterInitialized(ctx, deployment, testcaseEnvInst.GetName(), idxcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for ClusterInitialized event on IndexerCluster")

	mc, resourceVersion, namespaceScopedSecretName := deployMCAndVerifyInitialSecret(ctx, deployment, testcaseEnvInst)

	// Update Secret Value on Secret Object
	updatedSecretData := generateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, namespaceScopedSecretName)

	config.VerifyClusterManagerPhaseUpdating(ctx, deployment, testcaseEnvInst)

	verifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)

	// Ensure Search Head Cluster go to Ready phase
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	// Wait for PasswordSyncCompleted event on SearchHeadCluster
	shcName := deployment.GetName() + "-shc"
	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), shcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on SearchHeadCluster")

	// Ensure Indexers go to Ready phase
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

	// Wait for PasswordSyncCompleted event on IndexerCluster
	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), idxcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on IndexerCluster")

	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	// Verify RF SF is met
	testcaseEnvInst.Log.Info("Checkin RF SF after secret change")
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	verifySecretsPropagated(ctx, deployment, testcaseEnvInst, updatedSecretData, true)
}

func deployMCAndVerifyInitialSecret(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv) (*enterpriseApi.MonitoringConsole, string, string) {
	mc, resourceVersion := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), deployment.GetName())
	testcaseEnvInst.Log.Info("Checkin RF SF before secret change")
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")
	return mc, resourceVersion, namespaceScopedSecretName
}

// RunM4SecretUpdateTest runs the standard M4 secret update test workflow
func RunM4SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	// Download License File and create config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	siteCount := 3
	mcName := deployment.GetName()
	var err error

	err = config.DeployMultisiteCluster(ctx, deployment, deployment.GetName(), 1, siteCount, mcName)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	verifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)
	testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)

	mc, resourceVersion, namespaceScopedSecretName := deployMCAndVerifyInitialSecret(ctx, deployment, testcaseEnvInst)

	// Update Secret Value on Secret Object
	updatedSecretData := generateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, namespaceScopedSecretName)

	config.VerifyClusterManagerPhaseUpdating(ctx, deployment, testcaseEnvInst)

	verifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)
	testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)

	testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)

	// Verify RF SF is met
	testcaseEnvInst.Log.Info("Checkin RF SF after secret change")
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	verifySecretsPropagated(ctx, deployment, testcaseEnvInst, updatedSecretData, true)
}
