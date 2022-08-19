// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package licensemanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Licensemanager test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var s3TestDir string
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
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("licensemanager, integration, c3: Splunk Operator can configure License Manager with Indexers and Search Heads in C3 SVA", func() {

			// Download License File
			licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath)

			mcRef := deployment.GetName()
			err = deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true /*shc*/, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

			// Verify LM is configured on indexers
			indexerPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 1)
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 2)
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)

			// Verify LM is configured on SHs
			searchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)
			searchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)
			searchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)

			// Verify LM Configured on Monitoring Console
			monitoringConsolePodName := fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName())
			testenv.VerifyLMConfiguredOnPod(ctx, deployment, monitoringConsolePodName)

		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("licensemanager: Splunk Operator can configure a C3 SVA and have apps installed locally on LM", func() {

			var (
				appListV1        []string
				appListV2        []string
				testS3Bucket     = os.Getenv("TEST_INDEXES_S3_BUCKET")
				testDataS3Bucket = os.Getenv("TEST_BUCKET")
				s3AppDirV1       = testenv.AppLocationV1
				s3AppDirV2       = testenv.AppLocationV2
				currDir, _       = os.Getwd()
				downloadDirV1    = filepath.Join(currDir, "lmV1-"+testenv.RandomDNSName(4))
				downloadDirV2    = filepath.Join(currDir, "lmV2-"+testenv.RandomDNSName(4))
				uploadedApps     []string
			)

			// Create a list of apps to upload to S3
			appListV1 = testenv.BasicApps
			appFileList := testenv.GetAppFileList(appListV1)

			// Download V1 Apps from S3
			err := testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
			Expect(err).To(Succeed(), "Unable to download V1 app files")

			// Upload V1 apps to S3
			s3TestDir = "lm-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Download License File
			licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath)

			// Create App framework Spec
			volumeName := "lm-test-volume-" + testenv.RandomDNSName(3)

			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3", testenv.GetDefaultS3Region())}

			// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
			appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
				VolName: volumeName,
				Scope:   enterpriseApi.ScopeLocal,
			}

			// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
			appSourceName := "lm-" + testenv.RandomDNSName(3)
			appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, s3TestDir, appSourceDefaultSpec)}

			// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
			appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
				Defaults:             appSourceDefaultSpec,
				AppsRepoPollInterval: 60,
				VolList:              volumeSpec,
				AppSources:           appSourceSpec,
			}

			spec := enterpriseApi.LicenseManagerSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Volumes: []corev1.Volume{
						{
							Name: "licenses",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: testcaseEnvInst.GetLMConfigMap(),
									},
								},
							},
						},
					},
					LicenseURL: "/mnt/licenses/enterprise.lic",
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy the LM with App Framework
			_, err = deployment.DeployLicenseManagerWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy LM with App framework")

			// Wait for LM to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

			// Verify apps are copied at the correct location on LM (/etc/apps/)
			podName := []string{fmt.Sprintf(testenv.LicenseManagerPod, deployment.GetName(), 0)}
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testenvInstance.GetName(), podName, appListV1, true, enterpriseApi.ScopeLocal)

			// Verify apps are installed on LM
			lmPodName := []string{fmt.Sprintf(testenv.LicenseManagerPod, deployment.GetName(), 0)}
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), lmPodName, appListV1, false, "enabled", false, false)

			// Delete files uploaded to S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Create a list of apps to upload to S3 after poll period
			appListV2 = append(appListV1, testenv.NewAppsAddedBetweenPolls...)
			appFileList = testenv.GetAppFileList(appListV2)

			// Download V2 Apps from S3
			err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV2, downloadDirV2, appFileList)
			Expect(err).To(Succeed(), "Unable to download V2 app files")

			//Upload new Versioned Apps to S3
			uploadedFiles, err = testenv.UploadFilesToS3(testS3Bucket, s3TestDir, appFileList, downloadDirV2)
			Expect(err).To(Succeed(), "Unable to upload apps to S3 test directory")
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Wait for the poll period for the apps to be downloaded
			time.Sleep(2 * time.Minute)

			// Wait for LM to be in READY status
			testenv.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

			// Verify apps are copied at the correct location on LM (/etc/apps/)
			testenv.VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testenvInstance.GetName(), podName, appListV2, true, enterpriseApi.ScopeLocal)

			// Verify apps are installed on LM
			testenv.VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), lmPodName, appListV2, true, "enabled", true, false)

			// Delete files uploaded to S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)

			// Delete locally downloaded app files
			os.RemoveAll(downloadDirV1)
			os.RemoveAll(downloadDirV2)
		})
	})
})
