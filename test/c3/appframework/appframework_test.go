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
package c3appfw

import (
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	testenv "github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("c3appfw test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv

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

		// Delete files uploaded to S3
		if !testcaseEnvInst.SkipTeardown {
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
		}
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("smoke, c3, appframeworkc3, appframework: can deploy a C3 SVA with App Framework enabled, install apps then upgrade them", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)

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
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V1 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
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

			// Upload V2 apps to S3 for Monitoring Console
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("smoke, c3, appframeworkc3, appframework: can deploy a C3 SVA with App Framework enabled, install apps then downgrade them", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for Monitoring Console
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)

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
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V2 apps to S3 for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V2 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// get revision number of the resource
			resourceVersion := testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############## DOWNGRADE APPS ###############
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// get revision number of the resource
			resourceVersion = testenv.GetResourceVersion(ctx, deployment, testcaseEnvInst, mc)

			// Upload V1 apps to S3 for Indexer Cluster
			appVersion = "V1"
			appFileList = testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexers", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexers", appVersion))
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
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("integration, c3, appframeworkc3, appframework: can deploy a C3 SVA with App Framework enabled, install apps, scale up clusters, install apps on new pods, scale down", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			appFileList := testenv.GetAppFileList(appListV1)
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc := "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc := "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

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
			testenv.VerifySearchHeadClusterPhase(ctx, deployment, testcaseEnvInst, splcommon.PhaseScalingUp)

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
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, splcommon.PhaseScalingUp, idxcName)

			// Ensure Indexer Cluster go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify New Indexer On Cluster Manager
			indexerName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), scaledIndexerReplicas-1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Checking for New Indexer %s On Cluster Manager", indexerName))
			Expect(testenv.CheckIndexerOnCM(ctx, deployment, indexerName)).To(Equal(true))

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//########## SCALING UP VERIFICATIONS #########
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			shcPodNames = []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			shcPodNames = append(shcPodNames, testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)...)
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, shcPodNames)

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
			testenv.VerifySearchHeadClusterPhase(ctx, deployment, testcaseEnvInst, splcommon.PhaseScalingDown)

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
			testenv.VerifyIndexerClusterPhase(ctx, deployment, testcaseEnvInst, splcommon.PhaseScalingDown, idxcName)

			// Ensure Indexer Cluster go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//######## SCALING DOWN VERIFICATIONS #########
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, shcPodNames)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("smoke, c3, appframeworkc3, appframework: can deploy a C3 SVA and have apps installed locally on Cluster Manager and Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3
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
			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 60)

			// Deploy C3 CRD
			indexerReplicas := 3
			shReplicas := 3
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	XContext("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3, appframeworkc3, appframework: can deploy a C3 SVA and have ES app installed on Search Head Cluster", func() {

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
			testcaseEnvInst.Log.Info("Download ES app from S3")
			esApp := []string{"SplunkEnterpriseSecuritySuite"}
			appFileList := testenv.GetAppFileList(esApp)
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download ES app file from S3")

			// Create local directory for file download
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)

			// Upload ES app to S3
			testcaseEnvInst.Log.Info("Upload ES app to S3")
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload ES app to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceName := "appframework-shc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeClusterWithPreConfig, appSourceName, s3TestDirShc, 60)

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
			_, err = deployment.DeploySearchHeadClusterWithGivenSpec(ctx, deployment.GetName()+"-shc", shSpec)
			Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
		It("c3, integration, appframeworkc3, appframework: can deploy a C3 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then upgrade them", func() {

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

			// Upload appListLocal list of apps to S3 (to be used for local install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))

			s3TestDirIdxcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install for Indexers", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))

			s3TestDirShcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))

			s3TestDirIdxcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))

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
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, s3TestDirIdxcLocal, 60)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameIdxcCluster, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, s3TestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, s3TestDirShcLocal, 60)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameShcCluster, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, s3TestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

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
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install for Indexers", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for local install for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of V2 apps to S3 (to be used for cluster-wide install)
			clusterappFileList = testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for cluster-wide install", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameClusterIdxc, clusterappFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration, appframeworkc3, appframework: can deploy a C3 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then downgrade them", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirIdxcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListLocal list of apps to S3 (to be used for local install) for Shc
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirShcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirIdxcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
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
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, s3TestDirIdxcLocal, 60)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameIdxcCluster, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, s3TestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, s3TestDirShcLocal, 60)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameShcCluster, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, s3TestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			clusterManagerBundleHash := testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############# DOWNGRADE APPS ################
			// Delete apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Redefine app lists as LDAP app isn't in V1 apps
			appListLocal = appListV1[len(appListV1)/2:]
			appListCluster = appListV1[:len(appListV1)/2]

			// Upload appListLocal list of V1 apps to S3 (to be used for local install)
			appVersion = "V1"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
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

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameClusterIdxc, clusterappFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3, appframeworkc3, appframework: can deploy a C3 SVA instance with App Framework enabled and install above 200MB of apps at once", func() {

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
			testcaseEnvInst.Log.Info("Download bigger amount of apps from S3 for this test")
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
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Create Single Site Indexer Cluster and Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")

			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It("integration, c3, appframeworkc3, appframework: can deploy a C3 SVA with App Framework enabled for manual update", func() {
			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
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
			// Upload V1 apps to S3 for Monitoring Console
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Monitoring Console", appVersion))
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 0)

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
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Upload V1 apps to S3 for Indexer Cluster
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
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
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 0)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			indexerReplicas := 3
			shReplicas := 3
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster")
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with App framework")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			// ############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3 for C3
			appVersion = "V2"
			appFileList = testenv.GetAppFileList(appListV2)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Uploading %s apps to S3", appVersion))

			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

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
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			config.Data["ClusterMaster"] = strings.Replace(config.Data["ClusterMaster"], "off", "on", 1)
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
			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off") && strings.Contains(config.Data["MonitoringConsole"], "status: off")).To(Equal(true), "Config map update not complete")

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("integration, c3, appframeworkc3, appframework: can deploy a C3 SVA and have apps installed and updated locally on Cluster Manager and Deployer for manual polling", func() {

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
			// Upload V1 apps to S3 for Indexer Cluster
			appVersion := "V1"
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			appFileList := testenv.GetAppFileList(appListV1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Indexer Cluster", appVersion))
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to S3 for Search Head Cluster
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for Search Head Cluster", appVersion))
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 0)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 0)

			// Deploy C3 CRD
			indexerReplicas := 3
			shReplicas := 3
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

			//############### UPGRADE APPS ################
			// Delete V1 apps on S3
			testcaseEnvInst.Log.Info(fmt.Sprintf("Delete %s apps on S3", appVersion))
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Upload V2 apps to S3
			appVersion = "V2"
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3", appVersion))
			appFileList = testenv.GetAppFileList(appListV2)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Indexer Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Check for changes in App phase to determine if next poll has been triggered
			testenv.WaitforPhaseChange(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			//  ############ VERIFICATION APPS ARE NOT UPDATED BEFORE ENABLING MANUAL POLL ############
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, "")

			// ############ ENABLE MANUAL POLL ############
			testcaseEnvInst.Log.Info("Get config map for triggering manual update")
			config, err := testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(err).To(Succeed(), "Unable to get config map for manual poll")

			testcaseEnvInst.Log.Info("Modify config map to trigger manual update")
			config.Data["ClusterMaster"] = strings.Replace(config.Data["ClusterMaster"], "off", "on", 1)

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

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

			// ########## Verify Manual Poll config map disabled after the poll is triggered #################
			// Verify config map set back to off after poll trigger
			testcaseEnvInst.Log.Info("Verify config map set back to off after poll trigger for app", "version", appVersion)
			config, _ = testenv.GetAppframeworkManualUpdateConfigMap(ctx, deployment, testcaseEnvInst.GetName())
			Expect(strings.Contains(config.Data["ClusterMaster"], "status: off") && strings.Contains(config.Data["SearchHeadCluster"], "status: off")).To(Equal(true), "Config map update not complete")

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("c3, integration, appframeworkc3, appframework: can deploy a C3 SVA with apps installed locally on Cluster Manager and Deployer, cluster-wide on Peers and Search Heads, then upgrade them via a manual poll", func() {

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
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirIdxcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			localappFileList := testenv.GetAppFileList(appListLocal)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListLocal list of apps to S3 (to be used for local install) for Shc
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for local install (local scope)", appVersion))
			s3TestDirShcLocal = "c3appfw-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShcLocal, localappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (local scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
			s3TestDirIdxcCluster = "c3appfw-cluster-" + testenv.RandomDNSName(4)
			clusterappFileList := testenv.GetAppFileList(appListCluster)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxcCluster, clusterappFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps (cluster scope) to S3 test directory", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload appListCluster list of apps to S3 (to be used for cluster-wide install)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3 for cluster-wide install (cluster scope)", appVersion))
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
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalIdxc, s3TestDirIdxcLocal, 0)
			volumeSpecCluster := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameIdxcCluster, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecIdxc.VolList = append(appFrameworkSpecIdxc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameIdxcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterIdxc, s3TestDirIdxcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecIdxc.AppSources = append(appFrameworkSpecIdxc.AppSources, appSourceSpecCluster...)

			// Create App framework Spec for Search head cluster with scope local and append cluster scope
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShcLocal, enterpriseApi.ScopeLocal, appSourceNameLocalShc, s3TestDirShcLocal, 0)
			volumeSpecCluster = []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(appSourceVolumeNameShcCluster, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3")}
			appFrameworkSpecShc.VolList = append(appFrameworkSpecShc.VolList, volumeSpecCluster...)
			appSourceClusterDefaultSpec = enterpriseApi.AppSourceDefaultSpec{
				VolName: appSourceVolumeNameShcCluster,
				Scope:   enterpriseApi.ScopeCluster,
			}
			appSourceSpecCluster = []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceNameClusterShc, s3TestDirShcCluster, appSourceClusterDefaultSpec)}
			appFrameworkSpecShc.AppSources = append(appFrameworkSpecShc.AppSources, appSourceSpecCluster...)

			// Create Single site Cluster and Search Head Cluster, with App Framework enabled on Cluster Manager and Deployer
			testcaseEnvInst.Log.Info("Deploy Single site Indexer Cluster with both Local and Cluster scope for apps installation")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

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
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)

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
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Get Pod age to check for pod resets later
			splunkPodAge = testenv.GetPodsStartTime(testcaseEnvInst.GetName())

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
			testenv.AppFrameWorkVerifications(ctx, deployment, testcaseEnvInst, allAppSourceInfo, splunkPodAge, clusterManagerBundleHash)

			// Verify no pods reset by checking the pod age
			testenv.VerifyNoPodReset(deployment, testcaseEnvInst, testcaseEnvInst.GetName(), splunkPodAge, nil)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3, appframeworkc3, appframework: can deploy a C3, add new apps to app source while install is in progress and have all apps installed locally on Cluster Manager and Deployer", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload big-size app to S3 for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework
			   ############## VERIFICATIONS ################
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
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)

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
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Download all apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload big-size app to S3 for Cluster Manager
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Cluster Manager")
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big-size app to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Search Head Cluster")
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeLocal + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeLocal, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeLocal, appSourceNameShc, s3TestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			cm, _, _, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify App installation is in progress on Cluster Manager
			testenv.VerifyAppInstallInProgress(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

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

			// Verify all apps are installed on Deployer
			appList = append(testenv.BigSingleApp, testenv.ExtraApps...)
			deployerPod := []string{fmt.Sprintf(testenv.DeployerPod, deployment.GetName())}
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Deployer", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), deployerPod, appList, true, "enabled", false, false)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It("integration, c3, appframeworkc3, appframework: can deploy a C3, add new apps to app source while install is in progress and have all apps installed cluster-wide", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Upload V1 apps to S3 for Monitoring Console
			   * Create app source for Monitoring Console
			   * Prepare and deploy Monitoring Console CRD with app framework and wait for the pod to be ready
			   * Upload big-size app to S3 for Indexer Cluster and Search Head Cluster
			   * Create app sources for Cluster Manager and Deployer
			   * Prepare and deploy C3 CRD with app framework and wait for the pods to be ready
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
			s3TestDirMC := "c3appfw-mc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDirMC, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3 test directory for Monitoring Console", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Prepare Monitoring Console spec with its own app source
			appSourceNameMC := "appframework-" + enterpriseApi.ScopeLocal + "mc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameMC := "appframework-test-volume-mc-" + testenv.RandomDNSName(3)
			appFrameworkSpecMC := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameMC, enterpriseApi.ScopeLocal, appSourceNameMC, s3TestDirMC, 60)

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
			testcaseEnvInst.Log.Info("Deploy Monitoring Console")
			mcName := deployment.GetName()
			mc, err := deployment.DeployMonitoringConsoleWithGivenSpec(ctx, testcaseEnvInst.GetName(), mcName, mcSpec)
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

			// Verify Monitoring Console is ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Download all apps from S3
			appList := append(testenv.BigSingleApp, testenv.ExtraApps...)
			appFileList = testenv.GetAppFileList(appList)
			err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download big-size app")

			// Upload big-size app to S3 for Cluster Manager
			appList = testenv.BigSingleApp
			appFileList = testenv.GetAppFileList(appList)
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Cluster Manager")
			s3TestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Cluster Manager")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload big-size app to S3 for Search Head Cluster
			testcaseEnvInst.Log.Info("Upload big-size app to S3 for Search Head Cluster")
			s3TestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload big-size app to S3 test directory for Search Head Cluster")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, s3TestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, s3TestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			shReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, mcName, "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify App installation is in progress
			testenv.VerifyAppInstallInProgress(ctx, deployment, testcaseEnvInst, deployment.GetName(), cm.Kind, appSourceNameIdxc, appFileList)

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

			// Wait for polling interval to pass
			testenv.WaitForAppInstall(ctx, deployment, testcaseEnvInst, deployment.GetName()+"-shc", shc.Kind, appSourceNameShc, appFileList)

			// Verify all apps are installed on Search Heads
			shcPodNames := testenv.GeneratePodNameSlice(testenv.SearchHeadPod, deployment.GetName(), shReplicas, false, 1)
			testcaseEnvInst.Log.Info(fmt.Sprintf("Verify all apps %v are installed on Search Heads", appList))
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), shcPodNames, appList, true, "enabled", false, true)

		})
	})
})
