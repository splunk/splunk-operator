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

	gomega "github.com/onsi/gomega"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// PodDetailsStruct captures output of kubectl get pods podname -o json
type PodDetailsStruct struct {
	Spec struct {
		ServiceAccount     string `json:"serviceAccount"`
		ServiceAccountName string `json:"serviceAccountName"`
	}
}

// StandaloneReady verify Standlone is in ReadyStatus and does not flip-flop
func StandaloneReady(deployment *Deployment, deploymentName string, standalone *enterprisev1.Standalone, testenvInstance *TestEnv) {
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
	shc := &enterprisev1.SearchHeadCluster{}
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

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() splcommon.Phase {
		_ = deployment.GetInstance(deployment.GetName(), shc)
		return shc.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(splcommon.PhaseReady))
}

// SingleSiteIndexersReady verify single site indexers go to ready state
func SingleSiteIndexersReady(deployment *Deployment, testenvInstance *TestEnv) {
	idc := &enterprisev1.IndexerCluster{}
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
	cm := &enterprisev1.ClusterMaster{}
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
		idc := &enterprisev1.IndexerCluster{}
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
		podName := fmt.Sprintf("splunk-%s-cluster-master-0", deployment.GetName())
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
	gomega.Eventually(func() bool {
		shStatus := CheckSearchHeadRemoved(deployment)
		testenvInstance.Log.Info("Verifying no SH in DISCONNECTED state present on CM", "Status", shStatus)
		return shStatus
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
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
	licenseMaster := &enterprisev1.LicenseMaster{}

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
	gomega.Eventually(func() bool {
		lmConfigured := CheckLicenseMasterConfigured(deployment, podName)
		return lmConfigured
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyServiceAccountConfiguredOnPod check if given service account is configured on given pod
func VerifyServiceAccountConfiguredOnPod(deployment *Deployment, ns string, podName string, serviceAccount string) {
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
			logf.Log.Error(err, "Failed to parse cluster searchheads")
			return false
		}
		logf.Log.Info("Service Account on Pod", "FOUND", restResponse.Spec.ServiceAccount, "EXPECTED", serviceAccount)
		return strings.Contains(serviceAccount, restResponse.Spec.ServiceAccount)
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyIndexFoundOnPod verify index found on a given POD
func VerifyIndexFoundOnPod(deployment *Deployment, podName string, indexName string) {
	gomega.Eventually(func() bool {
		indexFound := GetIndexOnPod(deployment, podName, indexName)
		logf.Log.Info("Checking status of index on pod", "PODNAME", podName, "INDEX NAME", indexName, "STATUS", indexFound)
		return indexFound
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyIndexExistsOnS3 Verify Index Exists on S3
func VerifyIndexExistsOnS3(deployment *Deployment, podName string, indexName string) {
	gomega.Eventually(func() bool {
		indexFound := CheckPrefixExistsOnS3(indexName)
		logf.Log.Info("Checking Index on S3", "INDEX NAME", indexName, "STATUS", indexFound)
		return indexFound
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}
