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

package testenv

import (
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

// PerformSearchSync performs a syncronous search within splunk and returns the search results
func PerformSearchSync(podName string, search string, deployment *Deployment) (string, error) {
	// Build the search curl command and send it to an instance
	curlCmd := "curl -ks -u"
	username := "admin"
	password := "$(cat /mnt/splunk-secrets/password)"
	url := "https://localhost:8089/services/search/jobs/export"

	searchReq := fmt.Sprintf("%s %s:%s %s -d output_mode=json -d search=\"search %s\"", curlCmd, username, password, url, search)
	command := []string{"/bin/sh"}
	searchReqResp, stderr, err := deployment.PodExecCommand(podName, command, searchReq, false)
	_ = stderr
	if err != nil {
		logf.Log.Error(err, "Failed to execute cmd on pod", "pod", podName, "command", command)
		return "", err
	}

	logf.Log.Info("Output of search Query", "Search", search, "Output", searchReqResp)

	// Since results can have multiple formats depending on the search SPL, leave this response as a string
	return searchReqResp, err
}

// PerformSearchReq makes a search request for a search to be performed.  Returns a sid to be used to check for status and results
func PerformSearchReq(podName string, search string, deployment *Deployment) (string, error) {
	// Build the search curl command
	curlCmd := "curl -ks -u"
	url := "https://localhost:8089/services/search/jobs"
	username := "admin"
	password := "$(cat /mnt/splunk-secrets/password)"

	searchReq := fmt.Sprintf("%s %s:%s %s -d output_mode=json -d search=\"search %s\"", curlCmd, username, password, url, search)

	// Send search request to instance
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, searchReq, false)
	_ = stderr
	if err != nil {
		logf.Log.Error(err, "Failed to execute cmd on pod", "pod", podName, "command", command)
		return "", err
	}

	logf.Log.Info("Output of search Query", "Search", search, "Output", stdout)

	// Get SID
	var searchReqResult map[string]interface{}
	jsonErr := json.Unmarshal([]byte(stdout), &searchReqResult)
	if jsonErr != nil {
		logf.Log.Error(jsonErr, "Failed to unmarshal JSON Search Request Response to get SID")
		return "", jsonErr
	}
	sid := searchReqResult["sid"].(string)
	return sid, err
}

// GetSearchStatus checks the search status for a given <sid>
func GetSearchStatus(podName string, sid string, deployment *Deployment) (*SearchJobStatusResponse, error) {
	// Build search status request curl command
	curlCmd := "curl -ks -u"
	url := "https://localhost:8089/services/search/jobs"
	username := "admin"
	password := "$(cat /mnt/splunk-secrets/password)"

	searchStatusReq := fmt.Sprintf("%s %s:%s %s/%s -d output_mode=json", curlCmd, username, password, url, sid)

	// Send search status request to instance
	command := []string{"/bin/sh"}
	searchStatusResp, stderr, err := deployment.PodExecCommand(podName, command, searchStatusReq, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute cmd on pod", "pod", podName, "command", command, "stderr", stderr)
		return nil, err
	}

	// Parse resulting JSON
	searchStatusResult := SearchJobStatusResponse{}
	jsonErr := json.Unmarshal([]byte(searchStatusResp), &searchStatusResult)
	if jsonErr != nil {
		logf.Log.Error(jsonErr, "Failed to unmarshal JSON Search Status Response to get SID")
		return nil, jsonErr
	}
	return &searchStatusResult, err
}

// GetSearchResults retrieve the results for a given <sid> once the search status isDone == true
func GetSearchResults(podName string, sid string, deployment *Deployment) (string, error) {
	// Build search results request curl command
	curlCmd := "curl -ks -u"
	url := "https://localhost:8089/services/search/jobs"
	username := "admin"
	password := "$(cat /mnt/splunk-secrets/password)"

	searchResultsReq := fmt.Sprintf("%s %s:%s %s/%s/results/ --get -d output_mode=json", curlCmd, username, password, url, sid)

	// Send search results request to instance
	command := []string{"/bin/sh"}
	searchResultsResp, stderr, err := deployment.PodExecCommand(podName, command, searchResultsReq, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute cmd on pod", "pod", podName, "command", command, "stderr", stderr)
		return "", err
	}

	// Since results can have multiple formats depending on the search SPL (transforming vs. non-transforming, etc.), leave this response as a string
	return searchResultsResp, err
}
