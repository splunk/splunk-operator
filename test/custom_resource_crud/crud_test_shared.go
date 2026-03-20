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

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// CRUDTestConfig holds configuration for CRUD tests to support both v3 and v4 API versions
type CRUDTestConfig struct {
	LicenseManagerReady func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv)
	ClusterManagerReady func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv)
	APIVersion          string
}

// NewCRUDTestConfigV3 creates configuration for v3 API (LicenseMaster/ClusterMaster)
func NewCRUDTestConfigV3() *CRUDTestConfig {
	return &CRUDTestConfig{
		LicenseManagerReady: func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv) {
			testcaseEnv.VerifyLicenseMasterReady(ctx, deployment)
		},
		ClusterManagerReady: func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv) {
			testcaseEnv.VerifyClusterMasterReady(ctx, deployment)
		},
		APIVersion: "v3",
	}
}

// NewCRUDTestConfigV4 creates configuration for v4 API (LicenseManager/ClusterManager)
func NewCRUDTestConfigV4() *CRUDTestConfig {
	return &CRUDTestConfig{
		LicenseManagerReady: func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv) {
			testcaseEnv.VerifyLicenseManagerReady(ctx, deployment)
		},
		ClusterManagerReady: func(ctx context.Context, deployment *testenv.Deployment, testcaseEnv *testenv.TestCaseEnv) {
			testcaseEnv.VerifyClusterManagerReady(ctx, deployment)
		},
		APIVersion: "v4",
	}
}

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
func RunC3CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *CRUDTestConfig, defaultCPULimits string, newCPULimits string) {
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
func RunC3PVCDeletionTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *CRUDTestConfig, verificationTimeout time.Duration) {
	// Deploy Single site Cluster and Search Head Clusters
	mcRef := deployment.GetName()
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true /*shc*/, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Verify cluster is ready and RF/SF is met
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)
	testcaseEnvInst.VerifyClusterReadyAndRFSF(ctx, deployment)

	// Deploy and verify Monitoring Console
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcRef, "")

	// Verify Search Heads PVCs (etc and var) exists
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-search-head", 3, true, verificationTimeout)

	// Verify Deployer PVCs (etc and var) exists
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-deployer", 1, true, verificationTimeout)

	// Verify Indexers PVCs (etc and var) exists
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "idxc-indexer", 3, true, verificationTimeout)

	// Verify Cluster Manager PVCs (etc and var) exists
	clusterManagerType := "cluster-master"
	if config.APIVersion == "v4" {
		clusterManagerType = "cluster-manager"
	}
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, clusterManagerType, 1, true, verificationTimeout)

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
	if config.APIVersion == "v3" {
		cm := &enterpriseApiV3.ClusterMaster{}
		err = deployment.GetInstance(ctx, deployment.GetName(), cm)
		Expect(err).To(Succeed(), "Unable to GET Cluster Master instance", "Cluster Master Name", cm)
		err = deployment.DeleteCR(ctx, cm)
		Expect(err).To(Succeed(), "Unable to delete Cluster Master instance", "Cluster Master Name", cm)
	} else {
		cm := &enterpriseApi.ClusterManager{}
		err = deployment.GetInstance(ctx, deployment.GetName(), cm)
		Expect(err).To(Succeed(), "Unable to GET Cluster Manager instance", "Cluster Manager Name", cm)
		err = deployment.DeleteCR(ctx, cm)
		Expect(err).To(Succeed(), "Unable to delete Cluster Manager instance", "Cluster Manager Name", cm)
	}

	// Delete Monitoring Console
	err = deployment.GetInstance(ctx, mcRef, mc)
	Expect(err).To(Succeed(), "Unable to GET Monitoring Console instance", "Monitoring Console Name", mcRef)
	err = deployment.DeleteCR(ctx, mc)
	Expect(err).To(Succeed(), "Unable to delete Monitoring Console instance", "Monitoring Console Name", mcRef)

	// Verify Search Heads PVCs (etc and var) have been deleted
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-search-head", 3, false, verificationTimeout)

	// Verify Deployer PVCs (etc and var) have been deleted
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-deployer", 1, false, verificationTimeout)

	// Verify Indexers PVCs (etc and var) have been deleted
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "idxc-indexer", 3, false, verificationTimeout)

	// Verify Cluster Manager PVCs (etc and var) have been deleted
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, clusterManagerType, 1, false, verificationTimeout)

	// Verify Monitoring Console PVCs (etc and var) have been deleted
	testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "monitoring-console", 1, false, verificationTimeout)
}

// RunM4CPUUpdateTest runs the standard M4 CPU limit update test workflow
func RunM4CPUUpdateTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *CRUDTestConfig, defaultCPULimits string, newCPULimits string) {
	// Deploy Multisite Cluster and Search Head Clusters
	mcRef := deployment.GetName()
	prevTelemetrySubmissionTime := testcaseEnvInst.GetTelemetryLastSubmissionTime(ctx, deployment)
	siteCount := 3
	var err error

	if config.APIVersion == "v3" {
		err = deployment.DeployMultisiteClusterMasterWithSearchHead(ctx, deployment.GetName(), 1, siteCount, mcRef)
	} else {
		err = deployment.DeployMultisiteClusterWithSearchHead(ctx, deployment.GetName(), 1, siteCount, mcRef)
	}
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure that the cluster-manager goes to Ready phase
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

	// Ensure the indexers of all sites go to Ready phase
	testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

	// Ensure cluster configured as multisite
	testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

	// Ensure search head cluster go to Ready phase
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

	// Verify telemetry
	testcaseEnvInst.TriggerTelemetrySubmission(ctx, deployment)
	testcaseEnvInst.VerifyTelemetry(ctx, deployment, prevTelemetrySubmissionTime)

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
