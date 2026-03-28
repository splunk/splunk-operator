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
	standalone := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, deployment.GetName(), deployment.GetName(), "")

	// Verify telemetry
	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	testcaseEnvInst.TriggerAndVerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)

	// Deploy and verify Monitoring Console
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), "")

	// Verify CPU limits before updating the CR
	standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	testcaseEnvInst.VerifyCPULimits(deployment, standalonePodName, defaultCPULimits)

	// Change CPU limits to trigger CR update
	standalone.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	err := deployment.UpdateCR(ctx, standalone)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with updated CR ")

	// Verify Standalone is updating
	testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseUpdating)

	// Verify Standalone goes to ready state
	testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, deployment.GetName(), enterpriseApi.PhaseReady)

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Verify CPU limits after updating the CR
	testcaseEnvInst.VerifyCPULimits(deployment, standalonePodName, newCPULimits)
}

// RunC3CPUUpdateTest runs the standard C3 CPU limit update test workflow
func RunC3CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, defaultCPULimits string, newCPULimits string) {
	// Deploy Single site Cluster and Search Head Clusters
	mcRef := deployment.GetName()
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true /*shc*/, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Verify cluster is ready, RF/SF is met, and MC is ready
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)
	testcaseEnvInst.StandardC3Verification(ctx, deployment, deployment.GetName(), testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, deployment.GetName(), ""))

	// Verify CPU limits on Indexers before updating the CR
	indexerCount := 3
	testcaseEnvInst.VerifyIndexerCPULimits(deployment, deployment.GetName(), indexerCount, defaultCPULimits)

	// Change CPU limits to trigger CR update
	idxc := &enterpriseApi.IndexerCluster{}
	instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
	testenv.GetInstanceWithExpect(ctx, deployment, idxc, instanceName, "Unable to get instance of indexer cluster")
	idxc.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	testenv.UpdateCRWithExpect(ctx, deployment, idxc, "Unable to deploy Indexer Cluster with updated CR")

	// Verify Indexer Cluster is updating
	idxcName := deployment.GetName() + "-idxc"
	testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating, idxcName)

	// Verify Indexers go to ready state
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

	// Verify CPU limits on Indexers after updating the CR
	testcaseEnvInst.VerifyIndexerCPULimits(deployment, deployment.GetName(), indexerCount, newCPULimits)

	// Verify CPU limits on Search Heads before updating the CR
	searchHeadCount := 3
	testcaseEnvInst.VerifySearchHeadCPULimits(deployment, deployment.GetName(), searchHeadCount, defaultCPULimits)

	// Change CPU limits to trigger CR update
	shc := &enterpriseApi.SearchHeadCluster{}
	instanceName = fmt.Sprintf("%s-shc", deployment.GetName())
	testenv.GetInstanceWithExpect(ctx, deployment, shc, instanceName, "Unable to fetch Search Head Cluster deployment")

	shc.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	testenv.UpdateCRWithExpect(ctx, deployment, shc, "Unable to deploy Search Head Cluster with updated CR")

	// Verify Search Head Cluster is updating
	testcaseEnvInst.VerifySearchHeadClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)

	// Verify Search Head go to ready state
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	// Verify CPU limits on Search Heads after updating the CR
	testcaseEnvInst.VerifySearchHeadCPULimits(deployment, deployment.GetName(), searchHeadCount, newCPULimits)
}

// RunC3PVCDeletionTest runs the standard C3 PVC deletion test workflow
func RunC3PVCDeletionTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, verificationTimeout time.Duration) {
	// Deploy Single site Cluster and Search Head Clusters
	mcRef := deployment.GetName()
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true /*shc*/, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Verify cluster is ready and RF/SF is met
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)
	testcaseEnvInst.VerifyClusterReadyAndRFSF(ctx, deployment)

	// Deploy and verify Monitoring Console
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcRef, "")

	clusterManagerType := config.ClusterManagerPVCType()
	verifyC3ClusterPVCs(testcaseEnvInst, deployment, clusterManagerType, true, verificationTimeout)

	// Delete the Search Head Cluster
	shc := &enterpriseApi.SearchHeadCluster{}
	err = deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)
	Expect(err).To(Succeed(), "Unable to GET SHC instance", "SHC Name", shc)
	err = deployment.DeleteCR(ctx, shc)
	Expect(err).To(Succeed(), "Unable to delete SHC instance", "SHC Name", shc)

	// Delete the Indexer Cluster
	idxc := &enterpriseApi.IndexerCluster{}
	err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", idxc)
	Expect(err).To(Succeed(), "Unable to GET IDXC instance", "IDXC Name", idxc)
	err = deployment.DeleteCR(ctx, idxc)
	Expect(err).To(Succeed(), "Unable to delete IDXC instance", "IDXC Name", idxc)

	// Delete the Cluster Manager (v3 or v4)
	config.DeleteClusterManager(ctx, deployment)

	// Delete Monitoring Console
	err = deployment.GetInstance(ctx, mcRef, mc)
	Expect(err).To(Succeed(), "Unable to GET Monitoring Console instance", "Monitoring Console Name", mcRef)
	err = deployment.DeleteCR(ctx, mc)
	Expect(err).To(Succeed(), "Unable to delete Monitoring Console instance", "Monitoring Console Name", mcRef)

	verifyC3ClusterPVCs(testcaseEnvInst, deployment, clusterManagerType, false, verificationTimeout)

	// Verify Monitoring Console PVCs (etc and var) have been deleted
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "monitoring-console", 1, false, verificationTimeout)
}

func verifyC3ClusterPVCs(testcaseEnvInst *testenv.TestCaseEnv, deployment *testenv.Deployment, clusterManagerType string, exists bool, timeout time.Duration) {
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-search-head", 3, exists, timeout)
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-deployer", 1, exists, timeout)
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "idxc-indexer", 3, exists, timeout)
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, clusterManagerType, 1, exists, timeout)
}

// RunSHCDeployerResourceSpecTest deploys a Search Head Cluster, verifies default CPU limits,
// updates the deployer resource spec, and verifies the deployer is reconfigured while search heads retain defaults.
func RunSHCDeployerResourceSpecTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, defaultCPULimits string) {
	shcName := fmt.Sprintf("%s-shc", deployment.GetName())
	_, err := deployment.DeploySearchHeadCluster(ctx, shcName, "", "", "", "")
	if err != nil {
		Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster", "shc", shcName)
	}

	// Verify CPU limits on Search Heads and deployer before updating CR
	searchHeadCount := 3
	testcaseEnvInst.VerifySearchHeadCPULimits(deployment, deployment.GetName(), searchHeadCount, defaultCPULimits)

	DeployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
	testcaseEnvInst.VerifyCPULimits(deployment, DeployerPodName, defaultCPULimits)

	shc := &enterpriseApi.SearchHeadCluster{}
	testenv.GetInstanceWithExpect(ctx, deployment, shc, shcName, "Unable to fetch Search Head Cluster deployment")

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

	testenv.UpdateCRWithExpect(ctx, deployment, shc, "Unable to deploy Search Head Cluster with updated CR")

	// Verify Search Head go to ready state
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	// Verify CPU limits on Search Heads - Should be same as before
	testcaseEnvInst.VerifySearchHeadCPULimits(deployment, deployment.GetName(), searchHeadCount, defaultCPULimits)

	// Verify modified deployer spec
	testcaseEnvInst.VerifyResourceConstraints(deployment, DeployerPodName, depResSpec)
}

// RunM4CPUUpdateTest runs the standard M4 CPU limit update test workflow
func RunM4CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, defaultCPULimits string, newCPULimits string) {
	// Deploy Multisite Cluster and Search Head Clusters
	mcRef := deployment.GetName()
	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	siteCount := 3
	var err error

	err = config.DeployMultisiteCluster(ctx, deployment, deployment.GetName(), 1, siteCount, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure that the cluster-manager goes to Ready phase
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

	testcaseEnvInst.VerifyM4ComponentsReady(ctx, deployment, siteCount)

	testcaseEnvInst.TriggerAndVerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)

	// Deploy Monitoring Console CRD
	mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Verify RF SF is met
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	// Verify CPU limits on Indexers before updating the CR
	for i := 1; i <= siteCount; i++ {
		podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), i, 0)
		testcaseEnvInst.VerifyCPULimits(deployment, podName, defaultCPULimits)
	}

	// Change CPU limits to trigger CR update
	idxc := &enterpriseApi.IndexerCluster{}
	for i := 1; i <= siteCount; i++ {
		siteName := fmt.Sprintf("site%d", i)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		testenv.GetInstanceWithExpect(ctx, deployment, idxc, instanceName, "Unable to fetch Indexer Cluster deployment")
		idxc.Spec.Resources.Limits = corev1.ResourceList{
			"cpu": resource.MustParse(newCPULimits),
		}
		testenv.UpdateCRWithExpect(ctx, deployment, idxc, "Unable to deploy Indexer Cluster with updated CR")
	}

	// Verify Indexer Cluster is updating
	idxcName := deployment.GetName() + "-" + "site1"
	testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating, idxcName)

	// Verify Indexers go to ready state
	testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	// Verify RF SF is met
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	// Verify CPU limits after updating the CR
	testcaseEnvInst.VerifyCPULimitsOnAllSites(deployment, deployment.GetName(), siteCount, newCPULimits)
}
