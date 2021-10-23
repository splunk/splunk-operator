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
	"strings"

	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	corev1 "k8s.io/api/core/v1"

	gomega "github.com/onsi/gomega"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// getMCPod Get MC Pod String
func getMCPod(ns string) string {
	mcPod := fmt.Sprintf(MonitoringConsolePod, ns, 0)
	output, err := exec.Command("kubectl", "get", "pod", "-n", ns, mcPod).Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s %s", ns, mcPod)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return ""
	}
	return strings.Split(string(output), "\n")[1]
}

// getMCSts Get Monitoring Console StatefulSet
func getMCSts(ns string) string {
	mcSts := fmt.Sprintf(MonitoringConsoleSts, ns)
	output, err := exec.Command("kubectl", "get", "sts", "-n", ns, mcSts).Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl get sts -n %s %s", ns, mcSts)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return ""
	}
	return strings.Split(string(output), "\n")[1]
}

// CheckMCPodReady check if monitoring pod is ready. Checking status of MC pod and Stateful set.
func CheckMCPodReady(ns string) bool {
	// Check Status of monitoring console statefulset
	stsLine := getMCSts(ns)
	if len(stsLine) == 0 {
		return false
	}
	stsSlice := strings.Fields(stsLine)
	logf.Log.Info("MC statefulset found", "POD", stsSlice[0], "READY", stsSlice[1])
	stsReady := strings.Contains(stsSlice[1], "1/1")

	// Check Status of monitoring console pod
	podLine := getMCPod(ns)
	if len(podLine) == 0 {
		return false
	}
	podSlice := strings.Fields(podLine)
	logf.Log.Info("MC Pod Found", "POD", podSlice[0], "READY", podSlice[1])
	podReady := strings.Contains(podSlice[1], "1/1") && strings.Contains(podSlice[2], "Running")

	return stsReady && podReady
}

// GetConfiguredPeers get list of Peers Configured on Montioring Console
func GetConfiguredPeers(ns string, mcName string) []string {
	podName := fmt.Sprintf(MonitoringConsolePod, mcName, 0)
	var peerList []string
	if len(podName) > 0 {
		peerFile := "/opt/splunk/etc/apps/splunk_monitoring_console/local/splunk_monitoring_console_assets.conf"
		output, err := exec.Command("kubectl", "exec", "-n", ns, podName, "--", "cat", peerFile).Output()
		if err != nil {
			cmd := fmt.Sprintf("kubectl exec -n %s %s -- cat %s", ns, podName, peerFile)
			logf.Log.Error(err, "Failed to execute command", "command", cmd)
		}
		for _, line := range strings.Split(string(output), "\n") {
			// Check for empty lines to prevent an error in logic below
			if len(line) == 0 {
				continue
			}
			// configuredPeers only appear in splunk_monitoring_console_assets.conf when peers are configured.
			if strings.Contains(line, "configuredPeers") {
				// Splitting configured peers on "=" and then "," to get list of peers configured
				peerString := strings.Trim(strings.Split(line, "=")[1], "")
				peerList = strings.Split(peerString, ",")
				break
			}
		}
	}
	logf.Log.Info("Peer List found on MC Pod", "MC POD", podName, "Configured Peers", peerList)
	return peerList
}

// DeleteMCPod delete monitoring console deployment
func DeleteMCPod(ns string) {
	mcSts := fmt.Sprintf(MonitoringConsoleSts, ns)
	output, err := exec.Command("kubectl", "delete", "sts", "-n", ns, mcSts).Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl delete sts -n %s %s", ns, mcSts)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
	} else {
		logf.Log.Info("Monitoring Console Stateful Set deleted", "Statefulset", mcSts, "stdout", output)
	}
}

// MCPodReady waits for MC pod to be in ready state
func MCPodReady(ns string, deployment *Deployment) {
	// Monitoring Console Pod is in Ready State
	gomega.Eventually(func() bool {
		logf.Log.Info("Checking status of Monitoring Console Pod")
		check := CheckMCPodReady(ns)
		DumpGetPods(ns)
		return check
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))

	// Verify MC Pod Stays in ready state
	gomega.Consistently(func() bool {
		logf.Log.Info("Checking status of Monitoring Console Pod")
		check := CheckMCPodReady(ns)
		DumpGetPods(ns)
		return check
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// CheckPodNameOnMC Check given pod is configured on Monitoring console pod
func CheckPodNameOnMC(ns string, mcName string, podName string) bool {
	// Get Peers configured on Monitoring Console
	peerList := GetConfiguredPeers(ns, mcName)
	logf.Log.Info("Peer List", "instance", peerList)
	found := false
	for _, peer := range peerList {
		if strings.Contains(peer, podName) {
			logf.Log.Info("Check Peer matches on pod", "Pod Name", podName, "Peer in peer list", peer)
			found = true
			break
		}
	}
	return found
}

// GetPodIP returns IP address of a POD as a string
func GetPodIP(ns string, podName string) string {
	output, err := exec.Command("kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s %s -o json", ns, podName)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return ""
	}
	restResponse := PodDetailsStruct{}
	err = json.Unmarshal([]byte(output), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse cluster searchheads")
		return ""
	}
	return restResponse.Status.PodIP
}

// GetMCConfigMap gets config map for give Monitoring Console Name
func GetMCConfigMap(deployment *Deployment, ns string, mcName string) (*corev1.ConfigMap, error) {
	mcConfigMapName := enterprise.GetSplunkMonitoringconsoleConfigMapName(mcName, enterprise.SplunkMonitoringConsole)
	mcConfigMap, err := GetConfigMap(deployment, ns, mcConfigMapName)
	if err != nil {
		logf.Log.Error(err, "Failed to get Monitoring Console Config Map")
		return mcConfigMap, err
	}
	logf.Log.Info("MC Config Map contents", "MC CONFIG MAP NAME", mcConfigMapName, "Data", mcConfigMap.Data)
	return mcConfigMap, err
}

// CheckPodNameInString checks for pod name in string
func CheckPodNameInString(podName string, configString string) bool {
	logf.Log.Info("Check MC Config String has Pod configured", "Monitoring Console Config Map Pod Config String", configString, "POD String", podName)
	return strings.Contains(configString, podName)
}
