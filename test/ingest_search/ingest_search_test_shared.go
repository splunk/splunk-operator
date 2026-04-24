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
package ingestsearch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

// RunS1InternalLogSearchTest deploys a Standalone instance and verifies internal log searches
// using both synchronous and asynchronous search APIs.
func RunS1InternalLogSearchTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv) {
	_, err := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, "", "")
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance")

	podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)

	// Verify sync search on _internal index
	syncSearchString := "index=_internal | stats count by host"
	Eventually(func() error {
		searchResultsResp, err := testenv.PerformSearchSync(ctx, deployment, podName, syncSearchString)
		if err != nil {
			return fmt.Errorf("failed to execute sync search: %w", err)
		}

		var searchResults map[string]interface{}
		if err := json.Unmarshal([]byte(searchResultsResp), &searchResults); err != nil {
			return fmt.Errorf("failed to unmarshal JSON response: %w", err)
		}

		prettyResults, err := json.MarshalIndent(searchResults, "", "    ")
		if err != nil {
			testcaseEnvInst.Log.Error(err, "Failed to generate pretty JSON")
		} else {
			testcaseEnvInst.Log.Info("Sync search results", "prettyResults", string(prettyResults))
		}

		return nil
	}, deployment.GetTimeout(), testenv.PollInterval).Should(Succeed(), "Sync search on _internal index failed")

	// Verify async search on _internal index
	asyncSearchString := "index=_internal GUID component=ServerConfig"
	Eventually(func() error {
		sid, err := testenv.PerformSearchReq(ctx, deployment, podName, asyncSearchString)
		if err != nil {
			return fmt.Errorf("failed to execute async search: %w", err)
		}
		testcaseEnvInst.Log.Info("Got a search with SID", "sid", sid)

		searchStatusResult, err := testenv.GetSearchStatus(ctx, deployment, podName, sid)
		if err != nil {
			return fmt.Errorf("failed to get search status: %w", err)
		}
		testcaseEnvInst.Log.Info("Search status", "searchStatusResult", searchStatusResult)

		searchResultsResp, err := testenv.GetSearchResults(ctx, deployment, podName, sid)
		if err != nil {
			return fmt.Errorf("failed to get search results: %w", err)
		}

		prettyResults, err := json.MarshalIndent(searchResultsResp, "", "    ")
		if err != nil {
			testcaseEnvInst.Log.Error(err, "Failed to generate pretty JSON")
		} else {
			testcaseEnvInst.Log.Info("Async search results", "prettyResults", string(prettyResults))
		}

		return nil
	}, deployment.GetTimeout(), testenv.PollInterval).Should(Succeed(), "Async search on _internal index failed")
}

// RunS1IngestAndSearchTest deploys a Standalone instance, ingests a custom log file into a new
// index, and verifies the ingested data is searchable via both sync and async search APIs.
func RunS1IngestAndSearchTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv) {
	_, err := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, "", "")
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance")

	podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	indexName := "myTestIndex"

	err = testenv.CreateAnIndexStandalone(ctx, deployment, indexName, podName)
	Expect(err).To(Succeed(), "Failed to add index to Standalone")

	logFile := "/tmp/test.log"
	err = testenv.CreateMockLogfile(logFile, 1)
	Expect(err).To(Succeed(), "Failed to create mock logfile %s", logFile)

	err = testenv.IngestFileViaOneshot(ctx, deployment, logFile, indexName, podName)
	Expect(err).To(Succeed(), "Failed to ingest logfile %s on pod %s", logFile, podName)

	file, openErr := os.Open(logFile)
	Expect(openErr).To(Succeed(), "Failed to open logfile %s", logFile)
	defer file.Close()

	reader := bufio.NewReader(file)
	firstLine, readErr := reader.ReadString('\n')
	Expect(readErr).Should(Or(BeNil(), Equal(io.EOF)), "Failed to read first line of logfile %s on pod %s", logFile, podName)

	tokens := strings.Fields(firstLine)
	Expect(len(tokens)).To(BeNumerically(">=", 2), "Incorrect tokens (%s) in first logline %s for logfile %s", tokens, firstLine, logFile)

	searchToken := tokens[len(tokens)-1]
	testcaseEnvInst.Log.Info("Got search token successfully", "logFile", logFile, "searchToken", searchToken)

	searchString := fmt.Sprintf("index=%s | stats count by host", indexName)

	err = testenv.WaitForSearchResultsNonEmpty(ctx, deployment, podName, searchString, 2*time.Second)
	Expect(err).To(Succeed(), "Timed out waiting for search results")

	searchResultsResp, err := testenv.PerformSearchSync(ctx, deployment, podName, searchString)
	Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchString, podName)

	var searchResults map[string]interface{}
	jsonErr := json.Unmarshal([]byte(searchResultsResp), &searchResults)
	Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON search results from response '%s'", searchResultsResp)

	testcaseEnvInst.Log.Info("Search results", "searchResults", searchResults["result"])
	Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, podName)

	hostCount := searchResults["result"].(map[string]interface{})
	testcaseEnvInst.Log.Info("Sync search results host count", "count", hostCount["count"].(string), "host", hostCount["host"].(string))
	Expect(hostCount["count"].(string)).To(Equal("1"), "Incorrect search results for count. Expected: 1 Got: %s", hostCount["count"].(string))
	Expect(hostCount["host"].(string)).To(Equal(podName), "Incorrect search result hostname. Expected: %s Got: %s", podName, hostCount["host"].(string))

	searchString2 := fmt.Sprintf("index=%s %s", indexName, searchToken)
	sid, reqErr := testenv.PerformSearchReq(ctx, deployment, podName, searchString2)
	Expect(reqErr).To(Succeed(), "Failed to execute search '%s' on pod %s", searchString2, podName)
	testcaseEnvInst.Log.Info("Got a search with SID", "sid", sid)

	searchStatusResult, statusErr := testenv.GetSearchStatus(ctx, deployment, podName, sid)
	Expect(statusErr).To(Succeed(), "Failed to get search status on pod %s for SID %s", podName, sid)
	testcaseEnvInst.Log.Info("Search status", "searchStatusResult", searchStatusResult)

	searchResultsResp, resErr := testenv.GetSearchResults(ctx, deployment, podName, sid)
	Expect(resErr).To(Succeed(), "Failed to get search results on pod %s for SID %s", podName, sid)

	testcaseEnvInst.Log.Info("Raw search results", "searchResultsResp", searchResultsResp)
	var searchResults2 testenv.SearchJobResultsResponse
	jsonErr = json.Unmarshal([]byte(searchResultsResp), &searchResults2)
	Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON search results from response '%s'", searchResultsResp)

	trimFirstLine := strings.TrimSuffix(firstLine, "\n")
	found := false
	for key, elem := range searchResults2.Results {
		testcaseEnvInst.Log.Info("Search results _raw and host", "_raw", elem.Raw, "host", elem.SplunkServer, "firstLine", firstLine)
		if elem.Raw == trimFirstLine {
			testcaseEnvInst.Log.Info("Found search results in _raw and splunk_server", "key", key, "podName", podName, "elem", elem)
			found = true
		}
	}
	Expect(found).To(BeTrue(), "Incorrect search results %s", searchResults)
}
