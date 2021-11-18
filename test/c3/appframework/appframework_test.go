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
package c3appfw

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	testenv "github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("c3appfw test", func() {

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

	Context("Single Site Indexer Cluster with SHC (C3) with App Framework", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA with App Framework enabled, install apps and upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload older versions of apps (V1) to S3
			   * Create 2 app sources for Monitoring Console and C3 SVA (CM and SHC Deployer)
			   * Prepare and deploy MC CRD with app framework and wait for pod to be ready
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ############### VERIFICATIONS ###############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed on MC and also on SH and Indexers pods
			   ############### UPGRADE APPS ################
			   * Upload newer versions of apps (V2) on S3
			   * Wait for MC and C3 pods to be ready
			   ############### VERIFICATIONS ###############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed and upgraded on MC and also on SH and indexers pods
			*/

			// Upload older versions of apps (V1) to S3 for MC
			appFileList := testenv.GetAppFileList(appListV1, 1)
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for MC")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload older versions of apps (V1) to S3 for C3
			s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for C3")
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
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

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

			// Deploy C3 CRD
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster with SHC")
			indexerReplicas := 3
			shReplicas := 3
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container on CM, Deployer
			appVersion := "V1"
			initContDownloadLocation := "/init-apps/" + appSourceName
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify V1 apps are downloaded by init container for apps for C3", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify apps are downloaded by init-container on MC
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			testenvInstance.Log.Info("Verify V1 apps are downloaded by init container for MC", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V1 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps are copied to location
			allPodNames := podNames
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)...)

			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			testenvInstance.Log.Info("Verify apps are copied to correct location for MC", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, false)

			// Verify apps are installed on C3
			testenvInstance.Log.Info("Verify apps are installed on C3 Indexers and SHs", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Verify apps are installed on MC
			testenvInstance.Log.Info("Verify apps are installed on MC", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, "enabled", false, false)

			// Delete apps on S3
			testenvInstance.Log.Info("Delete apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload newer version of apps (V2) to S3 for C3 and MC
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2, 2)
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for C3")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for MC")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify V2 apps are downloaded by init-container
			testenvInstance.Log.Info("Verify V2 apps are downloaded by init container for C3 for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify V2 apps are downloaded by init container for MC for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify bundle push status and compare bundle hash with previous V1 bundle hash
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify V2 apps are copied to correct location for C3 pods", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			testenvInstance.Log.Info("Verify V2 apps are copied to correct location for MC", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, false)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify V2 apps are NOT copied to /etc/apps on CM and Deployer", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, false)

			// Verify apps are updated on C3(cluster-wide)
			testenvInstance.Log.Info("Verify V2 apps are updated on the C3 pods", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Verify apps are updated on MC
			testenvInstance.Log.Info("Verify V2 apps are updated on MC", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, "enabled", true, false)
		})
	})

	Context("Single Site Indexer Cluster with SHC (C3) with App Framework", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA with App Framework enabled, install apps and downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload newer versions of apps (V2) to S3
			   * Create 2 App Sources for MC (Monitoring Console) and C3 SVA (CM and SHC Deployer)
			   * Prepare and deploy MC CRD with app framework and wait for pod to be ready
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ############### VERIFICATIONS ###############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed on MC and also on SH and Indexers pods
			   ############## DOWNGRADE APPS ###############
			   * Upload older versions of apps (V1) on S3
			   * Wait for MC and C3 to be READY
			   ############### VERIFICATIONS ###############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed and downgraded on MC and also on SH and Indexers pods
			*/

			// Upload V2 apps to S3 for C3
			appFileList := testenv.GetAppFileList(appListV2, 2)
			s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for C3")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for MC
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for MC")
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
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Create App framework Spec for C3
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

			// Deploy C3 CRD
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster with SHC")
			indexerReplicas := 3
			shReplicas := 3
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, mcName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container on CM, Deployer
			appVersion := "V2"
			initContDownloadLocation := "/init-apps/" + appSourceName
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify apps are downloaded by init container for C3", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify apps are downloaded by init-container on MC
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for MC", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V2 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps are copied to location
			allPodNames := podNames
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)...)

			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			testenvInstance.Log.Info("Verify apps are copied to correct location for MC", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, false)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, false)

			// Verify apps are installed on C3
			testenvInstance.Log.Info("Verify V2 apps are installed on C3 Indexers and SHs", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Verify apps are installed on MC
			testenvInstance.Log.Info("Verify V2 apps are installed on MC", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, "enabled", true, false)

			// Delete apps on S3
			testenvInstance.Log.Info("Delete apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload older version of apps (V1) to S3 for C3 and MC
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for C3")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for MC")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify apps are downloaded by init container for C3 for apps", " version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for MC for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify apps are copied to correct location for C3 pods", " version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			testenvInstance.Log.Info("Verify V2 apps are copied to correct location for MC", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, false)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app", " version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, false)

			// Verify apps are downgraded on C3(cluster-wide)
			testenvInstance.Log.Info("Verify apps are downgraded on the C3 pods", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Verify apps are downgraded on MC
			testenvInstance.Log.Info("Verify apps are downgraded on MC", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, "enabled", false, false)

		})
	})

	Context("Single Site Indexer Cluster with SHC (C3) with App Framework", func() {
		It("integration, c3, appframework: can deploy a C3 SVA with App Framework enabled, install apps,scale up IDXC and SHC, install app on new pods, scale down", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload apps on S3
			   * Create App Source for C3 SVA (CM and SHC Deployer)
			   * Prepare and deploy C3 CRD with app config and wait for pods to be ready
			   ################## VERIFICATIONS #############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed on SH,Indexers pods
			   ##########  SCALING UP ###########
			   * Scale up indexers and Search Heads
			   * Wait for C3 to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is sucessful
			   * Verify apps are copied and installed on new SH and Indexers pods
			   ############### SCALING DOWN ################
			   * Scale down indexers and Search Heads
			   * Wait for C3 to be ready
			   ############### VERIFICATION ##############
			   * Verify bundle push is sucessful
			   * Verify apps are still copied and installed on all SH and Indexers pods
			*/

			// Upload apps to S3 for C3
			appFileList := testenv.GetAppFileList(appListV1, 1)
			s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for C3")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
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

			// Deploy C3 CRD
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster")
			indexerReplicas := 3
			shReplicas := 3
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps are downloaded by init-container on CM, Deployer
			appVersion := "V1"
			initContDownloadLocation := "/init-apps/" + appSourceName
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to correct location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed on C3
			testenvInstance.Log.Info("Verify apps are installed on C3 Indexers and SHs", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale up SHC
			defaultSHReplicas := shc.Spec.Replicas
			scaledSHReplicas := defaultSHReplicas + 1
			testenvInstance.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of SHC
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Failed to scale Search Head Cluster")

			// Ensure SHC scales up and go to ScalingUp phase
			testenv.VerifySearchHeadClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp)

			// Get instance of current Indexer CR with latest config
			idxcName := deployment.GetName() + "-idxc"
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas := idxc.Spec.Replicas
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testenvInstance.Log.Info("Scaling up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Failed to scale Indxer Cluster")

			// Ensure Indexer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp, idxcName)

			// Ensure Indexer cluster go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Verify New Indexer On Cluster Manager
			indexerName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas-1)
			testenvInstance.Log.Info("Checking for New Indexer On Cluster Manager", "Indexer Name", indexerName)
			Expect(testenv.CheckIndexerOnCM(deployment, indexerName)).To(Equal(true))

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify bundle push status. Bundle hash not compared as scaleup does not involve new config
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledSHReplicas))

			// Verify apps are copied to location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed on C3
			testenvInstance.Log.Info("Verify apps are installed on C3 Indexers and SHs after scaling up", "version", appVersion)
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

			// Ensure Indexer cluster go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledSHReplicas))

			// Verify apps are copied to correct location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND after scaling down of indexers and SH", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer after scaling down of indexers and SH", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed on C3
			testenvInstance.Log.Info("Verify apps are installed on C3 Indexers and SHs after scaling down", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA and have apps installed locally on CM and SHC Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create App Source with local scope for C3 SVA (CM and SHC Deployer)
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ################## VERIFICATION #############
			   * Verify apps are installed locally on CM and Deployer
			   ############### UPGRADE APPS ################
			   * Upgrade apps in app sources
			   * Wait for pods to be ready
			   ############### VERIFICATIONS ###############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed and upgraded on CM and Deployer
			*/

			// Upload V1 apps to S3
			s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			// volumeSpec: Volume name, Endpoint, Path and SecretRef

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

			// Deploy C3 CRD
			indexerReplicas := 3
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster")
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appVersion := "V1"
			testenvInstance.Log.Info("Verify apps are downloaded by init container", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify apps are copied at the correct location on CM and on Deployer (/etc/apps/)
			testenvInstance.Log.Info("Verify apps are copied to correct location on CM and on Deployer (/etc/apps/)", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, false)

			// Verify apps are installed locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify apps are installed locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, "enabled", false, false)

			// Verify apps are not copied in the apps folder on CM and /etc/shcluster/ on Deployer (therefore not installed on peers and on SH)
			testenvInstance.Log.Info("Verify apps are NOT copied to "+splcommon.ManagerAppsLoc+" on CM and "+splcommon.SHCluster+" on Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, true)

			// Delete apps on S3
			testenvInstance.Log.Info("Delete apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing upgrade scenario")

			// Upload newer version of apps (V2) to S3
			appVersion = "V2"
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)
			appFileList = testenv.GetAppFileList(appListV2, 2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify apps are copied at the correct location on CM and on Deployer (/etc/apps/)
			testenvInstance.Log.Info("Verify apps are copied to correct location on CM and on Deployer (/etc/apps/) for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, false)

			// Verify apps are installed locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)

			// Verify apps are not copied in the apps folder on CM and /etc/shcluster/ on Deployer (therefore not installed on peers and on SH)
			testenvInstance.Log.Info("Verify apps are NOT copied to "+splcommon.ManagerAppsLoc+" on CM and "+splcommon.SHCluster+" on Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3, appframework: can deploy a C3 SVA and have ES app installed on SHC", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload ES app to S3
			   * Create App Source with 'ScopeClusterWithPreConfig' scope for C3 SVA
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ################## VERIFICATION #############
			   * Verify ES app is installed on Deployer and on Search Heads
			*/

			// Create local directory for file download
			s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)

			// Upload ES app to S3
			esApp := []string{"SplunkEnterpriseSecuritySuite"}
			appFileList := testenv.GetAppFileList(esApp, 1)

			// Download ES app from S3
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
				Scope:   enterpriseApi.ScopeClusterWithPreConfig,
			}
			appSourceName := "appframework-" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			// Deploy C3 SVA
			// Deploy the Cluster manager
			testenvInstance.Log.Info("Deploy Cluster manager in single site configuration")
			_, err = deployment.DeployClusterMaster(deployment.GetName(), "", "", "")
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy the Indexer cluster
			testenvInstance.Log.Info("Deploy Indexer Cluster in single site configuration")
			indexerReplicas := 3
			_, err = deployment.DeployIndexerCluster(deployment.GetName()+"-idxc", deployment.GetName(), indexerReplicas, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster")

			// Deploy the SHC
			shSpec := enterpriseApi.SearchHeadClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "Always",
					},
					ExtraEnv: []corev1.EnvVar{
						{
							Name:  "SPLUNK_ES_SSL_ENABLEMENT",
							Value: "ignore"},
					},
					Volumes: []corev1.Volume{},
					ClusterMasterRef: corev1.ObjectReference{
						Name: deployment.GetName(),
					},
				},
				Replicas:           3,
				AppFrameworkConfig: appFrameworkSpec,
			}
			_, err = deployment.DeploySearchHeadClusterWithGivenSpec(deployment.GetName()+"-shc", shSpec)
			Expect(err).To(Succeed(), "Unable to deploy SHC with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify ES is downloaded by init-container
			testenvInstance.Log.Info("Verfiy ES app is downloaded by init container on deployer pod")
			initContDownloadLocation := "/init-apps/" + appSourceName
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), deployerPod, appFileList, initContDownloadLocation)

			// Verify ES app is installed locally on SHC Deployer
			testenvInstance.Log.Info("Verfiy ES app is installed locally on deployer pod")
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), deployerPod, esApp, true, "disabled", false, false)

			// Verify bundle push status
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(shSpec.Replicas))

			// Verify apps are installed on SHs
			testenvInstance.Log.Info("Verfiy ES app is installed on Search Heads")
			podNames := []string{}
			for i := 0; i < int(shSpec.Replicas); i++ {
				sh := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				podNames = append(podNames, string(sh))
			}
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, esApp, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration, appframework: can deploy a C3 SVA with apps installed locally on CM and Deployer, cluster-wide on Peers and SHs, then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create App framework spec with app sources having both local and cluster scopes for C3 SVA
			   * Prepare and deploy C3 CRD and wait for pods to be ready
			   ################## VERIFICATION #############
			   * Verify apps with local scope are installed locally on Cluster manager and SHC deployer
			   * Verify apps with cluster scope are installed cluster-wide on indexers and Search Heads
			   ############### UPGRADE APPS ################
			   * Upload newer versions of apps (V2) on S3
			   * Wait for pods to be ready
			   ############### VERIFICATION ################
			   * Verify bundle push is successful
			   * Verify apps with local scope are upgraded locally on Cluster manager and on SHC deployer
			   * Verify apps with cluster scope are upgraded cluster-wide on indexers and Search Heads
			*/

			// Create directory for file download
			s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)

			// Split Applist into 2 lists for local and cluster install
			appListLocal := appListV1[len(appListV1)/2:]
			appListCluster := appListV1[:len(appListV1)/2]

			// Upload appListLocal to S3
			appFileList := testenv.GetAppFileList(appListLocal, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to a 2nd directory on S3 bucket as we need 2 buckets locations for this test.(appListLocal to be used for local install, appListCluster for cluster install)
			s3TestDirCluster := "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster, 1)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceLocalSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceClusterSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceNameLocal := "appframework-localapps-" + testenv.RandomDNSName(3)
			appSourceSpecLocal := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameLocal, s3TestDir, appSourceLocalSpec)}
			appSourceNameCluster := "appframework-clusterapps-" + testenv.RandomDNSName(3)
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameCluster, s3TestDirCluster, appSourceClusterSpec)}

			appSourceSpec := append(appSourceSpecLocal, appSourceSpecCluster...)

			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceLocalSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			// Create Single site Cluster and SHC, with App Framework enabled on CM and SHC Deployer
			testenvInstance.Log.Info("Create Single site Indexer Cluster with Local and Cluster Install for apps")
			indexerReplicas := 3
			shReplicas := 3
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps with local scope are downloaded by init-container
			appVersion := "V1"
			initContDownloadLocation := "/init-apps/" + appSourceNameLocal
			downloadPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList = testenv.GetAppFileList(appListLocal, 1)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify apps with cluster scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameCluster
			appFileList = testenv.GetAppFileList(appListCluster, 1)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V1 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps with local scope are installed locally on CM and on SHC Deployer
			localPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", false, false)

			// Verify apps with cluster scope are installed on indexers
			clusterPodNames := []string{}
			for i := 0; i < int(indexerReplicas); i++ {
				sh := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				clusterPodNames = append(clusterPodNames, string(sh))
			}

			for i := 0; i < int(shReplicas); i++ {
				sh := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				clusterPodNames = append(clusterPodNames, string(sh))
			}
			testenvInstance.Log.Info("Verify apps are installed clusterwide on indexers and search-heads by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", false, true)

			// Delete apps on S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload newer version of apps to S3
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListLocal, 2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to a 2nd directory on S3 bucket as we need 2 buckets locations for this test.(appListLocal apps to be used for local install, appListCluster apps for cluster install)
			clusterappFileList = testenv.GetAppFileList(appListCluster, 2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps with local scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameLocal
			appFileList = testenv.GetAppFileList(appListLocal, 2)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify apps with cluster scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameCluster
			appFileList = testenv.GetAppFileList(appListCluster, 2)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps with local scope are upgraded locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify apps are upgraded Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", true, false)

			// Verify apps with cluster scope are upgraded on indexers
			testenvInstance.Log.Info("Verify apps are upgraded clusterwide on indexers and search-heads by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", true, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration, appframework: can deploy a C3 SVA with apps installed locally on CM and Deployer, cluster-wide on Peers and SHs, then downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create App framework spec with app sources having both local and cluster scopes for C3 SVA
			   * Prepare and deploy C3 CRD and wait for pods to be ready
			   ################## VERIFICATION #############
			   * Verify apps with local scope are installed locally on Cluster manager and SHC deployer
			   * Verify apps with cluster scope are installed cluster-wide on indexers and Search Heads
			   ############### DOWNGRADE APPS ##############
			   * Upload older versions of apps (V1) on S3
			   * Wait for pods to be ready
			   ############### VERIFICATION ################
			   * Verify bundle push is successful
			   * Verify apps with local scope are downgraded locally on Cluster manager and on SHC deployer
			   * Verify apps with cluster scope are downgraded cluster-wide on indexers and Search Heads
			*/

			// Delete apps on S3 for new apps to split them across both cluster and local
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Split Applist into 2 lists for local and cluster install
			appListLocal := appListV2[len(appListV2)/2:]
			appListCluster := appListV2[:len(appListV2)/2]

			// Upload appListLocal to S3
			appFileList := testenv.GetAppFileList(appListLocal, 2)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to a 2nd directory on S3 bucket as we need 2 buckets locations for this test.(appListLocal to be used for local install, appListCluster for cluster install)
			s3TestDirCluster := "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster, 2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appSourceLocalSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}
			appSourceClusterSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceNameLocal := "appframework-localapps-" + testenv.RandomDNSName(3)
			appSourceSpecLocal := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameLocal, s3TestDir, appSourceLocalSpec)}
			appSourceNameCluster := "appframework-clusterapps-" + testenv.RandomDNSName(3)
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameCluster, s3TestDirCluster, appSourceClusterSpec)}

			appSourceSpec := append(appSourceSpecLocal, appSourceSpecCluster...)

			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceLocalSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			// Create Single site Cluster and SHC, with App Framework enabled on CM and SHC Deployer
			testenvInstance.Log.Info("Create Single site Indexer Cluster with Local and Cluster scope for apps")
			indexerReplicas := 3
			shReplicas := 3
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps with local scope are downloaded by init-container
			appVersion := "V2"
			initContDownloadLocation := "/init-apps/" + appSourceNameLocal
			downloadPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList = testenv.GetAppFileList(appListLocal, 2)
			testenvInstance.Log.Info("Verify apps are downloaded by init container", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify apps with cluster scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameCluster
			appFileList = testenv.GetAppFileList(appListCluster, 2)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V2 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps with local scope are installed locally on CM and on SHC Deployer
			localPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", true, false)

			// Verify apps with cluster scope are installed on indexers
			clusterPodNames := []string{}
			for i := 0; i < int(indexerReplicas); i++ {
				sh := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				clusterPodNames = append(clusterPodNames, string(sh))
			}

			for i := 0; i < int(shReplicas); i++ {
				sh := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				clusterPodNames = append(clusterPodNames, string(sh))
			}
			testenvInstance.Log.Info("Verify apps are installed clusterwide on indexers and search-heads by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", true, true)

			// Delete apps on S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Redefine app lists as LDAP app isn't in V1 apps
			appListLocal = appListV1[len(appListV1)/2:]
			appListCluster = appListV1[:len(appListV1)/2]

			// Upload older version of apps to S3
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListLocal, 1)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to a 2nd directory on S3 bucket as we need 2 buckets locations for this test.(appListLocal apps to be used for local install, appListCluster apps for cluster install)
			clusterappFileList = testenv.GetAppFileList(appListCluster, 1)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps with local scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameLocal
			appFileList = testenv.GetAppFileList(appListLocal, 1)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify apps with cluster scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameCluster
			appFileList = testenv.GetAppFileList(appListCluster, 1)
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps with local scope are downgraded locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify apps are downgraded locally on CM and Deployer", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", false, false)

			// Verify apps with cluster scope are downgraded on indexers
			testenvInstance.Log.Info("Verify apps are downgraded cluster-wide on indexers and search-heads", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3, appframework: can deploy a C3 SVA instance with App Framework enabled and install above 200MB of apps at once", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create App Source for C3 SVA (CM and SHC Deployer)
			   * Add more apps than usual on S3 for this test
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ############### VERIFICATIONS ###############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed on SH and Indexers pods
			*/

			// Create directory for app file download
			s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)

			// Creating a bigger list of apps to be installed than the default one
			appList := []string{"splunk_app_db_connect", "splunk_app_aws", "Splunk_TA_microsoft-cloudservices", "Splunk_ML_Toolkit", "Splunk_Security_Essentials"}
			appFileList := testenv.GetAppFileList(appList, 1)

			// Download App from S3
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Create consolidated list of app files
			appList = append(appListV1, appList...)
			appFileList = testenv.GetAppFileList(appList, 1)

			// Upload app to S3
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
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

			// Create Single site Cluster and SHC, with App Framework enabled on CM and SHC Deployer
			testenvInstance.Log.Info("Create Single site Indexer Cluster and SHC with App framework")
			indexerReplicas := 3
			shReplicas := 3
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND")
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer")
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Get indexers and SH pod names
			podNames := []string{}
			podNames = append(podNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			podNames = append(podNames, testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)...)

			// Verify apps are installed on indexers and SH
			testenvInstance.Log.Info("Verify apps are installed on the Indexers and Search Heads")
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, true)
		})
	})
})
