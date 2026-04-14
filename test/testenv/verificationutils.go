// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gomega "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	wait "k8s.io/apimachinery/pkg/util/wait"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

// PodDetailsStruct captures output of kubectl get pods podname -o json
type PodDetailsStruct struct {
	Metadata struct {
		UID string `json:"uid"`
	} `json:"metadata"`

	Spec struct {
		Containers []struct {
			Resources struct {
				Limits struct {
					CPU    string `json:"cpu"`
					Memory string `json:"memory"`
				} `json:"limits"`
				Requests struct {
					CPU    string `json:"cpu"`
					Memory string `json:"memory"`
				} `json:"requests"`
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
func (testenv *TestCaseEnv) VerifyMonitoringConsoleReady(ctx context.Context, deployment *Deployment, mcName string, monitoringConsole *enterpriseApi.MonitoringConsole) {
	gomega.Eventually(func() enterpriseApi.Phase {
		err := deployment.GetInstance(ctx, mcName, monitoringConsole)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for Monitoring Console phase to be ready", "instance", monitoringConsole.ObjectMeta.Name, "Phase", monitoringConsole.Status.Phase)
		DumpGetPods(testenv.GetName())

		return monitoringConsole.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, mcName, monitoringConsole)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "monitoring-console")
		return monitoringConsole.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// VerifyStandaloneReady verify Standalone is in ReadyStatus and does not flip-flop
func (testenv *TestCaseEnv) VerifyStandaloneReady(ctx context.Context, deployment *Deployment, deploymentName string, standalone *enterpriseApi.Standalone) {
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForStandalonePhase(ctx, deployment, testenv.GetName(), standalone.Name, enterpriseApi.PhaseReady, deployment.GetTimeout())
	gomega.Expect(err).To(gomega.Succeed(), "Standalone failed to reach Ready phase")

	// Refresh the instance to get latest state
	err = deployment.GetInstance(ctx, standalone.Name, standalone)
	gomega.Expect(err).To(gomega.Succeed())
	testenv.Log.Info("Standalone reached Ready phase", "instance", standalone.ObjectMeta.Name, "Phase", standalone.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, standalone.Name, standalone)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "standalone")
		return standalone.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// VerifySearchHeadClusterReady verify SHC is in READY status and does not flip-flop
func (testenv *TestCaseEnv) VerifySearchHeadClusterReady(ctx context.Context, deployment *Deployment) {
	instanceName := fmt.Sprintf("%s-shc", deployment.GetName())
	// Use optimized watch to wait for Ready phase (checks both Phase and DeployerPhase)
	err := testenv.WatchForSearchHeadClusterPhase(ctx, deployment, testenv.GetName(), instanceName, enterpriseApi.PhaseReady, deployment.GetTimeout())
	gomega.Expect(err).To(gomega.Succeed(), "SearchHeadCluster failed to reach Ready phase")

	// Refresh the instance to get latest state
	shc := &enterpriseApi.SearchHeadCluster{}
	err = deployment.GetInstance(ctx, instanceName, shc)
	gomega.Expect(err).To(gomega.Succeed())
	testenv.Log.Info("SearchHeadCluster reached Ready phase", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase, "DeployerPhase", shc.Status.DeployerPhase)
	DumpGetPods(testenv.GetName())

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, deployment.GetName(), shc)
		testenv.Log.Info("Check for Consistency Search Head Cluster phase to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "-shc-")
		return shc.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// VerifySingleSiteIndexersReady verify single site indexers go to ready state
func (testenv *TestCaseEnv) VerifySingleSiteIndexersReady(ctx context.Context, deployment *Deployment) {
	instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForIndexerClusterPhase(ctx, deployment, testenv.GetName(), instanceName, enterpriseApi.PhaseReady, deployment.GetTimeout())
	gomega.Expect(err).To(gomega.Succeed(), "IndexerCluster failed to reach Ready phase")

	// Refresh the instance to get latest state
	idc := &enterpriseApi.IndexerCluster{}
	err = deployment.GetInstance(ctx, instanceName, idc)
	gomega.Expect(err).To(gomega.Succeed())
	testenv.Log.Info("IndexerCluster reached Ready phase", "instance", instanceName, "Phase", idc.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, instanceName, idc)
		testenv.Log.Info("Check for Consistency indexer instance's phase to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "-idxc-indexer-")
		return idc.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// IngestorsReady verify ingestors go to ready state
func (testenv *TestCaseEnv) VerifyIngestorReady(ctx context.Context, deployment *Deployment) {
	instanceName := fmt.Sprintf("%s-ingest", deployment.GetName())
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForIngestorClusterPhase(ctx, deployment, testenv.GetName(), instanceName, enterpriseApi.PhaseReady, deployment.GetTimeout())
	gomega.Expect(err).To(gomega.Succeed(), "IngestorCluster failed to reach Ready phase")

	// Refresh the instance to get latest state
	ingest := &enterpriseApi.IngestorCluster{}
	err = deployment.GetInstance(ctx, instanceName, ingest)
	gomega.Expect(err).To(gomega.Succeed())
	testenv.Log.Info("IngestorCluster reached Ready phase", "instance", instanceName, "phase", ingest.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, instanceName, ingest)

		testenv.Log.Info("Check for Consistency ingestor instance's phase to be ready", "instance", instanceName, "phase", ingest.Status.Phase)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "-ingest-")

		return ingest.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// VerifyClusterManagerReady verify Cluster Manager Instance is in ready status
func (testenv *TestCaseEnv) VerifyClusterManagerReady(ctx context.Context, deployment *Deployment) {
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForClusterManagerPhase(ctx, deployment, testenv.GetName(), deployment.GetName(), enterpriseApi.PhaseReady, deployment.GetTimeout())
	gomega.Expect(err).To(gomega.Succeed(), "ClusterManager failed to reach Ready phase")

	// Refresh the instance to get latest state
	cm := &enterpriseApi.ClusterManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	gomega.Expect(err).To(gomega.Succeed())
	testenv.Log.Info("ClusterManager reached Ready phase", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, cluster-manager should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, deployment.GetName(), cm)
		testenv.Log.Info("Check for Consistency "+splcommon.ClusterManager+" phase to be ready", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "cluster-manager")
		testenv.Log.Info("Check for Consistency cluster-manager phase to be ready", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
		return cm.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// VerifyClusterMasterReady verify Cluster Master Instance is in ready status
func (testenv *TestCaseEnv) VerifyClusterMasterReady(ctx context.Context, deployment *Deployment) {
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForClusterMasterPhase(ctx, deployment, testenv.GetName(), deployment.GetName(), enterpriseApi.PhaseReady, deployment.GetTimeout())
	gomega.Expect(err).To(gomega.Succeed(), "ClusterMaster failed to reach Ready phase")

	// Refresh the instance to get latest state
	cm := &enterpriseApiV3.ClusterMaster{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	gomega.Expect(err).To(gomega.Succeed())
	testenv.Log.Info("ClusterMaster reached Ready phase", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, cluster-master should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, deployment.GetName(), cm)
		testenv.Log.Info("Check for Consistency cluster-master phase to be ready", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
		return cm.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// VerifyIndexersReady verify indexers of all sites go to ready state
func (testenv *TestCaseEnv) VerifyIndexersReady(ctx context.Context, deployment *Deployment, siteCount int) {
	siteIndexerMap := map[string][]string{}
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-indexer-0", instanceName)}
		// Ensure indexers go to Ready phase
		idc := &enterpriseApi.IndexerCluster{}
		gomega.Eventually(func() enterpriseApi.Phase {
			err := deployment.GetInstance(ctx, instanceName, idc)
			if err != nil {
				return enterpriseApi.PhaseError
			}
			testenv.Log.Info("Waiting for indexer site instance phase to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
			DumpGetPods(testenv.GetName())

			return idc.Status.Phase
		}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))

		// In a steady state, we should stay in Ready and not flip-flop around
		gomega.Consistently(func() enterpriseApi.Phase {
			_ = deployment.GetInstance(ctx, instanceName, idc)
			testenv.Log.Info("Check for Consistency indexer site instance phase to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
			DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "-idxc-indexer-")
			return idc.Status.Phase
		}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
	}
}

// VerifyIndexerClusterMultisiteStatus verify indexer Cluster is configured as multisite
func (testenv *TestCaseEnv) VerifyIndexerClusterMultisiteStatus(ctx context.Context, deployment *Deployment, siteCount int) {
	siteIndexerMap := map[string][]string{}
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-indexer-0", instanceName)}
	}
	gomega.Eventually(func() map[string][]string {
		var podName string
		if strings.Contains(deployment.name, "master") {
			podName = fmt.Sprintf(ClusterMasterPod, deployment.GetName())
		} else {
			podName = fmt.Sprintf(ClusterManagerPod, deployment.GetName())
		}
		stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/manager/sites?output_mode=json"
		command := []string{"/bin/sh"}
		stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
		if err != nil {
			testenv.Log.Error(err, "Failed to execute command", "on pod", podName, "command", command)
			return map[string][]string{}
		}
		testenv.Log.Info("Command executed", "on pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
		siteIndexerResponse := ClusterManagerSitesResponse{}
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
func (testenv *TestCaseEnv) VerifyRFSFMet(ctx context.Context, deployment *Deployment) {
	gomega.Eventually(func() bool {
		rfSfStatus := CheckRFSF(ctx, deployment)
		testenv.Log.Info("Verifying RF SF is met", "Status", rfSfStatus)
		return rfSfStatus
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(true))
}

// VerifyNoDisconnectedSHPresentOnCM is present on cluster manager
func (testenv *TestCaseEnv) VerifyNoDisconnectedSHPresentOnCM(ctx context.Context, deployment *Deployment) {
	gomega.Consistently(func() bool {
		shStatus := CheckSearchHeadRemoved(ctx, deployment)
		testenv.Log.Info("Verifying no Search Head in DISCONNECTED state present on Cluster Manager", "Status", shStatus)
		return shStatus
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyNoSHCInNamespace verify no SHC is present in namespace
func (testenv *TestCaseEnv) VerifyNoSHCInNamespace(deployment *Deployment) {
	gomega.Eventually(func() bool {
		shcStatus := SHCInNamespace(testenv.GetName())
		testenv.Log.Info("Verifying no Search Head Cluster is present in namespace", "Status", shcStatus)
		return shcStatus
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(false))
}

// VerifyLicenseManagerReady verify LM is in ready status and does not flip flop
func (testenv *TestCaseEnv) VerifyLicenseManagerReady(ctx context.Context, deployment *Deployment) {
	LicenseManager := &enterpriseApi.LicenseManager{}

	testenv.Log.Info("Verifying License Manager becomes READY")
	gomega.Eventually(func() enterpriseApi.Phase {
		err := deployment.GetInstance(ctx, deployment.GetName(), LicenseManager)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for License Manager instance status to be ready",
			"instance", LicenseManager.ObjectMeta.Name, "Phase", LicenseManager.Status.Phase)
		DumpGetPods(testenv.GetName())

		return LicenseManager.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, deployment.GetName(), LicenseManager)
		return LicenseManager.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// VerifyLicenseMasterReady verify LM is in ready status and does not flip flop
func (testenv *TestCaseEnv) VerifyLicenseMasterReady(ctx context.Context, deployment *Deployment) {
	LicenseMaster := &enterpriseApiV3.LicenseMaster{}

	testenv.Log.Info("Verifying License Master becomes READY")
	gomega.Eventually(func() enterpriseApi.Phase {
		err := deployment.GetInstance(ctx, deployment.GetName(), LicenseMaster)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for License Master instance status to be ready",
			"instance", LicenseMaster.ObjectMeta.Name, "Phase", LicenseMaster.Status.Phase)
		DumpGetPods(testenv.GetName())

		return LicenseMaster.Status.Phase
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))

	// In a steady state, we should stay in Ready and not flip-flop around
	gomega.Consistently(func() enterpriseApi.Phase {
		_ = deployment.GetInstance(ctx, deployment.GetName(), LicenseMaster)
		return LicenseMaster.Status.Phase
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(enterpriseApi.PhaseReady))
}

// VerifyLMConfiguredOnPod verify LM is configured on given POD
func VerifyLMConfiguredOnPod(ctx context.Context, deployment *Deployment, podName string) {
	gomega.Consistently(func() bool {
		lmConfigured := CheckLicenseManagerConfigured(ctx, deployment, podName)
		return lmConfigured
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyServiceAccountConfiguredOnPod check if given service account is configured on given pod
func (testenv *TestCaseEnv) VerifyServiceAccountConfiguredOnPod(deployment *Deployment, ns string, podName string, serviceAccount string) {
	gomega.Consistently(func() bool {
		output, err := exec.Command("kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
		if err != nil {
			cmd := fmt.Sprintf("kubectl get pods -n %s %s -o json", ns, podName)
			testenv.Log.Error(err, "Failed to execute command", "command", cmd)
			return false
		}
		restResponse := PodDetailsStruct{}
		err = json.Unmarshal([]byte(output), &restResponse)
		if err != nil {
			testenv.Log.Error(err, "Failed to parse cluster Search heads")
			return false
		}
		testenv.Log.Info("Service Account on Pod", "found", restResponse.Spec.ServiceAccount, "expected", serviceAccount)
		return strings.Contains(serviceAccount, restResponse.Spec.ServiceAccount)
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyIndexFoundOnPod verify index found on a given POD
func (testenv *TestCaseEnv) VerifyIndexFoundOnPod(ctx context.Context, deployment *Deployment, podName string, indexName string) {
	gomega.Eventually(func() bool {
		indexFound, _ := GetIndexOnPod(ctx, deployment, podName, indexName)
		testenv.Log.Info("Checking status of index on pod", "podName", podName, "indexName", indexName, "status", indexFound)
		return indexFound
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyIndexConfigsMatch verify index specific config
func (testenv *TestCaseEnv) VerifyIndexConfigsMatch(ctx context.Context, deployment *Deployment, podName string, indexName string, maxGlobalDataSizeMB int, maxGlobalRawDataSizeMB int) {
	gomega.Consistently(func() bool {
		indexFound, data := GetIndexOnPod(ctx, deployment, podName, indexName)
		testenv.Log.Info("Checking status of index on pod", "podName", podName, "indexName", indexName, "status", indexFound)
		if indexFound {
			if data.Content.MaxGlobalDataSizeMB == maxGlobalDataSizeMB && data.Content.MaxGlobalRawDataSizeMB == maxGlobalRawDataSizeMB {
				testenv.Log.Info("Checking index configs", "maxGlobalDataSizeMB", data.Content.MaxGlobalDataSizeMB, "maxGlobalRawDataSizeMB", data.Content.MaxGlobalRawDataSizeMB)
				return true
			}
		}
		return false
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifyIndexExistsOnS3 Verify Index Exists on S3
func (testenv *TestCaseEnv) VerifyIndexExistsOnS3(ctx context.Context, deployment *Deployment, indexName string, podName string) {
	gomega.Eventually(func() bool {
		indexFound := CheckPrefixExistsOnS3(indexName)
		testenv.Log.Info("Checking Index on S3", "indexName", indexName, "status", indexFound)
		// During testing found some false failure. Rolling index buckets again to ensure data is pushed to remote storage
		if !indexFound {
			testenv.Log.Info("Index NOT found. Rolling buckets again", "indexName", indexName)
			RollHotToWarm(ctx, deployment, podName, indexName)
		}
		return indexFound
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyRollingRestartFinished verify no rolling restart is active
func (testenv *TestCaseEnv) VerifyRollingRestartFinished(ctx context.Context, deployment *Deployment) {
	gomega.Eventually(func() bool {
		rollingRestartStatus := CheckRollingRestartStatus(ctx, deployment)
		testenv.Log.Info("Rolling Restart Status", "active", rollingRestartStatus)
		return rollingRestartStatus
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(true))
}

// VerifyConfOnPod Verify give conf and value on config file on pod
func (testenv *TestCaseEnv) VerifyConfOnPod(deployment *Deployment, podName string, confFilePath string, config string, value string) {
	gomega.Consistently(func() bool {
		confLine, err := GetConfLineFromPod(podName, confFilePath, testenv.GetName(), config, "", false)
		if err != nil {
			testenv.Log.Error(err, "Failed to get config on pod")
			return false
		}
		if strings.Contains(confLine, config) && strings.Contains(confLine, value) {
			testenv.Log.Info("Config found", "config", config, "value", value, "confLine", confLine)
			return true
		}
		testenv.Log.Info("Config NOT found")
		return false
	}, ConsistentDuration, ConsistentPollInterval).Should(gomega.Equal(true))
}

// VerifySearchHeadClusterPhase verify the phase of SHC matches given phase
func (testenv *TestCaseEnv) VerifySearchHeadClusterPhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase) {
	gomega.Eventually(func() enterpriseApi.Phase {
		shc := &enterpriseApi.SearchHeadCluster{}
		shcName := deployment.GetName() + "-shc"
		err := deployment.GetInstance(ctx, shcName, shc)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for Search Head Cluster Phase", "instance", shc.ObjectMeta.Name, "Expected", phase, "Phase", shc.Status.Phase)
		DumpGetPods(testenv.GetName())

		return shc.Status.Phase
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(enterpriseApi.PhaseScalingUp))
}

// VerifyIndexerClusterPhase verify the phase of idxc matches the given phase
func (testenv *TestCaseEnv) VerifyIndexerClusterPhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase, idxcName string) {
	gomega.Eventually(func() enterpriseApi.Phase {
		idxc := &enterpriseApi.IndexerCluster{}
		err := deployment.GetInstance(ctx, idxcName, idxc)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for Indexer Cluster Phase", "instance", idxc.ObjectMeta.Name, "Expected", phase, "Phase", idxc.Status.Phase)
		DumpGetPods(testenv.GetName())

		return idxc.Status.Phase
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(phase))
}

// VerifyStandalonePhase verify the phase of Standalone CR
func (testenv *TestCaseEnv) VerifyStandalonePhase(ctx context.Context, deployment *Deployment, crName string, phase enterpriseApi.Phase) {
	gomega.Eventually(func() enterpriseApi.Phase {
		standalone := &enterpriseApi.Standalone{}
		err := deployment.GetInstance(ctx, deployment.GetName(), standalone)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for Standalone status", "instance", standalone.ObjectMeta.Name, "Expected", phase, " Actual Phase", standalone.Status.Phase)
		DumpGetPods(testenv.GetName())

		return standalone.Status.Phase
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(phase))
}

// VerifyMonitoringConsolePhase verify the phase of Monitoring Console CR
func (testenv *TestCaseEnv) VerifyMonitoringConsolePhase(ctx context.Context, deployment *Deployment, crName string, phase enterpriseApi.Phase) {
	gomega.Eventually(func() enterpriseApi.Phase {
		mc := &enterpriseApi.MonitoringConsole{}
		err := deployment.GetInstance(ctx, crName, mc)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for Monitoring Console CR status", "instance", mc.ObjectMeta.Name, "Expected", phase, " Actual Phase", mc.Status.Phase)
		DumpGetPods(testenv.GetName())

		return mc.Status.Phase
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(phase))
}

// GetResourceVersion get resource version id
func (testenv *TestCaseEnv) GetResourceVersion(ctx context.Context, deployment *Deployment, instance interface{}) string {
	var newResourceVersion string
	var err error

	switch cr := instance.(type) {
	case *enterpriseApi.Standalone:
		err = deployment.GetInstance(ctx, cr.Name, cr)
		newResourceVersion = cr.ResourceVersion
	case *enterpriseApi.LicenseManager:
		err = deployment.GetInstance(ctx, cr.Name, cr)
		newResourceVersion = cr.ResourceVersion
	case *enterpriseApi.IndexerCluster:
		err = deployment.GetInstance(ctx, cr.Name, cr)
		newResourceVersion = cr.ResourceVersion
	case *enterpriseApi.ClusterManager:
		err = deployment.GetInstance(ctx, cr.Name, cr)
		newResourceVersion = cr.ResourceVersion
	case *enterpriseApi.MonitoringConsole:
		err = deployment.GetInstance(ctx, cr.Name, cr)
		newResourceVersion = cr.ResourceVersion
	case *enterpriseApi.SearchHeadCluster:
		err = deployment.GetInstance(ctx, cr.Name, cr)
		newResourceVersion = cr.ResourceVersion
	default:
		return "-1"
	}
	if err != nil {
		return "-1"
	}
	return newResourceVersion
}

// VerifyCustomResourceVersionChanged verify the version id
func (testenv *TestCaseEnv) VerifyCustomResourceVersionChanged(ctx context.Context, deployment *Deployment, instance interface{}, resourceVersion string) {
	var kind string
	var newResourceVersion string
	var name string
	var err error

	gomega.Eventually(func() string {
		switch cr := instance.(type) {
		case *enterpriseApi.Standalone:
			err = deployment.GetInstance(ctx, cr.Name, cr)
			kind = cr.Kind
			newResourceVersion = cr.ResourceVersion
			name = cr.Name
		case *enterpriseApi.LicenseManager:
			err = deployment.GetInstance(ctx, cr.Name, cr)
			kind = cr.Kind
			newResourceVersion = cr.ResourceVersion
			name = cr.Name
		case *enterpriseApiV3.LicenseMaster:
			err = deployment.GetInstance(ctx, cr.Name, cr)
			kind = cr.Kind
			newResourceVersion = cr.ResourceVersion
			name = cr.Name
		case *enterpriseApi.IndexerCluster:
			err = deployment.GetInstance(ctx, cr.Name, cr)
			kind = cr.Kind
			newResourceVersion = cr.ResourceVersion
			name = cr.Name
		case *enterpriseApi.ClusterManager:
			err = deployment.GetInstance(ctx, cr.Name, cr)
			kind = cr.Kind
			newResourceVersion = cr.ResourceVersion
			name = cr.Name
		case *enterpriseApiV3.ClusterMaster:
			err = deployment.GetInstance(ctx, cr.Name, cr)
			kind = cr.Kind
			newResourceVersion = cr.ResourceVersion
			name = cr.Name
		case *enterpriseApi.MonitoringConsole:
			err = deployment.GetInstance(ctx, cr.Name, cr)
			kind = cr.Kind
			newResourceVersion = cr.ResourceVersion
			name = cr.Name
		case *enterpriseApi.SearchHeadCluster:
			err = deployment.GetInstance(ctx, cr.Name, cr)
			newResourceVersion = cr.ResourceVersion
			kind = cr.Kind
			name = cr.Name
		default:
			return "-1"
		}
		if err != nil {
			return "-1"
		}
		testenv.Log.Info("Waiting for ", kind, " CR status", "instance", name, "Not Expected", resourceVersion, " Actual Resource Version", newResourceVersion)
		DumpGetPods(testenv.GetName())

		return newResourceVersion
	}, deployment.GetTimeout(), ShortPollInterval).ShouldNot(gomega.Equal(resourceVersion))
}

// VerifyCPULimits verifies value of CPU limits is as expected
func (testenv *TestCaseEnv) VerifyCPULimits(deployment *Deployment, podName string, expectedCPULimits string) {
	gomega.Eventually(func() bool {
		ns := testenv.GetName()
		output, err := exec.Command("kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
		if err != nil {
			cmd := fmt.Sprintf("kubectl get pods -n %s %s -o json", ns, podName)
			testenv.Log.Error(err, "Failed to execute command", "command", cmd)
			return false
		}
		restResponse := PodDetailsStruct{}
		err = json.Unmarshal([]byte(output), &restResponse)
		if err != nil {
			testenv.Log.Error(err, "Failed to parse JSON")
			return false
		}
		result := false

		for i := 0; i < len(restResponse.Spec.Containers); i++ {
			if strings.Contains(restResponse.Spec.Containers[0].Resources.Limits.CPU, expectedCPULimits) {
				result = true
				testenv.Log.Info("Verifying CPU limits: ", "pod", podName, "found", restResponse.Spec.Containers[0].Resources.Limits.CPU, "expected", expectedCPULimits)
			}
		}
		return result
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyResourceConstraints verifies value of CPU limits is as expected
func (testenv *TestCaseEnv) VerifyResourceConstraints(deployment *Deployment, podName string, res corev1.ResourceRequirements) {
	gomega.Eventually(func() bool {
		ns := testenv.GetName()
		output, err := exec.Command("kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
		if err != nil {
			cmd := fmt.Sprintf("kubectl get pods -n %s %s -o json", ns, podName)
			testenv.Log.Error(err, "Failed to execute command", "command", cmd)
			return false
		}
		restResponse := PodDetailsStruct{}
		err = json.Unmarshal([]byte(output), &restResponse)
		if err != nil {
			testenv.Log.Error(err, "Failed to parse JSON")
			return false
		}
		result := false

		for i := 0; i < len(restResponse.Spec.Containers); i++ {
			if strings.Contains(restResponse.Spec.Containers[i].Resources.Limits.CPU, res.Limits.Cpu().String()) {
				result = true
				testenv.Log.Info("Verifying CPU limits: ", "pod", podName, "found", restResponse.Spec.Containers[0].Resources.Limits.CPU, "expected", res.Limits.Cpu().String())
			}

			if strings.Contains(restResponse.Spec.Containers[i].Resources.Limits.Memory, res.Limits.Memory().String()) {
				result = true
				testenv.Log.Info("Verifying Memory limits: ", "pod", podName, "found", restResponse.Spec.Containers[i].Resources.Limits.Memory, "expected", res.Limits.Memory().String())
			}

			if strings.Contains(restResponse.Spec.Containers[i].Resources.Requests.CPU, res.Requests.Cpu().String()) {
				result = true
				testenv.Log.Info("Verifying CPU limits: ", "pod", podName, "found", restResponse.Spec.Containers[i].Resources.Requests.CPU, "expected", res.Requests.Cpu().String())
			}

			if strings.Contains(restResponse.Spec.Containers[i].Resources.Requests.Memory, res.Requests.Memory().String()) {
				result = true
				testenv.Log.Info("Verifying CPU limits: ", "pod", podName, "found", restResponse.Spec.Containers[i].Resources.Requests.Memory, "expected", res.Requests.Memory().String())
			}
		}
		return result
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyClusterManagerPhase verify phase of cluster manager
func (testenv *TestCaseEnv) VerifyClusterManagerPhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase) {
	cm := &enterpriseApi.ClusterManager{}
	gomega.Eventually(func() enterpriseApi.Phase {
		err := deployment.GetInstance(ctx, deployment.GetName(), cm)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for cluster-manager Phase", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase, "Expected", phase)
		DumpGetPods(testenv.GetName())

		// Test ClusterManager Phase to see if its ready
		return cm.Status.Phase
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(phase))
}

// VerifyClusterMasterPhase verify phase of cluster manager
func (testenv *TestCaseEnv) VerifyClusterMasterPhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase) {
	cm := &enterpriseApiV3.ClusterMaster{}
	gomega.Eventually(func() enterpriseApi.Phase {
		err := deployment.GetInstance(ctx, deployment.GetName(), cm)
		if err != nil {
			return enterpriseApi.PhaseError
		}
		testenv.Log.Info("Waiting for cluster-manager Phase", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase, "Expected", phase)
		DumpGetPods(testenv.GetName())

		// Test ClusterManager Phase to see if its ready
		return cm.Status.Phase
	}, deployment.GetTimeout(), ShortPollInterval).Should(gomega.Equal(phase))
}

// VerifySecretsOnPods Check whether the secret object info is mounted on given pods
// Set match to true or false to indicate desired +ve or -ve match
func (testenv *TestCaseEnv) VerifySecretsOnPods(ctx context.Context, deployment *Deployment, verificationPods []string, data map[string][]byte, match bool) {
	for _, pod := range verificationPods {
		for secretKey, secretValue := range data {
			found := false
			currentValue := GetMountedKey(ctx, deployment, pod, secretKey)
			comparsion := bytes.Compare([]byte(currentValue), secretValue)
			if comparsion == 0 {
				found = true
				testenv.Log.Info("Secret Values on POD Match", "Match Expected", match, "Pod Name ", pod, "Secret Key", secretKey, "Given Value of Key", string(secretValue), "Key Value found", currentValue)
			} else {
				testenv.Log.Info("Secret Values on POD DONOT Match", "Match Expected", match, "Pod Name ", pod, "Secret Key", secretKey, "Given Value of Key", string(secretValue), "Key Value found", currentValue)
			}
			gomega.Expect(found).Should(gomega.Equal(match))
		}
	}
}

// VerifySecretsOnSecretObjects Compare secret value on passed in map to value present on secret object.
// Set match to true or false to indicate desired +ve or -ve match
func (testenv *TestCaseEnv) VerifySecretsOnSecretObjects(ctx context.Context, deployment *Deployment, secretObjectNames []string, data map[string][]byte, match bool) {
	for _, secretName := range secretObjectNames {
		currentSecretData, err := GetSecretStruct(ctx, deployment, testenv.GetName(), secretName)
		gomega.Expect(err).To(gomega.Succeed(), "Unable to get secret struct")
		for secretKey, secretValue := range data {
			found := false
			secretValueOnSecretObject := currentSecretData.Data[secretKey]
			comparsion := bytes.Compare(secretValueOnSecretObject, secretValue)
			if comparsion == 0 {
				testenv.Log.Info("Secret Values on Secret Object Match", "Match Expected", match, "Secret Object Name", secretName, "Secret Key", secretKey, "Given Value of Key", string(secretValue), "Key Value found", string(secretValueOnSecretObject))
				found = true
			} else {
				testenv.Log.Info("Secret Values on Secret Object DONOT match", "Match Expected", match, "Secret Object Name", secretName, "Secret Key", secretKey, "Given Value of Key", string(secretValue), "Key Value found", string(secretValueOnSecretObject))
			}
			gomega.Expect(found).Should(gomega.Equal(match))
		}
	}
}

// VerifySplunkServerConfSecrets Compare secret value on passed in map to value present on server.conf for given pods and secrets
// Set match to true or false to indicate desired +ve or -ve match
func (testenv *TestCaseEnv) VerifySplunkServerConfSecrets(ctx context.Context, deployment *Deployment, verificationPods []string, data map[string][]byte, match bool) {
	for _, podName := range verificationPods {
		keysToMatch := GetKeysToMatch(podName)
		testenv.Log.Info("Verificaton Keys Set", "Pod Name", podName, "Keys To Compare", keysToMatch)
		for _, secretName := range keysToMatch {
			found := false
			stanza := SecretKeytoServerConfStanza[secretName]
			_, value, err := GetSecretFromServerConf(ctx, deployment, podName, testenv.GetName(), "pass4SymmKey", stanza)
			gomega.Expect(err).To(gomega.Succeed(), "Secret not found in conf file", "Secret Name", secretName)
			comparsion := strings.Compare(value, string(data[secretName]))
			if comparsion == 0 {
				testenv.Log.Info("Secret Values on server.conf Match", "Match Expected", match, "Pod Name", podName, "Secret Key", secretName, "Given Value of Key", string(data[secretName]), "Key Value found", value)
				found = true
			} else {
				testenv.Log.Info("Secret Values on server.conf DONOT MATCH", "Match Expected", match, "Pod Name", podName, "Secret Key", secretName, "Given Value of Key", string(data[secretName]), "Key Value found", value)
			}
			gomega.Expect(found).Should(gomega.Equal(match))
		}
	}
}

// VerifySplunkInputConfSecrets Compare secret value on passed in map to value present on input.conf for given indexer or standalone pods
// Set match to true or false to indicate desired +ve or -ve match
func (testenv *TestCaseEnv) VerifySplunkInputConfSecrets(deployment *Deployment, verificationPods []string, data map[string][]byte, match bool) {
	secretName := "hec_token"
	for _, podName := range verificationPods {
		if strings.Contains(podName, "standalone") || strings.Contains(podName, "indexer") {
			found := false
			testenv.Log.Info("Key Verificaton", "Pod Name", podName, "Key", secretName)
			stanza := SecretKeytoServerConfStanza[secretName]
			_, value, err := GetSecretFromInputsConf(deployment, podName, testenv.GetName(), "token", stanza)
			gomega.Expect(err).To(gomega.Succeed(), "Secret not found in conf file", "Secret Name", secretName)
			comparsion := strings.Compare(value, string(data[secretName]))
			if comparsion == 0 {
				testenv.Log.Info("Secret Values on input.conf Match", "Match Expected", match, "Pod Name", podName, "Secret Key", secretName, "Given Value of Key", string(data[secretName]), "Key Value found", value)
				found = true
			} else {
				testenv.Log.Info("Secret Values on input.conf DONOT MATCH", "Match Expected", match, "Pod Name", podName, "Secret Key", secretName, "Given Value of Key", string(data[secretName]), "Key Value found", value)
			}
			gomega.Expect(found).Should(gomega.Equal(match))
		}
	}
}

// VerifySplunkSecretViaAPI check if keys can be used to access api i.e validate they are authentic
func (testenv *TestCaseEnv) VerifySplunkSecretViaAPI(ctx context.Context, deployment *Deployment, verificationPods []string, data map[string][]byte, match bool) {
	var keysToMatch []string
	for _, podName := range verificationPods {
		if strings.Contains(podName, "standalone") || strings.Contains(podName, "indexer") {
			keysToMatch = []string{"password", "hec_token"}
		} else {
			keysToMatch = []string{"password"}
		}
		for _, secretName := range keysToMatch {
			validKey := false
			testenv.Log.Info("Key Verificaton", "Pod Name", podName, "Key", secretName)
			validKey = CheckSecretViaAPI(ctx, deployment, podName, secretName, string(data[secretName]))
			gomega.Expect(validKey).Should(gomega.Equal(match))
		}
	}
}

// VerifyPVC verifies if PVC exists or not
func (testenv *TestCaseEnv) VerifyPVC(deployment *Deployment, ns string, pvcName string, expectedToExist bool, verificationTimeout time.Duration) {
	gomega.Eventually(func() bool {
		pvcExists := false
		pvcsList := DumpGetPvcs(testenv.GetName())

		for i := 0; i < len(pvcsList); i++ {
			if strings.EqualFold(pvcsList[i], pvcName) {
				pvcExists = true
				break
			}
		}
		testenv.Log.Info("PVC Status Verified", "PVC", pvcName, "STATUS", pvcExists, "EXPECTED", expectedToExist)
		return pvcExists
	}, verificationTimeout, PollInterval).Should(gomega.Equal(expectedToExist))
}

// VerifyPVCsPerDeployment verifies for a given deployment if PVCs (etc and var) exists
func (testenv *TestCaseEnv) VerifyPVCsPerDeployment(deployment *Deployment, deploymentType string, instances int, expectedtoExist bool, verificationTimeout time.Duration) {
	pvcKind := []string{"etc", "var"}
	for i := 0; i < instances; i++ {
		for _, pvcVolumeKind := range pvcKind {
			PvcName := fmt.Sprintf(PVCString, pvcVolumeKind, deployment.GetName(), deploymentType, i)
			testenv.VerifyPVC(deployment, testenv.GetName(), PvcName, expectedtoExist, verificationTimeout)
		}
	}
}

// VerifyAppInstalled verify that app of specific version is installed. Method assumes that app is installed in all CR's in namespace
func (testenv *TestCaseEnv) VerifyAppInstalled(ctx context.Context, deployment *Deployment, ns string, pods []string, apps []string, versionCheck bool, statusCheck string, checkupdated bool, clusterWideInstall bool) {
	// Fail-fast test: check first pod and first app before checking all pods
	if len(pods) > 0 && len(apps) > 0 {
		testenv.Log.Info("Running fail-fast test on first pod before checking all pods", "pod", pods[0], "app", apps[0])
		firstPod := pods[0]
		firstApp := apps[0]

		status, versionInstalled, err := GetPodAppStatus(ctx, deployment, firstPod, ns, firstApp, clusterWideInstall)
		if err != nil {
			gomega.Expect(err).To(gomega.Succeed(), fmt.Sprintf("Test failed - app %s not accessible on pod %s. This indicates a fundamental issue. Skipping remaining checks.", firstApp, firstPod))
		}
		testenv.Log.Info("Test passed - app is accessible", "pod", firstPod, "app", firstApp, "status", status, "version", versionInstalled)
		testenv.Log.Info("Proceeding with full verification of all pods and apps")
	}

	for _, podName := range pods {
		for _, appName := range apps {
			status, versionInstalled, err := GetPodAppStatus(ctx, deployment, podName, ns, appName, clusterWideInstall)
			testenv.Log.Info("App details", "app", appName, "status", status, "version", versionInstalled, "error", err)
			gomega.Expect(err).To(gomega.Succeed(), "Unable to get app status on pod ")
			comparison := strings.EqualFold(status, statusCheck)
			//Check the app is installed on specific pods and un-installed on others for cluster-wide install
			var check bool
			if clusterWideInstall {
				if strings.Contains(podName, "-indexer-") || strings.Contains(podName, "-search-head-") {
					check = true
					testenv.Log.Info("App Install Check", "pod", podName, "app", appName, "expected", check, "found", comparison, "scope:cluster", clusterWideInstall)
					gomega.Expect(comparison).Should(gomega.Equal(check))
				}
			} else {
				// For local install check pods individually
				if strings.Contains(podName, "-indexer-") || strings.Contains(podName, "-search-head-") {
					check = false
				} else {
					check = true
				}
				testenv.Log.Info("App Install Check", "pod", podName, "app", appName, "expected", check, "found", comparison, "scope:cluster", clusterWideInstall)
				gomega.Expect(comparison).Should(gomega.Equal(check))
			}

			if versionCheck {
				// For clusterwide install do not check for versions on deployer and cluster-manager as the apps arent installed there
				if !(clusterWideInstall && (strings.Contains(podName, "-deployer-") || strings.Contains(podName, "-cluster-manager-") || strings.Contains(podName, splcommon.TestClusterManagerDashed))) {
					var expectedVersion string
					if checkupdated {
						expectedVersion = AppInfo[appName]["V2"]
					} else {
						expectedVersion = AppInfo[appName]["V1"]
					}
					testenv.Log.Info("Verify app", "pod", podName, "app", appName, "expectedVersion", expectedVersion, "versionInstalled", versionInstalled, "updated", checkupdated)
					gomega.Expect(versionInstalled).Should(gomega.Equal(expectedVersion))
				}
			}
		}
	}
}

// VerifyAppsCopied verify that apps are copied to correct location based on POD. Set checkAppDirectory false to verify app is not copied.
func (testenv *TestCaseEnv) VerifyAppsCopied(ctx context.Context, deployment *Deployment, ns string, pods []string, apps []string, checkAppDirectory bool, scope string) {

	for _, podName := range pods {
		path := "etc/apps"
		//For cluster-wide install the apps are extracted to different locations
		if scope == enterpriseApi.ScopeCluster {
			if strings.Contains(podName, "cluster-manager") || strings.Contains(podName, splcommon.ClusterManager) {
				path = splcommon.ManagerAppsLoc
			} else if strings.Contains(podName, "-deployer-") {
				path = splcommon.SHClusterAppsLoc
			} else if strings.Contains(podName, "-indexer-") {
				path = splcommon.PeerAppsLoc
			}
		}
		testenv.VerifyAppsInFolder(ctx, deployment, ns, podName, apps, path, checkAppDirectory)
	}
}

// VerifyAppsInFolder verify that apps are present in folder. Set checkAppDirectory false to verify app is not copied.
func (testenv *TestCaseEnv) VerifyAppsInFolder(ctx context.Context, deployment *Deployment, ns string, podName string, apps []string, path string, checkAppDirectory bool) {
	gomega.Eventually(func() bool {
		// Using checkAppDirectory here to get all files in case of negative check.  GetDirsOrFilesInPath  will return files/directory when checkAppDirecotry is FALSE
		appList, err := GetDirsOrFilesInPath(ctx, deployment, podName, path, checkAppDirectory)
		gomega.Expect(err).To(gomega.Succeed(), "Unable to get apps on pod", "Pod", podName)
		for _, app := range apps {
			folderName := app + "/"
			found := CheckStringInSlice(appList, folderName)
			testenv.Log.Info("App check", "pod", podName, "folderName", folderName, "path", path, "status", found)
			if found != checkAppDirectory {
				return false
			}
		}
		return true
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyAppsDownloadedOnContainer verify that apps are downloaded by init container
func (testenv *TestCaseEnv) VerifyAppsDownloadedOnContainer(ctx context.Context, deployment *Deployment, ns string, pods []string, apps []string, path string) {

	for _, podName := range pods {
		appList, err := GetDirsOrFilesInPath(ctx, deployment, podName, path, false)
		gomega.Expect(err).To(gomega.Succeed(), "Unable to get apps on pod", "Pod", podName)
		for _, app := range apps {
			found := CheckStringInSlice(appList, app)
			testenv.Log.Info("Check App files present on the pod", "Pod Name", podName, "App Name", app, "directory", path, "Status", found)
			gomega.Expect(found).Should(gomega.Equal(true))
		}
	}
}

// VerifyAppsPackageDeletedOnOperatorContainer verify that apps are deleted by container
func (testenv *TestCaseEnv) VerifyAppsPackageDeletedOnOperatorContainer(ctx context.Context, deployment *Deployment, ns string, pods []string, apps []string, path string) {
	for _, podName := range pods {
		for _, app := range apps {
			gomega.Eventually(func() bool {
				appList, err := GetOperatorDirsOrFilesInPath(ctx, deployment, podName, path, false)
				if err != nil {
					testenv.Log.Error(err, "Unable to get apps on operator pod", "Pod", podName)
					return true
				}
				found := CheckStringInSlice(appList, app+"_")
				testenv.Log.Info(fmt.Sprintf("Check App package deleted on the pod %s. App Name %s. Directory %s, Status %t", podName, app, path, found))
				return found
			}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(false))
		}
	}
}

// VerifyAppsPackageDeletedOnContainer verify that apps are deleted by container
func (testenv *TestCaseEnv) VerifyAppsPackageDeletedOnContainer(ctx context.Context, deployment *Deployment, ns string, pods []string, apps []string, path string) {
	for _, podName := range pods {
		for _, app := range apps {
			gomega.Eventually(func() bool {
				appList, err := GetDirsOrFilesInPath(ctx, deployment, podName, path, false)
				if err != nil {
					testenv.Log.Error(err, "Unable to get apps on pod", "Pod", podName)
					return true
				}
				found := CheckStringInSlice(appList, app+"_")
				testenv.Log.Info(fmt.Sprintf("Check App package deleted on the pod %s. App Name %s. Directory %s, Status %t", podName, app, path, found))
				return found
			}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(false))
		}
	}
}

// VerifyAppListPhase verify given app Phase has completed for the given list of apps for given CR Kind
func (testenv *TestCaseEnv) VerifyAppListPhase(ctx context.Context, deployment *Deployment, name string, crKind string, appSourceName string, phase enterpriseApi.AppPhaseType, appList []string) {
	if phase == enterpriseApi.PhaseDownload || phase == enterpriseApi.PhasePodCopy {
		for _, appName := range appList {
			testenv.Log.Info(fmt.Sprintf("Check App Status for CR %s NAME %s APP NAME %s Expected Phase not to be %s", crKind, name, appName, phase))
			gomega.Eventually(func() enterpriseApi.AppPhaseType {
				appDeploymentInfo, err := GetAppDeploymentInfo(ctx, deployment, testenv, name, crKind, appSourceName, appName)
				if err != nil {
					testenv.Log.Error(err, "Failed to get app deployment info")
					return phase // Continue polling
				}
				if appDeploymentInfo.AppName == "" {
					testenv.Log.Info(fmt.Sprintf("App deployment info not found yet for app %s (CR %s/%s, AppSource %s), continuing to poll", appName, crKind, name, appSourceName))
					return phase // Continue polling
				}
				testenv.Log.Info(fmt.Sprintf("App State found for CR %s NAME %s APP NAME %s Expected Phase should not be %s", crKind, name, appName, phase), "Actual Phase", appDeploymentInfo.PhaseInfo.Phase, "App State", appDeploymentInfo)
				return appDeploymentInfo.PhaseInfo.Phase
			}, deployment.GetTimeout(), PollInterval).ShouldNot(gomega.Equal(phase))
		}
	} else {
		for _, appName := range appList {
			testenv.Log.Info(fmt.Sprintf("Check App Status for CR %s NAME %s APP NAME %s Expected Phase %s", crKind, name, appName, phase))
			gomega.Eventually(func() enterpriseApi.AppPhaseType {
				appDeploymentInfo, err := GetAppDeploymentInfo(ctx, deployment, testenv, name, crKind, appSourceName, appName)
				if err != nil {
					testenv.Log.Error(err, "Failed to get app deployment info")
					return enterpriseApi.PhaseDownload // Continue polling
				}
				if appDeploymentInfo.AppName == "" {
					testenv.Log.Info(fmt.Sprintf("App deployment info not found yet for app %s (CR %s/%s, AppSource %s), continuing to poll", appName, crKind, name, appSourceName))
					return enterpriseApi.PhaseDownload // Continue polling
				}
				testenv.Log.Info(fmt.Sprintf("App State found for CR %s NAME %s APP NAME %s Expected Phase %s", crKind, name, appName, phase), "Actual Phase", appDeploymentInfo.PhaseInfo.Phase, "App Phase Status", appDeploymentInfo.PhaseInfo.Status, "App State", appDeploymentInfo)
				if appDeploymentInfo.PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
					testenv.Log.Info("Phase Install Not Complete.", "Phase Found", appDeploymentInfo.PhaseInfo.Phase, "Phase Status Found", appDeploymentInfo.PhaseInfo.Status)
					return enterpriseApi.PhaseDownload
				}
				return appDeploymentInfo.PhaseInfo.Phase
			}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(phase))
		}
	}
}

// VerifyAppState verify given app state is in between states passed as parameters, i.e when Status is between 101 and 303 we would pass enterpriseApi.AppPkgInstallComplete and enterpriseApi.AppPkgPodCopyComplete
func (testenv *TestCaseEnv) VerifyAppState(ctx context.Context, deployment *Deployment, name string, crKind string, appSourceName string, appList []string, appStateFinal enterpriseApi.AppPhaseStatusType, appStateInitial enterpriseApi.AppPhaseStatusType) {
	for _, appName := range appList {
		gomega.Eventually(func() enterpriseApi.AppPhaseStatusType {
			appDeploymentInfo, _ := GetAppDeploymentInfo(ctx, deployment, testenv, name, crKind, appSourceName, appName)
			return appDeploymentInfo.PhaseInfo.Status
		}, deployment.GetTimeout(), PollInterval).Should(gomega.BeNumerically("~", appStateFinal, appStateInitial)) //Check status value is between appStateInitial and appStateFinal
	}
}

// WaitForAppInstall waits until an app is correctly installed (having status equal to 303)
func (testenv *TestCaseEnv) WaitForAppInstall(ctx context.Context, deployment *Deployment, name string, crKind string, appSourceName string, appList []string) {
	for _, appName := range appList {
		gomega.Eventually(func() enterpriseApi.AppPhaseStatusType {
			appDeploymentInfo, _ := GetAppDeploymentInfo(ctx, deployment, testenv, name, crKind, appSourceName, appName)
			return appDeploymentInfo.PhaseInfo.Status
		}, deployment.GetTimeout(), PollInterval).Should(gomega.BeEquivalentTo(enterpriseApi.AppPkgInstallComplete))
	}

}

// VerifyPodsInMCConfigMap checks if given pod names are present in given KEY of given MC's Config Map
func (testenv *TestCaseEnv) VerifyPodsInMCConfigMap(ctx context.Context, deployment *Deployment, pods []string, key string, mcName string, expected bool) {
	// Get contents of MC config map
	mcConfigMap, err := GetMCConfigMap(ctx, deployment, testenv.GetName(), mcName)
	gomega.Expect(err).To(gomega.Succeed(), "Unable to get MC config map")
	for _, podName := range pods {
		testenv.Log.Info("Checking for POD on  MC Config Map", "POD Name", podName, "DATA", mcConfigMap.Data)
		gomega.Expect(expected).To(gomega.Equal(CheckPodNameInString(podName, mcConfigMap.Data[key])), "Verify Pod in MC Config Map. Pod Name %s.", podName)
	}
}

// VerifyPodsInMCConfigString checks if given pod names are present in given KEY of given MC's Config Map
func (testenv *TestCaseEnv) VerifyPodsInMCConfigString(ctx context.Context, deployment *Deployment, pods []string, mcName string, expected bool, checkPodIP bool) {
	for _, podName := range pods {
		testenv.Log.Info("Checking pod configured in MC POD Peers String", "Pod Name", podName)
		var found bool
		if checkPodIP {
			podIP := GetPodIP(testenv.GetName(), podName)
			found = CheckPodNameOnMC(testenv.GetName(), mcName, podIP)
		} else {
			found = CheckPodNameOnMC(testenv.GetName(), mcName, podName)
		}
		gomega.Expect(expected).To(gomega.Equal(found), "Verify Pod in MC Config String. Pod Name %s.", podName)
	}
}

// VerifyClusterManagerBundlePush verify that bundle push was pushed on all indexers
func (testenv *TestCaseEnv) VerifyClusterManagerBundlePush(ctx context.Context, deployment *Deployment, ns string, replicas int, previousBundleHash string) {
	gomega.Eventually(func() bool {
		// Get Bundle status and check that each pod has successfully deployed the latest bundle
		clusterManagerBundleStatus := CMBundlePushstatus(ctx, deployment, previousBundleHash, "cmanager")
		if strings.Contains(deployment.GetName(), "master") {
			clusterManagerBundleStatus = CMBundlePushstatus(ctx, deployment, previousBundleHash, "cmaster")
		}
		if len(clusterManagerBundleStatus) < replicas {
			testenv.Log.Info("Bundle push on Pod not complete on all pods", "Pod with bundle push", clusterManagerBundleStatus)
			return false
		}
		clusterPodNames := DumpGetPods(testenv.GetName())

		for _, podName := range clusterPodNames {
			if strings.Contains(podName, "-indexer-") {
				if _, present := clusterManagerBundleStatus[podName]; present {
					if clusterManagerBundleStatus[podName] != "Up" {
						testenv.Log.Info("Bundle push on Pod not complete", "Pod Name", podName, "Status", clusterManagerBundleStatus[podName])
						return false
					}
				} else {
					testenv.Log.Info("Bundle push not found on pod", "Podname", podName)
					return false
				}
			}
		}
		return true
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyDeployerBundlePush verify that bundle push was pushed on all search heads
func (testenv *TestCaseEnv) VerifyDeployerBundlePush(ctx context.Context, deployment *Deployment, ns string, replicas int) {
	gomega.Eventually(func() bool {
		deployerAppPushStatus := DeployerBundlePushstatus(ctx, deployment, ns)
		if len(deployerAppPushStatus) == 0 {
			testenv.Log.Info("Bundle push not complete on all pods")
			DumpGetPods(testenv.GetName())

			return false
		}
		for appName, val := range deployerAppPushStatus {
			if val < replicas {
				testenv.Log.Info("Bundle push not complete on all pods for", "App Name", appName, "Replicas with bundle push", val, "Expected replicas", replicas)
				DumpGetPods(testenv.GetName())

				return false
			}
		}
		return true
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyNoPodResetByUID verify that no pod reset during App install by comparing pod UIDs
func (testenv *TestCaseEnv) VerifyNoPodResetByUID(ctx context.Context, deployment *Deployment, podUIDMap map[string]string, podToSkip []string) {
	if podUIDMap == nil {
		testenv.Log.Info("podUIDMap is empty. Skipping validation")
	} else {
		currentSplunkPodUIDs := GetPodUIDs(testenv.GetName())
		for podName, currentUID := range currentSplunkPodUIDs {
			if strings.Contains(podName, "monitoring-console") {
				continue
			}
			testenv.Log.Info("Checking Pod reset for Pod Name", "podName", podName, "currentUID", currentUID)
			if previousUID, ok := podUIDMap[podName]; ok {
				if !CheckStringInSlice(podToSkip, podName) {
					gomega.Expect(currentUID).To(gomega.Equal(previousUID), "Pod reset was detected. Pod Name %s. Current Pod UID %s. Previous Pod UID %s", podName, currentUID, previousUID)
				}
			}
		}
	}
}

// WaitForSplunkPodCleanup Wait for cleanup to happend
func (testenv *TestCaseEnv) WaitForSplunkPodCleanup(ctx context.Context, deployment *Deployment) {
	gomega.Eventually(func() int {
		testenv.Log.Info("Waiting for Splunk Pods to be deleted before running test")
		return len(DumpGetPods(testenv.GetName()))
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(0))
}

// WaitforAppInstallState Wait for App to reach state specified in conf file
func (testenv *TestCaseEnv) WaitforAppInstallState(ctx context.Context, deployment *Deployment, podNames []string, ns string, appName string, newState string, clusterWideInstall bool) {
	testenv.Log.Info("Retrieve App state on pod")
	for _, podName := range podNames {
		gomega.Eventually(func() string {
			status, _, err := GetPodAppStatus(ctx, deployment, podName, ns, appName, clusterWideInstall)
			testenv.Log.Info("App details", "app", appName, "status", status, "error", err, "podName", podName)
			return status
		}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(strings.ToUpper(newState)))
	}
}

// VerifyAppRepoState verify given app repo state is equal to given value for app for given CR Kind
func (testenv *TestCaseEnv) VerifyAppRepoState(ctx context.Context, deployment *Deployment, name string, crKind string, appSourceName string, repoValue int, appName string) {
	testenv.Log.Info("Check for app repo state in CR")
	gomega.Eventually(func() int {
		appDeploymentInfo, err := GetAppDeploymentInfo(ctx, deployment, testenv, name, crKind, appSourceName, appName)
		if err != nil {
			testenv.Log.Error(err, "Failed to get app deployment info")
			return 0
		}
		testenv.Log.Info(fmt.Sprintf("App State found for CR %s NAME %s APP NAME %s Expected repo value %d", crKind, name, appName, repoValue), "Actual Value", appDeploymentInfo.RepoState, "App State", appDeploymentInfo)
		return int(appDeploymentInfo.RepoState)
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(repoValue))
}

// VerifyIsDeploymentInProgressFlagIsSet verify IsDeploymentInProgress flag is set to true
func (testenv *TestCaseEnv) VerifyIsDeploymentInProgressFlagIsSet(ctx context.Context, deployment *Deployment, name string, crKind string) {
	testenv.Log.Info("Check IsDeploymentInProgress Flag is set", "CR NAME", name, "CR Kind", crKind)
	gomega.Eventually(func() bool {
		isDeploymentInProgress, err := GetIsDeploymentInProgressFlag(ctx, deployment, testenv, name, crKind)
		if err != nil {
			testenv.Log.Error(err, "Failed to get isDeploymentInProgress Flag")
			return false
		}
		testenv.Log.Info("IsDeploymentInProgress Flag status found", "CR NAME", name, "CR Kind", crKind, "IsDeploymentInProgress", isDeploymentInProgress)
		return isDeploymentInProgress
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// VerifyFilesInDirectoryOnPod verify that files are present in folder.
func (testenv *TestCaseEnv) VerifyFilesInDirectoryOnPod(ctx context.Context, deployment *Deployment, podNames []string, files []string, path string, checkDirectory bool, checkPresent bool) {
	for _, podName := range podNames {
		gomega.Eventually(func() bool {
			// Using checkDirectory here to get all files in case of negative check.  GetDirsOrFilesInPath  will return files/directory when checkDirecotry is FALSE
			filelist, err := GetDirsOrFilesInPath(ctx, deployment, podName, path, checkDirectory)
			gomega.Expect(err).To(gomega.Succeed(), "Unable to get files on pod", "Pod", podName)
			for _, file := range files {
				found := CheckStringInSlice(filelist, file)
				testenv.Log.Info("File check", "pod", podName, "filename", file, "path", path, "status", found)
				if found != checkPresent {
					return false
				}
			}
			return true
		}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
	}
}

func (testenv *TestCaseEnv) GetTelemetryLastSubmissionTime(ctx context.Context, deployment *Deployment) string {
	const (
		configMapName = "splunk-operator-manager-telemetry"
		statusKey     = "status"
	)
	type telemetryStatus struct {
		LastTransmission string `json:"lastTransmission"`
	}

	cm := &corev1.ConfigMap{}
	err := deployment.testenv.GetKubeClient().Get(ctx, client.ObjectKey{Name: configMapName, Namespace: "splunk-operator"}, cm)
	if err != nil {
		testenv.Log.Error(err, "GetTelemetryLastSubmissionTime: failed to retrieve configmap")
		return ""
	}

	statusVal, ok := cm.Data[statusKey]
	if !ok || statusVal == "" {
		testenv.Log.Info("GetTelemetryLastSubmissionTime: failed to retrieve status")
		return ""
	}
	testenv.Log.Info("GetTelemetryLastSubmissionTime: retrieved status", "status", statusVal)

	var status telemetryStatus
	if err := json.Unmarshal([]byte(statusVal), &status); err != nil {
		testenv.Log.Error(err, "GetTelemetryLastSubmissionTime: failed to unmarshal status", "status", statusVal)
		return ""
	}
	return status.LastTransmission
}

// VerifyTelemetry checks that the telemetry ConfigMap has a non-empty lastTransmission field in its status key.
func (testenv *TestCaseEnv) VerifyTelemetry(ctx context.Context, deployment *Deployment, prevVal string) {
	testenv.Log.Info("VerifyTelemetry: start")
	gomega.Eventually(func() bool {
		currentVal := testenv.GetTelemetryLastSubmissionTime(ctx, deployment)
		if currentVal != "" && currentVal != prevVal {
			testenv.Log.Info("VerifyTelemetry: success", "previous", prevVal, "current", currentVal)
			return true
		}
		return false
	}, deployment.GetTimeout(), PollInterval).Should(gomega.Equal(true))
}

// TriggerTelemetrySubmission updates or adds the 'test_submission' key in the telemetry ConfigMap with a JSON value containing a random number.
func (testenv *TestCaseEnv) TriggerTelemetrySubmission(ctx context.Context, deployment *Deployment) {
	const (
		configMapName = "splunk-operator-manager-telemetry"
		testKey       = "test_submission"
	)

	// Generate a random number
	rand.Seed(time.Now().UnixNano())
	randomNumber := rand.Intn(1000)

	// Create the JSON value
	jsonValue, err := json.Marshal(map[string]int{"value": randomNumber})
	if err != nil {
		testenv.Log.Error(err, "Failed to marshal JSON value")
		return
	}

	// Wait for ConfigMap to exist before updating
	cm := &corev1.ConfigMap{}
	err = testenv.WaitForResourceToExist(ctx, deployment, configMapName, "splunk-operator", cm, 30*time.Second)
	if err != nil {
		testenv.Log.Error(err, "Failed to wait for ConfigMap to exist")
		return
	}

	// Update the test_submission key
	cm.Data[testKey] = string(jsonValue)
	err = deployment.testenv.GetKubeClient().Update(ctx, cm)
	if err != nil {
		testenv.Log.Error(err, "Failed to update ConfigMap")
		return
	}

	testenv.Log.Info("Successfully updated telemetry ConfigMap", "key", testKey, "value", jsonValue)
}

// WaitForEvent waits for an event instead of relying on time
func (testenv *TestCaseEnv) WaitForEvent(ctx context.Context, deployment *Deployment, namespace, crName, eventReason string, timeout time.Duration) error {
	return testenv.WatchForEventWithReason(ctx, deployment, namespace, crName, eventReason, timeout)
}

// WaitForClusterManagerPhase waits for ClusterManager to reach expected phase
func (testenv *TestCaseEnv) WaitForClusterManagerPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForClusterManagerPhase(ctx, deployment, namespace, crName, expectedPhase, timeout)
}

// WaitForSearchHeadClusterPhase waits for SearchHeadCluster to reach expected phase
func (testenv *TestCaseEnv) WaitForSearchHeadClusterPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForSearchHeadClusterPhase(ctx, deployment, namespace, crName, expectedPhase, timeout)
}

// WaitForMonitoringConsolePhase waits for MonitoringConsole to reach expected phase
func (testenv *TestCaseEnv) WaitForMonitoringConsolePhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForMonitoringConsolePhase(ctx, deployment, namespace, crName, expectedPhase, timeout)
}

// WaitForClusterInitialized waits for ClusterInitialized event on IndexerCluster
func (testenv *TestCaseEnv) WaitForClusterInitialized(ctx context.Context, deployment *Deployment, namespace, crName string, timeout time.Duration) error {
	return testenv.WaitForEvent(ctx, deployment, namespace, crName, "ClusterInitialized", timeout)
}

// WaitForScaledUp waits for ScaledUp event on a CR (Standalone, IndexerCluster, SearchHeadCluster)
func (testenv *TestCaseEnv) WaitForScaledUp(ctx context.Context, deployment *Deployment, namespace, crName string, timeout time.Duration) error {
	return testenv.WaitForEvent(ctx, deployment, namespace, crName, "ScaledUp", timeout)
}

// WaitForScaledDown waits for ScaledDown event on a CR (Standalone, IndexerCluster, SearchHeadCluster)
func (testenv *TestCaseEnv) WaitForScaledDown(ctx context.Context, deployment *Deployment, namespace, crName string, timeout time.Duration) error {
	return testenv.WaitForEvent(ctx, deployment, namespace, crName, "ScaledDown", timeout)
}

// WaitForPasswordSyncCompleted waits for PasswordSyncCompleted event on IndexerCluster or SearchHeadCluster
func (testenv *TestCaseEnv) WaitForPasswordSyncCompleted(ctx context.Context, deployment *Deployment, namespace, crName string, timeout time.Duration) error {
	return testenv.WaitForEvent(ctx, deployment, namespace, crName, "PasswordSyncCompleted", timeout)
}

// WaitForPodsInMCConfigMap waits for pods to appear in MC ConfigMap
func (testenv *TestCaseEnv) WaitForPodsInMCConfigMap(ctx context.Context, deployment *Deployment, pods []string, key string, mcName string, expected bool, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		mcConfigMap, err := GetMCConfigMap(ctx, deployment, testenv.GetName(), mcName)
		if err != nil {
			return false, nil
		}
		for _, podName := range pods {
			found := CheckPodNameInString(podName, mcConfigMap.Data[key])
			if found != expected {
				return false, nil
			}
		}
		return true, nil
	})
}

// WaitForPodsInMCConfigString waits for pods to appear in MC config string
func (testenv *TestCaseEnv) WaitForPodsInMCConfigString(ctx context.Context, deployment *Deployment, pods []string, mcName string, expected bool, checkPodIP bool, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		for _, podName := range pods {
			var found bool
			if checkPodIP {
				podIP := GetPodIP(testenv.GetName(), podName)
				found = CheckPodNameOnMC(testenv.GetName(), mcName, podIP)
			} else {
				found = CheckPodNameOnMC(testenv.GetName(), mcName, podName)
			}
			if found != expected {
				return false, nil
			}
		}
		return true, nil
	})
}

// WaitForAppPhase waits for an app to reach a specific phase on a CR
func (testenv *TestCaseEnv) WaitForAppPhase(ctx context.Context, deployment *Deployment, crName string, crKind string, appSourceName string, appName string, expectedPhase enterpriseApi.AppPhaseType, timeout time.Duration) error {
	return testenv.WatchForAppPhaseChange(ctx, deployment, testenv.GetName(), crName, crKind, appSourceName, appName, expectedPhase, timeout)
}

// WaitForAllAppsPhase waits for all apps in a list to reach a specific phase
func (testenv *TestCaseEnv) WaitForAllAppsPhase(ctx context.Context, deployment *Deployment, crName string, crKind string, appSourceName string, appList []string, expectedPhase enterpriseApi.AppPhaseType, timeout time.Duration) error {
	return testenv.WatchForAllAppsPhaseChange(ctx, deployment, testenv.GetName(), crName, crKind, appSourceName, appList, expectedPhase, timeout)
}

// WaitForStandalonePhase waits for Standalone to reach expected phase
func (testenv *TestCaseEnv) WaitForStandalonePhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForStandalonePhase(ctx, deployment, namespace, crName, expectedPhase, timeout)
}

// WaitForLicenseManagerPhase waits for LicenseManager to reach expected phase
func (testenv *TestCaseEnv) WaitForLicenseManagerPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForLicenseManagerPhase(ctx, deployment, namespace, crName, expectedPhase, timeout)
}

// WaitForLicenseMasterPhase waits for LicenseMaster to reach expected phase
func (testenv *TestCaseEnv) WaitForLicenseMasterPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForLicenseMasterPhase(ctx, deployment, namespace, crName, expectedPhase, timeout)
}

// WaitForIndexerClusterPhase waits for IndexerCluster to reach expected phase
func (testenv *TestCaseEnv) WaitForIndexerClusterPhase(ctx context.Context, deployment *Deployment, namespace, crName string, expectedPhase enterpriseApi.Phase, timeout time.Duration) error {
	return testenv.WatchForIndexerClusterPhase(ctx, deployment, namespace, crName, expectedPhase, timeout)
}

// WaitForSearchResultsNonEmpty waits for search results to return a non-empty "result" field
func WaitForSearchResultsNonEmpty(ctx context.Context, deployment *Deployment, podName string, searchString string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		searchResultsResp, err := PerformSearchSync(ctx, podName, searchString, deployment)
		if err != nil {
			return false, nil
		}
		var searchResults map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(searchResultsResp), &searchResults); jsonErr != nil {
			return false, nil
		}
		return searchResults["result"] != nil, nil
	})
}

// WaitForPodExecSuccess retries pod exec command until success or timeout
func WaitForPodExecSuccess(ctx context.Context, deployment *Deployment, podName string, command []string, stdin string, timeout time.Duration) (string, error) {
	var stdout string
	err := wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		var err error
		stdout, _, err = deployment.PodExecCommand(ctx, podName, command, stdin, false)
		return err == nil, nil
	})
	return stdout, err
}

// ValidateTestPrerequisites performs early validation checks to fail fast before long operations
// This saves time by catching configuration errors immediately instead of after minutes of waiting
func (testenv *TestCaseEnv) ValidateTestPrerequisites(ctx context.Context, deployment *Deployment) error {
	testenv.Log.Info("Validating test prerequisites for fail-fast behavior")

	ns := &corev1.Namespace{}
	if err := deployment.testenv.GetKubeClient().Get(ctx, client.ObjectKey{Name: testenv.GetName()}, ns); err != nil {
		return fmt.Errorf("namespace validation failed - namespace '%s' does not exist: %w", testenv.GetName(), err)
	}
	testenv.Log.Info("Namespace exists", "namespace", testenv.GetName())

	operatorNamespace := testenv.GetName()
	if testenv.clusterWideOperator == "true" {
		operatorNamespace = "splunk-operator"
	}

	var runningPod *corev1.Pod
	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		podList := &corev1.PodList{}
		listOpts := []client.ListOption{
			client.InNamespace(operatorNamespace),
		}

		if err := deployment.testenv.GetKubeClient().List(ctx, podList, listOpts...); err != nil {
			testenv.Log.Info("Failed to list pods in operator namespace", "namespace", operatorNamespace, "error", err)
			return false, nil
		}

		for i := range podList.Items {
			pod := &podList.Items[i]
			if strings.HasPrefix(pod.Name, "splunk-operator-controller-manager") || strings.HasPrefix(pod.Name, "splunk-op") {
				if pod.Status.Phase == corev1.PodRunning {
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
							runningPod = pod
							testenv.Log.Info("Found running and ready operator pod", "pod", pod.Name, "phase", pod.Status.Phase)
							return true, nil
						}
					}
					testenv.Log.Info("Found operator pod but not ready yet", "pod", pod.Name, "phase", pod.Status.Phase)
				} else {
					testenv.Log.Info("Found operator pod but not running", "pod", pod.Name, "phase", pod.Status.Phase)
				}
			}
		}
		testenv.Log.Info("No running operator pod found yet", "namespace", operatorNamespace)
		return false, nil
	})

	if err != nil {
		return fmt.Errorf("operator pod not found or not ready in namespace '%s' after 30s: %w", operatorNamespace, err)
	}

	testenv.Log.Info("Operator pod is running and ready", "pod", runningPod.Name, "phase", runningPod.Status.Phase)
	testenv.Log.Info("All test prerequisites validated successfully")
	return nil
}

// WaitForResourceToExist waits for a Kubernetes resource to exist before proceeding with verification
// This provides fail-fast behavior when resources haven't been created yet
func (testenv *TestCaseEnv) WaitForResourceToExist(ctx context.Context, deployment *Deployment, name, namespace string, obj client.Object, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 1*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		err := deployment.testenv.GetKubeClient().Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, obj)
		if err != nil {
			if errors.IsNotFound(err) {
				testenv.Log.Info("Resource not found yet", "name", name, "namespace", namespace)
				return false, nil
			}
			testenv.Log.Error(err, "Error checking resource existence", "name", name, "namespace", namespace)
			return false, err
		}
		testenv.Log.Info("Resource exists", "name", name, "namespace", namespace)
		return true, nil
	})
}

// WaitForAppRepoStateChange waits for app repo state to change to expected value, indicating poll interval has completed
func (testenv *TestCaseEnv) WaitForAppRepoStateChange(ctx context.Context, deployment *Deployment, crName, crKind, appSourceName string, appList []string, expectedRepoState int, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}

		allAppsReady := true
		for _, appName := range appList {
			lookupAppName := appName
			if appInfo, ok := AppInfo[appName]; ok {
				if appFileName, ok := appInfo["filename"]; ok && appFileName != "" {
					lookupAppName = appFileName
				}
			}

			appDeploymentInfo, err := GetAppDeploymentInfo(ctx, deployment, testenv, crName, crKind, appSourceName, lookupAppName)
			if err != nil {
				testenv.Log.Info("Failed to get app deployment info while waiting for repo state change", "app", appName, "error", err)
				return false, nil
			}

			if appDeploymentInfo.AppName == "" {
				testenv.Log.Info("App deployment info not found yet", "app", appName)
				allAppsReady = false
				continue
			}

			currentRepoState := int(appDeploymentInfo.RepoState)
			if currentRepoState != expectedRepoState {
				testenv.Log.Info("App repo state not yet at expected value", "app", appName, "current", currentRepoState, "expected", expectedRepoState)
				allAppsReady = false
			}
		}

		if allAppsReady {
			testenv.Log.Info("All apps reached expected repo state", "count", len(appList), "repoState", expectedRepoState)
			return true, nil
		}
		return false, nil
	})
}
