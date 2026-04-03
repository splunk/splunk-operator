// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package crcrud

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// RunS1CPUUpdateTest runs the standard S1 CPU limit update test workflow
func RunS1CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, defaultCPULimits string, newCPULimits string) {
	// Deploy and verify Standalone
	standalone, err := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance")

	// Verify telemetry
	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	Expect(testcaseEnvInst.TriggerAndVerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)).To(Succeed())

	// Deploy and verify Monitoring Console
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify CPU limits before updating the CR
	standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	Expect(testcaseEnvInst.VerifyCPULimits(deployment, standalonePodName, defaultCPULimits)).To(Succeed())

	// Change CPU limits to trigger CR update
	standalone.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	err = deployment.UpdateCR(ctx, standalone)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with updated CR ")

	// Verify Standalone is updating
	Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseUpdating)).To(Succeed())

	// Verify Standalone goes to ready state
	Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseReady)).To(Succeed())

	// Verify Monitoring Console is Ready and stays in ready state
	Expect(testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)).To(Succeed())

	// Verify CPU limits after updating the CR
	Expect(testcaseEnvInst.VerifyCPULimits(deployment, standalonePodName, newCPULimits)).To(Succeed())
}

// RunC3CPUUpdateTest runs the standard C3 CPU limit update test workflow
func RunC3CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, defaultCPULimits string, newCPULimits string) {
	// Deploy Single site Cluster and Search Head Clusters
	mcRef := deployment.GetName()
	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	Expect(config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, 3, true /*shc*/, mcRef)).To(Succeed(), "Unable to deploy C3 cluster")

	// Verify telemetry
	Expect(testcaseEnvInst.TriggerAndVerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)).To(Succeed())

	// Deploy and verify Monitoring Console, RF/SF
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")
	Expect(testcaseEnvInst.StandardC3Verification(ctx, deployment, mc)).To(Succeed())

	// Verify CPU limits on Indexers before updating the CR
	indexerCount := 3
	Expect(testcaseEnvInst.VerifyIndexerCPULimits(deployment, indexerCount, defaultCPULimits)).To(Succeed())

	// Change CPU limits to trigger CR update
	idxc := &enterpriseApi.IndexerCluster{}
	instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
	Expect(testenv.GetInstanceWithExpect(ctx, deployment, idxc, instanceName, "Unable to get instance of Indexer Cluster")).To(Succeed())
	idxc.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	Expect(testenv.UpdateCRWithExpect(ctx, deployment, idxc, "Unable to deploy Indexer Cluster with updated CR")).To(Succeed())

	// Verify Indexer Cluster is updating
	idxcName := deployment.GetName() + "-idxc"
	Expect(testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating, idxcName)).To(Succeed())

	// Verify Indexers go to ready state
	Expect(testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)).To(Succeed())

	// Verify CPU limits on Indexers after updating the CR
	Expect(testcaseEnvInst.VerifyIndexerCPULimits(deployment, indexerCount, newCPULimits)).To(Succeed())

	// Verify CPU limits on Search Heads before updating the CR
	searchHeadCount := 3
	Expect(testcaseEnvInst.VerifySearchHeadCPULimits(deployment, searchHeadCount, defaultCPULimits)).To(Succeed())

	// Change CPU limits to trigger CR update
	shc := &enterpriseApi.SearchHeadCluster{}
	instanceName = fmt.Sprintf("%s-shc", deployment.GetName())
	Expect(testenv.GetInstanceWithExpect(ctx, deployment, shc, instanceName, "Unable to fetch Search Head Cluster deployment")).To(Succeed())

	shc.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	Expect(testenv.UpdateCRWithExpect(ctx, deployment, shc, "Unable to deploy Search Head Cluster with updated CR")).To(Succeed())

	// Verify Search Head Cluster is updating
	Expect(testcaseEnvInst.VerifySearchHeadClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)).To(Succeed())

	// Verify Search Head go to ready state
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed())

	// Verify Monitoring Console is Ready and stays in ready state
	Expect(testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)).To(Succeed())

	// Verify CPU limits on Search Heads after updating the CR
	Expect(testcaseEnvInst.VerifySearchHeadCPULimits(deployment, searchHeadCount, newCPULimits)).To(Succeed())
}

// RunC3PVCDeletionTest runs the standard C3 PVC deletion test workflow
func RunC3PVCDeletionTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, verificationTimeout time.Duration) {
	// Deploy Single site Cluster and Search Head Clusters
	mcRef := deployment.GetName()
	Expect(config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, 3, true /*shc*/, mcRef)).To(Succeed(), "Unable to deploy C3 cluster")
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed())

	// Deploy and verify Monitoring Console
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcRef, "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	clusterManagerType := config.ClusterManagerPVCType()
	Expect(testenv.VerifyC3ClusterPVCs(testcaseEnvInst, deployment, clusterManagerType, true, verificationTimeout)).To(Succeed())

	// Delete the Search Head Cluster
	Expect(testenv.GetAndDeleteCR(ctx, deployment, &enterpriseApi.SearchHeadCluster{}, deployment.GetName()+"-shc")).To(Succeed(), "Unable to delete SHC instance")

	// Delete the Indexer Cluster
	Expect(testenv.GetAndDeleteCR(ctx, deployment, &enterpriseApi.IndexerCluster{}, deployment.GetName()+"-idxc")).To(Succeed(), "Unable to delete IDXC instance")

	// Delete the Cluster Manager (v3 or v4)
	Expect(config.DeleteClusterManager(ctx, deployment)).To(Succeed(), "Unable to delete Cluster Manager")

	// Delete Monitoring Console
	Expect(testenv.GetAndDeleteCR(ctx, deployment, mc, mcRef)).To(Succeed(), "Unable to delete Monitoring Console instance")

	Expect(testenv.VerifyC3ClusterPVCs(testcaseEnvInst, deployment, clusterManagerType, false, verificationTimeout)).To(Succeed())

	// Verify Monitoring Console PVCs (etc and var) have been deleted
	Expect(testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "monitoring-console", 1, false, verificationTimeout)).To(Succeed())
}

// RunSHCDeployerResourceSpecTest deploys a Search Head Cluster, verifies default CPU limits,
// updates the deployer resource spec, and verifies the deployer is reconfigured while search heads retain defaults.
func RunSHCDeployerResourceSpecTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, defaultCPULimits string) {
	shcName := fmt.Sprintf("%s-shc", deployment.GetName())
	_, err := deployment.DeploySearchHeadCluster(ctx, shcName, "", "", "", "")
	Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster", "shc", shcName)

	// Verify CPU limits on Search Heads and deployer before updating CR
	searchHeadCount := 3
	Expect(testcaseEnvInst.VerifySearchHeadCPULimits(deployment, searchHeadCount, defaultCPULimits)).To(Succeed())

	deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
	Expect(testcaseEnvInst.VerifyCPULimits(deployment, deployerPodName, defaultCPULimits)).To(Succeed())

	shc := &enterpriseApi.SearchHeadCluster{}
	Expect(testenv.GetInstanceWithExpect(ctx, deployment, shc, shcName, "Unable to fetch Search Head Cluster deployment")).To(Succeed())

	// Assign new resources for deployer pod only
	newCPULimits := "4"
	newCPURequests := "2"
	newMemoryLimits := "14Gi"
	newMemoryRequests := "12Gi"

	depResSpec := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			"cpu":    resource.MustParse(newCPURequests),
			"memory": resource.MustParse(newMemoryRequests),
		},
		Limits: corev1.ResourceList{
			"cpu":    resource.MustParse(newCPULimits),
			"memory": resource.MustParse(newMemoryLimits),
		},
	}
	shc.Spec.DeployerResourceSpec = depResSpec

	Expect(testenv.UpdateCRWithExpect(ctx, deployment, shc, "Unable to deploy Search Head Cluster with updated CR")).To(Succeed())

	// Verify Search Head go to ready state
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed())

	// Verify CPU limits on Search Heads - Should be same as before
	Expect(testcaseEnvInst.VerifySearchHeadCPULimits(deployment, searchHeadCount, defaultCPULimits)).To(Succeed())

	// Verify modified deployer spec
	Expect(testcaseEnvInst.VerifyResourceConstraints(deployment, deployerPodName, depResSpec)).To(Succeed())
}

// RunM4CPUUpdateTest runs the standard M4 CPU limit update test workflow
func RunM4CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, defaultCPULimits string, newCPULimits string) {
	// Deploy Multisite Cluster and Search Head Clusters
	mcRef := deployment.GetName()
	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	siteCount := 3
	Expect(config.DeployAndVerifyM4(ctx, deployment, testcaseEnvInst, 1, siteCount, mcRef)).To(Succeed(), "Unable to deploy M4 cluster")

	Expect(testcaseEnvInst.TriggerAndVerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)).To(Succeed())

	// Deploy and verify Monitoring Console
	mc, err := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcRef, "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify RF SF is met
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed())

	// Verify CPU limits on Indexers before updating the CR
	Expect(testcaseEnvInst.VerifyCPULimitsOnAllSites(deployment, siteCount, defaultCPULimits)).To(Succeed())

	// Change CPU limits to trigger CR update
	idxc := &enterpriseApi.IndexerCluster{}
	for i := 1; i <= siteCount; i++ {
		siteName := fmt.Sprintf("site%d", i)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		Expect(testenv.GetInstanceWithExpect(ctx, deployment, idxc, instanceName, "Unable to fetch Indexer Cluster deployment")).To(Succeed())
		idxc.Spec.Resources.Limits = corev1.ResourceList{
			"cpu": resource.MustParse(newCPULimits),
		}
		Expect(testenv.UpdateCRWithExpect(ctx, deployment, idxc, "Unable to deploy Indexer Cluster with updated CR")).To(Succeed())
	}

	// Verify Indexer Cluster is updating
	idxcName := deployment.GetName() + "-site1"
	Expect(testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating, idxcName)).To(Succeed())

	// Verify Indexers go to ready state
	Expect(testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)).To(Succeed())

	// Verify Monitoring Console is Ready and stays in ready state
	Expect(testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)).To(Succeed())

	// Verify RF SF is met
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed())

	// Verify CPU limits after updating the CR
	Expect(testcaseEnvInst.VerifyCPULimitsOnAllSites(deployment, siteCount, newCPULimits)).To(Succeed())
}
