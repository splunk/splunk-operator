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
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("secret test", func() {

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

			// Get Current Secrets
			secretName := fmt.Sprintf(testenv.SecretObjectName, testenvInstance.GetName())
			secretObj := testenv.GetSecretObject(deployment, testenvInstance.GetName(), secretName)

			// Update Secret Values
			modifedKeyValue := "whatever"
			hecToken := testenv.DecodeBase64(secretObj.Data.HecToken)
			modifiedHecToken := hecToken[:len(hecToken)-2] + testenv.RandomDNSName(2)
			secretObj.Data.HecToken = testenv.EncodeBase64(modifiedHecToken)
			secretObj.Data.Password = testenv.EncodeBase64(modifedKeyValue)
			secretObj.Data.Pass4SymmKey = testenv.EncodeBase64(modifedKeyValue)
			err = testenv.UpdateSecret(deployment, testenvInstance.GetName(), secretObj, false /*delete*/)
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
			standaloneSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "standalone", 2)
			licenseMasterSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "license-master", 2)
			monitoringConsoleSecretName := fmt.Sprintf(testenv.SecretObjectPodName, testenvInstance.GetName(), "monitoring-console", 2)
			verificationSecrets := []string{standaloneSecretName, licenseMasterSecretName, monitoringConsoleSecretName}

			// Verify that HEC TOKEN is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that Admin Password is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that Pass4SymmKey is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Once Pods are READY check each pod for updated secret keys
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			licenseMasterPodName := fmt.Sprintf(testenv.LicenseMasterPod, deployment.GetName(), 0)
			monitoringConsolePodName := fmt.Sprintf(testenv.MonitoringConsolePod, testenvInstance.GetName(), 0)
			verificationPods := []string{standalonePodName, licenseMasterPodName, monitoringConsolePodName}

			// Verify that HEC TOKEN is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that Admin Password is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that Pass4SymmKey is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Delete secret key
			err = testenv.UpdateSecret(deployment, testenvInstance.GetName(), secretObj, true /*delete*/)
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
			standaloneSecretName = fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "standalone", 3)
			licenseMasterSecretName = fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "license-master", 3)
			monitoringConsoleSecretName = fmt.Sprintf(testenv.SecretObjectPodName, testenvInstance.GetName(), "monitoring-console", 3)
			verificationSecrets = []string{standaloneSecretName, licenseMasterSecretName, monitoringConsoleSecretName}

			// Verify that new HEC TOKEN is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that new Admin Password is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that new Pass4SymmKey is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Verify that new IdxcSecret is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["IdxcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.IdxcSecret))

			// Verify that new ShcSecret is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["ShcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.ShcSecret))

			// Verify that new HEC TOKEN is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that new Admin Password is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that new Pass4SymmKey is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Verify that new IdxcSecret is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["IdxcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.IdxcSecret))

			// Verify that new ShcSecret is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["ShcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.ShcSecret))

			// secret object for reference comparison
			secretObj = testenv.GetSecretObject(deployment, testenvInstance.GetName(), secretName)

			// delete secret by passing empty data in spec
			var data map[string][]byte
			err = testenv.ModifySecretObject(deployment, data, testenvInstance.GetName(), secretName)
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
			standaloneSecretName = fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "standalone", 4)
			licenseMasterSecretName = fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "license-master", 4)
			monitoringConsoleSecretName = fmt.Sprintf(testenv.SecretObjectPodName, testenvInstance.GetName(), "monitoring-console", 4)
			verificationSecrets = []string{standaloneSecretName, licenseMasterSecretName, monitoringConsoleSecretName}

			// Verify that new HEC TOKEN is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that new Admin Password is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that new Pass4SymmKey is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Verify that new IdxcSecret is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["IdxcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.IdxcSecret))

			// Verify that new ShcSecret is created
			testenv.VerifyNewSecretValueOnVersionedSecretObject(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["ShcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.ShcSecret))

			// Verify that new HEC TOKEN is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that new Admin Password is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that new Pass4SymmKey is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Verify that new IdxcSecret is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["IdxcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.IdxcSecret))

			// Verify that new ShcSecret is updated on pod
			testenv.VerifyNewVersionedSecretValueUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["ShcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.ShcSecret))

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("secret: secret update on indexers and search head cluster", func() {

			// Download License File
			licenseFilePath, err := testenv.DownloadFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			err = deployment.DeploySingleSiteCluster(deployment.GetName(), 3, true /*shc*/)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Get Current Secrets
			secretName := fmt.Sprintf(testenv.SecretObjectName, testenvInstance.GetName())
			secretObj := testenv.GetSecretObject(deployment, testenvInstance.GetName(), secretName)

			// Update Secret Values
			modifedKeyValue := "whatever"
			hecToken := testenv.DecodeBase64(secretObj.Data.HecToken)
			modifiedHecToken := hecToken[:len(hecToken)-2] + testenv.RandomDNSName(2)
			secretObj.Data.HecToken = testenv.EncodeBase64(modifiedHecToken)
			secretObj.Data.Password = testenv.EncodeBase64(modifedKeyValue)
			secretObj.Data.Pass4SymmKey = testenv.EncodeBase64(modifedKeyValue)
			secretObj.Data.IdxcSecret = testenv.EncodeBase64(modifedKeyValue)
			secretObj.Data.ShcSecret = testenv.EncodeBase64(modifedKeyValue)
			err = testenv.UpdateSecret(deployment, testenvInstance.GetName(), secretObj, false /*delete*/)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// Ensure that Cluster Master goes to update phase
			testenv.VerifyClusterMasterPhase(deployment, testenvInstance, splcommon.PhaseUpdating)

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Once Pods are READY check each versioned secret for updated secret keys
			clusterMasterSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "cluster-master", 2)
			indexerSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "idxc-indexer", 2)
			licenseMasterSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "license-master", 2)
			searchHeadDeployerSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "shc-deployer", 2)
			searchHeadSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "shc-search-head", 2)
			monitoringConsoleSecretName := fmt.Sprintf(testenv.SecretObjectPodName, testenvInstance.GetName(), "monitoring-console", 2)
			verificationSecrets := []string{clusterMasterSecretName, indexerSecretName, licenseMasterSecretName, searchHeadDeployerSecretName, searchHeadSecretName, monitoringConsoleSecretName}

			// Verify that HEC TOKEN is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that Admin Password is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that Pass4SymmKey is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Verify that IdxcPass4Symmkey is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["IdxcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.IdxcSecret))

			// Verify that ShcPass4Symmkey is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["ShcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.ShcSecret))

			// Once Pods are READY check each pod for updated secret keys
			clusterMasterPodName := fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())
			licenseMasterPodName := fmt.Sprintf(testenv.LicenseMasterPod, deployment.GetName(), 0)
			monitoringConsolePodName := fmt.Sprintf(testenv.MonitoringConsolePod, testenvInstance.GetName(), 0)
			indexerPodName0 := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
			indexerPodName1 := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 1)
			indexerPodName2 := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 2)
			SearchHeadPodName0 := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			SearchHeadPodName1 := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			SearchHeadPodName2 := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			verificationPods := []string{licenseMasterPodName, monitoringConsolePodName, clusterMasterPodName, indexerPodName1, indexerPodName2, indexerPodName0, SearchHeadPodName0, SearchHeadPodName1, SearchHeadPodName2}

			// Verify that HEC TOKEN is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that Admin Password is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that Pass4SymmKey is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Verify that IdxcPass4Symmkey is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["IdxcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.IdxcSecret))

			// Verify that ShcPass4Symmkey is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["ShcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.ShcSecret))

		})
	})

	Context("Multisite cluster deployment (M13 - Multisite indexer cluster, Search head cluster)", func() {
		It("secret: secret update on multisite indexers and search head cluster", func() {

			// Download License File
			licenseFilePath, err := testenv.DownloadFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			siteCount := 3
			err = deployment.DeployMultisiteClusterWithSearchHead(deployment.GetName(), 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Get Current Secrets
			secretName := fmt.Sprintf(testenv.SecretObjectName, testenvInstance.GetName())
			secretObj := testenv.GetSecretObject(deployment, testenvInstance.GetName(), secretName)

			// Update Secret Values
			modifedKeyValue := "whatever"
			hecToken := testenv.DecodeBase64(secretObj.Data.HecToken)
			modifiedHecToken := hecToken[:len(hecToken)-2] + testenv.RandomDNSName(2)
			secretObj.Data.HecToken = testenv.EncodeBase64(modifiedHecToken)
			secretObj.Data.Password = testenv.EncodeBase64(modifedKeyValue)
			secretObj.Data.Pass4SymmKey = testenv.EncodeBase64(modifedKeyValue)
			secretObj.Data.IdxcSecret = testenv.EncodeBase64(modifedKeyValue)
			secretObj.Data.ShcSecret = testenv.EncodeBase64(modifedKeyValue)
			err = testenv.UpdateSecret(deployment, testenvInstance.GetName(), secretObj, false /*delete*/)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// Ensure that Cluster Master goes to update phase
			testenv.VerifyClusterMasterPhase(deployment, testenvInstance, splcommon.PhaseUpdating)

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Once Pods are READY check each versioned secret for updated secret keys
			clusterMasterSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "cluster-master", 2)
			licenseMasterSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "license-master", 2)
			searchHeadDeployerSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "shc-deployer", 2)
			searchHeadSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "shc-search-head", 2)
			monitoringConsoleSecretName := fmt.Sprintf(testenv.SecretObjectPodName, testenvInstance.GetName(), "monitoring-console", 2)
			site1IndexerSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "site1-indexer", 2)
			site2IndexerSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "site2-indexer", 2)
			site3IndexerSecretName := fmt.Sprintf(testenv.SecretObjectPodName, deployment.GetName(), "site3-indexer", 2)
			verificationSecrets := []string{site1IndexerSecretName, site3IndexerSecretName, site2IndexerSecretName, clusterMasterSecretName, licenseMasterSecretName, searchHeadDeployerSecretName, searchHeadSecretName, monitoringConsoleSecretName}

			// Verify that HEC TOKEN is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that Admin Password is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that Pass4SymmKey is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Verify that IdxcPass4Symmkey is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["IdxcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.IdxcSecret))

			// Verify that ShcPass4Symmkey is updated
			testenv.VerifySecretObjectUpdated(deployment, testenvInstance, verificationSecrets, testenv.SecretObject["ShcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.ShcSecret))

			// Once Pods are READY check each versioned secret for updated secret keys
			clusterMasterPodName := fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())
			licenseMasterPodName := fmt.Sprintf(testenv.LicenseMasterPod, deployment.GetName(), 0)
			monitoringConsolePodName := fmt.Sprintf(testenv.MonitoringConsolePod, testenvInstance.GetName(), 0)
			SearchHeadPodName0 := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			SearchHeadPodName1 := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			SearchHeadPodName2 := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			Site1IndexerPodName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, 0)
			Site2IndexerPodName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 2, 0)
			Site3IndexerPodName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 3, 0)
			verificationPods := []string{licenseMasterPodName, monitoringConsolePodName, clusterMasterPodName, SearchHeadPodName0, SearchHeadPodName1, SearchHeadPodName2, Site1IndexerPodName, Site2IndexerPodName, Site3IndexerPodName}

			// Verify that HEC TOKEN is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["HecToken"], testenv.DecodeBase64(secretObj.Data.HecToken))

			// Verify that Admin Password is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["AdminPassword"], testenv.DecodeBase64(secretObj.Data.Password))

			// Verify that Pass4SymmKey is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["Pass4SymmKey"], testenv.DecodeBase64(secretObj.Data.Pass4SymmKey))

			// Verify that IdxcPass4Symmkey is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["IdxcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.IdxcSecret))

			// Verify that ShcPass4Symmkey is updated
			testenv.VerifySecretsUpdatedOnPod(deployment, testenvInstance, verificationPods, testenv.SecretObject["ShcPass4Symmkey"], testenv.DecodeBase64(secretObj.Data.ShcSecret))
		})
	})
})
