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
package s1appfw

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	testenv "github.com/splunk/splunk-operator/test/testenv"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("s1appfw test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var s3TestDir string
	var uploadedApps []string
	var appSourceName string
	var appSourceVolumeName string

	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		s3TestDir = "s1appfw-" + testenv.RandomDNSName(4)
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
		// Delete files uploaded to S3
		if !testcaseEnvInst.SkipTeardown {
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, install apps then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			   * Upload V1 apps to S3 for Standalone
			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############ INITIAL VERIFICATION ###########
			   * Verify V1 apps are copied and installed on Monitoring Console and on Standalone
			   ############## UPGRADE APPS #################
			   * Upload V2 apps on S3
			   * Wait for Monitoring Console and Standalone pods to be ready
			   ############ UPGRADE VERIFICATION ###########
			   * Verify V2 apps are copied, installed and upgraded on Monitoring Console and on Standalone
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
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

			// Upload V1 apps to S3 for Standalone
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
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

			//############ INITIAL VERIFICATION ###########
			// Verify V1 apps are downloaded on Standalone and Monitoring Console
			initContDownloadLocationStandalonePod := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded on Standalone pod %s", appVersion, standalonePodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify V1 apps are copied to location
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Standalone and Monitoring Console", appVersion))
			podNames := []string{standalonePodName, mcPodName}
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, false)

			// Verify V1 apps are installed
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are installed on Standalone and Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, "enabled", false, false)

			//############## UPGRADE APPS #################
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for Standalone and Monitoring Console
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone and Monitoring Console", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			//############ UPGRADE VERIFICATION ###########
			// Verify V2 apps are downloaded
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded on Standalone pod %s", appVersion, standalonePodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify V2 apps are copied to location
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Standalone and Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV2, true, false)

			// Verify V2 apps are installed
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify apps have been updated to %s on Standalone and Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV2, true, "enabled", true, false)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, install apps then downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V2 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			   * Upload V2 apps to S3 for Standalone
			   * Create app source for Standalone
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############ INITIAL VERIFICATION ###########
			   * Verify apps are copied and installed on Monitoring Console and on Standalone
			   ############# DOWNGRADE APPS ################
			   * Upload V1 apps on S3
			   * Wait for Monitoring Console and Standalone pods to be ready
			   ########## DOWNGRADE VERIFICATION ###########
			   * Verify apps are copied, installed and downgraded on Monitoring Console and on Standalone
			*/

			//################## SETUP ####################
			// Upload V2 apps to S3
			appVersion := "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone and Monitoring Console", appVersion))
			appFileList := testenv.GetAppFileList(appListV2)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
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
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
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

			//############ INITIAL VERIFICATION ###########
			// Verify V2 apps are downloaded on Standalone Pod and Monitoring Console
			initContDownloadLocationStandalonePod := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded for Standalone pod %s", appVersion, standalonePodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded for Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify V2 apps are copied to location
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Standalone and Monitoring Console", appVersion))
			podNames := []string{standalonePodName, mcPodName}
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV2, true, false)

			// Verify V2 apps are installed
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are installed on Standalone and Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV2, true, "enabled", true, false)

			//############# DOWNGRADE APPS ################
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Upload V1 apps to S3 for Standalone and Monitoring Console
			appVersion = "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone and Monitoring Console", appVersion))
			appFileList = testenv.GetAppFileList(appListV1)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			//########## DOWNGRADE VERIFICATION ###########
			// Verify apps are downloaded
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded for Standalone pod %s", appVersion, standalonePodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded for Monitoring Console pod %s ", appVersion, mcPodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify apps are copied to location
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Standalone and Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, false)

			// Verify apps are downgraded
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify apps have been downgraded to %s on Standalone and Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, "enabled", false, false)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1, integration, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, install apps, scale up, install apps on new pod, scale down", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload apps on S3
			   * Create 2 app sources for Monitoring Console and Standalone
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Prepare and deploy Standalone CRD with app framework and wait for the pod to be ready
			   ########## INITIAL VERIFICATION #############
			   * Verify apps are copied and installed on Monitoring Console and Standalone
			   ############### SCALING UP ##################
			   * Scale up Standalone
			   * Wait for Monitoring Console and  Standalone to be ready
			   ########### SCALING UP VERIFICATION #########
			   * Verify apps are copied and installed on new Standalone pod
			   ############## SCALING DOWN #################
			   * Scale down Standalone
			   * Wait for Monitoring Console and Standalone to be ready
			   ########### SCALING DOWN VERIFICATION #######
			   * Verify apps are still copied and installed on Standalone
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Standalone and Monitoring Console
			appVersion := "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone and Monitoring Console", appVersion))
			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
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

			// Upload apps to S3 for Standalone
			s3TestDir := "s1appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
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

			//########## INITIAL VERIFICATION #############
			// Verify apps are downloaded on Standalone and Monitoring Console
			initContDownloadLocationStandalonePod := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			appFileList = testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded for Standalone pod %s", appVersion, standalonePodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded for Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify apps are copied to location
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Standalone and Monitoring Console", appVersion))
			podNames := []string{standalonePodName, mcPodName}
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, true)

			// Verify apps are installed
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are installed on Standalone and Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, "enabled", false, false)

			//############### SCALING UP ##################
			// Scale up Standalone instance
			testcaseEnvInst.Log.Info("Scale up Standalone")
			scaledReplicaCount := 2
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(ctx, deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(ctx, standalone)
			Expect(err).To(Succeed(), "Failed to scale up Standalone")

			// Ensure Standalone is scaling up
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), splcommon.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), splcommon.PhaseReady)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			podNames = append(podNames, fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 1))
			standalonePods := []string{standalonePodName, fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 1)}

			//########### SCALING UP VERIFICATION #########
			// Verify apps are downloaded
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded after scaling up on Standalone pods %s", appVersion, standalonePods))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), standalonePods, appFileList, initContDownloadLocationStandalonePod)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded after scaling up on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify apps are copied to location
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on all pods after scaling up", appVersion))
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, false)

			// Verify apps are installed
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are installed on all pods after scaling up", appVersion))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, "enabled", false, false)

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
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), splcommon.PhaseScalingDown)

			// Wait for Standalone to be in READY status
			testenv.VerifyStandalonePhase(ctx, deployment, testcaseEnvInst, deployment.GetName(), splcommon.PhaseReady)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			podNames = testenv.DumpGetPods(testcaseEnvInst.GetName())

			//########### SCALING DOWN VERIFICATION #######
			// Verify apps are downloaded
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded after scaling down for Standalone pod %s", appVersion, standalonePodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are downloaded after scaling down for Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify apps are copied to location
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on all Pods after scaling down ", appVersion))
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, false)

			// Verify apps are installed
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify %s apps are still installed on the pods after scaling down", appVersion))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, appListV1, true, "enabled", false, false)

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1, integration, appframeworks1, appframework: can deploy a Standalone and have ES app installed", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload ES app to S3
			   * Create App Source for Standalone
			   * Prepare and deploy Standalone and wait for the pod to be ready
			   ################## VERIFICATION #############
			   * Verify ES app is installed on Standalone
			*/

			//################## SETUP ####################

			// Download ES App from S3
			testcaseEnvInst.Log.Info("Download ES app from S3")
			esApp := []string{"SplunkEnterpriseSecuritySuite"}
			appFileList := testenv.GetAppFileList(esApp)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download ES app")

			// Upload ES app to S3
			testcaseEnvInst.Log.Info("Upload ES app on S3")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload ES app to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
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

			// Ensure Standalone goes to Ready phase
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			//################## VERIFICATION #############
			// Verify ES app is downloaded
			testcaseEnvInst.Log.Info("Verify ES app is downloaded on Standalone")
			initContDownloadLocation := "/init-apps/" + appSourceName
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			// Verify ES app is installed
			testcaseEnvInst.Log.Info("Verify ES app is installed on Standalone")
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), standalonePod, esApp, false, "enabled", false, false)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled and install around 350MB of apps at once", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create app source for Standalone
			   * Add more apps than usual on S3 for this test
			   * Prepare and deploy Standalone with app framework and wait for the pod to be ready
			   ############### VERIFICATION ################
			   * Verify apps are copied, installed on Standalone
			*/

			//################## SETUP ####################

			// Creating a bigger list of apps to be installed than the default one
			appList := append(appListV1, testenv.RestartNeededApps...)
			appFileList := testenv.GetAppFileList(appList)

			// Download apps from S3
			testcaseEnvInst.Log.Info("Download bigger amount of apps from S3 for this test")
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, testenv.GetAppFileList(appList))
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload apps to S3
			testcaseEnvInst.Log.Info("Upload bigger amount of apps to S3 for this test")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
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

			//############### VERIFICATION ################
			// Verify apps are downloaded
			testcaseEnvInst.Log.Info("Verify apps are downloaded for Standalone")
			initContDownloadLocation := "/init-apps/" + appSourceName
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyAppsDownloadedByInitContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			// Verify apps are copied to correct location
			testcaseEnvInst.Log.Info("Verify apps are copied to correct location on Standalone")
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{podName}, appList, true, false)

			// Verify apps are installed
			testcaseEnvInst.Log.Info("Verify apps are installed on Standalone")
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{podName}, appList, true, "enabled", false, false)
		})
	})
})
