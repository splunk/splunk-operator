// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testenv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	gomega "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// PodDetailsStruct captures output of kubectl get pods podname -o json
type PodDetailsStruct struct {
	Spec struct {
		Containers []struct {
			Resources struct {
				Limits struct {
					CPU    string `json:"cpu"`
					Memory string `json:"memory"`
				} `json:"limits"`
			} `json:"resources"`
		}
		ServiceAccount     string `json:"serviceAccount"`
		ServiceAccountName string `json:"serviceAccountName"`
	}

	Status struct {
		ContainerStatuses []struct {
			ContainerID string `json:"containerID"`
			Image       string `json:"image"`
			ImageID     string `json:"imageID"`
		} `json:"containerStatuses"`
		HostIP string `json:"hostIP"`
		Phase  string `json:"phase"`
		PodIP  string `json:"podIP"`
		PodIPs []struct {
			IP string `json:"ip"`
		} `json:"podIPs"`
		StartTime string `json:"startTime"`
	} `json:"status"`
}

// VerifyMonitoringConsoleReady verify Monitoring Console CR is in Ready Status and does not flip-flop
func VerifyMonitoringConsoleReady(deployment *Deployment, mcName string, monitoringConsole *enterpriseApi.MonitoringConsole, testenvInstance *TestEnv) {
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(mcName, monitoringConsole)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Monitoring Console phase to be ready", "instance", monitoringConsole.ObjectMeta.Name, "Phase", monitoringConsole.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return monitoringConsole.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() splcommon.Phase {
		_ = deployment.GetInstance(mcName, monitoringConsole)
		return monitoringConsole.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
}

// StandaloneReady verify Standalone is in ReadyStatus and does not flip-flop
func StandaloneReady(deployment *Deployment, deploymentName string, standalone *enterpriseApi.Standalone, testenvInstance *TestEnv) {
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(deploymentName, standalone)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Standalone phase to be ready", "instance", standalone.ObjectMeta.Name, "Phase", standalone.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return standalone.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() splcommon.Phase {
		_ = deployment.GetInstance(deployment.GetName(), standalone)
		return standalone.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
}

// SearchHeadClusterReady verify SHC is in READY status and does not flip-flop
func SearchHeadClusterReady(deployment *Deployment, testenvInstance *TestEnv) {
	shc := &enterpriseApi.SearchHeadCluster{}
	instanceName := fmt.Sprintf("%s-shc", deployment.GetName())
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(instanceName, shc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Search head cluster phase to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return shc.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(instanceName, shc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Deployer phase to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.DeployerPhase)
		DumpGetPods(testenvInstance.GetName())
		return shc.Status.DeployerPhase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(instanceName, shc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Search Head Cluster phase to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return shc.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() splcommon.Phase {
		_ = deployment.GetInstance(deployment.GetName(), shc)
		return shc.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
}

// SingleSiteIndexersReady verify single site indexers go to ready state
func SingleSiteIndexersReady(deployment *Deployment, testenvInstance *TestEnv) {
	idc := &enterpriseApi.IndexerCluster{}
	instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(instanceName, idc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for indexer instance's phase to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return idc.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() splcommon.Phase {
		_ = deployment.GetInstance(instanceName, idc)
		return idc.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
}

// ClusterManagerReady verify Cluster Manager Instance is in ready status
func ClusterManagerReady(deployment *Deployment, testenvInstance *TestEnv) {
	// Ensure that the cluster-manager goes to Ready phase
	cm := &enterpriseApi.ClusterMaster{}
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(deployment.GetName(), cm)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for "+splcommon.ClusterManager+" phase to be ready", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		// Test ClusterManager Phase to see if its ready
		return cm.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	// In a steady state, cluster-manager should stay in Ready and not flip-flop around
	gomega.Consistently(func() splcommon.Phase {
		_ = deployment.GetInstance(deployment.GetName(), cm)
		return cm.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
}

// IndexersReady verify indexers of all sites go to ready state
func IndexersReady(deployment *Deployment, testenvInstance *TestEnv, siteCount int) {
	siteIndexerMap := map[string][]string{}
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-indexer-0", instanceName)}
		// Ensure indexers go to Ready phase
		idc := &enterpriseApi.IndexerCluster{}
		gomega.Eventually(func() splcommon.Phase {
			err := deployment.GetInstance(instanceName, idc)
			if err != nil {
				return splcommon.PhaseError
			}
			testenvInstance.Log.Info("Waiting for indexer site instance phase to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
			DumpGetPods(testenvInstance.GetName())
			return idc.Status.Phase
		}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

		// In a steady state, we should stay in Ready and not flip-flop around
		gomega.Consistently(func() splcommon.Phase {
			_ = deployment.GetInstance(instanceName, idc)
			return idc.Status.Phase
		}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
	}
}

// IndexerClusterMultisiteStatus verify indexer Cluster is configured as multisite
func IndexerClusterMultisiteStatus(deployment *Deployment, testenvInstance *TestEnv, siteCount int) {
	siteIndexerMap := map[string][]string{}
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-indexer-0", instanceName)}
	}
	gomega.Eventually(func() map[string][]string {
		podName := fmt.Sprintf(ClusterManagerPod, deployment.GetName())
		stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) " + splcommon.LocalURLClusterManagerGetSite
		command := []string{"/bin/sh"}
		stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
		if err != nil {
			testenvInstance.Log.Error(err, "Failed to execute command", "on pod", podName, "command", command)
			return map[string][]string{}
		}
		testenvInstance.Log.Info("Command executed", "on pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
		siteIndexerResponse := ClusterMasterSitesResponse{}
		json.Unmarshal([]byte(stdout), &siteIndexerResponse)
		siteIndexerStatus := map[string][]string{}
		for _, site := range siteIndexerResponse.Entries {
			siteIndexerStatus[site.Name] = []string{}
			for _, peer := range site.Content.Peers {
				siteIndexerStatus[site.Name] = append(siteIndexerStatus[site.Name], peer.ServerName)
			}
		}
		return siteIndexerStatus
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(siteIndexerMap))
}

// VerifyRFSFMet verify RF SF is met on cluster manager
func VerifyRFSFMet(deployment *Deployment, testenvInstance *TestEnv) {
	gomega.Eventually(func() bool {
		rfSfStatus := CheckRFSF(deployment)
		testenvInstance.Log.Info("Verifying RF SF is met", "Status", rfSfStatus)
		return rfSfStatus
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyNoDisconnectedSHPresentOnCM is present on cluster manager
func VerifyNoDisconnectedSHPresentOnCM(deployment *Deployment, testenvInstance *TestEnv) {
	gomega.Consistently(func() bool {
		shStatus := CheckSearchHeadRemoved(deployment)
		testenvInstance.Log.Info("Verifying no Search Head in DISCONNECTED state present on Cluster Manager", "Status", shStatus)
		return shStatus
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyNoSHCInNamespace verify no SHC is present in namespace
func VerifyNoSHCInNamespace(deployment *Deployment, testenvInstance *TestEnv) {
	gomega.Eventually(func() bool {
		shcStatus := SHCInNamespace(testenvInstance.GetName())
		testenvInstance.Log.Info("Verifying no Search Head Cluster is present in namespace", "Status", shcStatus)
		return shcStatus
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(false))
}

// LicenseManagerReady verify LM is in ready status and does not flip flop
func LicenseManagerReady(deployment *Deployment, testenvInstance *TestEnv) {
	licenseMaster := &enterpriseApi.LicenseMaster{}

	testenvInstance.Log.Info("Verifying License Manager becomes READY")
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(deployment.GetName(), licenseMaster)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for License Manager instance status to be ready",
			"instance", licenseMaster.ObjectMeta.Name, "Phase", licenseMaster.Status.Phase)
		DumpGetPods(testenvInstance.GetName())

		return licenseMaster.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() splcommon.Phase {
		_ = deployment.GetInstance(deployment.GetName(), licenseMaster)
		return licenseMaster.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
}

// VerifyLMConfiguredOnPod verify LM is configured on given POD
func VerifyLMConfiguredOnPod(deployment *Deployment, podName string) {
	gomega.Consistently(func() bool {
		lmConfigured := CheckLicenseManagerConfigured(deployment, podName)
		return lmConfigured
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyServiceAccountConfiguredOnPod check if given service account is configured on given pod
func VerifyServiceAccountConfiguredOnPod(deployment *Deployment, ns string, podName string, serviceAccount string) {
	gomega.Consistently(func() bool {
		output, err := exec.Command("kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
		if err != nil {
			cmd := fmt.Sprintf("kubectl get pods -n %s %s -o json", ns, podName)
			logf.Log.Error(err, "Failed to execute command", "command", cmd)
			return false
		}
		restResponse := PodDetailsStruct{}
		err = json.Unmarshal([]byte(output), &restResponse)
		if err != nil {
			logf.Log.Error(err, "Failed to parse cluster Search heads")
			return false
		}
		logf.Log.Info("Service Account on Pod", "FOUND", restResponse.Spec.ServiceAccount, "EXPECTED", serviceAccount)
		return strings.Contains(serviceAccount, restResponse.Spec.ServiceAccount)
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyIndexFoundOnPod verify index found on a given POD
func VerifyIndexFoundOnPod(deployment *Deployment, podName string, indexName string) {
	gomega.Consistently(func() bool {
		indexFound, _ := GetIndexOnPod(deployment, podName, indexName)
		logf.Log.Info("Checking status of index on pod", "PODNAME", podName, "INDEX NAME", indexName, "STATUS", indexFound)
		return indexFound
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyIndexConfigsMatch verify index specific config
func VerifyIndexConfigsMatch(deployment *Deployment, podName string, indexName string, maxGlobalDataSizeMB int, maxGlobalRawDataSizeMB int) {
	gomega.Consistently(func() bool {
		indexFound, data := GetIndexOnPod(deployment, podName, indexName)
		logf.Log.Info("Checking status of index on pod", "PODNAME", podName, "INDEX NAME", indexName, "STATUS", indexFound)
		if indexFound {
			if data.Content.MaxGlobalDataSizeMB == maxGlobalDataSizeMB && data.Content.MaxGlobalRawDataSizeMB == maxGlobalRawDataSizeMB {
				logf.Log.Info("Checking index configs", "MaxGlobalDataSizeMB", data.Content.MaxGlobalDataSizeMB, "MaxGlobalRawDataSizeMB", data.Content.MaxGlobalRawDataSizeMB)
				return true
			}
		}
		return false
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyIndexExistsOnS3 Verify Index Exists on S3
func VerifyIndexExistsOnS3(deployment *Deployment, indexName string, podName string) {
	gomega.Eventually(func() bool {
		indexFound := CheckPrefixExistsOnS3(indexName)
		logf.Log.Info("Checking Index on S3", "INDEX NAME", indexName, "STATUS", indexFound)
		// During testing found some false failure. Rolling index buckets again to ensure data is pushed to remote storage
		if !indexFound {
			logf.Log.Info("Index NOT found. Rolling buckets again", "Index Name", indexName)
			RollHotToWarm(deployment, podName, indexName)
		}
		return indexFound
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyRollingRestartFinished verify no rolling restart is active
func VerifyRollingRestartFinished(deployment *Deployment) {
	gomega.Eventually(func() bool {
		rollingRestartStatus := CheckRollingRestartStatus(deployment)
		logf.Log.Info("Rolling Restart Status", "Active", rollingRestartStatus)
		return rollingRestartStatus
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyConfOnPod Verify give conf and value on config file on pod
func VerifyConfOnPod(deployment *Deployment, namespace string, podName string, confFilePath string, config string, value string) {
	gomega.Consistently(func() bool {
		confLine, err := GetConfLineFromPod(podName, confFilePath, namespace, config, "", false)
		if err != nil {
			logf.Log.Error(err, "Failed to get config on pod")
			return false
		}
		if strings.Contains(confLine, config) && strings.Contains(confLine, value) {
			logf.Log.Info("Config found", "Config", config, "Value", value, "Conf Line", confLine)
			return true
		}
		logf.Log.Info("Config NOT found")
		return false
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifySearchHeadClusterPhase verify the phase of SHC matches given phase
func VerifySearchHeadClusterPhase(deployment *Deployment, testenvInstance *TestEnv, phase splcommon.Phase) {
	gomega.Eventually(func() splcommon.Phase {
		shc := &enterpriseApi.SearchHeadCluster{}
		shcName := deployment.GetName() + "-shc"
		err := deployment.GetInstance(shcName, shc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Search Head Cluster Phase", "instance", shc.ObjectMeta.Name, "Expected", phase, "Phase", shc.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return shc.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseScalingUp))
}

// VerifyIndexerClusterPhase verify the phase of idxc matches the given phase
func VerifyIndexerClusterPhase(deployment *Deployment, testenvInstance *TestEnv, phase splcommon.Phase, idxcName string) {
	gomega.Eventually(func() splcommon.Phase {
		idxc := &enterpriseApi.IndexerCluster{}
		err := deployment.GetInstance(idxcName, idxc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Indexer Cluster Phase", "instance", idxc.ObjectMeta.Name, "Expected", phase, "Phase", idxc.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return idxc.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(phase))
}

// VerifyStandalonePhase verify the phase of Standalone CR
func VerifyStandalonePhase(deployment *Deployment, testenvInstance *TestEnv, crName string, phase splcommon.Phase) {
	gomega.Eventually(func() splcommon.Phase {
		standalone := &enterpriseApi.Standalone{}
		err := deployment.GetInstance(deployment.GetName(), standalone)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Standalone status", "instance", standalone.ObjectMeta.Name, "Expected", phase, " Actual Phase", standalone.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return standalone.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(phase))
}

// VerifyMonitoringConsolePhase verify the phase of Monitoring Console CR
func VerifyMonitoringConsolePhase(deployment *Deployment, testenvInstance *TestEnv, crName string, phase splcommon.Phase) {
	gomega.Eventually(func() splcommon.Phase {
		mc := &enterpriseApi.MonitoringConsole{}
		err := deployment.GetInstance(crName, mc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Monitoring Console CR status", "instance", mc.ObjectMeta.Name, "Expected", phase, " Actual Phase", mc.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return mc.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(phase))
}

// VerifyCPULimits verifies value of CPU limits is as expected
func VerifyCPULimits(deployment *Deployment, ns string, podName string, expectedCPULimits string) {
	gomega.Eventually(func() bool {
		output, err := exec.Command("kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
		if err != nil {
			cmd := fmt.Sprintf("kubectl get pods -n %s %s -o json", ns, podName)
			logf.Log.Error(err, "Failed to execute command", "command", cmd)
			return false
		}
		restResponse := PodDetailsStruct{}
		err = json.Unmarshal([]byte(output), &restResponse)
		if err != nil {
			logf.Log.Error(err, "Failed to parse JSON")
			return false
		}
		result := false

		for i := 0; i < len(restResponse.Spec.Containers); i++ {
			if strings.Contains(restResponse.Spec.Containers[0].Resources.Limits.CPU, expectedCPULimits) {
				result = true
				logf.Log.Info("Verifying CPU limits: ", "POD", podName, "FOUND", restResponse.Spec.Containers[0].Resources.Limits.CPU, "EXPECTED", expectedCPULimits)
			}
		}
		return result
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyClusterManagerPhase verify phase of cluster manager
func VerifyClusterManagerPhase(deployment *Deployment, testenvInstance *TestEnv, phase splcommon.Phase) {
	cm := &enterpriseApi.ClusterMaster{}
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(deployment.GetName(), cm)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for"+splcommon.ClusterManager+"Phase", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase, "Expected", phase)
		DumpGetPods(testenvInstance.GetName())
		// Test ClusterManager Phase to see if its ready
		return cm.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(phase))
}

// VerifySecretsOnPods Check whether the secret object info is mounted on given pods
// Set match to true or false to indicate desired +ve or -ve match
func VerifySecretsOnPods(deployment *Deployment, testenvInstance *TestEnv, verificationPods []string, data map[string][]byte, match bool) {
	for _, pod := range verificationPods {
		for secretKey, secretValue := range data {
			found := false
			currentValue := GetMountedKey(deployment, pod, secretKey)
			comparsion := bytes.Compare([]byte(currentValue), secretValue)
			if comparsion == 0 {
				found = true
				testenvInstance.Log.Info("Secret Values on POD Match", "Match Expected", match, "Pod Name ", pod, "Secret Key", secretKey, "Given Value of Key", string(secretValue), "Key Value found", currentValue)
			} else {
				testenvInstance.Log.Info("Secret Values on POD DONOT Match", "Match Expected", match, "Pod Name ", pod, "Secret Key", secretKey, "Given Value of Key", string(secretValue), "Key Value found", currentValue)
			}
			gomega.Expect(found).Should(gomega.Equal(match))
		}
	}
}

// VerifySecretsOnSecretObjects Compare secret value on passed in map to value present on secret object.
// Set match to true or false to indicate desired +ve or -ve match
func VerifySecretsOnSecretObjects(deployment *Deployment, testenvInstance *TestEnv, secretObjectNames []string, data map[string][]byte, match bool) {
	for _, secretName := range secretObjectNames {
		currentSecretData, err := GetSecretStruct(deployment, testenvInstance.GetName(), secretName)
		gomega.Expect(err).To(gomega.Succeed(), "Unable to get secret struct")
		for secretKey, secretValue := range data {
			found := false
			secretValueOnSecretObject := currentSecretData.Data[secretKey]
			comparsion := bytes.Compare(secretValueOnSecretObject, secretValue)
			if comparsion == 0 {
				testenvInstance.Log.Info("Secret Values on Secret Object Match", "Match Expected", match, "Secret Object Name", secretName, "Secret Key", secretKey, "Given Value of Key", string(secretValue), "Key Value found", string(secretValueOnSecretObject))
				found = true
			} else {
				testenvInstance.Log.Info("Secret Values on Secret Object DONOT match", "Match Expected", match, "Secret Object Name", secretName, "Secret Key", secretKey, "Given Value of Key", string(secretValue), "Key Value found", string(secretValueOnSecretObject))
			}
			gomega.Expect(found).Should(gomega.Equal(match))
		}
	}
}

// VerifySplunkServerConfSecrets Compare secret value on passed in map to value present on server.conf for given pods and secrets
// Set match to true or false to indicate desired +ve or -ve match
func VerifySplunkServerConfSecrets(deployment *Deployment, testenvInstance *TestEnv, verificationPods []string, data map[string][]byte, match bool) {
	for _, podName := range verificationPods {
		keysToMatch := GetKeysToMatch(podName)
		testenvInstance.Log.Info("Verificaton Keys Set", "Pod Name", podName, "Keys To Compare", keysToMatch)
		for _, secretName := range keysToMatch {
			found := false
			stanza := SecretKeytoServerConfStanza[secretName]
			_, value, err := GetSecretFromServerConf(deployment, podName, testenvInstance.GetName(), "pass4SymmKey", stanza)
			gomega.Expect(err).To(gomega.Succeed(), "Secret not found in conf file", "Secret Name", secretName)
			comparsion := strings.Compare(value, string(data[secretName]))
			if comparsion == 0 {
				testenvInstance.Log.Info("Secret Values on server.conf Match", "Match Expected", match, "Pod Name", podName, "Secret Key", secretName, "Given Value of Key", string(data[secretName]), "Key Value found", value)
				found = true
			} else {
				testenvInstance.Log.Info("Secret Values on server.conf DONOT MATCH", "Match Expected", match, "Pod Name", podName, "Secret Key", secretName, "Given Value of Key", string(data[secretName]), "Key Value found", value)
			}
			gomega.Expect(found).Should(gomega.Equal(match))
		}
	}
}

// VerifySplunkInputConfSecrets Compare secret value on passed in map to value present on input.conf for given indexer or standalone pods
// Set match to true or false to indicate desired +ve or -ve match
func VerifySplunkInputConfSecrets(deployment *Deployment, testenvInstance *TestEnv, verificationPods []string, data map[string][]byte, match bool) {
	secretName := "hec_token"
	for _, podName := range verificationPods {
		if strings.Contains(podName, "standalone") || strings.Contains(podName, "indexer") {
			found := false
			testenvInstance.Log.Info("Key Verificaton", "Pod Name", podName, "Key", secretName)
			stanza := SecretKeytoServerConfStanza[secretName]
			_, value, err := GetSecretFromInputsConf(deployment, podName, testenvInstance.GetName(), "token", stanza)
			gomega.Expect(err).To(gomega.Succeed(), "Secret not found in conf file", "Secret Name", secretName)
			comparsion := strings.Compare(value, string(data[secretName]))
			if comparsion == 0 {
				testenvInstance.Log.Info("Secret Values on input.conf Match", "Match Expected", match, "Pod Name", podName, "Secret Key", secretName, "Given Value of Key", string(data[secretName]), "Key Value found", value)
				found = true
			} else {
				testenvInstance.Log.Info("Secret Values on input.conf DONOT MATCH", "Match Expected", match, "Pod Name", podName, "Secret Key", secretName, "Given Value of Key", string(data[secretName]), "Key Value found", value)
			}
			gomega.Expect(found).Should(gomega.Equal(match))
		}
	}
}

// VerifySplunkSecretViaAPI check if keys can be used to access api i.e validate they are authentic
func VerifySplunkSecretViaAPI(deployment *Deployment, testenvInstance *TestEnv, verificationPods []string, data map[string][]byte, match bool) {
	var keysToMatch []string
	for _, podName := range verificationPods {
		if strings.Contains(podName, "standalone") || strings.Contains(podName, "indexer") {
			keysToMatch = []string{"password", "hec_token"}
		} else {
			keysToMatch = []string{"password"}
		}
		for _, secretName := range keysToMatch {
			validKey := false
			testenvInstance.Log.Info("Key Verificaton", "Pod Name", podName, "Key", secretName)
			validKey = CheckSecretViaAPI(deployment, podName, secretName, string(data[secretName]))
			gomega.Expect(validKey).Should(gomega.Equal(match))
		}
	}
}

// VerifyPVC verifies if PVC exists or not
func VerifyPVC(deployment *Deployment, testenvInstance *TestEnv, ns string, pvcName string, expectedToExist bool, verificationTimeout time.Duration) {
	gomega.Eventually(func() bool {
		pvcExists := false
		pvcsList := DumpGetPvcs(testenvInstance.GetName())
		for i := 0; i < len(pvcsList); i++ {
			if strings.EqualFold(pvcsList[i], pvcName) {
				pvcExists = true
				break
			}
		}
		testenvInstance.Log.Info("PVC Status Verified", "PVC", pvcName, "STATUS", pvcExists, "EXPECTED", expectedToExist)
		return pvcExists
	}, verificationTimeout, PollInterval).Should(gomega.Equal(expectedToExist))
}

// VerifyPVCsPerDeployment verifies for a given deployment if PVCs (etc and var) exists
func VerifyPVCsPerDeployment(deployment *Deployment, testenvInstance *TestEnv, deploymentType string, instances int, expectedtoExist bool, verificationTimeout time.Duration) {
	pvcKind := []string{"etc", "var"}
	for i := 0; i < instances; i++ {
		for _, pvcVolumeKind := range pvcKind {
			PvcName := fmt.Sprintf(PVCString, pvcVolumeKind, deployment.GetName(), deploymentType, i)
			VerifyPVC(deployment, testenvInstance, testenvInstance.GetName(), PvcName, expectedtoExist, verificationTimeout)
		}
	}
}

// VerifyAppInstalled verify that app of specific version is installed. Method assumes that app is installed in all CR's in namespace
func VerifyAppInstalled(deployment *Deployment, testenvInstance *TestEnv, ns string, pods []string, apps []string, versionCheck bool, statusCheck string, checkupdated bool, clusterWideInstall bool) {
	for _, podName := range pods {
		for _, appName := range apps {
			status, versionInstalled, err := GetPodAppStatus(deployment, podName, ns, appName, clusterWideInstall)
			logf.Log.Info("App details", "App", appName, "Status", status, "Version", versionInstalled, "Error", err)
			gomega.Expect(err).To(gomega.Succeed(), "Unable to get app status on pod ")
			comparison := strings.EqualFold(status, statusCheck)
			//Check the app is installed on specific pods and un-installed on others for cluster-wide install
			var check bool
			if clusterWideInstall {
				if strings.Contains(podName, "-indexer-") || strings.Contains(podName, "-search-head-") {
					check = true
					testenvInstance.Log.Info("App Install Check", "Pod", podName, "App", appName, "Expected", check, "Found", comparison, "Scope:cluster", clusterWideInstall)
					gomega.Expect(comparison).Should(gomega.Equal(check))
				}
			} else {
				// For local install check pods individually
				if strings.Contains(podName, "-indexer-") || strings.Contains(podName, "-search-head-") {
					check = false
				} else {
					check = true
				}
				testenvInstance.Log.Info("App Install Check", "Pod", podName, "App", appName, "Expected", check, "Found", comparison, "Scope:cluster", clusterWideInstall)
				gomega.Expect(comparison).Should(gomega.Equal(check))
			}

			if versionCheck {
				// For clusterwide install do not check for versions on deployer and cluster-manager as the apps arent installed there
				if !(clusterWideInstall && (strings.Contains(podName, splcommon.TestDeployerDashed) || strings.Contains(podName, splcommon.TestClusterManagerDashed))) {
					var expectedVersion string
					if checkupdated {
						expectedVersion = AppInfo[appName]["V2"]
					} else {
						expectedVersion = AppInfo[appName]["V1"]
					}
					testenvInstance.Log.Info("Verify app", "On pod", podName, "App name", appName, "Expected version", expectedVersion, "Version installed", versionInstalled, "Updated", checkupdated)
					gomega.Expect(versionInstalled).Should(gomega.Equal(expectedVersion))
				}
			}
		}
	}
}

// VerifyAppsCopied verify that apps are copied to correct location based on POD. Set checkAppDirectory false to verify app is not copied.
func VerifyAppsCopied(deployment *Deployment, testenvInstance *TestEnv, ns string, pods []string, apps []string, checkAppDirectory bool, scope string) {
	for _, podName := range pods {
		path := "etc/apps"
		//For cluster-wide install the apps are extracted to different locations
		if scope == enterpriseApi.ScopeCluster {
			if strings.Contains(podName, splcommon.ClusterManager) {
				path = splcommon.ManagerAppsLoc
			} else if strings.Contains(podName, splcommon.TestDeployerDashed) {
				path = splcommon.SHClusterAppsLoc
			} else if strings.Contains(podName, "-indexer-") {
				path = splcommon.PeerAppsLoc
			}
		}
		VerifyAppsInFolder(deployment, testenvInstance, ns, podName, apps, path, checkAppDirectory)
	}
}

// VerifyAppsInFolder verify that apps are present in folder. Set checkAppDirectory false to verify app is not copied.
func VerifyAppsInFolder(deployment *Deployment, testenvInstance *TestEnv, ns string, podName string, apps []string, path string, checkAppDirectory bool) {
	gomega.Eventually(func() bool {
		// Using checkAppDirectory here to get all files in case of negative check.  GetDirsOrFilesInPath  will return files/directory when checkAppDirecotry is FALSE
		appList, err := GetDirsOrFilesInPath(deployment, podName, path, checkAppDirectory)
		gomega.Expect(err).To(gomega.Succeed(), "Unable to get apps on pod", "Pod", podName)
		for _, app := range apps {
			folderName := app + "/"
			found := CheckStringInSlice(appList, folderName)
			logf.Log.Info("App check", "On pod", podName, "check app", folderName, "is in path", path, "Status", found)
			if found != checkAppDirectory {
				return false
			}
		}
		return true
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyAppsDownloadedOnContainer verify that apps are downloaded by init container
func VerifyAppsDownloadedOnContainer(deployment *Deployment, testenvInstance *TestEnv, ns string, pods []string, apps []string, path string) {
	for _, podName := range pods {
		appList, err := GetDirsOrFilesInPath(deployment, podName, path, false)
		gomega.Expect(err).To(gomega.Succeed(), "Unable to get apps on pod", "Pod", podName)
		for _, app := range apps {
			found := CheckStringInSlice(appList, app)
			testenvInstance.Log.Info("Check App files present on the pod", "Pod Name", podName, "App Name", app, "directory", path, "Status", found)
			gomega.Expect(found).Should(gomega.Equal(true))
		}
	}
}

// VerifyAppsPackageDeletedOnContainer verify that apps are deleted by container
func VerifyAppsPackageDeletedOnContainer(deployment *Deployment, testenvInstance *TestEnv, ns string, pods []string, apps []string, path string) {
	for _, podName := range pods {
		for _, app := range apps {
			gomega.Eventually(func() bool {
				appList, err := GetDirsOrFilesInPath(deployment, podName, path, false)
				if err != nil {
					testenvInstance.Log.Error(err, "Unable to get apps on pod", "Pod", podName)
					return true
				}
				found := CheckStringInSlice(appList, app+"_")
				testenvInstance.Log.Info(fmt.Sprintf("Check App package deleted on the pod %s. App Name %s. Directory %s, Status %t", podName, app, path, found))
				return found
			}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(false))
		}
	}
}

// VerifyAppListPhase verify given app Phase has completed for the given list of apps for given CR Kind
func VerifyAppListPhase(deployment *Deployment, testenvInstance *TestEnv, name string, crKind string, appSourceName string, phase enterpriseApi.AppPhaseType, appList []string) {
	if phase == enterpriseApi.PhaseDownload || phase == enterpriseApi.PhasePodCopy {
		for _, appName := range appList {
			testenvInstance.Log.Info(fmt.Sprintf("Check App Status for CR %s NAME %s APP NAME %s Expected Phase %s", crKind, name, appName, phase))
			gomega.Eventually(func() enterpriseApi.AppPhaseType {
				appDeploymentInfo, err := GetAppDeploymentInfo(deployment, testenvInstance, name, crKind, appSourceName, appName)
				if err != nil {
					testenvInstance.Log.Error(err, "Failed to get app deployment info")
					return phase
				}
				testenvInstance.Log.Info(fmt.Sprintf("App State found for CR %s NAME %s APP NAME %s Expected Phase %s", crKind, name, appName, phase), "Actual Phase", appDeploymentInfo.PhaseInfo.Phase, "App State", appDeploymentInfo)
				return appDeploymentInfo.PhaseInfo.Phase
			}, deployment.GetTimeout(), PollInterval).ShouldNot(gomega.Equal(phase))
		}
	} else {
		for _, appName := range appList {
			testenvInstance.Log.Info(fmt.Sprintf("Check App Status for CR %s NAME %s APP NAME %s Expected Phase %s", crKind, name, appName, phase))
			gomega.Eventually(func() enterpriseApi.AppPhaseType {
				appDeploymentInfo, err := GetAppDeploymentInfo(deployment, testenvInstance, name, crKind, appSourceName, appName)
				if err != nil {
					testenvInstance.Log.Error(err, "Failed to get app deployment info")
					return enterpriseApi.PhaseDownload
				}
				testenvInstance.Log.Info(fmt.Sprintf("App State found for CR %s NAME %s APP NAME %s Expected Phase %s", crKind, name, appName, phase), "Actual Phase", appDeploymentInfo.PhaseInfo.Phase, "App Phase Status", appDeploymentInfo.PhaseInfo.Status, "App State", appDeploymentInfo)
				if appDeploymentInfo.PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
					testenvInstance.Log.Info("Phase Install Not Complete.", "Phase Found", appDeploymentInfo.PhaseInfo.Phase, "Phase Status Found", appDeploymentInfo.PhaseInfo.Status)
					return enterpriseApi.PhaseDownload
				}
				return appDeploymentInfo.PhaseInfo.Phase
			}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(phase))
		}
	}
}

// VerifyAppState verify given app state is in between states passed as parameters, i.e when Status is between 101 and 303 we would pass enterpriseApi.AppPkgInstallComplete and enterpriseApi.AppPkgPodCopyComplete
func VerifyAppState(deployment *Deployment, testenvInstance *TestEnv, name string, crKind string, appSourceName string, appList []string, appStateFinal enterpriseApi.AppPhaseStatusType, appStateInitial enterpriseApi.AppPhaseStatusType) {
	for _, appName := range appList {
		gomega.Eventually(func() enterpriseApi.AppPhaseStatusType {
			appDeploymentInfo, _ := GetAppDeploymentInfo(deployment, testenvInstance, name, crKind, appSourceName, appName)
			return appDeploymentInfo.PhaseInfo.Status
		}, deployment.GetTimeout(), PollInterval).Should(gomega.BeNumerically("~", appStateFinal, appStateInitial)) //Check status value is between appStateInitial and appStateFinal
	}
}

// WaitForAppInstall waits until an app is correctly installed (having status equal to 303)
func WaitForAppInstall(deployment *Deployment, testenvInstance *TestEnv, name string, crKind string, appSourceName string, appList []string) {
	for _, appName := range appList {
		gomega.Eventually(func() enterpriseApi.AppPhaseStatusType {
			appDeploymentInfo, _ := GetAppDeploymentInfo(deployment, testenvInstance, name, crKind, appSourceName, appName)
			return appDeploymentInfo.PhaseInfo.Status
		}, deployment.GetTimeout(), PollInterval).Should(gomega.BeEquivalentTo(enterpriseApi.AppPkgInstallComplete))
	}

}

// VerifyPodsInMCConfigMap checks if given pod names are present in given KEY of given MC's Config Map
func VerifyPodsInMCConfigMap(deployment *Deployment, testenvInstance *TestEnv, pods []string, key string, mcName string, expected bool) {
	// Get contents of MC config map
	mcConfigMap, err := GetMCConfigMap(deployment, testenvInstance.GetName(), mcName)
	gomega.Expect(err).To(gomega.Succeed(), "Unable to get MC config map")
	for _, podName := range pods {
		testenvInstance.Log.Info("Checking for POD on  MC Config Map", "POD Name", podName, "DATA", mcConfigMap.Data)
		gomega.Expect(expected).To(gomega.Equal(CheckPodNameInString(podName, mcConfigMap.Data[key])), "Verify Pod in MC Config Map. Pod Name %s.", podName)
	}
}

// VerifyPodsInMCConfigString checks if given pod names are present in given KEY of given MC's Config Map
func VerifyPodsInMCConfigString(deployment *Deployment, testenvInstance *TestEnv, pods []string, mcName string, expected bool, checkPodIP bool) {
	for _, podName := range pods {
		testenvInstance.Log.Info("Checking pod configured in MC POD Peers String", "Pod Name", podName)
		var found bool
		if checkPodIP {
			podIP := GetPodIP(testenvInstance.GetName(), podName)
			found = CheckPodNameOnMC(testenvInstance.GetName(), mcName, podIP)
		} else {
			found = CheckPodNameOnMC(testenvInstance.GetName(), mcName, podName)
		}
		gomega.Expect(expected).To(gomega.Equal(found), "Verify Pod in MC Config String. Pod Name %s.", podName)
	}
}

// VerifyClusterManagerBundlePush verify that bundle push was pushed on all indexers
func VerifyClusterManagerBundlePush(deployment *Deployment, testenvInstance *TestEnv, ns string, replicas int, previousBundleHash string) {
	gomega.Eventually(func() bool {
		// Get Bundle status and check that each pod has successfully deployed the latest bundle
		clusterMasterBundleStatus := ClusterManagerBundlePushstatus(deployment, previousBundleHash)
		if len(clusterMasterBundleStatus) < replicas {
			testenvInstance.Log.Info("Bundle push on Pod not complete on all pods", "Pod with bundle push", clusterMasterBundleStatus)
			return false
		}
		clusterPodNames := DumpGetPods(testenvInstance.GetName())
		for _, podName := range clusterPodNames {
			if strings.Contains(podName, "-indexer-") {
				if _, present := clusterMasterBundleStatus[podName]; present {
					if clusterMasterBundleStatus[podName] != "Up" {
						testenvInstance.Log.Info("Bundle push on Pod not complete", "Pod Name", podName, "Status", clusterMasterBundleStatus[podName])
						return false
					}
				} else {
					testenvInstance.Log.Info("Bundle push not found on pod", "Podname", podName)
					return false
				}
			}
		}
		return true
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyDeployerBundlePush verify that bundle push was pushed on all search heads
func VerifyDeployerBundlePush(deployment *Deployment, testenvInstance *TestEnv, ns string, replicas int) {
	gomega.Eventually(func() bool {
		deployerAppPushStatus := DeployerBundlePushstatus(deployment, ns)
		if len(deployerAppPushStatus) == 0 {
			testenvInstance.Log.Info("Bundle push not complete on all pods")
			return false
		}
		for appName, val := range deployerAppPushStatus {
			if val < replicas {
				testenvInstance.Log.Info("Bundle push not complete on all pods for", "AppName", appName)
				return false
			}
		}
		return true
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyNoPodReset verify that no pod reset during App install using phase3 framework
func VerifyNoPodReset(deployment *Deployment, testenvInstance *TestEnv, ns string, podStartTimeMap map[string]time.Time, podToSkip []string) {
	// Get current Age on all splunk pods and compare with previous
	currentSplunkPodAge := GetPodsStartTime(ns)
	for podName, currentpodAge := range currentSplunkPodAge {
		// Only compare if the pod was present in previous pod iteration
		if _, ok := podStartTimeMap[podName]; ok {
			// Check if pod needs to be skipped
			if !CheckStringInSlice(podToSkip, podName) {
				podReset := currentpodAge.Equal(podStartTimeMap[podName])
				gomega.Expect(podReset).To(gomega.Equal(true), "Pod reset was detected. Pod Name %s. Current Pod Start Time %d. Previous Pod Start Time %d", podName, currentpodAge, podStartTimeMap[podName])
			}
		}
	}
}
