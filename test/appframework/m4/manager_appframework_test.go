// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.s
package m4appfw

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	testenv "github.com/splunk/splunk-operator/test/testenv"
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

	BeforeEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		var err error
		testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "")
		Expect(err).ToNot(HaveOccurred())

		s3TestDirIdxc = "m4appfw-idxc-" + testenv.RandomDNSName(4)
		s3TestDirShc = "m4appfw-shc-" + testenv.RandomDNSName(4)
		appSourceVolumeNameIdxc = "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
		appSourceVolumeNameShc = "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
	})

	AfterEach(NodeTimeout(testenv.SetupTeardownTimeout), func(ctx SpecContext) {
		Expect(testenv.TeardownAppFrameworkTestCaseEnv(ctx, testcaseEnvInst, deployment, testenv.CloudCleanup(ctx, cloudBackend, uploadedApps), filePresentOnOperator)).To(Succeed())
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (M4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled, install apps and upgrade them", Label("tier:e2e-pr", "sva:m4", "cloud:aws", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

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
			   ############# UPGRADE APPS ################
			   * Upgrade apps in app sources
			   ########## UPGRADE VERIFICATIONS ##########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			*/

			//################## SETUP ##################
			appFileList := testenv.GetAppFileList(appListV1)
			appVersion := "V1"

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App Framework")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// ############ Verify livenessProbe and readinessProbe config object and scripts############
			testcaseEnvInst.Log.Info("Get config map for livenessProbe and readinessProbe")
			ConfigMapName := enterprise.GetProbeConfigMapName(testcaseEnvInst.GetName())
			_, err = testenv.GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
			Expect(err).To(Succeed(), "Unable to get config map for livenessProbe and readinessProbe", "ConfigMap name", ConfigMapName)
			scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName(), enterprise.GetStartupScriptName()}
			allPods := testenv.DumpGetPods(testcaseEnvInst.GetName())
			Expect(testcaseEnvInst.VerifyFilesInDirectoryOnPod(ctx, deployment, allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)).To(Succeed(), "Files not found in directory on pod")

			//########## INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			clusterManagerBundleHash, err := testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

			//############# UPGRADE APPS ################
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			Expect(cloudBackend.DeleteFiles(ctx, uploadedApps)).To(Succeed(), "S3 file deletion failed")
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps for Monitoring Console

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## UPGRADE VERIFICATIONS ##########
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, clusterManagerBundleHash)
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled, install apps and downgrade them", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V2 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework and wait for the pods to be ready
			   ########## INITIAL VERIFICATIONS ##########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   ############ DOWNGRADE APPS ###############
			   * Downgrade apps in app sources
			   ########## DOWNGRADE VERIFICATIONS ########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			*/

			//################## SETUP ##################
			appFileList := testenv.GetAppFileList(appListV2)
			appVersion := "V2"

			// Upload V2 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App Framework")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			clusterManagerBundleHash, err := testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

			//############# DOWNGRADE APPS ################
			// Delete V2 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))

			Expect(cloudBackend.DeleteFiles(ctx, uploadedApps)).To(Succeed(), "S3 file deletion failed")
			uploadedApps = nil

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## DOWNGRADE VERIFICATIONS ########
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV1
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV1
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV1)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, clusterManagerBundleHash)
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled, install apps, scale up clusters, install apps on new pods, scale down", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "variant:manager", "feature:appframework", "feature:scaling"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

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
			   * Verify bundle push is successful
			   ############### SCALING UP ################
			   * Scale up Indexers and Search Head Cluster
			   ######### SCALING UP VERIFICATIONS ########
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are copied and installed on new Search Heads and Indexers pods
			   ############### SCALING DOWN ##############
			   * Scale down Indexers and Search Head Cluster
			   ######### SCALING DOWN VERIFICATIONS ######
			   * Verify Apps Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify apps are still copied and installed on all Search Heads and Indexers pods
			*/

			//################## SETUP ##################
			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			indexersPerSite := 1
			shReplicas := 3
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// Ingest data on Indexers
			for i := 1; i <= siteCount; i++ {
				podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), i, 0)
				logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
				Expect(testenv.CreateMockLogfile(logFile, 2000)).To(Succeed(), "Failed to create mock logfile")
				Expect(testenv.IngestFileViaMonitor(ctx, deployment, logFile, "main", podName)).To(Succeed(), "Failed to ingest file via monitor")
			}

			// ############ Verify livenessProbe and readinessProbe config object and scripts############
			testcaseEnvInst.Log.Info("Get config map for livenessProbe and readinessProbe")
			ConfigMapName := enterprise.GetProbeConfigMapName(testcaseEnvInst.GetName())
			_, err = testenv.GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
			Expect(err).To(Succeed(), "Unable to get config map for livenessProbe and readinessProbe", "ConfigMap name", ConfigMapName)

			//########### INITIAL VERIFICATIONS #########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

			//Delete configMap Object
			err = testenv.DeleteConfigMap(testcaseEnvInst.GetName(), ConfigMapName)
			Expect(err).To(Succeed(), "Unable to delete ConfigMao", "ConfigMap name", ConfigMapName)

			//############### SCALING UP ################
			// Get instance of current Search Head Cluster CR with latest config
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale up Search Head Cluster
			defaultSHReplicas := shc.Spec.Replicas
			scaledSHReplicas := defaultSHReplicas + 1
			testcaseEnvInst.Log.Info("Scale up Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of Search Head Cluster
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to scale up Search Head Cluster")

			// ScalingUp is transient, so verify the requested generation and
			// replica count were fully reconciled instead of waiting for that phase.
			Expect(testcaseEnvInst.WaitForSearchHeadClusterScaleComplete(ctx, deployment, int32(scaledSHReplicas))).To(Succeed(), "Search Head Cluster scale up did not complete")

			// Get instance of current Indexer CR with latest config
			idxcName := deployment.GetName() + "-" + "site1"
			idxc := &enterpriseApi.IndexerCluster{}
			Expect(deployment.GetInstance(ctx, idxcName, idxc)).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas := idxc.Spec.Replicas
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testcaseEnvInst.Log.Info("Scale up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to Scale Up Indexer Cluster")

			// Ensure Indexer cluster scales up and go to ScalingUp phase
			Expect(testcaseEnvInst.VerifyIndexerClusterPhase(ctx, deployment, enterpriseApi.PhaseScalingUp, idxcName)).To(Succeed(), "Indexer Cluster phase mismatch")

			// Ensure Indexer cluster go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Indexers not ready")

			// Ingest data on  new Indexers
			podName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, 1)
			logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
			Expect(testenv.CreateMockLogfile(logFile, 2000)).To(Succeed(), "Failed to create mock logfile")
			Expect(testenv.IngestFileViaMonitor(ctx, deployment, logFile, "main", podName)).To(Succeed(), "Failed to ingest file via monitor")

			// Ensure Search Head Cluster go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Search Head Cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Search for data on newly added indexer
			searchPod := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			indexerName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, 1)
			searchString := fmt.Sprintf("index=%s host=%s | stats count by host", "main", indexerName)
			searchResultsResp, err := testenv.PerformSearchSync(ctx, deployment, searchPod, searchString)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchPod, searchString)

			// Verify result.
			searchResponse := strings.Split(searchResultsResp, "\n")[0]
			var searchResults map[string]interface{}
			jsonErr := json.Unmarshal([]byte(searchResponse), &searchResults)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			testcaseEnvInst.Log.Info("Search results :", "searchResults", searchResults["result"])
			Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, searchPod)

			resultLine := searchResults["result"].(map[string]interface{})
			testcaseEnvInst.Log.Info("Sync Search results host count:", "count", resultLine["count"].(string), "host", resultLine["host"].(string))
			testHostname := strings.Compare(resultLine["host"].(string), indexerName)
			Expect(testHostname).To(Equal(0), "Incorrect search result hostname. Expect: %s Got: %s", indexerName, resultLine["host"].(string))

			//######### SCALING UP VERIFICATIONS ########
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// ############ Verify livenessProbe and readinessProbe config object and scripts############
			testcaseEnvInst.Log.Info("Get config map for livenessProbe and readinessProbe")
			_, err = testenv.GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
			Expect(err).To(Succeed(), "Unable to get config map for livenessProbe and readinessProbe", "ConfigMap name", ConfigMapName)
			scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName(), enterprise.GetStartupScriptName()}
			allPods := testenv.DumpGetPods(testcaseEnvInst.GetName())
			Expect(testcaseEnvInst.VerifyFilesInDirectoryOnPod(ctx, deployment, allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)).To(Succeed(), "Files not found in directory on pod")

			// Listing the Search Head cluster pods to exclude them from the 'no pod reset' test as they are expected to be reset after scaling
			shcPodNames = []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			shcPodNames = append(shcPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, shcPodNames)).To(Succeed(), "Unexpected pod reset detected")

			//############### SCALING DOWN ##############
			// Get instance of current Search Head Cluster CR with latest config
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale down Search Head Cluster
			defaultSHReplicas = shc.Spec.Replicas
			scaledSHReplicas = defaultSHReplicas - 1
			testcaseEnvInst.Log.Info("Scaling down Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

			// Update Replicas of Search Head Cluster
			shc.Spec.Replicas = int32(scaledSHReplicas)
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to scale down Search Head Cluster")

			Expect(testcaseEnvInst.WaitForSearchHeadClusterScaleComplete(ctx, deployment, int32(scaledSHReplicas))).To(Succeed(), "Search Head Cluster scale down did not complete")

			// Get instance of current Indexer CR with latest config
			Expect(deployment.GetInstance(ctx, idxcName, idxc)).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas = idxc.Spec.Replicas
			scaledIndexerReplicas = defaultIndexerReplicas - 1
			testcaseEnvInst.Log.Info("Scaling down Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to Scale down Indexer Cluster")

			// Ensure Search Head Cluster go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Search Head Cluster not ready")

			// Ensure Indexer cluster go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Indexers not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Search for data from removed indexer
			searchString = fmt.Sprintf("index=%s host=%s | stats count by host", "main", indexerName)
			searchResultsResp, err = testenv.PerformSearchSync(ctx, deployment, searchPod, searchString)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchPod, searchString)

			// Verify result.
			searchResponse = strings.Split(searchResultsResp, "\n")[0]
			jsonErr = json.Unmarshal([]byte(searchResponse), &searchResults)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			testcaseEnvInst.Log.Info("Search results :", "searchResults", searchResults["result"])
			Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, searchPod)

			resultLine = searchResults["result"].(map[string]interface{})
			testcaseEnvInst.Log.Info("Sync Search results host count:", "count", resultLine["count"].(string), "host", resultLine["host"].(string))
			testHostname = strings.Compare(resultLine["host"].(string), indexerName)
			Expect(testHostname).To(Equal(0), "Incorrect search result hostname. Expect: %s Got: %s", indexerName, resultLine["host"].(string))

			//######### SCALING DOWN VERIFICATIONS ######
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, shcPodNames)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA and have apps installed locally on Cluster Manager and Deployer", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

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
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 60)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			indexersPerSite := 1
			shReplicas := 3
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure M4 indexers and SHC are ready
			Expect(testcaseEnvInst.VerifyM4IndexersAndSHCReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 Indexers and SHC not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			Expect(cloudBackend.DeleteFiles(ctx, uploadedApps)).To(Succeed(), "S3 file deletion failed")
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure M4 indexers and SHC are ready
			Expect(testcaseEnvInst.VerifyM4IndexersAndSHCReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 Indexers and SHC not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## UPGRADE VERIFICATIONS ############
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled for manual poll", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {
			/* Test Steps
			   ################## SETUP ####################
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
			appFileList := testenv.GetAppFileList(appListV1)
			appVersion := "V1"

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 0)

			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with App Framework")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			clusterManagerBundleHash, err := testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

			// ############### UPGRADE APPS ################

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			appVersion = "V1"
			allPodNames := append(idxcPodNames, shcPodNames...)
			Eventually(func() error {
				return testcaseEnvInst.VerifyAppInstalled(ctx, deployment, testcaseEnvInst.GetName(), allPodNames, appListV1, true, "enabled", false, true)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "App installation verification failed")

			// ############ ENABLE MANUAL POLL ############
			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterManager"] = strings.Replace(config.Data["ClusterManager"], "off", "on", 1)
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure that the Cluster Manager goes to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Cluster Manager not ready")

			// Ensure the Indexers of all sites go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Indexers not ready")

			// Ensure Indexer cluster configured as multisite
			Expect(testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)).To(Succeed(), "Indexer Cluster multisite status verification failed")

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			Expect(err).To(Succeed(), "Timed out waiting for MonitoringConsole to reach Ready phase")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// ########## Verify Manual Poll disabled after the check #################

			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify config map set back to off after poll trigger for %s app", appVersion))
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())

			Expect(strings.Contains(config.Data["ClusterManager"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			//############### UPGRADE APPS ################
			appVersion = "V2"
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, clusterManagerBundleHash)
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA and have apps installed and updated locally on Cluster Manager and Deployer via manual poll", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

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
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 0)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure M4 indexers and SHC are ready
			Expect(testcaseEnvInst.VerifyM4IndexersAndSHCReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 Indexers and SHC not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			Expect(cloudBackend.DeleteFiles(ctx, uploadedApps)).To(Succeed(), "S3 file deletion failed")
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure M4 indexers and SHC are ready
			Expect(testcaseEnvInst.VerifyM4IndexersAndSHCReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 Indexers and SHC not ready")

			// ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			appVersion = "V1"
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// ############ ENABLE MANUAL POLL ############
			appVersion = "V2"
			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterManager"] = strings.Replace(config.Data["ClusterManager"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure that the Cluster Manager goes to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Cluster Manager not ready")

			// Ensure the Indexers of all sites go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Indexers not ready")

			// Ensure Indexer cluster configured as multisite
			Expect(testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)).To(Succeed(), "Indexer Cluster multisite status verification failed")

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure Search Head Cluster go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Search Head Cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			// ########## Verify Manual Poll disabled after the poll is triggered #################

			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify config map set back to off after poll trigger for %s app", appVersion))
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())

			Expect(strings.Contains(config.Data["ClusterManager"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			//########## UPGRADE VERIFICATIONS ############
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a m4 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then upgrade them via a manual poll", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumLongTimeout), func(ctx SpecContext) {

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
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListLocal list of apps to S3 (to be used for local install) for Shc
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirShcLocal = "m4appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirIdxcCluster = "m4appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirShcCluster = "m4appfw-cluster-" + testenv.RandomDNSName(4)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec
			appSourceNameLocalIdxc := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameLocalShc := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameClusterIdxc := "appframework-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameClusterShc := "appframework-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxcLocal := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShcLocal := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxcCluster := "appframework-test-volume-idxc-cluster-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShcCluster := "appframework-test-volume-shc-cluster-" + testenv.RandomDNSName(3)

			// Create App Framework Spec for Cluster manager with scope local and append cluster scope

			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, s3TestDirIdxcLocal, 0)
			volumeSpecCluster := testcaseEnvInst.GenerateVolumeSpecForProvider(ctx, appSourceVolumeNameIdxcCluster)
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, s3TestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App Framework Spec for Search Head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, s3TestDirShcLocal, 0)
			volumeSpecCluster = testcaseEnvInst.GenerateVolumeSpecForProvider(ctx, appSourceVolumeNameShcCluster)

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
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameLocalIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcLocal, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			cmAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameClusterIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcCluster, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			shcAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameLocalShc, CrAppSourceVolumeName: appSourceVolumeNameShcLocal, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			shcAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameClusterShc, CrAppSourceVolumeName: appSourceVolumeNameShcCluster, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfoLocal, cmAppSourceInfoCluster, shcAppSourceInfoLocal, shcAppSourceInfoCluster}
			clusterManagerBundleHash, err := testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

			//############### UPGRADE APPS ################
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			Expect(cloudBackend.DeleteFiles(ctx, uploadedApps)).To(Succeed(), "S3 file deletion failed")
			uploadedApps = nil

			// Redefine app lists as LDAP app isn't in V1 apps
			appListLocal = appListV1[len(appListV1)/2:]
			appListCluster = appListV1[:len(appListV1)/2]

			// Upload appListLocal list of V2 apps to S3 (to be used for local install)
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			localappFileList = testenv.GetAppFileList(appListLocal)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of V2 apps to S3 (to be used for cluster-wide install)
			clusterappFileList = testenv.GetAppFileList(appListCluster)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster install (cluster scope)", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// ############ ENABLE MANUAL POLL ############

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterManager"] = strings.Replace(config.Data["ClusterManager"], "off", "on", 1)
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameClusterIdxc, clusterappFileList)

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// ########## Verify Manual Poll config map disabled after the poll is triggered #################

			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info("Verify config map set back to off after poll trigger for app", "version", appVersion)
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["ClusterManager"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

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
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, clusterManagerBundleHash)
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (M4) and App Framework", func() {
		It("can deploy a M4, add new apps to app source while install is in progress and have all apps installed locally on Cluster Manager and Deployer", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "cloud:azure", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload big-size app to S3 for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy M4 CRD with app framework
			   * Verify app installation is in progress on Cluster Manager and Deployer
			   * Upload more apps from S3 during bigger app install
			   * Wait for polling interval to pass
			   * Verify all apps are installed on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Download all test apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			err := cloudBackend.DownloadFiles(ctx, testenv.AppLocationV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to S3 for Cluster Manager
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Cluster Manager")
			s3TestDirIdxc = "m4appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big-size app to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Search Head Cluster")
			s3TestDirShc = "m4appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify App installation is in progress on Cluster Manager
			Expect(testcaseEnvInst.VerifyAppState(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyComplete, testenv.AppStateVerificationTimeout)).To(Succeed(), "App state verification failed")

			// Upload more apps to S3 for Cluster Manager
			appList = testenv.ExtraApps
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Cluster Manager")
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for  Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload more apps to S3 for Deployer
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Deployer")
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for Deployer")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Ensure Cluster Manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Wait for polling interval to pass
			testcaseEnvInst.WaitForAppInstall(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Verify all apps are installed on Cluster Manager
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Cluster Manager", appList))
			Eventually(func() error {
				return testcaseEnvInst.VerifyAppInstalled(ctx, deployment, testcaseEnvInst.GetName(), cmPod, appList, true, "enabled", false, false)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "App installation verification failed")

			// Ensure Search Head Cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Wait for SearchHeadCluster to reach Ready phase
			err = testcaseEnvInst.WaitForSearchHeadClusterPhase(ctx, deployment, testcaseEnvInst.GetName(), shc.Name, enterpriseApi.PhaseReady, 60*time.Second)
			Expect(err).To(Succeed(), "Timed out waiting for SearchHeadCluster to reach Ready phase")

			// Wait for polling interval to pass
			testcaseEnvInst.WaitForAppInstall(ctx, deployment, deployment.GetName()+"-shc", shc.Kind, appSourceNameShc, appFileList)

			// Verify all apps are installed on Deployer
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Deployer", appList))
			Eventually(func() error {
				return testcaseEnvInst.VerifyAppInstalled(ctx, deployment, testcaseEnvInst.GetName(), deployerPod, appList, true, "enabled", false, false)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "App installation verification failed")
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (M4) and App Framework", func() {
		It("can deploy a M4, add new apps to app source while install is in progress and have all apps installed cluster-wide", Label("tier:e2e-pr", "sva:m4", "cloud:aws", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
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
			// Download all test apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			err := cloudBackend.DownloadFiles(ctx, testenv.AppLocationV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload big-size app to S3 for Cluster Manager
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Cluster Manager")
			s3TestDirIdxc = "m4appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big-size app to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Search Head Cluster")
			s3TestDirShc = "m4appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App framework")

			// Verify App installation is in progress
			Expect(testcaseEnvInst.VerifyAppState(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyComplete, testenv.AppStateVerificationTimeout)).To(Succeed(), "App state verification failed")

			// Upload more apps to S3 for Cluster Manager
			appList = testenv.ExtraApps
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Cluster Manager")
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload more apps to S3 for Deployer
			testcaseEnvInst.Log.Info("Upload more apps to S3 for Deployer")
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload more apps to S3 test directory for Deployer")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Ensure Cluster Manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Wait for polling interval to pass
			testcaseEnvInst.WaitForAppInstall(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure the Indexers of all sites go to Ready phase
			testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount)

			// Verify all apps are installed on indexers
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			idxcPodNames := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), indexersPerSite, true, siteCount)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on indexers", appList))
			Eventually(func() error {
				return testcaseEnvInst.VerifyAppInstalled(ctx, deployment, testcaseEnvInst.GetName(), idxcPodNames, appList, true, "enabled", false, true)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "App installation verification failed")

			// Ensure Search Head Cluster go to Ready phase
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Wait for polling interval to pass
			testcaseEnvInst.WaitForAppInstall(ctx, deployment, deployment.GetName()+"-shc", shc.Kind, appSourceNameShc, appFileList)

			// Verify all apps are installed on Search Heads
			shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Search Heads", appList))
			Eventually(func() error {
				return testcaseEnvInst.VerifyAppInstalled(ctx, deployment, testcaseEnvInst.GetName(), shcPodNames, appList, true, "enabled", false, true)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "App installation verification failed")

		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled and reset operator pod while app install is in progress", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "cloud:azure", "variant:manager", "feature:appframework"), Serial, NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

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
			*/

			//################## SETUP ##################
			// Download all apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			err := cloudBackend.DownloadFiles(ctx, testenv.AppLocationV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App Framework")

			// Verify App installation is in progress on Cluster Manager
			Expect(testcaseEnvInst.VerifyAppState(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyComplete, testenv.AppStateVerificationTimeout)).To(Succeed(), "App state verification failed")

			// Delete Operator pod while Install in progress
			Expect(testcaseEnvInst.DeleteOperatorPod()).To(Succeed(), "Failed to delete operator pod")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled and reset operator pod while app download is in progress", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "cloud:azure", "variant:manager", "feature:appframework"), Serial, NodeTimeout(testenv.LongTimeout), func(ctx SpecContext) {

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
			*/

			//################## SETUP ##################
			// Download all apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			err := cloudBackend.DownloadFiles(ctx, testenv.AppLocationV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App Framework")

			// Verify App Download is in progress on Cluster Manager
			Expect(testcaseEnvInst.VerifyAppState(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgDownloadComplete, enterpriseApi.AppPkgDownloadPending, testenv.AppStateVerificationTimeout)).To(Succeed(), "App state verification failed")

			// Delete Operator pod while Install in progress
			Expect(testcaseEnvInst.DeleteOperatorPod()).To(Succeed(), "Failed to delete operator pod")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled, install an app, then disable it by using a disabled version of the app and then remove it from app source", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

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
			   * Disable the app
			   * Delete the app from s3
			   * Check for repo state in App Deployment Info
			*/

			//################## SETUP ##################
			appFileList := testenv.GetAppFileList(appListV1)
			appVersion := "V1"

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App Framework")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATIONS ##########
			idxcPodNames := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify repo state on App to be disabled to be 1 (i.e app present on S3 bucket)
			appName := appListV1[0]
			appFileName := testenv.GetAppFileList([]string{appName})
			Expect(testcaseEnvInst.VerifyAppRepoState(ctx, deployment, cm.Name, cm.Kind, appSourceNameIdxc, 1, appFileName[0])).To(Succeed(), "App repo state verification failed")

			// Disable the app
			err = cloudBackend.DisableApps(ctx, downloadDirV1, appFileName, s3TestDirIdxc)
			Expect(err).To(Succeed(), "Unable to disable apps on S3")

			// Check for changes in App phase to determine if next poll has been triggered
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileName)

			// Ensure Cluster Manager goes to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Cluster Manager not ready")

			// Ensure the Indexers of all sites go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Indexers not ready")

			// Ensure Indexer Cluster configured as Multisite
			Expect(testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)).To(Succeed(), "Indexer Cluster multisite status verification failed")

			// Wait for App state to update after config file change
			testcaseEnvInst.WaitforAppInstallState(ctx, deployment, idxcPodNames, testcaseEnvInst.GetName(), appName, "disabled", true)

			// Delete the file from S3
			s3Filepath := filepath.Join(s3TestDirIdxc, appFileName[0])
			err = cloudBackend.DeleteFile(ctx, s3Filepath)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to delete %s app on S3 test directory", appFileName))

			// Verify repo state is set to 2 (i.e app deleted from S3 bucket)
			Expect(testcaseEnvInst.VerifyAppRepoState(ctx, deployment, cm.Name, cm.Kind, appSourceNameIdxc, 2, appFileName[0])).To(Succeed(), "App repo state verification failed")
		})
	})

	Context("Multi Site Indexer Cluster with Search Head Cluster (M4) with App Framework", func() {
		It("can deploy a M4 SVA, install apps via manual polling, switch to periodic polling, verify apps are not updated before the end of AppsRepoPollInterval, then updated after", Label("tier:e2e-full", "sva:m4", "cloud:aws", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

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
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 0)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure M4 indexers and SHC are ready
			Expect(testcaseEnvInst.VerifyM4IndexersAndSHCReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 Indexers and SHC not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATION #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")

			// Verify status is 'OFF' in config map for Cluster Manager and Search Head Cluster
			testcaseEnvInst.Log.Info("Verify status is 'OFF' in config map for Cluster Manager and Search Head Cluster")
			config, _ := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["ClusterManager"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			//######### SWITCH FROM MANUAL TO PERIODIC POLLING ############
			// Get instance of current Cluster Manager CR with latest config
			cm = &enterpriseApi.ClusterManager{}
			Expect(deployment.GetInstance(ctx, deployment.GetName(), cm)).To(Succeed(), "Failed to get instance of Cluster Manager")

			// Set AppsRepoPollInterval for Cluster Manager to 180 seconds
			testcaseEnvInst.Log.Info("Set AppsRepoPollInterval for Cluster Manager to 180 seconds")
			cm.Spec.AppFrameworkConfig.AppsRepoPollInterval = int64(180)
			err = deployment.UpdateCR(ctx, cm)
			Expect(err).To(Succeed(), "Failed to change AppsRepoPollInterval value for Cluster Manager")

			// Get instance of current Search Head Cluster CR with latest config
			shc = &enterpriseApi.SearchHeadCluster{}
			Expect(deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Set AppsRepoPollInterval for Search Head Cluster to 180 seconds
			testcaseEnvInst.Log.Info("Set AppsRepoPollInterval for Search Head Cluster to 180 seconds")
			shc.Spec.AppFrameworkConfig.AppsRepoPollInterval = int64(180)
			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Failed to change AppsRepoPollInterval value for Search Head Cluster")

			// Wait for the poll-interval spec change to be reconciled into status before
			// flipping the manual-update configmap. Otherwise a reconcile still running
			// against the stale status (AppsRepoStatusPollInterval == 0, i.e. manual mode)
			// observes the configmap flip to "on" and immediately resets it to "off",
			// racing the assertion below.
			testcaseEnvInst.Log.Info("Wait for Cluster Manager status to reflect new AppsRepoPollInterval")
			Eventually(func() int64 {
				_ = deployment.GetInstance(ctx, cm.Name, cm)
				return cm.Status.AppContext.AppsRepoStatusPollInterval
			}, 5*time.Minute, testenv.PollInterval).Should(Equal(int64(180)), "Cluster Manager status did not reflect new AppsRepoPollInterval")

			testcaseEnvInst.Log.Info("Wait for Search Head Cluster status to reflect new AppsRepoPollInterval")
			Eventually(func() int64 {
				_ = deployment.GetInstance(ctx, shc.Name, shc)
				return shc.Status.AppContext.AppsRepoStatusPollInterval
			}, 5*time.Minute, testenv.PollInterval).Should(Equal(int64(180)), "Search Head Cluster status did not reflect new AppsRepoPollInterval")

			// Change status to 'ON' in config map for Cluster Manager and Search Head Cluster
			testcaseEnvInst.Log.Info("Change status to 'ON' in config map for Cluster Manager")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map")

			config.Data["ClusterManager"] = strings.Replace(config.Data["ClusterManager"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map for Cluster Manager")

			testcaseEnvInst.Log.Info("Change status to 'ON' in config map for Search Head Cluster")
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map for Search Head Cluster")

			// Wait for ClusterManager to reach Ready phase
			testcaseEnvInst.Log.Info("Wait for ClusterManager and SearchHeadCluster to reach Ready phase")
			err = testcaseEnvInst.WaitForClusterManagerPhase(ctx, deployment, testcaseEnvInst.GetName(), cm.Name, enterpriseApi.PhaseReady, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for ClusterManager to reach Ready phase")

			// Verify status is 'ON' in config map for Cluster Manager and Search Head Cluster.
			// Poll rather than one-shot check: the two sequential UpdateCR calls above race
			// with configmap read-after-write consistency, the same class of race already
			// guarded against for the AppsRepoStatusPollInterval check above.
			testcaseEnvInst.Log.Info("Verify status is 'ON' in config map for Cluster Manager and Search Head Cluster")
			Eventually(func() bool {
				config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
				return strings.Contains(config.Data["ClusterManager"], "status: on") && strings.Contains(config.Data["SearchHeadCluster"], "status: on")
			}, 5*time.Minute, testenv.PollInterval).Should(Equal(true), "Config map update not complete")

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			Expect(cloudBackend.DeleteFiles(ctx, uploadedApps)).To(Succeed(), "S3 file deletion failed")
			uploadedApps = nil

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Get Pod age to check for pod resets later
			splunkPodUIDs = testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## UPGRADE VERIFICATIONS ############
			testcaseEnvInst.Log.Info("Verify apps are not updated before the end of AppsRepoPollInterval duration")
			appVersion = "V1"
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Wait for app repo state to change, indicating poll interval has completed
			testcaseEnvInst.Log.Info("Wait for app repo state to change after AppsRepoPollInterval")
			err = testcaseEnvInst.WaitForAppRepoStateChange(ctx, deployment, cm.Name, cm.Kind, appSourceNameIdxc, appListV2, 1, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for Cluster Manager apps to update after poll interval")
			err = testcaseEnvInst.WaitForAppRepoStateChange(ctx, deployment, shc.Name, shc.Kind, appSourceNameShc, appListV2, 1, 5*time.Minute)
			Expect(err).To(Succeed(), "Timed out waiting for Search Head Cluster apps to update after poll interval")

			testcaseEnvInst.Log.Info("Verify apps are updated after the end of AppsRepoPollInterval duration")
			appVersion = "V2"
			cmAppSourceInfo.CrAppVersion = appVersion
			cmAppSourceInfo.CrAppList = appListV2
			cmAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			shcAppSourceInfo.CrAppVersion = appVersion
			shcAppSourceInfo.CrAppList = appListV2
			shcAppSourceInfo.CrAppFileList = testenv.GetAppFileList(appListV2)
			allAppSourceInfo = []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled and update apps after app download is completed", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "cloud:azure", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

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
			err := cloudBackend.DownloadFiles(ctx, testenv.AppLocationV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 120)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 120)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App Framework")

			// Verify App Download is in progress on Cluster Manager
			Expect(testcaseEnvInst.VerifyAppState(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyPending, testenv.AppStateVerificationTimeout)).To(Succeed(), "App state verification failed")

			// Upload V2 apps to S3 for Indexer Cluster
			appVersion = "V2"
			appListV2 := []string{appListV2[0]}
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## VERIFICATIONS ##########
			appVersion = "V1"
			Eventually(func() error {
				return testcaseEnvInst.VerifyAppInstalled(ctx, deployment, testcaseEnvInst.GetName(), []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}, appListV1, false, "enabled", false, false)
			}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "App installation verification failed")

			// Check for changes in App phase to determine if next poll has been triggered
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Cluster Manager not ready")

			// Ensure Search Head Cluster go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Search Head Cluster not ready")

			// Ensure the Indexers of all sites go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Indexers not ready")

			// Ensure Indexer Cluster configured as Multisite
			Expect(testcaseEnvInst.VerifyIndexerClusterMultisiteStatus(ctx, deployment, siteCount)).To(Succeed(), "Indexer Cluster multisite status verification failed")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			//############  UPGRADE VERIFICATIONS ############
			appVersion = "V2"
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA and install a bigger volume of apps than the operator PV disk space", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "cloud:azure", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload 15 apps of 100MB size each to S3 for Indexer Cluster and Search Head Cluster for cluster scope
			   * Create app sources for Cluster Master and Deployer with cluster scope
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
			appVersion := "V1"
			opPod := testcaseEnvInst.GetOperatorPodName()
			err := testenv.CreateDummyFileOnOperator(ctx, deployment, opPod, testenv.AppDownloadVolume, "1G", "test_file.img")
			Expect(err).To(Succeed(), "Unable to create file on operator")
			filePresentOnOperator = true

			// Download apps for test
			appList := testenv.PVTestApps
			appFileList := testenv.GetAppFileList(appList)
			err = cloudBackend.DownloadFiles(ctx, testenv.PVTestAppsLocation, downloadDirPVTestApps, appFileList)
			Expect(err).To(Succeed(), "Unable to download app files")

			// Upload apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			s3TestDirIdxc := "m4appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirPVTestApps)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			s3TestDirShc := "m4appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirPVTestApps)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 30

			// Create App Framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecIdxc.MaxConcurrentAppDownloads = int64(maxConcurrentAppDownloads)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)
			appFrameworkSpecShc.MaxConcurrentAppDownloads = int64(maxConcurrentAppDownloads)

			// Deploy Multisite Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Cluster Manager not ready")

			// Ensure Search Head Cluster go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Search Head Cluster not ready")

			// Ensure the Indexers of all sites go to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyIndexersReady(ctx, deployment, siteCount) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Indexers not ready")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled and delete apps from app directory when download is complete", Label("tier:e2e-full", "sva:m4", "cloud:aws", "cloud:gcp", "cloud:azure", "variant:manager", "feature:appframework"), NodeTimeout(testenv.MediumTimeout), func(ctx SpecContext) {

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
			*/

			//################## SETUP ##################
			// Download big size apps from S3
			appVersion := "V1"
			appList := testenv.BigSingleApp
			appFileList := testenv.GetAppFileList(appList)
			err := cloudBackend.DownloadFiles(ctx, testenv.AppLocationV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload big size app to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info("Upload big size app to S3 for Indexer Cluster")
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big size to S3 test directory for Indexer Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big size app to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big size app to S3 for Search Head Cluster")
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big size to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			shReplicas := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App Framework")

			// Verify App Download is completed on Cluster Manager
			Expect(testcaseEnvInst.VerifyAppState(ctx, deployment, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgPodCopyComplete, enterpriseApi.AppPkgPodCopyPending, testenv.AppStateVerificationTimeout)).To(Succeed(), "App state verification failed")

			//Delete apps from app directory when app download is complete
			opPod := testcaseEnvInst.GetOperatorPodName()
			podDownloadPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameIdxc, testenv.AppInfo[appList[0]]["filename"])
			err = testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
			Expect(err).To(Succeed(), "Unable to delete file on pod")

			// Ensure M4 cluster is ready
			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")

			// Get Pod age to check for pod resets later
			splunkPodUIDs := testenv.GetPodUIDs(testcaseEnvInst.GetName())

			//########## VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexersPerSite, CrMultisite: true, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			_, err = testcaseEnvInst.VerifyAppFrameworkState(ctx, deployment, allAppSourceInfo, splunkPodUIDs, "")
			Expect(err).To(Succeed(), "Failed to verify app framework state")

			// Verify no pods reset by checking the pod age
			Expect(testcaseEnvInst.VerifyNoPodResetByUID(ctx, splunkPodUIDs, nil)).To(Succeed(), "Unexpected pod reset detected")
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (m4) with App Framework", func() {
		It("can deploy a M4 SVA with App Framework enabled, install apps and check IsDeploymentInProgress for CM and SHC CR's", Label("tier:e2e-pr", "sva:m4", "cloud:aws", "variant:manager", "feature:appframework"), NodeTimeout(testenv.LongTimeout), func(ctx SpecContext) {

			/* Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to S3 for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy M4 CRD with app framework
			   * Verify IsDeploymentInProgress is set
			   * Wait for the pods to be ready
			*/

			//################## SETUP ##################
			appFileList := testenv.GetAppFileList(appListV1)
			appVersion := "V1"

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := cloudBackend.UploadFiles(ctx, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster %s", appVersion, testS3Bucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = cloudBackend.UploadFiles(ctx, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App Framework Spec for M4
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testcaseEnvInst.GenerateAppFrameworkSpec(ctx, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy M4 CRD
			testcaseEnvInst.Log.Info("Deploy Multisite Indexer Cluster with Search Head Cluster")
			siteCount := 3
			indexersPerSite := 1
			cm, _, shc, err := deployment.DeployMultisiteClusterWithSearchHeadAndAppFramework(ctx, deployment.GetName(), indexersPerSite, siteCount, appFrameworkSpecIdxc, appFrameworkSpecShc, true, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Multisite Indexer Cluster and Search Head Cluster with App Framework")

			// Verify IsDeploymentInProgress Flag is set to true for Cluster Master CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag for Cluster Manager")
			Expect(testcaseEnvInst.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, cm.Name, cm.Kind)).To(Succeed(), "IsDeploymentInProgress flag not set")

			// Ensure that the Cluster Manager goes to Ready phase
			Eventually(func() error { return testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "Cluster Manager not ready")

			// Verify IsDeploymentInProgress Flag is set to true for SHC CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag for SHC")
			Expect(testcaseEnvInst.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, shc.Name, shc.Kind)).To(Succeed(), "IsDeploymentInProgress flag not set")

			Expect(testcaseEnvInst.VerifyM4ClusterReady(ctx, deployment, siteCount, testcaseEnvInst.VerifyClusterManagerReady)).To(Succeed(), "M4 cluster not ready")

			// Verify RF SF is met
			Expect(testcaseEnvInst.VerifyRFSFMet(ctx, deployment)).To(Succeed(), "RF/SF not met")
		})
	})
})
