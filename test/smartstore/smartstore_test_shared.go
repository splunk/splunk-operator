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
package smartstore

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

// RunS1MultipleIndexesTest runs the standard S1 multiple indexes SmartStore test workflow
func RunS1MultipleIndexesTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, waitTimeout time.Duration) {
	volName := "test-volume-" + testenv.RandomDNSName(3)
	indexVolumeMap := map[string]string{
		"test-index-" + testenv.RandomDNSName(3): volName,
		"test-index-" + testenv.RandomDNSName(3): volName,
	}
	testcaseEnvInst.Log.Info("Index secret name ", "secret name ", testcaseEnvInst.GetIndexSecretName())

	var indexSpec []enterpriseApi.IndexSpec
	volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volName, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3", testenv.GetDefaultS3Region())}

	// Create index volume spec from index volume map
	for index, volume := range indexVolumeMap {
		indexSpec = append(indexSpec, testenv.GenerateIndexSpec(index, volume))
	}

	// Generate smartstore spec
	smartStoreSpec := enterpriseApi.SmartStoreSpec{
		VolList:   volumeSpec,
		IndexList: indexSpec,
	}

	// Deploy Standalone
	standalone, err := deployment.DeployStandaloneWithGivenSmartStoreSpec(ctx, deployment.GetName(), smartStoreSpec)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

	// Wait for Standalone to reach Ready phase
	err = testcaseEnvInst.WaitForStandalonePhase(ctx, deployment, testcaseEnvInst.GetName(), standalone.Name, enterpriseApi.PhaseReady, waitTimeout)
	Expect(err).To(Succeed(), "Timed out waiting for Standalone to reach Ready phase")

	// Verify standalone goes to ready state and stays ready
	Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed())

	// Check index on pod
	podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	for indexName := range indexVolumeMap {
		Expect(testcaseEnvInst.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)).To(Succeed())
	}

	// Ingest data to the index
	for indexName := range indexVolumeMap {
		logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
		testenv.CreateMockLogfile(logFile, 2000)
		testenv.IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)
	}

	// Roll Hot Buckets on the test index by restarting splunk and check for index on S3
	for indexName := range indexVolumeMap {
		testenv.RollHotToWarm(ctx, deployment, podName, indexName)
		Expect(testcaseEnvInst.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)).To(Succeed())
	}
}

// RunS1DefaultVolumesTest runs the standard S1 default volumes SmartStore test workflow
func RunS1DefaultVolumesTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv) {
	volName := "test-volume-" + testenv.RandomDNSName(3)
	indexName := "test-index-" + testenv.RandomDNSName(3)

	specialConfig := map[string]int{"MaxGlobalDataSizeMB": 100, "MaxGlobalRawDataSizeMB": 100}

	volSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volName, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3", testenv.GetDefaultS3Region())}

	indexSpec := []enterpriseApi.IndexSpec{{Name: indexName, RemotePath: indexName}}
	defaultSmartStoreSpec := enterpriseApi.IndexConfDefaultsSpec{IndexAndGlobalCommonSpec: enterpriseApi.IndexAndGlobalCommonSpec{VolName: volName, MaxGlobalDataSizeMB: uint(specialConfig["MaxGlobalDataSizeMB"]), MaxGlobalRawDataSizeMB: uint(specialConfig["MaxGlobalRawDataSizeMB"])}}
	cacheManagerSmartStoreSpec := enterpriseApi.CacheManagerSpec{MaxCacheSizeMB: 9900000, EvictionPaddingSizeMB: 1000, MaxConcurrentDownloads: 6, MaxConcurrentUploads: 6, EvictionPolicy: "lru"}

	smartStoreSpec := enterpriseApi.SmartStoreSpec{
		VolList:          volSpec,
		IndexList:        indexSpec,
		Defaults:         defaultSmartStoreSpec,
		CacheManagerConf: cacheManagerSmartStoreSpec,
	}

	// Deploy Standalone with given smartstore spec
	standalone, err := deployment.DeployStandaloneWithGivenSmartStoreSpec(ctx, deployment.GetName(), smartStoreSpec)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

	// Verify standalone goes to ready state
	Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed())

	// Check index on pod
	podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	Expect(testcaseEnvInst.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)).To(Succeed())

	// Check special index configs
	Expect(testcaseEnvInst.VerifyIndexConfigsMatch(ctx, deployment, podName, indexName, specialConfig["MaxGlobalDataSizeMB"], specialConfig["MaxGlobalRawDataSizeMB"])).To(Succeed())

	// Ingest data to the index
	logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
	testenv.CreateMockLogfile(logFile, 2000)
	testenv.IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)

	// Roll Hot Buckets on the test index by restarting splunk
	testenv.RollHotToWarm(ctx, deployment, podName, indexName)

	// Check for indexes on S3
	Expect(testcaseEnvInst.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)).To(Succeed())

	// Verify Cachemanager Values
	serverConfPath := "/opt/splunk/etc/apps/splunk-operator/local/server.conf"

	// Validate MaxCacheSizeMB
	Expect(testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "max_cache_size", fmt.Sprint(cacheManagerSmartStoreSpec.MaxCacheSizeMB))).To(Succeed())

	// Validate EvictionPaddingSizeMB
	Expect(testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "eviction_padding", fmt.Sprint(cacheManagerSmartStoreSpec.EvictionPaddingSizeMB))).To(Succeed())

	// Validate MaxConcurrentDownloads
	Expect(testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "max_concurrent_downloads", fmt.Sprint(cacheManagerSmartStoreSpec.MaxConcurrentDownloads))).To(Succeed())

	// Validate MaxConcurrentUploads
	Expect(testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "max_concurrent_uploads", fmt.Sprint(cacheManagerSmartStoreSpec.MaxConcurrentUploads))).To(Succeed())

	// Validate EvictionPolicy
	Expect(testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "eviction_policy", cacheManagerSmartStoreSpec.EvictionPolicy)).To(Succeed())
}

// RunS1EphemeralStorageTest deploys a Standalone with one ephemeral storage volume configured and verifies it is ready.
// Pass etcStorage=true to set EtcVolumeStorageConfig, false to set VarVolumeStorageConfig.
func RunS1EphemeralStorageTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, storageConfig enterpriseApi.StorageClassSpec, etcStorage bool) {
	spec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				ImagePullPolicy: "Always",
				Image:           testcaseEnvInst.GetSplunkImage(),
			},
			Volumes: []corev1.Volume{},
		},
	}
	if etcStorage {
		spec.CommonSplunkSpec.EtcVolumeStorageConfig = storageConfig
	} else {
		spec.CommonSplunkSpec.VarVolumeStorageConfig = storageConfig
	}

	standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

	Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed())
}

// RunM4MultisiteSmartStoreTest runs the standard M4 multisite SmartStore test workflow
func RunM4MultisiteSmartStoreTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig) {
	volName := "test-volume-" + testenv.RandomDNSName(3)
	indexName := "test-index-" + testenv.RandomDNSName(3)

	volSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volName, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3", testenv.GetDefaultS3Region())}
	indexSpec := []enterpriseApi.IndexSpec{testenv.GenerateIndexSpec(indexName, volName)}
	smartStoreSpec := enterpriseApi.SmartStoreSpec{
		VolList:   volSpec,
		IndexList: indexSpec,
	}

	siteCount := 3
	err := config.DeployMultisiteClusterWithIndexes(ctx, deployment, deployment.GetName(), 1, siteCount, testcaseEnvInst.GetIndexSecretName(), smartStoreSpec)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	Expect(testenv.VerifyM4ClusterAndRFSF(ctx, deployment, testcaseEnvInst, config, siteCount)).To(Succeed())

	// Use multisite workflow helper to verify index, ingest data, roll to warm, and verify on S3
	Expect(testcaseEnvInst.MultisiteIndexerWorkflow(ctx, deployment, deployment.GetName(), siteCount, indexName)).To(Succeed())

	// Get old bundle hash before adding new index
	oldBundleHash := config.GetBundleHash(ctx, deployment)

	testcaseEnvInst.Log.Info("Adding new index to Cluster Manager CR")
	indexNameTwo := "test-index-" + testenv.RandomDNSName(3)
	indexList := []string{indexName, indexNameTwo}
	newIndex := []enterpriseApi.IndexSpec{testenv.GenerateIndexSpec(indexNameTwo, volName)}

	// Update CR with new index based on API version
	Expect(config.AppendSmartStoreIndex(ctx, deployment, newIndex)).To(Succeed(), "Unable to append SmartStore index")

	Expect(testenv.VerifyM4ClusterAndRFSF(ctx, deployment, testcaseEnvInst, config, siteCount)).To(Succeed())

	// Verify new bundle is pushed (only for v3)
	if config.GetAPIVersion() == "v3" {
		Expect(testcaseEnvInst.VerifyClusterManagerBundlePush(ctx, deployment, testcaseEnvInst.GetName(), 1, oldBundleHash)).To(Succeed())
	}

	// Verify both indexes on all sites
	for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
		podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
		for _, index := range indexList {
			Expect(testcaseEnvInst.VerifyIndexFoundOnPod(ctx, deployment, podName, index)).To(Succeed())
		}
	}

	// Use multisite workflow helper for the new index
	testcaseEnvInst.Log.Info("Ingesting data on index", "Index Name", indexNameTwo)
	Expect(testcaseEnvInst.MultisiteIndexerWorkflow(ctx, deployment, deployment.GetName(), siteCount, indexNameTwo)).To(Succeed())
}
