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

		// Upload V1 apps to S3
		s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)
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

	Context("Single Site Indexer Cluster with SHC (C3) with App Framework", func() {
		It("integration, c3, appframework: can deploy a C3 SVA with App Framework enabled, install apps and upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create 2 App Sources for MC and C3 SVA (CM and SHC Deployer)
			   * Prepare C3 CRD with MC and App config
			   * Apply C3 CRD and wait for pod to be ready
			   * Prepare MC CRD with App Config
			   * Wait for MC to be READY
			   ################## VERIFICATIONS #############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed on MC AND SH,Indexers pods
			   ############ UPGRADE APPS #############################
			   * Upgrade apps in app sources
			   * Wait for MC and C3 are READY
			   * Verify bundle push is successful
			   * Verify apps are copied, installed and upgraded on MC AND SH,Indexers pods
			*/

			// Upload Apps to S3 for MC
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for MC
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			volumeSpecMC := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeNameMC, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
			appSourceDefaultSpecMC := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeNameMC,
				Scope:   enterpriseApi.ScopeLocal,
			}

			// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
			appSourceNameMC := "appframework-mc-" + testenv.RandomDNSName(3)
			appSourceSpecMC := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameMC, s3TestDirMC, appSourceDefaultSpecMC)}

			// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
			appFrameworkSpecMC := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpecMC,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpecMC,
				AppSources:           appSourceSpecMC,
			}

			// MC AppFramework Spec
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
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(testenvInstance.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Create App framework Spec for C3
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeCluster,
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

			indexerReplicas := 3

			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster")
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, 10, true)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify Apps are downloaded by init-container on CM, Deployer and MC
			initContDownloadLocation := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList = testenv.GetAppFileList(appListV1, 1)
			appVersion := "V1"
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			shReplicas := int(shc.Spec.Replicas)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)
			// Saving current V1 bundle hash for future comparision
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify Apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify Apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Delete apps on S3 for new Apps
			testenvInstance.Log.Info("Delete Apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing upgrade scenario")

			// Upload new Versioned Apps to S3
			appFileList = testenv.GetAppFileList(appListV2, 2)
			appVersion = "V2"
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify Apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Verify bundle push status and compare bundle hash with previous v1 bundle hash
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify Apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV2, false, false)

			// Verify Apps are updated
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)
		})
	})

	Context("Single Site Indexer Cluster with SHC (C3) with App Framework", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA with App Framework enabled, install apps and downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create 2 App Sources for MC and C3 SVA (CM and SHC Deployer)
			   * Prepare C3 CRD with MC and App config
			   * Apply C3 CRD and wait for pod to be ready
			   * Prepare MC CRD with App Config
			   * Wait for MC to be READY
			   ################## VERIFICATIONS #############
			   * Verify bundle push is successful
			   * Verify apps are copied, installed on MC AND SH,Indexers pods
			   ##########  SCALE UP SHC and Indexers ###########
			   * Wait for SHC, indexers and MC to be ready
			   * Verify apps are copied, installed on MC AND SH,Indexers pods
			   ############ DOWNGRADE APPS #############################
			   * Downgrade apps in app sources
			   * Wait for MC and C3 to be READY
			   * Verify bundle push is successful
			   * Verify apps are copied, installed and downgraded on MC AND SH,Indexers pods
			*/

			// Delete pre-installed apps on S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing downgrade scenario")

			// Upload newer version of apps to S3
			s3TestDir = "c3appfw-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV2, 2)
			appVersion := "V2"
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for MC
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			volumeSpecMC := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeNameMC, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
			appSourceDefaultSpecMC := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeNameMC,
				Scope:   enterpriseApi.ScopeLocal,
			}

			// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
			appSourceNameMC := "appframework-mc-" + testenv.RandomDNSName(3)
			appSourceSpecMC := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameMC, s3TestDirMC, appSourceDefaultSpecMC)}

			// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
			appFrameworkSpecMC := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpecMC,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpecMC,
				AppSources:           appSourceSpecMC,
			}

			// MC AppFramework Spec
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
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(testenvInstance.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Create App framework Spec for C3
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeCluster,
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

			indexerReplicas := 3

			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster")
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, 10, true)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify Apps are downloaded by init-container on CM, Deployer and MC
			initContDownloadLocation := "/init-apps/" + appSourceName
			initContDownloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList = testenv.GetAppFileList(appListV2, 2)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "POD", mcPodName, "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, initContDownloadLocationMCPod)

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			shReplicas := int(shc.Spec.Replicas)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)
			// Saving current V1 bundle hash for future comparision
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify Apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV2, false, false)

			// Verify Apps are installed
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Get instance of current SHC CR with latest config
			shcName = deployment.GetName() + "-shc"
			shc = &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale SHC
			defaultSHReplicas := shc.Spec.Replicas
			scaledSHReplicas := defaultSHReplicas + 1
			testenvInstance.Log.Info("Scaling up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of SHC
			err = deployment.GetInstance(shcName, shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")
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

			// Scale indexers
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

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify bundle push status. Bundle hash not compared as scaleup does not involve new config
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledSHReplicas))
			// Saving current V2 bundle hash to future comparision with new config bundle hash
			clusterManagerBundleHash = testenv.GetClusterManagerBundleHash(deployment)

			// Verify Apps are copied to location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV2, false, false)

			// Verify apps install status and version
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Delete apps on S3 for new Apps
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing downgrade scenario")

			// Upload older version of apps to S3
			appFileList = testenv.GetAppFileList(appListV1, 1)
			appVersion = "V1"
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", " version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledSHReplicas))

			// Verify Apps are copied to location
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND for app", " version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app", " version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed cluster-wide
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", " version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA and have apps installed locally on CM and SHC Deployer", func() {

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

			// Create Single site Cluster and SHC, with App Framework enabled on CM and SHC Deployer
			indexerReplicas := 3
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster")
			err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, 10, false)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList := testenv.GetAppFileList(appListV1, 1)
			appVersion := "V1"
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify apps are copied at the correct location on CM and on Deployer (/etc/apps/)
			testenvInstance.Log.Info("Verify Apps are copied to correct location on CM and on Deployer (/etc/apps/) for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, false)

			// Verify apps are installed locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify Apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, "enabled", false, false)

			// Verify apps are not copied in the apps folder on CM and /etc/shcluster/ on Deployer (therefore not installed on peers and on SH)
			testenvInstance.Log.Info("Verify Apps are NOT copied to "+splcommon.ManagerAppsLoc+" on CM and "+splcommon.SHCluster+" on Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, true)

			//Delete apps on S3 for new Apps
			testenvInstance.Log.Info("Delete Apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing upgrade scenario")

			//Upload new Versioned Apps to S3
			appVersion = "V2"
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)
			appFileList = testenv.GetAppFileList(appListV2, 2)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
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

			// Verify Apps are downloaded by init-container
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify apps are copied at the correct location on CM and on Deployer (/etc/apps/)
			testenvInstance.Log.Info("Verify Apps are copied to correct location on CM and on Deployer (/etc/apps/) for app", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, false)

			// Verify apps are installed locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify Apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)

			// Verify apps are not copied in the apps folder on CM and /etc/shcluster/ on Deployer (therefore not installed on peers and on SH)
			testenvInstance.Log.Info("Verify Apps are NOT copied to "+splcommon.ManagerAppsLoc+" on CM and "+splcommon.SHCluster+" on Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3, appframework: can deploy a C3 SVA and have ES app installed on SHC", func() {

			// Delete apps on S3 for new Apps
			testenvInstance.Log.Info("Delete existing apps on S3 before starting upload of ES App")
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

			// Create Single site Cluster and SHC, with App Framework enabled on SHC Deployer
			// Deploy the CM
			testenvInstance.Log.Info("Deploy Cluster manager in single site configuration")
			_, err = deployment.DeployClusterMaster(deployment.GetName(), "", "")
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy the indexer cluster
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

			// Verify Apps are downloaded by init-container
			testenvInstance.Log.Info("Verfiy ES app is downloaded by init container on deployer pod")
			initContDownloadLocation := "/init-apps/" + appSourceName
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), deployerPod, appFileList, initContDownloadLocation)

			// Verify ES app is installed locally on SHC Deployer
			testenvInstance.Log.Info("Verfiy ES app is installed locally on deployer pod")
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), deployerPod, esApp, true, "disabled", false, false)

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
		It("c3, integration, appframework: can deploy a C3 SVA with apps installed locally on CM and SHC Deployer, and cluster-wide on Peers and SHs", func() {

			// Delete apps on S3 for new Apps to split them across both cluster and local
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Split Applist into 2 list for local and cluster install
			appListLocal := appListV1[len(appListV1)/2:]
			appListCluster := appListV1[:len(appListV1)/2]

			// Upload appListLocal to bucket 1 on S3
			appFileList := testenv.GetAppFileList(appListLocal, 1)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to a 2nd directory on S3 bucket as we need 2 buckets locations for this test.(appListLocal apps to be used for local install, appListCluster apps for cluster install)
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
			testenvInstance.Log.Info("Create Single site Indexer Cluster with Local and Cluster Install for Apps")
			indexerReplicas := 3
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, 10, false)
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
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify apps with cluster scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameCluster
			appFileList = testenv.GetAppFileList(appListCluster, 1)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			shReplicas := int(shc.Spec.Replicas)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)
			// Saving current V1 bundle hash for future comparision
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps with local scope are installed locally on CM and on SHC Deployer
			localPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify Apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
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
			testenvInstance.Log.Info("Verify Apps are installed clusterwide on indexers and search-heads by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", false, true)

			// Delete apps on S3 for new Apps
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing upgrade scenario")

			// Upload new Versioned Apps to S3
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
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify apps with cluster scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameCluster
			appFileList = testenv.GetAppFileList(appListCluster, 2)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)
			// Saving current V2 bundle hash for future comparision
			clusterManagerBundleHash = testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps with local scope are installed locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify Apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", true, false)

			// Verify apps with cluster scope are installed on indexers
			testenvInstance.Log.Info("Verify Apps are installed clusterwide on indexers and search-heads by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", true, true)

			// Delete apps on S3 for new Apps
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing downgrade scenario")

			// Upload new Versioned Apps to S3 to test downgrade scenario
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
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify apps with cluster scope are downloaded by init-container
			initContDownloadLocation = "/init-apps/" + appSourceNameCluster
			appFileList = testenv.GetAppFileList(appListCluster, 1)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), downloadPodNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps with local scope are installed locally on CM and on SHC Deployer
			testenvInstance.Log.Info("Verify Apps are installed Locally on CM and Deployer by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", false, false)

			// Verify apps with cluster scope are installed on indexers
			testenvInstance.Log.Info("Verify Apps are installed clusterwide on indexers and search-heads by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA instance with App Framework enabled and install above 200MB of apps at once", func() {

			// Creating a bigger list of apps to be installed than the default one
			appList := append(appListV1, "splunk_app_db_connect", "splunk_app_aws", "Splunk_TA_microsoft-cloudservices", "Splunk_ML_Toolkit", "Splunk_Security_Essentials")
			appFileList := testenv.GetAppFileList(appList, 1)

			// Download App from S3
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, testenv.GetAppFileList(appList, 1))
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload app to S3
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, testenv.GetAppFileList(appList, 1), downloadDirV1)
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
			err = deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpec, 10, false)
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

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			shReplicas := int(shc.Spec.Replicas)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND")
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer")
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Get indexers and SH pod names
			podNames := []string{}
			for i := 0; i < int(indexerReplicas); i++ {
				sh := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				podNames = append(podNames, string(sh))
			}

			for i := 0; i < int(shReplicas); i++ {
				sh := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				podNames = append(podNames, string(sh))
			}

			// Verify apps are installed on indexers and SH
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands")
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, true)
		})
	})
})
