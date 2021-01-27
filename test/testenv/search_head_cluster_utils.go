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
	"fmt"
	"os/exec"
	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// DeleteSHC delete Search Head Cluster in given namespace
func DeleteSHC(ns string) {
	output, err := exec.Command("kubectl", "delete", "shc", "-n", ns, "--all").Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl delete shc -n %s --all", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
	} else {
		logf.Log.Info("SHC deleted", "Namespace", ns, "stdout", output)
	}
}

// SHCInNamespace returns true if SHC is present in namespace
func SHCInNamespace(ns string) bool {
	output, err := exec.Command("kubectl", "get", "searchheadcluster", "-n", ns).Output()
	deleted := true
	if err != nil {
		cmd := fmt.Sprintf("kubectl get shc -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return deleted
	}
	logf.Log.Info("Output of command", "Output", string(output))
	if strings.Contains(string(output), "No resources found in default namespace") {
		deleted = false
	}
	return deleted
}
