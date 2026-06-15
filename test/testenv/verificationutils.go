// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
	"reflect"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	wait "k8s.io/apimachinery/pkg/util/wait"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
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

// getPodDetails fetches and unmarshals the JSON details for a single pod.
func getPodDetails(ns, podName string) (*PodDetailsStruct, error) {
	ctx, cancel := context.WithTimeout(context.Background(), KubectlQuickTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, "kubectl", "get", "pods", "-n", ns, podName, "-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl get pod %s in ns %s: %w", podName, ns, err)
	}
	var details PodDetailsStruct
	if err := json.Unmarshal(output, &details); err != nil {
		return nil, fmt.Errorf("unmarshal pod %s details: %w", podName, err)
	}
	return &details, nil
}

// PollConsistently verifies a condition holds for the entire duration.
// condFn should return nil if the condition holds, or an error if it fails.
// The check is abandoned early if ctx is cancelled.
func PollConsistently(ctx context.Context, duration, interval time.Duration, condFn func() error) error {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := condFn(); err != nil {
			return fmt.Errorf("consistency check failed: %w", err)
		}
		time.Sleep(interval)
	}
	return nil
}

// VerifyMonitoringConsoleReady verifies the Monitoring Console CR reaches
// Ready status and stays there (does not flip-flop).
// The effective timeout is min(deployment.GetTimeout(), ctx deadline).
// Callers that need a shorter per-attempt budget (e.g. inside Eventually)
// should pass a context with a tighter deadline.
func (testenv *TestCaseEnv) VerifyMonitoringConsoleReady(ctx context.Context, deployment *Deployment, mcName string, monitoringConsole *enterpriseApi.MonitoringConsole) error {
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForMonitoringConsolePhase(ctx, deployment, testenv.GetName(), mcName, enterpriseApi.PhaseReady, DefaultTimeout)
	if err != nil {
		return fmt.Errorf("monitoring console %s failed to reach Ready phase: %w", mcName, err)
	}

	// Refresh the instance to get latest state
	err = deployment.GetInstance(ctx, mcName, monitoringConsole)
	if err != nil {
		return fmt.Errorf("failed to get MonitoringConsole instance: %w", err)
	}
	testenv.Log.Info("MonitoringConsole reached Ready phase", "instance", monitoringConsole.ObjectMeta.Name, "phase", monitoringConsole.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, we should stay in Ready and not flip-flop around
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		if err := deployment.GetInstance(ctx, mcName, monitoringConsole); err != nil {
			testenv.Log.Info("Transient error refreshing MonitoringConsole during consistency check", "error", err)
		}
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "monitoring-console")
		if monitoringConsole.Status.Phase != enterpriseApi.PhaseReady {
			return fmt.Errorf("monitoring console phase flipped to %s", monitoringConsole.Status.Phase)
		}
		return nil
	})
}

// VerifyStandaloneReady verify Standalone is in ReadyStatus and does not flip-flop
func (testenv *TestCaseEnv) VerifyStandaloneReady(ctx context.Context, deployment *Deployment, deploymentName string, standalone *enterpriseApi.Standalone) error {
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForStandalonePhase(ctx, deployment, testenv.GetName(), standalone.Name, enterpriseApi.PhaseReady, DefaultTimeout)
	if err != nil {
		return fmt.Errorf("standalone failed to reach Ready phase: %w", err)
	}

	// Refresh the instance to get latest state
	err = deployment.GetInstance(ctx, standalone.Name, standalone)
	if err != nil {
		return fmt.Errorf("failed to get standalone instance: %w", err)
	}
	testenv.Log.Info("Standalone reached Ready phase", "instance", standalone.ObjectMeta.Name, "phase", standalone.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, we should stay in Ready and not flip-flop around
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		if err := deployment.GetInstance(ctx, standalone.Name, standalone); err != nil {
			testenv.Log.Info("Transient error refreshing Standalone during consistency check", "error", err)
		}
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "standalone")
		if standalone.Status.Phase != enterpriseApi.PhaseReady {
			return fmt.Errorf("standalone phase flipped to %s", standalone.Status.Phase)
		}
		return nil
	})
}

// VerifyStandalonePhaseAndReady verifies the Standalone reaches the given transitional phase
// (e.g. ScalingUp, Updating) and then returns to Ready without flip-flopping.
func (testenv *TestCaseEnv) VerifyStandalonePhaseAndReady(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase, standalone *enterpriseApi.Standalone) error {
	if err := testenv.VerifyStandalonePhase(ctx, deployment, phase); err != nil {
		return err
	}
	return testenv.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)
}

// VerifySearchHeadClusterReady verify SHC is in READY status and does not flip-flop
func (testenv *TestCaseEnv) VerifySearchHeadClusterReady(ctx context.Context, deployment *Deployment) error {
	instanceName := fmt.Sprintf("%s-shc", deployment.GetName())

	// Honor the deployment's configured timeout so suites that explicitly opt
	// into a larger budget (e.g. LongTimeout / MediumLongTimeout / WithTimeout(4000))
	// aren't silently hard-capped at DefaultTimeout (30m).
	overallTimeout := deployment.GetTimeout()
	if overallTimeout <= 0 {
		overallTimeout = DefaultTimeout
	}
	overallDeadline := time.Now().Add(overallTimeout)

	shc := &enterpriseApi.SearchHeadCluster{}
	// Retry the "wait for Ready + verify it stays Ready" cycle as a single
	// unit, bounded by the deployment timeout. A brief flip back to Pending
	// during an in-flight app-framework reconcile (legitimate operator
	// behavior between download/copy/install stages) should not fail the
	// whole spec — we just re-wait for the next steady Ready and retry the
	// consistency window.
	for {
		remaining := time.Until(overallDeadline)
		if remaining <= 0 {
			return fmt.Errorf("SearchHeadCluster did not reach steady Ready phase within %s", overallTimeout)
		}

		// Use optimized watch to wait for Ready phase (checks both Phase and DeployerPhase).
		// Cap each wait attempt at the remaining overall budget.
		err := testenv.WatchForSearchHeadClusterPhase(ctx, deployment, testenv.GetName(), instanceName, enterpriseApi.PhaseReady, remaining)
		if err != nil {
			return fmt.Errorf("SearchHeadCluster failed to reach Ready phase: %w", err)
		}

		// Refresh the instance to get latest state
		if err := deployment.GetInstance(ctx, instanceName, shc); err != nil {
			return fmt.Errorf("failed to get SearchHeadCluster instance: %w", err)
		}
		testenv.Log.Info("SearchHeadCluster reached Ready phase", "instance", shc.ObjectMeta.Name, "phase", shc.Status.Phase, "deployerPhase", shc.Status.DeployerPhase)
		DumpGetPods(testenv.GetName())

		// In a steady state, we should stay in Ready and not flip-flop around.
		consistencyErr := PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
			if err := deployment.GetInstance(ctx, instanceName, shc); err != nil {
				testenv.Log.Info("Transient error refreshing SearchHeadCluster during consistency check", "error", err)
			}
			testenv.Log.Info("Check for Consistency Search Head Cluster phase to be ready", "instance", shc.ObjectMeta.Name, "phase", shc.Status.Phase)
			DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "-shc-")
			if shc.Status.Phase != enterpriseApi.PhaseReady {
				return fmt.Errorf("SHC phase flipped to %s", shc.Status.Phase)
			}
			return nil
		})
		if consistencyErr == nil {
			return nil
		}

		// Bail out immediately on context cancellation rather than spinning.
		if ctx.Err() != nil {
			return fmt.Errorf("context cancelled while waiting for steady SHC Ready: %w", consistencyErr)
		}

		testenv.Log.Info("SHC consistency check failed, will re-wait for steady Ready", "error", consistencyErr, "remaining", time.Until(overallDeadline))
	}
}

// VerifySingleSiteIndexersReady verify single site indexers go to ready state
func (testenv *TestCaseEnv) VerifySingleSiteIndexersReady(ctx context.Context, deployment *Deployment) error {
	instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForIndexerClusterPhase(ctx, deployment, testenv.GetName(), instanceName, enterpriseApi.PhaseReady, DefaultTimeout)
	if err != nil {
		return fmt.Errorf("IndexerCluster failed to reach Ready phase: %w", err)
	}

	// Refresh the instance to get latest state
	idc := &enterpriseApi.IndexerCluster{}
	err = deployment.GetInstance(ctx, instanceName, idc)
	if err != nil {
		return fmt.Errorf("failed to get IndexerCluster instance: %w", err)
	}
	testenv.Log.Info("IndexerCluster reached Ready phase", "instance", instanceName, "phase", idc.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, we should stay in Ready and not flip-flop around
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		if err := deployment.GetInstance(ctx, instanceName, idc); err != nil {
			testenv.Log.Info("Transient error refreshing IndexerCluster during consistency check", "error", err)
		}
		testenv.Log.Info("Check for Consistency indexer instance's phase to be ready", "instance", instanceName, "phase", idc.Status.Phase)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "-idxc-indexer-")
		if idc.Status.Phase != enterpriseApi.PhaseReady {
			return fmt.Errorf("indexer phase flipped to %s", idc.Status.Phase)
		}
		return nil
	})
}

// IngestorsReady verify ingestors go to ready state
func (testenv *TestCaseEnv) VerifyIngestorReady(ctx context.Context, deployment *Deployment) error {
	instanceName := fmt.Sprintf("%s-ingest", deployment.GetName())
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForIngestorClusterPhase(ctx, deployment, testenv.GetName(), instanceName, enterpriseApi.PhaseReady, DefaultTimeout)
	if err != nil {
		return fmt.Errorf("IngestorCluster failed to reach Ready phase: %w", err)
	}

	// Refresh the instance to get latest state
	ingest := &enterpriseApi.IngestorCluster{}
	err = deployment.GetInstance(ctx, instanceName, ingest)
	if err != nil {
		return fmt.Errorf("failed to get IngestorCluster instance: %w", err)
	}
	testenv.Log.Info("IngestorCluster reached Ready phase", "instance", instanceName, "phase", ingest.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, we should stay in Ready and not flip-flop around
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		if err := deployment.GetInstance(ctx, instanceName, ingest); err != nil {
			testenv.Log.Info("Transient error refreshing IngestorCluster during consistency check", "error", err)
		}
		testenv.Log.Info("Check for Consistency ingestor instance's phase to be ready", "instance", instanceName, "phase", ingest.Status.Phase)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "-ingest-")
		if ingest.Status.Phase != enterpriseApi.PhaseReady {
			return fmt.Errorf("ingestor phase flipped to %s", ingest.Status.Phase)
		}
		return nil
	})
}

// VerifyClusterManagerReady verify Cluster Manager Instance is in ready status
func (testenv *TestCaseEnv) VerifyClusterManagerReady(ctx context.Context, deployment *Deployment) error {
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForClusterManagerPhase(ctx, deployment, testenv.GetName(), deployment.GetName(), enterpriseApi.PhaseReady, DefaultTimeout)
	if err != nil {
		return fmt.Errorf("ClusterManager failed to reach Ready phase: %w", err)
	}

	// Refresh the instance to get latest state
	cm := &enterpriseApi.ClusterManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	if err != nil {
		return fmt.Errorf("failed to get ClusterManager instance: %w", err)
	}
	testenv.Log.Info("ClusterManager reached Ready phase", "instance", cm.ObjectMeta.Name, "phase", cm.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, cluster-manager should stay in Ready and not flip-flop around
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		if err := deployment.GetInstance(ctx, deployment.GetName(), cm); err != nil {
			testenv.Log.Info("Transient error refreshing ClusterManager during consistency check", "error", err)
		}
		testenv.Log.Info("Check for Consistency "+splcommon.ClusterManager+" phase to be ready", "instance", cm.ObjectMeta.Name, "phase", cm.Status.Phase)
		DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "cluster-manager")
		testenv.Log.Info("Check for Consistency cluster-manager phase to be ready", "instance", cm.ObjectMeta.Name, "phase", cm.Status.Phase)
		if cm.Status.Phase != enterpriseApi.PhaseReady {
			return fmt.Errorf("cluster manager phase flipped to %s", cm.Status.Phase)
		}
		return nil
	})
}

// VerifyClusterMasterReady verify Cluster Master Instance is in ready status
func (testenv *TestCaseEnv) VerifyClusterMasterReady(ctx context.Context, deployment *Deployment) error {
	// Use optimized watch to wait for Ready phase
	err := testenv.WatchForClusterMasterPhase(ctx, deployment, testenv.GetName(), deployment.GetName(), enterpriseApi.PhaseReady, DefaultTimeout)
	if err != nil {
		return fmt.Errorf("ClusterMaster failed to reach Ready phase: %w", err)
	}

	// Refresh the instance to get latest state
	cm := &enterpriseApiV3.ClusterMaster{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	if err != nil {
		return fmt.Errorf("failed to get ClusterMaster instance: %w", err)
	}
	testenv.Log.Info("ClusterMaster reached Ready phase", "instance", cm.ObjectMeta.Name, "phase", cm.Status.Phase)
	DumpGetPods(testenv.GetName())

	// In a steady state, cluster-master should stay in Ready and not flip-flop around
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		if err := deployment.GetInstance(ctx, deployment.GetName(), cm); err != nil {
			testenv.Log.Info("Transient error refreshing ClusterMaster during consistency check", "error", err)
		}
		testenv.Log.Info("Check for Consistency cluster-master phase to be ready", "instance", cm.ObjectMeta.Name, "phase", cm.Status.Phase)
		if cm.Status.Phase != enterpriseApi.PhaseReady {
			return fmt.Errorf("cluster master phase flipped to %s", cm.Status.Phase)
		}
		return nil
	})
}

// VerifyIndexersReady verify indexers of all sites go to ready state
func (testenv *TestCaseEnv) VerifyIndexersReady(ctx context.Context, deployment *Deployment, siteCount int) error {
	siteIndexerMap := map[string][]string{}
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-indexer-0", instanceName)}
		// Ensure indexers go to Ready phase
		idc := &enterpriseApi.IndexerCluster{}
		err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
			err := deployment.GetInstance(ctx, instanceName, idc)
			if err != nil {
				return false, nil
			}
			testenv.Log.Info("Waiting for indexer site instance phase to be ready", "instance", instanceName, "phase", idc.Status.Phase)
			DumpGetPods(testenv.GetName())
			return idc.Status.Phase == enterpriseApi.PhaseReady, nil
		})
		if err != nil {
			return fmt.Errorf("indexer site %s failed to reach Ready phase: %w", siteName, err)
		}

		// In a steady state, we should stay in Ready and not flip-flop around
		err = PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
			if err := deployment.GetInstance(ctx, instanceName, idc); err != nil {
				testenv.Log.Info("Transient error refreshing IndexerCluster site during consistency check", "error", err)
			}
			testenv.Log.Info("Check for Consistency indexer site instance phase to be ready", "instance", instanceName, "phase", idc.Status.Phase)
			DumpGetSplunkVersion(ctx, testenv.GetName(), deployment, "-idxc-indexer-")
			if idc.Status.Phase != enterpriseApi.PhaseReady {
				return fmt.Errorf("indexer phase flipped to %s for site %s", idc.Status.Phase, siteName)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// VerifyIndexerClusterMultisiteStatus verify Indexer Cluster is configured as multisite
func (testenv *TestCaseEnv) VerifyIndexerClusterMultisiteStatus(ctx context.Context, deployment *Deployment, siteCount int) error {
	siteIndexerMap := map[string][]string{}
	for site := 1; site <= siteCount; site++ {
		siteName := fmt.Sprintf("site%d", site)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-indexer-0", instanceName)}
	}
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		podName := GetCMPodName(deployment)
		stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/manager/sites?output_mode=json"
		command := []string{"/bin/sh"}
		stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
		if err != nil {
			testenv.Log.Error(err, "Failed to execute command", "onPod", podName, "command", command)
			return false, nil
		}
		testenv.Log.Info("Command executed", "onPod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
		siteIndexerResponse := ClusterManagerSitesResponse{}
		json.Unmarshal([]byte(stdout), &siteIndexerResponse)
		siteIndexerStatus := map[string][]string{}
		for _, site := range siteIndexerResponse.Entries {
			siteIndexerStatus[site.Name] = []string{}
			for _, peer := range site.Content.Peers {
				siteIndexerStatus[site.Name] = append(siteIndexerStatus[site.Name], peer.ServerName)
			}
		}
		return reflect.DeepEqual(siteIndexerStatus, siteIndexerMap), nil
	})
}

// VerifyRFSFMet verify RF SF is met on Cluster Manager
func (testenv *TestCaseEnv) VerifyRFSFMet(ctx context.Context, deployment *Deployment) error {
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		rfSfStatus := CheckRFSF(ctx, deployment)
		testenv.Log.Info("Verifying RF SF is met", "status", rfSfStatus)
		return rfSfStatus, nil
	})
}

// VerifyNoDisconnectedSHPresentOnCM verifies no disconnected SH is present on Cluster Manager
func (testenv *TestCaseEnv) VerifyNoDisconnectedSHPresentOnCM(ctx context.Context, deployment *Deployment) error {
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		shStatus := CheckSearchHeadRemoved(ctx, deployment)
		testenv.Log.Info("Verifying no Search Head in DISCONNECTED state present on Cluster Manager", "status", shStatus)
		if !shStatus {
			return fmt.Errorf("disconnected search head found on Cluster Manager")
		}
		return nil
	})
}

// VerifyLicenseManagerReady verify LM is in ready status and does not flip flop
func (testenv *TestCaseEnv) VerifyLicenseManagerReady(ctx context.Context, deployment *Deployment) error {
	LicenseManager := &enterpriseApi.LicenseManager{}

	testenv.Log.Info("Verifying License Manager becomes READY")
	err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		err := deployment.GetInstance(ctx, deployment.GetName(), LicenseManager)
		if err != nil {
			return false, nil
		}
		testenv.Log.Info("Waiting for License Manager instance status to be ready",
			"instance", LicenseManager.ObjectMeta.Name, "phase", LicenseManager.Status.Phase)
		DumpGetPods(testenv.GetName())
		return LicenseManager.Status.Phase == enterpriseApi.PhaseReady, nil
	})
	if err != nil {
		return fmt.Errorf("license manager failed to reach Ready phase: %w", err)
	}

	// In a steady state, we should stay in Ready and not flip-flop around
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		if err := deployment.GetInstance(ctx, deployment.GetName(), LicenseManager); err != nil {
			testenv.Log.Info("Transient error refreshing LicenseManager during consistency check", "error", err)
		}
		if LicenseManager.Status.Phase != enterpriseApi.PhaseReady {
			return fmt.Errorf("license manager phase flipped to %s", LicenseManager.Status.Phase)
		}
		return nil
	})
}

// VerifyLicenseMasterReady verify LM is in ready status and does not flip flop
func (testenv *TestCaseEnv) VerifyLicenseMasterReady(ctx context.Context, deployment *Deployment) error {
	LicenseMaster := &enterpriseApiV3.LicenseMaster{}

	testenv.Log.Info("Verifying License Master becomes READY")
	err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		err := deployment.GetInstance(ctx, deployment.GetName(), LicenseMaster)
		if err != nil {
			return false, nil
		}
		testenv.Log.Info("Waiting for License Master instance status to be ready",
			"instance", LicenseMaster.ObjectMeta.Name, "phase", LicenseMaster.Status.Phase)
		DumpGetPods(testenv.GetName())
		return LicenseMaster.Status.Phase == enterpriseApi.PhaseReady, nil
	})
	if err != nil {
		return fmt.Errorf("license master failed to reach Ready phase: %w", err)
	}

	// In a steady state, we should stay in Ready and not flip-flop around
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		if err := deployment.GetInstance(ctx, deployment.GetName(), LicenseMaster); err != nil {
			testenv.Log.Info("Transient error refreshing LicenseMaster during consistency check", "error", err)
		}
		if LicenseMaster.Status.Phase != enterpriseApi.PhaseReady {
			return fmt.Errorf("license master phase flipped to %s", LicenseMaster.Status.Phase)
		}
		return nil
	})
}

// VerifyLMConfiguredOnPod verify LM is configured on given POD
func VerifyLMConfiguredOnPod(ctx context.Context, deployment *Deployment, podName string) error {
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		lmConfigured := CheckLicenseManagerConfigured(ctx, deployment, podName)
		if !lmConfigured {
			return fmt.Errorf("license manager not configured on pod %s", podName)
		}
		return nil
	})
}

// VerifyServiceAccountConfiguredOnPod check if given service account is configured on given pod
func (testenv *TestCaseEnv) VerifyServiceAccountConfiguredOnPod(ctx context.Context, ns string, podName string, serviceAccount string) error {
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		podDetails, err := getPodDetails(ns, podName)
		if err != nil {
			testenv.Log.Error(err, "Failed to get pod details", "pod", podName)
			return err
		}
		testenv.Log.Info("Service Account on Pod", "found", podDetails.Spec.ServiceAccount, "expected", serviceAccount)
		if !strings.Contains(serviceAccount, podDetails.Spec.ServiceAccount) {
			return fmt.Errorf("service account mismatch on pod %s: expected %s, found %s", podName, serviceAccount, podDetails.Spec.ServiceAccount)
		}
		return nil
	})
}

// VerifyIndexFoundOnPod verify index found on a given POD
func (testenv *TestCaseEnv) VerifyIndexFoundOnPod(ctx context.Context, deployment *Deployment, podName string, indexName string) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		indexFound, _ := GetIndexOnPod(ctx, deployment, podName, indexName)
		testenv.Log.Info("Checking status of index on pod", "podName", podName, "indexName", indexName, "status", indexFound)
		return indexFound, nil
	})
}

// VerifyIndexConfigsMatch verify index specific config
func (testenv *TestCaseEnv) VerifyIndexConfigsMatch(ctx context.Context, deployment *Deployment, podName string, indexName string, maxGlobalDataSizeMB int, maxGlobalRawDataSizeMB int) error {
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		indexFound, data := GetIndexOnPod(ctx, deployment, podName, indexName)
		testenv.Log.Info("Checking status of index on pod", "podName", podName, "indexName", indexName, "status", indexFound)
		if !indexFound {
			return fmt.Errorf("index %s not found on pod %s", indexName, podName)
		}
		if data.Content.MaxGlobalDataSizeMB != maxGlobalDataSizeMB || data.Content.MaxGlobalRawDataSizeMB != maxGlobalRawDataSizeMB {
			return fmt.Errorf("index config mismatch on pod %s: maxGlobalDataSizeMB=%d (expected %d), maxGlobalRawDataSizeMB=%d (expected %d)",
				podName, data.Content.MaxGlobalDataSizeMB, maxGlobalDataSizeMB, data.Content.MaxGlobalRawDataSizeMB, maxGlobalRawDataSizeMB)
		}
		testenv.Log.Info("Checking index configs", "maxGlobalDataSizeMB", data.Content.MaxGlobalDataSizeMB, "maxGlobalRawDataSizeMB", data.Content.MaxGlobalRawDataSizeMB)
		return nil
	})
}

// VerifyIndexExistsOnS3 Verify Index Exists on S3
func (testenv *TestCaseEnv) VerifyIndexExistsOnS3(ctx context.Context, deployment *Deployment, indexName string, podName string) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		indexFound := CheckPrefixExistsOnS3(indexName)
		testenv.Log.Info("Checking Index on S3", "indexName", indexName, "status", indexFound)
		// During testing found some false failure. Rolling index buckets again to ensure data is pushed to remote storage
		if !indexFound {
			testenv.Log.Info("Index NOT found. Rolling buckets again", "indexName", indexName)
			RollHotToWarm(ctx, deployment, podName, indexName)
		}
		return indexFound, nil
	})
}

// VerifyConfOnPod Verify give conf and value on config file on pod
func (testenv *TestCaseEnv) VerifyConfOnPod(ctx context.Context, podName string, confFilePath string, config string, value string) error {
	return PollConsistently(ctx, ConsistentDuration, ConsistentPollInterval, func() error {
		confLine, err := GetConfLineFromPod(ctx, podName, confFilePath, testenv.GetName(), config, "", false)
		if err != nil {
			testenv.Log.Error(err, "Failed to get config on pod")
			return fmt.Errorf("failed to get config on pod %s: %w", podName, err)
		}
		if strings.Contains(confLine, config) && strings.Contains(confLine, value) {
			testenv.Log.Info("Config found", "config", config, "value", value, "confLine", confLine)
			return nil
		}
		testenv.Log.Info("Config NOT found")
		return fmt.Errorf("config %s=%s not found on pod %s", config, value, podName)
	})
}

// VerifySearchHeadClusterPhase verify the phase of SHC matches given phase
func (testenv *TestCaseEnv) VerifySearchHeadClusterPhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase) error {
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		shc := &enterpriseApi.SearchHeadCluster{}
		shcName := deployment.GetName() + "-shc"
		err := deployment.GetInstance(ctx, shcName, shc)
		if err != nil {
			return false, nil
		}
		testenv.Log.Info("Waiting for Search Head Cluster Phase", "instance", shc.ObjectMeta.Name, "expected", phase, "phase", shc.Status.Phase)
		DumpGetPods(testenv.GetName())
		return shc.Status.Phase == phase, nil
	})
}

// VerifyIndexerClusterPhase verify the phase of idxc matches the given phase
func (testenv *TestCaseEnv) VerifyIndexerClusterPhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase, idxcName string) error {
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		idxc := &enterpriseApi.IndexerCluster{}
		err := deployment.GetInstance(ctx, idxcName, idxc)
		if err != nil {
			return false, nil
		}
		testenv.Log.Info("Waiting for Indexer Cluster Phase", "instance", idxc.ObjectMeta.Name, "expected", phase, "phase", idxc.Status.Phase)
		DumpGetPods(testenv.GetName())
		return idxc.Status.Phase == phase, nil
	})
}

// VerifyStandalonePhase verify the phase of Standalone CR
func (testenv *TestCaseEnv) VerifyStandalonePhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase) error {
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		standalone := &enterpriseApi.Standalone{}
		err := deployment.GetInstance(ctx, deployment.GetName(), standalone)
		if err != nil {
			return false, nil
		}
		testenv.Log.Info("Waiting for Standalone status", "instance", standalone.ObjectMeta.Name, "expected", phase, "actualPhase", standalone.Status.Phase)
		DumpGetPods(testenv.GetName())
		return standalone.Status.Phase == phase, nil
	})
}

// VerifyMonitoringConsolePhase verify the phase of Monitoring Console CR
func (testenv *TestCaseEnv) VerifyMonitoringConsolePhase(ctx context.Context, deployment *Deployment, crName string, phase enterpriseApi.Phase) error {
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		mc := &enterpriseApi.MonitoringConsole{}
		err := deployment.GetInstance(ctx, crName, mc)
		if err != nil {
			return false, nil
		}
		testenv.Log.Info("Waiting for Monitoring Console CR status", "instance", mc.ObjectMeta.Name, "expected", phase, "actualPhase", mc.Status.Phase)
		DumpGetPods(testenv.GetName())
		return mc.Status.Phase == phase, nil
	})
}

// GetResourceVersion get resource version id
func (testenv *TestCaseEnv) GetResourceVersion(ctx context.Context, deployment *Deployment, obj client.Object) string {
	if err := deployment.GetInstance(ctx, obj.GetName(), obj); err != nil {
		return "-1"
	}
	return obj.GetResourceVersion()
}

// VerifyCustomResourceVersionChanged verify the version id
func (testenv *TestCaseEnv) VerifyCustomResourceVersionChanged(ctx context.Context, deployment *Deployment, obj client.Object, resourceVersion string) error {
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		if err := deployment.GetInstance(ctx, obj.GetName(), obj); err != nil {
			return false, nil
		}
		newResourceVersion := obj.GetResourceVersion()
		testenv.Log.Info("Waiting for CR status change", "type", fmt.Sprintf("%T", obj), "instance", obj.GetName(), "notExpected", resourceVersion, "actualResourceVersion", newResourceVersion)
		DumpGetPods(testenv.GetName())
		return newResourceVersion != resourceVersion, nil
	})
}

// VerifyCPULimits verifies value of CPU limits is as expected
func (testenv *TestCaseEnv) VerifyCPULimits(deployment *Deployment, podName string, expectedCPULimits string) error {
	return wait.PollUntilContextTimeout(context.TODO(), PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		podDetails, err := getPodDetails(testenv.GetName(), podName)
		if err != nil {
			testenv.Log.Error(err, "Failed to get pod details", "pod", podName)
			return false, nil
		}
		for i := 0; i < len(podDetails.Spec.Containers); i++ {
			if strings.Contains(podDetails.Spec.Containers[i].Resources.Limits.CPU, expectedCPULimits) {
				testenv.Log.Info("Verifying CPU limits", "pod", podName, "found", podDetails.Spec.Containers[i].Resources.Limits.CPU, "expected", expectedCPULimits)
				return true, nil
			}
		}
		return false, nil
	})
}

// VerifyResourceConstraints verifies that all resource constraints (CPU/memory limits and requests) match on at least one container.
func (testenv *TestCaseEnv) VerifyResourceConstraints(deployment *Deployment, podName string, res corev1.ResourceRequirements) error {
	return wait.PollUntilContextTimeout(context.TODO(), PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		podDetails, err := getPodDetails(testenv.GetName(), podName)
		if err != nil {
			testenv.Log.Error(err, "Failed to get pod details", "pod", podName)
			return false, nil
		}

		for i := 0; i < len(podDetails.Spec.Containers); i++ {
			c := podDetails.Spec.Containers[i]
			cpuLimits := strings.Contains(c.Resources.Limits.CPU, res.Limits.Cpu().String())
			memLimits := strings.Contains(c.Resources.Limits.Memory, res.Limits.Memory().String())
			cpuRequests := strings.Contains(c.Resources.Requests.CPU, res.Requests.Cpu().String())
			memRequests := strings.Contains(c.Resources.Requests.Memory, res.Requests.Memory().String())

			if cpuLimits && memLimits && cpuRequests && memRequests {
				testenv.Log.Info("All resource constraints match", "pod", podName,
					"cpuLimits", c.Resources.Limits.CPU, "memLimits", c.Resources.Limits.Memory,
					"cpuRequests", c.Resources.Requests.CPU, "memRequests", c.Resources.Requests.Memory)
				return true, nil
			}
		}
		return false, nil
	})
}

// VerifyClusterManagerPhase verify phase of Cluster Manager
func (testenv *TestCaseEnv) VerifyClusterManagerPhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase) error {
	cm := &enterpriseApi.ClusterManager{}
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		err := deployment.GetInstance(ctx, deployment.GetName(), cm)
		if err != nil {
			return false, nil
		}
		testenv.Log.Info("Waiting for Cluster Manager Phase", "instance", cm.ObjectMeta.Name, "phase", cm.Status.Phase, "expected", phase)
		DumpGetPods(testenv.GetName())
		return cm.Status.Phase == phase, nil
	})
}

// VerifyClusterMasterPhase verify phase of Cluster Master
func (testenv *TestCaseEnv) VerifyClusterMasterPhase(ctx context.Context, deployment *Deployment, phase enterpriseApi.Phase) error {
	cm := &enterpriseApiV3.ClusterMaster{}
	return wait.PollUntilContextTimeout(ctx, ShortPollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		err := deployment.GetInstance(ctx, deployment.GetName(), cm)
		if err != nil {
			return false, nil
		}
		testenv.Log.Info("Waiting for Cluster Master Phase", "instance", cm.ObjectMeta.Name, "phase", cm.Status.Phase, "expected", phase)
		DumpGetPods(testenv.GetName())
		return cm.Status.Phase == phase, nil
	})
}

// VerifySecretsOnPods Check whether the secret object info is mounted on given pods
// Set match to true or false to indicate desired +ve or -ve match
func (testenv *TestCaseEnv) VerifySecretsOnPods(ctx context.Context, deployment *Deployment, verificationPods []string, data map[string][]byte, match bool) error {
	for _, pod := range verificationPods {
		for secretKey, secretValue := range data {
			found := false
			currentValue := GetMountedKey(ctx, deployment, pod, secretKey)
			comparison := bytes.Compare([]byte(currentValue), secretValue)
			if comparison == 0 {
				found = true
				testenv.Log.Info("Secret Values on POD Match", "matchExpected", match, "podName", pod, "secretKey", secretKey, "givenValue", string(secretValue), "foundValue", currentValue)
			} else {
				testenv.Log.Info("Secret Values on POD DONOT Match", "matchExpected", match, "podName", pod, "secretKey", secretKey, "givenValue", string(secretValue), "foundValue", currentValue)
			}
			if found != match {
				return fmt.Errorf("secret %s on pod %s: found=%v, expected=%v", secretKey, pod, found, match)
			}
		}
	}
	return nil
}

// VerifySecretsOnSecretObjects Compare secret value on passed in map to value present on secret object.
// Set match to true or false to indicate desired +ve or -ve match
func (testenv *TestCaseEnv) VerifySecretsOnSecretObjects(ctx context.Context, deployment *Deployment, secretObjectNames []string, data map[string][]byte, match bool) error {
	for _, secretName := range secretObjectNames {
		currentSecretData, err := GetSecretStruct(ctx, deployment, testenv.GetName(), secretName)
		if err != nil {
			return fmt.Errorf("unable to get secret struct %s: %w", secretName, err)
		}
		for secretKey, secretValue := range data {
			found := false
			secretValueOnSecretObject := currentSecretData.Data[secretKey]
			comparison := bytes.Compare(secretValueOnSecretObject, secretValue)
			if comparison == 0 {
				testenv.Log.Info("Secret Values on Secret Object Match", "matchExpected", match, "secretObjectName", secretName, "secretKey", secretKey, "givenValue", string(secretValue), "foundValue", string(secretValueOnSecretObject))
				found = true
			} else {
				testenv.Log.Info("Secret Values on Secret Object DONOT match", "matchExpected", match, "secretObjectName", secretName, "secretKey", secretKey, "givenValue", string(secretValue), "foundValue", string(secretValueOnSecretObject))
			}
			if found != match {
				return fmt.Errorf("secret %s on object %s: found=%v, expected=%v", secretKey, secretName, found, match)
			}
		}
	}
	return nil
}

// VerifySplunkServerConfSecrets Compare secret value on passed in map to value present on server.conf for given pods and secrets
// Set match to true or false to indicate desired +ve or -ve match
func (testenv *TestCaseEnv) VerifySplunkServerConfSecrets(ctx context.Context, deployment *Deployment, verificationPods []string, data map[string][]byte, match bool) error {
	for _, podName := range verificationPods {
		keysToMatch := GetKeysToMatch(podName)
		testenv.Log.Info("Verificaton Keys Set", "podName", podName, "keysToCompare", keysToMatch)
		for _, secretName := range keysToMatch {
			found := false
			stanza := SecretKeytoServerConfStanza[secretName]
			_, value, err := GetSecretFromServerConf(ctx, deployment, podName, testenv.GetName(), "pass4SymmKey", stanza)
			if err != nil {
				return fmt.Errorf("secret %s not found in conf file on pod %s: %w", secretName, podName, err)
			}
			comparison := strings.Compare(value, string(data[secretName]))
			if comparison == 0 {
				testenv.Log.Info("Secret Values on server.conf Match", "matchExpected", match, "podName", podName, "secretKey", secretName, "givenValue", string(data[secretName]), "foundValue", value)
				found = true
			} else {
				testenv.Log.Info("Secret Values on server.conf DONOT MATCH", "matchExpected", match, "podName", podName, "secretKey", secretName, "givenValue", string(data[secretName]), "foundValue", value)
			}
			if found != match {
				return fmt.Errorf("secret %s on server.conf pod %s: found=%v, expected=%v", secretName, podName, found, match)
			}
		}
	}
	return nil
}

// VerifySplunkInputConfSecrets compares secret values on passed-in map to values present in input.conf for given Indexer or Standalone pods
// Set match to true or false to indicate desired +ve or -ve match
func (testenv *TestCaseEnv) VerifySplunkInputConfSecrets(ctx context.Context, deployment *Deployment, verificationPods []string, data map[string][]byte, match bool) error {
	secretName := "hec_token"
	for _, podName := range verificationPods {
		if strings.Contains(podName, "standalone") || strings.Contains(podName, "indexer") {
			found := false
			testenv.Log.Info("Key Verificaton", "podName", podName, "key", secretName)
			stanza := SecretKeytoServerConfStanza[secretName]
			_, value, err := GetSecretFromInputsConf(ctx, deployment, podName, testenv.GetName(), "token", stanza)
			if err != nil {
				return fmt.Errorf("secret %s not found in input.conf on pod %s: %w", secretName, podName, err)
			}
			comparison := strings.Compare(value, string(data[secretName]))
			if comparison == 0 {
				testenv.Log.Info("Secret Values on input.conf Match", "matchExpected", match, "podName", podName, "secretKey", secretName, "givenValue", string(data[secretName]), "foundValue", value)
				found = true
			} else {
				testenv.Log.Info("Secret Values on input.conf DONOT MATCH", "matchExpected", match, "podName", podName, "secretKey", secretName, "givenValue", string(data[secretName]), "foundValue", value)
			}
			if found != match {
				return fmt.Errorf("secret %s on input.conf pod %s: found=%v, expected=%v", secretName, podName, found, match)
			}
		}
	}
	return nil
}

// VerifySplunkSecretViaAPI check if keys can be used to access api i.e validate they are authentic
func (testenv *TestCaseEnv) VerifySplunkSecretViaAPI(ctx context.Context, deployment *Deployment, verificationPods []string, data map[string][]byte, match bool) error {
	var keysToMatch []string
	for _, podName := range verificationPods {
		if strings.Contains(podName, "standalone") || strings.Contains(podName, "indexer") {
			keysToMatch = []string{"password", "hec_token"}
		} else {
			keysToMatch = []string{"password"}
		}
		for _, secretName := range keysToMatch {
			validKey := false
			testenv.Log.Info("Key Verificaton", "podName", podName, "key", secretName)
			validKey = CheckSecretViaAPI(ctx, deployment, podName, secretName, string(data[secretName]))
			if validKey != match {
				return fmt.Errorf("secret %s via API on pod %s: valid=%v, expected=%v", secretName, podName, validKey, match)
			}
		}
	}
	return nil
}

// VerifyPVC verifies if PVC exists or not
func (testenv *TestCaseEnv) VerifyPVC(pvcName string, expectedToExist bool, verificationTimeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.TODO(), PollInterval, verificationTimeout, true, func(ctx context.Context) (bool, error) {
		pvcExists := false
		pvcsList := DumpGetPvcs(testenv.GetName())

		for i := 0; i < len(pvcsList); i++ {
			if strings.EqualFold(pvcsList[i], pvcName) {
				pvcExists = true
				break
			}
		}
		testenv.Log.Info("PVC Status Verified", "pvc", pvcName, "status", pvcExists, "expected", expectedToExist)
		return pvcExists == expectedToExist, nil
	})
}

// VerifyPVCsPerDeployment verifies for a given deployment if PVCs (etc and var) exists
func (testenv *TestCaseEnv) VerifyPVCsPerDeployment(deployment *Deployment, deploymentType string, instances int, expectedtoExist bool, verificationTimeout time.Duration) error {
	pvcKind := []string{"etc", "var"}
	for i := 0; i < instances; i++ {
		for _, pvcVolumeKind := range pvcKind {
			PvcName := fmt.Sprintf(PVCString, pvcVolumeKind, deployment.GetName(), deploymentType, i)
			if err := testenv.VerifyPVC(PvcName, expectedtoExist, verificationTimeout); err != nil {
				return err
			}
		}
	}
	return nil
}

// VerifyAppInstalled verify that app of specific version is installed. Method assumes that app is installed in all CR's in namespace
func (testenv *TestCaseEnv) VerifyAppInstalled(ctx context.Context, deployment *Deployment, ns string, pods []string, apps []string, versionCheck bool, statusCheck string, checkupdated bool, clusterWideInstall bool) error {
	// Fail-fast test: check first pod and first app before checking all pods
	if len(pods) > 0 && len(apps) > 0 {
		testenv.Log.Info("Running fail-fast test on first pod before checking all pods", "pod", pods[0], "app", apps[0])
		firstPod := pods[0]
		firstApp := apps[0]

		status, versionInstalled, err := GetPodAppStatus(ctx, deployment, firstPod, ns, firstApp, clusterWideInstall)
		if err != nil {
			return fmt.Errorf("test failed - app %s not accessible on pod %s: %w", firstApp, firstPod, err)
		}
		testenv.Log.Info("Test passed - app is accessible", "pod", firstPod, "app", firstApp, "status", status, "version", versionInstalled)
		testenv.Log.Info("Proceeding with full verification of all pods and apps")
	}

	for _, podName := range pods {
		for _, appName := range apps {
			// Poll per (pod, app) to tolerate transient mismatches just after install/bundle-push
			var lastErr error
			pollErr := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
				status, versionInstalled, err := GetPodAppStatus(ctx, deployment, podName, ns, appName, clusterWideInstall)
				testenv.Log.Info("App details", "app", appName, "status", status, "version", versionInstalled, "error", err)
				if err != nil {
					lastErr = fmt.Errorf("unable to get app status on pod %s: %w", podName, err)
					return false, nil
				}
				comparison := strings.EqualFold(status, statusCheck)
				//Check the app is installed on specific pods and un-installed on others for cluster-wide install
				var check bool
				if clusterWideInstall {
					if strings.Contains(podName, "-indexer-") || strings.Contains(podName, "-search-head-") {
						check = true
						testenv.Log.Info("App Install Check", "pod", podName, "app", appName, "expected", check, "found", comparison, "scope:cluster", clusterWideInstall)
						if comparison != check {
							lastErr = fmt.Errorf("app %s install check failed on pod %s: expected=%v, found=%v", appName, podName, check, comparison)
							return false, nil
						}
					}
				} else {
					// For local install check pods individually
					if strings.Contains(podName, "-indexer-") || strings.Contains(podName, "-search-head-") {
						check = false
					} else {
						check = true
					}
					testenv.Log.Info("App Install Check", "pod", podName, "app", appName, "expected", check, "found", comparison, "scope:cluster", clusterWideInstall)
					if comparison != check {
						lastErr = fmt.Errorf("app %s install check failed on pod %s: expected=%v, found=%v", appName, podName, check, comparison)
						return false, nil
					}
				}

				if versionCheck {
					// For clusterwide install do not check for versions on deployer and cluster-manager as the apps arent installed there
					if !(clusterWideInstall && (strings.Contains(podName, "-deployer-") || strings.Contains(podName, "-cluster-manager-") || strings.Contains(podName, "-"+splcommon.ClusterManager+"-"))) {
						var expectedVersion string
						if checkupdated {
							expectedVersion = AppInfo[appName]["V2"]
						} else {
							expectedVersion = AppInfo[appName]["V1"]
						}
						testenv.Log.Info("Verify app", "pod", podName, "app", appName, "expectedVersion", expectedVersion, "versionInstalled", versionInstalled, "updated", checkupdated)
						if versionInstalled != expectedVersion {
							lastErr = fmt.Errorf("app %s version mismatch on pod %s: expected=%s, found=%s", appName, podName, expectedVersion, versionInstalled)
							return false, nil
						}
					}
				}
				lastErr = nil
				return true, nil
			})
			if pollErr != nil {
				if lastErr != nil {
					return lastErr
				}
				return fmt.Errorf("timed out verifying app %s on pod %s: %w", appName, podName, pollErr)
			}
		}
	}
	return nil
}

// VerifyAppsCopied verify that apps are copied to correct location based on POD. Set checkAppDirectory false to verify app is not copied.
func (testenv *TestCaseEnv) VerifyAppsCopied(ctx context.Context, deployment *Deployment, pods []string, apps []string, checkAppDirectory bool, scope string) error {

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
		if err := testenv.VerifyAppsInFolder(ctx, deployment, podName, apps, path, checkAppDirectory); err != nil {
			return err
		}
	}
	return nil
}

// VerifyAppsInFolder verify that apps are present in folder. Set checkAppDirectory false to verify app is not copied.
func (testenv *TestCaseEnv) VerifyAppsInFolder(ctx context.Context, deployment *Deployment, podName string, apps []string, path string, checkAppDirectory bool) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, AppInstallTimeout, true, func(ctx context.Context) (bool, error) {
		// Using checkAppDirectory here to get all files in case of negative check.  GetDirsOrFilesInPath  will return files/directory when checkAppDirecotry is FALSE
		appList, err := GetDirsOrFilesInPath(ctx, deployment, podName, path, checkAppDirectory)
		if err != nil {
			return false, fmt.Errorf("unable to get apps on pod %s: %w", podName, err)
		}
		for _, app := range apps {
			folderName := app + "/"
			found := CheckStringInSlice(appList, folderName)
			testenv.Log.Info("App check", "pod", podName, "folderName", folderName, "path", path, "status", found)
			if found != checkAppDirectory {
				return false, nil
			}
		}
		return true, nil
	})
}

// VerifyAppsDownloadedOnContainer verify that apps are downloaded by init container
func (testenv *TestCaseEnv) VerifyAppsDownloadedOnContainer(ctx context.Context, deployment *Deployment, pods []string, apps []string, path string) error {

	for _, podName := range pods {
		appList, err := GetDirsOrFilesInPath(ctx, deployment, podName, path, false)
		if err != nil {
			return fmt.Errorf("unable to get apps on pod %s: %w", podName, err)
		}
		for _, app := range apps {
			found := CheckStringInSlice(appList, app)
			testenv.Log.Info("Check App files present on the pod", "podName", podName, "appName", app, "directory", path, "status", found)
			if !found {
				return fmt.Errorf("app %s not found on pod %s in path %s", app, podName, path)
			}
		}
	}
	return nil
}

// VerifyAppsPackageDeletedOnOperatorContainer verify that apps are deleted by container
func (testenv *TestCaseEnv) VerifyAppsPackageDeletedOnOperatorContainer(ctx context.Context, deployment *Deployment, pods []string, apps []string, path string) error {
	for _, podName := range pods {
		for _, app := range apps {
			err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
				appList, err := GetOperatorDirsOrFilesInPath(ctx, deployment, podName, path, false)
				if err != nil {
					testenv.Log.Error(err, "Unable to get apps on operator pod", "pod", podName)
					return false, nil
				}
				found := CheckStringInSlice(appList, app+"_")
				testenv.Log.Info(fmt.Sprintf("Check App package deleted on the pod %s. App Name %s. Directory %s, Status %t", podName, app, path, found))
				return !found, nil
			})
			if err != nil {
				return fmt.Errorf("app package %s not deleted on operator pod %s: %w", app, podName, err)
			}
		}
	}
	return nil
}

// VerifyAppsPackageDeletedOnContainer verify that apps are deleted by container
func (testenv *TestCaseEnv) VerifyAppsPackageDeletedOnContainer(ctx context.Context, deployment *Deployment, pods []string, apps []string, path string) error {
	for _, podName := range pods {
		for _, app := range apps {
			err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
				appList, err := GetDirsOrFilesInPath(ctx, deployment, podName, path, false)
				if err != nil {
					testenv.Log.Error(err, "Unable to get apps on pod", "pod", podName)
					return false, nil
				}
				found := CheckStringInSlice(appList, app+"_")
				testenv.Log.Info(fmt.Sprintf("Check App package deleted on the pod %s. App Name %s. Directory %s, Status %t", podName, app, path, found))
				return !found, nil
			})
			if err != nil {
				return fmt.Errorf("app package %s not deleted on pod %s: %w", app, podName, err)
			}
		}
	}
	return nil
}

// VerifyAppListPhase verify given app Phase has completed for the given list of apps for given CR Kind
func (testenv *TestCaseEnv) VerifyAppListPhase(ctx context.Context, deployment *Deployment, name string, crKind string, appSourceName string, phase enterpriseApi.AppPhaseType, appList []string) error {
	if phase == enterpriseApi.PhaseDownload || phase == enterpriseApi.PhasePodCopy {
		for _, appName := range appList {
			testenv.Log.Info(fmt.Sprintf("Check App Status for CR %s NAME %s APP NAME %s Expected Phase not to be %s", crKind, name, appName, phase))
			err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
				appDeploymentInfo, err := testenv.GetAppDeploymentInfo(ctx, deployment, name, crKind, appSourceName, appName)
				if err != nil {
					testenv.Log.Error(err, "Failed to get app deployment info")
					return false, nil // Continue polling
				}
				if appDeploymentInfo.AppName == "" {
					testenv.Log.Info(fmt.Sprintf("App deployment info not found yet for app %s (CR %s/%s, AppSource %s), continuing to poll", appName, crKind, name, appSourceName))
					return false, nil // Continue polling
				}
				testenv.Log.Info(fmt.Sprintf("App State found for CR %s NAME %s APP NAME %s Expected Phase should not be %s", crKind, name, appName, phase), "actualPhase", appDeploymentInfo.PhaseInfo.Phase, "appState", appDeploymentInfo)
				return appDeploymentInfo.PhaseInfo.Phase != phase, nil
			})
			if err != nil {
				return fmt.Errorf("app %s on CR %s/%s did not move past phase %s: %w", appName, crKind, name, phase, err)
			}
		}
	} else {
		for _, appName := range appList {
			testenv.Log.Info(fmt.Sprintf("Check App Status for CR %s NAME %s APP NAME %s Expected Phase %s", crKind, name, appName, phase))
			err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
				appDeploymentInfo, err := testenv.GetAppDeploymentInfo(ctx, deployment, name, crKind, appSourceName, appName)
				if err != nil {
					testenv.Log.Error(err, "Failed to get app deployment info")
					return false, nil // Continue polling
				}
				if appDeploymentInfo.AppName == "" {
					testenv.Log.Info(fmt.Sprintf("App deployment info not found yet for app %s (CR %s/%s, AppSource %s), continuing to poll", appName, crKind, name, appSourceName))
					return false, nil // Continue polling
				}
				testenv.Log.Info(fmt.Sprintf("App State found for CR %s NAME %s APP NAME %s Expected Phase %s", crKind, name, appName, phase), "actualPhase", appDeploymentInfo.PhaseInfo.Phase, "appPhaseStatus", appDeploymentInfo.PhaseInfo.Status, "appState", appDeploymentInfo)
				if appDeploymentInfo.PhaseInfo.Status != enterpriseApi.AppPkgInstallComplete {
					testenv.Log.Info("Phase Install Not Complete.", "phaseFound", appDeploymentInfo.PhaseInfo.Phase, "phaseStatusFound", appDeploymentInfo.PhaseInfo.Status)
					return false, nil
				}
				return appDeploymentInfo.PhaseInfo.Phase == phase, nil
			})
			if err != nil {
				return fmt.Errorf("app %s on CR %s/%s did not reach phase %s: %w", appName, crKind, name, phase, err)
			}
		}
	}
	return nil
}

// VerifyAppState verify given app state is in between states passed as parameters, i.e when Status is between 101 and 303 we would pass enterpriseApi.AppPkgInstallComplete and enterpriseApi.AppPkgPodCopyComplete
func (testenv *TestCaseEnv) VerifyAppState(ctx context.Context, deployment *Deployment, name string, crKind string, appSourceName string, appList []string, appStateFinal enterpriseApi.AppPhaseStatusType, appStateInitial enterpriseApi.AppPhaseStatusType, timeout time.Duration) error {
	for _, appName := range appList {
		err := wait.PollUntilContextTimeout(ctx, PollInterval, timeout, true, func(ctx context.Context) (bool, error) {
			appDeploymentInfo, _ := testenv.GetAppDeploymentInfo(ctx, deployment, name, crKind, appSourceName, appName)
			status := appDeploymentInfo.PhaseInfo.Status
			// Replaces gomega.BeNumerically("~", appStateFinal, appStateInitial).
			// Cast to int32 to avoid uint32 underflow when status < appStateFinal.
			diff := int32(status) - int32(appStateFinal)
			if diff < 0 {
				diff = -diff
			}
			return diff <= int32(appStateInitial), nil
		})
		if err != nil {
			return fmt.Errorf("app %s state not in expected range: %w", appName, err)
		}
	}
	return nil
}

// WaitForAppInstall waits until an app is correctly installed (having status equal to 303)
func (testenv *TestCaseEnv) WaitForAppInstall(ctx context.Context, deployment *Deployment, name string, crKind string, appSourceName string, appList []string) error {
	for _, appName := range appList {
		err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
			appDeploymentInfo, _ := testenv.GetAppDeploymentInfo(ctx, deployment, name, crKind, appSourceName, appName)
			return appDeploymentInfo.PhaseInfo.Status == enterpriseApi.AppPkgInstallComplete, nil
		})
		if err != nil {
			return fmt.Errorf("app %s did not reach install complete status: %w", appName, err)
		}
	}
	return nil
}

// VerifyPodsInMCConfigMap checks if given pod names are present in given KEY of given MC's Config Map
func (testenv *TestCaseEnv) VerifyPodsInMCConfigMap(ctx context.Context, deployment *Deployment, pods []string, key string, mcName string, expected bool) error {
	// Get contents of MC config map
	mcConfigMap, err := GetMCConfigMap(ctx, deployment, testenv.GetName(), mcName)
	if err != nil {
		return fmt.Errorf("unable to get MC config map: %w", err)
	}
	for _, podName := range pods {
		testenv.Log.Info("Checking for POD on MC Config Map", "podName", podName, "data", mcConfigMap.Data)
		found := CheckPodNameInString(podName, mcConfigMap.Data[key])
		if found != expected {
			return fmt.Errorf("verify pod in MC Config Map failed: pod %s, found=%v, expected=%v", podName, found, expected)
		}
	}
	return nil
}

// VerifyPodsInMCConfigString checks if given pod names are present in given KEY of given MC's Config Map
func (testenv *TestCaseEnv) VerifyPodsInMCConfigString(ctx context.Context, pods []string, mcName string, expected bool, checkPodIP bool) error {
	for _, podName := range pods {
		testenv.Log.Info("Checking pod configured in MC POD Peers String", "podName", podName)
		var found bool
		if checkPodIP {
			podIP := GetPodIP(testenv.GetName(), podName)
			found = CheckPodNameOnMC(testenv.GetName(), mcName, podIP)
		} else {
			found = CheckPodNameOnMC(testenv.GetName(), mcName, podName)
		}
		if found != expected {
			return fmt.Errorf("verify pod in MC Config String failed: pod %s, found=%v, expected=%v", podName, found, expected)
		}
	}
	return nil
}

// VerifyClusterManagerBundlePush verify that bundle push was pushed on all indexers
func (testenv *TestCaseEnv) VerifyClusterManagerBundlePush(ctx context.Context, deployment *Deployment, replicas int, previousBundleHash string) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		// Get Bundle status and check that each pod has successfully deployed the latest bundle
		cmEndpoint := "cmanager"
		if strings.Contains(deployment.GetName(), "master") {
			cmEndpoint = "cmaster"
		}
		clusterManagerBundleStatus := CMBundlePushstatus(ctx, deployment, previousBundleHash, cmEndpoint)
		if len(clusterManagerBundleStatus) < replicas {
			testenv.Log.Info("Bundle push on Pod not complete on all pods", "podWithBundlePush", clusterManagerBundleStatus)
			return false, nil
		}
		clusterPodNames := DumpGetPods(testenv.GetName())

		for _, podName := range clusterPodNames {
			if strings.Contains(podName, "-indexer-") {
				if _, present := clusterManagerBundleStatus[podName]; present {
					if clusterManagerBundleStatus[podName] != "Up" {
						testenv.Log.Info("Bundle push on Pod not complete", "podName", podName, "status", clusterManagerBundleStatus[podName])
						return false, nil
					}
				} else {
					testenv.Log.Info("Bundle push not found on pod", "podName", podName)
					return false, nil
				}
			}
		}
		return true, nil
	})
}

// VerifyDeployerBundlePush verify that bundle push was pushed on all search heads
func (testenv *TestCaseEnv) VerifyDeployerBundlePush(ctx context.Context, deployment *Deployment, ns string, replicas int) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		deployerAppPushStatus := DeployerBundlePushstatus(ctx, deployment, ns)
		if len(deployerAppPushStatus) == 0 {
			testenv.Log.Info("Bundle push not complete on all pods")
			DumpGetPods(testenv.GetName())

			return false, nil
		}
		for appName, val := range deployerAppPushStatus {
			if val < replicas {
				testenv.Log.Info("Bundle push not complete on all pods for", "appName", appName, "replicasWithBundlePush", val, "expectedReplicas", replicas)
				DumpGetPods(testenv.GetName())

				return false, nil
			}
		}
		return true, nil
	})
}

// VerifyNoPodResetByUID verify that no pod reset during App install by comparing pod UIDs
func (testenv *TestCaseEnv) VerifyNoPodResetByUID(ctx context.Context, podUIDMap map[string]string, podToSkip []string) error {
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
					if currentUID != previousUID {
						return fmt.Errorf("pod reset was detected. Pod Name %s. Current Pod UID %s. Previous Pod UID %s", podName, currentUID, previousUID)
					}
				}
			}
		}
	}
	return nil
}

// WaitForSplunkPodCleanup Wait for cleanup to happen
func (testenv *TestCaseEnv) WaitForSplunkPodCleanup(ctx context.Context, deployment *Deployment) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		testenv.Log.Info("Waiting for Splunk Pods to be deleted before running test")
		return len(DumpGetPods(testenv.GetName())) == 0, nil
	})
}

// WaitforAppInstallState Wait for App to reach state specified in conf file
func (testenv *TestCaseEnv) WaitforAppInstallState(ctx context.Context, deployment *Deployment, podNames []string, ns string, appName string, newState string, clusterWideInstall bool) error {
	testenv.Log.Info("Retrieve App state on pod")
	for _, podName := range podNames {
		err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
			status, _, err := GetPodAppStatus(ctx, deployment, podName, ns, appName, clusterWideInstall)
			testenv.Log.Info("App details", "app", appName, "status", status, "error", err, "podName", podName)
			return status == strings.ToUpper(newState), nil
		})
		if err != nil {
			return fmt.Errorf("app %s did not reach state %s on pod %s: %w", appName, newState, podName, err)
		}
	}
	return nil
}

// VerifyAppRepoState verify given app repo state is equal to given value for app for given CR Kind
func (testenv *TestCaseEnv) VerifyAppRepoState(ctx context.Context, deployment *Deployment, name string, crKind string, appSourceName string, repoValue int, appName string) error {
	testenv.Log.Info("Check for app repo state in CR")
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		appDeploymentInfo, err := testenv.GetAppDeploymentInfo(ctx, deployment, name, crKind, appSourceName, appName)
		if err != nil {
			testenv.Log.Error(err, "Failed to get app deployment info")
			return false, nil
		}
		testenv.Log.Info(fmt.Sprintf("App State found for CR %s NAME %s APP NAME %s Expected repo value %d", crKind, name, appName, repoValue), "actualValue", appDeploymentInfo.RepoState, "appState", appDeploymentInfo)
		return int(appDeploymentInfo.RepoState) == repoValue, nil
	})
}

// VerifyIsDeploymentInProgressFlagIsSet verify IsDeploymentInProgress flag is set to true
func (testenv *TestCaseEnv) VerifyIsDeploymentInProgressFlagIsSet(ctx context.Context, deployment *Deployment, name string, crKind string) error {
	testenv.Log.Info("Check IsDeploymentInProgress Flag is set", "crName", name, "crKind", crKind)
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		isDeploymentInProgress, err := testenv.GetIsDeploymentInProgressFlag(ctx, deployment, name, crKind)
		if err != nil {
			testenv.Log.Error(err, "Failed to get isDeploymentInProgress Flag")
			return false, nil
		}
		testenv.Log.Info("IsDeploymentInProgress Flag status found", "crName", name, "crKind", crKind, "isDeploymentInProgress", isDeploymentInProgress)
		return isDeploymentInProgress, nil
	})
}

// VerifyFilesInDirectoryOnPod verify that files are present in folder.
func (testenv *TestCaseEnv) VerifyFilesInDirectoryOnPod(ctx context.Context, deployment *Deployment, podNames []string, files []string, path string, checkDirectory bool, checkPresent bool) error {
	for _, podName := range podNames {
		err := wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
			// Using checkDirectory here to get all files in case of negative check.  GetDirsOrFilesInPath  will return files/directory when checkDirecotry is FALSE
			filelist, err := GetDirsOrFilesInPath(ctx, deployment, podName, path, checkDirectory)
			if err != nil {
				return false, fmt.Errorf("unable to get files on pod %s: %w", podName, err)
			}
			for _, file := range files {
				found := CheckStringInSlice(filelist, file)
				testenv.Log.Info("File check", "pod", podName, "filename", file, "path", path, "status", found)
				if found != checkPresent {
					return false, nil
				}
			}
			return true, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
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
func (testenv *TestCaseEnv) VerifyTelemetry(ctx context.Context, deployment *Deployment, prevVal string) error {
	testenv.Log.Info("VerifyTelemetry: start")
	return wait.PollUntilContextTimeout(ctx, PollInterval, deployment.GetTimeout(), true, func(ctx context.Context) (bool, error) {
		currentVal := testenv.GetTelemetryLastSubmissionTime(ctx, deployment)
		if currentVal != "" && currentVal != prevVal {
			testenv.Log.Info("VerifyTelemetry: success", "previous", prevVal, "current", currentVal)
			return true, nil
		}
		return false, nil
	})
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
func (testenv *TestCaseEnv) WaitForPodsInMCConfigString(ctx context.Context, pods []string, mcName string, expected bool, checkPodIP bool, timeout time.Duration) error {
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
		searchResultsResp, err := PerformSearchSync(ctx, deployment, podName, searchString)
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

			appDeploymentInfo, err := testenv.GetAppDeploymentInfo(ctx, deployment, crName, crKind, appSourceName, lookupAppName)
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

// VerifyC3ClusterPVCs verifies that PVCs for SHC, Deployer, Indexers, and Cluster Manager exist or are deleted.
func VerifyC3ClusterPVCs(testcaseEnvInst *TestCaseEnv, deployment *Deployment, clusterManagerType string, exists bool, timeout time.Duration) error {
	if err := testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-search-head", 3, exists, timeout); err != nil {
		return err
	}
	if err := testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-deployer", 1, exists, timeout); err != nil {
		return err
	}
	if err := testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "idxc-indexer", 3, exists, timeout); err != nil {
		return err
	}
	return testcaseEnvInst.VerifyPVCsPerDeployment(deployment, clusterManagerType, 1, exists, timeout)
}

// VerifyM4ClusterAndRFSF verifies Cluster Manager and multisite cluster are ready and RF/SF is met.
// When skipMultisiteStatus is true the VerifyIndexerClusterMultisiteStatus check is omitted
// (useful on second-round verification after an index addition where multisite topology is unchanged).
func VerifyM4ClusterAndRFSF(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *ClusterReadinessConfig, siteCount int, skipMultisiteStatus bool) error {
	if err := config.ClusterManagerReady(ctx, deployment, testcaseEnvInst); err != nil {
		return err
	}
	if err := testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount); err != nil {
		return err
	}
	if !skipMultisiteStatus {
		if err := testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount); err != nil {
			return err
		}
	}
	if err := testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment); err != nil {
		return err
	}
	return testcaseEnvInst.VerifyRFSFMet(ctx, deployment)
}

// VerifyLMAppsOnPod verifies that apps are copied and installed on the License Manager pod.
// The updated flag controls whether apps are expected to be updated versions.
func VerifyLMAppsOnPod(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, testenvInstance *TestEnv, podName []string, appList []string, updated bool) error {
	if err := testcaseEnvInst.VerifyAppsCopied(ctx, deployment, podName, appList, true, enterpriseApi.ScopeLocal); err != nil {
		return err
	}
	return testcaseEnvInst.VerifyAppInstalled(ctx, deployment, testcaseEnvInst.GetName(), podName, appList, updated, "enabled", updated, false)
}

// VerifyLMConfiguredOnCluster verifies that the License Manager is configured on
// the given indexer pods, on search head pods, and on the Monitoring Console.
func VerifyLMConfiguredOnCluster(ctx context.Context, deployment *Deployment, indexerPods []string) error {
	shPods := GeneratePodNameSlice(SearchHeadPod, deployment.GetName(), 3, false, 0)
	if err := VerifyLMConfiguredOnPods(ctx, deployment, append(indexerPods, shPods...)); err != nil {
		return err
	}
	return VerifyLMConfiguredOnMC(ctx, deployment)
}

// VerifyMCConfigForCluster verifies that the CM, deployer, search heads, and indexers
// are all correctly registered in the MC config map and pod config string.
// It uses the service name and URL key from the MCVersionConfig so it works for both V3 and V4.
func VerifyMCConfigForCluster(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv,
	cfg MCVersionConfig, mcName string, shPods, indexerPods []string) error {
	if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(cfg.CMServiceNameFmt, deployment.GetName())}, cfg.CMURLKey, mcName, true); err != nil {
		return err
	}
	if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true); err != nil {
		return err
	}
	if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true); err != nil {
		return err
	}
	if err := testcaseEnvInst.VerifyPodsInMCConfigString(ctx, shPods, mcName, true, false); err != nil {
		return err
	}
	return testcaseEnvInst.VerifyPodsInMCConfigString(ctx, indexerPods, mcName, true, true)
}

// VerifyStandalonePodsInMC verifies that the given standalone pods are present (or absent) in the
// MC config map and pod config string.
func VerifyStandalonePodsInMC(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, pods []string, mcName string, shouldExist bool) error {
	if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, pods, "SPLUNK_STANDALONE_URL", mcName, shouldExist); err != nil {
		return err
	}
	return testcaseEnvInst.VerifyPodsInMCConfigString(ctx, pods, mcName, shouldExist, false)
}

// VerifyMCTwoAfterCMReconfig verifies that MC Two is correctly configured after the Cluster Manager
// has been reconfigured to point to it: CM and indexers should be present, SH should be absent.
// If checkDeployerAbsent is true, also verifies deployer is absent on MC Two (used in C3 tests).
func VerifyMCTwoAfterCMReconfig(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv,
	params MCReconfigParams, mcTwoName string, shPods, indexerPods []string, checkDeployerAbsent bool) error {

	testcaseEnvInst.Log.Info("Verify CM in MC Two Config Map after CM Reconfig")
	if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}, params.CMURLKey, mcTwoName, true); err != nil {
		return err
	}

	testcaseEnvInst.Log.Info("Verify Indexers in MC Two Config String after CM Reconfig")
	if err := testcaseEnvInst.VerifyPodsInMCConfigString(ctx, indexerPods, mcTwoName, true, true); err != nil {
		return err
	}

	if checkDeployerAbsent {
		testcaseEnvInst.Log.Info("Verify Deployer NOT in MC Two Config Map after CM Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
			[]string{fmt.Sprintf(DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcTwoName, false); err != nil {
			return err
		}
	}

	testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC Two Config Map after CM Reconfig")
	if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, false); err != nil {
		return err
	}

	testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC Two Config String after CM Reconfig")
	return testcaseEnvInst.VerifyPodsInMCConfigString(ctx, shPods, mcTwoName, false, false)
}

// VerifyMCOneAfterCMReconfig verifies that MC One is correctly configured after the Cluster Manager
// has been reconfigured away from it: CM should be absent, SH should still be present.
// If checkDeployerPresent is true, also verifies deployer is still present on MC One (used in M4 tests).
func VerifyMCOneAfterCMReconfig(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv,
	params MCReconfigParams, mcName string, mc *enterpriseApi.MonitoringConsole, shPods []string, checkDeployerPresent bool) error {

	if err := testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc); err != nil {
		return err
	}

	testcaseEnvInst.Log.Info("Verify CM NOT in MC One Config Map after CM Reconfig")
	if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
		[]string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}, params.CMURLKey, mcName, false); err != nil {
		return err
	}

	// CSPL-619: Indexer verification on MC One is commented out in all test variants

	if checkDeployerPresent {
		testcaseEnvInst.Log.Info("Verify Deployer still in MC One Config Map after CM Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment,
			[]string{fmt.Sprintf(DeployerServiceName, deployment.GetName())}, "SPLUNK_DEPLOYER_URL", mcName, true); err != nil {
			return err
		}
	}

	testcaseEnvInst.Log.Info("Verify SH Pods still in MC One Config Map after CM Reconfig")
	if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, true); err != nil {
		return err
	}

	testcaseEnvInst.Log.Info("Verify SH Pods still in MC One Config String after CM Reconfig")
	return testcaseEnvInst.VerifyPodsInMCConfigString(ctx, shPods, mcName, true, false)
}

// VerifyMCTwoAfterSHCReconfig verifies that MC Two has all components (CM, deployer, SH, indexers)
// after the SHC has been reconfigured to point to it.
// If timeout > 0, uses WaitForPodsInMCConfigString; otherwise uses direct VerifyPodsInMCConfigString.
func VerifyMCTwoAfterSHCReconfig(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv,
	params MCReconfigParams, mcTwoName string, shPods, indexerPods []string, timeout time.Duration) error {

	cmService := []string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}
	deployerService := []string{fmt.Sprintf(DeployerServiceName, deployment.GetName())}

	if timeout > 0 {
		testcaseEnvInst.Log.Info("Verify CM in MC Two Config Map after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, cmService, params.CMURLKey, mcTwoName, true, timeout); err != nil {
			return fmt.Errorf("timed out waiting for CM in MC two config map after SHC reconfig: %w", err)
		}

		testcaseEnvInst.Log.Info("Verify Deployer in MC Two Config Map after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, deployerService, "SPLUNK_DEPLOYER_URL", mcTwoName, true, timeout); err != nil {
			return fmt.Errorf("timed out waiting for deployer in MC two config map after SHC reconfig: %w", err)
		}

		testcaseEnvInst.Log.Info("Verify SH Pods in MC Two Config Map after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, true, timeout); err != nil {
			return fmt.Errorf("timed out waiting for search heads in MC two config map after SHC reconfig: %w", err)
		}

		testcaseEnvInst.Log.Info("Verify SH Pods in MC Two Config String after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigString(ctx, shPods, mcTwoName, true, false, timeout); err != nil {
			return fmt.Errorf("timed out waiting for search heads in MC two config after SHC reconfig: %w", err)
		}

		testcaseEnvInst.Log.Info("Verify Indexers in MC Two Config String after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigString(ctx, indexerPods, mcTwoName, true, true, timeout); err != nil {
			return fmt.Errorf("timed out waiting for indexers in MC two config after SHC reconfig: %w", err)
		}
	} else {
		testcaseEnvInst.Log.Info("Verify CM in MC Two Config Map after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, cmService, params.CMURLKey, mcTwoName, true); err != nil {
			return err
		}

		testcaseEnvInst.Log.Info("Verify Deployer in MC Two Config Map after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, deployerService, "SPLUNK_DEPLOYER_URL", mcTwoName, true); err != nil {
			return err
		}

		testcaseEnvInst.Log.Info("Verify SH Pods in MC Two Config Map after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcTwoName, true); err != nil {
			return err
		}

		testcaseEnvInst.Log.Info("Verify SH Pods in MC Two Config String after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigString(ctx, shPods, mcTwoName, true, false); err != nil {
			return err
		}

		testcaseEnvInst.Log.Info("Verify Indexers in MC Two Config String after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigString(ctx, indexerPods, mcTwoName, true, true); err != nil {
			return err
		}
	}
	return nil
}

// VerifyMCOneAfterSHCReconfig verifies that MC One has lost all components (CM, deployer, SH)
// after the SHC has been reconfigured away from it.
// If timeout > 0, uses WaitForPodsInMCConfigString; otherwise uses direct VerifyPodsInMCConfigString.
func VerifyMCOneAfterSHCReconfig(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv,
	params MCReconfigParams, mcName string, mc *enterpriseApi.MonitoringConsole, shPods []string, timeout time.Duration) error {

	if err := testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc); err != nil {
		return err
	}

	cmService := []string{fmt.Sprintf(params.CMServiceNameFmt, deployment.GetName())}
	deployerService := []string{fmt.Sprintf(DeployerServiceName, deployment.GetName())}

	if timeout > 0 {
		testcaseEnvInst.Log.Info("Verify CM NOT in MC One Config Map after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, cmService, params.CMURLKey, mcName, false, timeout); err != nil {
			return fmt.Errorf("timed out waiting for CM to be removed from MC one config map after SHC reconfig: %w", err)
		}

		testcaseEnvInst.Log.Info("Verify Deployer NOT in MC One Config Map after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, deployerService, "SPLUNK_DEPLOYER_URL", mcName, false, timeout); err != nil {
			return fmt.Errorf("timed out waiting for deployer to be removed from MC one config map after SHC reconfig: %w", err)
		}

		testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC One Config Map after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, false, timeout); err != nil {
			return fmt.Errorf("timed out waiting for search heads to be removed from MC one config map after SHC reconfig: %w", err)
		}

		testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC One Config String after SHC Reconfig (with wait)")
		if err := testcaseEnvInst.WaitForPodsInMCConfigString(ctx, shPods, mcName, false, false, timeout); err != nil {
			return fmt.Errorf("timed out waiting for search heads to be removed from MC one config after SHC reconfig: %w", err)
		}
	} else {
		testcaseEnvInst.Log.Info("Verify CM NOT in MC One Config Map after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, cmService, params.CMURLKey, mcName, false); err != nil {
			return err
		}

		testcaseEnvInst.Log.Info("Verify Deployer NOT in MC One Config Map after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, deployerService, "SPLUNK_DEPLOYER_URL", mcName, false); err != nil {
			return err
		}

		testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC One Config Map after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, false); err != nil {
			return err
		}

		testcaseEnvInst.Log.Info("Verify SH Pods NOT in MC One Config String after SHC Reconfig")
		if err := testcaseEnvInst.VerifyPodsInMCConfigString(ctx, shPods, mcName, false, false); err != nil {
			return err
		}
	}

	// CSPL-619: Indexer verification on MC One is commented out in all test variants
	return nil
}

// VerifySecretsPropagated checks that the given secret data has been propagated to all
// versioned secret objects, pods, server config, input config, and via the API.
func VerifySecretsPropagated(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, secretData map[string][]byte, updated bool) error {
	// Once Pods are READY check each versioned secret for updated secret keys
	secretObjectNames := GetVersionedSecretNames(testcaseEnvInst.GetName(), 2)

	// Verify Secrets on versioned secret objects
	if err := testcaseEnvInst.VerifySecretsOnSecretObjects(ctx, deployment, secretObjectNames, secretData, updated); err != nil {
		return err
	}

	// Once Pods are READY check each pod for updated secret keys
	verificationPods := DumpGetPods(testcaseEnvInst.GetName())

	// Verify secrets on pods
	if err := testcaseEnvInst.VerifySecretsOnPods(ctx, deployment, verificationPods, secretData, updated); err != nil {
		return err
	}

	// Verify Secrets on ServerConf on Pod
	if err := testcaseEnvInst.VerifySplunkServerConfSecrets(ctx, deployment, verificationPods, secretData, updated); err != nil {
		return err
	}

	// Verify Hec token on InputConf on Pod
	if err := testcaseEnvInst.VerifySplunkInputConfSecrets(ctx, deployment, verificationPods, secretData, updated); err != nil {
		return err
	}

	// Verify Secrets via api access on Pod
	return testcaseEnvInst.VerifySplunkSecretViaAPI(ctx, deployment, verificationPods, secretData, updated)
}

// S1WithLMSetup holds the resources created by SetupS1WithLMAndMC so that
// individual test functions can operate on them without repeating the setup.
type S1WithLMSetup struct {
	Standalone                *enterpriseApi.Standalone
	Mc                        *enterpriseApi.MonitoringConsole
	ResourceVersion           string
	NamespaceScopedSecretName string
}

// SetupS1WithLMAndMC performs the common S1 setup shared by the secret-update
// and secret-delete tests: license config map, standalone with LM, MC, and
// initial secret verification.
func SetupS1WithLMAndMC(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *ClusterReadinessConfig) (S1WithLMSetup, error) {
	if err := SetupLicenseConfigMap(ctx, testcaseEnvInst); err != nil {
		return S1WithLMSetup{}, err
	}

	standalone, err := config.DeployStandaloneWithLM(ctx, deployment, deployment.GetName())
	if err != nil {
		return S1WithLMSetup{}, fmt.Errorf("unable to deploy standalone instance with LM: %w", err)
	}

	if err := VerifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, standalone); err != nil {
		return S1WithLMSetup{}, fmt.Errorf("LM or standalone not ready: %w", err)
	}

	mc, resourceVersion, err := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), deployment.GetName())
	if err != nil {
		return S1WithLMSetup{}, fmt.Errorf("unable to deploy Monitoring Console: %w", err)
	}

	namespaceScopedSecretName := fmt.Sprintf(NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	if _, err = GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName); err != nil {
		return S1WithLMSetup{}, fmt.Errorf("unable to get secret struct: %w", err)
	}

	return S1WithLMSetup{
		Standalone:                standalone,
		Mc:                        mc,
		ResourceVersion:           resourceVersion,
		NamespaceScopedSecretName: namespaceScopedSecretName,
	}, nil
}

// VerifyLMAndStandaloneReady waits for License Manager then Standalone to reach READY status.
func VerifyLMAndStandaloneReady(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *ClusterReadinessConfig, standalone *enterpriseApi.Standalone) error {
	if err := config.LicenseManagerReady(ctx, deployment, testcaseEnvInst); err != nil {
		return err
	}
	return testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)
}

// VerifyLMAndClusterManagerReady waits for License Manager then Cluster Manager to reach READY status.
func VerifyLMAndClusterManagerReady(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *ClusterReadinessConfig) error {
	if err := config.LicenseManagerReady(ctx, deployment, testcaseEnvInst); err != nil {
		return err
	}
	return config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)
}

// VerifyS1SecretChangeApplied verifies that a secret change (update or delete)
// has been applied to the S1 stack: standalone enters Updating phase, LM and
// standalone return to Ready, MC version changes, and secrets are propagated.
func VerifyS1SecretChangeApplied(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *ClusterReadinessConfig, setup S1WithLMSetup, secretData map[string][]byte, updated bool) error {
	if err := testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseUpdating); err != nil {
		return err
	}
	if err := VerifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config, setup.Standalone); err != nil {
		return err
	}
	if err := testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, setup.Mc, setup.ResourceVersion); err != nil {
		return err
	}
	return VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, secretData, updated)
}

// VerifyPostSecretChangeCluster performs the common tail verification after a
// secret change on a clustered deployment: MC version changed, RF/SF met, and
// secrets propagated to all pods.
func VerifyPostSecretChangeCluster(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, mc *enterpriseApi.MonitoringConsole, resourceVersion string, updatedSecretData map[string][]byte) error {
	if err := testcaseEnvInst.VerifyMCVersionChangedAndReady(ctx, deployment, mc, resourceVersion); err != nil {
		return err
	}

	testcaseEnvInst.Log.Info("Checking RF SF after secret change")
	if err := testcaseEnvInst.VerifyRFSFMet(ctx, deployment); err != nil {
		return err
	}

	return VerifySecretsPropagated(ctx, deployment, testcaseEnvInst, updatedSecretData, true)
}

// VerifyConfFileContent retrieves a conf file from a pod and validates its content.
func VerifyConfFileContent(pod, confPath, deploymentName string, expectedContent []string, errorMsg string) error {
	conf, err := GetConfFile(pod, confPath, deploymentName)
	if err != nil {
		return fmt.Errorf("%s: %w", errorMsg, err)
	}
	return ValidateContent(conf, expectedContent, true)
}

// ApplySecretUpdateAndVerifyCMUpdating deploys MC, verifies RF/SF and initial secret state,
// applies a secret update, and confirms the Cluster Manager enters the Updating phase.
// Returns the MC, its resource version, and the updated secret data for post-change verification.
func ApplySecretUpdateAndVerifyCMUpdating(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *ClusterReadinessConfig) (*enterpriseApi.MonitoringConsole, string, map[string][]byte, error) {
	mc, resourceVersion, err := testcaseEnvInst.DeployMCAndGetVersion(ctx, deployment, deployment.GetName(), deployment.GetName())
	if err != nil {
		return nil, "", nil, fmt.Errorf("unable to deploy Monitoring Console: %w", err)
	}
	testcaseEnvInst.Log.Info("Checking RF SF before secret change")
	if err := testcaseEnvInst.VerifyRFSFMet(ctx, deployment); err != nil {
		return nil, "", nil, err
	}
	namespaceScopedSecretName := fmt.Sprintf(NamespaceScopedSecretObjectName, testcaseEnvInst.GetName())
	_, err = GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), namespaceScopedSecretName)
	if err != nil {
		return nil, "", nil, fmt.Errorf("unable to get secret struct: %w", err)
	}
	updatedSecretData, err := GenerateAndApplySecretUpdate(ctx, deployment, testcaseEnvInst, namespaceScopedSecretName)
	if err != nil {
		return nil, "", nil, fmt.Errorf("unable to generate and apply secret update: %w", err)
	}
	if err := config.VerifyClusterManagerPhaseUpdating(ctx, deployment, testcaseEnvInst); err != nil {
		return nil, "", nil, err
	}
	return mc, resourceVersion, updatedSecretData, nil
}

// WaitForDaemonSetPodsReady polls until every scheduled pod in the DaemonSet with the given
// name is ready (numberReady == desiredNumberScheduled and desiredNumberScheduled > 0).
func WaitForDaemonSetPodsReady(ctx context.Context, deployment *Deployment, namespace, dsName string) error {
	return wait.PollUntilContextTimeout(ctx, PollInterval, DefaultTimeout, true, func(ctx context.Context) (bool, error) {
		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dsName,
				Namespace: namespace,
			},
		}
		if err := deployment.testenv.GetKubeClient().Get(ctx, client.ObjectKeyFromObject(ds), ds); err != nil {
			return false, nil
		}
		desired := ds.Status.DesiredNumberScheduled
		ready := ds.Status.NumberReady
		return desired > 0 && ready == desired, nil
	})
}

// CountSearchResults runs a stats search that returns a single "count" field and returns the
// integer value. Returns (0, nil) when the search produces no results yet (non-fatal).
func CountSearchResults(ctx context.Context, deployment *Deployment, podName string, searchString string) (int, error) {
	resp, err := PerformSearchSync(ctx, deployment, podName, searchString)
	if err != nil {
		return 0, err
	}
	// The export endpoint streams one JSON object per line; we only need the first result line.
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]interface{}
		if jsonErr := json.Unmarshal([]byte(line), &row); jsonErr != nil {
			continue
		}
		result, ok := row["result"]
		if !ok {
			continue
		}
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			continue
		}
		countVal, ok := resultMap["count"]
		if !ok {
			continue
		}
		switch v := countVal.(type) {
		case string:
			n, convErr := strconv.Atoi(v)
			if convErr != nil {
				return 0, convErr
			}
			return n, nil
		case float64:
			return int(v), nil
		}
	}
	return 0, nil
}
