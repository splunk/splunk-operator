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
	"fmt"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// SearchHeadPodName returns the pod name for a given search head index within an SHC deployment.
// Uses the operator's naming convention: splunk-<name>-search-head-<index>.
// Note: SearchHeadPod constant includes "-shc-" and is only correct when the CR name
// already has "-shc" appended (e.g. from DeploySingleSiteCluster). For direct SHC
// deployments using deployment.GetName() as the CR name, use this helper instead.
func SearchHeadPodName(deploymentName string, index int) string {
	return fmt.Sprintf("splunk-%s-search-head-%d", deploymentName, index)
}

// StartRealtimeSearch starts a never-ending real-time search on a search head pod via the Splunk REST API.
// The search runs until the pod is restarted or the job is explicitly cancelled.
// Retries for up to 2 minutes to handle the window between PhaseReady and the pod being exec-ready.
func StartRealtimeSearch(ctx context.Context, deployment *Deployment, podName string) error {
	stdin := `curl -k -u admin:$(cat /mnt/splunk-secrets/password) \
		--data-urlencode "search=search index=_internal" \
		-d "earliest_time=rt&latest_time=rt&exec_mode=normal&search_mode=realtime" \
		https://localhost:8089/services/search/jobs`
	return podExecWithRetry(ctx, deployment, podName, stdin)
}

// StartHistoricalSearch starts a bounded historical search that completes naturally.
// Used to verify that normal search drain does not trigger the detention timeout.
// Retries for up to 2 minutes to handle the window between PhaseReady and the pod being exec-ready.
func StartHistoricalSearch(ctx context.Context, deployment *Deployment, podName string) error {
	stdin := `curl -k -u admin:$(cat /mnt/splunk-secrets/password) \
		--data-urlencode "search=search index=_internal | head 1000" \
		-d "earliest_time=-5m&latest_time=now&exec_mode=normal" \
		https://localhost:8089/services/search/jobs`
	return podExecWithRetry(ctx, deployment, podName, stdin)
}

// podExecWithRetry retries a shell command on a pod for up to 2 minutes to handle
// the window between PhaseReady and the pod being exec-ready. The 2-minute deadline
// is enforced independently of the caller's context so that a persistent exec failure
// reports quickly rather than blocking for the full spec timeout.
func podExecWithRetry(ctx context.Context, deployment *Deployment, podName string, stdin string) error {
	retryCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	command := []string{"/bin/sh"}
	var lastErr error
	for {
		select {
		case <-retryCtx.Done():
			return fmt.Errorf("pod exec on %s did not succeed within 2 minutes: %w", podName, lastErr)
		default:
		}
		_, _, err := deployment.PodExecCommand(retryCtx, podName, command, stdin, false)
		if err == nil {
			return nil
		}
		lastErr = err
		logf.Log.Info("Retrying pod exec", "pod", podName, "error", err)
		time.Sleep(10 * time.Second)
	}
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
			logf.Log.Info("Deployer app not found on any members", "appName", appName)
			return make(map[string]int)
		}
	}
	logf.Log.Info("App bundle push info for deployer", podName, appBundlePush)
	return appBundlePush
}
