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
package m4appfw

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	testenv "github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("m4appfw test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var uploadedApps []string
	var appSourceNameIdxc string
	var appSourceNameShc string
	var s3TestDirShc string
	var s3TestDirIdxc string
	var appSourceVolumeNameIdxc string
	var appSourceVolumeNameShc string
	var s3TestDirShcLocal string
	var s3TestDirIdxcLocal string
	var s3TestDirShcCluster string
	var s3TestDirIdxcCluster string
	var filePresentOnOperator bool

	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", "master"+testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		s3TestDirIdxc = "m4appfw-idxc-" + testenv.RandomDNSName(4)
		s3TestDirShc = "m4appfw-shc-" + testenv.RandomDNSName(4)
		appSourceVolumeNameIdxc = "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
		appSourceVolumeNameShc = "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
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

		if filePresentOnOperator {
			//Delete files from app-directory
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			podDownloadPath := filepath.Join(testenv.AppDownloadVolume, "test_file.img")
			testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
		}
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("smoke, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and upgrade them", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, volumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
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

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, mcName, "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			ClusterMasterBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############# UPGRADE APPS ################
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Verify MC is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## UPGRADE VERIFICATIONS ##########
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			mcAppSourceInfo.CrAppVersion = appVersion
			mcAppSourceInfo.CrAppList = appListV2
			mcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and downgrade them", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, volumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)
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
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console instance")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V2 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, mcName, "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			ClusterMasterBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############# DOWNGRADE APPS ################
			// Delete V2 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))

			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## DOWNGRADE VERIFICATIONS ########
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV1
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV1
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
			mcAppSourceInfo.CrAppVersion = appVersion
			mcAppSourceInfo.CrAppList = appListV1
			mcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled, install apps, scale up clusters, install apps on new pods, scale down", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			indexersPerSite := 1
			shReplicas := 3
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########### INITIAL VERIFICATIONS #########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### SCALING UP ################
			// Get instance of current Search Head Cluster CR with latest config
			err = deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)

			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale up Search Head Cluster
			defaultSHReplicas := shc.Spec.Replicas
			scaledSHReplicas := defaultSHReplicas + 1
			testcaseEnvInst.Log.Info("Scale up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of Search Head Cluster
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to scale up Search Head Cluster")

			// Ensure Search Head Cluster scales up and go to ScalingUp phase
			testenv.VerifySearchHeadClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingUp)

			// Get instance of current Indexer CR with latest config
			idxcName := deployment.GetName() + "-" + "site1"
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas := idxc.Spec.Replicas
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testcaseEnvInst.Log.Info("Scale up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to Scale Up Indexer Cluster")

			// Ensure Indexer cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingUp, idxcName)

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//######### SCALING UP VERIFICATIONS ########
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Listing the Search Head cluster pods to exclude them from the 'no pod reset' test as they are expected to be reset after scaling
			shcPodNames = []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			shcPodNames = append(shcPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, shcPodNames)

			//############### SCALING DOWN ##############
			// Get instance of current Search Head Cluster CR with latest config
			err = deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)

			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale down Search Head Cluster
			defaultSHReplicas = shc.Spec.Replicas
			scaledSHReplicas = defaultSHReplicas - 1
			testcaseEnvInst.Log.Info("Scaling down Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of Search Head Cluster
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to scale down Search Head Cluster")

			// Ensure Search Head Cluster scales down and go to ScalingDown phase
			testenv.VerifySearchHeadClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingDown)

			// Get instance of current Indexer CR with latest config
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas = idxc.Spec.Replicas
			scaledIndexerReplicas = defaultIndexerReplicas - 1
			testcaseEnvInst.Log.Info("Scaling down Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to Scale down Indexer Cluster")

			// Ensure Indexer cluster scales down and go to ScalingDown phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingDown, idxcName)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexer cluster go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//######### SCALING DOWN VERIFICATIONS ######
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, shcPodNames)
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA and have apps installed locally on Cluster Manager and Deployer", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 60)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			indexersPerSite := 1
			shReplicas := 3
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## UPGRADE VERIFICATIONS ############
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled for manual poll", func() {
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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			volumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, volumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 0)
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

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 0)

			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multi Site Indexer Cluster with App framework")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			ClusterMasterBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			// ############### UPGRADE APPS ################

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			appVersion = "V1"
			allPodNames := append(idxcPodNames, shcPodNames...)
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// ############ ENABLE MANUAL POLL ############
			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterMaster"] = strings.Replace(config.Data["ClusterMaster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["MonitoringConsole"] = strings.Replace(config.Data["MonitoringConsole"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			time.Sleep(2 * time.Minute)
			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ########## Verify Manual Poll disabled after the check #################

			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify config map set back to off after poll trigger for %s app", appVersion))
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())

			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off") && strings.Contains(config.Data["MonitoringConsole"], "status: off")).To(Equal(true), "Config map update not complete")

			// ############ VERIFY APPS UPDATED TO V2 #############
			appVersion = "V2"
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			mcAppSourceInfo.CrAppVersion = appVersion
			mcAppSourceInfo.CrAppList = appListV2
			mcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA and have apps installed and updated locally on Cluster Manager and Deployer via manual poll", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 0)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			appVersion = "V1"
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############ ENABLE MANUAL POLL ############
			appVersion = "V2"
			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterMaster"] = strings.Replace(config.Data["ClusterMaster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ########## Verify Manual Poll config map disabled after the poll is triggered #################

			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify config map set back to off after poll trigger for %s app", appVersion))
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())

			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			//########## UPGRADE VERIFICATIONS ############
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("m4, integration, masterappframeworkm4, appframework: can deploy a m4 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then upgrade them via a manual poll", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Split Applist into clusterlist and local list
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster for local and cluster scope
			   * Create app sources for Cluster Manager and Deployer with local and cluster scope
			   * Prepare and deploy m4 CRD with app framework and wait for the pods to be ready
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
			   * Wait for all m4 pods to be ready
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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirIdxcLocal = "m4appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListLocal list of apps to S3 (to be used for local install) for Shc
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirShcLocal = "m4appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirIdxcCluster = "m4appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirShcCluster = "m4appfw-cluster-" + testenv.RandomDNSName(4)
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

			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, s3TestDirIdxcLocal, 0)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameIdxcCluster, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3", testenv.GetDefaultS3Region())}
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, s3TestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, s3TestDirShcLocal, 0)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameShcCluster, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3", testenv.GetDefaultS3Region())}

			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, s3TestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameLocalIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcLocal, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			cmAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameClusterIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcCluster, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			shcAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameLocalShc, CrAppSourceVolumeName: appSourceVolumeNameShcLocal, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			shcAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameClusterShc, CrAppSourceVolumeName: appSourceVolumeNameShcCluster, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfoLocal, cmAppSourceInfoCluster, shcAppSourceInfoLocal, shcAppSourceInfoCluster}
			ClusterMasterBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Redefine app lists as LDAP app isn't in V1 apps
			appListLocal = appListV1[len(appListV1)/2:]
			appListCluster = appListV1[:len(appListV1)/2]

			// Upload appListLocal list of V2 apps to S3 (to be used for local install)
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			localappFileList = testenv.GetAppFileList(appListLocal)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of V2 apps to S3 (to be used for cluster-wide install)
			clusterappFileList = testenv.GetAppFileList(appListCluster)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster install (cluster scope)", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// ############ ENABLE MANUAL POLL ############

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterMaster"] = strings.Replace(config.Data["ClusterMaster"], "off", "on", 1)
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameClusterIdxc, clusterappFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// ########## Verify Manual Poll config map disabled after the poll is triggered #################

			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info("Verify config map set back to off after poll trigger for app", "version", appVersion)
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			//########## UPGRADE VERIFICATION #############
			cmAppSourceInfoLocal.CrAppVersion = appVersion
			cmAppSourceInfoLocal.CrAppList = appListLocal
			cmAppSourceInfoLocal.CrAppFileList = localappFileList
			cmAppSourceInfoCluster.CrAppVersion = appVersion
			cmAppSourceInfoCluster.CrAppList = appListCluster
			cmAppSourceInfoCluster.CrAppFileList = clusterappFileList
			shcAppSourceInfoLocal.CrAppVersion = appVersion
			shcAppSourceInfoLocal.CrAppList = appListLocal
			shcAppSourceInfoLocal.CrAppFileList = localappFileList
			shcAppSourceInfoCluster.CrAppVersion = appVersion
			shcAppSourceInfoCluster.CrAppList = appListCluster
			shcAppSourceInfoCluster.CrAppFileList = clusterappFileList
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfoLocal, cmAppSourceInfoCluster, shcAppSourceInfoLocal, shcAppSourceInfoCluster}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (M4) and App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4, add new apps to app source while install is in progress and have all apps installed locally on Cluster Manager and Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload big-size app to S3 for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy M4 CRD with app framework
			   * Verify app installation is in progress on Cluster Manager and Deployer
			   * Upload more apps from S3 during bigger app install
			   * Wait for polling interval to pass
			   * Verify all apps are installed on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
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

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Download all test apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to S3 for Cluster Manager
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Cluster Manager")
			s3TestDirIdxc = "m4appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big-size app to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Search Head Cluster")
			s3TestDirShc = "m4appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify App installation is in progress on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyComplete)

			// Upload more apps to S3 for Cluster Manager
			appList = testenv.ExtraApps
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Cluster Manager")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for  Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload more apps to S3 for Deployer
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Deployer")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for Deployer")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Verify all apps are installed on Cluster Manager
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Cluster Manager", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), cmPod, appList, true, "enabled", false, false)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			time.Sleep(60 * time.Second)
			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName()+"-shc", shc.Kind, appSourceNameShc, appFileList)

			// Verify all apps are installed on Deployer
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Deployer", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), deployerPod, appList, true, "enabled", false, false)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (M4) and App Framework", func() {
		It("smoke, m4, masterappframeworkm4, appframework: can deploy a M4, add new apps to app source while install is in progress and have all apps installed cluster-wide", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload big-size app to S3 for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   ############## VERIFICATIONS ################
			   * Verify App installation is in progress on Cluster Manager and Deployer
			   * Upload more apps from S3 during bigger app install
			   * Wait for polling interval to pass
			   * Verify all apps are installed on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "m4appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
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

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Download all test apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to S3 for Cluster Manager
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Cluster Manager")
			s3TestDirIdxc = "m4appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big-size app to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Search Head Cluster")
			s3TestDirShc = "m4appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify App installation is in progress
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyComplete)

			// Upload more apps to S3 for Cluster Manager
			appList = testenv.ExtraApps
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Cluster Manager")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload more apps to S3 for Deployer
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Deployer")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for Deployer")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Verify all apps are installed on indexers
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			idxcPodNames := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), indexersPerSite, true, siteCount)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on indexers", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), idxcPodNames, appList, true, "enabled", false, true)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName()+"-shc", shc.Kind, appSourceNameShc, appFileList)

			// Verify all apps are installed on Search Heads
			shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Search Heads", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), shcPodNames, appList, true, "enabled", false, true)

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled and reset operator pod while app install is in progress", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   * While app install is in progress, restart the operator
			   ########## VERIFICATIONS ##########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and installed on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ##################
			// Download all apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify App installation is in progress on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgInstallPending)

			// Delete Operator pod while Install in progress
			testenv.DeleteOperatorPod(testcaseEnvInst)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled and reset operator pod while app download is in progress", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   * While app download is in progress, restart the operator
			   ########## VERIFICATIONS ##########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and installed on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ##################
			// Download all apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify App Download is in progress on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgDownloadComplete, enterpriseApi.AppPkgDownloadPending)

			// Delete Operator pod while Install in progress
			testenv.DeleteOperatorPod(testcaseEnvInst)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled, install an app, then disable it by using a disabled version of the app and then remove it from app source", func() {

			/* Test Steps
			   ################## SETUP ##################
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
			   ############  Upload Disabled App ###########
			   * Download disabled app from s3
			   * Delete the app from s3
			   * Check for repo state in App Deployment Info
			*/

			//################## SETUP ##################
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATIONS ##########
			idxcPodNames := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############ Upload Disabled App ###########
			// Verify repo state on App to be disabled to be 1 (i.e app present on S3 bucket)
			appName := appListV1[0]
			appFileName := testenv.GetAppFileList([]string{appName})
			testenv.VerifyAppRepoState(ctx, deployment, testcaseEnvInst, cm.Name, cm.Kind, appSourceNameIdxc, 1, appFileName[0])

			// Download disabled version of app from S3
			testcaseEnvInst.Log.Info("Download disabled version of apps from S3 for this test")
			err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirDisabled, downloadDirV1, appFileName)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Upload disabled version of app to S3
			testcaseEnvInst.Log.Info("Upload disabled version of app to S3 for this test")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileName, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileName)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Wait for App state to update after config file change
			testenv.WaitforAppInstallState(ctx, deployment, testcaseEnvInst, idxcPodNames, testcaseEnvInst.GetName(), appName, "disabled", true)

			//Delete the file from s3
			s3Filepath := filepath.Join(s3TestDirIdxc, appFileName[0])
			err = testenv.DeleteFileOnS3(testS3Bucket, s3Filepath)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to delete %s app on S3 test directory", appFileName))

			// Verify repo state is set  to 2 (i.e app deleted from S3 bucket)
			testenv.VerifyAppRepoState(ctx, deployment, testcaseEnvInst, cm.Name, cm.Kind, appSourceNameIdxc, 2, appFileName[0])
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (M4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA, install apps via manual polling, switch to periodic polling, verify apps are not updated before the end of AppsRepoPollInterval, then updated after", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3
			   * Create app source with local scope for M4 SVA, AppsRepoPollInterval=0 to set apps polling as manual
			   * Prepare and deploy M4 CRD with app framework and wait for pods to be ready
			   ########## INITIAL VERIFICATION #############
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are installed locally on Cluster Manager and Deployer
			   * Verify status is 'OFF' in config map for Cluster Manager and Search Head Cluster
			   ######### SWITCH FROM MANUAL TO PERIODIC POLLING ############
			   * Set AppsRepoPollInterval to 180 seconds for Cluster Manager and Search Head Cluster
			   * Change status to 'ON' in config map for Cluster Manager and Search Head Cluster
			   ############### UPGRADE APPS ################
			   * Upgrade apps in app sources
			   * Wait for pods to be ready
			   ############ UPGRADE VERIFICATION ##########
			   * Verify apps are not updated before the end of AppsRepoPollInterval duration
			   * Verify apps are updated after the end of AppsRepoPollInterval duration
			*/

			//################## SETUP ####################
			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 0)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			// Verify status is 'OFF' in config map for Cluster Manager and Search Head Cluster
			testcaseEnvInst.Log.Info("Verify status is 'OFF' in config map for Cluster Master and Search Head Cluster")
			config, _ := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			//######### SWITCH FROM MANUAL TO PERIODIC POLLING ############
			// Get instance of current Cluster Manager CR with latest config
			cm = &enterpriseApi.ClusterMaster{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Failed to edit Cluster Master")

			// Set AppsRepoPollInterval for Cluster Manager to 180 seconds
			testcaseEnvInst.Log.Info("Set AppsRepoPollInterval for Cluster Master to 180 seconds")
			cm.Spec.AppFrameworkConfig.AppsRepoPollInterval = int64(180)
			err = deployment.UpdateCR(ctx, cm)
			Expect(err).To(Succeed(), "Failed to change AppsRepoPollInterval value for Cluster Master")

			// Get instance of current Search Head Cluster CR with latest config
			shc = &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)
			Expect(err).To(Succeed(), "Failed to edit Search Head Cluster")

			// Set AppsRepoPollInterval for Search Head Cluster to 180 seconds
			testcaseEnvInst.Log.Info("Set AppsRepoPollInterval for Search Head Cluster to 180 seconds")
			shc.Spec.AppFrameworkConfig.AppsRepoPollInterval = int64(180)
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to change AppsRepoPollInterval value for Search Head Cluster")

			// Change status to 'ON' in config map for Cluster Manager and Search Head Cluster
			testcaseEnvInst.Log.Info("Change status to 'ON' in config map for Cluster Master")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map")

			config.Data["ClusterMaster"] = strings.Replace(config.Data["ClusterMaster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map for Cluster Master")

			testcaseEnvInst.Log.Info("Change status to 'ON' in config map for Search Head Cluster")
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map for Search Head Cluster")

			// Wait 5 seconds to be sure reconcile caused by CR update and config map update are done
			testcaseEnvInst.Log.Info("Wait 5 seconds to be sure reconcile caused by CR update and config map update are done")
			time.Sleep(5 * time.Second)

			// Verify status is 'ON' in config map for Cluster Manager and Search Head Cluster
			testcaseEnvInst.Log.Info("Verify status is 'ON' in config map for Cluster Master and Search Head Cluster")
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["ClusterMaster"], "status: on") && strings.Contains(config.Data["SearchHeadCluster"], "status: on")).To(Equal(true), "Config map update not complete")

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## UPGRADE VERIFICATIONS ############
			testcaseEnvInst.Log.Info("Verify apps are not updated before the end of AppsRepoPollInterval duration")
			appVersion = "V1"
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Wait for the end of AppsRepoPollInterval duration
			testcaseEnvInst.Log.Info("Wait for the end of AppsRepoPollInterval duration")
			time.Sleep(2 * time.Minute)

			testcaseEnvInst.Log.Info("Verify apps are updated after the end of AppsRepoPollInterval duration")
			appVersion = "V2"
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled and update apps after app download is completed", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   * While app download is in progress, restart the operator
			   * While app download is completed, upload new versions of the apps
			   ######### VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied, installed on Search Heads and Indexers pods
			    ######### UPGRADE VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied, installed on Search Heads and Indexers pods
			*/

			//################## SETUP ##################
			// Download all apps from S3
			appVersion := "V1"
			appListV1 := []string{appListV1[0]}
			appFileList := testenv.GetAppFileList(appListV1)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 120)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 120)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify App Download is in progress on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyPending)

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appListV2 := []string{appListV2[0]}
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## VERIFICATIONS ##########
			appVersion = "V1"
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}, appListV1, false, "enabled", false, false)

			// Check for changes in App phase to determine if next poll has been triggered
			appFileList = testenv.GetAppFileList(appListV2)
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//############  UPGRADE VERIFICATIONS ############
			appVersion = "V2"
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("m4, integration, masterappframeworkm4, appframework: can deploy a M4 SVA and install a bigger volume of apps than the operator PV disk space", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload 15 apps of 100MB size each to S3 for Indexer Cluster and Search Head Cluster for cluster scope
			   * Create app sources for Cluster Manager and Deployer with cluster scope
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   ######### INITIAL VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify  apps are copied, installed on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Create a large file on Operator pod
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			err := testenv.CreateDummyFileOnOperator(ctx, deployment, opPod, testenv.AppDownloadVolume, "1G", "test_file.img")
			Expect(err).To(Succeed(), "Unable to create file on operator")
			filePresentOnOperator = true

			// Upload apps to S3 for Indexer Cluster
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			s3TestDirIdxc := "m4appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for Indexer Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search head Cluster", appVersion))
			s3TestDirShc := "m4appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 30

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecIdxc.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)
			appFrameworkSpecShc.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("integration, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled and delete apps from app directory when download is complete", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload big-size app to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   * When app download is complete, delete apps from app directory
			   ########## VERIFICATIONS ##########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and installed on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ##################
			// Download big size apps from S3
			appList := testenv.BigSingleApp
			appFileList := testenv.GetAppFileList(appList)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload big size app to S3 for Indexer Cluster
			appVersion := "V1"
			testcaseEnvInst.Log.Info("Upload big size app to S3 for Indexer Cluster")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big size to S3 test directory for Indexer Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big size app to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big size app to S3 for Search Head Cluster")
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big size to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify App Download is completed on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgPodCopyComplete, enterpriseApi.AppPkgPodCopyPending)

			//Delete apps from app directory when app download is complete
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			podDownloadPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameIdxc, testenv.AppInfo[appList[0]]["filename"])
			err = testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
			Expect(err).To(Succeed(), "Unable to delete file on pod")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("smoke, m4, masterappframeworkm4, appframework: can deploy a M4 SVA with App Framework enabled, install apps and check IsDeploymentInProgress for CM and SHC CR's", func() {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework
			   * Verify IsDeploymentInProgress is set
			   * Wait for the pods to be ready
			*/

			//################## SETUP ##################
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterMasterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify IsDeploymentInProgress Flag is set to true for Cluster Manager CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag for Cluster Manager")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, cm.Name, cm.Kind)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Verify IsDeploymentInProgress Flag is set to true for SHC CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag for SHC")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, shc.Name, shc.Kind)

			// Ensure the Indexers of all sites go to Ready phase
			testenv.IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Indexer Cluster configured as Multisite
			testenv.IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
		})
	})

})
