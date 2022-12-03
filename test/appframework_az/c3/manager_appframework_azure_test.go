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
package azurec3appfw

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	testenv "github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("c3appfw test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var azTestDir string
	var azTestDirShc string
	var azTestDirIdxc string
	var azTestDirShcLocal string
	var azTestDirIdxcLocal string
	var azTestDirShcCluster string
	var azTestDirIdxcCluster string
	var appSourceNameIdxc string
	var appSourceNameShc string
	var uploadedApps []string
	var filePresentOnOperator bool

	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}

		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}

		// Delete files uploaded to Azure
		if !testcaseEnvInst.SkipTeardown {
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
		}

		if filePresentOnOperator {
			// Delete files from app-directory
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			podDownloadPath := filepath.Join(testenv.AppDownloadVolume, "test_file.img")
			testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
		}
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("smoke, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled, install apps then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V1 apps to Azure for Indexer Cluster and Search Head Cluster
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
			   * Upload V2 apps on Azure
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
			// Upload V1 apps to Azure for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			azTestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)

			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
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

			// Upload V1 apps to Azure for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "", deployerName)

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Verify no SH in disconnected status is present on CM
			testenv.VerifyNoDisconnectedSHPresentOnCM(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//######### INITIAL VERIFICATIONS #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			clusterManagerBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Upload V2 apps to Azure for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to Azure for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			//uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirMC, appFileList)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ FINAL VERIFICATIONS ############
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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, clusterManagerBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure, azure_sanity: can deploy a C3 SVA with App Framework enabled, install apps then downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V2 apps to Azure for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V2 apps to Azure for Indexer Cluster and Search Head Cluster
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
			   * Upload V1 apps on Azure
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
			// Upload V2 apps to Azure for Monitoring Console
			appVersion := "V2"
			appFileList := testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			azTestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)

			// Monitoring Console AppFramework Spec
			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
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

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V2 apps to Azure for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "", deployerName)

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Wait for Custom resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########### INITIAL VERIFICATIONS ###########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			clusterManagerBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############## DOWNGRADE APPS ###############
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Upload V1 apps to Azure for Indexer Cluster
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexers", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexers", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########### FINAL VERIFICATIONS #############
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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, clusterManagerBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled, install apps, scale up clusters, install apps on new pods, scale down", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V1 apps on Azure for Indexer Cluster and Search Head Cluster
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
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			azTestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)

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

			// Upload V1 apps to Azure for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## INITIAL VERIFICATIONS ############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			time.Sleep(60 * time.Second)
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//#############  SCALING UP ###################
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
			idxcName := deployment.GetName() + "-idxc"
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Failed to get instance of Indexer Cluster")
			defaultIndexerReplicas := idxc.Spec.Replicas
			scaledIndexerReplicas := defaultIndexerReplicas + 1
			testcaseEnvInst.Log.Info("Scale up Indexer Cluster", "Current Replicas", defaultIndexerReplicas, "New Replicas", scaledIndexerReplicas)

			// Update Replicas of Indexer Cluster
			idxc.Spec.Replicas = int32(scaledIndexerReplicas)
			err = deployment.UpdateCR(ctx, idxc)
			Expect(err).To(Succeed(), "Failed to scale up Indexer Cluster")

			// Ensure Indexer Cluster scales up and go to ScalingUp phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingUp, idxcName)

			// Ensure Indexer Cluster go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify New Indexer On Cluster Manager
			indexerName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas-1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Checking for New Indexer %s On Cluster Manager", indexerName))
			Expect(testenv.CheckIndexerOnCM(ctx, deployment, indexerName)).To(Equal(true))

			// Ingest data on Indexers
			for i := 0; i < int(scaledIndexerReplicas); i++ {
				podName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), i)
				logFile := fmt.Sprintf("test-log-%s.log", testenv.RandomDNSName(3))
				testenv.CreateMockLogfile(logFile, 2000)
				testenv.IngestFileViaMonitor(ctx, logFile, "main", podName, deployment)
			}

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Search for data on newly added indexer
			searchPod := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), scaledSHReplicas-1)
			searchString := fmt.Sprintf("index=%s host=%s | stats count by host", "main", indexerName)
			searchResultsResp, err := testenv.PerformSearchSync(ctx, searchPod, searchString, deployment)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchPod, searchString)

			// Verify result
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

			//########## SCALING UP VERIFICATIONS #########
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			shcPodNames = []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			shcPodNames = append(shcPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, shcPodNames)

			//############### SCALING DOWN ################
			// Get instance of current Search Head Cluster CR with latest config
			shc = &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)

			Expect(err).To(Succeed(), "Failed to get instance of Search Head Cluster")

			// Scale down Search Head Cluster
			defaultSHReplicas = shc.Spec.Replicas
			scaledSHReplicas = defaultSHReplicas - 1
			testcaseEnvInst.Log.Info("Scale down Search Head Cluster", "Current Replicas", defaultSHReplicas, "New Replicas", scaledSHReplicas)

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

			// Ensure Indexer Cluster scales down and go to ScalingDown phase
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, enterpriseApi.PhaseScalingDown, idxcName)

			// Ensure Indexer Cluster go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Search for data from removed indexer
			searchPod = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), scaledSHReplicas-1)
			searchString = fmt.Sprintf("index=%s host=%s | stats count by host", "main", indexerName)
			searchResultsResp, err = testenv.PerformSearchSync(ctx, searchPod, searchString, deployment)
			Expect(err).To(Succeed(), "Failed to execute search '%s' on pod %s", searchPod, searchString)

			// Verify result
			searchResponse = strings.Split(searchResultsResp, "\n")[0]
			jsonErr = json.Unmarshal([]byte(searchResponse), &searchResults)
			Expect(jsonErr).To(Succeed(), "Failed to unmarshal JSON Search Results from response '%s'", searchResultsResp)

			testcaseEnvInst.Log.Info("Search results :", "searchResults", searchResults["result"])
			Expect(searchResults["result"]).ShouldNot(BeNil(), "No results in search response '%s' on pod %s", searchResults, searchPod)

			resultLine = searchResults["result"].(map[string]interface{})
			testcaseEnvInst.Log.Info("Sync Search results host count:", "count", resultLine["count"].(string), "host", resultLine["host"].(string))
			testHostname = strings.Compare(resultLine["host"].(string), indexerName)
			Expect(testHostname).To(Equal(0), "Incorrect search result hostname. Expect: %s Got: %s", indexerName, resultLine["host"].(string))

			//######## SCALING DOWN VERIFICATIONS #########
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, shcPodNames)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure, azure_sanity: can deploy a C3 SVA and have apps installed locally on Cluster Manager and Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure
			   * Create app source with local scope for C3 SVA (Cluster Manager and Deployer)
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ############# INITIAL VERIFICATIONS ##########
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
			// Upload V1 apps to Azure for Indexer Cluster
			appVersion := "V1"
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############## INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete V1 apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to Azure
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########### UPGRADE VERIFICATIONS ###########
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

	XContext("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA and have ES app installed on Search Head Cluster", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload ES app to Azure
			   * Create App Source with 'ScopeClusterWithPreConfig' scope for C3 SVA
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ################## VERIFICATION #############
			   * Verify ES app is installed on Deployer and on Search Heads
			*/

			//################## SETUP ####################
			// Download ES app from Azure
			testcaseEnvInst.Log.Info("Download ES app from Azure")
			esApp := []string{"SplunkEnterpriseSecuritySuite"}
			appFileList := testenv.GetAppFileList(esApp)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download ES app file from Azure")

			// Create local directory for file download
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)

			// Upload ES app to Azure
			testcaseEnvInst.Log.Info("Upload ES app to Azure")
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDir, appFileList)
			Expect(err).To(Succeed(), "Unable to upload ES app to Azure test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName := "appframework-shc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeClusterWithPreConfig, appSourceName, azTestDirShc, 60)

			// Deploy C3 SVA
			// Deploy the Cluster Manager
			testcaseEnvInst.Log.Info("Deploy Cluster Manager")
			_, err = deployment.DeployClusterMaster(ctx, deployment.GetName(), "", "", "")
			Expect(err).To(Succeed(), "Unable to deploy Cluster Manager")

			// Deploy the Indexer Cluster
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster")
			indexerReplicas := 3
			_, err = deployment.DeployIndexerCluster(ctx, deployment.GetName()+"-idxc", deployment.GetName(), indexerReplicas, deployment.GetName(), "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster")

			// Deploy the Search Head Cluster
			testcaseEnvInst.Log.Info("Deploy Search Head Cluster")
			shSpec := enterpriseApi.SearchHeadClusterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
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
			_, err = deployment.DeploySearchHeadClusterWithGivenSpec(ctx, deployment.GetName()+"-shc", shSpec)
			Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//################## VERIFICATIONS #############
			// Verify ES is downloaded
			testcaseEnvInst.Log.Info("Verify ES app is downloaded on Deployer")
			initContDownloadLocation := testenv.AppStagingLocOnPod + appSourceName
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testenv.VerifyAppsDownloadedOnContainer(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), deployerPod, appFileList, initContDownloadLocation)

			// Verify ES app is installed locally on Deployer
			testcaseEnvInst.Log.Info("Verify ES app is installed locally on Deployer")
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), deployerPod, esApp, true, "disabled", false, false)

			// Verify ES is installed on Search Heads
			testcaseEnvInst.Log.Info("Verify ES app is installed on Search Heads")
			podNames := []string{}
			for i := 0; i < int(shSpec.Replicas); i++ {
				sh := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				podNames = append(podNames, string(sh))
			}
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podNames, esApp, true, "enabled", false, true)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration,managerappframeworkazurec3, appframeworkazure, azure_sanity: can deploy a C3 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Split Applist into clusterlist and local list
			   * Upload V1 apps to Azure for Indexer Cluster and Search Head Cluster for local and cluster scope
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
			   * Upload V2 apps on Azure
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

			// Upload appListLocal list of apps to Azure (to be used for local install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))

			azTestDirIdxcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for local install for Indexers", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))

			azTestDirShcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for local install for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to Azure (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for cluster-wide install (cluster scope)", appVersion))

			azTestDirIdxcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to Azure (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for cluster-wide install (cluster scope)", appVersion))

			azTestDirShcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to Azure test directory", appVersion))
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

			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, azTestDirIdxcLocal, 60)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpecAzure(appSourceVolumeNameIdxcCluster, testenv.GetAzureEndpoint(ctx), testcaseEnvInst.GetIndexSecretName(), "azure", "blob")}

			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, azTestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, azTestDirShcLocal, 60)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpecAzure(appSourceVolumeNameShcCluster, testenv.GetAzureEndpoint(ctx), testcaseEnvInst.GetIndexSecretName(), "azure", "blob")}
			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, azTestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameLocalIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcLocal, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			cmAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameClusterIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcCluster, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameLocalShc, CrAppSourceVolumeName: appSourceVolumeNameShcLocal, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			shcAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameClusterShc, CrAppSourceVolumeName: appSourceVolumeNameShcCluster, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfoLocal, cmAppSourceInfoCluster, shcAppSourceInfoLocal, shcAppSourceInfoCluster}
			clusterManagerBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// Redefine app lists as LDAP app isn't in V1 apps
			appListLocal = appListV1[len(appListV1)/2:]
			appListCluster = appListV1[:len(appListV1)/2]

			// Upload appListLocal list of V2 apps to Azure (to be used for local install)
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))
			localappFileList = testenv.GetAppFileList(appListLocal)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for local install for Indexers", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for local install for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of V2 apps to Azure (to be used for cluster-wide install)
			clusterappFileList = testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameClusterIdxc, clusterappFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, clusterManagerBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration,managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Split Applist into clusterlist and local list
			   * Upload V2 apps to Azure for Indexer Cluster and Search Head Cluster for local and cluster scope
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
			   * Upload V1 apps on Azure
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

			// Upload appListLocal list of apps to Azure (to be used for local install) for Idxc
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))
			azTestDirIdxcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListLocal list of apps to Azure (to be used for local install) for Shc
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))
			azTestDirShcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to Azure (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for cluster-wide install (cluster scope)", appVersion))
			azTestDirIdxcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to Azure (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for cluster-wide install (cluster scope)", appVersion))
			azTestDirShcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to Azure test directory", appVersion))
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
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, azTestDirIdxcLocal, 60)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpecAzure(appSourceVolumeNameIdxcCluster, testenv.GetAzureEndpoint(ctx), testcaseEnvInst.GetIndexSecretName(), "azure", "blob")}
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, azTestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, azTestDirShcLocal, 60)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpecAzure(appSourceVolumeNameShcCluster, testenv.GetAzureEndpoint(ctx), testcaseEnvInst.GetIndexSecretName(), "azure", "blob")}
			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, azTestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameLocalIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcLocal, CrPod: cmPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			cmAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameClusterIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcCluster, CrPod: cmPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameLocalShc, CrAppSourceVolumeName: appSourceVolumeNameShcLocal, CrPod: deployerPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			shcAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameClusterShc, CrAppSourceVolumeName: appSourceVolumeNameShcCluster, CrPod: deployerPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfoLocal, cmAppSourceInfoCluster, shcAppSourceInfoLocal, shcAppSourceInfoCluster}
			time.Sleep(2 * time.Minute) // FIXME adding sleep to see if verification succeedes
			clusterManagerBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############# DOWNGRADE APPS ################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// Redefine app lists as LDAP app isn't in V1 apps
			appListLocal = appListV1[len(appListV1)/2:]
			appListCluster = appListV1[:len(appListV1)/2]

			// Upload appListLocal list of V1 apps to Azure (to be used for local install)
			appVersion = "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))
			localappFileList = testenv.GetAppFileList(appListLocal)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of V2 apps to Azure (to be used for cluster-wide install)
			clusterappFileList = testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameClusterIdxc, clusterappFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########## DOWNGRADE VERIFICATION #############
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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, clusterManagerBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA instance with App Framework enabled and install above 200MB of apps at once", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create App Source for C3 SVA (Cluster Manager and Deployer)
			   * Add more apps than usual on Azure for this test
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

			// Download apps from Azure
			testcaseEnvInst.Log.Info("Download bigger amount of apps from Azure for this test")
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps files")

			// Create consolidated list of app files
			appList = append(appListV1, appList...)
			appFileList = testenv.GetAppFileList(appList)

			// Upload app to Azure for Indexer Cluster
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory for Indexer Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload app to Azure for Search Head Cluster
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload apps to Azure test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Create Single Site Indexer Cluster and Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//  ############### VERIFICATIONS ###############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled for manual update", func() {
			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V1 apps to Azure
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
			// Upload V1 apps to Azure for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			azTestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 0)

			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
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

			// Upload V1 apps to Azure for Indexer Cluster
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 0)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			indexerReplicas := 3
			shReplicas := 3
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster")
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//######### INITIAL VERIFICATIONS #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			clusterManagerBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			// ############### UPGRADE APPS ################
			// Delete V1 apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to Azure for C3
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Uploading %s apps to Azure", appVersion))

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to Azure for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//  ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			appVersion = "V1"
			allPodNames := append(idxcPodNames, shcPodNames...)
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), allPodNames, appListV1, true, "enabled", false, true)

			// ############ ENABLE MANUAL POLL ############
			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterManager"] = strings.Replace(config.Data["ClusterManager"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["MonitoringConsole"] = strings.Replace(config.Data["MonitoringConsole"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info("Verify config map set back to off after poll trigger for app", "version", appVersion)
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["ClusterManager"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off") && strings.Contains(config.Data["MonitoringConsole"], "status: off")).To(Equal(true), "Config map update not complete")

			// ############## UPGRADE VERIFICATIONS ############
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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, clusterManagerBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA and have apps installed and updated locally on Cluster Manager and Deployer for manual polling", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure
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
			   ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			   * Verify Apps are not updated
			   ############ ENABLE MANUAL POLL ############
			   * Verify Manual Poll disabled after the poll is triggered
			   ########### UPGRADE VERIFICATIONS ###########
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are copied, installed and upgraded on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to Azure for Indexer Cluster
			appVersion := "V1"
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, azTestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, azTestDirShc, 0)

			// Deploy C3 CRD
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############## INITIAL VERIFICATION ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete V1 apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to Azure
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//  ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############ ENABLE MANUAL POLL ############
			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterManager"] = strings.Replace(config.Data["ClusterManager"], "off", "on", 1)

			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["SearchHeadCluster"] = strings.Replace(config.Data["SearchHeadCluster"], "off", "on", 1)
			err = deployment.UpdateCR(ctx, config)
			Expect(err).To(Succeed(), "Unable to update config map")

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ########## Verify Manual Poll config map disabled after the poll is triggered #################
			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info("Verify config map set back to off after poll trigger for app", "version", appVersion)
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["ClusterManager"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

			//########### UPGRADE VERIFICATIONS ###########
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

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration,managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then upgrade them via a manual poll", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Split Applist into clusterlist and local list
			   * Upload V1 apps to Azure for Indexer Cluster and Search Head Cluster for local and cluster scope
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
			   * Upload V2 apps on Azure
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

			// Upload appListLocal list of apps to Azure (to be used for local install) for Idxc
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))
			azTestDirIdxcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListLocal list of apps to Azure (to be used for local install) for Shc
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))
			azTestDirShcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to Azure (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for cluster-wide install (cluster scope)", appVersion))
			azTestDirIdxcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to Azure test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to Azure (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for cluster-wide install (cluster scope)", appVersion))
			azTestDirShcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to Azure test directory", appVersion))
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
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, azTestDirIdxcLocal, 0)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpecAzure(appSourceVolumeNameIdxcCluster, testenv.GetAzureEndpoint(ctx), testcaseEnvInst.GetIndexSecretName(), "azure", "blob")}
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, azTestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, azTestDirShcLocal, 0)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpecAzure(appSourceVolumeNameShcCluster, testenv.GetAzureEndpoint(ctx), testcaseEnvInst.GetIndexSecretName(), "azure", "blob")}
			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, azTestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameLocalIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcLocal, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			cmAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameClusterIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxcCluster, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfoLocal := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameLocalShc, CrAppSourceVolumeName: appSourceVolumeNameShcLocal, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListLocal, CrAppFileList: localappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			shcAppSourceInfoCluster := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameClusterShc, CrAppSourceVolumeName: appSourceVolumeNameShcCluster, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListCluster, CrAppFileList: clusterappFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfoLocal, cmAppSourceInfoCluster, shcAppSourceInfoLocal, shcAppSourceInfoCluster}
			clusterManagerBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete apps on Azure
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on Azure", appVersion))
			azureBlobClient := &testenv.AzureBlobClient{}
			azureBlobClient.DeleteFilesOnAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, uploadedApps)
			uploadedApps = nil

			// Redefine app lists as LDAP app isn't in V1 apps
			appListLocal = appListV1[len(appListV1)/2:]
			appListCluster = appListV1[:len(appListV1)/2]

			// Upload appListLocal list of V2 apps to Azure (to be used for local install)
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for local install (local scope)", appVersion))
			localappFileList = testenv.GetAppFileList(appListLocal)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShcLocal, localappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for local install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of V2 apps to Azure (to be used for cluster-wide install)
			clusterappFileList = testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShcCluster, clusterappFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for cluster-wide install", appVersion))
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
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameClusterIdxc, clusterappFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, clusterManagerBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3, add new apps to app source while install is in progress and have all apps installed locally on Cluster Manager and Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload big-size app to Azure for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework
			   ############## VERIFICATIONS ################
			   * Verify app installation is in progress on Cluster Manager and Deployer
			   * Upload more apps from Azure during bigger app install
			   * Wait for polling interval to pass
			   * Verify all apps are installed on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to Azure for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			azTestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)

			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
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

			// Download all apps from Azure
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err = testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload big-size app to Azure for Cluster Manager
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Cluster Manager")
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big-size app to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Search Head Cluster")
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			deployerName := deployment.GetName()
			cm, _, _, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify App installation is in progress on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyComplete)

			// Upload more apps to Azure for Cluster Manager
			appList = testenv.ExtraApps
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload more apps to Azure for Cluster Manager")
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload more apps to Azure test directory for  Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload more apps to Azure for Deployer
			testcaseEnvInst.Log.Info("Upload more apps to Azure for Deployer")
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload more apps to Azure test directory for Deployer")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Verify all apps are installed on Cluster Manager
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Cluster Manager", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), cmPod, appList, true, "enabled", false, false)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify all apps are installed on Deployer
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Deployer", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), deployerPod, appList, true, "enabled", false, false)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure, azure_sanity: can deploy a C3, add new apps to app source while install is in progress and have all apps installed cluster-wide", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload big-size app to Azure for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   ############## VERIFICATIONS ################
			   * Verify App installation is in progress on Cluster Manager and Deployer
			   * Upload more apps from Azure during bigger app install
			   * Wait for polling interval to pass
			   * Verify all apps are installed on Cluster Manager and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to Azure for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Monitoring Console", appVersion))
			azTestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirMC, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, azTestDirMC, 60)

			mcSpec := enterpriseApi.MonitoringConsoleSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
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

			// Download all apps from Azure
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err = testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload big-size app to Azure for Cluster Manager
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Cluster Manager")
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big-size app to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big-size app to Azure for Search Head Cluster")
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big-size app to Azure test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyComplete)

			// Upload more apps to Azure for Cluster Manager
			appList = testenv.ExtraApps
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload more apps to Azure for Cluster Manager")
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload more apps to Azure test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload more apps to Azure for Deployer
			testcaseEnvInst.Log.Info("Upload more apps to Azure for Deployer")
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload more apps to Azure test directory for Deployer")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify all apps are installed on indexers
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			idxcPodNames := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on indexers", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), idxcPodNames, appList, true, "enabled", false, true)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName()+"-shc", shc.Kind, appSourceNameShc, appFileList)

			// Verify all apps are installed on Search Heads
			shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Search Heads", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), shcPodNames, appList, true, "enabled", false, true)

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled and reset operator pod while app install is in progress", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   * While app install is in progress, restart the operator
			   ######### VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied, installed on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Download all apps from Azure
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload V1 apps to Azure for Indexer Cluster
			appVersion := "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify App installation is in progress on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgInstallPending)

			// Delete Operator pod while Install in progress
			testenv.DeleteOperatorPod(testcaseEnvInst)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//######### VERIFICATIONS #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled and reset operator pod while app download is in progress", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   * While app download is in progress, restart the operator
			   ######### VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied, installed on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Download all apps from Azure
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList := testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload V1 apps to Azure for Indexer Cluster
			appVersion := "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify App Download is in progress on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgDownloadComplete, enterpriseApi.AppPkgDownloadPending)

			// Delete Operator pod while Install in progress
			testenv.DeleteOperatorPod(testcaseEnvInst)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//######### VERIFICATIONS #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled, install an app, then disable it by using a disabled version of the app and then remove it from app source", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   ######### VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied, installed on Monitoring Console and on Search Heads and Indexers pods
			   ############ Upload Disabled App ###########
			   * Download disabled app from az
			   * Delete the app from az
			   * Check for repo state in App Deployment Info
			*/

			//################## SETUP ####################
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			// Upload V1 apps to Azure for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// //######### INITIAL VERIFICATIONS #############
			idxcPodNames := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify repo state on App to be disabled to be 1 (i.e app present on Azure bucket)
			appName := appListV1[0]
			appFileName := testenv.GetAppFileList([]string{appName})
			testenv.VerifyAppRepoState(ctx, deployment, testcaseEnvInst, cm.Name, cm.Kind, appSourceNameIdxc, 1, appFileName[0])

			// Disable the app
			testcaseEnvInst.Log.Info("Download disabled version of apps from Azure for this test")
			testenv.DisableAppsOnAzure(ctx, downloadDirV1, appFileName, azTestDirIdxc)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileName)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Wait for App state to update after config file change
			testenv.WaitforAppInstallState(ctx, deployment, testcaseEnvInst, idxcPodNames, testcaseEnvInst.GetName(), appName, "disabled", true)

			//Delete the file from az
			azFilepath := "/" + AzureContainer + "/" + filepath.Join(azTestDirIdxc, appFileName[0])
			azureBlobClient := &testenv.AzureBlobClient{}
			err = azureBlobClient.DeleteFileOnAzure(ctx, azFilepath, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to delete %s app on Azure test directory", appFileName[0]))

			// Verify repo state is set to 2 (i.e app deleted from Azure bucket)
			testenv.VerifyAppRepoState(ctx, deployment, testcaseEnvInst, cm.Name, cm.Kind, appSourceNameIdxc, 2, appFileName[0])

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled and update apps after app download is completed", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to Azure for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
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

			//################## SETUP ####################
			// Download all apps from Azure
			appVersion := "V1"
			appListV1 := []string{appListV1[0]}
			appFileList := testenv.GetAppFileList(appListV1)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download apps")

			// Upload V1 apps to Azure for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, azTestDirIdxc, 120)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, azTestDirShc, 120)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify App Download is in progress on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgInstallComplete, enterpriseApi.AppPkgPodCopyPending)

			// Upload V2 apps to Azure for Indexer Cluster
			appVersion = "V2"
			appListV2 := []string{appListV2[0]}
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV2, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//######### VERIFICATIONS #############
			appVersion = "V1"
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}, appListV1, false, "enabled", false, false)

			// Check for changes in App phase to determine if next poll has been triggered
			appFileList = testenv.GetAppFileList(appListV2)
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//############  UPGRADE VERIFICATIONS ############
			appVersion = "V2"
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV2, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration,managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA and install a bigger volume of apps than the operator PV disk space", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create a file on operator to utilize over 1G of space
			   * Upload files to Azure for Indexer Cluster and Search Head Cluster for cluster scope
			   * Create app sources for Cluster Manager and Deployer with cluster scope
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
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

			// Upload apps to Azure for Indexer Cluster
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc := "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search head Cluster", appVersion))
			azTestDirShc := "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Maximum apps to be downloaded in parallel
			maxConcurrentAppDownloads := 30

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecIdxc.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)
			appFrameworkSpecShc.MaxConcurrentAppDownloads = uint64(maxConcurrentAppDownloads)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############ INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled and delete apps from app directory when download is complete", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload big-size app to Azure for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
			   * When app download is complete, delete apps from app directory
			   ######### VERIFICATIONS #############
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify bundle push is successful
			   * Verify V1 apps are copied, installed on Monitoring Console and on Search Heads and Indexers pods
			*/

			//################## SETUP ####################
			// Download big size apps from Azure
			appList := testenv.BigSingleApp
			appFileList := testenv.GetAppFileList(appList)
			containerName := "/" + AzureDataContainer + "/" + AzureAppDirV1
			err := testenv.DownloadFilesFromAzure(ctx, testenv.GetAzureEndpoint(ctx), testenv.StorageAccountKey, testenv.StorageAccount, downloadDirV1, containerName, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload big size app to Azure for Indexer Cluster
			appVersion := "V1"
			testcaseEnvInst.Log.Info("Upload big size app to Azure for Indexer Cluster")
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big size to Azure test directory for Indexer Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big size app to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big size app to Azure for Search Head Cluster")
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), "Unable to upload big size to Azure test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify App Download is completed on Cluster Manager
			testenv.VerifyAppState(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList, enterpriseApi.AppPkgPodCopyComplete, enterpriseApi.AppPkgPodCopyPending)

			//Delete apps from app directory when app download is complete
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			podDownloadPath := filepath.Join(splcommon.AppDownloadVolume, "downloadedApps", testenvInstance.GetName(), cm.Kind, deployment.GetName(), enterpriseApi.ScopeCluster, appSourceNameIdxc, testenv.AppInfo[appList[0]]["filename"])
			err = testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
			Expect(err).To(Succeed(), "Unable to delete file on pod")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//######### VERIFICATIONS #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterManagerPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appList, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("smoke, c3azure, managerappframeworkazurec3, appframeworkazure: can deploy a C3 SVA with App Framework enabled and check isDeploymentInProgressFlag for CM and SHC CR's", func() {

			/*
			   Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to Azure for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy C3 CRD with app framework
			   * Verify IsDeploymentInProgress is set
			   * Wait for the pods to be ready
			*/

			//################## SETUP ####################
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)

			// Upload V1 apps to Azure for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Indexer Cluster", appVersion))
			azTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirIdxc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to Azure for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure for Search Head Cluster", appVersion))
			azTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToAzure(ctx, testenv.StorageAccount, testenv.StorageAccountKey, downloadDirV1, azTestDirShc, appFileList)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, azTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, azTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			deployerName := deployment.GetName()
			cm, _, shc, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify IsDeploymentInProgress Flag is set to true for Cluster Manager CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, cm.Name, cm.Kind)

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Verify IsDeploymentInProgress Flag is set to true for SHC CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, shc.Name, shc.Kind)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Deployer goes to Ready phase
			testenv.DeployerReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
		})
	})

	XContext("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3: can deploy a C3 SVA and a Standalone, then add that Standalone as a Search Head to the cluster", func() {

			/* Test Steps
			   ################## SETUP ###################
			   * Deploy C3 CRD
			   * Deploy Standalone with clusterMasterRef
			   ############# VERIFICATION #################
			   * Verify clusterMasterRef is present in Standalone's server.conf file
			*/
			//################## SETUP ####################
			// Deploy C3 CRD
			indexerReplicas := 3
			deployerName := deployment.GetName()
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster")
			err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), indexerReplicas, false, "", deployerName)
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster")

			// Create spec with clusterMasterRef for Standalone
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
					ClusterMasterRef: corev1.ObjectReference{
						Name: deployment.GetName(),
					},
				},
			}

			// Deploy Standalone with clusterMasterRef
			testcaseEnvInst.Log.Info("Deploy Standalone with clusterMasterRef")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with clusterMasterRef")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//  Ensure that the Standalone goes to Ready phase
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############# VERIFICATION #################
			// Verify Standalone is configured as a Search Head for the Cluster Manager
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			Expect(testenv.CheckSearchHeadOnCM(ctx, deployment, standalonePodName)).To(Equal(true))

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})
})
