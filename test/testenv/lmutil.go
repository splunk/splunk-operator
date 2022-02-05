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

package testenv

import (
	"context"
	"encoding/json"
	"strings"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type licenserLocalPeerResponse struct {
	Entry []struct {
		Name    string `json:"name"`
		ID      string `json:"id"`
		Content struct {
			GUID                     []string `json:"guid"`
			LastTrackerdbServiceTime int      `json:"last_trackerdb_service_time"`
			LicenseKeys              []string `json:"license_keys"`
			MasterGUID               string   `json:"master_guid"`
			MasterURI                string   `json:"master_uri"`
		} `json:"content"`
	} `json:"entry"`
}

// CheckLicenseManagerConfigured checks if lm is configured on given pod
func CheckLicenseManagerConfigured(ctx context.Context, deployment *Deployment, podName string) bool {
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) " + splcommon.LocalURLLicensePeerJSONOutput
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	restResponse := licenserLocalPeerResponse{}
	err = json.Unmarshal([]byte(stdout), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse health status")
		return false
	}
	licenseManager := restResponse.Entry[0].Content.MasterURI
	logf.Log.Info("License Manager configuration on POD", "POD", podName, "License Manager", licenseManager)
	return strings.Contains(licenseManager, splcommon.TestLicenseManagerMgmtPort)
}
