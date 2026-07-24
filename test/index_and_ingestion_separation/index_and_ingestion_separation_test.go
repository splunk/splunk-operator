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

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"

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
		It("Splunk Operator can deploy Ingestors and Indexers", Label("tier:e2e-pr", "cloud:aws", "feature:indingsep"), NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			// TODO: Remove secret reference and uncomment serviceAccountName part once IRSA fixed for Splunk and EKS 1.34+
			// Create Service Account
			// testcaseEnvInst.Log.Info("Create Service Account")
			// testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			Expect(testcaseEnvInst.SetupIngestorStack(ctx, deployment, queue, objectStorage, cmSpec)).To(Succeed(), "Unable to setup ingestor stack")

			Expect(testenv.DeleteIngestorStack(ctx, deployment)).To(Succeed(), "Unable to delete ingestor stack")
		})

		It("Splunk Operator can disable resource defaults for IngestorCluster", Label("tier:e2e-full", "cloud:aws", "feature:indingsep"), NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			Expect(testcaseEnvInst.SetupIngestorStack(ctx, deployment, queue, objectStorage, cmSpec)).To(Succeed(), "Unable to setup ingestor stack")

			ingestorName := deployment.GetName() + "-ingest"
			ingestorStatefulSet := &appsv1.StatefulSet{}
			Expect(deployment.GetInstance(ctx, fmt.Sprintf("splunk-%s-ingestor", ingestorName), ingestorStatefulSet)).To(Succeed(), "Unable to get IngestorCluster StatefulSet")
			Expect(ingestorStatefulSet.Spec.Template.Spec.Containers).NotTo(BeEmpty(), "IngestorCluster StatefulSet has no containers")
			Expect(ingestorStatefulSet.Spec.Template.Spec.Containers[0].Resources.Requests).To(HaveKeyWithValue(v1.ResourceCPU, resource.MustParse("100m")), "IngestorCluster should receive default CPU requests")
			Expect(ingestorStatefulSet.Spec.Template.Spec.Containers[0].Resources.Limits).To(HaveKeyWithValue(v1.ResourceMemory, resource.MustParse("8Gi")), "IngestorCluster should receive default memory limits")

			ingestor := &enterpriseApi.IngestorCluster{}
			Expect(deployment.GetInstance(ctx, ingestorName, ingestor)).To(Succeed(), "Unable to get IngestorCluster")
			ingestor.Spec.DisableResourceDefaults = true
			Expect(deployment.UpdateCR(ctx, ingestor)).To(Succeed(), "Unable to enable the IngestorCluster resource-default opt-out")

			Eventually(func() error {
				updatedStatefulSet := &appsv1.StatefulSet{}
				if err := deployment.GetInstance(ctx, fmt.Sprintf("splunk-%s-ingestor", ingestorName), updatedStatefulSet); err != nil {
					return err
				}
				if len(updatedStatefulSet.Spec.Template.Spec.Containers) == 0 {
					return fmt.Errorf("IngestorCluster StatefulSet has no containers")
				}
				resources := updatedStatefulSet.Spec.Template.Spec.Containers[0].Resources
				if len(resources.Requests) != 0 || len(resources.Limits) != 0 {
					return fmt.Errorf("IngestorCluster resources were not cleared after opt-out: %v", resources)
				}
				return nil
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "IngestorCluster resources should remain empty after explicitly opting out")
			Expect(testcaseEnvInst.VerifyIngestorReady(ctx, deployment)).To(Succeed(), "IngestorCluster should return to Ready after enabling the resource-default opt-out")

			Expect(testenv.DeleteIngestorStack(ctx, deployment)).To(Succeed(), "Unable to delete ingestor stack")
		})

		It("Splunk Operator can deploy Ingestors and Indexers with additional configurations", Label("tier:e2e-pr", "cloud:aws", "feature:indingsep"), NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
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
			appFrameworkSpec.MaxConcurrentAppDownloads = int64(5)
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

			Expect(deployment.GetInstance(ctx, ic.Name, ic)).To(Succeed(), "Failed to re-fetch IngestorCluster")
			Expect(testenv.VerifyCRConditionsForPhase("IngestorCluster", ic.Name, ic.Status.Conditions, enterpriseApi.PhaseReady)).To(Succeed(), "IngestorCluster conditions not met")
		})

		It("Splunk Operator can update IngestorCluster and IndexerCluster queueRef and objectStorageRef", Label("tier:e2e-full", "cloud:aws", "feature:indingsep"), NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			Expect(testcaseEnvInst.SetupIngestorStack(ctx, deployment, queue, objectStorage, cmSpec)).To(Succeed(), "Unable to setup ingestor stack")

			// Deploy a second Queue and ObjectStorage with different names
			queue2 := queue
			queue2.SQS.Name = queue.SQS.Name + "-v2"
			queue2.SQS.DLQ = queue.SQS.DLQ + "-v2"
			q2, err := deployment.DeployQueue(ctx, "queue-v2", queue2)
			Expect(err).To(Succeed(), "Unable to deploy second Queue")
			os2, err := deployment.DeployObjectStorage(ctx, "os-v2", objectStorage)
			Expect(err).To(Succeed(), "Unable to deploy second ObjectStorage")

			// Update IngestorCluster refs
			ingest := &enterpriseApi.IngestorCluster{}
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)).To(Succeed(), "Failed to get IngestorCluster")
			ingest.Spec.QueueRef = v1.ObjectReference{Name: q2.Name}
			ingest.Spec.ObjectStorageRef = v1.ObjectReference{Name: os2.Name}
			Expect(deployment.UpdateCR(ctx, ingest)).To(Succeed(), "Unable to update IngestorCluster CR with new refs")
			Expect(testcaseEnvInst.VerifyIngestorReady(ctx, deployment)).To(Succeed(), "IngestorCluster not ready after ref update")

			// Update IndexerCluster refs
			idxc := &enterpriseApi.IndexerCluster{}
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-idxc", idxc)).To(Succeed(), "Failed to get IndexerCluster")
			idxc.Spec.QueueRef = &v1.ObjectReference{Name: q2.Name}
			idxc.Spec.ObjectStorageRef = &v1.ObjectReference{Name: os2.Name}
			Expect(deployment.UpdateCR(ctx, idxc)).To(Succeed(), "Unable to update IndexerCluster CR with new refs")
			Expect(testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)).To(Succeed(), "IndexerCluster not ready after ref update")

			// Both IngestorCluster and IndexerCluster now deliver SmartBus config declaratively
			// via content-addressed ConfigMap (structural) + Secret (credentials). A ref change
			// produces new resource names and rolls the pods — verified via conf files below.
			// Neither CR tracks applied refs in status any more; readiness is the signal.
			Expect(deployment.GetInstance(ctx, ingest.Name, ingest)).To(Succeed(), "Failed to re-fetch IngestorCluster")
			Expect(testenv.VerifyCRConditionsForPhase("IngestorCluster", ingest.Name, ingest.Status.Conditions, enterpriseApi.PhaseReady)).To(Succeed(), "IngestorCluster not ready after ref update")

			// Verify conf files reflect the new v2 queue configuration
			expectedV2 := []string{
				fmt.Sprintf("[remote_queue:%s]", queue2.SQS.Name),
				fmt.Sprintf("remote_queue.sqs_smartbus.dead_letter_queue.name = %s", queue2.SQS.DLQ),
			}
			oldQueueStale := []string{
				fmt.Sprintf("[remote_queue:%s]", queue.SQS.Name),
				fmt.Sprintf("remote_queue.sqs_smartbus.dead_letter_queue.name = %s", queue.SQS.DLQ),
			}
			pods := testenv.DumpGetPods(deployment.GetName())
			for _, pod := range pods {
				if strings.Contains(pod, "ingest") || strings.Contains(pod, "idxc") {
					outputsConf, err := testenv.GetConfFile(pod, smartBusConfPath(pod, "outputs.conf"), deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get outputs.conf from pod %s", pod)
					Expect(testenv.ValidateContent(outputsConf, expectedV2, true)).To(Succeed(), "outputs.conf on %s missing v2 queue config", pod)
					Expect(testenv.ValidateContent(outputsConf, oldQueueStale, false)).To(Succeed(), "outputs.conf on %s still contains old queue config", pod)
				}
				if strings.Contains(pod, "idxc") {
					inputsConf, err := testenv.GetConfFile(pod, smartBusConfPath(pod, "inputs.conf"), deployment.GetName())
					Expect(err).To(Succeed(), "Failed to get inputs.conf from pod %s", pod)
					Expect(testenv.ValidateContent(inputsConf, expectedV2, true)).To(Succeed(), "inputs.conf on %s missing v2 queue config", pod)
					Expect(testenv.ValidateContent(inputsConf, oldQueueStale, false)).To(Succeed(), "inputs.conf on %s still contains old queue config", pod)
				}
			}

			Expect(testenv.DeleteIngestorStack(ctx, deployment)).To(Succeed(), "Unable to delete ingestor stack")
		})

		It("Splunk Operator can deploy Ingestors and Indexers with correct setup", Label("tier:e2e-full", "cloud:aws", "feature:indingsep"), NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {
			// TODO: Remove secret reference and uncomment serviceAccountName part once IRSA fixed for Splunk and EKS 1.34+
			// Create Service Account
			// testcaseEnvInst.Log.Info("Create Service Account")
			// testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			Expect(testcaseEnvInst.SetupIngestorStack(ctx, deployment, queue, objectStorage, cmSpec)).To(Succeed(), "Unable to setup ingestor stack")

			// Get instance of current Ingestor Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Ingestor Cluster CR with latest config")
			ingest := &enterpriseApi.IngestorCluster{}
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Verify Ingestor Cluster Status. SmartBus config and credentials are now
			// delivered declaratively via a content-addressed ConfigMap + Secret, so
			// readiness is the signal that the mounted config was applied.
			testcaseEnvInst.Log.Info("Verify Ingestor Cluster Status")
			Expect(testenv.VerifyCRConditionsForPhase("IngestorCluster", ingest.Name, ingest.Status.Conditions, enterpriseApi.PhaseReady)).To(Succeed(), "IngestorCluster conditions not met at initial setup")

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index := &enterpriseApi.IndexerCluster{}
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Verify Indexer Cluster Status. The IndexerCluster no longer tracks a credential
			// secret version in status: SmartBus config and credentials are delivered
			// declaratively via a content-addressed ConfigMap + Secret, so readiness is the
			// signal that the mounted config was applied.
			testcaseEnvInst.Log.Info("Verify Indexer Cluster Status")
			Expect(testenv.VerifyCRConditionsForPhase("IndexerCluster", index.Name, index.Status.Conditions, enterpriseApi.PhaseReady)).To(Succeed(), "IndexerCluster conditions not met")

			// Verify conf files
			testcaseEnvInst.Log.Info("Verify conf files")
			pods := testenv.DumpGetPods(deployment.GetName())
			for _, pod := range pods {
				defaultsConf := ""

				if strings.Contains(pod, "ingest") || strings.Contains(pod, "idxc") {
					// Verify outputs.conf (indexer: declarative 100-sok/local; ingestor: imperative system/local)
					Expect(testenv.VerifyConfFileContent(pod, smartBusConfPath(pod, "outputs.conf"), deployment.GetName(), outputs, "Failed to get outputs.conf from pod")).To(Succeed(), "outputs.conf verification failed")

					// Verify default-mode.conf (always system/local — SOK leaves the pipeline conf in the default directory)
					Expect(testenv.VerifyConfFileContent(pod, "opt/splunk/etc/system/local/default-mode.conf", deployment.GetName(), defaultsAll, "Failed to get default-mode.conf from pod")).To(Succeed(), "default-mode.conf verification failed")

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
					// Verify inputs.conf (indexer: declarative 100-sok/local)
					Expect(testenv.VerifyConfFileContent(pod, smartBusConfPath(pod, "inputs.conf"), deployment.GetName(), inputs, "Failed to get inputs.conf from Indexer Cluster pod")).To(Succeed(), "inputs.conf verification failed")
				}
			}

			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)).To(Succeed(), "Failed to re-fetch IngestorCluster")
			Expect(testenv.VerifyCRConditionsForPhase("IngestorCluster", ingest.Name, ingest.Status.Conditions, enterpriseApi.PhaseReady)).To(Succeed(), "IngestorCluster conditions not met")
		})
	})
})

// smartBusConfPath returns the on-pod path of a SmartBus conf file for the given pod.
//
// Both IngestorCluster and IndexerCluster deliver their structural SmartBus config declaratively
// through a content-addressed ConfigMap mounted via SPLUNK_DEFAULTS_URL, which splunk-ansible
// renders into the dedicated app directory 100-sok/local. Credentials (access_key/secret_key)
// live separately in 101-sok-creds/local and are not asserted here.
func smartBusConfPath(_, confFile string) string {
	return "opt/splunk/etc/apps/100-sok/local/" + confFile
}
