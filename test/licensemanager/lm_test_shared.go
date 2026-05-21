// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
// limitations under the License.
package licensemanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// RunLMS1Test deploys a Standalone with License Manager and Monitoring Console,
// then verifies LM is configured on the standalone and MC pods.
func RunLMS1Test(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.LicenseTestConfig) {
	// Set up license config map
	Expect(testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)).To(Succeed(), "Unable to setup license config map")

	// Create Standalone deployment with License Manager/Master
	mcRef := deployment.GetName()
	standalone, err := config.DeployStandaloneWithLM(ctx, deployment, deployment.GetName(), mcRef)
	Expect(err).To(Succeed(), "Unable to deploy Standalone instance with LM")

	// Wait for License Manager/Master and Standalone to be in READY status
	Eventually(func() error {
		return testenv.VerifyLMAndStandaloneReady(ctx, deployment, testcaseEnvInst, config.ClusterReadinessConfig, standalone)
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "License Manager or Standalone not ready")

	// Deploy and verify Monitoring Console
	_, err = testcaseEnvInst.DeployAndVerifyMonitoringConsole(ctx, deployment, mcRef, deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify livenessProbe and readinessProbe config object and scripts
	Expect(testcaseEnvInst.VerifyProbeConfigAndScripts(ctx, deployment, false)).To(Succeed(), "Probe config verification failed")

	// Verify License Manager/Master is configured on Standalone instance
	standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
	Eventually(func() error { return testenv.VerifyLMConfiguredOnPod(ctx, deployment, standalonePodName) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "LM not configured on Standalone pod")

	// Verify License Manager/Master is configured on Monitoring Console
	Eventually(func() error { return testenv.VerifyLMConfiguredOnMC(ctx, deployment) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "LM not configured on MC")
}

// RunLMC3Test deploys a C3 cluster with License Manager and Monitoring Console,
// then verifies LM is configured on indexers, search heads, and MC.
func RunLMC3Test(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.LicenseTestConfig) {
	// Deploy single site Cluster with License Manager/Master
	mcRef := deployment.GetName()
	Expect(config.DeployC3WithLicense(ctx, deployment, testcaseEnvInst, 3, true, mcRef)).To(Succeed(), "Unable to deploy C3 with license")

	Expect(testenv.DeployMCAndVerifyRFSF(ctx, deployment, testcaseEnvInst, mcRef)).To(Succeed(), "Unable to deploy MC and verify RFSF")

	// Verify License Manager/Master is configured on indexers, search heads, and MC
	indexerPods := testenv.GeneratePodNameSlice(testenv.IndexerPod, deployment.GetName(), 3, false, 0)
	Eventually(func() error { return testenv.VerifyLMConfiguredOnCluster(ctx, deployment, indexerPods) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "LM not configured on cluster pods")
}

// RunLMC3AppFrameworkTest deploys a License Manager with App Framework, verifies V1 apps
// are installed, upgrades to V2 apps, and verifies the updated apps.
func RunLMC3AppFrameworkTest(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, testenvInstance *testenv.TestEnv, config *testenv.LicenseTestConfig) {
	var (
		appListV1          []string
		appListV2          []string
		testS3Bucket       = os.Getenv("TEST_INDEXES_S3_BUCKET")
		testDataS3Bucket   = os.Getenv("TEST_BUCKET")
		azureDataContainer = os.Getenv("TEST_CONTAINER")
		appDirV1           = testenv.AppLocationV1
		appDirV2           = testenv.AppLocationV2
		currDir, _         = os.Getwd()
		downloadDirV1      = filepath.Join(currDir, "lmV1-"+testenv.RandomDNSName(4))
		downloadDirV2      = filepath.Join(currDir, "lmV2-"+testenv.RandomDNSName(4))
		uploadedApps       []string
		testDir            string
	)

	// Create a list of apps to upload
	appVersion := "V1"
	appListV1 = testenv.BasicApps
	appFileList := testenv.GetAppFileList(appListV1)

	// Download V1 Apps
	Expect(testenv.DownloadAppFiles(ctx, testDataS3Bucket, azureDataContainer, appDirV1, downloadDirV1, appFileList, appVersion)).To(Succeed(), "Unable to download V1 app files")

	// Upload V1 apps
	testDir = "lm-" + testenv.RandomDNSName(4)
	uploadedFiles, err := testenv.UploadAppFiles(ctx, testcaseEnvInst, testS3Bucket, testDir, downloadDirV1, appFileList, appVersion)
	Expect(err).To(Succeed(), "Unable to upload V1 app files")
	uploadedApps = append(uploadedApps, uploadedFiles...)

	// Set up license config map
	Expect(testenv.SetupLicenseConfigMap(ctx, testcaseEnvInst)).To(Succeed(), "Unable to setup license config map")

	// Create app framework spec
	volumeName := "lm-test-volume-" + testenv.RandomDNSName(3)
	volumeSpec := testcaseEnvInst.GenerateVolumeSpecForProvider(ctx, volumeName)

	// AppSourceDefaultSpec: Remote Storage volume name and scope of app deployment
	appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
		VolName: volumeName,
		Scope:   enterpriseApi.ScopeLocal,
	}

	// appSourceSpec: app source name, location and volume name and scope from appSourceDefaultSpec
	appSourceName := "lm-" + testenv.RandomDNSName(3)
	appSourceSpec := []enterpriseApi.AppSourceSpec{testenv.GenerateAppSourceSpec(appSourceName, testDir, appSourceDefaultSpec)}

	// appFrameworkSpec: AppSource settings, poll interval, volumes, appSources on volumes
	appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
		Defaults:             appSourceDefaultSpec,
		AppsRepoPollInterval: 60,
		VolList:              volumeSpec,
		AppSources:           appSourceSpec,
	}
	spec := config.BuildLMAppFrameworkSpec(testcaseEnvInst, appFrameworkSpec)

	// Deploy the License Manager/Master with App Framework
	_, err = config.DeployLicenseManagerWithGivenSpec(ctx, deployment, deployment.GetName(), spec)
	Expect(err).To(Succeed(), "Unable to deploy LM with App Framework")

	// Wait for License Manager/Master to be in READY status
	Eventually(func() error { return config.LicenseManagerReady(ctx, deployment, testcaseEnvInst) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "License Manager not ready")

	// Wait for V1 apps to reach Install phase on License Manager/Master
	err = testcaseEnvInst.WaitForAllAppsPhase(ctx, deployment, deployment.GetName(), config.CrKind, appSourceName, appListV1, enterpriseApi.PhaseInstall, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for V1 apps to reach Install phase on LicenseManager")

	// Verify apps are copied and installed on License Manager/Master
	podName := []string{fmt.Sprintf(config.LicenseManagerPodName, deployment.GetName(), 0)}
	Eventually(func() error {
		return testenv.VerifyLMAppsOnPod(ctx, deployment, testcaseEnvInst, testenvInstance, podName, appListV1, false)
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "V1 apps not found on LM pod")

	// Delete files uploaded
	testenv.DeleteUploadedFiles(ctx, testS3Bucket, uploadedApps)
	uploadedApps = nil

	// Create a list of apps to upload to S3 after poll period
	appListV2 = append(appListV1, testenv.NewAppsAddedBetweenPolls...)
	appFileList = testenv.GetAppFileList(appListV2)
	appVersion = "V2"

	// Download V2 Apps
	Expect(testenv.DownloadAppFiles(ctx, testDataS3Bucket, azureDataContainer, appDirV2, downloadDirV2, appFileList, appVersion)).To(Succeed(), "Unable to download V2 app files")

	// Upload V2 apps
	uploadedFiles, err = testenv.UploadAppFiles(ctx, testcaseEnvInst, testS3Bucket, testDir, downloadDirV2, appFileList, appVersion)
	Expect(err).To(Succeed(), "Unable to upload V2 app files")
	uploadedApps = append(uploadedApps, uploadedFiles...)

	// Wait for operator to detect V2 apps (any app leaves Install phase)
	testcaseEnvInst.WaitforPhaseChange(ctx, deployment, deployment.GetName(), config.CrKind, appSourceName, appFileList)

	// Wait for License Manager/Master to finish processing V2 apps and become Ready
	Eventually(func() error { return config.LicenseManagerReady(ctx, deployment, testcaseEnvInst) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "License Manager not ready after V2 upload")

	// Wait for V2 apps to reach Install phase on License Manager/Master
	err = testcaseEnvInst.WaitForAllAppsPhase(ctx, deployment, deployment.GetName(), config.CrKind, appSourceName, appListV2, enterpriseApi.PhaseInstall, 2*time.Minute)
	Expect(err).To(Succeed(), "Timed out waiting for V2 apps to reach Install phase on LicenseManager")

	// Verify apps are copied and installed on License Manager/Master
	Eventually(func() error {
		return testenv.VerifyLMAppsOnPod(ctx, deployment, testcaseEnvInst, testenvInstance, podName, appListV2, true)
	}, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "V2 apps not found on LM pod")

	// Delete files uploaded
	testenv.DeleteUploadedFiles(ctx, testS3Bucket, uploadedApps)

	// Delete locally downloaded app files
	Expect(os.RemoveAll(downloadDirV1)).To(Succeed(), "Unable to delete locally downloaded V1 app files")
	Expect(os.RemoveAll(downloadDirV2)).To(Succeed(), "Unable to delete locally downloaded V2 app files")
}

// RunLMM4Test deploys a multisite cluster with License Manager and Monitoring Console,
// then verifies LM is configured on indexers, search heads, and MC.
func RunLMM4Test(ctx context.Context, deployment *testenv.Deployment, testcaseEnvInst *testenv.TestCaseEnv, config *testenv.LicenseTestConfig) {
	// Deploy Multisite Cluster with License Manager/Master and Search Head
	siteCount := 3
	mcRef := deployment.GetName()
	Expect(config.DeployM4WithLicense(ctx, deployment, testcaseEnvInst, 1, siteCount, mcRef)).To(Succeed(), "Unable to deploy M4 with license")
	Expect(testenv.DeployMCAndVerifyRFSF(ctx, deployment, testcaseEnvInst, mcRef)).To(Succeed(), "Unable to deploy MC and verify RFSF")

	// Verify License Manager/Master is configured on indexers, search heads, and MC
	indexerPods := testenv.GeneratePodNameSlice(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, true, siteCount)
	Eventually(func() error { return testenv.VerifyLMConfiguredOnCluster(ctx, deployment, indexerPods) }, testenv.DefaultTimeout, testenv.PollInterval).Should(Succeed(), "LM not configured on multisite cluster pods")
}
