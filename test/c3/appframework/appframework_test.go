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
	"path/filepath"
	"strings"
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
	var s3TestDirShc string
	var s3TestDirIdxc string
	var s3TestDirShcLocal string
	var s3TestDirIdxcLocal string
	var s3TestDirShcCluster string
	var s3TestDirIdxcCluster string
	var appSourceNameIdxc string
	var appSourceNameShc string
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

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA with App Framework enabled, install apps then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   ######### INITIAL VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied, installed on Monitoring Console and on Search Heads and Indexers pods
			   ############### UPGRADE APPS ################
			   * Upload V2 apps on S3
			   * Wait for Monitoring Console and C3 pods to be ready
			   ############ FINAL VERIFICATIONS ############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V2 apps are copied and upgraded on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)

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
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy C3 CRD
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			//######### INITIAL VERIFICATIONS #############

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
			downloadLocationClusterMaster := "/init-apps/" + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are deleted on Monitoring Console Pod
			downloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V1 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify V1 apps are copied to location
			allPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)...)

			// Verify V1 apps are copied on Indexers and Search Heads
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify V2 apps are copied on Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, true)

			// Verify V1 apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer (App List: %s) ", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, false)

			// Verify V1 apps are installed on Indexers and Search Heads
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Search Heads and Indexers pods: %s", appVersion, allPodNames))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Verify V1 apps are installed on Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, "enabled", false, false)

			//############### UPGRADE APPS ################
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

			// Upload V2 apps to S3 for Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			//############ FINAL VERIFICATIONS ############
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

			// Verify bundle push status and compare bundle hash with previous V1 bundle hash
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify V2 apps are copied to location
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on C3 pods", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, false)

			// Verify V2 apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Cluster Manager and Deployer)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, false)

			// Verify V2 apps are updated on Search Heads and Indexers
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been updated to %s on Search Heads and Indexers pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Verify V2 apps are updated on Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been updated to %s on Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, "enabled", true, false)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA with App Framework enabled, install apps then downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V2 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V2 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Create app source for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   ########### INITIAL VERIFICATIONS ###########
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V2 apps are copied, installed on Monitoring Console and also on Search Heads and Indexers pods
			   ############## DOWNGRADE APPS ###############
			   * Upload V1 apps on S3
			   * Wait for Monitoring Console and C3 pods to be ready
			   ########### FINAL VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and downgraded on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Upload V2 apps to S3 for Monitoring Console
			appVersion := "V2"
			appFileList := testenv.GetAppFileList(appListV2)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)

			// Monitoring Console AppFramework Spec
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

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Upload V2 apps to S3 for Indexer Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy C3 CRD
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			//########### INITIAL VERIFICATIONS ###########

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
			downloadLocationClusterMaster := "/init-apps/" + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are deleted on Monitoring Console Pod
			downloadLocationMCPod := "/init-apps/" + appSourceNameMC
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, mcName, 0)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Monitoring Console pod %s", appVersion, mcPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appFileList, downloadLocationMCPod)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V2 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify V2 apps are copied to location
			allPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			allPodNames = append(allPodNames, testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)...)

			// Verify V2 apps are copied on Indexers and Search Heads
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify V2 apps are copied on Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, false)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer (App list: %s)", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, false)

			// Verify V2 apps are installed on Indexers and Search Heads
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Indexers and Search Heads", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Verify V2 apps are installed on Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Monitoring Console", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV2, true, "enabled", true, false)

			//############## DOWNGRADE APPS ###############
			// Delete apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexers", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexers", appVersion))
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

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			//########### FINAL VERIFICATIONS #############
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
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify V1 apps are copied to location
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on C3 pods", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Monitoring Console", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, false)

			// Verify V1 apps are not copied in /etc/apps/ on Cluster Manager and Deployer (therefore not installed on Cluster Manager and Deployer)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, false)

			// Verify V1 apps are downgraded on Search Heads and Indexers
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been downgraded to %s on Search Heads and Indexers pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Verify V1 apps are downgraded on Monitoring Console
			testenvInstance.Log.Info(fmt.Sprintf("Verify apps have been downgraded to %s on Monitoring Console pod", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), []string{mcPodName}, appListV1, true, "enabled", false, false)

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("integration, c3, appframework: can deploy a C3 SVA with App Framework enabled, install apps, scale up clusters, install apps on new pods, scale down", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps on S3 for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app config and wait for pods to be ready
			   ########## INITIAL VERIFICATIONS ############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied, installed on Search Heads and Indexers
			   #############  SCALING UP ###################
			   * Scale up indexers and Search Heads
			   * Wait for C3 to be ready
			   ########## SCALING UP VERIFICATIONS #########
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is sucessful
			   * Verify apps are copied and installed on all Search Heads and Indexers pods
			   ############### SCALING DOWN ################
			   * Scale down Indexers and Search Heads
			   * Wait for C3 to be ready
			   ######## SCALING DOWN VERIFICATIONS #########
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
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			appFileList := testenv.GetAppFileList(appListV1)
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy C3 CRD
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//########## INITIAL VERIFICATIONS ############

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
			downloadLocationClusterMaster := "/init-apps/" + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify bundle push status
			testenvInstance.Log.Info("Verify bundle push status")
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to correct location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on all pods", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer (App list: %s)", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed on C3
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed cluster-wide", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			//#############  SCALING UP ###################
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
			idxcName := deployment.GetName() + "-idxc"
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas := idxc.Spec.Replicas
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testenvInstance.Log.Info("Scale up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Failed to scale up Indexer Cluster")

			// Ensure Indexer Cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingUp, idxcName)

			// Ensure Indexer Cluster go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Verify New Indexer On Cluster Manager
			indexerName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas-1)
			testenvInstance.Log.Info(fmt.Sprintf("Checking for New Indexer %s On Cluster Manager", indexerName))
			Expect(testenv.CheckIndexerOnCM(deployment, indexerName)).To(Equal(true))

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//########## SCALING UP VERIFICATIONS #########
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

			// Verify bundle push status. Bundle hash not compared as scaleup does not involve new config
			testenvInstance.Log.Info("Verify bundle push status after scaling up")
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledSHReplicas))

			// Verify V1 apps are copied to location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location after scaling up Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify V1 apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer after scaling up of Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify V1 apps are installed on C3
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on Indexers and Search Heads after scaling up", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			//############### SCALING DOWN ################
			// Get instance of current Search Head Cluster CR with latest config
			shc = &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(deployment.GetName()+"-shc", shc)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale down Search Head Cluster
			defaultSHReplicas = shc.Spec.Replicas
			scaledSHReplicas = defaultSHReplicas - 1
			testenvInstance.Log.Info("Scale down Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

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

			// Ensure Indexer Cluster scales down and go to ScalingDown phase
			testenv.VerifyIndexerClusterPhase(deployment, testenvInstance, splcommon.PhaseScalingDown, idxcName)

			// Ensure Indexer Cluster go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//######## SCALING DOWN VERIFICATIONS #########
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
			testenvInstance.Log.Info("Verify bundle push status after scaling down")
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledSHReplicas))

			// Verify apps are copied to correct location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location after scaling down of Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer after scaling down of Indexers and Search Heads", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Verify apps are installed cluster-wide after scaling down
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on the pods after scaling down of Indexers and Search Heads", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("smoke, c3, appframework: can deploy a C3 SVA and have apps installed locally on Cluster Manager and Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3
			   * Create app source with local scope for C3 SVA (Cluster Manager and Deployer)
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ############# INITIAL VERIFICATION ##########
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are installed locally on Cluster Manager and Deployer
			   ############### UPGRADE APPS ################
			   * Upgrade apps in app sources
			   * Wait for pods to be ready
			   ########### UPGRADE VERIFICATIONS ###########
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are copied, installed and upgraded on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 60)

			// Deploy C3 CRD
			indexerReplicas := 3
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//############## INITIAL VERIFICATION ##########
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
			downloadLocationClusterMaster := "/init-apps/" + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify V1 apps are copied at the correct location on Cluster Manager and on Deployer (/etc/apps/)
			podNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Cluster Manager and on Deployer (/etc/apps/)", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, false)

			// Verify V1 apps are installed locally on Cluster Manager and on Deployer
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed locally on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, "enabled", false, false)

			// Verify V1 apps are not copied in the apps folder on Cluster Manager and /etc/shcluster/ on Deployer (therefore not installed on Indexers and on Search Heads)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to "+splcommon.ManagerAppsLoc+" on Cluster Manager and "+splcommon.SHCluster+" on Deployer (App list: %s)", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, false, true)

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3
			appVersion = "V2"
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//########### UPGRADE VERIFICATIONS ###########
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

			// Verify V2 apps are copied at the correct location on Cluster Manager and on Deployer (/etc/apps/)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location on Cluster Manager and on Deployer (/etc/apps/)", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, false)

			// Verify V2 apps are installed locally on Cluster Manager and on Deployer
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed locally on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, true, "enabled", true, false)

			// Verify V2 apps are not copied in the apps folder on Cluster Manager and /etc/shcluster/ on Deployer (therefore not installed on Indexers and on Search Heads)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to "+splcommon.ManagerAppsLoc+" on Cluster Manager and "+splcommon.SHCluster+" on Deployer (App list: %s)", appVersion, appFileList))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV2, false, true)
		})
	})

	XContext("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3, appframework: can deploy a C3 SVA and have ES app installed on Search Head Cluster", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload ES app to S3
			   * Create App Source with 'ScopeClusterWithPreConfig' scope for C3 SVA
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ################## VERIFICATION #############
			   * Verify ES app is installed on Deployer and on Search Heads
			*/

			//################## SETUP ####################

			// Download ES app from S3
			testenvInstance.Log.Info("Download ES app from S3")
			esApp := []string{"SplunkEnterpriseSecuritySuite"}
			appFileList := testenv.GetAppFileList(esApp)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download ES app file from S3")

			// Create local directory for file download
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)

			// Upload ES app to S3
			testenvInstance.Log.Info("Upload ES app to S3")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload ES app to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName := "appframework-shc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeClusterWithPreConfig, appSourceName, s3TestDirShc, 60)

			// Deploy C3 SVA
			// Deploy the Cluster Manager
			testenvInstance.Log.Info("Deploy Cluster Manager")
			_, err = deployment.DeployClusterMaster(deployment.GetName(), "", "", "")
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy the Indexer Cluster
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster")
			indexerReplicas := 3
			_, err = deployment.DeployIndexerCluster(deployment.GetName()+"-idxc", deployment.GetName(), indexerReplicas, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster")

			// Deploy the Search Head Cluster
			testenvInstance.Log.Info("Deploy Search Head Cluster")
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
				AppFrameworkConfig: appFrameworkSpecShc,
			}
			_, err = deployment.DeploySearchHeadClusterWithGivenSpec(deployment.GetName()+"-shc", shSpec)
			Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//################## VERIFICATIONS #############
			// Verify ES is downloaded
			testenvInstance.Log.Info("Verify ES app is downloaded on Deployer")
			initContDownloadLocation := "/init-apps/" + appSourceName
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsDownloadedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), deployerPod, appFileList, initContDownloadLocation)

			// Verify ES app is installed locally on Deployer
			testenvInstance.Log.Info("Verify ES app is installed locally on Deployer")
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), deployerPod, esApp, true, "disabled", false, false)

			// Verify ES is installed on Search Heads
			testenvInstance.Log.Info("Verify ES app is installed on Search Heads")
			podNames := []string{}
			for i := 0; i < int(shSpec.Replicas); i++ {
				sh := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				podNames = append(podNames, string(sh))
			}
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, esApp, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration, appframework: can deploy a C3 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Split Applist into clusterlist and local list
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster for local and cluster scope
			   * Create app sources for Cluster Manager and Deployer with local and cluster scope
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   ######### INITIAL VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied, installed on Monitoring Console and on Search Heads and Indexers pods
			   ############### UPGRADE APPS ################
			   * Upload V2 apps on S3
			   * Wait for all C3 pods to be ready
			   ############ FINAL VERIFICATIONS ############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V2 apps are copied and upgraded on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Split Applist into 2 lists for local and cluster install
			appVersion := "V1"
			appListLocal := appListV1[len(appListV1)/2:]
			appListCluster := appListV1[:len(appListV1)/2]

			// Upload appListLocal list of apps to S3 (to be used for local install) for Idxc
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirIdxcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListLocal list of apps to S3 (to be used for local install) for Shc
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirShcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirIdxcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirShcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameLocalIdxc := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameLocalShc := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameClusterIdxc := "appframework-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameClusterShc := "appframework-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxcLocal := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShcLocal := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxcCluster := "appframework-test-volume-idxc-cluster-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShcCluster := "appframework-test-volume-shc-cluster-" + testenv.RandomDNSName(3)

			// Create App framework Spec for Cluster manager with scope local and append cluster scope
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, s3TestDirIdxcLocal, 60)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameIdxcCluster, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, s3TestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, s3TestDirShcLocal, 60)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameShcCluster, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, s3TestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testenvInstance.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//############ INITIAL VERIFICATIONS ##########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhaseDownload, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhaseDownload, clusterappFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhaseDownload, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhaseDownload, clusterappFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhasePodCopy, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhasePodCopy, clusterappFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhasePodCopy, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhasePodCopy, clusterappFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			opLocalAppPathClusterManagerLocalInstall := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameLocalIdxc)
			opLocalAppPathClusterManagerClusterInstall := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameClusterIdxc)
			opPod := testenv.GetOperatorPodName(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, localappFileList, opLocalAppPathClusterManagerLocalInstall)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, clusterappFileList, opLocalAppPathClusterManagerClusterInstall)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			opLocalAppPathSearchHeadClusterLocalInstall := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameLocalShc)
			opLocalAppPathSearchHeadClusterClusterInstall := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameClusterShc)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, localappFileList, opLocalAppPathSearchHeadClusterLocalInstall)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, clusterappFileList, opLocalAppPathSearchHeadClusterClusterInstall)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhaseInstall, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhaseInstall, clusterappFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhaseInstall, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhaseInstall, clusterappFileList)

			// Verify apps are deleted on Cluster Manager Pod
			localDownloadLocationClusterManager := "/init-apps/" + appSourceVolumeNameIdxcLocal
			clusterDownloadLocationClusterManager := "/init-apps/" + appSourceVolumeNameIdxcCluster
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager Pod pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, localappFileList, localDownloadLocationClusterManager)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, clusterappFileList, clusterDownloadLocationClusterManager)

			// Verify apps are deleted on Search Head Cluster
			localDownloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShcLocal
			clusterDownloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShcCluster
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, localappFileList, localDownloadLocationSearchHeadCluster)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, clusterappFileList, clusterDownloadLocationSearchHeadCluster)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V1 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps with local scope are installed locally on Cluster Manager and on Deployer
			localPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with local scope are installed locally on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", false, false)

			// Verify apps with cluster scope are installed on Indexers
			clusterPodNames := []string{}
			clusterPodNames = append(clusterPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			clusterPodNames = append(clusterPodNames, testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)...)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with cluster scope are installed on Indexers and Search Heads", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", false, true)

			//############### UPGRADE APPS ################
			// Delete apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload appListLocal list of V2 apps to S3 (to be used for local install)
			appVersion = "V2"
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			localappFileList = testenv.GetAppFileList(appListLocal)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of V2 apps to S3 (to be used for cluster-wide install)
			clusterappFileList = testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//########## UPGRADE VERIFICATION #############
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhaseDownload, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhaseDownload, clusterappFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhaseDownload, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhaseDownload, clusterappFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhasePodCopy, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhasePodCopy, clusterappFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhasePodCopy, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhasePodCopy, clusterappFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, localappFileList, opLocalAppPathClusterManagerLocalInstall)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, clusterappFileList, opLocalAppPathClusterManagerClusterInstall)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, localappFileList, opLocalAppPathSearchHeadClusterLocalInstall)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, clusterappFileList, opLocalAppPathSearchHeadClusterClusterInstall)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhaseInstall, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhaseInstall, clusterappFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhaseInstall, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhaseInstall, clusterappFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, localappFileList, localDownloadLocationClusterManager)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, clusterappFileList, clusterDownloadLocationClusterManager)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, localappFileList, localDownloadLocationSearchHeadCluster)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, clusterappFileList, clusterDownloadLocationSearchHeadCluster)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps with local scope are upgraded locally on Cluster Manager and on Deployer
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with local scope are upgraded locally on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", true, false)

			// Verify apps with cluster scope are upgraded on Indexers
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with cluster scope are upgraded on Indexers and Search Heads", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", true, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration, appframework: can deploy a C3 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Split Applist into clusterlist and local list
			   * Upload V2 apps to S3 for Indexer Cluster and Search Head Cluster for local and cluster scope
			   * Create app sources for Cluster Manager and Deployer with local and cluster scope
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   ######### INITIAL VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V2 apps are copied, installed on Monitoring Console and on Search Heads and Indexers pods
			   ############### Downgrade APPS ################
			   * Upload V1 apps on S3
			   * Wait for all C3 pods to be ready
			   ############ FINAL VERIFICATIONS ############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied and upgraded on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Split Applist into 2 lists for local and cluster install
			appVersion := "V2"
			appListLocal := appListV2[len(appListV2)/2:]
			appListCluster := appListV2[:len(appListV2)/2]

			// Upload appListLocal list of apps to S3 (to be used for local install) for Idxc
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirIdxcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListLocal list of apps to S3 (to be used for local install) for Shc
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirShcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirIdxcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirShcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameLocalIdxc := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameLocalShc := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameClusterIdxc := "appframework-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameClusterShc := "appframework-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxcLocal := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShcLocal := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxcCluster := "appframework-test-volume-idxc-cluster-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShcCluster := "appframework-test-volume-shc-cluster-" + testenv.RandomDNSName(3)

			// Create App framework Spec for Cluster manager with scope local and append cluster scope
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, s3TestDirIdxcLocal, 60)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameIdxcCluster, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, s3TestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, s3TestDirShcLocal, 60)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameShcCluster, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, s3TestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testenvInstance.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//############ INITIAL VERIFICATIONS ##########
			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhaseDownload, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhaseDownload, clusterappFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhaseDownload, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhaseDownload, clusterappFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhasePodCopy, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhasePodCopy, clusterappFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhasePodCopy, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhasePodCopy, clusterappFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			opLocalAppPathClusterManagerLocalInstall := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameLocalIdxc)
			opLocalAppPathClusterManagerClusterInstall := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameClusterIdxc)
			opPod := testenv.GetOperatorPodName(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, localappFileList, opLocalAppPathClusterManagerLocalInstall)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, clusterappFileList, opLocalAppPathClusterManagerClusterInstall)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			opLocalAppPathSearchHeadClusterLocalInstall := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeLocal, appSourceNameLocalShc)
			opLocalAppPathSearchHeadClusterClusterInstall := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), shc.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameClusterShc)
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, localappFileList, opLocalAppPathSearchHeadClusterLocalInstall)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, clusterappFileList, opLocalAppPathSearchHeadClusterClusterInstall)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhaseInstall, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhaseInstall, clusterappFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhaseInstall, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhaseInstall, clusterappFileList)

			// Verify apps are deleted on Cluster Manager Pod
			localDownloadLocationClusterManager := "/init-apps/" + appSourceVolumeNameIdxcLocal
			clusterDownloadLocationClusterManager := "/init-apps/" + appSourceVolumeNameIdxcCluster
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager Pod pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, localappFileList, localDownloadLocationClusterManager)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, clusterappFileList, clusterDownloadLocationClusterManager)

			// Verify apps are deleted on Search Head Cluster
			localDownloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShcLocal
			clusterDownloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShcCluster
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, localappFileList, localDownloadLocationSearchHeadCluster)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, clusterappFileList, clusterDownloadLocationSearchHeadCluster)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Saving current V2 bundle hash for future comparison
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify apps with local scope are installed locally on Cluster Manager and on Deployer
			localPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with local scope are installed locally on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", true, false)

			// Verify apps with cluster scope are installed on Indexers
			clusterPodNames := []string{}
			clusterPodNames = append(clusterPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			clusterPodNames = append(clusterPodNames, testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)...)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with cluster scope are installed on Indexers and Search Heads", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", true, true)

			//############# DOWNGRADE APPS ################
			// Delete apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Redefine app lists as LDAP app isn't in V1 apps
			appListLocal = appListV1[len(appListV1)/2:]
			appListCluster = appListV1[:len(appListV1)/2]

			// Upload appListLocal list of V1 apps to S3 (to be used for local install)
			appVersion = "V1"
			testenvInstance.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			localappFileList = testenv.GetAppFileList(appListLocal)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of V2 apps to S3 (to be used for cluster-wide install)
			clusterappFileList = testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//########## UPGRADE VERIFICATION #############
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhaseDownload, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhaseDownload, clusterappFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhaseDownload, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhaseDownload, clusterappFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhasePodCopy, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhasePodCopy, clusterappFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhasePodCopy, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhasePodCopy, clusterappFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, localappFileList, opLocalAppPathClusterManagerLocalInstall)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, clusterappFileList, opLocalAppPathClusterManagerClusterInstall)

			// Verify Apps Deleted on Operator Pod for Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify Apps are deleted on Splunk Operator for version %s", appVersion))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, localappFileList, opLocalAppPathSearchHeadClusterLocalInstall)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{opPod}, clusterappFileList, opLocalAppPathSearchHeadClusterClusterInstall)

			// Verify App Install State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameLocalIdxc, enterpriseApi.PhaseInstall, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameClusterIdxc, enterpriseApi.PhaseInstall, clusterappFileList)

			//Verify App Install State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameLocalShc, enterpriseApi.PhaseInstall, localappFileList)
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameClusterShc, enterpriseApi.PhaseInstall, clusterappFileList)

			// Verify apps are deleted on Cluster Manager Pod
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, localappFileList, localDownloadLocationClusterManager)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, clusterappFileList, clusterDownloadLocationClusterManager)

			// Verify apps are deleted on Search Head Cluster
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, localappFileList, localDownloadLocationSearchHeadCluster)
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, clusterappFileList, clusterDownloadLocationSearchHeadCluster)

			// Verify bundle push status
			testenvInstance.Log.Info(fmt.Sprintf("Verify bundle push status (%s apps)", appVersion))
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps with local scope are downgraded locally on Cluster Manager and on Deployer
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with local scope are downgraded locally on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), localPodNames, appListLocal, true, "enabled", false, false)

			// Verify apps with cluster scope are downgraded on Indexers
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps with cluster scope are downgraded on Indexers and Search Heads", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), clusterPodNames, appListCluster, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3, appframework: can deploy a C3 SVA instance with App Framework enabled and install above 200MB of apps at once", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create App Source for C3 SVA (Cluster Manager and Deployer)
			   * Add more apps than usual on S3 for this test
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ############### VERIFICATIONS ###############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied, installed on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Creating a bigger list of apps to be installed than the default one
			appList := []string{"splunk_app_db_connect", "splunk_app_aws", "Splunk_TA_microsoft-cloudservices", "Splunk_ML_Toolkit", "Splunk_Security_Essentials"}
			appFileList := testenv.GetAppFileList(appList)
			appVersion := "V1"

			// Download apps from S3
			testenvInstance.Log.Info("Download bigger amount of apps from S3 for this test")
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Create consolidated list of app files
			appList = append(appListV1, appList...)
			appFileList = testenv.GetAppFileList(appList)

			// Upload app to S3 for Indexer Cluster
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for Indexer Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload app to S3 for Search Head Cluster
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testenvInstance.Log.Info("Create Single Site Indexer Cluster and Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//  ############### VERIFICATIONS ###############

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
			downloadLocationClusterMaster := "/init-apps/" + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify bundle push status
			testenvInstance.Log.Info("Verify bundle push status")
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), indexerReplicas, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify apps are copied to correct location")
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			managerPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on Cluster Manager and Deployer")
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), managerPodNames, appListV1, false, false)

			// Get Indexers and Search Heads pod names
			podNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			podNames = append(podNames, testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)...)

			// Verify apps are installed on Indexers and Search Heads
			testenvInstance.Log.Info("Verify apps are installed on Indexers and Search Heads")
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), podNames, appListV1, true, "enabled", false, true)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("integration, c3, appframework: can deploy a C3 SVA with App Framework enabled for manual update", func() {
			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3
			   * Create app source with manaul poll for M4 SVA (Cluster Manager and Deployer)
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
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

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testenvInstance, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 0)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			indexerReplicas := 3
			testenvInstance.Log.Info("Deploy Single Site Indexer Cluster")
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// ########## INITIAL VERIFICATION #############

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
			downloadLocationClusterMaster := "/init-apps/" + appSourceVolumeNameIdxc
			clusterManagerPodName := fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Cluster Manager pod %s", appVersion, clusterManagerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{clusterManagerPodName}, appFileList, downloadLocationClusterMaster)

			// Verify apps are deleted on Search Head Cluster
			downloadLocationSearchHeadCluster := "/init-apps/" + appSourceVolumeNameShc
			deployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are deleted on Deployer pod %s", appVersion, deployerPodName))
			testenv.VerifyAppsPackageDeletedOnContainer(deployment, testenvInstance, testenvInstance.GetName(), []string{deployerPodName}, appFileList, downloadLocationSearchHeadCluster)

			// Verify apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location based on Pod KIND", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer for app, (App List: %s)", appVersion, appFileList))
			masterPodNames := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), masterPodNames, appListV1, false, false)

			// Verify apps are installed
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on the pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// ############### UPGRADE APPS ################

			// Delete V1 apps on S3
			testenvInstance.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for C3
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testenvInstance.Log.Info(fmt.Sprintf("Uploading %s apps to S3", appVersion))

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			//  ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############

			// Verify App Download State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhaseDownload, appFileList)

			//Verify App Download State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhaseDownload, appFileList)

			// Verify App Copy State on Cluster Manager CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, cm.Name, cm.Kind, appSourceNameIdxc, enterpriseApi.PhasePodCopy, appFileList)

			//Verify App Copy State on Search Head Cluster CR
			testenv.VerifyAppListPhase(deployment, testenvInstance, shc.Name, shc.Kind, appSourceNameShc, enterpriseApi.PhasePodCopy, appFileList)

			// Verify Apps Deleted on Operator Pod for Cluster Manager
			appVersion = "V1"
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

			// Verify apps are not updated
			appVersion = "V2"
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s are not updated", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

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

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify config map set back to off after poll trigger
			testenvInstance.Log.Info("Verify config map set back to off after poll trigger for app", "version", appVersion)
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(deployment, testenvInstance.GetName())
			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			// ############## UPGRADE VERIFICATIONS ############

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

			// Verify apps are copied to location
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are copied to correct location based on Pod KIND", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on Cluster Manager and on Deployer (therefore not installed on Deployer and on Cluster Manager)
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are NOT copied to /etc/apps on Cluster Manager and Deployer", appVersion))
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), masterPodNames, appListV2, false, false)

			// Verify apps are updated
			testenvInstance.Log.Info(fmt.Sprintf("Verify %s apps are installed on the pods", appVersion))
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)
		})
	})
})
