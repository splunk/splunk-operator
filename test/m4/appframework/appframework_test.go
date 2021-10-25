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
package m4appfw

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

var _ = Describe("m4appfw test", func() {

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

	Context("Multi Site Indexer Cluster with SHC (m4) with App Framework", func() {
		It("integration, m4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and upgrade them", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Create 2 App Sources for MC (Monitoring Console) and M4 SVA (CM and SHC Deployer)
			   * Prepare and deploy MC CRD with app framework and wait for pod to be ready
			   * Prepare and deploy M4 CRD with app framework and wait for pods to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is successful
			   * Verify apps are copied and installed on MC and on SH and Indexers pods
			   ############### UPGRADE APPS ##############
			   * Upgrade apps in app sources
			   * Wait for MC and M4 pod to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is successful
			   * Verify apps are copied and upgraded on MC and on SH and Indexers pods
			*/

			// Upload apps to S3 for MC
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps for MC to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for MC
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

			// Deploy MC CRD
			testenvInstance.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(testenvInstance.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify MC is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Upload V1 apps to S3
			s3TestDir = "m4appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			// Deploy M4 CRD
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster with SHC")
			siteCount := 3
			indexersPerSite := 1
			err = deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpec, true, 10, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster and SHC with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container on CM, Deployer and MC
			appVersion := "V1"
			initContDownloadLocation := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList = testenv.GetAppFileList(appListV1, 1)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container on MC for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			shReplicas := int(shc.Spec.Replicas)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)
			// Saving current V1 bundle hash for future comparision
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed on MC and M4(cluster-wide)
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Delete apps on S3
			testenvInstance.Log.Info("Delete Apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing upgrade scenario")

			// Upload newer version of apps to S3 for M4 and MC
			appFileList = testenv.GetAppFileList(appListV2, 2)
			appVersion = "V2"
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload newer version of apps for M4 to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload newer version of apps for MC to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify apps are downloaded by init container for M4 for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for MC for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify bundle push status and compare bundle hash with previous V1 bundle
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV2, false, false)

			// Verify apps are updated on MC and M4(cluster-wide)
			testenvInstance.Log.Info("Verify apps are updated on the MC and M4 pods for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)
		})
	})

	Context("Multi Site Indexer Cluster with SHC (m4) with App Framework", func() {
		It("integration, m4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and downgrade them", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Create 2 App Sources for MC and M4 SVA (CM and SHC Deployer)
			   * Prepare and deploy MC CRD with app config and wait for pod to be ready
			   * Prepare and deploy M4 CRD with app config and wait for pods to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is successful
			   * Verify apps are copied and installed on MC and on SH and Indexers pods
			   ############### DOWNGRADE APPS ############
			   * Downgrade apps in app sources
			   * Wait for MC and M4 to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is successful
			   * Verify apps are copied and downgraded on MC and on SH and Indexers pods
			*/

			// Upload newer version of apps to S3
			s3TestDir = "m4appfw-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV2, 2)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload newer version of apps for M4 to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload newer version of apps for MC to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for MC
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

			// Deploy MC CRD
			testenvInstance.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(testenvInstance.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify MC is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Create App framework Spec for M4
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			// Deploy M4 CRD
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster with SHC")
			siteCount := 3
			indexersPerSite := 1
			err = deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpec, true, 10, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster and SHC with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container on CM, Deployer and MC
			appVersion := "V2"
			initContDownloadLocation := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList = testenv.GetAppFileList(appListV2, 2)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container on MC pod for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			shReplicas := int(shc.Spec.Replicas)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)
			// Saving current V1 bundle hash for future comparision
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV2, false, false)

			// Verify apps are installed on MC and M4(cluster-wide)
			testenvInstance.Log.Info("Verify newer version of apps are installed on the pods", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Delete apps on S3
			testenvInstance.Log.Info("Delete Apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload older version of apps to S3 for M4 and MC
			appFileList = testenv.GetAppFileList(appListV1, 1)
			appVersion = "V1"
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload older version of apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload older version of apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", " version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container on MC pod for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app", " version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app", " version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are downgraded on MC and M4(cluster-wide)
			testenvInstance.Log.Info("Verify apps are downgraded on the pods by running Splunk CLI commands for app", " version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)
		})
	})

	XContext("Multi Site Indexer Cluster with SHC (m4) with App Framework", func() {
		It("integration, m4, appframework: can deploy a M4 SVA with App Framework enabled, install apps, scale up IDXC and SHC, install app on new pods", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Create 2 App Sources for MC and M4 SVA (CM and SHC Deployer)
			   * Prepare and deploy MC CRD with app config and wait for pod to be ready
			   * Prepare and deploy M4 CRD with app config and wait for pods to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is sucessful
			   * Verify apps are copied and installed on MC and also on SH and Indexers pods
			   ############### SCALING UP ################
			   * Scale up indexers and SHC
			   * Wait for MC and M4 to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is sucessful
			   * Verify apps are copied and installed on new SH and Indexers pods
			   ############### SCALING DOWN ################
			   * Scale down indexers and SHC
			   * Wait for MC and M4 to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is sucessful
			   * Verify apps are still copied and installed on all SH and Indexers pods
			*/

			// Upload apps to S3 for MC
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for MC
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

			// Deploy MC CRD
			testenvInstance.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(testenvInstance.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify MC is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Upload V1 apps to S3
			s3TestDir = "m4appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceName := "appframework" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			// Deploy M4 CRD
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster with SHC")
			siteCount := 3
			indexersPerSite := 1
			err = deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpec, true, 10, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container on CM, Deployer and MC
			appVersion := "V1"
			initContDownloadLocation := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList = testenv.GetAppFileList(appListV1, 1)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container on MC for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			shReplicas := int(shc.Spec.Replicas)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to correct location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed on MC and M4(cluster-wide)
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Get instance of current SHC CR with latest config
			shc = &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale up SHC
			defaultSHReplicas := shc.Spec.Replicas
			scaledSHReplicas := defaultSHReplicas + 1
			testenvInstance.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of SHC
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Failed to scale up Search Head Cluster")

			// Ensure SHC scales up and go to ScalingUp phase
			testenv.VerifySearchHeadClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp)

			// Get instance of current Indexer CR with latest config
			idxcName := deployment.GetName() + "-" + "site1"
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas := idxc.Spec.Replicas
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testenvInstance.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Failed to Scale Up Indexer Cluster")

			// Ensure Indexer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp, idxcName)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount)

			// Verify Apps are copied to correct location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND after scaling up of indexers and SH", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer after scaling up of indexers and SH", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed cluster-wide
			testenvInstance.Log.Info("Verify apps are installed on the pods by running Splunk CLI commands after scaling up of indexers and SH", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Get instance of current SHC CR with latest config
			shc = &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale down SHC
			defaultSHReplicas = shc.Spec.Replicas
			scaledSHReplicas = defaultSHReplicas - 1
			testenvInstance.Log.Info("Scaling down Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of SHC
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Failed to scale down Search Head Cluster")

			// Ensure SHC scales down and go to ScalingDown phase
			testenv.VerifySearchHeadClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingDown)

			// Get instance of current Indexer CR with latest config
			idxcName = deployment.GetName() + "-" + "site1"
			idxc = &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas = idxc.Spec.Replicas
			scaledIndexerReplicas = defaultIndexerReplicas - 1
			testenvInstance.Log.Info("Scaling down Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Failed to Scale down Indexer Cluster")

			// Ensure Indexer cluster scales down and go to ScalingDown phase
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingDown, idxcName)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount)

			// Verify apps are copied to correct location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND after scaling down of indexers and SH", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer after scaling down of indexers and SH", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed cluster-wide
			testenvInstance.Log.Info("Verify apps are installed on the pods by running Splunk CLI commands after scaling down of indexers and SH", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (M4 - clustered indexer, search head cluster)", func() {
		It("integration, m4, appframework: can deploy a M4 SVA and have apps installed locally on CM and SHC Deployer", func() {

			// Upload V1 apps to S3
			s3TestDir = "m4appfw-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			// volumeSpec: Volume name, Endpoint, Path and SecretRef
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}

			// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
			appSourceName := "appframework-" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}

			// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			// Create Multi site Cluster and SHC, with App Framework enabled on CM and SHC Deployer
			siteCount := 3
			indexersPerSite := 1
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster")
			err = deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpec, true, 10, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify Apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appVersion := "V1"
			testenvInstance.Log.Info("Verify Apps are downloaded by init container local install on CM and Deployer for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify apps are copied at the correct location on CM and on Deployer (/etc/apps/)
			testenvInstance.Log.Info("Verify Apps are copied to /etc/apps on CM and Deployer for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, false)

			// Verify apps are installed locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify Apps are installed locally on CM and Deployer for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, "enabled", false, false)

			// Verify apps are not copied in the apps folder on CM and /etc/shcluster/ on Deployer (therefore not installed on peers and on SH)
			testenvInstance.Log.Info("Verify Apps are not copied to "+splcommon.ManagerAppsLoc+" on CM and "+splcommon.SHCluster+" on Deployer for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, true)

			//Delete apps on S3 for new Apps
			testenvInstance.Log.Info("Delete Apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			//Upload new Versioned Apps to S3
			appFileList = testenv.GetAppFileList(appListV2, 2)
			appVersion = "V2"
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify Apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container local install on CM and Deployer for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify apps are copied at the correct location on CM and on Deployer (/etc/apps/)
			testenvInstance.Log.Info("Verify Apps are copied to /etc/apps on CM and Deployer for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, false)

			// Verify apps are installed locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify Apps are installed locally on CM and Deployer for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)

			// Verify apps are not copied in the apps folder on CM and /etc/shcluster/ on Deployer (therefore not installed on peers and on SH)
			testenvInstance.Log.Info("Verify Apps are not copied to "+splcommon.ManagerAppsLoc+" on CM and "+splcommon.SHCluster+" on Deployer for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, true)
		})
	})
})
