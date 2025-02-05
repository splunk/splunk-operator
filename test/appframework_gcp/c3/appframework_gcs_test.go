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
package c3gcpappfw

import (
	"context"
	//"encoding/json"
	"fmt"
	"path/filepath"

	//"strings"
	//"time"

	//enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	//splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	testenv "github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("c3appfw test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv

	var deployment *testenv.Deployment
	var gcsTestDirShc string
	var gcsTestDirIdxc string
	//var gcsTestDirShcLocal string
	//var gcsTestDirIdxcLocal string
	//var gcsTestDirShcCluster string
	//var gcsTestDirIdxcCluster string
	var appSourceNameIdxc string
	var appSourceNameShc string
	var uploadedApps []string
	var filePresentOnOperator bool

	ctx := context.TODO()

	BeforeEach(func() {

		var err error
		name := fmt.Sprintf("%s-%s", "master"+testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		testenv.SpecifiedTestTimeout = 5000
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

		// Delete files uploaded to GCS
		if !testcaseEnvInst.SkipTeardown {
			testenv.DeleteFilesOnGCP(testGcsBucket, uploadedApps)
		}

		if filePresentOnOperator {
			//Delete files from app-directory
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			podDownloadPath := filepath.Join(testenv.AppDownloadVolume, "test_file.img")
			testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
		}
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It(" c3gcp, masterappframeworkc3gcp,  c3_gcp_sanity: can deploy a C3 SVA with App Framework enabled, install apps then upgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to GCS for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V1 apps to GCS for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Master and Deployer
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
			   * Upload V2 apps on GCS
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
			// Upload V1 apps to GCS for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Monitoring Console", appVersion))
			gcsTestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Monitoring Console %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, gcsTestDirMC, 60)

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

			// Upload V1 apps to GCS for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Bucket for Indexer Cluster", appVersion))
			gcsTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Indexer Cluster %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to GCS for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Search Head Cluster", appVersion))
			gcsTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, gcsTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, gcsTestDirShc, 60)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

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

			// ############ Verify livenessProbe and readinessProbe config object and scripts############
			testcaseEnvInst.Log.Info("Get config map for livenessProbe and readinessProbe")
			ConfigMapName := enterprise.GetProbeConfigMapName(testcaseEnvInst.GetName())
			_, err = testenv.GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
			Expect(err).To(Succeed(), "Unable to get config map for livenessProbe and readinessProbe", "ConfigMap name", ConfigMapName)
			scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName(), enterprise.GetStartupScriptName()}
			allPods := testenv.DumpGetPods(testcaseEnvInst.GetName())
			testenv.VerifyFilesInDirectoryOnPod(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)

			//######### INITIAL VERIFICATIONS #############
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			ClusterMasterBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete apps on GCS
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on GCS", appVersion))
			testenv.DeleteFilesOnGCP(testGcsBucket, uploadedApps)
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Upload V2 apps to GCS for Indexer Cluster
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Indexer Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Indexer Cluster %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to GCS for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to GCS for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Monitoring Console %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It(" c3gcp, masterappframeworkc3gcp,  c3_gcp_sanity: can deploy a C3 SVA with App Framework enabled, install apps then downgrade them", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V2 apps to GCS for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload V2 apps to GCS for Indexer Cluster and Search Head Cluster
			   * Create app source for Cluster Master and Deployer
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
			   * Upload V1 apps on GCS
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
			// Upload V2 apps to GCS for Monitoring Console
			appVersion := "V2"
			appFileList := testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Monitoring Console", appVersion))
			gcsTestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Monitoring Console %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, gcsTestDirMC, 60)

			// Monitoring Console AppFramework Spec
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

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V2 apps to GCS for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Indexer Cluster", appVersion))
			gcsTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Indexer Cluster %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to GCS for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Search Head Cluster", appVersion))
			gcsTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, gcsTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, gcsTestDirShc, 60)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// wait for custom resource resource version to change
			testenv.VerifyCustomResourceVersionChanged(ctx, deployment, testcaseEnvInst, mc, resourceVersion)

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//########### INITIAL VERIFICATIONS ###########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			mcPod := []string{fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeCluster, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			mcAppSourceInfo := testenv.AppSourceInfo{CrKind: mc.Kind, CrName: mc.Name, CrAppSourceName: appSourceNameMC, CrAppSourceVolumeName: appSourceNameMC, CrPod: mcPod, CrAppVersion: "V2", CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo, mcAppSourceInfo}
			ClusterMasterBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############## DOWNGRADE APPS ###############
			// Delete apps on GCS
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on GCS", appVersion))
			testenv.DeleteFilesOnGCP(testGcsBucket, uploadedApps)
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Upload V1 apps to GCS for Indexer Cluster
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Indexers", appVersion))
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Indexers", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to GCS for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to GCS for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Monitoring Console %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, ClusterMasterBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It(" c3gcp, masterappframeworkc3gcp,  c3_gcp_sanity: can deploy a C3 SVA and have apps installed locally on Cluster Manager and Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to GCS
			   * Create app source with local scope for C3 SVA (Cluster Master and Deployer)
			   * Prepare and deploy C3 CRD with app framework and wait for pods to be ready
			   ############# INITIAL VERIFICATIONS ##########
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are installed locally on Cluster Master and Deployer
			   ############### UPGRADE APPS ################
			   * Upgrade apps in app sources
			   * Wait for pods to be ready
			   ########### UPGRADE VERIFICATIONS ###########
			   * Verify Apps are Downloaded in App Deployment Info
			   * Verify Apps Copied in App Deployment Info
			   * Verify App Package is deleted from Operator Pod
			   * Verify Apps Installed in App Deployment Info
			   * Verify App Package is deleted from Splunk Pod
			   * Verify apps are copied, installed and upgraded on Cluster Master and Deployer
			*/

			//################## SETUP ####################
			// Upload V1 apps to GCS for Indexer Cluster
			appVersion := "V1"
			gcsTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Indexer Cluster %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to GCS for Search Head Cluster
			gcsTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, gcsTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, gcsTestDirShc, 60)

			// Deploy C3 CRD
			indexerReplicas := 3
			shReplicas := 3
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge := testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			//############## INITIAL VERIFICATIONS ##########
			var idxcPodNames, shcPodNames []string
			idxcPodNames = testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), indexerReplicas, false, 1)
			shcPodNames = testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), indexerReplicas, false, 1)
			cmPod := []string{fmt.Sprintf(testenv.ClusterMasterPod, deployment.GetName())}
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			cmAppSourceInfo := testenv.AppSourceInfo{CrKind: cm.Kind, CrName: cm.Name, CrAppSourceName: appSourceNameIdxc, CrAppSourceVolumeName: appSourceVolumeNameIdxc, CrPod: cmPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: indexerReplicas, CrClusterPods: idxcPodNames}
			shcAppSourceInfo := testenv.AppSourceInfo{CrKind: shc.Kind, CrName: shc.Name, CrAppSourceName: appSourceNameShc, CrAppSourceVolumeName: appSourceVolumeNameShc, CrPod: deployerPod, CrAppVersion: appVersion, CrAppScope: enterpriseApi.ScopeLocal, CrAppList: appListV1, CrAppFileList: appFileList, CrReplicas: shReplicas, CrClusterPods: shcPodNames}
			allAppSourceInfo := []testenv.AppSourceInfo{cmAppSourceInfo, shcAppSourceInfo}
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete V1 apps on GCS
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on GCS", appVersion))
			testenv.DeleteFilesOnGCP(testGcsBucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to GCS
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Indexer Cluster %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

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

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It(" c3gcp, masterappframeworkc3gcp,  c3_gcp_sanity: can deploy a C3 SVA with App Framework enabled and check isDeploymentInProgressFlag for CM and SHC CR's", func() {

			/*
			   Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to GCS for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy C3 CRD with app framework
			   * Verify IsDeploymentInProgress is set
			   * Wait for the pods to be ready
			*/

			//################## SETUP ####################
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)

			// Upload V1 apps to GCS for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Indexer Cluster", appVersion))
			gcsTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Indexer Cluster %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to GCS for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Search Head Cluster", appVersion))
			gcsTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, gcsTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, gcsTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify IsDeploymentInProgress Flag is set to true for Cluster Master CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, cm.Name, cm.Kind)

			// Ensure Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Verify IsDeploymentInProgress Flag is set to true for SHC CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, shc.Name, shc.Kind)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
		})
	})
})
