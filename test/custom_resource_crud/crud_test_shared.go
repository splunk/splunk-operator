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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// RunS1CPUUpdateTest runs the standard S1 CPU limit update test workflow
func RunS1CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, defaultCPULimits string, newCPULimits string) {
	// Deploy and verify Standalone
	standalone, err := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, "")
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance")

	// Verify telemetry
	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	Expect(testcaseEnvInst.TriggerAndVerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)).To(Succeed(), "Telemetry verification failed")

	// Verify CPU limits on Standalone before updating the CR
	standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	Expect(testcaseEnvInst.VerifyCPULimits(deployment, standalonePodName, defaultCPULimits)).To(Succeed(), "Standalone CPU limits mismatch before CR update")

	// Change CPU limits to trigger CR update
	standalone.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	err = deployment.UpdateCR(ctx, standalone)
	Expect(err).To(Succeed(), "Unable to update Standalone CR")

	// Verify Standalone reaches Updating phase and returns to Ready
	Expect(testcaseEnvInst.VerifyStandalonePhaseAndReady(ctx, deployment, enterpriseApi.PhaseUpdating, standalone)).To(Succeed(), "Standalone did not reach Updating phase or return to Ready")

	// Verify CPU limits on Standalone after updating the CR
	Expect(testcaseEnvInst.VerifyCPULimits(deployment, standalonePodName, newCPULimits)).To(Succeed(), "Standalone CPU limits mismatch after CR update")

	Expect(testcaseEnvInst.VerifyStandaloneConditionReady(ctx, deployment, standalone)).To(Succeed(), "Standalone Ready condition not met")
}

// RunC3CPUUpdateTest runs the standard C3 CPU limit update test workflow
func RunC3CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, defaultCPULimits string, newCPULimits string) {
	// Deploy Single site Cluster and Search Head Clusters
	Expect(config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, 3, true /*shc*/)).To(Succeed(), "Unable to deploy C3 cluster")

	// Verify telemetry
	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	Expect(testcaseEnvInst.TriggerAndVerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)).To(Succeed(), "Telemetry verification failed")

	// Verify RF/SF
	Expect(testcaseEnvInst.VerifyClusterReadyAndRFSF(ctx, deployment)).To(Succeed(), "Cluster not ready or RF/SF not met")

	// Verify CPU limits on Indexers before updating the CR
	indexerCount := 3
	Expect(testcaseEnvInst.VerifyIndexerCPULimits(deployment, indexerCount, defaultCPULimits)).To(Succeed(), "Indexer CPU limits mismatch before CR update")

	// Change CPU limits to trigger CR update
	idxc := &enterpriseApi.IndexerCluster{}
	instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
	Expect(deployment.GetInstance(ctx, instanceName, idxc)).To(Succeed(), "Unable to get Indexer Cluster instance")
	idxc.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	Expect(deployment.UpdateCR(ctx, idxc)).To(Succeed(), "Unable to update Indexer Cluster CR")

	// Verify Indexer Cluster is updating
	Expect(testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating, instanceName)).To(Succeed(), "Indexer Cluster did not reach Updating phase")

	// Verify Indexers go to ready state
	Expect(testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)).To(Succeed(), "Indexers not ready after CR update")

	// Verify CPU limits on Indexers after updating the CR
	Expect(testcaseEnvInst.VerifyIndexerCPULimits(deployment, indexerCount, newCPULimits)).To(Succeed(), "Indexer CPU limits mismatch after CR update")

	// Verify CPU limits on Search Heads before updating the CR
	searchHeadCount := 3
	Expect(testcaseEnvInst.VerifySearchHeadCPULimits(deployment, searchHeadCount, defaultCPULimits)).To(Succeed(), "Search Head CPU limits mismatch before CR update")

	// Change CPU limits to trigger CR update
	shc := &enterpriseApi.SearchHeadCluster{}
	instanceName = fmt.Sprintf("%s-shc", deployment.GetName())
	Expect(deployment.GetInstance(ctx, instanceName, shc)).To(Succeed(), "Unable to get Search Head Cluster instance")

	shc.Spec.Resources.Limits = corev1.ResourceList{
		"cpu": resource.MustParse(newCPULimits),
	}
	Expect(deployment.UpdateCR(ctx, shc)).To(Succeed(), "Unable to update Search Head Cluster CR")

	// Verify Search Head Cluster is updating
	Expect(testcaseEnvInst.VerifySearchHeadClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)).To(Succeed(), "Search Head Cluster did not reach Updating phase")

	// Verify Search Heads go to ready state
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed(), "Search Head Cluster not ready after CR update")

	// Verify CPU limits on Search Heads after updating the CR
	Expect(testcaseEnvInst.VerifySearchHeadCPULimits(deployment, searchHeadCount, newCPULimits)).To(Succeed(), "Search Head CPU limits mismatch after CR update")

	Expect(testcaseEnvInst.VerifyC3ConditionsReady(ctx, deployment)).To(Succeed(), "C3 Ready conditions not met")
}

// RunC3PVCDeletionTest runs the standard C3 PVC deletion test workflow
func RunC3PVCDeletionTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, verificationTimeout time.Duration) {
	// Deploy Single site Cluster and Search Head Clusters
	Expect(config.DeployAndVerifyC3(ctx, deployment, testcaseEnvInst, 3, true /*shc*/)).To(Succeed(), "Unable to deploy C3 cluster")
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

	clusterManagerType := config.ClusterManagerPVCType()
	Expect(testenv.VerifyC3ClusterPVCs(testcaseEnvInst, deployment, clusterManagerType, true, verificationTimeout)).To(Succeed(), "C3 cluster PVCs not present")

	// Delete the Search Head Cluster
	Expect(testenv.GetAndDeleteCR(ctx, deployment, &enterpriseApi.SearchHeadCluster{}, deployment.GetName()+"-shc")).To(Succeed(), "Unable to delete SHC instance")

	// Delete the Indexer Cluster
	Expect(testenv.GetAndDeleteCR(ctx, deployment, &enterpriseApi.IndexerCluster{}, deployment.GetName()+"-idxc")).To(Succeed(), "Unable to delete IDXC instance")

	// Delete the Cluster Manager (v3 or v4)
	Expect(config.DeleteClusterManager(ctx, deployment)).To(Succeed(), "Unable to delete Cluster Manager")

	Expect(testenv.VerifyC3ClusterPVCs(testcaseEnvInst, deployment, clusterManagerType, false, verificationTimeout)).To(Succeed(), "C3 cluster PVCs not deleted")
}

// RunSHCDeployerResourceSpecTest deploys a Search Head Cluster, verifies default CPU limits,
// updates the deployer resource spec, and verifies the deployer is reconfigured while search heads retain defaults.
func RunSHCDeployerResourceSpecTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, defaultCPULimits string) {
	shcName := fmt.Sprintf("%s-shc", deployment.GetName())
	_, err := deployment.DeploySearchHeadCluster(ctx, shcName, "", "", "")
	Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster", "shc", shcName)

	// Verify CPU limits on Search Heads and deployer before updating CR
	searchHeadCount := 3
	Expect(testcaseEnvInst.VerifySearchHeadCPULimits(deployment, searchHeadCount, defaultCPULimits)).To(Succeed(), "Search Head CPU limits mismatch before CR update")

	deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
	Expect(testcaseEnvInst.VerifyCPULimits(deployment, deployerPodName, defaultCPULimits)).To(Succeed(), "Deployer CPU limits mismatch before CR update")

	shc := &enterpriseApi.SearchHeadCluster{}
	Expect(deployment.GetInstance(ctx, shcName, shc)).To(Succeed(), "Unable to get Search Head Cluster instance")

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
	Expect(deployment.UpdateCR(ctx, shc)).To(Succeed(), "Unable to update Search Head Cluster CR")

	// Verify Search Heads go to ready state
	Expect(testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)).To(Succeed(), "Search Head Cluster not ready after deployer spec update")

	// Verify CPU limits on Search Heads - Should be same as before
	Expect(testcaseEnvInst.VerifySearchHeadCPULimits(deployment, searchHeadCount, defaultCPULimits)).To(Succeed(), "Search Head CPU limits changed unexpectedly")

	// Verify modified deployer spec
	Expect(testcaseEnvInst.VerifyResourceConstraints(deployment, deployerPodName, depResSpec)).To(Succeed(), "Deployer resource constraints mismatch")

	shcCR := &enterpriseApi.SearchHeadCluster{}
	Expect(deployment.GetInstance(ctx, shcName, shcCR)).To(Succeed(), "Failed to get SHC instance")
	Expect(testenv.VerifyCRConditionsForPhase("SearchHeadCluster", shcName, shcCR.Status.Conditions, enterpriseApi.PhaseReady)).To(Succeed(), "SHC conditions not met")
}

// RunM4CPUUpdateTest runs the standard M4 CPU limit update test workflow
func RunM4CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.ClusterReadinessConfig, defaultCPULimits string, newCPULimits string) {
	// Deploy Multisite Cluster and Search Head Clusters
	siteCount := 3
	Expect(config.DeployAndVerifyM4(ctx, deployment, testcaseEnvInst, 1, siteCount)).To(Succeed(), "Unable to deploy M4 cluster")

	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	Expect(testcaseEnvInst.TriggerAndVerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)).To(Succeed(), "Telemetry verification failed")

	// Verify RF SF is met
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

	// Verify CPU limits on Indexers before updating the CR
	Expect(testcaseEnvInst.VerifyCPULimitsOnAllSites(deployment, siteCount, defaultCPULimits)).To(Succeed(), "Multisite Indexer CPU limits mismatch before CR update")

	// Change CPU limits to trigger CR update
	idxc := &enterpriseApi.IndexerCluster{}
	for i := 1; i <= siteCount; i++ {
		siteName := fmt.Sprintf("site%d", i)
		instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
		Expect(deployment.GetInstance(ctx, instanceName, idxc)).To(Succeed(), "Unable to get Indexer Cluster instance")
		idxc.Spec.Resources.Limits = corev1.ResourceList{
			"cpu": resource.MustParse(newCPULimits),
		}
		Expect(deployment.UpdateCR(ctx, idxc)).To(Succeed(), "Unable to update Indexer Cluster CR")
	}

	// Verify Indexer Cluster is updating
	idxcName := deployment.GetName() + "-site1"
	Expect(testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseUpdating, idxcName)).To(Succeed(), "Indexer Cluster did not reach Updating phase")

	// Verify Indexers go to ready state
	Expect(testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)).To(Succeed(), "Multisite Indexers not ready after CR update")

	// Verify RF SF is met
	Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met after CR update")

	// Verify CPU limits after updating the CR
	Expect(testcaseEnvInst.VerifyCPULimitsOnAllSites(deployment, siteCount, newCPULimits)).To(Succeed(), "Multisite Indexer CPU limits mismatch after CR update")

	Expect(testcaseEnvInst.VerifyM4ConditionsReady(ctx, deployment, siteCount)).To(Succeed(), "M4 Ready conditions not met")
}
