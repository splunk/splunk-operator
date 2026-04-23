// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
// limitations under the License.s
package azures1appfw

import (
	"fmt"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	testenv "github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("s1appfw test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var azTestDir string
	var uploadedApps []string
	var appSourceName string
	var appSourceVolumeName string
	var filePresentOnOperator bool

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).ToNot(HaveOccurred())

		azTestDir = "s1appfw-" + testenv.RandomDNSName(4)
		appSourceVolumeName = "appframework-test-volume-" + testenv.RandomDNSName(3)
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownAppFrameworkTestCaseEnv(ctx, testcaseEnvInst, deployment, testenv.AzureCloudCleanup(ctx, uploadedApps), filePresentOnOperator)).To(Succeed())
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworkazures1, appframeworkazure, azure_sanity: can deploy a Standalone instance with App Framework enabled, install apps then upgrade them", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Standalone
			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############ V1 APP VERIFICATION FOR STANDALONE ###########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify no pod resets triggered due to app install
			   * Verify App enabled  and version by running splunk cmd
			   ############ UPGRADE V2 APPS ###########
			   * Upload V2 apps to Azure App Source
			   ############ V2 APP VERIFICATION FOR STANDALONE  ###########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify no pod resets triggered due to app install
			   * Verify App enabled  and version by running splunk cmd
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Upload V1 apps to Azure for Standalone
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			azTestDir = "s1appfw-" + testenv.RandomDNSName(4)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// ############ INITIAL VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// ############## UPGRADE APPS #################

			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			Expect(azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)).To(Succeed(), "Azure file deletion failed")

			uploadedApps = nil
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############ UPGRADE VERIFICATION ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV2
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1azure, appframeworkazures1, appframework: can deploy a Standalone instance with App Framework enabled, install apps then downgrade them", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			################## SETUP ####################
			* Upload V2 apps to Azure for Standalone			* Upload V2 apps to Azure for Standalone
			* Create app source for Standalone
			* Prepare and deploy Standalone with app framework and wait for the pod to be ready
			############ INITIAL VERIFICATION FOR STANDALONE ###########
			* Verify Apps Downloaded in App Deployment Info
			* Verify Apps Copied in App Deployment Info
			* Verify App Package is deleted from Operator Pod
			* Verify Apps Installed in App Deployment Info
			* Verify App Package is deleted from Splunk Pod
			* Verify App Directory in under splunk path
			* Verify no pod resets triggered due to app install
			* Verify App enabled  and version by running splunk cmd
			############# DOWNGRADE APPS ################
			* Upload V1 apps on Azure
			* Wait for Standalone pods to be ready
			########## DOWNGRADE VERIFICATION FOR STANDALONE ###########
			* Verify Apps Downloaded in App Deployment Info
			* Verify Apps Copied in App Deployment Info
			* Verify App Package is deleted from Operator Pod
			* Verify Apps Installed in App Deployment Info
			* Verify App Package is deleted from Splunk Pod
			* Verify App Directory in under splunk path
			* Verify no pod resets triggered due to app install
			* Verify App enabled  and version by running splunk cmd
			*/

			//################## SETUP ####################
			// Upload V2 apps to Azure
			appVersion := "V2"
			appFileList := testenv.GetAppFileList(appListV2)
			azTestDir = "azures1appfw-" + testenv.RandomDNSName(4)

			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// ############# DOWNGRADE APPS ################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			Expect(azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)).To(Succeed(), "Azure file deletion failed")
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion := testcaseEnvInst.GetResourceVersion(ctx, deployment, standalone)

			// Upload V1 apps to Azure for Standalone
			appVersion = "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			appFileList = testenv.GetAppFileList(appListV1)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// wait for custom resource resource version to change
			Expect(testcaseEnvInst.VerifyCustomResourceVersionChanged(ctx, deployment, standalone, resourceVersion)).To(Succeed(), "Custom resource version not changed")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## DOWNGRADE VERIFICATION ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV1
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1azure, integration, appframeworkazures1, appframework, azure_sanity: can deploy a Standalone instance with App Framework enabled, install apps, scale up, install apps on new pod, scale down", NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

			/* Test Steps
			################## SETUP ####################
			* Upload apps on Azure
			* Create 2 app sources for Standalone			* Prepare and deploy Standalone CRD with app framework and wait for the pod to be ready
			########## INITIAL VERIFICATION #############
			* Verify Apps Downloaded in App Deployment Info
			* Verify Apps Copied in App Deployment Info
			* Verify App Package is deleted from Operator Pod
			* Verify Apps Installed in App Deployment Info
			* Verify App Package is deleted from Splunk Pod
			* Verify App Directory in under splunk path
			* Verify no pod resets triggered due to app install
			* Verify App enabled  and version by running splunk cmd
			############### SCALING UP ##################
			* Scale up Standalone
			* Wait for  Standalone to be ready
			########### SCALING UP VERIFICATION #########
			* Verify Apps Downloaded in App Deployment Info
			* Verify Apps Copied in App Deployment Info
			* Verify App Package is deleted from Operator Pod
			* Verify Apps Installed in App Deployment Info
			* Verify App Package is deleted from Splunk Pod
			* Verify App Directory in under splunk path
			* Verify no pod resets triggered due to app install
			* Verify App enabled  and version by running splunk cmd
			############## SCALING DOWN #################
			* Scale down Standalone
			* Wait for Standalone to be ready
			########### SCALING DOWN VERIFICATION #######
			* Verify Apps Downloaded in App Deployment Info
			* Verify Apps Copied in App Deployment Info
			* Verify App Package is deleted from Operator Pod
			* Verify Apps Installed in App Deployment Info
			* Verify App Package is deleted from Splunk Pod
			* Verify App Directory in under splunk path
			* Verify no pod resets triggered due to app install
			* Verify App enabled  and version by running splunk cmd
			*/

			//################## SETUP ####################
			// Upload V1 apps to Azure for Standalone
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			azTestDir := "azures1appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			scaledReplicaCount := 2
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: scaledReplicaCount}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			//############### SCALING UP ##################
			// Scale up Standalone instance
			testcaseEnvInst.Log.Info("Scale up Standalone")

			standalone = &enterpriseApi.Standalone{}
			Expect(deployment.GetInstance(ctx, deployment.GetName(), standalone)).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale up Standalone")

			// Ensure Standalone is scaling up
			Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseScalingUp)).To(Succeed(), "Standalone phase mismatch")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseReady)).To(Succeed(), "Standalone phase mismatch")

			//########### SCALING UP VERIFICATION #########
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			//############## SCALING DOWN #################
			// Scale down Standalone instance
			testcaseEnvInst.Log.Info("Scale down Standalone")
			scaledReplicaCount = 1
			standalone = &enterpriseApi.Standalone{}
			Expect(deployment.GetInstance(ctx, deployment.GetName(), standalone)).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)
			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale down Standalone")

			// Ensure Standalone is scaling down
			Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseScalingDown)).To(Succeed(), "Standalone phase mismatch")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseReady)).To(Succeed(), "Standalone phase mismatch")

			//########### SCALING DOWN VERIFICATION #######
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1azure, integration, appframeworkazures1, appframework: can deploy a Standalone instance with App Framework enabled, install apps, scale up, upgrade apps", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			################## SETUP ####################
			* Upload apps on Azure
			* Create app source for Standalone
			* Prepare and deploy Standalone CRD with app framework and wait for the pod to be ready
			########## INITIAL VERIFICATION #############
			* Verify Apps Downloaded in App Deployment Info
			* Verify Apps Copied in App Deployment Info
			* Verify App Package is deleted from Operator Pod
			* Verify Apps Installed in App Deployment Info
			* Verify App Package is deleted from Splunk Pod
			* Verify App Directory in under splunk path
			* Verify no pod resets triggered due to app install
			* Verify App enabled and version by running splunk cmd
			############### SCALING UP ##################
			* Scale up Standalone
			* Wait for Standalone to be ready
			############### UPGRADE APPS ################
			* Upload V2 apps to Azure App Source
			###### SCALING UP/UPGRADE VERIFICATIONS #####
			* Verify Apps Downloaded in App Deployment Info
			* Verify Apps Copied in App Deployment Info
			* Verify App Package is deleted from Operator Pod
			* Verify Apps Installed in App Deployment Info
			* Verify App Package is deleted from Splunk Pod
			* Verify App Directory in under splunk path
			* Verify no pod resets triggered due to app install
			* Verify App enabled and version by running splunk cmd
			*/

			//################## SETUP ####################
			// Upload V1 apps to Azure for Standalone
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))

			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to Azure for Standalone
			azTestDir := "azures1appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			scaledReplicaCount := 2
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: scaledReplicaCount}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			//############### SCALING UP ##################
			// Scale up Standalone instance
			testcaseEnvInst.Log.Info("Scale up Standalone")

			standalone = &enterpriseApi.Standalone{}
			Expect(deployment.GetInstance(ctx, deployment.GetName(), standalone)).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale up Standalone")

			// Ensure Standalone is scaling up
			Expect(testcaseEnvInst.VerifyStandalonePhase(ctx, deployment, enterpriseApi.PhaseScalingUp)).To(Succeed(), "Standalone phase mismatch")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// ############## UPGRADE APPS #################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			Expect(azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)).To(Succeed(), "Azure file deletion failed")
			uploadedApps = nil

			// Upload V2 apps to Azure for Standalone
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############ SCALING UP/UPGRADE VERIFICATIONS ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV2
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			standaloneAppSourceInfo.CrPod = []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0), fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 1)}
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")
		})
	})

	// ES App Installation not supported at the time. Will be added back at a later time.
	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1azure, integration, appframeworkazures1, appframework: can deploy a Standalone and have ES app installed", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			################## SETUP ####################
			* Upload ES app to Azure
			* Create App Source for Standalone
			* Prepare and deploy Standalone and wait for the pod to be ready
			################## VERIFICATION #############
			* Verify ES app is installed on Standalone
			*/

			//################## SETUP ####################

			// Download ES App from Azure
			testcaseEnvInst.Log.Info("Download ES app from Azure")
			esApp := []string{"SplunkEnterpriseSecuritySuite"}
			appFileList := testenv.GetAppFileList(esApp)
			containerName := "/" + AzureDataContainer + "/" + testenv.AppLocationV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)

			Expect(err).To(Succeed(), "Unable to download ES app")

			// Upload ES app to Azure
			testcaseEnvInst.Log.Info("Upload ES app on Azure")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload ES app to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopePremiumApps, appSourceName, azTestDir, 60)
			appFrameworkSpec.AppSources[0].PremiumAppsProps = enterpriseApi.PremiumAppsProps{
				Type: enterpriseApi.PremiumAppsTypeEs,
				EsDefaults: enterpriseApi.EsDefaults{
					SslEnablement: enterpriseApi.SslEnablementIgnore,
				},
			}
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone with App Framework")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// ############ INITIAL VERIFICATION ###########
			appVersion := "V1"
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: esApp, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// ############## UPGRADE APPS #################

			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			Expect(azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)).To(Succeed(), "Azure file deletion failed")

			// Download ES App from Azure
			containerName = "/" + AzureDataContainer + "/" + testenv.AppLocationV2
			err = testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV2, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download ES app")

			// Upload V2 apps to S3 for Standalone
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s ES app to Azure for Standalone", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s ES app to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############ UPGRADE VERIFICATION ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = esApp
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(esApp)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworkazures1, appframework: can deploy a Standalone instance with App Framework enabled and install around 350MB of apps at once", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
			   * Create app source for Standalone
			   * Add more apps than usual on Azure for this test
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############### VERIFICATION ################
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify App enabled and version by running splunk cmd
			*/

			//################## SETUP ####################

			// Creating a bigger list of apps to be installed than the default one
			appList := append(appListV1, testenv.RestartNeededApps...)
			appFileList := testenv.GetAppFileList(appList)
			appVersion := "V1"

			// Download apps from Azure
			testcaseEnvInst.Log.Info("Download bigger amount of apps from Azure for this test")
			containerName := "/" + AzureDataContainer + "/" + testenv.AppLocationV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload apps to Azure
			testcaseEnvInst.Log.Info("Upload bigger amount of apps to Azure for this test")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############### VERIFICATION ################
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1azure, smoke, appframeworkazures1, appframework: can deploy a standalone instance with App Framework enabled for manual poll", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Standalone			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework(MANUAL POLL) and wait for the pod to be ready
			   ############### VERIFICATION ################
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify no pod resets triggered due to app install
			   * Verify App enabled and version by running splunk cmd
			     ############ UPGRADE V2 APPS ###########
			   * Upload V2 apps to Azure App Source
			   ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			   * Verify Apps are not updated
			   ############ ENABLE MANUAL POLL ############
			   * Verify Manual Poll disabled after the check
			   ############ V2 APP VERIFICATION FOR STANDALONE  ###########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify no pod resets triggered due to app install
			   * Verify App enabled  and version by running splunk cmd
			*/

			//################## SETUP ####################

			// Upload V1 apps to Azure for Standalone
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			azTestDir = "azures1appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5

			// Create App Framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// ############ VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify repo state on App to be disabled to be 1 (i.e app present on Azure bucket)
			appName := appListV1[0]
			appFileName := testenv.GetAppFileList([]string{appName})
			Expect(testcaseEnvInst.VerifyAppRepoState(ctx, deployment, standalone.Name, standalone.Kind, appSourceName, 1, appFileName[0])).To(Succeed(), "App repo state verification failed")

			// Disable the app
			testcaseEnvInst.Log.Info("Download disabled version of apps from Azure for this test")
			err = testenv.DisableAppsOnAzure(ctx, downloadDirV1, appFileName, azTestDir)
			Expect(err).To(Succeed(), "Unable to disable apps on Azure")

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileName)

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Wait for App state to update after config file change
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testcaseEnvInst.WaitforAppInstallState(ctx, deployment, []string{standalonePodName}, testcaseEnvInst.GetName(), appName, "disabled", false)

			// Delete the file from Azure
			azFilepath := "/" + AzureContainer + "/" + filepath.Join(azTestDir, appFileName[0])
			azureBlobClient := &testenv.AzureBlobClient{}
			err = azureBlobClient.DeleteFileOnAzure(ctx, azFilepath, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to delete %s app on Azure test directory", appFileName[0]))

			// Verify repo state is set to 2 (i.e app deleted from Azure bucket)
			Expect(testcaseEnvInst.VerifyAppRepoState(ctx, deployment, standalone.Name, standalone.Kind, appSourceName, 2, appFileName[0])).To(Succeed(), "App repo state verification failed")

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworkazures1, appframework: can deploy a Standalone instance with App Framework enabled, attempt to update using incorrect Azure credentials", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Standalone
			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############ V1 APP VERIFICATION FOR STANDALONE###########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify no pod resets triggered due to app install
			   * Verify App enabled  and version by running splunk cmd
			   // ############  Modify secret key ###########
			   * Create App Framework volume with random credentials and apply to Spec
			   * Check for changes in App phase to determine if next poll has been triggered
			   ############ UPGRADE V2 APPS ###########
			   * Upload V2 apps to Azure App Source
			   * Check no apps are updated as auth key is incorrect
			   ############  Modify secret key to correct one###########
			   * Apply spec with correct credentials
			   * Wait for the pod to be ready
			   ############ V2 APP VERIFICATION###########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify no pod resets triggered due to app install
			   * Verify App enabled  and version by running splunk cmd
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Upload V1 apps to Azure for Standalone
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5

			// Create App Framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			secretref := standalone.Spec.AppFrameworkConfig.VolList[0].SecretRef
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			secretStruct, _ := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), secretref)
			secretData := secretStruct.Data
			modifiedSecretData := map[string][]byte{"azure_sa_name": []byte(testenv.RandomDNSName(5)), "azure_sa_secret_key": []byte(testenv.RandomDNSName(5))}

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// ############ INITIAL VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// ############  Modify secret key ###########
			// Create App Framework volume with invalid credentials and apply to Spec
			testcaseEnvInst.Log.Info("Update Standalone spec with invalid credentials")
			err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), secretref, modifiedSecretData)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// ############## UPGRADE APPS #################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			Expect(azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)).To(Succeed(), "Azure file deletion failed")
			uploadedApps = nil

			// Upload V2 apps to Azure for Standalone
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Check no apps are updated as auth key is incorrect
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// ############  Modify secret key to correct one###########
			// Apply spec with correct credentials
			err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), secretref, secretData)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############ UPGRADE VERIFICATION ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV2
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworkazures1, appframework: Deploy a Standalone instance with App Framework enabled and update apps after app download is completed", NodeTimeout(testenv.LongTimeout), func(ctx SpecContext) {

			/* Test Steps
			################## SETUP ####################
			* Upload app to Azure for Standalone
			* Create app source for Standalone
			* Prepare and deploy Standalone
			* While app download is completed, upload new versions of the apps
			############## VERIFICATIONS ################
			* Verify App download is in completed on Standalone
			* Upload updated app to Azure as previous app download is complete
			* Verify app is installed on Standalone
			############## UPGRADE VERIFICATIONS ################
			* Wait for next poll to trigger on Standalone
			* Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download test app from Azure
			appVersion := "V1"
			appListV1 := []string{appListV1[0]}
			appFileList := testenv.GetAppFileList(appListV1)
			containerName := "/" + AzureDataContainer + "/" + testenv.AppLocationV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload apps to Azure for Standalone
			testcaseEnvInst.Log.Info("Upload apps to Azure for Standalone")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload app to Azure test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 120)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			// Verify App download is in progress on Standalone
			Expect(testcaseEnvInst.VerifyAppState(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyPending, testenv.AppStateVerificationTimeout)).To(Succeed(), "App state verification failed")

			// Upload V2 apps to Azure for Standalone
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s app to Azure for Standalone", appVersion))
			appFileList = testenv.GetAppFileList([]string{appListV2[0]})

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s app to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//######### VERIFICATIONS #############
			appVersion = "V1"
			Expect(testcaseEnvInst.VerifyAppInstalled(ctx, deployment, testcaseEnvInst.GetName(), []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}, appListV1, false, "enabled", false, false)).To(Succeed(), "App installation verification failed")

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			//############ UPGRADE VERIFICATION ###########
			appVersion = "V2"
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: []string{appListV2[0]}, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworkazures1, appframework: can deploy a Standalone instance and install a bigger volume of apps than the operator PV disk space", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
			   * Create a file on operator to utilize over 1G of space
			   * Upload file to Azure for standalone
			   * Create app source for Standalone with parallelDownload=15
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############### VERIFICATION ################
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify App enabled and version by running splunk cmd
			*/

			//################## SETUP ####################
			// Create a large file on Operator pod
			opPod := testcaseEnvInst.GetOperatorPodName()
			err := testenv.CreateDummyFileOnOperator(ctx, deployment, opPod, testenv.AppDownloadVolume, "1G", "test_file.img")
			Expect(err).To(Succeed(), "Unable to create file on operator")
			filePresentOnOperator = true

			// Upload apps to Azure
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 15

			// Create App Framework Spec
			appSourceName := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############### VERIFICATION ################
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworkazures1, appframework: Deploy a Standalone instance with App Framework enabled and delete apps from app directory when app download is complete", NodeTimeout(testenv.LongTimeout), func(ctx SpecContext) {

			/* Test Steps
				################## SETUP ####################
				* Upload big-size app to Azure for Standalone
				* Create app source for Standalone
				* Prepare and deploy Standalone
				* When app download is complete, delete apps from app directory
				############## VERIFICATIONS ################
				* Verify App installation is in progress on Standalone
				* Upload more apps from Azure during bigger app install
				* Wait for polling interval to pass
			    * Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download big size apps from Azure
			appVersion := "V1"
			appList := testenv.BigSingleApp
			appFileList := testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + testenv.AppLocationV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download big app")

			// Upload big-size app to Azure for Standalone
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Standalone")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			// Verify App Download is completed on Standalone
			Expect(testcaseEnvInst.VerifyAppState(ctx, deployment, deployment.GetName(), standalone.Kind, appSourceName, appFileList, enterpriseApi.AppPkgPodCopyComplete, enterpriseApi.AppPkgPodCopyPending, testenv.AppStateVerificationTimeout)).To(Succeed(), "App state verification failed")

			//Delete apps from app-directory when app download is complete
			opPod := testcaseEnvInst.GetOperatorPodName()
			podDownloadPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), standalone.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceName, testenv.AppInfo[appList[0]]["filename"])
			err = testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
			Expect(err).To(Succeed(), "Unable to delete file on pod")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// ############ VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appList, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1azure, appframeworkazures1, appframework: can deploy a Standalone instance with App Framework enabled, install apps and check isDeploymentInProgress is set for Standaloen and MC CR's", NodeTimeout(testenv.ShortTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Standalone			   * Wait for the pod to be ready
			   * Upload V1 apps to Azure for Standalone
			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework			   * Wait for the pod to be ready
			*/

			// ################## SETUP FOR MONITORING CONSOLE ####################

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5

			// Create App Framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App Framework")

			// Verify IsDeploymentInProgress Flag is set to true for Standalone CR
			Expect(testcaseEnvInst.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, deployment.GetName(), standalone.Kind)).To(Succeed(), "IsDeploymentInProgress flag not set")

			// Wait for Standalone to be in READY status
			Expect(testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)).To(Succeed(), "Standalone not ready")

		})
	})
})
