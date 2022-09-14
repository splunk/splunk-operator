// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
package scalingtest

import (
	"context"
	"encoding/json"
	"fmt"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Scaling test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", "master"+testenvInstance.GetName(), testenv.RandomDNSName(3))
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
		if !testcaseEnvInst.SkipTeardown {
			testenv.DeleteMCPod(testcaseEnvInst.GetName())
		}
		if deployment != nil {
			deployment.Teardown()
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Standalone deployment (S1)", func() {
		It("masterscaling, integration: Can Scale Up and Scale Down Standalone CR", func() {

			standalone, err := deployment.DeployStandalone(ctx, deployment.GetName(), "", "")
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Scale Standalone instance
			testcaseEnvInst.Log.Info("Scaling Up Standalone CR")
			scaledReplicaCount := 2
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(ctx, deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale up Standalone")

			// Ensure standalone is scaling up
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseReady)

			// Scale Down Standalone
			testcaseEnvInst.Log.Info("Scaling Down Standalone CR")
			scaledReplicaCount = scaledReplicaCount - 1
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(ctx, deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale down Standalone")

			// Ensure standalone is scaling down
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseScalingDown)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("masterscaling, integration: SHC and IDXC can be scaled up and data is searchable", func() {

			defaultSHReplicas := 3
			defaultIndexerReplicas := 3
			err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), defaultIndexerReplicas, true, "")
			Expect(err).To(Succeed(), "Unable to deploy search head cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Scale Search Head Cluster
			scaledSHReplicas := defaultSHReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)
			shcName := deployment.GetName() + "-shc"

			// Get instance of current SHC CR with latest config
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(ctx, shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Update Replicas of SHC
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to scale Search Head Cluster")

			// Ensure Search Head cluster scales up and go to ScalingUp phase
			testenv.VerifySearchHeadClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingUp)

			// Scale indexers
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)
			idxcName := deployment.GetName() + "-idxc"

			// Get instance of current Indexer CR with latest config
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to scale Indxer Cluster")

			// Ensure Indexer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingUp, idxcName)

			// Ensure Indexer cluster go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify New Indexer On Cluster Master
			indexerName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas-1)
			testcaseEnvInst.Log.Info("Checking for Indexer On CM", "Indexer Name", indexerName)
			Expect(testenv.CheckIndexerOnCM(ctx, deployment, indexerName)).To(Equal(true))

			// Ingest data on Indexers
			for i := 0; i < scaledIndexerReplicas; i++ {
				podName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
				testenv.CreateMockLogfile(logFile, 2000)
				testenv.IngestFileViaMonitor(ctx, logFile, "main", podName, deployment)
			}

			// Ensure Search Head Cluster go to Ready Phase
			// Adding this check in the end as SHC take the longest time to scale up due recycle of SHC members
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			time.Sleep(60 * time.Second)

			// Verify New SearchHead is added to Cluster Master
			searchHeadName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), scaledSHReplicas-1)
			testcaseEnvInst.Log.Info("Checking for Search Head On CM", "Search Head Name", searchHeadName)
			Expect(testenv.CheckSearchHeadOnCM(ctx, deployment, searchHeadName)).To(Equal(true))

			// Wait for RF SF is Met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Search for data on newly added indexer
			searchPod := searchHeadName
			searchString := fmt.Sprintf("index=%s host=%s | stats count by host", "main", indexerName)
			searchResultsResp, err := testenv.PerformSearchSync(ctx, searchPod, searchString, deployment)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchPod, searchString)

			// Verify result
			searchResponse := strings.Split(searchResultsResp, "\n")[0]
			var searchResults map[string]interface{}
			jsonErr := json.Unmarshal([]byte(searchResponse), &searchResults)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			testcaseEnvInst.Log.Info("Search results :", "searchResults", searchResults["result"])
			Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, searchPod)

			resultLine := searchResults["result"].(map[string]interface{})
			testcaseEnvInst.Log.Info("Sync Search results host count:", "count", resultLine["count"].(string), "host", resultLine["host"].(string))
			testHostname := strings.Compare(resultLine["host"].(string), indexerName)
			Expect(testHostname).To(Equal(0), "Incorrect search result hostname. Expect: %s Got: %s", indexerName, resultLine["host"].(string))

			// Scale Down Indexer Cluster
			testcaseEnvInst.Log.Info("Scaling Down Indexer Cluster", "Current Replicas", scaledIndexerReplicas, "New Replicas", scaledIndexerReplicas-1)
			scaledIndexerReplicas = scaledIndexerReplicas - 1
			idxcName = deployment.GetName() + "-idxc"

			// Get instance of current Indexer CR with latest config
			idxc = &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to scale down Indxer Cluster")

			// Ensure Indxer cluster scales Down and go to ScalingDown phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingDown, idxcName)

			// Ensure Indexer cluster go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Wait for RF SF is Met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Search for data from removed indexer
			searchPod = searchHeadName
			searchString = fmt.Sprintf("index=%s host=%s | stats count by host", "main", indexerName)
			searchResultsResp, err = testenv.PerformSearchSync(ctx, searchPod, searchString, deployment)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchPod, searchString)

			// Verify result
			searchResponse = strings.Split(searchResultsResp, "\n")[0]
			jsonErr = json.Unmarshal([]byte(searchResponse), &searchResults)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			testcaseEnvInst.Log.Info("Search results :", "searchResults", searchResults["result"])
			Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, searchPod)

			resultLine = searchResults["result"].(map[string]interface{})
			testcaseEnvInst.Log.Info("Sync Search results host count:", "count", resultLine["count"].(string), "host", resultLine["host"].(string))
			testHostname = strings.Compare(resultLine["host"].(string), indexerName)
			Expect(testHostname).To(Equal(0), "Incorrect search result hostname. Expect: %s Got: %s", indexerName, resultLine["host"].(string))

		})
	})

	Context("Multisite Indexer Cluster (M4 - Multisite indexer Cluster, search head cluster)", func() {
		It("masterscaling, integration: Multisite IDXC can be scaled up and data is searchable", func() {

			defaultIndexerReplicas := 1
			siteCount := 3
			err := deployment.DeployMultisiteClusterMasterWithSearchHead(ctx, deployment.GetName(), defaultIndexerReplicas, siteCount, "")
			Expect(err).To(Succeed(), "Unable to deploy search head cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure indexers go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ingest data on Indexers
			for i := 1; i <= siteCount; i++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), i, 0)
				logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
				testenv.CreateMockLogfile(logFile, 2000)
				testenv.IngestFileViaMonitor(ctx, logFile, "main", podName, deployment)
			}

			// Scale indexers
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testcaseEnvInst.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)
			idxcName := deployment.GetName() + "-" + "site1"

			// Get instance of current Indexer CR with latest config
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to Scale Up Indexer Cluster")

			// Ensure Indxer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingUp, idxcName)

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ingest data on  new Indexers
			podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, 1)
			logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
			testenv.CreateMockLogfile(logFile, 2000)
			testenv.IngestFileViaMonitor(ctx, logFile, "main", podName, deployment)

			// Verify New Indexer On Cluster Master
			indexerName := podName
			testcaseEnvInst.Log.Info("Checking for Indexer On CM", "Indexer Name", indexerName)
			Expect(testenv.CheckIndexerOnCM(ctx, deployment, indexerName)).To(Equal(true))

			// Wait for RF SF is Met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Search for data on newly added indexer
			searchPod := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			searchString := fmt.Sprintf("index=%s host=%s | stats count by host", "main", indexerName)
			searchResultsResp, err := testenv.PerformSearchSync(ctx, searchPod, searchString, deployment)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchPod, searchString)

			// Verify result.
			searchResponse := strings.Split(searchResultsResp, "\n")[0]
			var searchResults map[string]interface{}
			jsonErr := json.Unmarshal([]byte(searchResponse), &searchResults)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			testcaseEnvInst.Log.Info("Search results :", "searchResults", searchResults["result"])
			Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, searchPod)

			resultLine := searchResults["result"].(map[string]interface{})
			testcaseEnvInst.Log.Info("Sync Search results host count:", "count", resultLine["count"].(string), "host", resultLine["host"].(string))
			testHostname := strings.Compare(resultLine["host"].(string), indexerName)
			Expect(testHostname).To(Equal(0), "Incorrect search result hostname. Expect: %s Got: %s", indexerName, resultLine["host"].(string))

			// Scale Down Indexer Cluster
			testcaseEnvInst.Log.Info("Scaling Down Indexer Cluster Site", "Current Replicas", scaledIndexerReplicas, "New Replicas", scaledIndexerReplicas-1)
			scaledIndexerReplicas = scaledIndexerReplicas - 1

			// Get instance of current Indexer CR with latest config
			idxc = &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to scale down Indxer Cluster")

			// Ensure Indxer cluster scales Down and go to ScalingDown phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingDown, idxcName)

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Wait for RF SF is Met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Search for data from removed indexer
			searchString = fmt.Sprintf("index=%s host=%s | stats count by host", "main", indexerName)
			searchResultsResp, err = testenv.PerformSearchSync(ctx, searchPod, searchString, deployment)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchPod, searchString)

			// Verify result.
			searchResponse = strings.Split(searchResultsResp, "\n")[0]
			jsonErr = json.Unmarshal([]byte(searchResponse), &searchResults)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			testcaseEnvInst.Log.Info("Search results :", "searchResults", searchResults["result"])
			Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, searchPod)

			resultLine = searchResults["result"].(map[string]interface{})
			testcaseEnvInst.Log.Info("Sync Search results host count:", "count", resultLine["count"].(string), "host", resultLine["host"].(string))
			testHostname = strings.Compare(resultLine["host"].(string), indexerName)
			Expect(testHostname).To(Equal(0), "Incorrect search result hostname. Expect: %s Got: %s", indexerName, resultLine["host"].(string))

		})
	})
})
