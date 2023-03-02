package smartstore

import (
	"context"
	"fmt"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo/v2"
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
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
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

			time.Sleep(1 * time.Minute)
			// Verify standalone goes to ready state
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Check index on pod
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			for indexName := range indexVolumeMap {
				testenv.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)
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
				testenv.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)
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
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Check index on pod
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)

			// Check special index configs
			testenv.VerifyIndexConfigsMatch(ctx, deployment, podName, indexName, specialConfig["MaxGlobalDataSizeMB"], specialConfig["MaxGlobalRawDataSizeMB"])

			// Ingest data to the index
			logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
			testenv.CreateMockLogfile(logFile, 2000)
			testenv.IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)

			// Roll Hot Buckets on the test index by restarting splunk
			testenv.RollHotToWarm(ctx, deployment, podName, indexName)

			// Check for indexes on S3
			testenv.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)

			// Verify Cachemanager Values
			serverConfPath := "/opt/splunk/etc/apps/splunk-operator/local/server.conf"

			// Validate MaxCacheSizeMB
			testenv.VerifyConfOnPod(deployment, testcaseEnvInst.GetName(), podName, serverConfPath, "max_cache_size", fmt.Sprint(cacheManagerSmartStoreSpec.MaxCacheSizeMB))

			// Validate EvictionPaddingSizeMB
			testenv.VerifyConfOnPod(deployment, testcaseEnvInst.GetName(), podName, serverConfPath, "eviction_padding", fmt.Sprint(cacheManagerSmartStoreSpec.EvictionPaddingSizeMB))

			// Validate MaxConcurrentDownloads
			testenv.VerifyConfOnPod(deployment, testcaseEnvInst.GetName(), podName, serverConfPath, "max_concurrent_downloads", fmt.Sprint(cacheManagerSmartStoreSpec.MaxConcurrentDownloads))

			// Validate MaxConcurrentUploads
			testenv.VerifyConfOnPod(deployment, testcaseEnvInst.GetName(), podName, serverConfPath, "max_concurrent_uploads", fmt.Sprint(cacheManagerSmartStoreSpec.MaxConcurrentUploads))

			// Validate EvictionPolicy
			testenv.VerifyConfOnPod(deployment, testcaseEnvInst.GetName(), podName, serverConfPath, "eviction_policy", cacheManagerSmartStoreSpec.EvictionPolicy)

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (M4)", func() {
		It("managersmartstore, integration: Can configure indexes and volumes on Multisite Indexer Cluster through app", func() {

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
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify MC Pod is Ready
			// testenv.MCPodReady(testcaseEnvInst.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Check index on pod
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testenv.VerifyIndexFoundOnPod(ctx, deployment, podName, indexName)
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
				testenv.VerifyIndexExistsOnS3(ctx, deployment, indexName, podName)
			}
		})
	})
})
