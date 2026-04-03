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

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
)

// ScaleSearchHeadCluster scales a Search Head Cluster to the specified replica count
func (testcaseenv *TestCaseEnv) ScaleSearchHeadCluster(ctx context.Context, deployment *Deployment, deploymentName string, newReplicas int) error {
	shcName := deploymentName + "-shc"

	// Get instance of current SHC CR with latest config
	shc := &enterpriseApi.SearchHeadCluster{}
	if err := deployment.GetInstance(ctx, shcName, shc); err != nil {
		return fmt.Errorf("failed to get instance of Search Head Cluster: %w", err)
	}

	// Update Replicas of SHC
	shc.Spec.Replicas = int32(newReplicas)
	if err := deployment.UpdateCR(ctx, shc); err != nil {
		return fmt.Errorf("failed to scale Search Head Cluster: %w", err)
	}

	// Verify Search Head Cluster scales up and goes to ScalingUp phase
	testcaseenv.VerifySearchHeadClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp)
	return nil
}

// ScaleIndexerCluster scales an Indexer Cluster to the specified replica count
func (testcaseenv *TestCaseEnv) ScaleIndexerCluster(ctx context.Context, deployment *Deployment, deploymentName string, newReplicas int) error {
	idxcName := deploymentName + "-idxc"

	// Get instance of current Indexer CR with latest config
	idxc := &enterpriseApi.IndexerCluster{}
	if err := deployment.GetInstance(ctx, idxcName, idxc); err != nil {
		return fmt.Errorf("failed to get instance of Indexer Cluster: %w", err)
	}

	// Update Replicas of Indexer Cluster
	idxc.Spec.Replicas = int32(newReplicas)
	if err := deployment.UpdateCR(ctx, idxc); err != nil {
		return fmt.Errorf("failed to scale Indexer Cluster: %w", err)
	}

	// Verify Indexer Cluster scales up and goes to ScalingUp phase
	testcaseenv.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp, idxcName)
	return nil
}

// UpdateMonitoringConsoleRefAndVerify updates the MonitoringConsoleRef in a CR and waits for the change to apply
func UpdateMonitoringConsoleRefAndVerify(ctx context.Context, deployment *Deployment, testcaseenv *TestCaseEnv, obj interface{}, instanceName string, newMCName string) error {
	// Get current resource version before update
	resourceVersion := testcaseenv.GetResourceVersion(ctx, deployment, obj)

	// Update the MonitoringConsoleRef based on the type
	switch cr := obj.(type) {
	case *enterpriseApi.ClusterManager:
		if err := deployment.GetInstance(ctx, instanceName, cr); err != nil {
			return fmt.Errorf("failed to get instance %s: %w", instanceName, err)
		}
		cr.Spec.MonitoringConsoleRef.Name = newMCName
		if err := deployment.UpdateCR(ctx, cr); err != nil {
			return fmt.Errorf("failed to update MonitoringConsoleRef: %w", err)
		}
	case *enterpriseApiV3.ClusterMaster:
		if err := deployment.GetInstance(ctx, instanceName, cr); err != nil {
			return fmt.Errorf("failed to get instance %s: %w", instanceName, err)
		}
		cr.Spec.MonitoringConsoleRef.Name = newMCName
		if err := deployment.UpdateCR(ctx, cr); err != nil {
			return fmt.Errorf("failed to update MonitoringConsoleRef: %w", err)
		}
	case *enterpriseApi.SearchHeadCluster:
		if err := deployment.GetInstance(ctx, instanceName, cr); err != nil {
			return fmt.Errorf("failed to get instance %s: %w", instanceName, err)
		}
		cr.Spec.MonitoringConsoleRef.Name = newMCName
		if err := deployment.UpdateCR(ctx, cr); err != nil {
			return fmt.Errorf("failed to update MonitoringConsoleRef: %w", err)
		}
	}

	// Wait for custom resource version to change
	testcaseenv.VerifyCustomResourceVersionChanged(ctx, deployment, obj, resourceVersion)
	return nil
}

// DeployStandaloneWithMCRef deploys a standalone instance with a MonitoringConsoleRef
func (testcaseenv *TestCaseEnv) DeployStandaloneWithMCRef(ctx context.Context, deployment *Deployment, deploymentName string, mcName string) (*enterpriseApi.Standalone, error) {
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
	if err != nil {
		return nil, fmt.Errorf("unable to deploy standalone instance: %w", err)
	}

	// Wait for Standalone to be in READY status
	testcaseenv.VerifyStandaloneReady(ctx, deployment, deploymentName, standalone)

	return standalone, nil
}

// VerifyStandaloneInMC verifies that a standalone instance is configured in the MC
func (testcaseenv *TestCaseEnv) VerifyStandaloneInMC(ctx context.Context, deployment *Deployment, deploymentName string, mcName string, shouldExist bool) {
	standalonePod := fmt.Sprintf(StandalonePod, deploymentName, 0)
	testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, []string{standalonePod}, "SPLUNK_STANDALONE_URL", mcName, shouldExist)
	testcaseenv.VerifyPodsInMCConfigString(ctx, deployment, []string{standalonePod}, mcName, shouldExist, false)
}

// VerifyLMConfiguredOnPods verifies License Manager is configured on all given pods
func VerifyLMConfiguredOnPods(ctx context.Context, deployment *Deployment, podNames []string) {
	for _, podName := range podNames {
		VerifyLMConfiguredOnPod(ctx, deployment, podName)
	}
}

// IngestDataOnIndexers ingests test data on all indexer pods
func IngestDataOnIndexers(ctx context.Context, deployment *Deployment, deploymentName string, indexerCount int) {
	for i := 0; i < indexerCount; i++ {
		podName := fmt.Sprintf(IndexerPod, deploymentName, i)
		logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
		CreateMockLogfile(logFile, LogLineCount)
		IngestFileViaMonitor(ctx, logFile, DefaultIngestIndex, podName, deployment)
	}
}

// VerifyM1ClusterReady verifies the cluster coordinator, indexers, and multisite status are ready (no SHC).
func (testcaseenv *TestCaseEnv) VerifyM1ClusterReady(ctx context.Context, deployment *Deployment, siteCount int, verifyCoordinator func(context.Context, *Deployment)) {
	verifyCoordinator(ctx, deployment)
	testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseenv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
}

// VerifyM4ClusterReady verifies the cluster coordinator, indexers, multisite status, and SHC are ready.
func (testcaseenv *TestCaseEnv) VerifyM4ClusterReady(ctx context.Context, deployment *Deployment, siteCount int, verifyCoordinator func(context.Context, *Deployment)) {
	verifyCoordinator(ctx, deployment)
	testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseenv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
}

// VerifyM4IndexersAndSHCReady verifies the cluster coordinator, indexers, and SHC are ready (without multisite check).
func (testcaseenv *TestCaseEnv) VerifyM4IndexersAndSHCReady(ctx context.Context, deployment *Deployment, siteCount int, verifyCoordinator func(context.Context, *Deployment)) {
	verifyCoordinator(ctx, deployment)
	testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
}

// VerifyC3ClusterReady verifies the cluster coordinator, SHC, and single-site indexers are ready.
func (testcaseenv *TestCaseEnv) VerifyC3ClusterReady(ctx context.Context, deployment *Deployment, verifyCoordinator func(context.Context, *Deployment)) {
	verifyCoordinator(ctx, deployment)
	testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
	testcaseenv.VerifySingleSiteIndexersReady(ctx, deployment)
}

// IngestDataOnMultisiteIndexers ingests test data on all multisite indexer pods
func IngestDataOnMultisiteIndexers(ctx context.Context, deployment *Deployment, deploymentName string, siteCount int) {
	for site := 1; site <= siteCount; site++ {
		podName := fmt.Sprintf(MultiSiteIndexerPod, deploymentName, site, 0)
		logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
		CreateMockLogfile(logFile, LogLineCount)
		IngestFileViaMonitor(ctx, logFile, DefaultIngestIndex, podName, deployment)
	}
}
