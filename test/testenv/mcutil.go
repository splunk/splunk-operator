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
	"os/exec"
	"strings"
	"time"

	enterprisev4 "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// getMCPod Get MC Pod String
func getMCPod(ns string) string {
	mcPod := fmt.Sprintf(MonitoringConsolePod, ns)
	ctx, cancel := context.WithTimeout(context.Background(), KubectlQuickTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "kubectl", "get", "pod", "-n", ns, mcPod).Output()
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
	ctx, cancel := context.WithTimeout(context.Background(), KubectlQuickTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "kubectl", "get", "sts", "-n", ns, mcSts).Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl get sts -n %s %s", ns, mcSts)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return ""
	}
	return strings.Split(string(output), "\n")[1]
}

// GetConfiguredPeers get list of Peers Configured on Monitoring Console
func GetConfiguredPeers(ns string, mcName string) []string {
	podName := fmt.Sprintf(MonitoringConsolePod, mcName)
	var peerList []string
	if len(podName) > 0 {
		peerFile := "/opt/splunk/etc/apps/splunk_monitoring_console/local/splunk_monitoring_console_assets.conf"
		ctx, cancel := context.WithTimeout(context.Background(), KubectlExecTimeout)
		defer cancel()
		output, err := exec.CommandContext(ctx, "kubectl", "exec", "-n", ns, podName, "--", "cat", peerFile).Output()
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
	logf.Log.Info("Peer List found on MC Pod", "mcPod", podName, "configuredPeers", peerList)
	return peerList
}

// CheckPodNameOnMC Check given pod is configured on Monitoring console pod
func CheckPodNameOnMC(ns string, mcName string, podName string) bool {
	// Get Peers configured on Monitoring Console
	peerList := GetConfiguredPeers(ns, mcName)
	logf.Log.Info("Peer List", "instance", peerList)
	found := false
	for _, peer := range peerList {
		if strings.Contains(peer, podName) {
			logf.Log.Info("Check Peer matches on pod", "podName", podName, "peerInPeerList", peer)
			found = true
			break
		}
	}
	return found
}

// GetPodIP returns IP address of a POD as a string
func GetPodIP(ns string, podName string) string {
	podDetails, err := getPodDetails(ns, podName)
	if err != nil {
		logf.Log.Error(err, "Failed to get pod details", "pod", podName)
		return ""
	}
	return podDetails.Status.PodIP
}

// GetMCConfigMap gets config map for give Monitoring Console Name
func GetMCConfigMap(ctx context.Context, deployment *Deployment, ns string, mcName string) (*corev1.ConfigMap, error) {
	mcConfigMapName := enterprise.GetSplunkMonitoringconsoleConfigMapName(mcName, enterprise.SplunkMonitoringConsole)
	mcConfigMap, err := GetConfigMap(ctx, deployment, ns, mcConfigMapName)
	if err != nil {
		logf.Log.Error(err, "Failed to get Monitoring Console Config Map")
		return mcConfigMap, err
	}
	logf.Log.Info("MC Config Map contents", "mcConfigMapName", mcConfigMapName, "data", mcConfigMap.Data)
	return mcConfigMap, err
}

// CheckPodNameInString checks for pod name in string
func CheckPodNameInString(podName string, configString string) bool {
	logf.Log.Info("Check MC Config String has Pod configured", "configString", configString, "podName", podName)
	return strings.Contains(configString, podName)
}

// MCReconfigParams holds the service name and URL parameters that differ between
// V3 (master) and V4 (manager) Monitoring Console tests.
type MCReconfigParams struct {
	CMServiceNameFmt string // format string for CM service name (e.g., ClusterMasterServiceName)
	CMURLKey         string // config map URL key (e.g., "SPLUNK_CLUSTER_MASTER_URL" or splcommon.ClusterManagerURL)
}

// MCVersionConfig captures the API-version-specific behaviour that differs
// between V3 (master) and V4 (manager) monitoring console tests.
type MCVersionConfig struct {
	MCReconfigParams

	NamePrefix string
	Label      string

	// DeployC3WithMC deploys a C3 single-site cluster with the given MC ref.
	DeployC3WithMC func(ctx context.Context, d *Deployment, name string, replicas int, shc bool, mcRef string) error

	// DeployM4WithMC deploys an M4 multisite cluster with the given MC ref.
	DeployM4WithMC func(ctx context.Context, d *Deployment, name string, replicas int, siteCount int, mcRef string, shc bool) error

	// NewCMObject returns a new, empty cluster-coordinator CR
	// (*ClusterMaster for V3, *ClusterManager for V4).
	NewCMObject func() client.Object

	// VerifyCMReady asserts the cluster coordinator has reached Ready phase.
	VerifyCMReady func(ctx context.Context, d *Deployment, te *TestCaseEnv) error

	// SHCReconfigTimeout is the timeout used when verifying MC config strings
	// after an SHC MC-ref reconfig (0 means use the synchronous check).
	SHCReconfigTimeout time.Duration

	// VerifyMCTwoReadyAfterSHC controls whether MC Two is explicitly
	// verified ready after the SHC reconfig step.
	VerifyMCTwoReadyAfterSHC bool
}

// ReconfigCMWithNewMC updates the Cluster Manager's MC ref to a new Monitoring Console,
// verifies the CM is ready, and deploys the new MC.
func ReconfigCMWithNewMC(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, cfg MCVersionConfig) (string, *enterprisev4.MonitoringConsole, error) {
	mcTwoName := deployment.GetName() + "-two"
	cm := cfg.NewCMObject()
	if err := testcaseEnvInst.UpdateMonitoringConsoleRefAndVerify(ctx, deployment, cm, deployment.GetName(), mcTwoName); err != nil {
		return "", nil, fmt.Errorf("unable to update CM MC ref: %w", err)
	}
	if err := cfg.VerifyCMReady(ctx, deployment, testcaseEnvInst); err != nil {
		return "", nil, fmt.Errorf("cluster manager not ready after MC reconfig: %w", err)
	}
	mcTwo, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcTwoName, "")
	if err != nil {
		return "", nil, fmt.Errorf("unable to deploy Monitoring Console Two: %w", err)
	}
	return mcTwoName, mcTwo, nil
}

// DeployMCAndVerifyRFSF deploys a Monitoring Console and verifies RF/SF is met.
func DeployMCAndVerifyRFSF(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, mcRef string) error {
	_, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcRef, deployment.GetName())
	if err != nil {
		return err
	}
	return testcaseEnvInst.VerifyRFSFMet(ctx, deployment)
}
