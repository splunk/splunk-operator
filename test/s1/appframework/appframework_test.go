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
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	testenv "github.com/splunk/splunk-operator/test/testenv"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
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

		// Upload V1 apps to S3
		s3TestDir = "s1appfw-" + testenv.RandomDNSName(4)
		appFileList := testenv.GetAppFileList(appListV1, 1)
		uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
		Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
		uploadedApps = append(uploadedApps, uploadedFiles...)

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

	Context("appframework Standalone deployment (S1) with App Framework", func() {
		It("smoke, s1, appframework: can deploy a standalone instance with App Framework enabled", func() {

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}

			// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}

			// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
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
			standalone, err := deployment.DeployStandalonewithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for MC to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify Apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			appVersion := "V1"
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			// Verify Apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on Pod for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV1, true, true)

			// Verify Apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV1, true, "enabled", false, false)

			// Delete apps on S3 for new Apps
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing upgrade scenario")

			// Upload new Versioned Apps to S3
			appFileList = testenv.GetAppFileList(appListV2, 2)
			appVersion = "V2"
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify Apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			// Verify Apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on Pod for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV2, true, true)

			// Verify Apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV2, true, "enabled", true, false)

			// Scale Standalone instance
			testenvInstance.Log.Info("Scaling Up Standalone CR")
			scaledReplicaCount := 2
			standalone = &enterpriseApi.Standalone{}
			err = deployment.GetInstance(deployment.GetName(), standalone)
			Expect(err).To(Succeed(), "Failed to get instance of Standalone")

			standalone.Spec.Replicas = int32(scaledReplicaCount)

			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Failed to scale up Standalone")

			// Ensure standalone is scaling up
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseScalingUp)

			// Wait for Standalone to be in READY status
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseReady)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			podNames := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0), fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 1)}

			// Verify Apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify Apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on all Pods for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, true)

			// Verify Apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)

			// Delete apps on S3 for new Apps
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing downgrade scenario")

			// Upload new Versioned Apps to S3
			appFileList = testenv.GetAppFileList(appListV1, 1)
			appVersion = "V1"
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify Apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify Apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on Pod for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, true)

			// Verify Apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, false)

			// Delete apps on S3 for new Apps
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing upgrade scenario")

			// New List of apps: 1 new app to install, 2 apps to update, 1 app unchanged, 1 app removed from list
			customAppList := []string{appListV1[0], appListV2[2], appListV2[3], appListV2[4]}
			appFileListV1 := testenv.GetAppFileList(appListV1, 1)
			appFileListV2 := testenv.GetAppFileList(appListV2, 2)

			customAppFileList := []string{appFileListV1[0], appFileListV2[2], appFileListV2[3], appFileListV2[4]}
			customAppFileListV1 := []string{appFileListV1[0]}
			customAppFileListV2 := []string{appFileListV2[2], appFileListV2[3], appFileListV2[4]}

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, customAppFileListV1, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, customAppFileListV2, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify Apps are downloaded by init-container
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, customAppFileList, initContDownloadLocation)

			//Verify Apps are copied to location
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, customAppList, true, true)

			// Verify app with unchanged version is installed
			unchangedApp := []string{appListV1[0]}
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, unchangedApp, true, "enabled", false, false)

			// Verify apps with updated version are installed
			changedApps := []string{appListV2[2], appListV2[3], appListV2[4]}
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, changedApps, true, "enabled", true, false)
		})
	})

	// Removing test from Nightly and Smoke runs due to consistent failure.
	Context("appframework Standalone deployment (S1) with App Framework", func() {
		It("s1, integration, appframework: can deploy a Standalone and have ES app installed", func() {

			//Delete apps on S3 for new Apps
			testenvInstance.Log.Info("Delete Apps on S3 before upload ES")
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// ES is a huge file, we configure it here rather than in BeforeSuite/BeforeEach to save time for other tests
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

			standalone, err := deployment.DeployStandalonewithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone with App framework")

			// Ensure Standalone goes to Ready phase
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify Apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			// Verify apps are installed locally
			standalonePod := []string{fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)}
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), standalonePod, esApp, false, "enabled", false, false)
		})
	})

	Context("appframework Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, appframework: can deploy a standalone instance with App Framework enabled for manual poll", func() {

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}

			// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}

			// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 0,
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
			standalone, err := deployment.DeployStandalonewithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for Standalone to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify Apps are downloaded by init-container
			appVersion := "V1"
			initContDownloadLocation := "/init-apps/" + appSourceName
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appFileList, initContDownloadLocation)

			//Verify Apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on standalone(/etc/apps/) for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV1, true, true)

			//Verify Apps are installed
			testenvInstance.Log.Info("Verify Apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV1, true, "enabled", false, false)

			//Delete apps on S3 for new Apps
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			//Upload new Versioned Apps to S3
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2, 2)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			standalone.Spec.AppFrameworkConfig.AppsRepoPollInterval = int64(0)
			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with updated CR ")

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			//Verify Apps are not updated
			testenvInstance.Log.Info("Verify Apps are not auto installed on standalone by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV1, true, "enabled", false, false)

			testenvInstance.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(deployment, testenvInstance.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")
			testenvInstance.Log.Info("config map data for", "Standalone", config.Data["Standalone"])

			testenvInstance.Log.Info("Modify config map to trigger manual update")
			config.Data["Standalone"] = strings.Replace(config.Data["Standalone"], "off", "on", 1)
			err = deployment.UpdateCR(config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure standalone is updating
			testenv.VerifyStandalonePhase(deployment, testenvInstance, deployment.GetName(), splcommon.PhaseUpdating)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for Standalone to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			//Verify config map set back to off after poll trigger
			testenvInstance.Log.Info("Verify config map set back to off after poll trigger for app", "version", appVersion)
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(deployment, testenvInstance.GetName())
			Expect(strings.Contains(config.Data["Standalone"], "status: off")).To(Equal(true), "Config map update not complete")

			//Verify Apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location on standalone(/etc/apps/) for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV1, true, true)

			//Verify Apps are updated
			testenvInstance.Log.Info("Verify Apps are installed Locally on standalone by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{podName}, appListV2, true, "enabled", true, false)
		})
	})
})
