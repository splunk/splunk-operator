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

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// RunS1SecretUpdateTest runs the standard S1 secret update test workflow
func RunS1SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	Expect(testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)).To(Succeed(), "Unable to setup license config map")

	standalone, err := config.DeployStandaloneWithLM(ctx, deployment, deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance with LM")

	Expect(testenv.VerifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)).To(Succeed(), "LM or Standalone not ready")

	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	updatedSecretData, err := testenv.GenerateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to generate and apply secret update")

	Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseUpdating)).To(Succeed(), "Standalone did not reach Updating phase")
	Expect(testenv.VerifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)).To(Succeed(), "LM or Standalone not ready after secret update")
	Expect(testenv.VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, updatedSecretData, true)).To(Succeed(), "Secrets not propagated after update")
}

// RunS1SecretDeleteTest runs the standard S1 secret delete test workflow
func RunS1SecretDeleteTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	Expect(testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)).To(Succeed(), "Unable to setup license config map")

	standalone, err := config.DeployStandaloneWithLM(ctx, deployment, deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance with LM")

	Expect(testenv.VerifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)).To(Succeed(), "LM or Standalone not ready")

	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	err = testenv.DeleteSecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to delete secret Object")

	Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseUpdating)).To(Succeed(), "Standalone did not reach Updating phase")
	Expect(testenv.VerifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone)).To(Succeed(), "LM or Standalone not ready after secret delete")
	Expect(testenv.VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, secretStruct.Data, false)).To(Succeed(), "Secrets not propagated after delete")
}

// RunS1SecretDeleteWithMCRefTest runs the S1 secret delete test verifying secrets are
// propagated when passing an empty Data map to the secret object.
func RunS1SecretDeleteWithMCRefTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	standalone, err := testcaseEnvInst.DeployStandaloneWithMCRef(ctx, deployment, deployment.GetName(), deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance")

	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	testcaseEnvInst.Log.Info("Data in secret object", "data", secretStruct.Data)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Delete secret by passing empty Data Map
	err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, map[string][]byte{})
	Expect(err).To(Succeed(), "Unable to delete secret Object")

	// Ensure standalone reaches Updating phase and returns to Ready
	Expect(testcaseEnvInst.VerifyStandalonePhaseAndReady(ctx, deployment, enterpriseApi.PhaseUpdating, standalone)).To(Succeed(), "Standalone did not reach Updating phase or not ready after secret delete")

	Expect(testenv.VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, secretStruct.Data, false)).To(Succeed(), "Secrets not propagated after delete")
}

// RunC3SecretUpdateTest runs the standard C3 secret update test workflow
func RunC3SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	Expect(config.DeployC3WithLicense(ctx, deployment, testcaseEnvInst, 3, true)).To(Succeed(), "Unable to deploy C3 with license")

	testcaseEnvInst.Log.Info("Checking RF SF before secret change")
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met before secret change")

	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	updatedSecretData, err := testenv.GenerateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to generate and apply secret update")

	Expect(config.VerifyClusterManagerPhaseUpdating(ctx, deployment, testcaseEnvInst)).To(Succeed(), "Cluster Manager did not enter Updating phase")

	Expect(testenv.VerifyLMAndClusterManagerReady(ctx, deployment, testcaseEnvInst, config)).To(Succeed(), "LM and Cluster Manager not ready")

	idxcName := deployment.GetName() + "-idxc"
	Expect(testcaseEnvInst.WatchForIndexerClusterPhase(ctx, deployment, testcaseEnvInst.GetName(), idxcName, enterpriseApi.PhaseReady, testenv.SecretUpdateClusterReadyTimeout)).To(Succeed(), "Indexers not ready")

	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), idxcName, testenv.PasswordSyncEventTimeout)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on IndexerCluster")

	shcInstance := deployment.GetName() + "-shc"
	Expect(testcaseEnvInst.WatchForSearchHeadClusterPhase(ctx, deployment, testcaseEnvInst.GetName(), shcInstance, enterpriseApi.PhaseReady, testenv.SecretUpdateClusterReadyTimeout)).To(Succeed(), "Search Head Cluster not ready")

	err = testcaseEnvInst.WaitForPasswordSyncCompleted(ctx, deployment, testcaseEnvInst.GetName(), shcInstance, testenv.PasswordSyncEventTimeout)
	Expect(err).To(Succeed(), "Timed out waiting for PasswordSyncCompleted event on SearchHeadCluster")

	testcaseEnvInst.Log.Info("Checking RF SF after secret change")
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met after secret change")
	Expect(testenv.VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, updatedSecretData, true)).To(Succeed(), "Secrets not propagated")
}

// RunM4SecretUpdateTest runs the standard M4 secret update test workflow
func RunM4SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	siteCount := 3
	Expect(config.DeployM4WithLicense(ctx, deployment, testcaseEnvInst, 1, siteCount)).To(Succeed(), "Unable to deploy M4 with license")

	testcaseEnvInst.Log.Info("Checking RF SF before secret change")
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met before secret change")

	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	updatedSecretData, err := testenv.GenerateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to generate and apply secret update")

	Expect(config.VerifyClusterManagerPhaseUpdating(ctx, deployment, testcaseEnvInst)).To(Succeed(), "Cluster Manager did not enter Updating phase")

	Expect(config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)).To(Succeed(), "License Manager not ready")
	Expect(testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount, func() error {
		return config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)
	})).To(Succeed(), "M4 components not ready")

	testcaseEnvInst.Log.Info("Checking RF SF after secret change")
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met after secret change")
	Expect(testenv.VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, updatedSecretData, true)).To(Succeed(), "Secrets not propagated")
}
