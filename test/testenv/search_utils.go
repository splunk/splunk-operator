// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package testenv

import (
	"context"
	"encoding/json"
	"fmt"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// SearchJobStatusResponse represents the search status returned by splunk for
// endpoint: https://localhost:8089/services/search/jobs/<sid>
type SearchJobStatusResponse struct {
	Entries []SearchJobStatusEntry `json:"entry"`
}

// SearchJobStatusEntry represents the metadata for a given sid returned as part of the search status
type SearchJobStatusEntry struct {
	Name    string
	ID      string
	Content SearchJobStatusContent
}

// SearchJobStatusContent represents the search metadata returned as part of the search status
type SearchJobStatusContent struct {
	IsDone bool
}

// SearchJobResultsResponse represents the search results on non-transforming searches
type SearchJobResultsResponse struct {
	Fields  []SearchJobResponseFields  `json:"fields"`
	Results []SearchJobResponseResults `json:"results"`
}

// SearchJobResponseFields represents the fields in results from non-transforming searches
type SearchJobResponseFields struct {
	Name string
}

// SearchJobResponseResults represents the results from non-transforming searches
type SearchJobResponseResults struct {
	Raw          string `json:"_raw"`
	Source       string `json:"source"`
	Sourcetype   string `json:"sourcetype"`
	SplunkServer string `json:"splunk_server"`
}

// splunkdCurlExec builds and executes a curl command against the local splunkd REST API on the given pod.
func splunkdCurlExec(ctx context.Context, deployment *Deployment, podName string, curlArgs string) (string, error) {
	stdin := fmt.Sprintf("curl -ks -u admin:$(cat /mnt/splunk-secrets/password) %s", curlArgs)
	stdout, _, err := deployment.PodExecCommand(ctx, podName, []string{"/bin/sh"}, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute curl on pod", "pod", podName)
		return "", err
	}
	return stdout, nil
}

// PerformSearchSync performs a syncronous search within splunk and returns the search results
func PerformSearchSync(ctx context.Context, deployment *Deployment, podName string, search string) (string, error) {
	curlArgs := fmt.Sprintf("https://localhost:8089/services/search/jobs/export -d output_mode=json -d search=\"search %s\"", search)
	resp, err := splunkdCurlExec(ctx, deployment, podName, curlArgs)
	if err != nil {
		return "", err
	}
	logf.Log.Info("Output of search Query", "search", search, "output", resp)
	return resp, nil
}

// PerformSearchReq makes a search request for a search to be performed.  Returns a sid to be used to check for status and results
func PerformSearchReq(ctx context.Context, deployment *Deployment, podName string, search string) (string, error) {
	curlArgs := fmt.Sprintf("https://localhost:8089/services/search/jobs -d output_mode=json -d search=\"search %s\"", search)
	stdout, err := splunkdCurlExec(ctx, deployment, podName, curlArgs)
	if err != nil {
		return "", err
	}
	logf.Log.Info("Output of search Query", "search", search, "output", stdout)

	var searchReqResult map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &searchReqResult); err != nil {
		logf.Log.Error(err, "Failed to unmarshal JSON search request response")
		return "", err
	}
	sid := searchReqResult["sid"].(string)
	return sid, nil
}

// GetSearchStatus checks the search status for a given <sid>
func GetSearchStatus(ctx context.Context, deployment *Deployment, podName string, sid string) (*SearchJobStatusResponse, error) {
	curlArgs := fmt.Sprintf("https://localhost:8089/services/search/jobs/%s -d output_mode=json", sid)
	resp, err := splunkdCurlExec(ctx, deployment, podName, curlArgs)
	if err != nil {
		return nil, err
	}

	var result SearchJobStatusResponse
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		logf.Log.Error(err, "Failed to unmarshal JSON search status response")
		return nil, err
	}
	return &result, nil
}

// GetSearchResults retrieve the results for a given <sid> once the search status isDone == true
func GetSearchResults(ctx context.Context, deployment *Deployment, podName string, sid string) (string, error) {
	curlArgs := fmt.Sprintf("https://localhost:8089/services/search/jobs/%s/results/ --get -d output_mode=json", sid)
	return splunkdCurlExec(ctx, deployment, podName, curlArgs)
}
