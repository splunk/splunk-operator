// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
package ingestsearchtest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Ingest and Search Test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var firstLine string
	ctx := context.TODO()

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

	Context("Standalone deployment (S1)", func() {
		It("ingest_search, integration, s1: can search internal logs for standalone instance", func() {

			standalone, err := deployment.DeployStandalone(ctx, deployment.GetName(), "", "")
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for standalone to be in READY Status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			Eventually(func() enterpriseApi.Phase {
				podName := fmt.Sprintf("splunk-%s-standalone-0", deployment.GetName())

				searchString := "index=_internal | stats count by host"
				searchResultsResp, err := testenv.PerformSearchSync(ctx, podName, searchString, deployment)
				if err != nil {
					testcaseEnvInst.Log.Error(err, "Failed to execute search on pod", "pod", podName, "searchString", searchString)
					return enterpriseApi.PhaseError
				}
				testcaseEnvInst.Log.Info("Performed a search", "searchString", searchString)

				var searchResults map[string]interface{}
				unmarshalErr := json.Unmarshal([]byte(searchResultsResp), &searchResults)
				if unmarshalErr != nil {
					testcaseEnvInst.Log.Error(unmarshalErr, "Failed to unmarshal JSON response")
				}

				prettyResults, jsonErr := json.MarshalIndent(searchResults, "", "    ")
				if jsonErr != nil {
					testcaseEnvInst.Log.Error(jsonErr, "Failed to generate pretty json")
				} else {
					testcaseEnvInst.Log.Info("Sync Search results:", "prettyResults", string(prettyResults))
				}

				return standalone.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(enterpriseApi.PhaseReady))

			Eventually(func() enterpriseApi.Phase {
				podName := fmt.Sprintf("splunk-%s-standalone-0", deployment.GetName())
				searchString := "index=_internal GUID component=ServerConfig"

				// Perform a simple search
				sid, reqErr := testenv.PerformSearchReq(ctx, podName, searchString, deployment)
				if reqErr != nil {
					testcaseEnvInst.Log.Error(reqErr, "Failed to execute search on pod", "pod", podName, "searchString", searchString)
					return enterpriseApi.PhaseError
				}
				testcaseEnvInst.Log.Info("Got a search with sid", "sid", sid)

				// Check SID status until done
				searchStatusResult, statusErr := testenv.GetSearchStatus(ctx, podName, sid, deployment)
				if statusErr != nil {
					testcaseEnvInst.Log.Error(statusErr, "Failed to get search status on pod", "pod", podName, "sid", sid)
					return enterpriseApi.PhaseError
				}
				testcaseEnvInst.Log.Info("Search status:", "searchStatusResult", searchStatusResult)

				// Get SID results
				searchResultsResp, resErr := testenv.GetSearchResults(ctx, podName, sid, deployment)
				if resErr != nil {
					testcaseEnvInst.Log.Error(resErr, "Failed to get search results on pod", "pod", podName, "sid", sid)
					return enterpriseApi.PhaseError
				}

				// Display results for debug purposes
				prettyResults, jsonErr := json.MarshalIndent(searchResultsResp, "", "    ")
				if jsonErr != nil {
					testcaseEnvInst.Log.Error(jsonErr, "Failed to generate pretty json")
				} else {
					testcaseEnvInst.Log.Info("Search results:", "prettyResults", string(prettyResults))
				}

				return standalone.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(enterpriseApi.PhaseReady))
		})
	})

	Context("Standalone deployment (S1)", func() {
		It("ingest_search, integration, s1: can ingest custom data to new index and search", func() {

			standalone, err := deployment.DeployStandalone(ctx, deployment.GetName(), "", "")
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for standalone to be in READY Status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify splunk status is up
			Eventually(func() enterpriseApi.Phase {
				podName := fmt.Sprintf("splunk-%s-standalone-0", deployment.GetName())

				splunkBin := "/opt/splunk/bin/splunk"
				username := "admin"
				password := "$(cat /mnt/splunk-secrets/password)"
				splunkCmd := "status"

				statusCmd := fmt.Sprintf("%s %s -auth %s:%s", splunkBin, splunkCmd, username, password)
				command := []string{"/bin/bash"}
				statusCmdResp, stderr, err := deployment.PodExecCommand(ctx, podName, command, statusCmd, false)
				if err != nil {
					testcaseEnvInst.Log.Error(err, "Failed to execute command on pod", "pod", podName, "statusCmd", statusCmd, "statusCmdResp", statusCmdResp, "stderr", stderr)
					return enterpriseApi.PhaseError
				}

				if !strings.Contains(strings.ToLower(statusCmdResp), strings.ToLower("splunkd is running")) {
					testcaseEnvInst.Log.Error(err, "Failed to find splunkd running", "pod", podName, "statusCmdResp", statusCmdResp)
					return enterpriseApi.PhaseError
				}

				testcaseEnvInst.Log.Info("Waiting for standalone splunkd status to be ready", "instance", standalone.ObjectMeta.Name, "Phase", standalone.Status.Phase)
				return standalone.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(enterpriseApi.PhaseReady))

			// Create an index
			podName := fmt.Sprintf("splunk-%s-standalone-0", deployment.GetName())
			indexName := "myTestIndex"

			// Create an index on a standalone instance
			err = testenv.CreateAnIndexStandalone(ctx, indexName, podName, deployment)
			Expect(err).To(Succeed(), "Failed response to add index to splunk")

			// Create a mock logfile to ingest
			logFile := "/tmp/test.log"
			err = testenv.CreateMockLogfile(logFile, 1)
			Expect(err).To(Succeed(), "Failed response to add index to splunk logfile %s", logFile)

			// Copy log file and ingest it
			err = testenv.IngestFileViaOneshot(ctx, logFile, indexName, podName, deployment)
			Expect(err).To(Succeed(), "Failed to ingest logfile %s on pod %s", logFile, podName)

			// Read first line to find a search token
			var file, openErr = os.Open(logFile)
			Expect(openErr).To(Succeed(), "Failed to open newly created logfile %s on pod %s", logFile, podName)

			reader := bufio.NewReader(file)
			var readErr error
			firstLine, readErr = reader.ReadString('\n')
			Expect(readErr).Should(Or(BeNil(), Equal(io.EOF)), "Failed to read first line of logfile %s on pod ", logFile, podName)

			tokens := strings.Fields(firstLine)
			Expect(len(tokens)).To(BeNumerically(">=", 2), "Incorrect tokens (%s) in first logline %s for logfile %s", tokens, firstLine, logFile)

			searchToken := tokens[len(tokens)-1]
			testcaseEnvInst.Log.Info("Got search token successfully", "logFile", logFile, "searchToken", searchToken)

			searchString := fmt.Sprintf("index=%s | stats count by host", indexName)

			// Wait for ingestion lag prior to searching
			time.Sleep(2 * time.Second)
			searchResultsResp, err := testenv.PerformSearchSync(ctx, podName, searchString, deployment)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", podName, searchString)

			// Verify result.  Should get count 1. result:{count:1}
			var searchResults map[string]interface{}
			jsonErr := json.Unmarshal([]byte(searchResultsResp), &searchResults)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			testcaseEnvInst.Log.Info("Search results :", "searchResults", searchResults["result"])
			Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, podName)

			hostCount := searchResults["result"].(map[string]interface{})
			testcaseEnvInst.Log.Info("Sync Search results host count:", "count", hostCount["count"].(string), "host", hostCount["host"].(string))
			testHostCnt := strings.Compare(hostCount["count"].(string), "1")
			testHostname := strings.Compare(hostCount["host"].(string), podName)
			Expect(testHostCnt).To(Equal(0), "Incorrect search results for count. Expect: 1 Got: %d", hostCount["count"].(string))
			Expect(testHostname).To(Equal(0), "Incorrect search result hostname. Expect: %s Got: %s", podName, hostCount["host"].(string))

			searchString2 := fmt.Sprintf("index=%s %s", indexName, searchToken)
			sid, reqErr := testenv.PerformSearchReq(ctx, podName, searchString2, deployment)
			Expect(reqErr).To(Succeed(), "Failed to execute search '%s' on pod %s", searchString, podName)
			testcaseEnvInst.Log.Info("Got a search with sid", "sid", sid)

			// Check SID status until done
			searchStatusResult, statusErr := testenv.GetSearchStatus(ctx, podName, sid, deployment)
			Expect(statusErr).To(Succeed(), "Failed to get search status on pod %s for sid %s", podName, sid)
			testcaseEnvInst.Log.Info("Search status:", "searchStatusResult", searchStatusResult)

			// Get SID results
			searchResultsResp, resErr := testenv.GetSearchResults(ctx, podName, sid, deployment)
			Expect(resErr).To(Succeed(), "Failed to get search results on pod %s for sid %s", podName, sid)

			testcaseEnvInst.Log.Info("Raw Search results:", "searchResultsResp", searchResultsResp)
			var searchResults2 testenv.SearchJobResultsResponse
			jsonErr = json.Unmarshal([]byte(searchResultsResp), &searchResults2)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			found := false
			for key, elem := range searchResults2.Results {
				testcaseEnvInst.Log.Info("Search results _raw and host:", "_raw", elem.Raw, "host", elem.SplunkServer, "firstLine", firstLine)
				trimFirstLine := strings.TrimSuffix(firstLine, "\n")
				if strings.Compare(elem.Raw, trimFirstLine) == 0 {
					testcaseEnvInst.Log.Info("Found search results in  _raw and splunk_server", "key", key, "podName", podName, "elem", elem)
					found = true
				}
			}
			Expect(found).To(Equal(true), "Incorrect search results %s", searchResults)
		})
	})
})
