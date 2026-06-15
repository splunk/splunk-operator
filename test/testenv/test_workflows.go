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

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	corev1 "k8s.io/api/core/v1"
)

// WorkflowResult contains the result of a workflow execution.
// Only fields that are actually populated by the workflow are kept here;
// extend the struct when a new workflow needs to return additional CRs.
type WorkflowResult struct {
	Standalone *enterpriseApi.Standalone
}

// RunStandaloneDeploymentWorkflow deploys a Standalone instance and verifies it's ready
func (testcaseEnvInst *TestCaseEnv) RunStandaloneDeploymentWorkflow(ctx context.Context, deployment *Deployment) (*WorkflowResult, error) {
	standalone, err := testcaseEnvInst.DeployAndVerifyStandalone(ctx, deployment, "")
	if err != nil {
		return nil, err
	}
	return &WorkflowResult{Standalone: standalone}, nil
}

// RunC3DeploymentWorkflow deploys a C3 cluster (CM + IDXC + SHC) and verifies all components are ready
func (testcaseEnvInst *TestCaseEnv) RunC3DeploymentWorkflow(ctx context.Context, deployment *Deployment, indexerReplicas int) (*WorkflowResult, error) {
	if err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), indexerReplicas, true); err != nil {
		return nil, fmt.Errorf("unable to deploy C3 cluster: %w", err)
	}

	if err := testcaseEnvInst.VerifyClusterReadyAndRFSF(ctx, deployment); err != nil {
		return nil, fmt.Errorf("cluster not ready: %w", err)
	}

	return &WorkflowResult{}, nil
}

// RunM4DeploymentWorkflow deploys a M4 multisite cluster and verifies all components are ready
func (testcaseEnvInst *TestCaseEnv) RunM4DeploymentWorkflow(ctx context.Context, deployment *Deployment, indexerReplicas int, siteCount int) (*WorkflowResult, error) {
	if err := deployment.DeployMultisiteClusterWithSearchHead(ctx, deployment.GetName(), indexerReplicas, siteCount); err != nil {
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

// RunM1DeploymentWorkflow deploys an M1 multisite Indexer Cluster (no SHC) and verifies components
func (testcaseEnvInst *TestCaseEnv) RunM1DeploymentWorkflow(ctx context.Context, deployment *Deployment, indexerReplicas int, siteCount int) (*WorkflowResult, error) {
	if err := deployment.DeployMultisiteCluster(ctx, deployment.GetName(), indexerReplicas, siteCount); err != nil {
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

// RunStandaloneWithServiceAccountWorkflow deploys a Standalone with a service account
func (testcaseEnvInst *TestCaseEnv) RunStandaloneWithServiceAccountWorkflow(ctx context.Context, deployment *Deployment, serviceAccountName string) (*WorkflowResult, error) {
	if err := testcaseEnvInst.CreateServiceAccount(serviceAccountName); err != nil {
		return nil, fmt.Errorf("unable to create service account: %w", err)
	}

	name := deployment.GetName()
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
	if err = testcaseEnvInst.VerifyServiceAccountConfiguredOnPod(ctx, testcaseEnvInst.GetName(), standalonePodName, serviceAccountName); err != nil {
		return nil, fmt.Errorf("service account not configured: %w", err)
	}

	return &WorkflowResult{Standalone: standalone}, nil
}

// RunDeleteStandaloneWorkflow deploys and deletes a standalone instance
func (testcaseEnvInst *TestCaseEnv) RunDeleteStandaloneWorkflow(ctx context.Context, deployment *Deployment) error {
	result, err := testcaseEnvInst.RunStandaloneDeploymentWorkflow(ctx, deployment)
	if err != nil {
		return fmt.Errorf("unable to deploy Standalone instance: %w", err)
	}

	if err := deployment.DeleteCR(ctx, result.Standalone); err != nil {
		return fmt.Errorf("unable to delete Standalone instance: %w", err)
	}
	return nil
}

// RunDeleteC3Workflow deploys and deletes a C3 cluster
func (testcaseEnvInst *TestCaseEnv) RunDeleteC3Workflow(ctx context.Context, deployment *Deployment, indexerReplicas int) error {
	if _, err := testcaseEnvInst.RunC3DeploymentWorkflow(ctx, deployment, indexerReplicas); err != nil {
		return err
	}

	name := deployment.GetName()
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
