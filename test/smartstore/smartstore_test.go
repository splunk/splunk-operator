package smartstore

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	"github.com/splunk/splunk-operator/test/testenv"
)

func dumpGetPods(ns string) {
	output, _ := exec.Command("kubectl", "get", "pod", "-n", ns).Output()
	for _, line := range strings.Split(string(output), "\n") {
		testenvInstance.Log.Info(line)
	}
}

var _ = Describe("Smoke test", func() {

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

	Context("Standalone Deployment (S1)", func() {
		It("smartstore, integration: Can configure multiple indexes through app", func() {
			volName := "test-volume-" + testenv.RandomDNSName(3)
			indexVolumeMap := map[string]string{"test-index-" + testenv.RandomDNSName(3): volName,
				"test-index-" + testenv.RandomDNSName(3): volName,
			}
			testenvInstance.Log.Info("Index secret name ", "secret name ", testenvInstance.GetIndexSecretName())

			var indexSpec []enterprisev1.IndexSpec
			volumeSpec := []enterprisev1.VolumeSpec{testenv.GenerateIndexVolumeSpec(volName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// Create index volume spec from index volume map
			for index, volume := range indexVolumeMap {
				indexSpec = append(indexSpec, testenv.GenerateIndexSpec(index, volume))
			}

			// Generate smartstore spec
			smartStoreSpec := enterprisev1.SmartStoreSpec{
				VolList:   volumeSpec,
				IndexList: indexSpec,
			}

			// Deploy Standalone
			standalone, err := deployment.DeployStandaloneWithGivenSmartStoreSpec(deployment.GetName(), smartStoreSpec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Check index on pod
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			for indexName := range indexVolumeMap {
				testenv.VerifyIndexFoundOnPod(deployment, podName, indexName)
			}

			// Ingest data to the index
			for indexName := range indexVolumeMap {
				logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
				testenv.CreateMockLogfile(logFile, 2000)
				testenv.IngestFileViaMonitor(logFile, indexName, podName, deployment)
			}

			// Roll Hot Buckets on the test index by restarting splunk and check for index on S3
			for indexName := range indexVolumeMap {
				testenv.RollHotToWarm(deployment, podName, indexName)
				testenv.VerifyIndexExistsOnS3(deployment, indexName, podName)
			}
		})
	})

	Context("Standalone Deployment (S1)", func() {
		It("smartstore, integration: Can configure indexes which use default volumes through app", func() {
			volName := "test-volume-" + testenv.RandomDNSName(3)
			indexName := "test-index-" + testenv.RandomDNSName(3)

			specialConfig := map[string]int{"MaxGlobalDataSizeMB": 100, "MaxGlobalRawDataSizeMB": 100}

			volSpec := []enterprisev1.VolumeSpec{testenv.GenerateIndexVolumeSpec(volName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			indexSpec := []enterprisev1.IndexSpec{{Name: indexName, RemotePath: indexName}}
			defaultSmartStoreSpec := enterprisev1.IndexConfDefaultsSpec{IndexAndGlobalCommonSpec: enterprisev1.IndexAndGlobalCommonSpec{VolName: volName, MaxGlobalDataSizeMB: uint(specialConfig["MaxGlobalDataSizeMB"]), MaxGlobalRawDataSizeMB: uint(specialConfig["MaxGlobalRawDataSizeMB"])}}
			cacheManagerSmartStoreSpec := enterprisev1.CacheManagerSpec{MaxCacheSizeMB: 9900000, EvictionPaddingSizeMB: 1000, MaxConcurrentDownloads: 6, MaxConcurrentUploads: 6, EvictionPolicy: "lru"}

			smartStoreSpec := enterprisev1.SmartStoreSpec{
				VolList:          volSpec,
				IndexList:        indexSpec,
				Defaults:         defaultSmartStoreSpec,
				CacheManagerConf: cacheManagerSmartStoreSpec,
			}

			// Deploy Standalone with given smartstore spec
			standalone, err := deployment.DeployStandaloneWithGivenSmartStoreSpec(deployment.GetName(), smartStoreSpec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Check index on pod
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyIndexFoundOnPod(deployment, podName, indexName)

			// Check special index configs
			testenv.VerifyIndexConfigsMatch(deployment, podName, indexName, specialConfig["MaxGlobalDataSizeMB"], specialConfig["MaxGlobalRawDataSizeMB"])

			// Ingest data to the index
			logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
			testenv.CreateMockLogfile(logFile, 2000)
			testenv.IngestFileViaMonitor(logFile, indexName, podName, deployment)

			// Roll Hot Buckets on the test index by restarting splunk
			testenv.RollHotToWarm(deployment, podName, indexName)

			// Check for indexes on S3
			testenv.VerifyIndexExistsOnS3(deployment, indexName, podName)

			// Verify Cachemanager Values
			serverConfPath := "/opt/splunk/etc/apps/splunk-operator/local/server.conf"

			// Validate MaxCacheSizeMB
			testenv.VerifyConfOnPod(deployment, testenvInstance.GetName(), podName, serverConfPath, "max_cache_size", fmt.Sprint(cacheManagerSmartStoreSpec.MaxCacheSizeMB))

			// Validate EvictionPaddingSizeMB
			testenv.VerifyConfOnPod(deployment, testenvInstance.GetName(), podName, serverConfPath, "eviction_padding", fmt.Sprint(cacheManagerSmartStoreSpec.EvictionPaddingSizeMB))

			// Validate MaxConcurrentDownloads
			testenv.VerifyConfOnPod(deployment, testenvInstance.GetName(), podName, serverConfPath, "max_concurrent_downloads", fmt.Sprint(cacheManagerSmartStoreSpec.MaxConcurrentDownloads))

			// Validate MaxConcurrentUploads
			testenv.VerifyConfOnPod(deployment, testenvInstance.GetName(), podName, serverConfPath, "max_concurrent_uploads", fmt.Sprint(cacheManagerSmartStoreSpec.MaxConcurrentUploads))

			// Validate EvictionPolicy
			testenv.VerifyConfOnPod(deployment, testenvInstance.GetName(), podName, serverConfPath, "eviction_policy", cacheManagerSmartStoreSpec.EvictionPolicy)

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (M4)", func() {
		It("smartstore, integration: Can configure indexes and volumes on Multisite Indexer Cluster through app", func() {

			volName := "test-volume-" + testenv.RandomDNSName(3)
			indexName := "test-index-" + testenv.RandomDNSName(3)

			volSpec := []enterprisev1.VolumeSpec{testenv.GenerateIndexVolumeSpec(volName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			indexSpec := []enterprisev1.IndexSpec{testenv.GenerateIndexSpec(indexName, volName)}
			smartStoreSpec := enterprisev1.SmartStoreSpec{
				VolList:   volSpec,
				IndexList: indexSpec,
			}

			siteCount := 3
			err := deployment.DeployMultisiteClusterWithSearchHeadAndIndexes(deployment.GetName(), 1, siteCount, testenvInstance.GetIndexSecretName(), smartStoreSpec)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Check index on pod
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testenv.VerifyIndexFoundOnPod(deployment, podName, indexName)
			}

			// Ingest data to the index
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
				testenv.CreateMockLogfile(logFile, 2000)
				testenv.IngestFileViaMonitor(logFile, indexName, podName, deployment)
			}

			// Roll Hot Buckets on the test index per indexer
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testenv.RollHotToWarm(deployment, podName, indexName)
			}

			// Roll index buckets and Check for indexes on S3
			for siteNumber := 1; siteNumber <= siteCount; siteNumber++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), siteNumber, 0)
				testenv.VerifyIndexExistsOnS3(deployment, indexName, podName)
			}
		})
	})
})
