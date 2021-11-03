// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	testenv "github.com/splunk/splunk-operator/test/testenv"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("s1appfw test", func() {

	var deployment *testenv.Deployment
	var s3TestDir string
	var uploadedApps []string

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testenvInstance.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
		// Delete files uploaded to S3
		if !testenvInstance.SkipTeardown {
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
		}
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframework: can deploy a standalone instance with App Framework enabled, install apps and upgrade them", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload older versions of apps (V1) to S3
				* Create 2 app sources for Monitoring Console and Standalone
			    * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			    * Prepare and deploy Standalone with app framework and wait for the pod to be ready
				############## VERIFICATION #################
				* Verify apps are copied and installed on Monitoring Console and on Standalone
				############### UPGRADE APPS ################
				* Upload newer versions of apps (V2) on S3
			    * Wait for Monitoring Console and Standalone pods to be ready
				############## VERIFICATIONS ################
				* Verify apps are copied, installed and upgraded on Monitoring Console and on standalone
			*/

			// Upload older versions of apps (V1) to S3
			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework spec for Monitoring Console
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			volumeSpecMC := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeNameMC, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpecMC := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeNameMC,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceNameMC := "appframework-mc-" + testenv.RandomDNSName(3)
			appSourceSpecMC := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameMC, s3TestDirMC, appSourceDefaultSpecMC)}
			appFrameworkSpecMC := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpecMC,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpecMC,
				AppSources:           appSourceSpecMC,
			}
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
			testenvInstance.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(testenvInstance.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Upload V1 apps to S3
			s3TestDir = "s1appfw-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Standalone
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}
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
			standalone, err := deployment.DeployStandaloneWithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify apps are downloaded by init-container on Standalone Pod and MC
			appVersion := "V1"
			initContDownloadLocationStandalonePod := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			appFileList = testenv.GetAppFileList(appListV1, 1)
			podNames := []string{standalonePodName, mcPodName}
			testenvInstance.Log.Info("Verify V1 apps are downloaded by init container for Standalone", "POD", standalonePodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testenvInstance.Log.Info("Verify V1 apps are downloaded by init container for Monitoring Console", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify V1 apps are copied to location
			testenvInstance.Log.Info("Verify V1 apps are copied to correct location on Pod for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, true)

			// Verify V1 apps are installed
			testenvInstance.Log.Info("Verify V1 apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, false)

			// Delete apps on S3
			testenvInstance.Log.Info("Delete apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload newer version of apps (V2) to S3 for Standalone and Monitoring Console
			appFileList = testenv.GetAppFileList(appListV2, 2)
			appVersion = "V2"
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)

			// Upload Apps to S3 for MC
			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			appFileList = testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for Standalone to be in UPDATING status
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhasePending)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify V2 apps are downloaded by init-container
			testenvInstance.Log.Info("Verify V2 apps are downloaded by init container for apps", "POD", standalonePodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testenvInstance.Log.Info("Verify V2 apps are downloaded by init container for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify V2 apps are copied to location
			testenvInstance.Log.Info("Verify V2 apps are copied to correct location on Pod for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, true)

			// Verify V2 apps are installed
			testenvInstance.Log.Info("Verify apps are updated on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframework: can deploy a standalone instance with App Framework enabled, install apps and downgrade them", func() {

			/* Test Steps
				################## SETUP ####################
				* Upload newer versions of apps (V2) to S3
				* Create 2 app sources for Monitoring Console and Standalone
			    * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			    * Prepare and deploy Standalone with app framework and wait for the pod to be ready
				############## VERIFICATION #################
				* Verify apps are copied and installed on Monitoring Console and on Standalone
				############# DOWNGRADE APPS ################
				* Upload older versions of apps (V1) on S3
			    * Wait for Monitoring Console and Standalone pods to be ready
				############## VERIFICATION #################
				* Verify apps are copied, installed and downgraded on Monitoring Console and also on standalone
			*/

			// Delete pre-installed apps on S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload newer versions of apps (V2) to S3 for MC
			appVersion := "V2"
			s3TestDir = "s1appfw-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV2, 2)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for C3")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			s3TestDirMC := "s1appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for Monitoring Console")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			volumeSpecMC := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeNameMC, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpecMC := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeNameMC,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceNameMC := "appframework-mc-" + testenv.RandomDNSName(3)
			appSourceSpecMC := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameMC, s3TestDirMC, appSourceDefaultSpecMC)}
			appFrameworkSpecMC := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpecMC,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpecMC,
				AppSources:           appSourceSpecMC,
			}
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
			testenvInstance.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(testenvInstance.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Create App framework Spec for Standalone
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}
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
			standalone, err := deployment.DeployStandaloneWithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify apps are downloaded by init-container on Standalone and Monitoring Console
			initContDownloadLocationStandalonePod := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			appFileList = testenv.GetAppFileList(appListV2, 2)
			podNames := []string{standalonePodName, mcPodName}
			testenvInstance.Log.Info("Verify V2 apps are downloaded by init container for Standalone", "POD", standalonePodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testenvInstance.Log.Info("Verify V2 apps are downloaded by init container for Monitoring Console", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify V2 apps are copied to location
			testenvInstance.Log.Info("Verify V2 apps are copied to correct location on Pod for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, false)

			// Verify V2 apps are installed
			testenvInstance.Log.Info("Verify V2 apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)

			// Delete apps on S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload older version of apps (V1) to S3 for Standalone and Monitoring Console
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for Standalone to be in UPDATING status
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhasePending)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify V1 apps are downloaded by init container for apps", "POD", standalonePodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testenvInstance.Log.Info("Verify V1 apps are downloaded by init container for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify V1 apps are copied to correct location on Pod for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, false)

			// Verify apps are downgraded
			testenvInstance.Log.Info("Verify V1 apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, false)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframework: can deploy a Standalone instance with App Framework enabled, install apps, scale up, install apps on new pod, scale down", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload apps on S3
			   * Create 2 app sources for Monitoring Console and Standalone
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Prepare and deploy Standalone CRD with app framework and wait for the pod to be ready
			    ################# VERIFICATION ###############
			   * Verify apps are copied and installed on Monitoring Console and Standalone
			   ################## SCALING UP ################
			   * Scale up Standalone
			   * Wait for Monitoring Console and  Standalone to be ready
			   ################# VERIFICATION ###############
			   * Verify apps are copied and installed on new Standalone pod
			   ################ SCALING DOWN ################
			   * Scale down Standalone
			   * Wait for Monitoring Console and Standalone to be ready
			    ################# VERIFICATION ###############
			   * Verify apps are still copied and installed on Standalone
			*/

			// Upload apps to S3 for Monitoring Console
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			volumeSpecMC := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeNameMC, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpecMC := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeNameMC,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceNameMC := "appframework-mc-" + testenv.RandomDNSName(3)
			appSourceSpecMC := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameMC, s3TestDirMC, appSourceDefaultSpecMC)}
			appFrameworkSpecMC := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpecMC,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpecMC,
				AppSources:           appSourceSpecMC,
			}
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
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(testenvInstance.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Create App framework Spec for Standalone
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}
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
			standalone, err := deployment.DeployStandaloneWithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify apps are downloaded by init-container on Standalone Pod and Monitoring Console
			appVersion := "V1"
			initContDownloadLocationStandalonePod := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			appFileList = testenv.GetAppFileList(appListV1, 1)
			podNames := []string{standalonePodName, mcPodName}
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for Standalone", "POD", standalonePodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for Monitoring Console", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on Pod for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, true)

			// Verify apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, false)

			// Scale up Standalone instance
			testenvInstance.Log.Info("Scaling Up Standalone CR")
			scaledReplicaCount := 2
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Failed to scale up Standalone")

			// Ensure Standalone is scaling up
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseReady)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			podNames = append(podNames, fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 1))

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container on Standalone after scaling up", "POD", standalonePodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container on Monitoring Console after scaling up", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on all Pods after scaling up", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, true)

			// Verify apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods after scaling up", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, false)

			// Scale down Standalone instance
			testenvInstance.Log.Info("Scaling down Standalone CR")
			scaledReplicaCount = 1
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)
			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Failed to scale down Standalone")

			// Ensure standalone is scaling down
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseScalingDown)

			// Wait for Standalone to be in READY status
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseReady)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			podNames = testenv.DumpGetPods(testenvInstance.GetName())

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for Standalone after scaling down ", "POD", standalonePodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{standalonePodName}, appFileList, initContDownloadLocationStandalonePod)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for Monitoring Console after scaling down ", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on all Pods after scaling down ", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, false)

			// Verify apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods after scaling down ", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, false)

		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("s1, integration, appframework: can deploy a Standalone and have ES app installed", func() {

			// Create local app Directory
			s3TestDir = "s1appfw-" + testenv.RandomDNSName(4)

			// Upload ES app to S3
			esApp := []string{"SplunkEnterpriseSecuritySuite"}
			appFileList := testenv.GetAppFileList(esApp, 1)

			// Download ES App from S3
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download ES app file")

			// Upload ES app to S3
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload ES app to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceName := "appframework-" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			// Create Standalone spec with App Framework enabled and some extra config to have ES installed correctly
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			standalone, err := deployment.DeployStandaloneWithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone with App framework")

			// Ensure Standalone goes to Ready phase
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			// Verify apps are installed locally
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), standalonePod, esApp, false, "enabled", false, false)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframework: can deploy a standalone instance with App Framework enabled and install around 350MB of apps at once", func() {

			// Create local app Directory
			s3TestDir = "s1appfw-" + testenv.RandomDNSName(4)

			// Creating a bigger list of apps to be installed than the default one
			appList := append(appListV1, testenv.RestartNeededApps...)

			// Download apps from S3
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, testenv.GetAppFileList(testenv.RestartNeededApps, 1))
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload apps to S3
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, testenv.GetAppFileList(appList, 1), downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}

			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}

			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Create Standalone Deployment with App Framework
			standalone, err := deployment.DeployStandaloneWithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			// Verify apps are copied to location
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV1, true, true)

			// Verify apps are installed
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV1, true, "enabled", false, false)
		})
	})
})
