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
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

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

// DeployAndVerifyC3Cluster deploys a C3 cluster (single site with SHC) and verifies all components are ready
func (testcaseenv *TestCaseEnv) DeployAndVerifyC3Cluster(ctx context.Context, deployment *Deployment, name string, indexerReplicas int, mcRef string) {
	err := deployment.DeploySingleSiteCluster(ctx, name, indexerReplicas, true, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy C3 cluster")

	testcaseenv.VerifyClusterManagerReady(ctx, deployment)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseenv.VerifySingleSiteIndexersReady(ctx, deployment)
}

// DeployAndVerifyM4Cluster deploys an M4 cluster (multisite with SHC) and verifies all components are ready
func (testcaseenv *TestCaseEnv) DeployAndVerifyM4Cluster(ctx context.Context, deployment *Deployment, name string, indexerReplicas int, siteCount int, mcRef string) {
	err := deployment.DeployMultisiteClusterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy M4 cluster")

	testcaseenv.VerifyClusterManagerReady(ctx, deployment)
	testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseenv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
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

// VerifyClusterReadyAndRFSF is a common verification pattern that checks cluster is ready and RF/SF is met
func (testcaseenv *TestCaseEnv) VerifyClusterReadyAndRFSF(ctx context.Context, deployment *Deployment) {
	testcaseenv.VerifyClusterManagerReady(ctx, deployment)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseenv.VerifySingleSiteIndexersReady(ctx, deployment)
	testcaseenv.VerifyRFSFMet(ctx, deployment)
}

// VerifyMultisiteClusterReadyAndRFSF is a common verification pattern for multisite clusters
func (testcaseenv *TestCaseEnv) VerifyMultisiteClusterReadyAndRFSF(ctx context.Context, deployment *Deployment, siteCount int) {
	testcaseenv.VerifyClusterManagerReady(ctx, deployment)
	testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseenv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
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

// StandardM4Verification performs the standard set of verifications for an M4 cluster
// This includes multisite cluster ready, RF/SF met, and monitoring console ready
func (testcaseenv *TestCaseEnv) StandardM4Verification(ctx context.Context, deployment *Deployment, siteCount int, mcName string, mc *enterpriseApi.MonitoringConsole) {
	testcaseenv.VerifyMultisiteClusterReadyAndRFSF(ctx, deployment, siteCount)
	testcaseenv.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)
}
