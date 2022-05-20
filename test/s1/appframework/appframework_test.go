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
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	testenv "github.com/splunk/splunk-operator/test/testenv"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
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
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, install apps then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			   * Upload V1 apps to S3 for Standalone
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
			   * Upload V2 apps to S3 App Source
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

			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))

			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
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

			// ################## SETUP FOR STANDALONE ####################
			// Upload V1 apps to S3 for Standalone
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, install apps then downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V2 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			   * Upload V2 apps to S3 for Standalone
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
			   * Upload V1 apps on S3
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
			// Upload V2 apps to S3
			appVersion := "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone and Monitoring Console", appVersion))
			appFileList := testenv.GetAppFileList(appListV2)
			s3TestDir = "s1appfw-" + testenv.RandomDNSName(4)

			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
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
					Spec: enterpriseApi.Spec{
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("s1, smoke, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, install apps, scale up, install apps on new pod, scale down", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload apps on S3
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
			// Upload V1 apps to S3 for Standalone and Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone and Monitoring Console", appVersion))

			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
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
					Spec: enterpriseApi.Spec{
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("s1, integration, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, install apps, scale up, upgrade apps", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload apps on S3
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
			   * Upload V2 apps to S3 App Source
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
			// Upload V1 apps to S3 for Standalone
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone", appVersion))

			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled and install around 350MB of apps at once", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create app source for Standalone
			   * Add more apps than usual on S3 for this test
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

			// Download apps from S3
			testcaseEnvInst.Log.Info("Download bigger amount of apps from S3 for this test")
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)

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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("s1, smoke, appframeworks1, appframework: can deploy a standalone instance with App Framework enabled for manual poll", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
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
			   * Upload V2 apps to S3 App Source
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

			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 0)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
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

			// Upload V1 apps to S3
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 0)

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

			//Delete apps on S3 for new Apps
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			//Upload new Versioned Apps to S3
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: can deploy Several standalone CRs in the same namespace with App Framework enabled", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Add more apps than usual on S3 for this test
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

			// Download apps from S3
			testcaseEnvInst.Log.Info("Download the extra apps from S3 for this test")
			appFileList := testenv.GetAppFileList(testenv.RestartNeededApps)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload apps to S3 for first Standalone
			testcaseEnvInst.Log.Info("Upload apps to S3 for 1st Standalone")
			appFileListStandalone1 := testenv.GetAppFileList(appList1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileListStandalone1, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to S3 for second Standalone
			testcaseEnvInst.Log.Info("Upload apps to S3 for 2nd Standalone")
			s3TestDirStandalone2 := "s1appfw-2-" + testenv.RandomDNSName(4)
			appFileListStandalone2 := testenv.GetAppFileList(appList2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirStandalone2, appFileListStandalone2, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
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
			appFrameworkSpecStandalone2 := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameStandalone2, enterpriseApi.ScopeLocal, appSourceNameStandalone2, s3TestDirStandalone2, 60)
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: can add new apps to app source while install is in progress and have all apps installed", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload V1 apps to S3 for Monitoring Console
			    * Create app source for Monitoring Console
			   	* Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
				* Upload big-size app to S3 for Standalone
				* Create app source for Standalone
				* Prepare and deploy Standalone
				############## VERIFICATIONS ################
				* Verify App installation is in progress on Standalone
				* Upload more apps from S3 during bigger app install
				* Wait for polling interval to pass
			    * Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR MONITORING CONSOLE ####################
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
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

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to S3 for Standalone
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Standalone")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
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

			// Upload more apps to S3 for Standalone
			appList = testenv.ExtraApps
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Standalone")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for Standalone")
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: Deploy a Standalone instance with App Framework enabled and reset operator pod while app install is in progress", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload big-size app to S3 for Standalone
				* Create app source for Standalone
				* Prepare and deploy Standalone
				* While app install is in progress, restart the operator
				############## VERIFICATIONS ################
				* Verify App installation is in progress on Standalone
				* Upload more apps from S3 during bigger app install
				* Wait for polling interval to pass
			    * Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from S3
			appVersion := "V1"
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to S3 for Standalone
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Standalone")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: Deploy a Standalone instance with App Framework enabled and reset operator pod while app download is in progress", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload big-size app to S3 for Standalone
				* Create app source for Standalone
				* Prepare and deploy Standalone
				* While app download is in progress, restart the operator
				############## VERIFICATIONS ################
				* Verify App download is in progress on Standalone
				* Upload more apps from S3 during bigger app install
				* Wait for polling interval to pass
			    * Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from S3
			appVersion := "V1"
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to S3 for Standalone
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Standalone")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, install an app, then disable it by using a disabled version of the app and then remove it from app source", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Standalone
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
			   * Download disabled app from s3
			   * Delete the app from s3
			   * Check for repo state in App Deployment Info
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Upload V1 apps to S3 for Standalone
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
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

			// ############ Upload Disabled App ###########
			// Verify repo state on App to be disabled to be 1 (i.e app present on S3 bucket)
			appName := appListV1[0]
			appFileName := testenv.GetAppFileList([]string{appName})
			testenv.VerifyAppRepoState(ctx, deployment, testcaseEnvInst, standalone.Name, standalone.Kind, appSourceName, 1, appFileName[0])

			// Download disabled version of app from S3
			testcaseEnvInst.Log.Info("Download disabled version of apps from S3 for this test")
			err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirDisabled, downloadDirV1, appFileName)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload disabled version of app to S3
			testcaseEnvInst.Log.Info("Upload disabled version of app to S3 for this test")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileName, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileName)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Wait for App state to update after config file change
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.WaitforAppInstallState(ctx, deployment, testcaseEnvInst, []string{standalonePodName}, testcaseEnvInst.GetName(), appName, "disabled", false)

			//Delete the file from s3
			s3Filepath := filepath.Join(s3TestDir, appFileName[0])
			err = testenv.DeleteFileOnS3(testS3Bucket, s3Filepath)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to delete %s app on S3 test directory", appFileName[0]))

			// Verify repo state is set to  2 (i.e app deleted from S3 bucket)
			testenv.VerifyAppRepoState(ctx, deployment, testcaseEnvInst, standalone.Name, standalone.Kind, appSourceName, 2, appFileName[0])

		})
	})

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: can deploy a Standalone instance with App Framework enabled, attempt to update using incorrect S3 credentials", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Standalone
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
			   * Upload V2 apps to S3 App Source
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
			// Upload V1 apps to S3 for Standalone
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 5

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
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

			secretStruct, err := testenv.GetSecretStruct(ctx, deployment, testcaseEnvInst.GetName(), secretref)
			secretData := secretStruct.Data
			modifiedSecretData := map[string][]byte{"s3_access_key": []byte(testenv.RandomDNSName(5)), "s3_secret_key": []byte(testenv.RandomDNSName(5))}

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
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for Standalone
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: Deploy a Standalone instance with App Framework enabled and update apps after app download is completed", func() {

			/* Test Steps
			################## SETUP ####################
			* Upload apps to S3 for Standalone
			* Create app source for Standalone
			* Prepare and deploy Standalone
			* While app download is completed, upload new versions of the apps
			############## VERIFICATIONS ################
			* Verify App download is in progress on Standalone
			* Upload more apps from S3 during bigger app install
			* Wait for polling interval to pass
			* Verify all apps are installed on Standalone
			############## UPGRADE VERIFICATIONS ################
			* Verify App download is in progress on Standalone
			* Upload more apps from S3 during bigger app install
			* Wait for polling interval to pass
			* Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from S3
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload apps to S3 for Standalone
			testcaseEnvInst.Log.Info("Upload apps to S3 for Standalone")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Standalone")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Standalone
			appSourceName = "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 120)
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

			// Upload V2 apps to S3 for Standalone
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Standalone", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Standalone", appVersion))
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

	XContext("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: can deploy a Standalone instance and install a bigger volume of apps than the operator PV disk space", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload 15 apps of 100MB size each to S3
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
			// Download 15 apps around 100MB each (Total 1.4GB) to have total volume above Operator PV default size (1GB)
			appVersion := "V1"
			appList := testenv.PVTestApps
			appFileList := testenv.GetAppFileList(appList)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3PVTestApps, downloadDirPVTestApps, appFileList)
			Expect(err).To(Succeed(), "Unable to download app files")

			// Upload apps to S3
			testcaseEnvInst.Log.Info("Upload 15 apps around 100MB each (Total 1.4GB) to be above Operator PV default size (1GB)")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirPVTestApps)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 15

			// Create App framework Spec
			appSourceName := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpec := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeName, enterpriseApi.ScopeLocal, appSourceName, s3TestDir, 60)
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
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appList, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframeworks1, appframework: Deploy a Standalone instance with App Framework enabled and delete apps from init-apps when app download is complete", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload big-size app to S3 for Standalone
				* Create app source for Standalone
				* Prepare and deploy Standalone
				* When app download is complete, delete apps from init-apps
				############## VERIFICATIONS ################
				* Verify App installation is in progress on Standalone
				* Upload more apps from S3 during bigger app install
				* Wait for polling interval to pass
			    * Verify all apps are installed on Standalone
			*/

			// ################## SETUP FOR STANDALONE ####################
			// Download all test apps from S3
			appVersion := "V1"
			appList := testenv.BigSingleApp
			appFileList := testenv.GetAppFileList(appList)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to S3 for Standalone
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Standalone")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Standalone")
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
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Verify App Download is completed on Standalone
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), standalone.Kind, appSourceName, appFileList, enterpriseApi.AppPkgPodCopyComplete, enterpriseApi.AppPkgPodCopyPending)

			//Delete apps from init-apps when app download is complete
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			podDownloadPath := testenv.AppStagingLocOnPod + appSourceName + "/" + testenv.AppInfo[appList[0]]["filename"]
			err = testenv.DeleteFilesOnPod(ctx, deployment, testcaseEnvInst.GetName(), standalonePod[0], []string{podDownloadPath})
			Expect(err).To(Succeed(), "Unable to delete file on pod")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ############ VERIFICATION ###########
			standaloneAppSourceInfo := testenv.AppSourceInfo{CrKind: standalone.Kind, CrName: standalone.Name, CrAppSourceName: appSourceName, CrPod: standalonePod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appList, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{standaloneAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})
})
