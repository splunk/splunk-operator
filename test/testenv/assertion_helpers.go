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
func (testcaseenv *TestCaseEnv) ScaleSearchHeadCluster(ctx context.Context, deployment *Deployment, newReplicas int) error {
	shcName := deployment.GetName() + "-shc"

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
	return testcaseenv.VerifySearchHeadClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp)
}

// ScaleIndexerCluster scales an Indexer Cluster to the specified replica count
func (testcaseenv *TestCaseEnv) ScaleIndexerCluster(ctx context.Context, deployment *Deployment, newReplicas int) error {
	idxcName := deployment.GetName() + "-idxc"

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
	return testcaseenv.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp, idxcName)
}

// UpdateMonitoringConsoleRefAndVerify updates the MonitoringConsoleRef in a CR and waits for the change to apply
func (testcaseenv *TestCaseEnv) UpdateMonitoringConsoleRefAndVerify(ctx context.Context, deployment *Deployment, obj interface{}, instanceName string, newMCName string) error {
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
	return testcaseenv.VerifyCustomResourceVersionChanged(ctx, deployment, obj, resourceVersion)
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
	if err = testcaseenv.VerifyStandaloneReady(ctx, deployment, deploymentName, standalone); err != nil {
		return nil, fmt.Errorf("standalone not ready: %w", err)
	}

	return standalone, nil
}

// VerifyStandaloneInMC verifies that a standalone instance is configured in the MC
func (testcaseenv *TestCaseEnv) VerifyStandaloneInMC(ctx context.Context, deployment *Deployment, deploymentName string, mcName string, shouldExist bool) error {
	standalonePod := fmt.Sprintf(StandalonePod, deploymentName, 0)
	if err := testcaseenv.VerifyPodsInMCConfigMap(ctx, deployment, []string{standalonePod}, "SPLUNK_STANDALONE_URL", mcName, shouldExist); err != nil {
		return err
	}
	return testcaseenv.VerifyPodsInMCConfigString(ctx, []string{standalonePod}, mcName, shouldExist, false)
}

// VerifyLMConfiguredOnPods verifies License Manager is configured on all given pods
func VerifyLMConfiguredOnPods(ctx context.Context, deployment *Deployment, podNames []string) error {
	for _, podName := range podNames {
		if err := VerifyLMConfiguredOnPod(ctx, deployment, podName); err != nil {
			return err
		}
	}
	return nil
}

// VerifyM1ClusterReady verifies the cluster coordinator, indexers, and multisite status are ready (no SHC).
func (testcaseenv *TestCaseEnv) VerifyM1ClusterReady(ctx context.Context, deployment *Deployment, siteCount int, verifyCoordinator func(context.Context, *Deployment) error) error {
	if err := verifyCoordinator(ctx, deployment); err != nil {
		return err
	}
	if err := testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount); err != nil {
		return err
	}
	return testcaseenv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)
}

// VerifyM4ClusterReady verifies the cluster coordinator, indexers, multisite status, and SHC are ready.
func (testcaseenv *TestCaseEnv) VerifyM4ClusterReady(ctx context.Context, deployment *Deployment, siteCount int, verifyCoordinator func(context.Context, *Deployment) error) error {
	if err := verifyCoordinator(ctx, deployment); err != nil {
		return err
	}
	if err := testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount); err != nil {
		return err
	}
	if err := testcaseenv.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount); err != nil {
		return err
	}
	return testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
}

// VerifyM4IndexersAndSHCReady verifies the cluster coordinator, indexers, and SHC are ready (without multisite check).
func (testcaseenv *TestCaseEnv) VerifyM4IndexersAndSHCReady(ctx context.Context, deployment *Deployment, siteCount int, verifyCoordinator func(context.Context, *Deployment) error) error {
	if err := verifyCoordinator(ctx, deployment); err != nil {
		return err
	}
	if err := testcaseenv.VerifyIndexersReady(ctx, deployment, siteCount); err != nil {
		return err
	}
	return testcaseenv.VerifySearchHeadClusterReady(ctx, deployment)
}

// VerifyC3ClusterReady verifies the cluster coordinator, SHC, and single-site indexers are ready.
func (testcaseenv *TestCaseEnv) VerifyC3ClusterReady(ctx context.Context, deployment *Deployment, verifyCoordinator func(context.Context, *Deployment) error) error {
	if err := verifyCoordinator(ctx, deployment); err != nil {
		return err
	}
	if err := testcaseenv.VerifySearchHeadClusterReady(ctx, deployment); err != nil {
		return err
	}
	return testcaseenv.VerifySingleSiteIndexersReady(ctx, deployment)
}

// IngestDataOnIndexers ingests test data on all indexer pods
func IngestDataOnIndexers(ctx context.Context, deployment *Deployment, indexerCount int) {
	for i := 0; i < indexerCount; i++ {
		podName := fmt.Sprintf(IndexerPod, deployment.GetName(), i)
		logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
		CreateMockLogfile(logFile, LogLineCount)
		IngestFileViaMonitor(ctx, deployment, logFile, DefaultIngestIndex, podName)
	}
}

// IngestDataOnMultisiteIndexers ingests test data on all multisite indexer pods
func IngestDataOnMultisiteIndexers(ctx context.Context, deployment *Deployment, siteCount int) {
	for site := 1; site <= siteCount; site++ {
		podName := fmt.Sprintf(MultiSiteIndexerPod, deployment.GetName(), site, 0)
		logFile := fmt.Sprintf("test-log-%s.log", RandomDNSName(3))
		CreateMockLogfile(logFile, LogLineCount)
		IngestFileViaMonitor(ctx, deployment, logFile, DefaultIngestIndex, podName)
	}
}
