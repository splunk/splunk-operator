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

// SecretTestConfig holds configuration for secret tests to support both v3 and v4 API versions
type SecretTestConfig struct {
	LicenseManagerReady func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv)
	ClusterManagerReady func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv)
	APIVersion          string
}

// NewSecretTestConfigV3 creates configuration for v3 API (LicenseMaster/ClusterMaster)
func NewSecretTestConfigV3() *SecretTestConfig {
	return &SecretTestConfig{
		LicenseManagerReady: func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv) {
			testcaseEnv.VerifyLicenseMasterReady(ctx, deployment)
		},
		ClusterManagerReady: func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv) {
			testcaseEnv.VerifyClusterMasterReady(ctx, deployment)
		},
		APIVersion: "v3",
	}
}

// NewSecretTestConfigV4 creates configuration for v4 API (LicenseManager/ClusterManager)
func NewSecretTestConfigV4() *SecretTestConfig {
	return &SecretTestConfig{
		LicenseManagerReady: func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv) {
			testcaseEnv.VerifyLicenseManagerReady(ctx, deployment)
		},
		ClusterManagerReady: func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv) {
			testcaseEnv.VerifyClusterManagerReady(ctx, deployment)
		},
		APIVersion: "v4",
	}
}

// RunS1SecretUpdateTest runs the standard S1 secret update test workflow
func RunS1SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *SecretTestConfig) {
	// Download License File and create config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	// Create standalone Deployment with License Manager
	mcRef := deployment.GetName()
	var standalone *enterpriseApi.Standalone
	var err error

	if config.APIVersion == "v3" {
		standalone, err = deployment.DeployStandaloneWithLMaster(ctx, deployment.GetName(), mcRef)
	} else {
		standalone, err = deployment.DeployStandaloneWithLM(ctx, deployment.GetName(), mcRef)
	}
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

	// Wait for License Manager to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Wait for Standalone to be in READY status
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

	// Deploy Monitoring Console CRD
	mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Get revision number of the resource
	resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Get Current Secrets Struct
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err = testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Update Secret Value on Secret Object
	modifiedHecToken := testenv.GetRandomeHECToken()
	modifiedValue := testenv.RandomDNSName(10)
	updatedSecretData := testenv.GetSecretDataMap(modifiedHecToken, modifiedValue, modifiedValue, modifiedValue, modifiedValue)
	err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, updatedSecretData)
	Expect(err).To(Succeed(), "Unable to update secret Object")

	// Ensure standalone is updating
	testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)

	// Wait for License Manager to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Wait for Standalone to be in READY status
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

	// Wait for custom resource resource version to change
	testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Once Pods are READY check each versioned secret for updated secret keys
	secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

	// Verify Secrets on versioned secret objects
	testcaseEnvInst.VerifySecretsOnSecretObjects(ctx, deployment, secretObjectNames, updatedSecretData, true)

	// Once Pods are READY check each pod for updated secret keys
	verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

	// Verify secrets on pods
	testcaseEnvInst.VerifySecretsOnPods(ctx, deployment, verificationPods, updatedSecretData, true)

	// Verify Secrets on ServerConf on Pod
	testcaseEnvInst.VerifySplunkServerConfSecrets(ctx, deployment, verificationPods, updatedSecretData, true)

	// Verify Hec token on InputConf on Pod
	testcaseEnvInst.VerifySplunkInputConfSecrets(deployment, verificationPods, updatedSecretData, true)

	// Verify Secrets via api access on Pod
	testcaseEnvInst.VerifySplunkSecretViaAPI(ctx, deployment, verificationPods, updatedSecretData, true)
}

// RunS1SecretDeleteTest runs the standard S1 secret delete test workflow
func RunS1SecretDeleteTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *SecretTestConfig) {
	// Download License File and create config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	// Create standalone Deployment with License Manager
	mcRef := deployment.GetName()
	var standalone *enterpriseApi.Standalone
	var err error

	if config.APIVersion == "v3" {
		standalone, err = deployment.DeployStandaloneWithLMaster(ctx, deployment.GetName(), mcRef)
	} else {
		standalone, err = deployment.DeployStandaloneWithLM(ctx, deployment.GetName(), mcRef)
	}
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

	// Wait for License Manager to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Wait for Standalone to be in READY status
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

	// Deploy Monitoring Console CRD
	mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

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

	// Wait for License Manager to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Wait for Standalone to be in READY status
	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

	// Wait for custom resource resource version to change
	testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Once Pods are READY check each versioned secret for updated secret keys
	secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

	// Verify Secrets on versioned secret objects
	testcaseEnvInst.VerifySecretsOnSecretObjects(ctx, deployment, secretObjectNames, secretStruct.Data, false)

	// Once Pods are READY check each pod for updated secret keys
	verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

	// Verify secrets on pods
	testcaseEnvInst.VerifySecretsOnPods(ctx, deployment, verificationPods, secretStruct.Data, false)

	// Verify Secrets on ServerConf on Pod
	testcaseEnvInst.VerifySplunkServerConfSecrets(ctx, deployment, verificationPods, secretStruct.Data, false)

	// Verify Hec token on InputConf on Pod
	testcaseEnvInst.VerifySplunkInputConfSecrets(deployment, verificationPods, secretStruct.Data, false)

	// Verify Secrets via api access on Pod
	testcaseEnvInst.VerifySplunkSecretViaAPI(ctx, deployment, verificationPods, secretStruct.Data, false)
}

// RunS1SecretDeleteWithMCRefTest runs the S1 secret delete test with MC reference workflow
func RunS1SecretDeleteWithMCRefTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *SecretTestConfig) {
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

	// Deploy Monitoring Console CRD
	mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

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

	// Wait for custom resource resource version to change
	testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Once Pods are READY check each versioned secret for updated secret keys
	secretObjectNames := testenv.GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

	// Verify Secrets on versioned secret objects
	testcaseEnvInst.VerifySecretsOnSecretObjects(ctx, deployment, secretObjectNames, secretStruct.Data, false)

	// Once Pods are READY check each pod for updated secret keys
	verificationPods := testenv.DumpGetPods(testcaseEnvInst.GetName())

	// Verify secrets on pods
	testcaseEnvInst.VerifySecretsOnPods(ctx, deployment, verificationPods, secretStruct.Data, false)

	// Verify Secrets on ServerConf on Pod
	testcaseEnvInst.VerifySplunkServerConfSecrets(ctx, deployment, verificationPods, secretStruct.Data, false)

	// Verify Hec token on InputConf on Pod
	testcaseEnvInst.VerifySplunkInputConfSecrets(deployment, verificationPods, secretStruct.Data, false)

	// Verify Secrets via api access on Pod
	testcaseEnvInst.VerifySplunkSecretViaAPI(ctx, deployment, verificationPods, secretStruct.Data, false)
}

// RunC3SecretUpdateTest runs the standard C3 secret update test workflow
func RunC3SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *SecretTestConfig) {
	// Download License File and create config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	mcRef := deployment.GetName()
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Wait for License Manager to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Ensure that the cluster-manager goes to Ready phase
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

	// Ensure Search Head Cluster go to Ready phase
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	// Ensure Indexers go to Ready phase
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

	// Wait for ClusterInitialized event to confirm cluster is fully initialized
	idxcName := deployment.GetName() + "-idxc"
	err = testcaseEnvInst.WaitForClusterInitialized(ctx, deployment, testcaseEnvInst.GetName(), idxcName, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for ClusterInitialized event on IndexerCluster")

	// Deploy Monitoring Console CRD
	mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Get revision number of the resource
	resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Verify RF SF is met
	testcaseEnvInst.Log.Info("Checkin RF SF before secret change")
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	// Get Current Secrets Struct
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err = testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Update Secret Value on Secret Object
	modifiedHecToken := testenv.GetRandomeHECToken()
	modifiedValue := testenv.RandomDNSName(10)
	updatedSecretData := testenv.GetSecretDataMap(modifiedHecToken, modifiedValue, modifiedValue, modifiedValue, modifiedValue)

	err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, updatedSecretData)
	Expect(err).To(Succeed(), "Unable to update secret Object")

	// Ensure that Cluster Manager goes to update phase
	if config.APIVersion == "v3" {
		testcaseEnvInst.VerifyClusterMasterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)
	} else {
		testcaseEnvInst.VerifyClusterManagerPhase(ctx, deployment, enterpriseApi.PhaseUpdating)
	}

	// Wait for License Manager to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Ensure that the cluster-manager goes to Ready phase
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

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

	// Wait for custom resource resource version to change
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
}

// RunM4SecretUpdateTest runs the standard M4 secret update test workflow
func RunM4SecretUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *SecretTestConfig) {
	// Download License File and create config map
	testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)

	siteCount := 3
	mcName := deployment.GetName()
	var err error

	if config.APIVersion == "v3" {
		err = deployment.DeployMultisiteClusterMasterWithSearchHead(ctx, deployment.GetName(), 1, siteCount, mcName)
	} else {
		err = deployment.DeployMultisiteClusterWithSearchHead(ctx, deployment.GetName(), 1, siteCount, mcName)
	}
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Wait for License Manager to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Ensure that the cluster-manager goes to Ready phase
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

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

	// Get revision number of the resource
	resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, mc)

	// Verify RF SF is met
	testcaseEnvInst.Log.Info("Checkin RF SF before secret change")
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	// Get Current Secrets Struct
	namespaceScopedSecretName := fmt.Sprintf(testenv.NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err = testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	Expect(err).To(Succeed(), "Unable to get secret struct")

	// Update Secret Value on Secret Object
	modifiedHecToken := testenv.GetRandomeHECToken()
	modifiedValue := testenv.RandomDNSName(10)
	updatedSecretData := testenv.GetSecretDataMap(modifiedHecToken, modifiedValue, modifiedValue, modifiedValue, modifiedValue)

	err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName, updatedSecretData)
	Expect(err).To(Succeed(), "Unable to update secret Object")

	// Ensure that Cluster Manager goes to update phase
	if config.APIVersion == "v3" {
		testcaseEnvInst.VerifyClusterMasterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)
	} else {
		testcaseEnvInst.VerifyClusterManagerPhase(ctx, deployment, enterpriseApi.PhaseUpdating)
	}

	// Ensure that the cluster-manager goes to Ready phase
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

	// Wait for License Manager to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Ensure the indexers of all sites go to Ready phase
	testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

	// Ensure search head cluster go to Ready phase
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	// Wait for custom resource resource version to change
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
}
