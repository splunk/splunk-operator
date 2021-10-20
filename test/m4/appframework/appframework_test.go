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

	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

var _ = Describe("m4appfw test", func() {

	var deployment *testenv.Deployment
	var s3TestDir string
	var uploadedApps []string

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")

		// Upload V1 apps to S3
		s3TestDir = "m4appfw-" + testenv.RandomDNSName(4)
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

	Context("Multi Site Indexer Cluster with SHC (M4) with App Framework", func() {
		It("integration, m4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and upgrade them", func() {

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// appSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
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

			siteCount := 3
			indexersPerSite := 1

			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster")
			err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpec, true, 10)
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps are downloaded by init-container
			appVersion := "V1"
			initContDownloadLocation := "/init-apps/" + appSourceName
			podNames := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList := testenv.GetAppFileList(appListV1, 1)
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

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
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			masterPodNames := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), masterPodNames, appListV1, false, false)

			// Verify apps are installed cluster-wide
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// Delete apps on S3 for new Apps
			testenvInstance.Log.Info("Delete Apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing upgrade scenario")

			// Upload newer version of apps to S3
			appFileList = testenv.GetAppFileList(appListV2, 2)
			appVersion = "V2"
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps are downloaded by init-container
			testenvInstance.Log.Info("Verify apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify bundle push status and compare bundle hash with previous V1 bundle
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			testenvInstance.Log.Info("Verify apps are copied to correct location based on Pod KIND for app after upgrade", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer for app after upgrade", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), masterPodNames, appListV2, false, false)

			// Verify Apps are installed cluster-wide
			testenvInstance.Log.Info("Verify apps are installed on the pods by running Splunk CLI commands for app after upgrade", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)
		})
	})

	Context("Multi Site Indexer Cluster with SHC (M4) with App Framework", func() {
		It("integration, m4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and downgrade them", func() {

			// Delete pre-installed apps on S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing downgrade scenario")

			// Upload newer version of apps to S3
			s3TestDir = "m4appfw-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV2, 2)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			volumeName := "appframework-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

			// appSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
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

			siteCount := 3
			indexersPerSite := 1

			testenvInstance.Log.Info("Deploy Multisite Indexer Cluster")
			err = deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpec, true, 10)
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster with App framework")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify apps are downloaded by init-container
			appVersion := "V2"
			initContDownloadLocation := "/init-apps/" + appSourceName
			podNames := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify Apps are downloaded by init container for apps", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Get instance of current SHC CR with latest config
			shcName := deployment.GetName() + "-shc"
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(shcName, shc)
			shReplicas := int(shc.Spec.Replicas)
			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify apps are copied to location
			allPodNames := testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND for app before downgrade", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			masterPodNames := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenvInstance.Log.Info("Verify Apps are NOT copied to /etc/apps on CM and Deployer for app before downgrade", "version", appVersion, "App List", appFileList)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), masterPodNames, appListV2, false, false)

			// Verify apps are installed cluster-wide
			testenvInstance.Log.Info("Verify Apps are installed on the pods by running Splunk CLI commands for app before downgrade", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

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

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), int(scaledIndexerReplicas), "")
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount)
			// Saving current V2 bundle hash for future comparision
			clusterManagerBundleHash := testenv.GetClusterManagerBundleHash(deployment)

			// Verify Apps are copied to correct location
			allPodNames = testenv.DumpGetPods(testenvInstance.GetName())
			testenvInstance.Log.Info("Verify Apps are copied to correct location based on Pod KIND after scaling up of indexers", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, true)

			// Verify apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify apps are NOT copied to /etc/apps on CM and Deployer after scaling up of indexers", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), masterPodNames, appListV2, false, false)

			// Verify Apps are installed cluster-wide
			testenvInstance.Log.Info("Verify apps are installed on the pods by running Splunk CLI commands after scaling up of indexers", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV2, true, "enabled", true, true)

			// Delete apps on S3 for new Apps
			testenvInstance.Log.Info("Delete Apps on S3 for", "Version", appVersion)
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil
			testenvInstance.Log.Info("Testing downgrade scenario")

			// Upload older versions of apps to S3
			appFileList = testenv.GetAppFileList(appListV1, 1)
			appVersion = "V1"
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify older version of apps are downloaded by init-container
			testenvInstance.Log.Info("Verify older version of apps are downloaded by init container", "version", appVersion)
			testenv.VerifyAppsDownloadedByInitContainer(deployment, testenvInstance, testenvInstance.GetName(), podNames, appFileList, initContDownloadLocation)

			// Verify bundle push status
			testenv.VerifyClusterManagerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), siteCount, clusterManagerBundleHash)
			testenv.VerifyDeployerBundlePush(deployment, testenvInstance, testenvInstance.GetName(), shReplicas)

			// Verify older version of apps are copied to correct location
			testenvInstance.Log.Info("Verify older version of apps are copied to correct location based on Pod KIND", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, true)

			// Verify older versions of apps are not copied in /etc/apps/ on CM and on Deployer (therefore not installed on Deployer and on CM)
			testenvInstance.Log.Info("Verify older versions of apps are NOT copied to /etc/apps on CM and Deployer", "version", appVersion)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), masterPodNames, appListV1, false, false)

			// Wait for the completion of app downgrade as it could take a while on indexers
			time.Sleep(2 * time.Minute)

			// Verify older versions of apps are installed cluster-wide
			testenvInstance.Log.Info("Verify older versions of apps are installed on the pods by running Splunk CLI commands", "version", appVersion)
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), allPodNames, appListV1, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (M4 - clustered indexer, search head cluster)", func() {
		It("integration, m4, appframework: can deploy a M4 SVA and have apps installed locally on CM and SHC Deployer", func() {

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
			err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpec, true, 10)
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster with App framework")

			// Ensure that the CM goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure SHC go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify Apps are downloaded by init-container
			initContDownloadLocation := "/init-apps/" + appSourceName
			podNames := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName()), fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			appFileList := testenv.GetAppFileList(appListV1, 1)
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
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)
			testenvInstance.Log.Info("Uploading apps S3 for", "version", appVersion)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Ensure that the CM goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

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
