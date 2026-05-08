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
package indexingestionsep

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Index and Ingestion Separation test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var cmSpec enterpriseApi.ClusterManagerSpec

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).To(Succeed(), "Failed to setup test case environment")

		// Validate test prerequisites early to fail fast
		err = testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")

		cmSpec = enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: "Always",
					Image:           testcaseEnvInst.GetSplunkImage(),
				},
			},
		}
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownTestCaseEnv(ctx, testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
	})

	Context("Ingestor and Indexer deployment", func() {
		It("indexingestionsep, smoke: Splunk Operator can deploy Ingestors and Indexers", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			// TODO: Remove secret reference and uncomment serviceAccountName part once IRSA fixed for Splunk and EKS 1.34+
			// Create Service Account
			// testcaseEnvInst.Log.Info("Create Service Account")
			// testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			Expect(testcaseEnvInst.SetupIngestorStack(ctx, deployment, queue, objectStorage, cmSpec)).To(Succeed(), "Unable to setup ingestor stack")

			Expect(testenv.DeleteIngestorStack(ctx, deployment)).To(Succeed(), "Unable to delete ingestor stack")
		})

		It("indexingestionsep, smoke: Splunk Operator can deploy Ingestors and Indexers with additional configurations", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			// TODO: Remove secret reference and uncomment serviceAccountName part once IRSA fixed for Splunk and EKS 1.34+
			// Create Service Account
			// testcaseEnvInst.Log.Info("Create Service Account")
			// testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// Secret reference
			volumeSpec := []enterpriseApi.SQSVolumeSpec{testenv.GenerateQueueVolumeSpec(
				"queue-secret-ref-volume",
				testcaseEnvInst.GetIndexIngestSepSecretName(),
			)}
			queue.SQS.VolList = volumeSpec

			// Deploy Queue and ObjectStorage
			q, objStorage, err := testenv.DeployQueueAndObjectStorage(ctx, deployment, queue, objectStorage)
			Expect(err).To(Succeed(), "Unable to deploy Queue and ObjectStorage")

			// Deploy Ingestor Cluster with additional configurations (similar to standalone app framework test)
			appSourceName := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(5)
			ic := &enterpriseApi.IngestorCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:       deployment.GetName() + "-ingest",
					Namespace:  testcaseEnvInst.GetName(),
					Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
				},
				Spec: enterpriseApi.IngestorClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						// ServiceAccount:               serviceAccountName,
						LivenessInitialDelaySeconds:  600,
						ReadinessInitialDelaySeconds: 50,
						StartupProbe: &enterpriseApi.Probe{
							InitialDelaySeconds: 40,
							TimeoutSeconds:      30,
							PeriodSeconds:       30,
							FailureThreshold:    12,
						},
						LivenessProbe: &enterpriseApi.Probe{
							InitialDelaySeconds: 400,
							TimeoutSeconds:      30,
							PeriodSeconds:       30,
							FailureThreshold:    12,
						},
						ReadinessProbe: &enterpriseApi.Probe{
							InitialDelaySeconds: 20,
							TimeoutSeconds:      30,
							PeriodSeconds:       30,
							FailureThreshold:    12,
						},
						Spec: enterpriseApi.Spec{
							ImagePullPolicy: "Always",
							Image:           testcaseEnvInst.GetSplunkImage(),
						},
					},
					QueueRef:           v1.ObjectReference{Name: q.Name},
					ObjectStorageRef:   v1.ObjectReference{Name: objStorage.Name},
					Replicas:           3,
					AppFrameworkConfig: appFrameworkSpec,
				},
			}

			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster with additional configurations")
			_, err = deployment.DeployIngestorClusterWithAdditionalConfiguration(ctx, ic)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Ensure that Ingestor Cluster is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Ingestor Cluster is in Ready phase")
			Expect(testcaseEnvInst.VerifyIngestorReady(ctx, deployment)).To(Succeed(), "Ingestor Cluster not ready")

			// Upload apps to S3
			testcaseEnvInst.Log.Info("Upload apps to S3")
			appFileList := testenv.GetAppFileList(appListV1)
			_, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload V1 apps to S3 test directory for IngestorCluster")

			// Verify Ingestor Cluster Pods have apps installed
			testcaseEnvInst.Log.Info("Verify Ingestor Cluster Pods have apps installed")
			ingestorPod := []string{fmt.Sprintf(testenv.IngestorPod, deployment.GetName()+"-ingest", 0)}
			ingestorAppSourceInfo := testenv.AppSourceInfo{
				CrKind:          ic.Kind,
				CrName:          ic.Name,
				CrAppSourceName: appSourceName,
				CrPod:           ingestorPod,
				CrAppVersion:    "V1",
				CrAppScope:      enterpriseApi.ScopeLocal,
				CrAppList:       testenv.BasicApps,
				CrAppFileList:   testenv.GetAppFileList(testenv.BasicApps),
				CrReplicas:      3,
			}
			allAppSourceInfo := []testenv.AppSourceInfo{ingestorAppSourceInfo}
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify probe configuration
			Expect(testcaseEnvInst.VerifyProbeConfigAndScripts(ctx, deployment, true)).To(Succeed(), "Probe config verification failed")
		})

		It("indexingestionsep, integration: Splunk Operator can deploy Ingestors and Indexers with correct setup", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			// TODO: Remove secret reference and uncomment serviceAccountName part once IRSA fixed for Splunk and EKS 1.34+
			// Create Service Account
			// testcaseEnvInst.Log.Info("Create Service Account")
			// testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			Expect(testcaseEnvInst.SetupIngestorStack(ctx, deployment, queue, objectStorage, cmSpec)).To(Succeed(), "Unable to setup ingestor stack")

			// Get instance of current Ingestor Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Ingestor Cluster CR with latest config")
			ingest := &enterpriseApi.IngestorCluster{}
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Verify Ingestor Cluster Status
			testcaseEnvInst.Log.Info("Verify Ingestor Cluster Status")
			Expect(testenv.VerifyCredentialSecretVersion(ingest.Status.CredentialSecretVersion, "Ingestor")).To(Succeed(), "Ingestor credential secret version invalid")

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index := &enterpriseApi.IndexerCluster{}
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Verify Indexer Cluster Status
			testcaseEnvInst.Log.Info("Verify Indexer Cluster Status")
			Expect(testenv.VerifyCredentialSecretVersion(index.Status.CredentialSecretVersion, "Indexer")).To(Succeed(), "Indexer credential secret version invalid")

			// Verify conf files
			testcaseEnvInst.Log.Info("Verify conf files")
			pods := testenv.DumpGetPods(deployment.GetName())
			for _, pod := range pods {
				defaultsConf := ""

				if strings.Contains(pod, "ingest") || strings.Contains(pod, "idxc") {
					// Verify outputs.conf
					Expect(testenv.VerifyConfFileContent(pod, "opt/splunk/etc/system/local/outputs.conf", deployment.GetName(), outputs, "Failed to get outputs.conf from Ingestor Cluster pod")).To(Succeed(), "outputs.conf verification failed")

					// Verify default-mode.conf
					Expect(testenv.VerifyConfFileContent(pod, "opt/splunk/etc/system/local/default-mode.conf", deployment.GetName(), defaultsAll, "Failed to get default-mode.conf from Ingestor Cluster pod")).To(Succeed(), "default-mode.conf verification failed")

					// Verify AWS env variables
					testcaseEnvInst.Log.Info("Verify AWS env variables")
					envVars, err := testenv.GetAWSEnv(pod, deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get AWS env variables from Ingestor Cluster pod")
					Expect(testenv.ValidateContent(envVars, awsEnvVars, true)).To(Succeed(), "AWS env variable validation failed")
				}

				if strings.Contains(pod, "ingest") {
					// Verify default-mode.conf
					testcaseEnvInst.Log.Info("Verify default-mode.conf")
					Expect(testenv.ValidateContent(defaultsConf, defaultsIngest, true)).To(Succeed(), "default-mode.conf validation failed")
				} else if strings.Contains(pod, "idxc") {
					// Verify inputs.conf
					Expect(testenv.VerifyConfFileContent(pod, "opt/splunk/etc/system/local/inputs.conf", deployment.GetName(), inputs, "Failed to get inputs.conf from Indexer Cluster pod")).To(Succeed(), "inputs.conf verification failed")
				}
			}
		})
	})
})
