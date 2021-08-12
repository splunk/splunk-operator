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
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	gomega "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
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
	} `json:"status"`
}

// StandaloneReady verify Standlone is in ReadyStatus and does not flip-flop
func StandaloneReady(deployment *Deployment, deploymentName string, standalone *enterpriseApi.Standalone, testenvInstance *TestEnv) {
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(deploymentName, standalone)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for standalone STATUS to be ready", "instance", standalone.ObjectMeta.Name, "Phase", standalone.Status.Phase)
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
		testenvInstance.Log.Info("Waiting for search head cluster STATUS to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return shc.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(instanceName, shc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for Deployer STATUS to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.DeployerPhase)
		DumpGetPods(testenvInstance.GetName())
		return shc.Status.DeployerPhase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(instanceName, shc)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for search head cluster STATUS to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
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
		testenvInstance.Log.Info("Waiting for indexer instance's status to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return idc.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() splcommon.Phase {
		_ = deployment.GetInstance(instanceName, idc)
		return idc.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
}

// ClusterMasterReady verify Cluster Master Instance is in ready status
func ClusterMasterReady(deployment *Deployment, testenvInstance *TestEnv) {
	// Ensure that the cluster-master goes to Ready phase
	cm := &enterpriseApi.ClusterMaster{}
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(deployment.GetName(), cm)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for cluster-master instance status to be ready", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		// Test ClusterMaster Phase to see if its ready
		return cm.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(splcommon.PhaseReady))

	// In a steady state, cluster-master should stay in Ready and not flip-flop around
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
			testenvInstance.Log.Info("Waiting for indexer site instance status to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
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
		podName := fmt.Sprintf(ClusterMasterPod, deployment.GetName())
		stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/master/sites?output_mode=json"
		command := []string{"/bin/sh"}
		stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
		if err != nil {
			testenvInstance.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
			return map[string][]string{}
		}
		testenvInstance.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
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

// VerifyRFSFMet verify RF SF is met on cluster masterr
func VerifyRFSFMet(deployment *Deployment, testenvInstance *TestEnv) {
	gomega.Eventually(func() bool {
		rfSfStatus := CheckRFSF(deployment)
		testenvInstance.Log.Info("Verifying RF SF is met", "Status", rfSfStatus)
		return rfSfStatus
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyNoDisconnectedSHPresentOnCM is present on cluster master
func VerifyNoDisconnectedSHPresentOnCM(deployment *Deployment, testenvInstance *TestEnv) {
	gomega.Consistently(func() bool {
		shStatus := CheckSearchHeadRemoved(deployment)
		testenvInstance.Log.Info("Verifying no SH in DISCONNECTED state present on CM", "Status", shStatus)
		return shStatus
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyNoSHCInNamespace verify no SHC is present in namespace
func VerifyNoSHCInNamespace(deployment *Deployment, testenvInstance *TestEnv) {
	gomega.Eventually(func() bool {
		shcStatus := SHCInNamespace(testenvInstance.GetName())
		testenvInstance.Log.Info("Verifying no SHC is present in namespace", "Status", shcStatus)
		return shcStatus
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(false))
}

// LicenseMasterReady verify LM is in ready status and does not flip flop
func LicenseMasterReady(deployment *Deployment, testenvInstance *TestEnv) {
	licenseMaster := &enterpriseApi.LicenseMaster{}

	testenvInstance.Log.Info("Verifying License Master becomes READY")
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(deployment.GetName(), licenseMaster)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for License Master instance status to be ready",
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
		lmConfigured := CheckLicenseMasterConfigured(deployment, podName)
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
			logf.Log.Error(err, "Failed to parse cluster searchheads")
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
		testenvInstance.Log.Info("Waiting for standalone status", "instance", standalone.ObjectMeta.Name, "Expected", phase, " Actual Phase", standalone.Status.Phase)
		DumpGetPods(testenvInstance.GetName())
		return standalone.Status.Phase
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

// VerifyClusterMasterPhase verify phase of cluster master
func VerifyClusterMasterPhase(deployment *Deployment, testenvInstance *TestEnv, phase splcommon.Phase) {
	cm := &enterpriseApi.ClusterMaster{}
	gomega.Eventually(func() splcommon.Phase {
		err := deployment.GetInstance(deployment.GetName(), cm)
		if err != nil {
			return splcommon.PhaseError
		}
		testenvInstance.Log.Info("Waiting for cluster-master Phase", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase, "Expected", phase)
		DumpGetPods(testenvInstance.GetName())
		// Test ClusterMaster Phase to see if its ready
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
		if !strings.Contains(podName, "monitoring-console") {
			for _, appName := range apps {
				status, versionInstalled, err := GetPodAppStatus(deployment, podName, ns, appName, clusterWideInstall)
				logf.Log.Info("App info returned for app", "App-name", appName, "status", status, "versionInstalled", versionInstalled, "error", err)
				gomega.Expect(err).To(gomega.Succeed(), "Unable to get app status on pod ")
				comparison := strings.EqualFold(status, statusCheck)
				//Check the app is installed on specific pods and un-installed on others for cluster-wide install
				var check bool
				if clusterWideInstall {
					if strings.Contains(podName, "-indexer-") || strings.Contains(podName, "-search-head-") {
						check = true
						testenvInstance.Log.Info("App Install Check", "Pod Name", podName, "App Name", appName, "Expected", check, "Found", comparison, "Cluster Install Scope", clusterWideInstall)
						gomega.Expect(comparison).Should(gomega.Equal(check))
					}
				} else {
					// For local install check pods individually
					if strings.Contains(podName, "-indexer-") || strings.Contains(podName, "-search-head-") {
						check = false
					} else {
						check = true
					}
					testenvInstance.Log.Info("App Install Check", "Pod Name", podName, "App Name", appName, "Expected", check, "Found", comparison, "Cluster Install Scope", clusterWideInstall)
					gomega.Expect(comparison).Should(gomega.Equal(check))
				}

				if versionCheck {
					// For clusterwide install do not check for versions on deployer and cluster-master as the apps arent installed there
					if !(clusterWideInstall && (strings.Contains(podName, "-deployer-") || strings.Contains(podName, "-cluster-master-"))) {
						var expectedVersion string
						if checkupdated {
							expectedVersion = AppInfo[appName]["V2"]
						} else {
							expectedVersion = AppInfo[appName]["V1"]
						}
						testenvInstance.Log.Info("Verify app Version", "Pod Name", podName, "App Name", appName, "Expected Version", expectedVersion, "Version Installed", versionInstalled, "Updated", checkupdated)
						gomega.Expect(versionInstalled).Should(gomega.Equal(expectedVersion))
					}
				}
			}
		}
	}
}

// VerifyAppsCopied verify that apps are copied to correct location based on POD. Set checkAppDirectory false to verify app is not copied.
func VerifyAppsCopied(deployment *Deployment, testenvInstance *TestEnv, ns string, pods []string, apps []string, checkAppDirectory bool, clusterWideInstall bool) {
	for _, podName := range pods {
		if !strings.Contains(podName, "monitoring-console") {
			path := "etc/apps"
			//For cluster-wide install the apps are extracted to different locations
			if clusterWideInstall {
				if strings.Contains(podName, "cluster-master") {
					path = "etc/master-apps/"
				} else if strings.Contains(podName, "-deployer-") {
					path = "etc/shcluster/apps"
				} else if strings.Contains(podName, "-indexer-") {
					path = "etc/slave-apps/"
				}
			}
			testenvInstance.Log.Info("Verifying App in Directory", "Directory Name", path, "Pod Name", podName)
			VerifyAppsInFolder(deployment, testenvInstance, ns, podName, apps, path, checkAppDirectory)
		}
	}
}

// VerifyAppsInFolder verify that apps are present in folder. Set checkAppDirectory false to verify app is not copied.
func VerifyAppsInFolder(deployment *Deployment, testenvInstance *TestEnv, ns string, podName string, apps []string, path string, checkAppDirectory bool) {
	gomega.Eventually(func() bool {
		// Useing checkAppDirectory here to get all files in case of negative check.  GetDirsOrFilesInPath  will return files/directory when checkAppDirecotry is FALSE
		appList, err := GetDirsOrFilesInPath(deployment, podName, path, checkAppDirectory)
		gomega.Expect(err).To(gomega.Succeed(), "Unable to get apps on pod", "Pod", podName)
		for _, app := range apps {
			folderName := app + "/"
			found := CheckStringInSlice(appList, folderName)
			logf.Log.Info("Check App in Directory", "Pod Name", podName, "App-name", folderName, "Path", path, "status", found)
			if found != checkAppDirectory {
				return false
			}
		}
		return true
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyAppsDownloadedByInitContainer verify that apps are downloaded by init container
func VerifyAppsDownloadedByInitContainer(deployment *Deployment, testenvInstance *TestEnv, ns string, pods []string, apps []string, path string) {
	for _, podName := range pods {
		appList, err := GetDirsOrFilesInPath(deployment, podName, path, false)
		gomega.Expect(err).To(gomega.Succeed(), "Unable to get apps on pod", "Pod", podName)
		for _, app := range apps {
			found := CheckStringInSlice(appList, app)
			testenvInstance.Log.Info("Check App directory downloaded by init container", "Pod Name", podName, "App Name", app, "Status", found)
			gomega.Expect(found).Should(gomega.Equal(true))
		}
	}
}
