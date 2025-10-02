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
package indexingestsep

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("indexingestsep test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	ctx := context.TODO()

	BeforeEach(func() {
		var err error

		name := fmt.Sprintf("%s-%s", "master"+testenvInstance.GetName(), testenv.RandomDNSName(3))
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
		It("indexingestsep, smoke, indexingestseparation: Splunk Operator can configure Ingestors and Indexers", func() {
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
			_, err := deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Create Service Account
			serviceAccountName := "index-ingest-sa"
			testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			pullBus := enterpriseApi.PushBusSpec{
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
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", pullBus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster")

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err = deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, pullBus, pipelineConfig, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Ensure that Cluster Manager is in Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Indexer Cluster is in Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure that Ingestor Cluster is in Ready phase
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)
		})
	})
})
