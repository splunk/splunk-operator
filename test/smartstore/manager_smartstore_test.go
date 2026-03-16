package smartstore

import (
	"context"
	"fmt"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Smartstore test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	ctx := context.TODO()
	var deployment *testenv.Deployment

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")

		// Validate test prerequisites early to fail fast
		err = testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if types.SpecState(CurrentSpecReport().State) == types.SpecStateFailed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Standalone Deployment (S1)", func() {
		It("managersmartstore, integration: Can configure multiple indexes through app", func() {
			volName := "test-volume-" + testenv.RandomDNSName(3)
			indexVolumeMap := map[string]string{"test-index-" + testenv.RandomDNSName(3): volName,
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
			err = testcaseEnvInst.WaitForStandalonePhase(ctx, deployment, testcaseEnvInst.GetName(), standalone.Name, enterpriseApi.PhaseReady, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for Standalone to reach Ready phase")

			// Verify standalone goes to ready state and stays ready
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

			// Check index on pod
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			for indexName := range indexVolumeMap {
				testcaseEnvInst.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)
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
				testcaseEnvInst.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)
			}
		})
	})

	Context("Standalone Deployment (S1)", func() {
		It("managersmartstore, integration: Can configure indexes which use default volumes through app", func() {
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
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

			// Check index on pod
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testcaseEnvInst.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)

			// Check special index configs
			testcaseEnvInst.VerifyIndexConfigsMatch(ctx, deployment, podName, indexName, specialConfig["MaxGlobalDataSizeMB"], specialConfig["MaxGlobalRawDataSizeMB"])

			// Ingest data to the index
			logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
			testenv.CreateMockLogfile(logFile, 2000)
			testenv.IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)

			// Roll Hot Buckets on the test index by restarting splunk
			testenv.RollHotToWarm(ctx, deployment, podName, indexName)

			// Check for indexes on S3
			testcaseEnvInst.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)

			// Verify Cachemanager Values
			serverConfPath := "/opt/splunk/etc/apps/splunk-operator/local/server.conf"

			// Validate MaxCacheSizeMB
			testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "max_cache_size", fmt.Sprint(cacheManagerSmartStoreSpec.MaxCacheSizeMB))

			// Validate EvictionPaddingSizeMB
			testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "eviction_padding", fmt.Sprint(cacheManagerSmartStoreSpec.EvictionPaddingSizeMB))

			// Validate MaxConcurrentDownloads
			testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "max_concurrent_downloads", fmt.Sprint(cacheManagerSmartStoreSpec.MaxConcurrentDownloads))

			// Validate MaxConcurrentUploads
			testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "max_concurrent_uploads", fmt.Sprint(cacheManagerSmartStoreSpec.MaxConcurrentUploads))

			// Validate EvictionPolicy
			testcaseEnvInst.VerifyConfOnPod(deployment, podName, serverConfPath, "eviction_policy", cacheManagerSmartStoreSpec.EvictionPolicy)

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (M4)", func() {
		It("managersmartstore, smoke: Can configure indexes and volumes on Multisite Indexer Cluster through app", func() {

			volName := "test-volume-" + testenv.RandomDNSName(3)
			indexName := "test-index-" + testenv.RandomDNSName(3)

			volSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volName, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3", testenv.GetDefaultS3Region())}
			indexSpec := []enterpriseApi.IndexSpec{testenv.GenerateIndexSpec(indexName, volName)}
			smartStoreSpec := enterpriseApi.SmartStoreSpec{
				VolList:   volSpec,
				IndexList: indexSpec,
			}

			siteCount := 3
			err := deployment.DeployMultisiteClusterWithSearchHeadAndIndexes(ctx, deployment.GetName(), 1, siteCount, testcaseEnvInst.GetIndexSecretName(), smartStoreSpec)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Ensure the indexers of all sites go to Ready phase
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Ensure cluster configured as multisite
			testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

			// Ensure search head cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Verify RF SF is met
			testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

			// Check index on pod
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testcaseEnvInst.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)
			}

			// Ingest data to the index
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
				testenv.CreateMockLogfile(logFile, 2000)
				testenv.IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)
			}

			// Roll Hot Buckets on the test index per indexer
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testenv.RollHotToWarm(ctx, deployment, podName, indexName)
			}

			// Roll index buckets and Check for indexes on S3
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testcaseEnvInst.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)
			}

			testcaseEnvInst.Log.Info("Adding new index to Cluster Manager CR")
			indexNameTwo := "test-index-two" + testenv.RandomDNSName(3)
			indexList := []string{indexName, indexNameTwo}
			newIndex := []enterpriseApi.IndexSpec{testenv.GenerateIndexSpec(indexNameTwo, volName)}

			cm := &enterpriseApi.ClusterManager{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to get instance of Cluster Master")
			cm.Spec.SmartStore.IndexList = append(cm.Spec.SmartStore.IndexList, newIndex...)
			err = deployment.UpdateCR(ctx, cm)
			Expect(err).To(Succeed(), "Failed to add new index to cluster master")

			// Ensure that the cluster-master goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Ensure the indexers of all sites go to Ready phase
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Ensure search head cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Verify RF SF is met
			testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

			// Check index on pod
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				for _, index := range indexList {
					testcaseEnvInst.VerifyIndexFoundOnPod(ctx, deployment, podName, index)
				}
			}

			// Ingest data to the index
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
				testenv.CreateMockLogfile(logFile, 2000)
				testenvInstance.Log.Info("Ingesting data on index", "Index Name", indexNameTwo)
				testenv.IngestFileViaMonitor(ctx, logFile, indexNameTwo, podName, deployment)
			}

			// Roll Hot Buckets on the test index per indexer
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testenv.RollHotToWarm(ctx, deployment, podName, indexNameTwo)
			}

			// Roll index buckets and Check for indexes on S3
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testenvInstance.Log.Info("Checking index on S3", "Index Name", indexNameTwo, "Pod Name", podName)
				testcaseEnvInst.VerifyIndexExistsOnS3(ctx, deployment, indexNameTwo, podName)
			}
		})
	})
})
