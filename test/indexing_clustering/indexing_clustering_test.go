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
package indexingclusteringtest

import (
	"encoding/json"
	"fmt"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Indexing and Clustering Test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	c3Config := testenv.NewClusterReadinessConfigV4()

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	// -------------------------------------------------------------------------
	// C3: Single-site cluster - basic RF/SF and indexing validation
	// -------------------------------------------------------------------------

	Context("Single-site cluster deployment (C3)", func() {
		It("cluster health endpoint reports all peers up and data searchable", Label("tier:e2e-full", "sva:c3", "cloud:aws", "variant:manager", "feature:idxclustering"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

			err := c3Config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, 3, false /*no shc*/)
			Expect(err).To(Succeed(), "Unable to deploy C3 cluster")
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Verify cluster health via cluster manager REST endpoint
			Eventually(func() bool {
				health, err := testenv.GetClusterManagerHealth(ctx, deployment)
				if err != nil {
					testcaseEnvInst.Log.Error(err, "Failed to query cluster health")
					return false
				}

				testcaseEnvInst.Log.Info("Cluster health", "allPeersUp", health.AllPeersAreUp, "allDataSearchable", health.AllDataIsSearchable,
					"rfMet", health.ReplicationFactorMet, "sfMet", health.SearchFactorMet)
				return health.AllPeersAreUp == "1" && health.AllDataIsSearchable == "1" &&
					health.ReplicationFactorMet == "1" && health.SearchFactorMet == "1"
			}, deployment.GetTimeout(), PollInterval).Should(BeTrue(), "Cluster health check failed")
		})
	})

	Context("Single-site cluster deployment (C3)", func() {
		It("all indexer peers are registered on cluster manager", Label("tier:e2e-pr", "sva:c3", "cloud:aws", "variant:manager", "feature:idxclustering"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

			indexerCount := 3
			err := c3Config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, indexerCount, false /*no shc*/)
			Expect(err).To(Succeed(), "Unable to deploy C3 cluster")

			// Verify each indexer pod is registered as a peer on the cluster manager
			Eventually(func() int {
				peersResp := testenv.GetIndexersOrSearchHeadsOnCM(ctx, deployment, "peer")
				activePeers := 0
				for _, entry := range peersResp.Entry {
					testcaseEnvInst.Log.Info("Peer status", "label", entry.Content.Label, "status", entry.Content.Status)
					if entry.Content.Status == "Up" {
						activePeers++
					}
				}
				return activePeers
			}, deployment.GetTimeout(), PollInterval).Should(Equal(indexerCount),
				"Expected %d indexer peers in Up state on cluster manager", indexerCount)
		})
	})

	// -------------------------------------------------------------------------
	// Indexing: ingest data into clustered indexers and verify replication
	// NOTE: Requires SHC (7+ pods). Disabled for single-node kind.
	// -------------------------------------------------------------------------

	Context("Single-site cluster deployment (C3)", func() {
		It("can ingest data to custom index and verify event count across cluster", Label("tier:e2e-full", "sva:c3", "cloud:aws", "variant:manager", "feature:idxclustering"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

			err := c3Config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, 3, true /*shc*/)
			Expect(err).To(Succeed(), "Unable to deploy C3 cluster")
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			indexName := "testidxcluster"
			err = testenv.CreateIndexOnClusterManager(ctx, deployment, indexName)
			Expect(err).To(Succeed(), "Failed to create clustered index %s", indexName)

			indexerPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
			Eventually(func() bool {
				indexFound, _ := testenv.GetIndexOnPod(ctx, deployment, indexerPodName, indexName)
				testcaseEnvInst.Log.Info("Checking clustered index on indexer", "pod", indexerPodName, "index", indexName, "found", indexFound)
				return indexFound
			}, deployment.GetTimeout(), PollInterval).Should(BeTrue(),
				"Expected index %s to be distributed to indexer %s", indexName, indexerPodName)

			logFile := "/tmp/cluster-ingest-test.log"
			err = testenv.CreateMockLogfile(logFile, 100)
			Expect(err).To(Succeed(), "Failed to create mock logfile")

			err = testenv.IngestFileViaOneshot(ctx, deployment, logFile, indexName, indexerPodName)
			Expect(err).To(Succeed(), "Failed to ingest logfile on pod %s", indexerPodName)

			rollHotToWarmOk := testenv.RollHotToWarm(ctx, deployment, indexerPodName, indexName)
			Expect(rollHotToWarmOk).To(BeTrue(), "Failed to roll hot buckets for index %s", indexName)

			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			shPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			searchString := fmt.Sprintf("index=%s | stats count", indexName)

			Eventually(func() int {
				count, searchErr := testenv.CountSearchResults(ctx, deployment, shPodName, searchString)
				if searchErr != nil {
					testcaseEnvInst.Log.Error(searchErr, "Search failed", "pod", shPodName)
					return 0
				}
				testcaseEnvInst.Log.Info("Event count in clustered index", "index", indexName, "count", count)
				return count
			}, deployment.GetTimeout(), PollInterval).Should(BeNumerically(">", 0),
				"Expected events in index %s to be searchable from search head", indexName)
		})
	})

	// -------------------------------------------------------------------------
	// Rolling restart: verify cluster remains healthy during pod cycling
	// -------------------------------------------------------------------------

	Context("Single-site cluster deployment (C3)", func() {
		It("cluster remains healthy during rolling restart", Label("tier:e2e-full", "sva:c3", "cloud:aws", "variant:manager", "feature:idxclustering"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

			err := c3Config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, 3, false /*no shc*/)
			Expect(err).To(Succeed(), "Unable to deploy C3 cluster")
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			cmPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())

			// Initiate a rolling restart via the cluster manager
			rollingRestartInitiated := testenv.StartClusterPeerRollingRestart(ctx, deployment)
			Expect(rollingRestartInitiated).To(BeTrue(), "Failed to initiate rolling restart")

			// Wait for the rolling restart flag to appear before waiting for completion.
			Eventually(func() bool {
				inProgress := testenv.CheckRollingRestartStatus(ctx, deployment)
				testcaseEnvInst.Log.Info("Rolling restart in progress", "status", inProgress, "pod", cmPodName)
				return inProgress
			}, deployment.GetTimeout(), PollInterval).Should(BeTrue(), "Rolling restart did not start")

			// Wait for the rolling restart flag to be cleared (restart complete)
			Eventually(func() bool {
				inProgress := testenv.CheckRollingRestartStatus(ctx, deployment)
				testcaseEnvInst.Log.Info("Rolling restart in progress", "status", inProgress, "pod", cmPodName)
				return inProgress
			}, deployment.GetTimeout(), PollInterval).Should(BeFalse(), "Rolling restart did not complete")

			// After rolling restart, cluster should still meet RF/SF
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// All peers should be back Up
			Eventually(func() bool {
				return testenv.CheckRFSF(ctx, deployment)
			}, deployment.GetTimeout(), PollInterval).Should(BeTrue(), "RF/SF not met after rolling restart")
		})
	})

	// -------------------------------------------------------------------------
	// Bundle push: verify config changes propagate to all indexer peers
	// -------------------------------------------------------------------------

	Context("Single-site cluster deployment (C3)", func() {
		It("bundle hash is consistent across all cluster peers after push", Label("tier:e2e-full", "sva:c3", "cloud:aws", "variant:manager", "feature:idxclustering"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

			err := c3Config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, 3, false /*no shc*/)
			Expect(err).To(Succeed(), "Unable to deploy C3 cluster")
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Record the initial bundle hash
			initialHash := testenv.GetClusterManagerBundleHash(ctx, deployment, "ClusterManager")
			Expect(initialHash).ToNot(BeEmpty(), "Initial bundle hash should not be empty")
			testcaseEnvInst.Log.Info("Initial bundle hash", "hash", initialHash)

			// Verify all peers are Up and have received the active bundle
			// (matching bundle ID), not just reachable with a stale bundle.
			Eventually(func() bool {
				bundleStatus := testenv.CMBundlePushstatusWithBundleID(ctx, deployment, "peer")
				for label, peer := range bundleStatus {
					testcaseEnvInst.Log.Info("Peer bundle status", "peer", label, "status", peer.Status, "bundleID", peer.BundleID)
					if !strings.Contains(peer.Status, "Up") {
						return false
					}
					if peer.BundleID != initialHash {
						return false
					}
				}
				return len(bundleStatus) == 3
			}, deployment.GetTimeout(), PollInterval).Should(BeTrue(),
				"Not all 3 peers are Up with the active bundle")
		})
	})

	// -------------------------------------------------------------------------
	// M4: Multisite cluster - sites, RF/SF per site
	// NOTE: Requires 7+ pods (CM + 3 sites x 1 indexer + deployer + 3 SH).
	// Disabled for single-node kind; enable on EKS/GKE with 4+ nodes.
	// -------------------------------------------------------------------------

	Context("Multisite cluster deployment (M4)", func() {
		It("cluster manager reports correct site topology", Label("tier:e2e-full", "sva:m4", "cloud:aws", "variant:manager", "feature:idxclustering"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

			siteCount := 3
			_, err := testcaseEnvInst.RunM4DeploymentWorkflow(ctx, deployment, 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy M4 cluster")

			Expect(testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)).To(Succeed(), "IndexerCluster multisite status not ready")

			cmPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())

			Eventually(func() int {
				stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/manager/sites?output_mode=json"
				stdout, _, err := deployment.PodExecCommand(ctx, cmPodName, []string{"/bin/sh"}, stdin, false)
				if err != nil {
					testcaseEnvInst.Log.Error(err, "Failed to query sites endpoint")
					return 0
				}
				sitesResp := testenv.ClusterManagerSitesResponse{}
				if jsonErr := json.Unmarshal([]byte(stdout), &sitesResp); jsonErr != nil {
					testcaseEnvInst.Log.Error(jsonErr, "Failed to parse sites response")
					return 0
				}
				testcaseEnvInst.Log.Info("Sites reported by cluster manager", "count", len(sitesResp.Entries))
				return len(sitesResp.Entries)
			}, deployment.GetTimeout(), PollInterval).Should(Equal(siteCount),
				"Expected %d sites to be visible on cluster manager", siteCount)
		})
	})

	Context("Multisite cluster deployment (M4)", func() {
		It("site replication and search factors are met", Label("tier:e2e-full", "sva:m4", "cloud:aws", "variant:manager", "feature:idxclustering"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

			siteCount := 3
			_, err := testcaseEnvInst.RunM4DeploymentWorkflow(ctx, deployment, 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy M4 cluster")

			Eventually(func() bool {
				health, err := testenv.GetClusterManagerHealth(ctx, deployment)
				if err != nil {
					testcaseEnvInst.Log.Error(err, "Failed to query cluster health")
					return false
				}
				testcaseEnvInst.Log.Info("Multisite cluster health",
					"multisite", health.Multisite,
					"siteRFMet", health.SiteReplicationFactorMet,
					"siteSFMet", health.SiteSearchFactorMet,
					"rfMet", health.ReplicationFactorMet,
					"sfMet", health.SearchFactorMet,
				)
				return health.Multisite == "1" &&
					health.SiteReplicationFactorMet == "1" &&
					health.SiteSearchFactorMet == "1" &&
					health.ReplicationFactorMet == "1" &&
					health.SearchFactorMet == "1"
			}, deployment.GetTimeout(), PollInterval).Should(BeTrue(),
				"Multisite cluster health check failed: site RF/SF not met")
		})
	})

	// -------------------------------------------------------------------------
	// Indexer scaling: scale up IndexerCluster replicas
	// -------------------------------------------------------------------------

	Context("Single-site cluster deployment (C3)", func() {
		It("indexer cluster can scale up and remains healthy", Label("tier:e2e-full", "sva:c3", "cloud:aws", "variant:manager", "feature:idxclustering", "feature:scaling"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

			initialReplicas := 3
			err := c3Config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, initialReplicas, false /*no shc*/)
			Expect(err).To(Succeed(), "Unable to deploy C3 cluster")
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Scale the IndexerCluster from 3 to 4 replicas
			scaledReplicas := 4
			idc := &enterpriseApi.IndexerCluster{}
			idcName := fmt.Sprintf("%s-idxc", deployment.GetName())
			err = deployment.GetInstance(ctx, idcName, idc)
			Expect(err).To(Succeed(), "Failed to get IndexerCluster CR %s", idcName)

			idc.Spec.Replicas = int32(scaledReplicas)
			err = deployment.UpdateCR(ctx, idc)
			Expect(err).To(Succeed(), "Failed to update IndexerCluster replicas to %d", scaledReplicas)
			testcaseEnvInst.Log.Info("Scaled IndexerCluster", "name", idcName, "replicas", scaledReplicas)

			// Wait for the new indexer pod to come up and rejoin the cluster
			newPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), scaledReplicas-1)
			Eventually(func() bool {
				return testenv.CheckIndexerOnCM(ctx, deployment, newPodName)
			}, deployment.GetTimeout(), PollInterval).Should(BeTrue(),
				"New indexer peer %s did not register on cluster manager", newPodName)

			// After scale-up, cluster should still meet RF/SF
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Verify total peers count
			Eventually(func() int {
				peersResp := testenv.GetIndexersOrSearchHeadsOnCM(ctx, deployment, "peer")
				upCount := 0
				for _, entry := range peersResp.Entry {
					if entry.Content.Status == "Up" {
						upCount++
					}
				}
				return upCount
			}, deployment.GetTimeout(), PollInterval).Should(Equal(scaledReplicas),
				"Expected %d Up peers after scale-up, got fewer", scaledReplicas)
		})
	})

})
