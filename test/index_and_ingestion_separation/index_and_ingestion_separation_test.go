// Copyright (c) 2018-2025 Splunk Inc. All rights reserved.

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
package indingsep

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("indingsep test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	var cmSpec enterpriseApi.ClusterManagerSpec

	ctx := context.TODO()

	BeforeEach(func() {
		var err error

		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")

		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")

		cmSpec = enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           testcaseEnvInst.GetSplunkImage(),
				},
			},
		}
	})

	AfterEach(func() {
		if types.SpecState(CurrentSpecReport().State) == types.SpecStateFailed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}

		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Ingestor and Indexer deployment", func() {
		It("indingsep, smoke, indingsep: Splunk Operator can deploy Ingestors and Indexers", func() {
			// Create Service Account
			testcaseEnvInst.Log.Info("Create Service Account")
			testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err := deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Deploy Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster")

			// Ensure that Ingestor Cluster is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Ingestor Cluster is in Ready phase")
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Cluster Manager is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Cluster Manager is in Ready phase")
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Indexer Cluster is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Indexer Cluster is in Ready phase")
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Delete the Indexer Cluster
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", idxc)
			Expect(err).To(Succeed(), "Unable to get Indexer Cluster instance", "Indexer Cluster Name", idxc)
			err = deployment.DeleteCR(ctx, idxc)
			Expect(err).To(Succeed(), "Unable to delete Indexer Cluster instance", "Indexer Cluster Name", idxc)

			// Delete the Ingestor Cluster
			ingest := &enterpriseApi.IngestorCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)
			Expect(err).To(Succeed(), "Unable to get Ingestor Cluster instance", "Ingestor Cluster Name", ingest)
			err = deployment.DeleteCR(ctx, ingest)
			Expect(err).To(Succeed(), "Unable to delete Ingestor Cluster instance", "Ingestor Cluster Name", ingest)
		})
	})

	// 	Context("Ingestor and Indexer deployment", func() {
	// 	It("indingsep, smoke, indingsep: Splunk Operator can deploy Ingestors and Indexers with additional configurations", func() {
	// 		// Create Service Account
	// 		testcaseEnvInst.Log.Info("Create Service Account")
	// 		testcaseEnvInst.CreateServiceAccount(serviceAccountName)

	// 		// Deploy Ingestor Cluster
	// 		testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
	// 		_, err := deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, bus, pipelineConfig, serviceAccountName)
	// 		Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

	// 		// Deploy Cluster Manager
	// 		testcaseEnvInst.Log.Info("Deploy Cluster Manager")
	// 		_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
	// 		Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

	// 		// Deploy Indexer Cluster
	// 		testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
	// 		_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", bus, pipelineConfig, serviceAccountName)
	// 		Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster")

	// 		// Ensure that Ingestor Cluster is in Ready phase
	// 		testcaseEnvInst.Log.Info("Ensure that Ingestor Cluster is in Ready phase")
	// 		testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

	// 		// Ensure that Cluster Manager is in Ready phase
	// 		testcaseEnvInst.Log.Info("Ensure that Cluster Manager is in Ready phase")
	// 		testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

	// 		// Ensure that Indexer Cluster is in Ready phase
	// 		testcaseEnvInst.Log.Info("Ensure that Indexer Cluster is in Ready phase")
	// 		testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

	// 	})
	// })

	Context("Ingestor and Indexer deployment", func() {
		It("indingsep, integration, indingsep: Splunk Operator can deploy Ingestors and Indexers with correct setup", func() {
			// Create Service Account
			testcaseEnvInst.Log.Info("Create Service Account")
			testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err := deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Deploy Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster")

			// Ensure that Ingestor Cluster is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Ingestor Cluster is in Ready phase")
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Cluster Manager is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Cluster Manager is in Ready phase")
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Indexer Cluster is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Indexer Cluster is in Ready phase")
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Get instance of current Ingestor Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Ingestor Cluster CR with latest config")
			ingest := &enterpriseApi.IngestorCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)
			Expect(err).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Verify Ingestor Cluster Status
			testcaseEnvInst.Log.Info("Verify Ingestor Cluster Status")
			Expect(ingest.Status.PushBus).To(Equal(bus), "Ingestor PushBus status is not the same as provided as input")
			Expect(ingest.Status.PipelineConfig).To(Equal(pipelineConfig), "Ingestor PipelineConfig status is not the same as provided as input")

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Verify Indexer Cluster Status
			testcaseEnvInst.Log.Info("Verify Indexer Cluster Status")
			Expect(index.Status.PullBus).To(Equal(bus), "Indexer PullBus status is not the same as provided as input")
			Expect(index.Status.PipelineConfig).To(Equal(pipelineConfig), "Indexer PipelineConfig status is not the same as provided as input")

			// Verify conf files
			testcaseEnvInst.Log.Info("Verify conf files")
			pods := testenv.DumpGetPods(deployment.GetName())
			for _, pod := range pods {
				defaultsConf := ""

				if strings.Contains(pod, "ingest") || strings.Contains(pod, "idxc") {
					// Verify outputs.conf
					testcaseEnvInst.Log.Info("Verify outputs.conf")
					outputsPath := "opt/splunk/etc/system/local/outputs.conf"
					outputsConf, err := testenv.GetConfFile(pod, outputsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get outputs.conf from Ingestor Cluster pod")
					testenv.ValidateContent(outputsConf, outputs, true)

					// Verify default-mode.conf
					testcaseEnvInst.Log.Info("Verify default-mode.conf")
					defaultsPath := "opt/splunk/etc/system/local/default-mode.conf"
					defaultsConf, err := testenv.GetConfFile(pod, defaultsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get default-mode.conf from Ingestor Cluster pod")
					testenv.ValidateContent(defaultsConf, defaultsAll, true)

					// Verify AWS env variables
					testcaseEnvInst.Log.Info("Verify AWS env variables")
					envVars, err := testenv.GetAWSEnv(pod, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get AWS env variables from Ingestor Cluster pod")
					testenv.ValidateContent(envVars, awsEnvVars, true)
				}

				if strings.Contains(pod, "ingest") {
					// Verify default-mode.conf
					testcaseEnvInst.Log.Info("Verify default-mode.conf")
					testenv.ValidateContent(defaultsConf, defaultsIngest, true)
				} else if strings.Contains(pod, "idxc") {
					// Verify inputs.conf
					testcaseEnvInst.Log.Info("Verify inputs.conf")
					inputsPath := "opt/splunk/etc/system/local/inputs.conf"
					inputsConf, err := testenv.GetConfFile(pod, inputsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get inputs.conf from Indexer Cluster pod")
					testenv.ValidateContent(inputsConf, inputs, true)
				}
			}
		})
	})

	Context("Ingestor and Indexer deployment", func() {
		It("indingsep, integration, indingsep: Splunk Operator can update Ingestors and Indexers with correct setup", func() {
			// Create Service Account
			testcaseEnvInst.Log.Info("Create Service Account")
			testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err := deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Deploy Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster")

			// Ensure that Ingestor Cluster is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Ingestor Cluster is in Ready phase")
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Cluster Manager is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Cluster Manager is in Ready phase")
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Indexer Cluster is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Indexer Cluster is in Ready phase")
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Get instance of current Ingestor Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Ingestor Cluster CR with latest config")
			ingest := &enterpriseApi.IngestorCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)
			Expect(err).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Update instance of Ingestor Cluster CR with new pushbus config
			testcaseEnvInst.Log.Info("Update instance of Ingestor Cluster CR with new pushbus config")
			ingest.Spec.PushBus = updateBus
			err = deployment.UpdateCR(ctx, ingest)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster with updated CR")

			// Ensure that Ingestor Cluster has not been restarted
			testcaseEnvInst.Log.Info("Ensure that Ingestor Cluster has not been restarted")
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

			// Get instance of current Ingestor Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Ingestor Cluster CR with latest config")
			ingest = &enterpriseApi.IngestorCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)
			Expect(err).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Verify Ingestor Cluster Status
			testcaseEnvInst.Log.Info("Verify Ingestor Cluster Status")
			Expect(ingest.Status.PushBus).To(Equal(updateBus), "Ingestor PushBus status is not the same as provided as input")
			Expect(ingest.Status.PipelineConfig).To(Equal(pipelineConfig), "Ingestor PipelineConfig status is not the same as provided as input")

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Update instance of Indexer Cluster CR with new pullbus config
			testcaseEnvInst.Log.Info("Update instance of Indexer Cluster CR with new pullbus config")
			index.Spec.PullBus = updateBus
			err = deployment.UpdateCR(ctx, index)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster with updated CR")

			// Ensure that Indexer Cluster has not been restarted
			testcaseEnvInst.Log.Info("Ensure that Indexer Cluster has not been restarted")
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index = &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Verify Indexer Cluster Status
			testcaseEnvInst.Log.Info("Verify Indexer Cluster Status")
			Expect(index.Status.PullBus).To(Equal(updateBus), "Indexer PullBus status is not the same as provided as input")
			Expect(index.Status.PipelineConfig).To(Equal(pipelineConfig), "Indexer PipelineConfig status is not the same as provided as input")

			// Verify conf files
			testcaseEnvInst.Log.Info("Verify conf files")
			pods := testenv.DumpGetPods(deployment.GetName())
			for _, pod := range pods {
				defaultsConf := ""

				if strings.Contains(pod, "ingest") || strings.Contains(pod, "idxc") {
					// Verify outputs.conf
					testcaseEnvInst.Log.Info("Verify outputs.conf")
					outputsPath := "opt/splunk/etc/system/local/outputs.conf"
					outputsConf, err := testenv.GetConfFile(pod, outputsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get outputs.conf from Ingestor Cluster pod")
					testenv.ValidateContent(outputsConf, updatedOutputs, true)
					testenv.ValidateContent(outputsConf, outputsShouldNotContain, false)

					// Verify default-mode.conf
					testcaseEnvInst.Log.Info("Verify default-mode.conf")
					defaultsPath := "opt/splunk/etc/system/local/default-mode.conf"
					defaultsConf, err := testenv.GetConfFile(pod, defaultsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get default-mode.conf from Ingestor Cluster pod")
					testenv.ValidateContent(defaultsConf, defaultsAll, true)

					// Verify AWS env variables
					testcaseEnvInst.Log.Info("Verify AWS env variables")
					envVars, err := testenv.GetAWSEnv(pod, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get AWS env variables from Ingestor Cluster pod")
					testenv.ValidateContent(envVars, awsEnvVars, true)
				}

				if strings.Contains(pod, "ingest") {
					// Verify default-mode.conf
					testcaseEnvInst.Log.Info("Verify default-mode.conf")
					testenv.ValidateContent(defaultsConf, defaultsIngest, true)
				} else if strings.Contains(pod, "idxc") {
					// Verify inputs.conf
					testcaseEnvInst.Log.Info("Verify inputs.conf")
					inputsPath := "opt/splunk/etc/system/local/inputs.conf"
					inputsConf, err := testenv.GetConfFile(pod, inputsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get inputs.conf from Indexer Cluster pod")
					testenv.ValidateContent(inputsConf, updatedInputs, true)
					testenv.ValidateContent(inputsConf, inputsShouldNotContain, false)
				}
			}

			// Get instance of current Ingestor Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Ingestor Cluster CR with latest config")
			ingest = &enterpriseApi.IngestorCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)
			Expect(err).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Update instance of Ingestor Cluster CR with new pipelineconfig config
			testcaseEnvInst.Log.Info("Update instance of Ingestor Cluster CR with new pipelineconfig config")
			ingest.Spec.PipelineConfig = updatePipelineConfig
			err = deployment.UpdateCR(ctx, ingest)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster with updated CR")

			// Ensure that Ingestor Cluster has not been restarted
			testcaseEnvInst.Log.Info("Ensure that Ingestor Cluster has not been restarted")
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

			// Get instance of current Ingestor Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Ingestor Cluster CR with latest config")
			ingest = &enterpriseApi.IngestorCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)
			Expect(err).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Verify Ingestor Cluster Status
			testcaseEnvInst.Log.Info("Verify Ingestor Cluster Status")
			Expect(ingest.Status.PushBus).To(Equal(updateBus), "Ingestor PushBus status is not the same as provided as input")
			Expect(ingest.Status.PipelineConfig).To(Equal(updatePipelineConfig), "Ingestor PipelineConfig status is not the same as provided as input")

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index = &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Update instance of Indexer Cluster CR with new pipelineconfig config
			testcaseEnvInst.Log.Info("Update instance of Indexer Cluster CR with new pipelineconfig config")
			index.Spec.PipelineConfig = updatePipelineConfig
			err = deployment.UpdateCR(ctx, index)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster with updated CR")

			// Ensure that Indexer Cluster has not been restarted
			testcaseEnvInst.Log.Info("Ensure that Indexer Cluster has not been restarted")
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index = &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Verify Indexer Cluster Status
			testcaseEnvInst.Log.Info("Verify Indexer Cluster Status")
			Expect(index.Status.PullBus).To(Equal(updateBus), "Indexer PullBus status is not the same as provided as input")
			Expect(index.Status.PipelineConfig).To(Equal(updatePipelineConfig), "Indexer PipelineConfig status is not the same as provided as input")

			// Verify conf files
			testcaseEnvInst.Log.Info("Verify conf files")
			pods = testenv.DumpGetPods(deployment.GetName())
			for _, pod := range pods {
				defaultsConf := ""

				if strings.Contains(pod, "ingest") || strings.Contains(pod, "idxc") {
					// Verify outputs.conf
					testcaseEnvInst.Log.Info("Verify outputs.conf")
					outputsPath := "opt/splunk/etc/system/local/outputs.conf"
					outputsConf, err := testenv.GetConfFile(pod, outputsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get outputs.conf from Ingestor Cluster pod")
					testenv.ValidateContent(outputsConf, updatedOutputs, true)
					testenv.ValidateContent(outputsConf, outputsShouldNotContain, false)

					// Verify default-mode.conf
					testcaseEnvInst.Log.Info("Verify default-mode.conf")
					defaultsPath := "opt/splunk/etc/system/local/default-mode.conf"
					defaultsConf, err := testenv.GetConfFile(pod, defaultsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get default-mode.conf from Ingestor Cluster pod")
					testenv.ValidateContent(defaultsConf, updatedDefaultsAll, true)

					// Verify AWS env variables
					testcaseEnvInst.Log.Info("Verify AWS env variables")
					envVars, err := testenv.GetAWSEnv(pod, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get AWS env variables from Ingestor Cluster pod")
					testenv.ValidateContent(envVars, awsEnvVars, true)
				}

				if strings.Contains(pod, "ingest") {
					// Verify default-mode.conf
					testcaseEnvInst.Log.Info("Verify default-mode.conf")
					testenv.ValidateContent(defaultsConf, updatedDefaultsIngest, true)
				} else if strings.Contains(pod, "idxc") {
					// Verify inputs.conf
					testcaseEnvInst.Log.Info("Verify inputs.conf")
					inputsPath := "opt/splunk/etc/system/local/inputs.conf"
					inputsConf, err := testenv.GetConfFile(pod, inputsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get inputs.conf from Indexer Cluster pod")
					testenv.ValidateContent(inputsConf, updatedInputs, true)
					testenv.ValidateContent(inputsConf, inputsShouldNotContain, false)
				}
			}
		})
	})
})
