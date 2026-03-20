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
	corev1 "k8s.io/api/core/v1"
)

// WorkflowResult contains the result of a workflow execution
type WorkflowResult struct {
	Standalone        *enterpriseApi.Standalone
	ClusterManager    *enterpriseApi.ClusterManager
	IndexerCluster    *enterpriseApi.IndexerCluster
	SearchHeadCluster *enterpriseApi.SearchHeadCluster
	MonitoringConsole *enterpriseApi.MonitoringConsole
	LicenseManager    *enterpriseApi.LicenseManager
}

// RunStandaloneDeploymentWorkflow deploys a standalone instance and verifies it's ready
func RunStandaloneDeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string) *WorkflowResult {
	standalone, err := deployment.DeployStandalone(ctx, name, "", "")
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, name, standalone)

	return &WorkflowResult{Standalone: standalone}
}

// RunStandaloneWithLMWorkflow deploys standalone with license manager and verifies both are ready
func RunStandaloneWithLMWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, lmName string) *WorkflowResult {
	lm, err := deployment.DeployLicenseManager(ctx, lmName)
	Expect(err).To(Succeed(), "Unable to deploy License Manager")

	testcaseEnvInst.VerifyLicenseManagerReady(ctx, deployment)

	standalone, err := deployment.DeployStandalone(ctx, name, lmName, "")
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, name, standalone)

	return &WorkflowResult{Standalone: standalone, LicenseManager: lm}
}

// RunStandaloneWithMCWorkflow deploys standalone with monitoring console and verifies both are ready
func RunStandaloneWithMCWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, mcName string) *WorkflowResult {
	mc, err := deployment.DeployMonitoringConsole(ctx, mcName, "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

	standalone, err := deployment.DeployStandalone(ctx, name, "", mcName)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, name, standalone)
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

	return &WorkflowResult{Standalone: standalone, MonitoringConsole: mc}
}

// RunC3DeploymentWorkflow deploys a C3 cluster (CM + IDXC + SHC) and verifies all components are ready
func RunC3DeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int, mcRef string) *WorkflowResult {
	err := deployment.DeploySingleSiteCluster(ctx, name, indexerReplicas, true, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy C3 cluster")

	testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	return &WorkflowResult{}
}

// RunC3WithMCWorkflow deploys a C3 cluster with monitoring console and verifies all components
func RunC3WithMCWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int, mcName string) *WorkflowResult {
	mc, err := deployment.DeployMonitoringConsole(ctx, mcName, "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

	err = deployment.DeploySingleSiteCluster(ctx, name, indexerReplicas, true, mcName)
	Expect(err).To(Succeed(), "Unable to deploy C3 cluster")

	testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)
	testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

	return &WorkflowResult{MonitoringConsole: mc}
}

// RunM4DeploymentWorkflow deploys a M4 multisite cluster and verifies all components are ready
func RunM4DeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int, siteCount int, mcRef string) *WorkflowResult {
	err := deployment.DeployMultisiteClusterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy M4 cluster")

	testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)
	testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	return &WorkflowResult{}
}

// RunM4WithMCWorkflow deploys a M4 multisite cluster with monitoring console
func RunM4WithMCWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int, siteCount int, mcName string) *WorkflowResult {
	mc, err := deployment.DeployMonitoringConsole(ctx, mcName, "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

	err = deployment.DeployMultisiteClusterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcName)
	Expect(err).To(Succeed(), "Unable to deploy M4 cluster")

	testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)
	testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)
	testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, mcName, mc)

	return &WorkflowResult{MonitoringConsole: mc}
}

// RunM1DeploymentWorkflow deploys a M1 multisite indexer cluster (no SHC) and verifies components
func RunM1DeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int, siteCount int) *WorkflowResult {
	err := deployment.DeployMultisiteCluster(ctx, name, indexerReplicas, siteCount, "")
	Expect(err).To(Succeed(), "Unable to deploy M1 cluster")

	testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)
	testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
	testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

	return &WorkflowResult{}
}

// RunStandaloneWithServiceAccountWorkflow deploys standalone with a service account
func RunStandaloneWithServiceAccountWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, serviceAccountName string) *WorkflowResult {
	testcaseEnvInst.CreateServiceAccount(serviceAccountName)

	spec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				ImagePullPolicy: "IfNotPresent",
				Image:           testcaseEnvInst.GetSplunkImage(),
			},
			Volumes:        []corev1.Volume{},
			ServiceAccount: serviceAccountName,
		},
	}

	standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, name, spec)
	Expect(err).To(Succeed(), "Unable to deploy standalone with service account")

	testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, name, standalone)

	standalonePodName := fmt.Sprintf(StandalonePod, name, 0)
	testcaseEnvInst.VerifyServiceAccountConfiguredOnPod(deployment, testcaseEnvInst.GetName(), standalonePodName, serviceAccountName)

	return &WorkflowResult{Standalone: standalone}
}

// RunDeleteStandaloneWorkflow deploys and deletes a standalone instance
func RunDeleteStandaloneWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string) {
	result := RunStandaloneDeploymentWorkflow(ctx, deployment, testcaseEnvInst, name)

	err := deployment.DeleteCR(ctx, result.Standalone)
	Expect(err).To(Succeed(), "Unable to delete standalone instance")
}

// RunDeleteC3Workflow deploys and deletes a C3 cluster
func RunDeleteC3Workflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int) {
	RunC3DeploymentWorkflow(ctx, deployment, testcaseEnvInst, name, indexerReplicas, "")

	idxc := &enterpriseApi.IndexerCluster{}
	idxcName := name + "-idxc"
	err := deployment.GetInstance(ctx, idxcName, idxc)
	Expect(err).To(Succeed(), "Unable to get Indexer Cluster instance")

	err = deployment.DeleteCR(ctx, idxc)
	Expect(err).To(Succeed(), "Unable to delete Indexer Cluster")

	shc := &enterpriseApi.SearchHeadCluster{}
	shcName := name + "-shc"
	err = deployment.GetInstance(ctx, shcName, shc)
	Expect(err).To(Succeed(), "Unable to get Search Head Cluster instance")

	err = deployment.DeleteCR(ctx, shc)
	Expect(err).To(Succeed(), "Unable to delete Search Head Cluster")

	cm := &enterpriseApi.ClusterManager{}
	err = deployment.GetInstance(ctx, name, cm)
	Expect(err).To(Succeed(), "Unable to get Cluster Manager instance")

	err = deployment.DeleteCR(ctx, cm)
	Expect(err).To(Succeed(), "Unable to delete Cluster Manager")
}

// RunIngestAndSearchWorkflow ingests data and performs a search on a pod
func RunIngestAndSearchWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, podName string, indexName string, searchQuery string) {
	logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
	CreateMockLogfile(logFile, 100)
	IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)

	searchResults, err := PerformSearchSync(ctx, podName, searchQuery, deployment)
	Expect(err).To(Succeed(), "Failed to perform search")
	testcaseEnvInst.Log.Info("Search completed", "results", searchResults)
}

// RunMonitoringConsoleDeploymentWorkflow deploys a Monitoring Console instance and verifies it is ready
func RunMonitoringConsoleDeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string) *WorkflowResult {
	mc := testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, name, "")
	return &WorkflowResult{MonitoringConsole: mc}
}

// RunLicenseManagerDeploymentWorkflow deploys a License Manager instance and verifies it is ready
func RunLicenseManagerDeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string) *WorkflowResult {
	lm, err := deployment.DeployLicenseManager(ctx, name)
	Expect(err).To(Succeed(), "Unable to deploy License Manager")
	testcaseEnvInst.VerifyLicenseManagerReady(ctx, deployment)
	return &WorkflowResult{LicenseManager: lm}
}

// RunCompleteDataIngestionWorkflow performs complete data ingestion workflow: ingest, verify, roll to warm, verify on S3
func RunCompleteDataIngestionWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, podName string, indexName string, logLineCount int) {
	logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
	CreateMockLogfile(logFile, logLineCount)
	IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)

	RollHotToWarm(ctx, deployment, podName, indexName)

	testcaseEnvInst.VerifyIndexExistsOnS3(ctx, deployment, podName, indexName)
}
