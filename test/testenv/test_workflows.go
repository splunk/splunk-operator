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
func RunStandaloneDeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string) (*WorkflowResult, error) {
	standalone, err := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, name, "", "")
	if err != nil {
		return nil, err
	}
	return &WorkflowResult{Standalone: standalone}, nil
}

// RunC3DeploymentWorkflow deploys a C3 cluster (CM + IDXC + SHC) and verifies all components are ready
func RunC3DeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int, mcRef string) (*WorkflowResult, error) {
	if err := deployment.DeploySingleSiteCluster(ctx, name, indexerReplicas, true, mcRef); err != nil {
		return nil, fmt.Errorf("unable to deploy C3 cluster: %w", err)
	}

	if err := testcaseEnvInst.VerifyClusterReadyAndRFSF(ctx, deployment); err != nil {
		return nil, fmt.Errorf("cluster not ready: %w", err)
	}

	return &WorkflowResult{}, nil
}

// RunM4DeploymentWorkflow deploys a M4 multisite cluster and verifies all components are ready
func RunM4DeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int, siteCount int, mcRef string) (*WorkflowResult, error) {
	if err := deployment.DeployMultisiteClusterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef); err != nil {
		return nil, fmt.Errorf("unable to deploy M4 cluster: %w", err)
	}

	if err := testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady); err != nil {
		return nil, fmt.Errorf("M4 cluster not ready: %w", err)
	}
	if err := testcaseEnvInst.VerifyRFSFMet(ctx, deployment); err != nil {
		return nil, fmt.Errorf("RF/SF not met: %w", err)
	}

	return &WorkflowResult{}, nil
}

// RunM1DeploymentWorkflow deploys a M1 multisite indexer cluster (no SHC) and verifies components
func RunM1DeploymentWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int, siteCount int) (*WorkflowResult, error) {
	if err := deployment.DeployMultisiteCluster(ctx, name, indexerReplicas, siteCount, ""); err != nil {
		return nil, fmt.Errorf("unable to deploy M1 cluster: %w", err)
	}

	if err := testcaseEnvInst.VerifyM1ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady); err != nil {
		return nil, fmt.Errorf("M1 cluster not ready: %w", err)
	}
	if err := testcaseEnvInst.VerifyRFSFMet(ctx, deployment); err != nil {
		return nil, fmt.Errorf("RF/SF not met: %w", err)
	}

	return &WorkflowResult{}, nil
}

// RunStandaloneWithServiceAccountWorkflow deploys standalone with a service account
func RunStandaloneWithServiceAccountWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, serviceAccountName string) (*WorkflowResult, error) {
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
	if err != nil {
		return nil, fmt.Errorf("unable to deploy standalone with service account: %w", err)
	}

	if err = testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, name, standalone); err != nil {
		return nil, fmt.Errorf("standalone not ready: %w", err)
	}

	standalonePodName := fmt.Sprintf(StandalonePod, name, 0)
	if err = testcaseEnvInst.VerifyServiceAccountConfiguredOnPod(deployment, testcaseEnvInst.GetName(), standalonePodName, serviceAccountName); err != nil {
		return nil, fmt.Errorf("service account not configured: %w", err)
	}

	return &WorkflowResult{Standalone: standalone}, nil
}

// RunDeleteStandaloneWorkflow deploys and deletes a standalone instance
func RunDeleteStandaloneWorkflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string) error {
	result, err := RunStandaloneDeploymentWorkflow(ctx, deployment, testcaseEnvInst, name)
	if err != nil {
		return fmt.Errorf("unable to deploy standalone instance: %w", err)
	}

	if err := deployment.DeleteCR(ctx, result.Standalone); err != nil {
		return fmt.Errorf("unable to delete standalone instance: %w", err)
	}
	return nil
}

// RunDeleteC3Workflow deploys and deletes a C3 cluster
func RunDeleteC3Workflow(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, name string, indexerReplicas int) error {
	if _, err := RunC3DeploymentWorkflow(ctx, deployment, testcaseEnvInst, name, indexerReplicas, ""); err != nil {
		return err
	}

	if err := GetAndDeleteCR(ctx, deployment, &enterpriseApi.IndexerCluster{}, name+"-idxc"); err != nil {
		return fmt.Errorf("unable to delete Indexer Cluster: %w", err)
	}

	if err := GetAndDeleteCR(ctx, deployment, &enterpriseApi.SearchHeadCluster{}, name+"-shc"); err != nil {
		return fmt.Errorf("unable to delete Search Head Cluster: %w", err)
	}

	if err := GetAndDeleteCR(ctx, deployment, &enterpriseApi.ClusterManager{}, name); err != nil {
		return fmt.Errorf("unable to delete Cluster Manager: %w", err)
	}

	return nil
}
