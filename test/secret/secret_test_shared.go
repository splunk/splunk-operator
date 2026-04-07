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
	setup, err := testenv.SetupS1WithLMAndMC(ctx, deployment, testcaseEnvInst, config)
	Expect(err).To(Succeed(), "Unable to setup S1 with LM and MC")

	// Update Secret Value on Secret Object
	updatedSecretData, err := testenv.GenerateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, setup.NamespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to generate and apply secret update")

	Expect(testenv.VerifyS1SecretChangeApplied(ctx, deployment, testcaseEnvInst, config, setup, updatedSecretData, true)).To(Succeed(), "S1 secret change not applied")
}

// RunS1SecretDeleteTest runs the standard S1 secret delete test workflow
func RunS1SecretDeleteTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	setup, err := testenv.SetupS1WithLMAndMC(ctx, deployment, testcaseEnvInst, config)
	Expect(err).To(Succeed(), "Unable to setup S1 with LM and MC")

	// Re-fetch secret struct so we can verify its data is restored after deletion
	secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), setup.NamespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Delete Secret Object
	err = testenv.DeleteSecretObject(ctx, deployment, testcaseEnvInst.GetName(), setup.NamespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to delete secret Object")

	Expect(testenv.VerifyS1SecretChangeApplied(ctx, deployment, testcaseEnvInst, config, setup, secretStruct.Data, false)).To(Succeed(), "S1 secret delete not applied")
}

// RunS1SecretDeleteWithMCRefTest runs the S1 secret delete test with MC reference workflow
func RunS1SecretDeleteWithMCRefTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	// Create standalone Deployment with MonitoringConsoleRef
	mcRef := deployment.GetName()
	standalone, err := testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, deployment.GetName(), mcRef)
	Expect(err).To(Succeed(), "Unable to deploy Standalone with MC reference")

	// Deploy and verify Monitoring Console
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

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

	// Ensure standalone reaches Updating phase and returns to Ready
	Expect(testcaseEnvInst.VerifyStandalonePhaseAndReady(ctx, deployment, enterpriseApi.PhaseUpdating, standalone)).To(Succeed(), "Standalone did not reach Updating phase or not ready after secret delete")

	Expect(testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion)).To(Succeed(), "MC version not changed or not ready")

	Expect(testenv.VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, secretStruct.Data, false)).To(Succeed(), "Secrets not propagated after delete")
}

// RunC3SecretUpdateTest runs the standard C3 secret update test workflow
func RunC3SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	mcRef := deployment.GetName()
	Expect(config.DeployC3WithLicense(ctx, deployment, testcaseEnvInst, 3, true, mcRef)).To(Succeed(), "Unable to deploy C3 with license")

	mc, resourceVersion, updatedSecretData, err := testenv.ApplySecretUpdateAndVerifyCMUpdating(ctx, deployment, testcaseEnvInst, config)
	Expect(err).To(Succeed(), "Unable to apply secret update and verify CM updating")

	Expect(testenv.VerifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)).To(Succeed(), "LM and Cluster Manager not ready")

	// Ensure Search Head Cluster goes to Ready phase
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed(), "Search Head Cluster not ready")

	// Wait for PasswordSyncCompleted event on SearchHeadCluster
	shcName := deployment.GetName() + "-shc"
	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), shcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on SearchHeadCluster")

	// Ensure Indexers go to Ready phase
	Expect(testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)).To(Succeed(), "Indexers not ready")

	// Wait for PasswordSyncCompleted event on IndexerCluster
	idxcName := deployment.GetName() + "-idxc"
	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), idxcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on IndexerCluster")

	Expect(testenv.VerifyPostSecretChangeCluster(ctx, deployment, testcaseEnvInst, mc, resourceVersion, updatedSecretData)).To(Succeed(), "Post secret change cluster verification failed")
}

// RunM4SecretUpdateTest runs the standard M4 secret update test workflow
func RunM4SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	siteCount := 3
	mcRef := deployment.GetName()

	Expect(config.DeployM4WithLicense(ctx, deployment, testcaseEnvInst, 1, siteCount, mcRef)).To(Succeed(), "Unable to deploy M4 with license")

	mc, resourceVersion, updatedSecretData, err := testenv.ApplySecretUpdateAndVerifyCMUpdating(ctx, deployment, testcaseEnvInst, config)
	Expect(err).To(Succeed(), "Unable to apply secret update and verify CM updating")

	Expect(config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)).To(Succeed(), "License Manager not ready")
	Expect(testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount, func() error {
		return config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)
	})).To(Succeed(), "M4 components not ready")

	Expect(testenv.VerifyPostSecretChangeCluster(ctx, deployment, testcaseEnvInst, mc, resourceVersion, updatedSecretData)).To(Succeed(), "Post secret change cluster verification failed")
}
