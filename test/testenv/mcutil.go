package testenv

import (
	"fmt"
	"os/exec"
	"strings"

	gomega "github.com/onsi/gomega"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func getPods(ns string) string {
	output, err := exec.Command("kubectl", "get", "pod", "-n", ns).Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return ""
	}
	return string(output)
}

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
		cmd := fmt.Sprintf("kubectl get pods -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return ""
	}
	return strings.Split(string(output), "\n")[1]
}

// CheckMCPodReady check if monitoring pod is ready. Checking status of MC pod and Stateful set.
func CheckMCPodReady(ns string) bool {
	// Check Status of monitoring console statefulset
	stsLine := getMCSts(ns)
	if len(stsLine) < 0 {
		return false
	}
	stsSlice := strings.Fields(stsLine)
	logf.Log.Info("MC statefulset found", "POD", stsSlice[0], "READY", stsSlice[1])
	stsReady := strings.Contains(stsSlice[1], "1/1")

	// Check Status of monitoring console pod
	podLine := getMCPod(ns)
	if len(podLine) < 0 {
		return false
	}
	podSlice := strings.Fields(podLine)
	logf.Log.Info("MC Pod Found", "POD", podSlice[0], "READY", podSlice[1])
	podReady := strings.Contains(podSlice[1], "1/1") && strings.Contains(podSlice[2], "Running")

	return stsReady && podReady
}

// GetConfiguredPeers get list of Peers Configured on Montioring Console
func GetConfiguredPeers(ns string) []string {
	podName := fmt.Sprintf(MonitoringConsolePod, ns, 0)
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
}

// GetSearchHeadPeersOnMC GET Search head configured on MC
func GetSearchHeadPeersOnMC(ns string, deploymentName string, shCount int) map[string]bool {
	// Get Peers configured on Monitoring Console
	peerList := GetConfiguredPeers(ns)
	found := make(map[string]bool)
	logf.Log.Info("Peer List", "instance", peerList)

	// Check for SearchHead Peers in Peer List
	for i := 0; i < shCount; i++ {
		podName := fmt.Sprintf(SearchHeadPod, deploymentName, i)
		found[podName] = false
		for _, peer := range peerList {
			if strings.Contains(peer, podName) {
				logf.Log.Info("Check Peer matches search head pod", "Search Head Pod", podName, "Peer in peer list", peer)
				found[podName] = true
				break
			}
		}
	}
	return found
}

// CheckStandalonePodOnMC Check Standalone Pod configured on MC
func CheckStandalonePodOnMC(ns string, podName string) bool {
	// Get Peers configured on Monitoring Console
	peerList := GetConfiguredPeers(ns)
	logf.Log.Info("Peer List", "instance", peerList)
	found := false
	for _, peer := range peerList {
		if strings.Contains(peer, podName) {
			logf.Log.Info("Check Peer matches Standalone pod", "Standalone Pod", podName, "Peer in peer list", peer)
			found = true
			break
		}
	}
	return found
}
