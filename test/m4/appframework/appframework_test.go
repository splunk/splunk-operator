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
	"path/filepath"
	"strings"
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
	var uploadedApps []string
	var appSourceNameIdxc string
	var appSourceNameShc string
	var s3TestDirShc string
	var s3TestDirIdxc string
	var appSourceVolumeNameIdxc string
	var appSourceVolumeNameShc string

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		s3TestDirIdxc = "m4appfw-idxc-" + testenv.RandomDNSName(4)
		s3TestDirShc = "m4appfw-shc-" + testenv.RandomDNSName(4)
		appSourceVolumeNameIdxc = "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
		appSourceVolumeNameShc = "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
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

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("smoke, m4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and upgrade them", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   ########## INITIAL VERIFICATIONS ##########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and installed on Monitoring Console and on Search Heads and Indexers pods
			   ############# UPGRADE APPS ################
			   * Upgrade apps in app sources
			   * Wait for Monitoring Console and M4 pod to be ready
			   ########## UPGRADE VERIFICATIONS ##########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and upgraded on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ##################
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testenvInstance, volumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
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
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Upload V1 apps to S3 for Indexer Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########## INITIAL VERIFICATIONS ##########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			opLocalAppPathClusterManager := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameIdxc)
			opPod := testenv.GetOperatorPodName(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			opLocalAppPathSearchHeadCluster := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameShc)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify Apps Deleted on Operator Pod for Monitoring Console
			opLocalAppPathMonitoringConsole := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), mc.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameMC)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathMonitoringConsole)

			// Verify App Install State on Cluster Manager CR
			appFileList = testenv.GetAppFileList(appListV1)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			downloadLocationClusterMaster := testenv.AppStagingLocOnPod + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := testenv.AppStagingLocOnPod + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are deleted on Monitoring Console Pod
			downloadLocationMCPod := testenv.AppStagingLocOnPod + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V1 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Add Search Head Cluster and Indexer Pods to all Pod Names
			allPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)...)
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)

			// Verify apps are copied to correct location for M4 SVA
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location for M4", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are copied to correct location for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location for Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, false)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer (App list: %s)", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify apps are installed on M4 (cluster-wide)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on M4 pods (cluster-wide)", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Verify apps are installed on Monitoring Console (local install)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Monitoring Console pod (local)", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, "enabled", false, false)

			//############# UPGRADE APPS ################
			// Delete apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########## UPGRADE VERIFICATIONS ##########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify Apps Deleted on Operator Pod for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathMonitoringConsole)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are deleted on Monitoring Console Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			// Verify bundle push status and compare bundle hash with previous V1 bundle
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify V2 apps are copied to location for M4 SVA
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on M4 pods", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify V2 apps are copied to location for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, false)

			// Verify V2 apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer", appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify apps are updated on M4(cluster-wide)
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been updated to %s on M4 pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Verify apps are updated on Monitoring Console (local install)
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been updated to %s on Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, "enabled", true, false)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and downgrade them", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V2 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V2 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   ########## INITIAL VERIFICATIONS ##########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and installed on Monitoring Console and on Search Heads and Indexers pods
			   ############ DOWNGRADE APPS ###############
			   * Downgrade apps in app sources
			   * Wait for Monitoring Console and M4 to be ready
			   ########## DOWNGRADE VERIFICATIONS ########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and downgraded on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ##################
			// Upload V2 version of apps to S3 for Monitoring Console
			appVersion := "V2"
			appFileList := testenv.GetAppFileList(appListV2)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testenvInstance, volumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
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
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Upload V2 apps to S3 for Indexer Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########## INITIAL VERIFICATIONS ##########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			opLocalAppPathClusterManager := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameIdxc)
			opPod := testenv.GetOperatorPodName(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			opLocalAppPathSearchHeadCluster := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameShc)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify Apps Deleted on Operator Pod for Monitoring Console
			opLocalAppPathMonitoringConsole := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), mc.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameMC)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathMonitoringConsole)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			downloadLocationClusterMaster := testenv.AppStagingLocOnPod + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := testenv.AppStagingLocOnPod + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are deleted on Monitoring Console Pod
			downloadLocationMCPod := testenv.AppStagingLocOnPod + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V2 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Add Search Head Cluster and Indexer Pods to all Pod Names
			allPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)...)
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)

			// Verify V2 apps are copied to location on M4
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on M4", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify V2 apps are copied to correct location for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location for Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, false)

			// Verify V2 apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer (App list: %s)", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify V2 apps are installed on (cluster-wide)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on the pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Verify apps are installed on Monitoring Console (local install)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Monitoring Console pod", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, "enabled", true, false)

			//############# DOWNGRADE APPS ################
			// Delete V2 apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########## DOWNGRADE VERIFICATIONS ########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify Apps Deleted on Operator Pod for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathMonitoringConsole)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are deleted on Monitoring Console Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify V1 apps are copied to correct location for M4 SVA
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location for M4", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify V1 apps are copied to correct location for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location for Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, false)

			// Verify V1 apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify apps are downgraded on M4 (cluster-wide)
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been downgraded to %s on the M4 pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Verify apps are downgraded on Monitoring Console (local install)
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been downgraded to %s on Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, "enabled", false, false)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, appframework: can deploy a M4 SVA with App Framework enabled, install apps, scale up clusters, install apps on new pods, scale down", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to S3 for M4
			   * Create app source for M4 SVA (Cluster Manager and Deployer)
			   * Prepare and deploy M4 CRD with app config and wait for pods to be ready
			   ########### INITIAL VERIFICATIONS #########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is sucessful
			   * Verify apps are copied and installed on Monitoring Console and also on Search Heads and Indexers pods
			   ############### SCALING UP ################
			   * Scale up Indexers and Search Head Cluster
			   * Wait for Monitoring Console and M4 to be ready
			   ######### SCALING UP VERIFICATIONS ########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is sucessful
			   * Verify apps are copied and installed on new Search Heads and Indexers pods
			   ############### SCALING DOWN ##############
			   * Scale down Indexers and Search Head Cluster
			   * Wait for Monitoring Console and M4 to be ready
			   ######### SCALING DOWN VERIFICATIONS ######
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is sucessful
			   * Verify apps are still copied and installed on all Search Heads and Indexers pods
			*/

			//################## SETUP ##################
			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			indexersPerSite := 1
			shReplicas := 3
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########### INITIAL VERIFICATIONS #########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			opLocalAppPathClusterManager := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameIdxc)
			opPod := testenv.GetOperatorPodName(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			opLocalAppPathSearchHeadCluster := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameShc)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			downloadLocationClusterMaster := testenv.AppStagingLocOnPod + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := testenv.AppStagingLocOnPod + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to correct location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer (App list: %s)", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify apps are installed on Monitoring Console and M4(cluster-wide)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed cluster-wide on the pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			//############### SCALING UP ################
			// Get instance of current Search Head Cluster CR with latest config
			err = deployment.GetInstance(deployment.GetName()+"-shc", shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale up Search Head Cluster
			defaultSHReplicas := shc.Spec.Replicas
			scaledSHReplicas := defaultSHReplicas + 1
			testenvInstance.Log.Info("Scale up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of Search Head Cluster
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Failed to scale up Search Head Cluster")

			// Ensure Search Head Cluster scales up and go to ScalingUp phase
			testenv.VerifySearchHeadClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp)

			// Get instance of current Indexer CR with latest config
			idxcName := deployment.GetName() + "-" + "site1"
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas := idxc.Spec.Replicas
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testenvInstance.Log.Info("Scale up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Failed to Scale Up Indexer Cluster")

			// Ensure Indexer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp, idxcName)

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//######### SCALING UP VERIFICATIONS ########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Search Head pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify bundle push status. Bundle hash not compared as scaleup does not involve new config
			testenvInstance.Log.Info("Verify bundle push status after scaling up")
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount)

			// Verify V1 apps are copied to correct location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location after scaling up of Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify V1 apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer after scaling up of Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify V1 apps are installed cluster-wide
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on the pods after scaling up of Indexers and Search Heads", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			//############### SCALING DOWN ##############
			// Get instance of current Search Head Cluster CR with latest config
			err = deployment.GetInstance(deployment.GetName()+"-shc", shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale down Search Head Cluster
			defaultSHReplicas = shc.Spec.Replicas
			scaledSHReplicas = defaultSHReplicas - 1
			testenvInstance.Log.Info("Scaling down Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of Search Head Cluster
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Failed to scale down Search Head Cluster")

			// Ensure Search Head Cluster scales down and go to ScalingDown phase
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

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//######### SCALING DOWN VERIFICATIONS ######
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount)

			// Verify apps are copied to correct location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location based on Pod KIND after scaling down of Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer after scaling down of Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify apps are installed cluster-wide after scaling down
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on the pods after scaling down of Indexers and Search Heads", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework)", func() {
		It("integration, m4, appframework: can deploy a M4 SVA and have apps installed locally on Cluster Manager and Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3
			   * Create app source with local scope for M4 SVA (Cluster Manager and Deployer)
			   * Prepare and deploy M4 CRD with app framework and wait for pods to be ready
			   ########## INITIAL VERIFICATION #############
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are installed locally on Cluster Manager and Deployer
			   ############### UPGRADE APPS ################
			   * Upgrade apps in app sources
			   * Wait for pods to be ready
			   ########## UPGRADE VERIFICATIONS ############
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are copied, installed and upgraded on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 60)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			indexersPerSite := 1
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########## INITIAL VERIFICATION #############
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			opLocalAppPathClusterManager := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameIdxc)
			opPod := testenv.GetOperatorPodName(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			opLocalAppPathSearchHeadCluster := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameShc)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			downloadLocationClusterMaster := testenv.AppStagingLocOnPod + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manger pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := testenv.AppStagingLocOnPod + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are copied at the correct location on Cluster Manager and on Deployer (/etc/apps/)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to /etc/apps on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify V1 apps are installed locally on Cluster Manager and on Deployer
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed locally on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, "enabled", false, false)

			// Verify V1 apps are not copied in the apps folder on Cluster Manager and /etc/shcluster/ on Deployer (therefore not installed on Indexers and on Search Heads)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are not copied to "+splcommon.ManagerAppsLoc+" on Cluster Manager and "+splcommon.SHCluster+" on Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, true)

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########## UPGRADE VERIFICATIONS ############
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manger pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V2 apps are copied at the correct location on Cluster Manager and on Deployer (/etc/apps/)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to /etc/apps on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, false)

			// Verify V2 apps are not copied in the apps folder on Cluster Manager and /etc/shcluster/ on Deployer (therefore not installed on Indexers and on Search Heads)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are not copied to "+splcommon.ManagerAppsLoc+" on Cluster Manager and "+splcommon.SHCluster+" on Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, true)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify V2 apps are installed locally on Cluster Manager and on Deployer
			testenvInstance.Log.Info("Verify apps have been updated to %s on Cluster Manager and Deployer", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)
		})
	})

	Context("Multi Site Indexer Cluster with SHC (m4) with App Framework", func() {
		It("integration, m4, appframework: can deploy a M4 SVA with App Framework enabled for manual poll", func() {
			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console with app framework and wait for the pod to be ready
			   * Upload V1 apps to S3
			   * Create app source with manaul poll for M4 SVA (Cluster Manager and Deployer)
			   * Prepare and deploy M4 CRD with app framework and wait for pods to be ready
			   ########## INITIAL VERIFICATION #############
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are installed locally on Cluster Manager and Deployer
			   ############### UPGRADE APPS ################
			   * Upgrade apps in app sources
			   * Wait for pods to be ready
			   ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			   * Verify Apps are not updated
			   ############ ENABLE MANUAL POLL ############
			   * Verify Manual Poll disabled after the check
			   ############## UPGRADE VERIFICATIONS ############
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify App Directory in under splunk path
			   * Verify apps are installed locally on Cluster Manager and Deployer
			*/

			// ################## SETUP ####################
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testenvInstance, volumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 0)
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
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Upload V1 apps to S3 for Indexer Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 0)

			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster with App framework")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########## INITIAL VERIFICATIONS ##########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			opLocalAppPathClusterManager := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameIdxc)
			opPod := testenv.GetOperatorPodName(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			opLocalAppPathSearchHeadCluster := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameShc)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify Apps Deleted on Operator Pod for Monitoring Console
			opLocalAppPathMonitoringConsole := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), mc.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameMC)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathMonitoringConsole)

			// Verify App Install State on Cluster Manager CR
			appFileList = testenv.GetAppFileList(appListV1)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			downloadLocationClusterMaster := testenv.AppStagingLocOnPod + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := testenv.AppStagingLocOnPod + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are deleted on Monitoring Console Pod
			downloadLocationMCPod := testenv.AppStagingLocOnPod + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V1 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Add Search Head Cluster and Indexer Pods to all Pod Names
			allPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)...)
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)

			// Verify apps are copied to correct location for M4 SVA
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location for M4", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are copied to correct location for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location for Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, false)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer (App list: %s)", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify apps are installed on M4 (cluster-wide)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on M4 pods (cluster-wide)", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Verify apps are installed on Monitoring Console (local install)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Monitoring Console pod (local)", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, "enabled", false, false)

			// ############### UPGRADE APPS ################

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)

			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify Apps Deleted on Operator Pod for Monitoring Console
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathMonitoringConsole)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify apps are deleted on Monitoring Console Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			//Verify Apps are not updated cluster-wide
			appVersion = "V2"
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are not updated on the pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Verify apps are not updated on Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are not updated on Monitoring Console pod (local)", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, "enabled", false, false)

			// ############ ENABLE MANUAL POLL ############
			testenvInstance.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(deployment, testenvInstance.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testenvInstance.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterMaster"] = strings.Replace(config.Data["ClusterMaster"], "off", "on", 1)
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			config.Data["MonitoringConsole"] = strings.Replace(config.Data["MonitoringConsole"], "off", "on", 1)
			err = deployment.UpdateCR(config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure Indexer cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.DumpGetPodsLife(testenvInstance.GetName())

			// ########## Verify Manual Poll disabled after the check #################

			// Verify config map set back to off after poll trigger
			testenvInstance.Log.Info(fmt.Sprintf("Verify config map set back to off after poll trigger for %s app", appVersion))
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(deployment, testenvInstance.GetName())

			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off") && strings.Contains(config.Data["MonitoringConsole"], "status: off")).To(Equal(true), "Config map update not complete")

			// ############ VERIFY APPS UPDATED TO V2 #############
			appFileList = testenv.GetAppFileList(appListV2)

			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify Apps Deleted on Operator Pod for Monitoring Console
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathMonitoringConsole)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Monitoring Console CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, mc.Name, mc.Kind, appSourceNameMC, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify apps are deleted on Monitoring Console Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			// Verify bundle push status and compare bundle hash with previous V1 bundle
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify V2 apps are copied to location for M4 SVA
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on M4 pods", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify V2 apps are copied to location for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, false)

			// Verify V2 apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer", appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify apps are updated on M4(cluster-wide)
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been updated to %s on M4 pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Verify apps are updated on Monitoring Console (local install)
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been updated to %s on Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, "enabled", true, false)
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework)", func() {
		It("integration, m4, appframework: can deploy a M4 SVA and have apps installed and updated locally on Cluster Manager and Deployer via manual poll", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3
			   * Create app source with local scope for M4 SVA (Cluster Manager and Deployer)
			   * Prepare and deploy M4 CRD with app framework and wait for pods to be ready
			   ########## INITIAL VERIFICATION #############
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are installed locally on Cluster Manager and Deployer
			   ############### UPGRADE APPS ################
			   * Upgrade apps in app sources
			   * Wait for pods to be ready
			   ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			   * Verify Apps are not updated
			   ############ ENABLE MANUAL POLL ############
			   * Verify Manual Poll disabled after the poll is triggered
			   ########## UPGRADE VERIFICATIONS ############
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are copied, installed and upgraded on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 0)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			indexersPerSite := 1
			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.DumpGetPodsLife(testenvInstance.GetName())

			//########## INITIAL VERIFICATION #############
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			opLocalAppPathClusterManager := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameIdxc)
			opPod := testenv.GetOperatorPodName(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			opLocalAppPathSearchHeadCluster := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameShc)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			downloadLocationClusterMaster := testenv.AppStagingLocOnPod + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manger pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := testenv.AppStagingLocOnPod + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are copied at the correct location on Cluster Manager and on Deployer (/etc/apps/)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to /etc/apps on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, false)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify V1 apps are installed locally on Cluster Manager and on Deployer
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed locally on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, "enabled", false, false)

			// Verify V1 apps are not copied in the apps folder on Cluster Manager and /etc/shcluster/ on Deployer (therefore not installed on Indexers and on Search Heads)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are not copied to "+splcommon.ManagerAppsLoc+" on Cluster Manager and "+splcommon.SHCluster+" on Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, true)

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)

			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			//Verify Apps are not updated locally
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are not updated on the pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, false)

			// ############ ENABLE MANUAL POLL ############
			testenvInstance.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(deployment, testenvInstance.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testenvInstance.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterMaster"] = strings.Replace(config.Data["ClusterMaster"], "off", "on", 1)
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure Indexer cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.DumpGetPodsLife(testenvInstance.GetName())

			// ########## Verify Manual Poll config map disabled after the poll is triggered #################

			// Verify config map set back to off after poll trigger
			testenvInstance.Log.Info(fmt.Sprintf("Verify config map set back to off after poll trigger for %s app", appVersion))
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(deployment, testenvInstance.GetName())

			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			//########## UPGRADE VERIFICATIONS ############
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathClusterManager)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, appFileList, opLocalAppPathSearchHeadCluster)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseInstall, appFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseInstall, appFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manger pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V2 apps are copied at the correct location on Cluster Manager and on Deployer (/etc/apps/)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to /etc/apps on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, false)

			// Verify V2 apps are not copied in the apps folder on Cluster Manager and /etc/shcluster/ on Deployer (therefore not installed on Indexers and on Search Heads)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are not copied to "+splcommon.ManagerAppsLoc+" on Cluster Manager and "+splcommon.SHCluster+" on Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, true)

			//Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testenvInstance, testenvInstance.GetName(), splunkPodAge)

			// Verify V2 apps are installed locally on Cluster Manager and on Deployer
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been updated to %s on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)
		})
	})
})
