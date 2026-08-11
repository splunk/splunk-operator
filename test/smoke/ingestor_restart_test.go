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
package smoke

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// Env vars consumed by these tests — same as the index_and_ingestion_separation suite.
var (
	ingestorSmokeQueueName = testenv.GetEnvWithDefault("TEST_SQS_QUEUE", "index-ingest-separation-test-q")
	ingestorSmokeDLQName   = testenv.GetEnvWithDefault("TEST_SQS_DLQ", "index-ingest-separation-test-dlq")
	ingestorSmokeS3Path    = testenv.GetEnvWithDefault("TEST_S3_BUCKET_PATH", "index-ingest-separation-test-bucket/smartbus-test")
	ingestorSmokeAWSRegion = testenv.GetEnvWithDefault("TEST_AWS_REGION", "us-west-2")
	ingestorSmokeSQSEndpt  = testenv.GetEnvWithDefault("TEST_SQS_ENDPOINT", "")
	ingestorSmokeS3Endpt   = testenv.GetEnvWithDefault("TEST_S3_ENDPOINT", "")
)

func init() {
	if ingestorSmokeSQSEndpt == "" {
		ingestorSmokeSQSEndpt = fmt.Sprintf("https://sqs.%s.amazonaws.com", ingestorSmokeAWSRegion)
	}
	if ingestorSmokeS3Endpt == "" {
		ingestorSmokeS3Endpt = fmt.Sprintf("https://s3.%s.amazonaws.com", ingestorSmokeAWSRegion)
	}
}

var _ = Describe("Ingestor rolling restart driven by reconcile loop", Label("tier:e2e-pr", "tier:e2e-full", "cloud:aws", "feature:ingestor-restart"), func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	// IngestorCluster rolling restart driven by the reconcile loop.
	//
	// Flow:
	//  1. Deploy Queue + ObjectStorage + IngestorCluster (3 replicas) and wait for Ready.
	//     No ClusterManager or IndexerCluster — IngestorCluster has no dependency on them.
	//  2. Trigger restart_required on every ingestor pod via the Splunk REST API (exec into pod,
	//     no port-forwarding needed — curl runs against localhost:8089 inside the pod).
	//  3. Wait for the operator to complete the rolling eviction (Restarting=False + Ready).
	//     The reconcile loop polls restart_required directly on each pod and evicts pod-by-pod.
	//  4. Verify no restart_required remains on any pod.
	Context("IngestorCluster with 3 replicas", func() {
		It("rolling restart via reconcile loop evicts pods one-by-one and returns to Ready",
			Label("sva:ingestor"), NodeTimeout(2*time.Hour),
			func(ctx SpecContext) {
				const replicas = 3
				icName := deployment.GetName() + "-ingest"

				secretName := testcaseEnvInst.GetIndexIngestSepSecretName()
				qSpec := enterpriseApi.QueueSpec{
					Provider: "sqs",
					SQS: enterpriseApi.SQSSpec{
						Name:       ingestorSmokeQueueName,
						AuthRegion: ingestorSmokeAWSRegion,
						Endpoint:   ingestorSmokeSQSEndpt,
						DLQ:        ingestorSmokeDLQName,
						SecretKeyRef: &enterpriseApi.SQSSecretKeyRef{
							AwsAccessKey: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{Name: secretName}, Key: "s3_access_key"},
							AwsSecretKey: v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{Name: secretName}, Key: "s3_secret_key"},
						},
					},
				}
				osSpec := enterpriseApi.ObjectStorageSpec{
					Provider: "s3",
					S3: enterpriseApi.S3Spec{
						Endpoint: ingestorSmokeS3Endpt,
						Path:     ingestorSmokeS3Path,
					},
				}

				// Step 1: deploy Queue + ObjectStorage + IngestorCluster and wait for Ready.
				// No ClusterManager or IndexerCluster needed — this test only exercises the
				// ingestor rolling-restart reconcile path, which is independent of indexing.
				q, objStorage, err := testenv.DeployQueueAndObjectStorage(ctx, deployment, qSpec, osSpec)
				Expect(err).To(Succeed(), "Failed to deploy Queue and ObjectStorage")

				_, err = deployment.DeployIngestorCluster(ctx, icName, replicas,
					v1.ObjectReference{Name: q.Name},
					v1.ObjectReference{Name: objStorage.Name}, "")
				Expect(err).To(Succeed(), "Failed to deploy IngestorCluster")

				Expect(testcaseEnvInst.VerifyIngestorReady(ctx, deployment)).
					To(Succeed(), "IngestorCluster did not reach Ready")

				// Step 2: trigger restart_required on all pods via exec+curl inside each pod.
				triggerTime := time.Now()
				Expect(testenv.TriggerIngestorRestartRequired(ctx, deployment, icName, replicas)).
					To(Succeed(), "Failed to trigger restart_required on ingestor pods")

				// Step 3: the reconcile loop polls restart_required on each pod and drives
				// rolling eviction pod-by-pod, gated by PDB.
				Expect(testenv.WaitForIngestorRollingRestartComplete(ctx, testcaseEnvInst, icName, triggerTime)).
					To(Succeed(), "Rolling restart did not complete within timeout")

				// Step 4: Splunk must report no restart_required on any pod.
				Expect(testenv.VerifyIngestorRestartCleared(ctx, deployment, replicas, icName)).
					To(Succeed(), "Splunk still reports restart_required after rolling restart")
			})
	})
})
