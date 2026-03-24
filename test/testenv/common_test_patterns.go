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

package testenv

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// ClusterReadinessConfig holds v3/v4 API version callbacks for cluster and license manager
// readiness verification. Shared across test packages to avoid per-package duplication.
type ClusterReadinessConfig struct {
	LicenseManagerReady func(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv)
	ClusterManagerReady func(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv)
	APIVersion          string
}

// NewClusterReadinessConfigV3 creates a ClusterReadinessConfig for v3 API (LicenseMaster/ClusterMaster)
func NewClusterReadinessConfigV3() *ClusterReadinessConfig {
	return &ClusterReadinessConfig{
		LicenseManagerReady: func(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) {
			testcaseEnv.VerifyLicenseMasterReady(ctx, deployment)
		},
		ClusterManagerReady: func(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) {
			testcaseEnv.VerifyClusterMasterReady(ctx, deployment)
		},
		APIVersion: "v3",
	}
}

// NewClusterReadinessConfigV4 creates a ClusterReadinessConfig for v4 API (LicenseManager/ClusterManager)
func NewClusterReadinessConfigV4() *ClusterReadinessConfig {
	return &ClusterReadinessConfig{
		LicenseManagerReady: func(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) {
			testcaseEnv.VerifyLicenseManagerReady(ctx, deployment)
		},
		ClusterManagerReady: func(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) {
			testcaseEnv.VerifyClusterManagerReady(ctx, deployment)
		},
		APIVersion: "v4",
	}
}

// DeployStandaloneWithLM deploys a standalone with the appropriate License Manager type for
// the API version: LicenseMaster (v3) or LicenseManager (v4).
func (c *ClusterReadinessConfig) DeployStandaloneWithLM(ctx context.Context, deployment *Deployment, name, mcRef string) (*enterpriseApi.Standalone, error) {
	if c.APIVersion == "v3" {
		return deployment.DeployStandaloneWithLMaster(ctx, name, mcRef)
	}
	return deployment.DeployStandaloneWithLM(ctx, name, mcRef)
}

// DeployMultisiteCluster deploys a multisite cluster with the appropriate Cluster Manager type
// for the API version: ClusterMaster (v3) or ClusterManager (v4).
func (c *ClusterReadinessConfig) DeployMultisiteCluster(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, mcRef string) error {
	if c.APIVersion == "v3" {
		return deployment.DeployMultisiteClusterMasterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef)
	}
	return deployment.DeployMultisiteClusterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef)
}

// VerifyClusterManagerPhaseUpdating asserts the Cluster Manager (or ClusterMaster for v3)
// has entered the Updating phase.
func (c *ClusterReadinessConfig) VerifyClusterManagerPhaseUpdating(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv) {
	if c.APIVersion == "v3" {
		testcaseEnv.VerifyClusterMasterPhase(ctx, deployment, enterpriseApi.PhaseUpdating)
	} else {
		testcaseEnv.VerifyClusterManagerPhase(ctx, deployment, enterpriseApi.PhaseUpdating)
	}
}

// ClusterManagerPVCType returns the PVC label fragment for the Cluster Manager:
// "cluster-master" for v3, "cluster-manager" for v4.
func (c *ClusterReadinessConfig) ClusterManagerPVCType() string {
	if c.APIVersion == "v3" {
		return "cluster-master"
	}
	return "cluster-manager"
}

// DeleteClusterManager fetches and deletes the Cluster Manager CR for the appropriate API version.
func (c *ClusterReadinessConfig) DeleteClusterManager(ctx context.Context, deployment *Deployment) {
	name := deployment.GetName()
	if c.APIVersion == "v3" {
		cm := &enterpriseApiV3.ClusterMaster{}
		err := deployment.GetInstance(ctx, name, cm)
		Expect(err).To(Succeed(), "Unable to GET Cluster Master instance", "Cluster Master Name", cm)
		err = deployment.DeleteCR(ctx, cm)
		Expect(err).To(Succeed(), "Unable to delete Cluster Master instance", "Cluster Master Name", cm)
	} else {
		cm := &enterpriseApi.ClusterManager{}
		err := deployment.GetInstance(ctx, name, cm)
		Expect(err).To(Succeed(), "Unable to GET Cluster Manager instance", "Cluster Manager Name", cm)
		err = deployment.DeleteCR(ctx, cm)
		Expect(err).To(Succeed(), "Unable to delete Cluster Manager instance", "Cluster Manager Name", cm)
	}
}

// DeployMultisiteClusterWithIndexes deploys a multisite cluster with SmartStore indexes using
// the appropriate Cluster Manager type for the API version.
func (c *ClusterReadinessConfig) DeployMultisiteClusterWithIndexes(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, secretName string, smartStoreSpec enterpriseApi.SmartStoreSpec) error {
	if c.APIVersion == "v3" {
		return deployment.DeployMultisiteClusterMasterWithSearchHeadAndIndexes(ctx, name, indexerReplicas, siteCount, secretName, smartStoreSpec)
	}
	return deployment.DeployMultisiteClusterWithSearchHeadAndIndexes(ctx, name, indexerReplicas, siteCount, secretName, smartStoreSpec)
}

// GetBundleHash returns the current bundle hash for the Cluster Manager (or ClusterMaster for v3).
func (c *ClusterReadinessConfig) GetBundleHash(ctx context.Context, deployment *Deployment) string {
	if c.APIVersion == "v3" {
		return GetClusterManagerBundleHash(ctx, deployment, "ClusterMaster")
	}
	return GetClusterManagerBundleHash(ctx, deployment, "ClusterManager")
}

// AppendSmartStoreIndex appends a new SmartStore index to the Cluster Manager CR
// for the appropriate API version.
func (c *ClusterReadinessConfig) AppendSmartStoreIndex(ctx context.Context, deployment *Deployment, newIndex []enterpriseApi.IndexSpec) {
	name := deployment.GetName()
	if c.APIVersion == "v3" {
		cm := &enterpriseApiV3.ClusterMaster{}
		err := deployment.GetInstance(ctx, name, cm)
		Expect(err).To(Succeed(), "Failed to get instance of Cluster Master")
		cm.Spec.SmartStore.IndexList = append(cm.Spec.SmartStore.IndexList, newIndex...)
		err = deployment.UpdateCR(ctx, cm)
		Expect(err).To(Succeed(), "Failed to add new index to cluster master")
	} else {
		cm := &enterpriseApi.ClusterManager{}
		err := deployment.GetInstance(ctx, name, cm)
		Expect(err).To(Succeed(), "Failed to get instance of Cluster Manager")
		cm.Spec.SmartStore.IndexList = append(cm.Spec.SmartStore.IndexList, newIndex...)
		err = deployment.UpdateCR(ctx, cm)
		Expect(err).To(Succeed(), "Failed to add new index to cluster manager")
	}
}

// DeployMCAndGetVersion deploys and verifies a Monitoring Console, then returns both the MC
// instance and its current resource version.
func (testcaseenv *TestCaseEnv) DeployMCAndGetVersion(ctx context.Context, deployment *Deployment, name string, lmRef string) (*enterpriseApi.MonitoringConsole, string) {
	mc := testcaseenv.DeployAndVerifyMonitoringConsole(ctx, deployment, name, lmRef)
	resourceVersion := testcaseenv.GetResourceVersion(ctx, deployment, mc)
	return mc, resourceVersion
}

// DeployAndVerifyStandalone deploys a standalone instance and verifies it reaches ready state
func (testcaseenv *TestCaseEnv) DeployAndVerifyStandalone(ctx context.Context, deployment *Deployment, name string, mcRef string, licenseManagerRef string) *enterpriseApi.Standalone {
	standalone, err := deployment.DeployStandalone(ctx, name, mcRef, licenseManagerRef)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	testcaseenv.VerifyStandaloneReady(ctx, deployment, name, standalone)
	return standalone
}

// DeployAndVerifyMonitoringConsole deploys a monitoring console and verifies it reaches ready state
func (testcaseenv *TestCaseEnv) DeployAndVerifyMonitoringConsole(ctx context.Context, deployment *Deployment, name string, licenseManagerRef string) *enterpriseApi.MonitoringConsole {
	mc, err := deployment.DeployMonitoringConsole(ctx, name, licenseManagerRef)
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

	testcaseenv.VerifyMonitoringConsoleReady(ctx, deployment, name, mc)
	return mc
}

// VerifyIndexerCPULimits verifies CPU limits on all indexer pods in a single-site cluster
func (testcaseenv *TestCaseEnv) VerifyIndexerCPULimits(deployment *Deployment, deploymentName string, indexerCount int, expectedCPULimit string) {
	for i := 0; i < indexerCount; i++ {
		podName := fmt.Sprintf(IndexerPod, deploymentName, i)
		testcaseenv.VerifyCPULimits(deployment, podName, expectedCPULimit)
	}
}

// VerifySearchHeadCPULimits verifies CPU limits on all search head pods
func (testcaseenv *TestCaseEnv) VerifySearchHeadCPULimits(deployment *Deployment, deploymentName string, searchHeadCount int, expectedCPULimit string) {
	for i := 0; i < searchHeadCount; i++ {
		podName := fmt.Sprintf(SearchHeadPod, deploymentName, i)
		testcaseenv.VerifyCPULimits(deployment, podName, expectedCPULimit)
	}
}

// VerifyC3ComponentsReady verifies SHC and single-site indexers are ready (without CM check or RFSF).
func (testcaseenv *TestCaseEnv) VerifyC3ComponentsReady(ctx context.Context, deployment *Deployment) {
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseenv.VerifySingleSiteIndexersReady(ctx, deployment)
}

// VerifyM4ComponentsReady verifies multisite indexers, multisite status, and SHC are ready (without CM check or RFSF).
func (testcaseenv *TestCaseEnv) VerifyM4ComponentsReady(ctx context.Context, deployment *Deployment, siteCount int) {
	testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseenv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
}

// VerifyMCVersionChangedAndReady waits for the MC resource version to change then verifies MC is ready.
func (testcaseenv *TestCaseEnv) VerifyMCVersionChangedAndReady(ctx context.Context, deployment *Deployment, mc *enterpriseApi.MonitoringConsole, resourceVersion string) {
	testcaseenv.VerifyCustomResourceVersionChanged(ctx, deployment, mc, resourceVersion)
	testcaseenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)
}

// VerifyClusterReadyAndRFSF is a common verification pattern that checks cluster is ready and RF/SF is met
func (testcaseenv *TestCaseEnv) VerifyClusterReadyAndRFSF(ctx context.Context, deployment *Deployment) {
	testcaseenv.VerifyClusterManagerReady(ctx, deployment)
	testcaseenv.VerifyC3ComponentsReady(ctx, deployment)
	testcaseenv.VerifyRFSFMet(ctx, deployment)
}

// VerifyMultisiteClusterReadyAndRFSF is a common verification pattern for multisite clusters
func (testcaseenv *TestCaseEnv) VerifyMultisiteClusterReadyAndRFSF(ctx context.Context, deployment *Deployment, siteCount int) {
	testcaseenv.VerifyClusterManagerReady(ctx, deployment)
	testcaseenv.VerifyM4ComponentsReady(ctx, deployment, siteCount)
	testcaseenv.VerifyRFSFMet(ctx, deployment)
}

// TriggerAndVerifyTelemetry is a common pattern for telemetry verification
func (testcaseenv *TestCaseEnv) TriggerAndVerifyTelemetry(ctx context.Context, deployment *Deployment, prevSubmissionTime string) {
	testcaseenv.TriggerTelemetrySubmission(ctx, deployment)
	testcaseenv.VerifyTelemetry(ctx, deployment, prevSubmissionTime)
}

// StandardC3Verification performs the standard set of verifications for a C3 cluster
// This includes cluster ready, RF/SF met, and monitoring console ready
func (testcaseenv *TestCaseEnv) StandardC3Verification(ctx context.Context, deployment *Deployment, mcName string, mc *enterpriseApi.MonitoringConsole) {
	testcaseenv.VerifyClusterReadyAndRFSF(ctx, deployment)
	testcaseenv.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)
}
