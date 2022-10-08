// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.s
package azures1appfw

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo"
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

	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		azTestDir = "s1appfw-" + testenv.RandomDNSName(4)
		appSourceVolumeName = "appframework-test-volume-" + testenv.RandomDNSName(3)
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}

		// Delete files uploaded to Azure
		if !testcaseEnvInst.SkipTeardown {
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}

		if filePresentOnOperator {
			//Delete files from app-directory
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			podDownloadPath := filepath.Join(testenv.AppDownloadVolume, "test_file.img")
			testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
		}
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1azure, appframeworks1azure, az_appfw: can deploy a Standalone instance with App Framework enabled, install apps then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			   * Upload V1 apps to Azure for Standalone
			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############ V1 APP VERIFICATION FOR STANDALONE AND MONITORING CONSOLE ###########
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
			   ############ V2 APP VERIFICATION FOR STANDALONE AND MONITORING CONSOLE  ###########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify no pod resets triggered due to app install
			   * Verify App enabled  and version by running splunk cmd
			*/

			// ################## SETUP FOR MONITORING CONSOLE ####################

			// Upload V1 apps to Azure for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))

			azTestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5

			// Create App framework spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)
			appFrameworkSpecMC.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpecMC,
			}

			// Deploy Monitoring Console
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// ################## SETUP FOR STANDALONE ####################
			// Upload V1 apps to Azure for Standalone
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			azTestDir = "s1appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ############ INITIAL VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo, mcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############## UPGRADE APPS #################

			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)

			uploadedApps = nil
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone and Monitoring Console", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ UPGRADE VERIFICATION ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV2
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			mcAppSourceInfo.CrAppVersion = appVersion
			mcAppSourceInfo.CrAppList = appListV2
			mcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo, mcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1azure, appframeworks1azure, az_appfw: can deploy a Standalone instance with App Framework enabled, install apps then downgrade them", func() {

			/* Test Steps
			################## SETUP ####################
			* Upload V2 apps to Azure for Monitoring Console
			* Create app source for Monitoring Console
			* Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			* Upload V2 apps to Azure for Standalone
			* Create app source for Standalone
			* Prepare and deploy Standalone with app framework and wait for the pod to be ready
			############ INITIAL VERIFICATION FOR STANDALONE AND MONITORING CONSOLE ###########
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
			* Wait for Monitoring Console and Standalone pods to be ready
			########## DOWNGRADE VERIFICATION FOR STANDALONE AND MONITORING CONSOLE ###########
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

			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			azTestDirMC := "azures1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpecMC,
			}

			// Deploy Monitoring Console
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Create App framework Spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo, mcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############# DOWNGRADE APPS ################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Upload V1 apps to Azure for Standalone and Monitoring Console
			appVersion = "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone and Monitoring Console", appVersion))
			appFileList = testenv.GetAppFileList(appListV1)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## DOWNGRADE VERIFICATION ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV1
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
			mcAppSourceInfo.CrAppVersion = appVersion
			mcAppSourceInfo.CrAppList = appListV1
			mcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo, mcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1azure, smoke, appframeworks1azure, az_appfw: can deploy a Standalone instance with App Framework enabled, install apps, scale up, install apps on new pod, scale down", func() {

			/* Test Steps
			################## SETUP ####################
			* Upload apps on Azure
			* Create 2 app sources for Monitoring Console and Standalone
			* Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			* Prepare and deploy Standalone CRD with app framework and wait for the pod to be ready
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
			* Wait for Monitoring Console and  Standalone to be ready
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
			* Wait for Monitoring Console and Standalone to be ready
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
			// Upload V1 apps to Azure for Standalone and Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone and Monitoring Console", appVersion))

			azTestDirMC := "azures1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)

			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpecMC,
			}

			// Deploy Monitoring Console
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload apps to Azure for Standalone
			azTestDir := "azures1appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			scaledReplicaCount := 2
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: scaledReplicaCount}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo, mcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			//############### SCALING UP ##################
			// Scale up Standalone instance
			testcaseEnvInst.Log.Info("Scale up Standalone")

			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(ctx, deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale up Standalone")

			// Ensure Standalone is scaling up
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseReady)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			//########### SCALING UP VERIFICATION #########
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			//############## SCALING DOWN #################
			// Scale down Standalone instance
			testcaseEnvInst.Log.Info("Scale down Standalone")
			scaledReplicaCount = 1
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(ctx, deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone after scaling down")

			standalone.Spec.Replicas = int32(scaledReplicaCount)
			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale down Standalone")

			// Ensure Standalone is scaling down
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseScalingDown)

			// Wait for Standalone to be in READY status
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseReady)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			//########### SCALING DOWN VERIFICATION #######
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1azure, integration, appframeworks1azure, az_appfw: can deploy a Standalone instance with App Framework enabled, install apps, scale up, upgrade apps", func() {

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

			// Create App framework Spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			scaledReplicaCount := 2
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: scaledReplicaCount}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			//############### SCALING UP ##################
			// Scale up Standalone instance
			testcaseEnvInst.Log.Info("Scale up Standalone")

			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(ctx, deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale up Standalone")

			// Ensure Standalone is scaling up
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), enterpriseApi.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// ############## UPGRADE APPS #################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to Azure for Standalone and Monitoring Console
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone and Monitoring Console", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ SCALING UP/UPGRADE VERIFICATIONS ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV2
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			standaloneAppSourceInfo.CrPod = []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0), fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 1)}
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	// ES App Installation not supported at the time. Will be added back at a later time.
	XContext("Standalone deployment (S1) with App Framework", func() {
		It("s1azure, integration, appframeworks1azure, az_appfw: can deploy a Standalone and have ES app installed", func() {

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
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)

			Expect(err).To(Succeed(), "Unable to download ES app")

			// Upload ES app to Azure
			testcaseEnvInst.Log.Info("Upload ES app on Azure")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload ES app to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone with App framework")

			// Verify App Downlaod State on CR
			// testenv.VerifyAppListPhase(deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, enterpriseApi.PhaseDownload, appFileList)

			// Verify Apps download on Operator Pod
			// kind := standalone.Kind
			// opLocalAppPathStandalone := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testcaseEnvInst.GetName(), kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceName)
			// opPod := testenv.GetOperatorPodName(testcaseEnvInst.GetName())
			appVersion := "V1"
			// testcaseEnvInst.Log.Info("Verify Apps are downloaded on Splunk Operator container for apps", "version", appVersion)
			// testenv.VerifyAppsDownloadedOnContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{opPod}, appFileList, opLocalAppPathStandalone)

			// Ensure Standalone goes to Ready phase
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			//################## VERIFICATION #############
			// Verify ES app is downloaded
			testcaseEnvInst.Log.Info("Verify ES app is downloaded on Standalone")
			initContDownloadLocation := testenv.AppStagingLocOnPod + appSourceName
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testcaseEnvInst.Log.Info("Verify Apps are downloaded on Pod", "version", appVersion, "Pod Name", podName)
			testenv.VerifyAppsDownloadedOnContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			// Verify ES app is installed
			testcaseEnvInst.Log.Info("Verify ES app is installed on Standalone")
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), standalonePod, esApp, false, "enabled", false, false)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: can deploy a Standalone instance with App Framework enabled and install around 350MB of apps at once", func() {

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
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload apps to Azure
			testcaseEnvInst.Log.Info("Upload bigger amount of apps to Azure for this test")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
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
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############### VERIFICATION ################
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1azure, smoke, appframeworks1azure, az_appfw: can deploy a standalone instance with App Framework enabled for manual poll", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			   * Create app source for Standalone
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
			   ############ V2 APP VERIFICATION FOR STANDALONE AND MONITORING CONSOLE  ###########
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

			// Upload V1 apps to Azure for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			azTestDirMC := "azures1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 0)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpecMC,
			}

			// Deploy Monitoring Console
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V1 apps to Azure
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 0)

			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Create Standalone Deployment with App Framework
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############### VERIFICATION ################
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			//############### UPGRADE APPS ################

			//Delete apps on Azure for new Apps
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			//Upload new Versioned Apps to Azure
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			appVersion = "V1"
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############ ENABLE MANUAL POLL ############
			appVersion = "V2"
			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")
			testcaseEnvInst.Log.Info("Config map data for", "Standalone", config.Data["Standalone"])

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["Standalone"] = strings.Replace(config.Data["Standalone"], "off", "on", 1)
			config.Data["MonitoringConsole"] = strings.Replace(config.Data["Standalone"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify config map set back to off after poll trigger for %s app", appVersion))
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["Standalone"], "status: off") && strings.Contains(config.Data["MonitoringConsole"], "status: off")).To(Equal(true), "Config map update not complete")

			//############### VERIFICATION FOR UPGRADE ################
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV2
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: can deploy Several standalone CRs in the same namespace with App Framework enabled", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Add more apps than usual on Azure for this test
			   * Split the App list into 2 segments with a common apps and uncommon apps for each Standalone
			   * Create app source for 2 Standalones
			   * Prepare and deploy Standalones with app framework and wait for the pod to be ready
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

			// Creating a list of apps to be installed on both standalone
			appList1 := append(appListV1, testenv.RestartNeededApps[len(testenv.RestartNeededApps)/2:]...)
			appList2 := append(appListV1, testenv.RestartNeededApps[:len(testenv.RestartNeededApps)/2]...)
			appVersion := "V1"

			// Download apps from Azure
			testcaseEnvInst.Log.Info("Download the extra apps from Azure for this test")
			appFileList := testenv.GetAppFileList(testenv.RestartNeededApps)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload apps to Azure for first Standalone
			testcaseEnvInst.Log.Info("Upload apps to Azure for 1st Standalone")
			appFileListStandalone1 := testenv.GetAppFileList(appList1)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileListStandalone1)
			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to Azure for second Standalone
			testcaseEnvInst.Log.Info("Upload apps to Azure for 2nd Standalone")
			azTestDirStandalone2 := "azures1appfw-2-" + testenv.RandomDNSName(4)
			appFileListStandalone2 := testenv.GetAppFileList(appList2)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirStandalone2, appFileListStandalone2)
			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Create App framework Spec
			appSourceNameStandalone2 := "appframework-2-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameStandalone2 := "appframework-test-volume-2-" + testenv.RandomDNSName(3)
			appFrameworkSpecStandalone2 := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameStandalone2, enterpriseApi.ScopeLocal, appSourceNameStandalone2, azTestDirStandalone2, 60)
			specStandalone2 := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpecStandalone2,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy 1st Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy 1st Standalone instance")
			testcaseEnvInst.Log.Info("Deploy 2nd Standalone")
			standalone2Name := deployment.GetName() + testenv.RandomDNSName(3)
			standalone2, err := deployment.DeployStandaloneWithGivenSpec(ctx, standalone2Name, specStandalone2)
			Expect(err).To(Succeed(), "Unable to deploy 2nd Standalone instance")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone2, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############### VERIFICATION ################
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appList1, CrAppFileList: appFileListStandalone1}
			standalone2Pod := []string{fmt.Sprintf(testenv.StandalonePod, standalone2Name, 0)}
			standalone2AppSourceInfo := testenv.AppSourceInfo{CrKind: standalone2.Kind, CrName: standalone2Name, CrAppSourceName: appSourceNameStandalone2, CrPod: standalone2Pod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appList2, CrAppFileList: appFileListStandalone2}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo, standalone2AppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: can add new apps to app source while install is in progress and have all apps installed", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload V1 apps to Azure for Monitoring Console
			    * Create app source for Monitoring Console
			   	* Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
				* Upload big-size app to Azure for Standalone
				* Create app source for Standalone
				* Prepare and deploy Standalone
				############## VERIFICATIONS ################
				* Verify App installation is in progress on Standalone
				* Upload more apps from Azure during bigger app install
				* Wait for polling interval to pass
			    * Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR MONITORING CONSOLE ####################
			// Upload V1 apps to Azure for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			azTestDirMC := "azures1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)

			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpecMC,
			}

			// Deploy Monitoring Console
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from Azure
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err = testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to Azure for Standalone
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Standalone")
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Verify App installation is in progress on Standalone
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyComplete)

			// Upload more apps to Azure for Standalone
			appList = testenv.ExtraApps
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload more apps to Azure for Standalone")
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload more apps to Azure test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Verify all apps are installed on Standalone
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Standalone", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{standalonePodName}, appList, true, "enabled", false, false)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: Deploy a Standalone instance with App Framework enabled and reset operator pod while app install is in progress", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload big-size app to Azure for Standalone
				* Create app source for Standalone
				* Prepare and deploy Standalone
				* While app install is in progress, restart the operator
				############## VERIFICATIONS ################
				* Verify App installation is in progress on Standalone
				* Upload more apps from Azure during bigger app install
				* Wait for polling interval to pass
			    * Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from Azure
			appVersion := "V1"
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to Azure for Standalone
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Standalone")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Verify App installation is in progress on Standalone
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgInstallPending)

			// Delete Operator pod while Install in progress
			testenv.DeleteOperatorPod(testcaseEnvInst)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ############ VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appList, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: Deploy a Standalone instance with App Framework enabled and reset operator pod while app download is in progress", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload big-size app to Azure for Standalone
				* Create app source for Standalone
				* Prepare and deploy Standalone
				* While app download is in progress, restart the operator
				############## VERIFICATIONS ################
				* Verify App download is in progress on Standalone
				* Upload more apps from Azure during bigger app install
				* Wait for polling interval to pass
			    * Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from Azure
			appVersion := "V1"
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to Azure for Standalone
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Standalone")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)

			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Verify App download is in progress on Standalone
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList, enterpriseApi.AppPkgDownloadComplete, enterpriseApi.AppPkgDownloadPending)

			// Delete Operator pod while Install in progress
			testenv.DeleteOperatorPod(testcaseEnvInst)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ############ VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appList, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	// CSPL-2068: This test needs to be updated to pass on Azure
	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: can deploy a Standalone instance with App Framework enabled, install an app, then disable it by using a disabled version of the app and then remove it from app source", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Standalone
			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############ VERIFICATION###########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify no pod resets triggered due to app install
			   * Verify App enabled  and version by running splunk cmd
			   ############ Upload Disabled App ###########
			   * Download disabled app from Azure
			   * Delete the app from Azure
			   * Check for repo state in App Deployment Info
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

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ############ VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify repo state on App to be disabled to be 1 (i.e app present on Azure bucket)
			appName := appListV1[0]
			appFileName := testenv.GetAppFileList([]string{appName})
			testenv.VerifyAppRepoState(ctx, deployment, testcaseEnvInst, standalone.Name, standalone.Kind, appSourceName, 1, appFileName[0])

			// Disable the app
			testcaseEnvInst.Log.Info("Download disabled version of apps from Azure for this test")
			testenv.DisableAppsOnAzure(ctx, downloadDirV1, appFileName, azTestDir)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileName)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Wait for App state to update after config file change
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.WaitforAppInstallState(ctx, deployment, testcaseEnvInst, []string{standalonePodName}, testcaseEnvInst.GetName(), appName, "disabled", false)

			// Delete the file from Azure
			azFilepath := "/" + AzureContainer + "/" + filepath.Join(azTestDir, appFileName[0])
			azureBlobClient := &testenv.AzureBlobClient{}
			err = azureBlobClient.DeleteFileOnAzure(ctx, azFilepath, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to delete %s app on Azure test directory", appFileName[0]))

			// Verify repo state is set to 2 (i.e app deleted from Azure bucket)
			testenv.VerifyAppRepoState(ctx, deployment, testcaseEnvInst, standalone.Name, standalone.Kind, appSourceName, 2, appFileName[0])

		})
	})

	// CSPL-2068: This test needs to be updated to pass on Azure
	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: can deploy a Standalone instance with App Framework enabled, attempt to update using incorrect Azure credentials", func() {

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
			   * Create App framework volume with random credentials and apply to Spec
			   * Check for changes in App phase to determine if next poll has been triggered
			   ############ UPGRADE V2 APPS ###########
			   * Upload V2 apps to Azure App Source
			   * Check no apps are updated as auth key is incorrect
			   ############  Modify secret key to correct one###########
			   * Apply spec with correct credentails
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

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			secretref := standalone.Spec.AppFrameworkConfig.VolList[0].SecretRef
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			secretStruct, _ := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), secretref)
			secretData := secretStruct.Data
			modifiedSecretData := map[string][]byte{"Azure_access_key": []byte(testenv.RandomDNSName(5)), "Azure_secret_key": []byte(testenv.RandomDNSName(5))}

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ############ INITIAL VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############  Modify secret key ###########
			// Create App framework volume with invalid credentials and apply to Spec
			testcaseEnvInst.Log.Info("Update Standalone spec with invalid credentials")
			err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), secretref, modifiedSecretData)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// ############## UPGRADE APPS #################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to Azure for Standalone
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Check no apps are updated as auth key is incorrect
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############  Modify secret key to correct one###########
			// Apply spec with correct credentials
			err = testenv.ModifySecretObject(ctx, deployment, testcaseEnvInst.GetName(), secretref, secretData)
			Expect(err).To(Succeed(), "Unable to update secret Object")

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ UPGRADE VERIFICATION ###########
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV2
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: Deploy a Standalone instance with App Framework enabled and update apps after app download is completed", func() {

			/* Test Steps
			################## SETUP ####################
			* Upload apps to Azure for Standalone
			* Create app source for Standalone
			* Prepare and deploy Standalone
			* While app download is completed, upload new versions of the apps
			############## VERIFICATIONS ################
			* Verify App download is in progress on Standalone
			* Upload more apps from Azure during bigger app install
			* Wait for polling interval to pass
			* Verify all apps are installed on Standalone
			############## UPGRADE VERIFICATIONS ################
			* Verify App download is in progress on Standalone
			* Upload more apps from Azure during bigger app install
			* Wait for polling interval to pass
			* Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from Azure
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload apps to Azure for Standalone
			testcaseEnvInst.Log.Info("Upload apps to Azure for Standalone")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 120)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Verify App download is in progress on Standalone
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyPending)

			// Upload V2 apps to Azure for Standalone
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ############ VERIFICATION ###########
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Check for changes in App phase to determine if next poll has been triggered
			appFileList = testenv.GetAppFileList(appListV2)
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ UPGRADE VERIFICATION ###########
			appVersion = "V2"
			standaloneAppSourceInfo.CrAppVersion = appVersion
			standaloneAppSourceInfo.CrAppList = appListV2
			standaloneAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: can deploy a Standalone instance and install a bigger volume of apps than the operator PV disk space", func() {

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
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
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

			// Create App framework Spec
			appSourceName := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
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
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############### VERIFICATION ################
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1azure, appframeworks1azure, az_appfw: Deploy a Standalone instance with App Framework enabled and delete apps from app directory when app download is complete", func() {

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
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download big app")

			// Upload big-size app to Azure for Standalone
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Standalone")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Verify App Download is completed on Standalone
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList, enterpriseApi.AppPkgPodCopyComplete, enterpriseApi.AppPkgPodCopyPending)

			//Delete apps from app-directory when app download is complete
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			podDownloadPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), standalone.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceName, testenv.AppInfo[appList[0]]["filename"])
			err = testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
			Expect(err).To(Succeed(), "Unable to delete file on pod")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ############ VERIFICATION ###########
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appList, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1azure, appframeworks1azure, az_appfw: can deploy a Standalone instance with App Framework enabled, install apps and check isDeploymentInProgress is set for Standaloen and MC CR's", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console with app framework
			   * Check isDeploymentInProgress is set for Monitoring Console CR
			   * Wait for the pod to be ready
			   * Upload V1 apps to Azure for Standalone
			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework
			   * Check isDeploymentInProgress is set for Monitoring Console CR
			   * Wait for the pod to be ready
			*/

			// ################## SETUP FOR MONITORING CONSOLE ####################

			// Upload V1 apps to Azure for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))

			azTestDirMC := "azures1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpecMC,
			}

			// Deploy Monitoring Console
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify IsDeploymentInProgress Flag is set to true for Monitroing Console CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgressFlag")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, mcName, mc.Kind)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// ################## SETUP FOR STANDALONE ####################
			// Upload V1 apps to Azure for Standalone
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Standalone", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, azTestDir, 60)
			appFrameworkSpec.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
					MonitoringConsoleRef: corev1.ObjectReference{
						Name: mcName,
					},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Verify IsDeploymentInProgress Flag is set to true for Standalone CR
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

		})
	})
})
