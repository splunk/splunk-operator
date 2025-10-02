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

	ctx := context.TODO()

	BeforeEach(func() {
		var err error

		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")

		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
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
		It("indingsep, smoke, indingsep: Splunk Operator can configure Ingestors and Indexers", func() {
			bus := enterpriseApi.PushBusSpec{
				Type: "sqs_smartbus",
				SQS: enterpriseApi.SQSSpec{
					QueueName:                 "test-queue",
					AuthRegion:                "us-west-2",
					Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
					LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
					LargeMessageStorePath:     "s3://test-bucket/smartbus-test",
					DeadLetterQueueName:       "test-dead-letter-queue",
					MaxRetriesPerPart:         4,
					RetryPolicy:               "max_count",
					SendInterval:              "5s",
					EncodingFormat:            "s2s",
				},
			}
			pipelineConfig := enterpriseApi.PipelineConfigSpec{
				RemoteQueueRuleset: false,
				RuleSet:            true,
				RemoteQueueTyping:  false,
				RemoteQueueOutput:  false,
				Typing:             true,
				IndexerPipe:        true,
			}
			serviceAccountName := "index-ingest-sa"

			// Create Service Account
			testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err := deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Deploy Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			cmSpec := enterpriseApi.ClusterManagerSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
				},
			}
			_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster")

			// Ensure that Ingestor Cluster is in Ready phase
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Cluster Manager is in Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Indexer Cluster is in Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)
		})
	})

	Context("Ingestor and Indexer deployment", func() {
		It("indingsep, integration, indingsep: Splunk Operator can configure Ingestors and Indexers", func() {
			bus := enterpriseApi.PushBusSpec{
				Type: "sqs_smartbus",
				SQS: enterpriseApi.SQSSpec{
					QueueName:                 "test-queue",
					AuthRegion:                "us-west-2",
					Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
					LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
					LargeMessageStorePath:     "s3://test-bucket/smartbus-test",
					DeadLetterQueueName:       "test-dead-letter-queue",
					MaxRetriesPerPart:         4,
					RetryPolicy:               "max_count",
					SendInterval:              "5s",
					EncodingFormat:            "s2s",
				},
			}
			pipelineConfig := enterpriseApi.PipelineConfigSpec{
				RemoteQueueRuleset: false,
				RuleSet:            true,
				RemoteQueueTyping:  false,
				RemoteQueueOutput:  false,
				Typing:             true,
				IndexerPipe:        true,
			}
			serviceAccountName := "index-ingest-sa"

			// Create Service Account
			testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err := deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Deploy Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			cmSpec := enterpriseApi.ClusterManagerSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
				},
			}
			_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", bus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster")

			// Ensure that Ingestor Cluster is in Ready phase
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Cluster Manager is in Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Indexer Cluster is in Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Get instance of current Ingestor Cluster CR with latest config
			ingest := &enterpriseApi.IngestorCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)
			Expect(err).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Verify Ingestor Cluster Status
			Expect(ingest.Status.PushBus).To(Equal(bus), "Ingestor PushBus status is not the same as provided as input")
			Expect(ingest.Status.PipelineConfig).To(Equal(pipelineConfig), "Ingestor PipelineConfig status is not the same as provided as input")

			// Get instance of current Indexer Cluster CR with latest config
			index := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Verify Indexer Cluster Status
			Expect(index.Status.PullBus).To(Equal(bus), "Indexer PullBus status is not the same as provided as input")
			Expect(index.Status.PipelineConfig).To(Equal(pipelineConfig), "Indexer PipelineConfig status is not the same as provided as input")

			// Verify conf files
			pods := testenv.DumpGetPods(deployment.GetName())
			for _, pod := range pods {
				defaultsConf := ""

				if strings.Contains(pod, "ingest") || strings.Contains(pod, "idxc") {
					// Get outputs.conf
					outputsPath := "opt/splunk/etc/system/local/outputs.conf"
					outputsConf, err := testenv.GetConfFile(pod, outputsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get outputs.conf from Ingestor Cluster pod")
					Expect(outputsConf).To(ContainSubstring("[remote_queue:test-queue]"), "outputs.conf does not contain smartbus queue name configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.type = sqs_smartbus"), "outputs.conf does not contain smartbus type configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.auth_region = us-west-2"), "outputs.conf does not contain smartbus region configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.dead_letter_queue.name = test-dead-letter-queue"), "outputs.conf does not contain smartbus dead letter queue configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.encoding_format = s2s"), "outputs.conf does not contain smartbus encoding format configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.endpoint = https://sqs.us-west-2.amazonaws.com"), "outputs.conf does not contain smartbus endpoint configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.large_message_store.endpoint = https://s3.us-west-2.amazonaws.com"), "outputs.conf does not contain smartbus large message store endpoint configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.large_message_store.path = s3://test-bucket/smartbus-test"), "outputs.conf does not contain smartbus large message store path configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.retry_policy = max_count"), "outputs.conf does not contain smartbus retry policy configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.send_interval = 5s"), "outputs.conf does not contain smartbus send interval configuration")
					Expect(outputsConf).To(ContainSubstring("remote_queue.max_count.sqs_smartbus.max_retries_per_part = 4"), "outputs.conf does not contain smartbus max retries per part configuration")

					// Get default-mode.conf
					defaultsPath := "opt/splunk/etc/system/local/default-mode.conf"
					defaultsConf, err := testenv.GetConfFile(pod, defaultsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get default-mode.conf from Ingestor Cluster pod")
					Expect(defaultsConf).To(ContainSubstring("[pipeline:remotequeueruleset]\ndisabled = false"), "default-mode.conf does not contain remote queue ruleset stanza")
					Expect(defaultsConf).To(ContainSubstring("[pipeline:ruleset]\ndisabled = true"), "default-mode.conf does not contain ruleset stanza")
					Expect(defaultsConf).To(ContainSubstring("[pipeline:remotequeuetyping]\ndisabled = false"), "default-mode.conf does not contain remote queue typing stanza")
					Expect(defaultsConf).To(ContainSubstring("[pipeline:remotequeueoutput]\ndisabled = false"), "default-mode.conf does not contain remote queue output stanza")
					Expect(defaultsConf).To(ContainSubstring("[pipeline:typing]\ndisabled = true"), "default-mode.conf does not contain typing stanza")

					// Get AWS env variables
					envVars, err := testenv.GetAWSEnv(pod, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get AWS env variables from Ingestor Cluster pod")
					Expect(envVars).To(ContainSubstring("AWS_REGION=us-west-2"), "AWS env variables do not contain region")
					Expect(envVars).To(ContainSubstring("AWS_DEFAULT_REGION=us-west-2"), "AWS env variables do not contain default region")
					Expect(envVars).To(ContainSubstring("AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token"), "AWS env variables do not contain web identity token file")
					Expect(envVars).To(ContainSubstring("AWS_ROLE_ARN=arn:aws:iam::"), "AWS env variables do not contain role arn")
					Expect(envVars).To(ContainSubstring("AWS_STS_REGIONAL_ENDPOINTS=regional"), "AWS env variables do not contain STS regional endpoints")
				}

				if strings.Contains(pod, "ingest") {
					Expect(defaultsConf).To(ContainSubstring("[pipeline:indexerPipe]\ndisabled = true"), "default-mode.conf does not contain indexer pipe stanza")
				} else if strings.Contains(pod, "idxc") {
					// Get inputs.conf
					inputsPath := "opt/splunk/etc/system/local/inputs.conf"
					inputsConf, err := testenv.GetConfFile(pod, inputsPath, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get inputs.conf from Indexer Cluster pod")
					Expect(inputsConf).To(ContainSubstring("[remote_queue:test-queue]"), "inputs.conf does not contain smartbus queue name configuration")
					Expect(inputsConf).To(ContainSubstring("remote_queue.type = sqs_smartbus"), "inputs.conf does not contain smartbus type configuration")
					Expect(inputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.auth_region = us-west-2"), "inputs.conf does not contain smartbus region configuration")
					Expect(inputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.dead_letter_queue.name = test-dead-letter-queue"), "inputs.conf does not contain smartbus dead letter queue configuration")
					Expect(inputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.endpoint = https://sqs.us-west-2.amazonaws.com"), "inputs.conf does not contain smartbus endpoint configuration")
					Expect(inputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.large_message_store.endpoint = https://s3.us-west-2.amazonaws.com"), "inputs.conf does not contain smartbus large message store endpoint configuration")
					Expect(inputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.large_message_store.path = s3://test-bucket/smartbus-test"), "inputs.conf does not contain smartbus large message store path configuration")
					Expect(inputsConf).To(ContainSubstring("remote_queue.sqs_smartbus.retry_policy = max_count"), "inputs.conf does not contain smartbus retry policy configuration")
					Expect(inputsConf).To(ContainSubstring("remote_queue.max_count.sqs_smartbus.max_retries_per_part = 4"), "inputs.conf does not contain smartbus max retries per part configuration")
				}
			}
		})
	})
})
