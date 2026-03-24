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
