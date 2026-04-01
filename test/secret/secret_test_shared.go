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

// RunS1SecretUpdateTest runs the standard S1 secret update test workflow
func RunS1SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	setup := testenv.SetupS1WithLMAndMC(ctx, deployment, testcaseEnvInst, config)

	// Update Secret Value on Secret Object
	updatedSecretData := testenv.GenerateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, setup.NamespaceScopedSecretName)

	testenv.VerifyS1SecretChangeApplied(ctx, deployment, testcaseEnvInst, config, setup, updatedSecretData, true)
}

// RunS1SecretDeleteTest runs the standard S1 secret delete test workflow
func RunS1SecretDeleteTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	setup := testenv.SetupS1WithLMAndMC(ctx, deployment, testcaseEnvInst, config)

	// Re-fetch secret struct so we can verify its data is restored after deletion
	secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), setup.NamespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Delete Secret Object
	err = testenv.DeleteSecretObject(ctx, deployment, testcaseEnvInst.GetName(), setup.NamespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to delete secret Object")

	testenv.VerifyS1SecretChangeApplied(ctx, deployment, testcaseEnvInst, config, setup, secretStruct.Data, false)
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

	testenv.VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, secretStruct.Data, false)
}

// RunC3SecretUpdateTest runs the standard C3 secret update test workflow
func RunC3SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	mcRef := deployment.GetName()
	config.DeployC3WithLicense(ctx, deployment, testcaseEnvInst, 3, true, mcRef)

	mc, resourceVersion, updatedSecretData := testenv.ApplySecretUpdateAndVerifyCMUpdating(ctx, deployment, testcaseEnvInst, config)

	testenv.VerifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)

	// Ensure Search Head Cluster goes to Ready phase
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	// Wait for PasswordSyncCompleted event on SearchHeadCluster
	shcName := deployment.GetName() + "-shc"
	err := testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), shcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on SearchHeadCluster")

	// Ensure Indexers go to Ready phase
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

	// Wait for PasswordSyncCompleted event on IndexerCluster
	idxcName := deployment.GetName() + "-idxc"
	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), idxcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on IndexerCluster")

	testenv.VerifyPostSecretChangeCluster(ctx, deployment, testcaseEnvInst, mc, resourceVersion, updatedSecretData)
}

// RunM4SecretUpdateTest runs the standard M4 secret update test workflow
func RunM4SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	siteCount := 3
	mcName := deployment.GetName()

	config.DeployM4WithLicense(ctx, deployment, testcaseEnvInst, 1, siteCount, mcName)

	mc, resourceVersion, updatedSecretData := testenv.ApplySecretUpdateAndVerifyCMUpdating(ctx, deployment, testcaseEnvInst, config)

	testenv.VerifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)
	testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)

	testenv.VerifyPostSecretChangeCluster(ctx, deployment, testcaseEnvInst, mc, resourceVersion, updatedSecretData)
}
