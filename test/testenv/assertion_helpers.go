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

// ScaleSearchHeadCluster scales a Search Head Cluster to the specified replica count
func (testcaseenv *TestCaseEnv) ScaleSearchHeadCluster(ctx context.Context, deployment *Deployment, deploymentName string, newReplicas int) {
	shcName := deploymentName + "-shc"

	// Get instance of current SHC CR with latest config
	shc := &enterpriseApi.SearchHeadCluster{}
	GetInstanceWithExpect(ctx, deployment, shc, shcName, "Failed to get instance of Search Head Cluster")

	// Update Replicas of SHC
	shc.Spec.Replicas = int32(newReplicas)
	UpdateCRWithExpect(ctx, deployment, shc, "Failed to scale Search Head Cluster")

	// Verify Search Head cluster scales up and goes to ScalingUp phase
	testcaseenv.VerifySearchHeadClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp)
}

// ScaleIndexerCluster scales an Indexer Cluster to the specified replica count
func (testcaseenv *TestCaseEnv) ScaleIndexerCluster(ctx context.Context, deployment *Deployment, deploymentName string, newReplicas int) {
	idxcName := deploymentName + "-idxc"

	// Get instance of current Indexer CR with latest config
	idxc := &enterpriseApi.IndexerCluster{}
	GetInstanceWithExpect(ctx, deployment, idxc, idxcName, "Failed to get instance of Indexer Cluster")

	// Update Replicas of Indexer Cluster
	idxc.Spec.Replicas = int32(newReplicas)
	UpdateCRWithExpect(ctx, deployment, idxc, "Failed to scale Indexer Cluster")

	// Verify Indexer cluster scales up and goes to ScalingUp phase
	testcaseenv.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp, idxcName)
}

// UpdateMonitoringConsoleRef updates the MonitoringConsoleRef in a CR and waits for the change to apply
func UpdateMonitoringConsoleRefAndVerify(ctx context.Context, deployment *Deployment, testcaseenv *TestCaseEnv, obj interface{}, instanceName string, newMCName string) {
	// Get current resource version before update
	resourceVersion := testcaseenv.GetResourceVersion(ctx, deployment, obj)

	// Update the MonitoringConsoleRef based on the type
	switch cr := obj.(type) {
	case *enterpriseApi.ClusterManager:
		GetInstanceWithExpect(ctx, deployment, cr, instanceName, "Failed to get instance")
		cr.Spec.MonitoringConsoleRef.Name = newMCName
		UpdateCRWithExpect(ctx, deployment, cr, "Failed to update MonitoringConsoleRef")
	case *enterpriseApi.SearchHeadCluster:
		GetInstanceWithExpect(ctx, deployment, cr, instanceName, "Failed to get instance")
		cr.Spec.MonitoringConsoleRef.Name = newMCName
		UpdateCRWithExpect(ctx, deployment, cr, "Failed to update MonitoringConsoleRef")
	}

	// Wait for custom resource version to change
	testcaseenv.VerifyCustomResourceVersionChanged(ctx, deployment, obj, resourceVersion)
}

// VerifyMCConfigForC3Cluster verifies the standard MC configuration for a C3 cluster
func (testcaseenv *TestCaseEnv) VerifyMCConfigForC3Cluster(ctx context.Context, deployment *Deployment, deploymentName string, mcName string, shReplicas int, indexerReplicas int, shouldExist bool) {
	// Check Cluster Manager in MC Config Map
	testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(ClusterMasterServiceName, deploymentName)}, "SPLUNK_CLUSTER_MASTER_URL", mcName, shouldExist)

	// Check Deployer in MC Config Map
	testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(DeployerServiceName, deploymentName)}, "SPLUNK_DEPLOYER_URL", mcName, shouldExist)

	// Check Search Head Pods in MC Config Map
	shPods := GeneratePodNameSlice(SearchHeadPod, deploymentName, shReplicas, false, 0)
	testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, shouldExist)

	// Check Search Heads in MC Pod config string
	testcaseenv.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, shouldExist, false)

	// Check Indexers in MC Pod config string
	indexerPods := GeneratePodNameSlice(IndexerPod, deploymentName, indexerReplicas, false, 0)
	testcaseenv.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, shouldExist, true)
}

// VerifyMCConfigForM4Cluster verifies the standard MC configuration for an M4 multisite cluster
func (testcaseenv *TestCaseEnv) VerifyMCConfigForM4Cluster(ctx context.Context, deployment *Deployment, deploymentName string, mcName string, shReplicas int, indexerReplicas int, siteCount int, shouldExist bool) {
	// Check Cluster Manager in MC Config Map
	testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(ClusterMasterServiceName, deploymentName)}, "SPLUNK_CLUSTER_MASTER_URL", mcName, shouldExist)

	// Check Deployer in MC Config Map
	testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, []string{fmt.Sprintf(DeployerServiceName, deploymentName)}, "SPLUNK_DEPLOYER_URL", mcName, shouldExist)

	// Check Search Head Pods in MC Config Map
	shPods := GeneratePodNameSlice(SearchHeadPod, deploymentName, shReplicas, false, 0)
	testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, shPods, "SPLUNK_SEARCH_HEAD_URL", mcName, shouldExist)

	// Check Search Heads in MC Pod config string
	testcaseenv.VerifyPodsInMCConfigString(ctx, deployment, shPods, mcName, shouldExist, false)

	// Check Indexers in MC Pod config string
	indexerPods := GeneratePodNameSlice(MultiSiteIndexerPod, deploymentName, indexerReplicas, true, siteCount)
	testcaseenv.VerifyPodsInMCConfigString(ctx, deployment, indexerPods, mcName, shouldExist, true)
}

// DeployAndVerifyC3WithMC deploys a C3 cluster with a given MC and verifies all components are ready
func (testcaseenv *TestCaseEnv) DeployAndVerifyC3WithMC(ctx context.Context, deployment *Deployment, deploymentName string, indexerReplicas int, mcName string) {
	err := deployment.DeploySingleSiteClusterMasterWithGivenMonitoringConsole(ctx, deploymentName, indexerReplicas, true, mcName)
	Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

	// Verify all components are ready
	testcaseenv.VerifyClusterMasterReady(ctx, deployment)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseenv.VerifySingleSiteIndexersReady(ctx, deployment)
}

// DeployStandaloneWithMCRef deploys a standalone instance with a MonitoringConsoleRef
func (testcaseenv *TestCaseEnv) DeployStandaloneWithMCRef(ctx context.Context, deployment *Deployment, deploymentName string, mcName string) *enterpriseApi.Standalone {
	spec := enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
			Spec: enterpriseApi.Spec{
				ImagePullPolicy: "IfNotPresent",
				Image:           testcaseenv.GetSplunkImage(),
			},
			MonitoringConsoleRef: corev1.ObjectReference{
				Name: mcName,
			},
		},
	}
	standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deploymentName, spec)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance")

	// Wait for Standalone to be in READY status
	testcaseenv.VerifyStandaloneReady(ctx, deployment, deploymentName, standalone)

	return standalone
}

// VerifyStandaloneInMC verifies that a standalone instance is configured in the MC
func (testcaseenv *TestCaseEnv) VerifyStandaloneInMC(ctx context.Context, deployment *Deployment, deploymentName string, mcName string, shouldExist bool) {
	standalonePod := fmt.Sprintf(StandalonePod, deploymentName, 0)
	testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, []string{standalonePod}, "SPLUNK_STANDALONE_URL", mcName, shouldExist)
	testcaseenv.VerifyPodsInMCConfigString(ctx, deployment, []string{standalonePod}, mcName, shouldExist, false)
}

// VerifyLMConfiguredOnIndexers verifies License Manager is configured on all indexer pods
func VerifyLMConfiguredOnIndexers(ctx context.Context, deployment *Deployment, deploymentName string, indexerCount int) {
	for i := 0; i < indexerCount; i++ {
		indexerPodName := fmt.Sprintf(IndexerPod, deploymentName, i)
		VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)
	}
}

// VerifyLMConfiguredOnSearchHeads verifies License Manager is configured on all search head pods
func VerifyLMConfiguredOnSearchHeads(ctx context.Context, deployment *Deployment, deploymentName string, searchHeadCount int) {
	for i := 0; i < searchHeadCount; i++ {
		searchHeadPodName := fmt.Sprintf(SearchHeadPod, deploymentName, i)
		VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)
	}
}

// VerifyLMConfiguredOnMultisiteIndexers verifies License Manager is configured on all multisite indexer pods
func VerifyLMConfiguredOnMultisiteIndexers(ctx context.Context, deployment *Deployment, deploymentName string, siteCount int) {
	for i := 1; i <= siteCount; i++ {
		indexerPodName := fmt.Sprintf(MultiSiteIndexerPod, deploymentName, i, 0)
		VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)
	}
}

// IngestDataOnIndexers ingests test data on all indexer pods
func IngestDataOnIndexers(ctx context.Context, deployment *Deployment, deploymentName string, indexerCount int, indexName string, logLineCount int) {
	for i := 0; i < indexerCount; i++ {
		podName := fmt.Sprintf(IndexerPod, deploymentName, i)
		logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
		CreateMockLogfile(logFile, logLineCount)
		IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)
	}
}

// IngestDataOnMultisiteIndexers ingests test data on all multisite indexer pods
func IngestDataOnMultisiteIndexers(ctx context.Context, deployment *Deployment, deploymentName string, siteCount int, indexName string, logLineCount int) {
	for site := 1; site <= siteCount; site++ {
		podName := fmt.Sprintf(MultiSiteIndexerPod, deploymentName, site, 0)
		logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
		CreateMockLogfile(logFile, logLineCount)
		IngestFileViaMonitor(ctx, logFile, indexName, podName, deployment)
	}
}
