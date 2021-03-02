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
	"os/exec"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// GetSplunkVersion gets splunk image version
func GetSplunkVersion(deployment *Deployment, ns string, podName string) *string {

	output, err := exec.Command("kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s %s -o json", ns, podName)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return nil
	}
	restResponse := PodDetailsStruct{}
	err = json.Unmarshal([]byte(output), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse JSON")
		return nil
	}
	version := restResponse.Status.ContainerStatuses[0].Image
	return &version

}

// ModifySplunkVersion updates the splunk version
func ModifySplunkVersion(deployment *Deployment, ns string, podName string, standalone *enterprisev1.Standalone, newImage string) bool {
	//Get current splunk version for update
	restResponse := GetSplunkVersion(deployment, ns, podName)
	_, err := json.Marshal(restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse response")
		return false
	}

	err = deployment.UpdateCR(standalone)
	if err != nil {
		logf.Log.Error(err, "Unable to update splunk version")
		return false
	}
	return true
}
