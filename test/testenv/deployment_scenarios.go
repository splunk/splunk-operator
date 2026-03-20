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

	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// DeploymentScenarioResult holds the result of a deployment scenario
type DeploymentScenarioResult struct {
	Standalone        *enterpriseApi.Standalone
	ClusterManager    *enterpriseApi.ClusterManager
	IndexerCluster    *enterpriseApi.IndexerCluster
	SearchHeadCluster *enterpriseApi.SearchHeadCluster
	MonitoringConsole *enterpriseApi.MonitoringConsole
	LicenseManager    *enterpriseApi.LicenseManager
}

// DeployStandardS1 deploys a standard S1 (Standalone) configuration
func (testcaseEnv *TestCaseEnv) DeployStandardS1(ctx context.Context, deployment *Deployment) *enterpriseApi.Standalone {
	standalone, err := deployment.DeployStandalone(ctx, deployment.GetName(), "", "")
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	// Verify standalone goes to ready state
	testcaseEnv.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

	return standalone
}

// DeployStandardS1WithLM deploys S1 with License Manager
func (testcaseEnv *TestCaseEnv) DeployStandardS1WithLM(ctx context.Context, deployment *Deployment) (*enterpriseApi.Standalone, *enterpriseApi.LicenseManager) {
	// Download License File and create config map
	SetupLicenseConfigMap(ctx, testcaseEnv)

	// Create standalone Deployment with License Manager
	mcRef := deployment.GetName()
	standalone, err := deployment.DeployStandaloneWithLM(ctx, deployment.GetName(), mcRef)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

	// Wait for License Manager to be in READY status
	testcaseEnv.VerifyLicenseManagerReady(ctx, deployment)

	// Wait for Standalone to be in READY status
	testcaseEnv.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

	// Get License Manager instance
	lm := &enterpriseApi.LicenseManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), lm)
	Expect(err).To(Succeed(), "Unable to get License Manager instance")

	return standalone, lm
}

// DeployStandardS1WithMC deploys S1 with Monitoring Console
func (testcaseEnv *TestCaseEnv) DeployStandardS1WithMC(ctx context.Context, deployment *Deployment) (*enterpriseApi.Standalone, *enterpriseApi.MonitoringConsole) {
	// Deploy Standalone
	standalone := testcaseEnv.DeployStandardS1(ctx, deployment)

	// Deploy Monitoring Console
	mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	return standalone, mc
}

// DeployStandardC3 deploys a standard C3 (Clustered indexer, search head cluster) configuration
func (testcaseEnv *TestCaseEnv) DeployStandardC3(ctx context.Context, deployment *Deployment, indexerReplicas int) *DeploymentScenarioResult {
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), indexerReplicas, true /*shc*/, "")
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure that the cluster-manager goes to Ready phase
	testcaseEnv.VerifyClusterManagerReady(ctx, deployment)

	// Ensure Search Head Cluster go to Ready phase
	testcaseEnv.VerifySearchHeadClusterReady(ctx, deployment)

	// Ensure Indexers go to Ready phase
	testcaseEnv.VerifySingleSiteIndexersReady(ctx, deployment)

	// Verify RF SF is met
	testcaseEnv.VerifyRFSFMet(ctx, deployment)

	// Get deployed instances
	cm := &enterpriseApi.ClusterManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	Expect(err).To(Succeed(), "Unable to get Cluster Manager instance")

	shc := &enterpriseApi.SearchHeadCluster{}
	shcName := deployment.GetName() + "-shc"
	err = deployment.GetInstance(ctx, shcName, shc)
	Expect(err).To(Succeed(), "Unable to get Search Head Cluster instance")

	idxc := &enterpriseApi.IndexerCluster{}
	idxcName := deployment.GetName() + "-idxc"
	err = deployment.GetInstance(ctx, idxcName, idxc)
	Expect(err).To(Succeed(), "Unable to get Indexer Cluster instance")

	return &DeploymentScenarioResult{
		ClusterManager:    cm,
		SearchHeadCluster: shc,
		IndexerCluster:    idxc,
	}
}

// DeployStandardC3WithLM deploys C3 with License Manager
func (testcaseEnv *TestCaseEnv) DeployStandardC3WithLM(ctx context.Context, deployment *Deployment, indexerReplicas int) *DeploymentScenarioResult {
	// Download License File and create config map
	SetupLicenseConfigMap(ctx, testcaseEnv)

	mcRef := deployment.GetName()
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), indexerReplicas, true /*shc*/, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure that the cluster-manager goes to Ready phase
	testcaseEnv.VerifyClusterManagerReady(ctx, deployment)

	// Ensure Search Head Cluster go to Ready phase
	testcaseEnv.VerifySearchHeadClusterReady(ctx, deployment)

	// Ensure Indexers go to Ready phase
	testcaseEnv.VerifySingleSiteIndexersReady(ctx, deployment)

	// Verify RF SF is met
	testcaseEnv.VerifyRFSFMet(ctx, deployment)

	// Get deployed instances
	cm := &enterpriseApi.ClusterManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	Expect(err).To(Succeed(), "Unable to get Cluster Manager instance")

	shc := &enterpriseApi.SearchHeadCluster{}
	shcName := deployment.GetName() + "-shc"
	err = deployment.GetInstance(ctx, shcName, shc)
	Expect(err).To(Succeed(), "Unable to get Search Head Cluster instance")

	idxc := &enterpriseApi.IndexerCluster{}
	idxcName := deployment.GetName() + "-idxc"
	err = deployment.GetInstance(ctx, idxcName, idxc)
	Expect(err).To(Succeed(), "Unable to get Indexer Cluster instance")

	lm := &enterpriseApi.LicenseManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), lm)
	Expect(err).To(Succeed(), "Unable to get License Manager instance")

	return &DeploymentScenarioResult{
		ClusterManager:    cm,
		SearchHeadCluster: shc,
		IndexerCluster:    idxc,
		LicenseManager:    lm,
	}
}

// DeployStandardC3WithMC deploys C3 with Monitoring Console
func (testcaseEnv *TestCaseEnv) DeployStandardC3WithMC(ctx context.Context, deployment *Deployment, indexerReplicas int) *DeploymentScenarioResult {
	result := testcaseEnv.DeployStandardC3(ctx, deployment, indexerReplicas)

	// Deploy Monitoring Console
	mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	result.MonitoringConsole = mc
	return result
}

// DeployStandardM4 deploys a standard M4 (Multisite indexer cluster, Search head cluster) configuration
func (testcaseEnv *TestCaseEnv) DeployStandardM4(ctx context.Context, deployment *Deployment, indexerReplicas int, siteCount int) *DeploymentScenarioResult {
	err := deployment.DeployMultisiteClusterWithSearchHead(ctx, deployment.GetName(), indexerReplicas, siteCount, "")
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure that the cluster-manager goes to Ready phase
	testcaseEnv.VerifyClusterManagerReady(ctx, deployment)

	// Ensure the indexers of all sites go to Ready phase
	testcaseEnv.VerifyIndexersReady(ctx, deployment, siteCount)

	// Ensure cluster configured as multisite
	testcaseEnv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

	// Ensure search head cluster go to Ready phase
	testcaseEnv.VerifySearchHeadClusterReady(ctx, deployment)

	// Verify RF SF is met
	testcaseEnv.VerifyRFSFMet(ctx, deployment)

	// Get deployed instances
	cm := &enterpriseApi.ClusterManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	Expect(err).To(Succeed(), "Unable to get Cluster Manager instance")

	shc := &enterpriseApi.SearchHeadCluster{}
	shcName := deployment.GetName() + "-shc"
	err = deployment.GetInstance(ctx, shcName, shc)
	Expect(err).To(Succeed(), "Unable to get Search Head Cluster instance")

	return &DeploymentScenarioResult{
		ClusterManager:    cm,
		SearchHeadCluster: shc,
	}
}

// DeployStandardM4WithLM deploys M4 with License Manager
func (testcaseEnv *TestCaseEnv) DeployStandardM4WithLM(ctx context.Context, deployment *Deployment, indexerReplicas int, siteCount int) *DeploymentScenarioResult {
	// Download License File and create config map
	SetupLicenseConfigMap(ctx, testcaseEnv)

	mcRef := deployment.GetName()
	err := deployment.DeployMultisiteClusterWithSearchHead(ctx, deployment.GetName(), indexerReplicas, siteCount, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure that the cluster-manager goes to Ready phase
	testcaseEnv.VerifyClusterManagerReady(ctx, deployment)

	// Ensure the indexers of all sites go to Ready phase
	testcaseEnv.VerifyIndexersReady(ctx, deployment, siteCount)

	// Ensure cluster configured as multisite
	testcaseEnv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

	// Ensure search head cluster go to Ready phase
	testcaseEnv.VerifySearchHeadClusterReady(ctx, deployment)

	// Verify RF SF is met
	testcaseEnv.VerifyRFSFMet(ctx, deployment)

	// Get deployed instances
	cm := &enterpriseApi.ClusterManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	Expect(err).To(Succeed(), "Unable to get Cluster Manager instance")

	shc := &enterpriseApi.SearchHeadCluster{}
	shcName := deployment.GetName() + "-shc"
	err = deployment.GetInstance(ctx, shcName, shc)
	Expect(err).To(Succeed(), "Unable to get Search Head Cluster instance")

	lm := &enterpriseApi.LicenseManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), lm)
	Expect(err).To(Succeed(), "Unable to get License Manager instance")

	return &DeploymentScenarioResult{
		ClusterManager:    cm,
		SearchHeadCluster: shc,
		LicenseManager:    lm,
	}
}

// DeployStandardM4WithMC deploys M4 with Monitoring Console
func (testcaseEnv *TestCaseEnv) DeployStandardM4WithMC(ctx context.Context, deployment *Deployment, indexerReplicas int, siteCount int) *DeploymentScenarioResult {
	result := testcaseEnv.DeployStandardM4(ctx, deployment, indexerReplicas, siteCount)

	// Deploy Monitoring Console
	mc, err := deployment.DeployMonitoringConsole(ctx, deployment.GetName(), "")
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify Monitoring Console is Ready and stays in ready state
	testcaseEnv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

	result.MonitoringConsole = mc
	return result
}

// DeployStandardM1 deploys a standard M1 (Multisite indexer cluster only) configuration
func (testcaseEnv *TestCaseEnv) DeployStandardM1(ctx context.Context, deployment *Deployment, indexerReplicas int, siteCount int) *DeploymentScenarioResult {
	err := deployment.DeployMultisiteCluster(ctx, deployment.GetName(), indexerReplicas, siteCount, "")
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Ensure that the cluster-manager goes to Ready phase
	testcaseEnv.VerifyClusterManagerReady(ctx, deployment)

	// Ensure the indexers of all sites go to Ready phase
	testcaseEnv.VerifyIndexersReady(ctx, deployment, siteCount)

	// Ensure cluster configured as multisite
	testcaseEnv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)

	// Verify RF SF is met
	testcaseEnv.VerifyRFSFMet(ctx, deployment)

	// Get deployed instances
	cm := &enterpriseApi.ClusterManager{}
	err = deployment.GetInstance(ctx, deployment.GetName(), cm)
	Expect(err).To(Succeed(), "Unable to get Cluster Manager instance")

	return &DeploymentScenarioResult{
		ClusterManager: cm,
	}
}
