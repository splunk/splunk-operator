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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/onsi/ginkgo/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"

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

			// TODO: Remove secret reference once IRSA fixed for Splunk and EKS 1.34+
			// Secret reference
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateBusVolumeSpec("bus-secret-ref-volume", testcaseEnvInst.GetIndexSecretName())}
			bus.SQS.VolList = volumeSpec

			// Deploy Bus
			testcaseEnvInst.Log.Info("Deploy Bus")
			b, err := deployment.DeployBus(ctx, "bus", bus)
			Expect(err).To(Succeed(), "Unable to deploy Bus")

			// Deploy LargeMessageStore
			testcaseEnvInst.Log.Info("Deploy LargeMessageStore")
			lm, err := deployment.DeployLargeMessageStore(ctx, "lms", lms)
			Expect(err).To(Succeed(), "Unable to deploy LargeMessageStore")

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err = deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, v1.ObjectReference{Name: b.Name}, v1.ObjectReference{Name: lm.Name}, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Deploy Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", v1.ObjectReference{Name: b.Name}, v1.ObjectReference{Name: lm.Name}, serviceAccountName)
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

			// Delete the Bus
			bus := &enterpriseApi.Bus{}
			err = deployment.GetInstance(ctx, "bus", bus)
			Expect(err).To(Succeed(), "Unable to get Bus instance", "Bus Name", bus)
			err = deployment.DeleteCR(ctx, bus)
			Expect(err).To(Succeed(), "Unable to delete Bus", "Bus Name", bus)

			// Delete the LargeMessageStore
			lm = &enterpriseApi.LargeMessageStore{}
			err = deployment.GetInstance(ctx, "lms", lm)
			Expect(err).To(Succeed(), "Unable to get LargeMessageStore instance", "LargeMessageStore Name", lm)
			err = deployment.DeleteCR(ctx, lm)
			Expect(err).To(Succeed(), "Unable to delete LargeMessageStore", "LargeMessageStore Name", lm)
		})
	})

	Context("Ingestor and Indexer deployment", func() {
		It("indingsep, smoke, indingsep: Splunk Operator can deploy Ingestors and Indexers with additional configurations", func() {
			// Create Service Account
			testcaseEnvInst.Log.Info("Create Service Account")
			testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// TODO: Remove secret reference once IRSA fixed for Splunk and EKS 1.34+
			// Secret reference
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateBusVolumeSpec("bus-secret-ref-volume", testcaseEnvInst.GetIndexSecretName())}
			bus.SQS.VolList = volumeSpec

			// Deploy Bus
			testcaseEnvInst.Log.Info("Deploy Bus")
			bc, err := deployment.DeployBus(ctx, "bus", bus)
			Expect(err).To(Succeed(), "Unable to deploy Bus")

			// Deploy LargeMessageStore
			testcaseEnvInst.Log.Info("Deploy LargeMessageStore")
			lm, err := deployment.DeployLargeMessageStore(ctx, "lms", lms)
			Expect(err).To(Succeed(), "Unable to deploy LargeMessageStore")

			// Upload apps to S3
			testcaseEnvInst.Log.Info("Upload apps to S3")
			appFileList := testenv.GetAppFileList(appListV1)
			_, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload V1 apps to S3 test directory for IngestorCluster")

			// Deploy Ingestor Cluster with additional configurations (similar to standalone app framework test)
			appSourceName := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(5)
			ic := &enterpriseApi.IngestorCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deployment.GetName() + "-ingest",
					Namespace: testcaseEnvInst.GetName(),
				},
				Spec: enterpriseApi.IngestorClusterSpec{
					CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
						ServiceAccount:               serviceAccountName,
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
					BusRef:               v1.ObjectReference{Name: bc.Name},
					LargeMessageStoreRef: v1.ObjectReference{Name: lm.Name},
					Replicas:             3,
					AppFrameworkConfig:   appFrameworkSpec,
				},
			}

			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster with additional configurations")
			_, err = deployment.DeployIngestorClusterWithAdditionalConfiguration(ctx, ic)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Ensure that Ingestor Cluster is in Ready phase
			testcaseEnvInst.Log.Info("Ensure that Ingestor Cluster is in Ready phase")
			testenv.IngestorReady(ctx, deployment, testcaseEnvInst)

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
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify probe configuration
			testcaseEnvInst.Log.Info("Get config map for probes")
			ConfigMapName := enterprise.GetProbeConfigMapName(testcaseEnvInst.GetName())
			_, err = testenv.GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
			Expect(err).To(Succeed(), "Unable to get config map for probes", "ConfigMap", ConfigMapName)
			testcaseEnvInst.Log.Info("Verify probe configurations on Ingestor pods")
			scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName(), enterprise.GetStartupScriptName()}
			allPods := testenv.DumpGetPods(testcaseEnvInst.GetName())
			testenv.VerifyFilesInDirectoryOnPod(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)
		})
	})

	Context("Ingestor and Indexer deployment", func() {
		It("indingsep, integration, indingsep: Splunk Operator can deploy Ingestors and Indexers with correct setup", func() {
			// Create Service Account
			testcaseEnvInst.Log.Info("Create Service Account")
			testcaseEnvInst.CreateServiceAccount(serviceAccountName)

			// TODO: Remove secret reference once IRSA fixed for Splunk and EKS 1.34+
			// Secret reference
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateBusVolumeSpec("bus-secret-ref-volume", testcaseEnvInst.GetIndexSecretName())}
			bus.SQS.VolList = volumeSpec

			// Deploy Bus
			testcaseEnvInst.Log.Info("Deploy Bus")
			bc, err := deployment.DeployBus(ctx, "bus", bus)
			Expect(err).To(Succeed(), "Unable to deploy Bus")

			// Deploy LargeMessageStore
			testcaseEnvInst.Log.Info("Deploy LargeMessageStore")
			lm, err := deployment.DeployLargeMessageStore(ctx, "lms", lms)
			Expect(err).To(Succeed(), "Unable to deploy LargeMessageStore")

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err = deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, v1.ObjectReference{Name: bc.Name}, v1.ObjectReference{Name: lm.Name}, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Deploy Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", v1.ObjectReference{Name: bc.Name}, v1.ObjectReference{Name: lm.Name}, serviceAccountName)
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
			Expect(ingest.Status.Bus).To(Equal(bus), "Ingestor bus status is not the same as provided as input")

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Verify Indexer Cluster Status
			testcaseEnvInst.Log.Info("Verify Indexer Cluster Status")
			Expect(index.Status.Bus).To(Equal(bus), "Indexer bus status is not the same as provided as input")

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

			// TODO: Remove secret reference once IRSA fixed for Splunk and EKS 1.34+
			// Secret reference
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateBusVolumeSpec("bus-secret-ref-volume", testcaseEnvInst.GetIndexSecretName())}
			bus.SQS.VolList = volumeSpec
			updateBus.SQS.VolList = volumeSpec

			// Deploy Bus
			testcaseEnvInst.Log.Info("Deploy Bus")
			bc, err := deployment.DeployBus(ctx, "bus", bus)
			Expect(err).To(Succeed(), "Unable to deploy Bus")

			// Deploy LargeMessageStore
			testcaseEnvInst.Log.Info("Deploy LargeMessageStore")
			lm, err := deployment.DeployLargeMessageStore(ctx, "lms", lms)
			Expect(err).To(Succeed(), "Unable to deploy LargeMessageStore")

			// Deploy Ingestor Cluster
			testcaseEnvInst.Log.Info("Deploy Ingestor Cluster")
			_, err = deployment.DeployIngestorCluster(ctx, deployment.GetName()+"-ingest", 3, v1.ObjectReference{Name: bc.Name}, v1.ObjectReference{Name: lm.Name}, serviceAccountName)
			Expect(err).To(Succeed(), "Unable to deploy Ingestor Cluster")

			// Deploy Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			_, err = deployment.DeployClusterManagerWithGivenSpec(ctx, deployment.GetName(), cmSpec)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Indexer Cluster")
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", "", 3, deployment.GetName(), "", v1.ObjectReference{Name: bc.Name}, v1.ObjectReference{Name: lm.Name}, serviceAccountName)
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

			// Get instance of current Bus CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Bus CR with latest config")
			bus := &enterpriseApi.Bus{}
			err = deployment.GetInstance(ctx, bc.Name, bus)
			Expect(err).To(Succeed(), "Failed to get instance of Bus")

			// Update instance of Bus CR with new bus
			testcaseEnvInst.Log.Info("Update instance of Bus CR with new bus")
			bus.Spec = updateBus
			err = deployment.UpdateCR(ctx, bus)
			Expect(err).To(Succeed(), "Unable to deploy Bus with updated CR")

			// Get instance of current Ingestor Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Ingestor Cluster CR with latest config")
			ingest := &enterpriseApi.IngestorCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-ingest", ingest)
			Expect(err).To(Succeed(), "Failed to get instance of Ingestor Cluster")

			// Verify Ingestor Cluster Status
			testcaseEnvInst.Log.Info("Verify Ingestor Cluster Status")
			Expect(ingest.Status.Bus).To(Equal(updateBus), "Ingestor bus status is not the same as provided as input")

			// Get instance of current Indexer Cluster CR with latest config
			testcaseEnvInst.Log.Info("Get instance of current Indexer Cluster CR with latest config")
			index := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", index)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")

			// Verify Indexer Cluster Status
			testcaseEnvInst.Log.Info("Verify Indexer Cluster Status")
			Expect(index.Status.Bus).To(Equal(updateBus), "Indexer bus status is not the same as provided as input")

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
