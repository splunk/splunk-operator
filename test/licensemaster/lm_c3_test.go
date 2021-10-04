// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
package licensemaster

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/pkg/apis/enterprise/v2"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Licensemaster test", func() {

	var deployment *testenv.Deployment
	var s3TestDir string

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
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("licensemaster, integration: Splunk Operator can configure License Master with Indexers and Search Heads in C3 SVA", func() {

			// Download License File
			licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			err = deployment.DeploySingleSiteCluster(deployment.GetName(), 3, true /*shc*/)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify LM is configured on indexers
			indexerPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(deployment, indexerPodName)
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 1)
			testenv.VerifyLMConfiguredOnPod(deployment, indexerPodName)
			indexerPodName = fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 2)
			testenv.VerifyLMConfiguredOnPod(deployment, indexerPodName)

			// Verify LM is configured on SHs
			searchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(deployment, searchHeadPodName)
			searchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			testenv.VerifyLMConfiguredOnPod(deployment, searchHeadPodName)
			searchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			testenv.VerifyLMConfiguredOnPod(deployment, searchHeadPodName)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("licensemaster: Splunk Operator can configure a C3 SVA and have apps installed locally on LM", func() {

			var (
				appListV1        []string
				appListV2        []string
				testS3Bucket     = os.Getenv("TEST_INDEXES_S3_BUCKET")
				testDataS3Bucket = os.Getenv("TEST_BUCKET")
				s3AppDirV1       = "appframework/regressionappsv1/"
				s3AppDirV2       = "appframework/regressionappsv2/"
				currDir, _       = os.Getwd()
				downloadDirV1    = filepath.Join(currDir, "lmV1-"+testenv.RandomDNSName(4))
				downloadDirV2    = filepath.Join(currDir, "lmV2-"+testenv.RandomDNSName(4))
				uploadedApps     []string
			)

			// Create a list of apps to upload to S3
			appListV1 = testenv.BasicApps
			appFileList := testenv.GetAppFileList(appListV1, 1)

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
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			// Create App framework Spec
			volumeName := "lm-test-volume-" + testenv.RandomDNSName(3)
			volumeSpec := []enterpriseApi.VolumeSpec{testenv.GenerateIndexVolumeSpec(volumeName, testenv.GetS3Endpoint(), testenvInstance.GetIndexSecretName(), "aws", "s3")}

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

			spec := enterpriseApi.LicenseMasterSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Volumes: []corev1.Volume{
						{
							Name: "licenses",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: testenvInstance.GetLMConfigMap(),
									},
								},
							},
						},
					},
					LicenseURL: "/mnt/licenses/enterprise.lic",
					Spec: splcommon.Spec{
						ImagePullPolicy: "Always",
					},
				},
				AppFrameworkConfig: appFrameworkSpec,
			}

			// Deploy the LM with App Framework
			_, err = deployment.DeployLicenseMasterWithGivenSpec(deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy LM with App framework")

			// Wait for LM to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Verify apps are copied at the correct location on LM (/etc/apps/)
			podName := []string{fmt.Sprintf(testenv.LicenseMasterPod, deployment.GetName(), 0)}
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podName, appListV1, true, false)

			// Verify apps are installed on LM
			lmPodName := []string{fmt.Sprintf(testenv.LicenseMasterPod, deployment.GetName(), 0)}
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), lmPodName, appListV1, false, "enabled", false, false)

			// Delete files uploaded to S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)
			uploadedApps = nil

			// Create a list of apps to upload to S3 after poll period
			appListV2 = append(appListV1, testenv.NewAppsAddedBetweenPolls...)
			appFileList = testenv.GetAppFileList(appListV2, 2)

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
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Verify apps are copied at the correct location on LM (/etc/apps/)
			testenv.VerifyAppsCopied(deployment, testenvInstance, testenvInstance.GetName(), podName, appListV2, true, false)

			// Verify apps are installed on LM
			testenv.VerifyAppInstalled(deployment, testenvInstance, testenvInstance.GetName(), lmPodName, appListV2, true, "enabled", true, false)

			// Delete files uploaded to S3
			testenv.DeleteFilesOnS3(testS3Bucket, uploadedApps)

			// Delete locally downloaded app files
			os.RemoveAll(downloadDirV1)
			os.RemoveAll(downloadDirV2)
		})
	})
})
