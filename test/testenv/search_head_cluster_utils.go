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

// DeployerAppChecksum Get the checksum for each app on the deployer
func DeployerAppChecksum(ctx context.Context, deployment *Deployment) map[string]string {
	appChecksum := make(map[string]string)
	podName := fmt.Sprintf(DeployerPod, deployment.GetName())
	stdin := "/opt/splunk/bin/splunk list shcluster-bundle -auth admin:$(cat /mnt/splunk-secrets/password)"
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return appChecksum
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr, "err", err)
	appName := ""

	for _, line := range strings.Split(string(stdout), "\n") {
		// Check for empty lines to prevent an error in logic below
		if len(line) == 0 {
			continue
		}
		// Extract
		if !strings.Contains(line, ":") {
			appName = strings.TrimSpace(line)
		}
		if strings.Contains(line, "checksum") {
			appChecksum[appName] = strings.TrimSpace(strings.Split(line, ":")[1])
		}
	}
	logf.Log.Info("App checksum", "appChecksum", appChecksum)
	return appChecksum
}

// DeployerBundlePushstatus Get the bundle push status on Deployer
func DeployerBundlePushstatus(ctx context.Context, deployment *Deployment, ns string) map[string]int {
	appBundlePush := make(map[string]int)
	appChecksum := DeployerAppChecksum(ctx, deployment)
	podName := fmt.Sprintf(DeployerPod, deployment.GetName())
	stdin := fmt.Sprintf("/opt/splunk/bin/splunk list shcluster-bundle -member_uri https://splunk-%s-shc-search-head-0.splunk-%s-shc-search-head-headless.%s.svc.cluster.local:8089 -auth admin:$(cat /mnt/splunk-secrets/password)", deployment.GetName(), deployment.GetName(), ns)
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return appBundlePush
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr, "err", err)

	var appName string
	var memberStanza bool
	var checksumCheck bool
	for _, line := range strings.Split(string(stdout), "\n") {
		// Check for empty lines to prevent an error in logic below
		if len(line) == 0 {
			continue
		}
		// Extract appName from output
		if !strings.Contains(line, ":") {
			appName = strings.TrimSpace(line)
			memberStanza = false
			checksumCheck = false
		}
		// Match app checksum with deployer
		if strings.Contains(line, "checksum") {
			if appChecksum[appName] == strings.TrimSpace(strings.Split(line, ":")[1]) {
				checksumCheck = true
			}
		}
		//Update the hashmap when checksum for the app matches
		if checksumCheck {
			// When looking into member info in output
			if memberStanza {
				if strings.Contains(line, "push_status") {
					if strings.TrimSpace(strings.Split(line, ":")[1]) == "in_sync" {
						checksumCheck = false
						if _, present := appBundlePush[appName]; present {
							appBundlePush[appName] = appBundlePush[appName] + 1
						} else {
							appBundlePush[appName] = 1
						}
					}
				}
				//When looking at Deployer info in output
			} else {
				if strings.Contains(line, "deployer_push_status") {
					if strings.TrimSpace(strings.Split(line, ":")[1]) == "in_sync_with_all_members" {
						memberStanza = true
						checksumCheck = false
					}
				}
			}
		}
	}
	for appName := range appChecksum {
		if _, present := appBundlePush[appName]; !present {
			logf.Log.Info("Deployer app not found on any members", "Appname", appName)
			return make(map[string]int)
		}
	}
	if len(appBundlePush) == 0 {
		stdin = "ls -lt /opt/splunk/etc/shcluster/apps/"
		stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
		logf.Log.Error(err, "vivek shcluster - Failed to execute command on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

		stdin = "ls -ltR /init-apps/"
		stdout, stderr, err = deployment.PodExecCommand(ctx, podName, command, stdin, false)
		logf.Log.Error(err, "vivek init-apps - Failed to execute command on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

		podName = fmt.Sprintf(SearchHeadPod, deployment.GetName(), 0)
		stdin = "ls -lt /opt/splunk/etc/apps/"
		stdout, stderr, err = deployment.PodExecCommand(ctx, podName, command, stdin, false)
		logf.Log.Error(err, "vivek shcluster-head-0 - Failed to execute command on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

		podName = fmt.Sprintf(SearchHeadPod, deployment.GetName(), 1)
		stdin = "ls -lt /opt/splunk/etc/apps/"
		stdout, stderr, err = deployment.PodExecCommand(ctx, podName, command, stdin, false)
		logf.Log.Error(err, "vivek shcluster-head-1 - Failed to execute command on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

		podName = fmt.Sprintf(SearchHeadPod, deployment.GetName(), 2)
		stdin = "ls -lt /opt/splunk/etc/apps/"
		stdout, stderr, err = deployment.PodExecCommand(ctx, podName, command, stdin, false)
		logf.Log.Error(err, "vivek shcluster-head-2 - Failed to execute command on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

	}

	logf.Log.Info("App bundle push info for deployer", podName, appBundlePush)
	return appBundlePush
}
