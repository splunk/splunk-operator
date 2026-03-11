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
package testenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

type LicenseTestConfig struct {
	DeployStandaloneWithLM               func(ctx context.Context, deployment *Deployment, name string, mcRef string) (*enterpriseApi.Standalone, error)
	LicenseManagerReady                  func(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv)
	ClusterManagerReady                  func(ctx context.Context, deployment *Deployment, testcaseEnv *TestCaseEnv)
	DeployMultisiteClusterWithSearchHead func(ctx context.Context, deployment *Deployment, name string, indexerReplicas, siteCount int, mcRef string) error
	DeployLicenseManagerWithGivenSpec    func(ctx context.Context, deployment *Deployment, name string, spec interface{}) (interface{}, error)
	LicenseManagerPodName                string
	LicenseManagerSpecType               string
}

func NewLicenseMasterConfig() *LicenseTestConfig {
	return &LicenseTestConfig{
		DeployStandaloneWithLM: func(ctx context.Context, deployment *Deployment, name string, mcRef string) (*enterpriseApi.Standalone, error) {
			return deployment.DeployStandaloneWithLMaster(ctx, name, mcRef)
		},
		LicenseManagerReady: LicenseMasterReady,
		ClusterManagerReady: ClusterMasterReady,
		DeployMultisiteClusterWithSearchHead: func(ctx context.Context, deployment *Deployment, name string, indexerReplicas int, siteCount int, mcRef string) error {
			return deployment.DeployMultisiteClusterMasterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef)
		},
		DeployLicenseManagerWithGivenSpec: func(ctx context.Context, deployment *Deployment, name string, spec interface{}) (interface{}, error) {
			return deployment.DeployLicenseMasterWithGivenSpec(ctx, name, spec.(enterpriseApiV3.LicenseMasterSpec))
		},
		LicenseManagerPodName:  LicenseMasterPod,
		LicenseManagerSpecType: "v3",
	}
}

func NewLicenseManagerConfig() *LicenseTestConfig {
	return &LicenseTestConfig{
		DeployStandaloneWithLM: func(ctx context.Context, deployment *Deployment, name string, mcRef string) (*enterpriseApi.Standalone, error) {
			return deployment.DeployStandaloneWithLM(ctx, name, mcRef)
		},
		LicenseManagerReady: LicenseManagerReady,
		ClusterManagerReady: ClusterManagerReady,
		DeployMultisiteClusterWithSearchHead: func(ctx context.Context, deployment *Deployment, name string, indexerReplicas int, siteCount int, mcRef string) error {
			return deployment.DeployMultisiteClusterWithSearchHead(ctx, name, indexerReplicas, siteCount, mcRef)
		},
		DeployLicenseManagerWithGivenSpec: func(ctx context.Context, deployment *Deployment, name string, spec interface{}) (interface{}, error) {
			return deployment.DeployLicenseManagerWithGivenSpec(ctx, name, spec.(enterpriseApi.LicenseManagerSpec))
		},
		LicenseManagerPodName:  LicenseManagerPod,
		LicenseManagerSpecType: "v4",
	}
}

func downloadLicenseAndCreateConfigMap(ctx context.Context, testcaseEnvInst *TestCaseEnv) {
	downloadDir := "licenseFolder"

	var err error
	var licenseFilePath string

	switch ClusterProvider {
	case "eks":
		licenseFilePath, err = DownloadLicenseFromS3Bucket()
		Expect(err).To(Succeed(), "Unable to download license file from S3")
	case "azure":
		licenseFilePath, err = DownloadLicenseFromAzure(ctx, downloadDir)
		Expect(err).To(Succeed(), "Unable to download license file from Azure")
	case "gcp":
		licenseFilePath, err = DownloadLicenseFromGCPBucket()
		Expect(err).To(Succeed(), "Unable to download license file from GCP")
	default:
		fmt.Printf("Unable to download license file")
		testcaseEnvInst.Log.Info(fmt.Sprintf("Unable to download license file with Cluster Provider set as %v", ClusterProvider))
	}

	testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath)
}

func downloadAppFiles(ctx context.Context, testDataS3Bucket, azureDataContainer, appDir, downloadDir string, appFileList []string, version string) {
	switch ClusterProvider {
	case "eks":
		err := DownloadFilesFromS3(testDataS3Bucket, appDir, downloadDir, appFileList)
		Expect(err).To(Succeed(), fmt.Sprintf("Unable to download %s app files", version))
	case "azure":
		containerName := "/" + azureDataContainer + "/" + appDir
		err := DownloadFilesFromAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, downloadDir, containerName, appFileList)
		Expect(err).To(Succeed(), fmt.Sprintf("Unable to download %s app files", version))
	case "gcp":
		err := DownloadFilesFromGCP(testDataS3Bucket, appDir, downloadDir, appFileList)
		Expect(err).To(Succeed(), fmt.Sprintf("Unable to download %s app files", version))
	}
}

func uploadAppFiles(ctx context.Context, testcaseEnvInst *TestCaseEnv, testS3Bucket, testDir, downloadDir string, appFileList []string, version string) []string {
	var uploadedFiles []string
	var err error

	switch ClusterProvider {
	case "eks":
		testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3", version))
		uploadedFiles, err = UploadFilesToS3(testS3Bucket, testDir, appFileList, downloadDir)
		Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to S3", version))
	case "azure":
		testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure", version))
		uploadedFiles, err = UploadFilesToAzure(ctx, StorageAccount, StorageAccountKey, downloadDir, testDir, appFileList)
		Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to Azure", version))
	case "gcp":
		testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCP", version))
		uploadedFiles, err = UploadFilesToGCP(testS3Bucket, testDir, appFileList, downloadDir)
		Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCP", version))
	}

	return uploadedFiles
}

func deleteUploadedFiles(ctx context.Context, testS3Bucket string, uploadedApps []string) {
	switch ClusterProvider {
	case "eks":
		DeleteFilesOnS3(testS3Bucket, uploadedApps)
	case "azure":
		azureBlobClient := &AzureBlobClient{}
		azureBlobClient.DeleteFilesOnAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, uploadedApps)
	case "gcp":
		DeleteFilesOnGCP(testS3Bucket, uploadedApps)
	}
}

func RunLMS1Test(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *LicenseTestConfig) {
	// Download License File
	downloadLicenseAndCreateConfigMap(ctx, testcaseEnvInst)

	// Create standalone Deployment with License Manager/Master
	mcRef := deployment.GetName()
	standalone, err := config.DeployStandaloneWithLM(ctx, deployment, deployment.GetName(), mcRef)
	Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

	// Wait for License Manager/Master to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Wait for Standalone to be in READY status
	StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

	// Deploy Monitoring Console
	mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify Monitoring Console is Ready and stays in ready state
	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	// ############ Verify livenessProbe and readinessProbe config object and scripts############
	testcaseEnvInst.Log.Info("Get config map for livenessProbe and readinessProbe")
	ConfigMapName := enterprise.GetProbeConfigMapName(testcaseEnvInst.GetName())
	_, err = GetConfigMap(ctx, deployment, testcaseEnvInst.GetName(), ConfigMapName)
	Expect(err).To(Succeed(), "Unable to get config map for livenessProbe and readinessProbe", "ConfigMap name", ConfigMapName)
	scriptsNames := []string{enterprise.GetLivenessScriptName(), enterprise.GetReadinessScriptName()}
	allPods := DumpGetPods(testcaseEnvInst.GetName())
	VerifyFilesInDirectoryOnPod(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), allPods, scriptsNames, enterprise.GetProbeMountDirectory(), false, true)

	// Verify License Manager/Master is configured on standalone instance
	standalonePodName := fmt.Sprintf(StandalonePod, deployment.GetName(), 0)
	VerifyLMConfiguredOnPod(ctx, deployment, standalonePodName)

	// Verify License Manager/Master is configured on Monitoring Console
	monitoringConsolePodName := fmt.Sprintf(MonitoringConsolePod, deployment.GetName())
	VerifyLMConfiguredOnPod(ctx, deployment, monitoringConsolePodName)
}

func RunLMC3Test(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *LicenseTestConfig) {
	// Download License File
	downloadLicenseAndCreateConfigMap(ctx, testcaseEnvInst)

	// Deploy Single site Cluster with License Manager/Master
	mcRef := deployment.GetName()
	err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Wait for Cluster Manager to be in READY status
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

	// Wait for Search Head Cluster to be in READY status
	SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

	// Wait for Indexers to be in READY status
	SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

	// Deploy Monitoring Console
	mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify Monitoring Console is Ready and stays in ready state
	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	// Verify RF SF is met
	VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

	// Verify License Manager/Master is configured on indexers
	indexerPodName := fmt.Sprintf(IndexerPod, deployment.GetName(), 0)
	VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)
	indexerPodName = fmt.Sprintf(IndexerPod, deployment.GetName(), 1)
	VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)
	indexerPodName = fmt.Sprintf(IndexerPod, deployment.GetName(), 2)
	VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)

	// Verify License Manager/Master is configured on SHs
	searchHeadPodName := fmt.Sprintf(SearchHeadPod, deployment.GetName(), 0)
	VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)
	searchHeadPodName = fmt.Sprintf(SearchHeadPod, deployment.GetName(), 1)
	VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)
	searchHeadPodName = fmt.Sprintf(SearchHeadPod, deployment.GetName(), 2)
	VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)

	// Verify License Manager/Master is configured on Monitoring Console
	monitoringConsolePodName := fmt.Sprintf(MonitoringConsolePod, deployment.GetName())
	VerifyLMConfiguredOnPod(ctx, deployment, monitoringConsolePodName)
}

func RunLMC3AppFrameworkTest(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, testenvInstance *TestEnv, config *LicenseTestConfig) {
	var (
		appListV1          []string
		appListV2          []string
		testS3Bucket       = os.Getenv("TEST_INDEXES_S3_BUCKET")
		testDataS3Bucket   = os.Getenv("TEST_BUCKET")
		AzureDataContainer = os.Getenv("TEST_CONTAINER")
		appDirV1           = AppLocationV1
		appDirV2           = AppLocationV2
		currDir, _         = os.Getwd()
		downloadDirV1      = filepath.Join(currDir, "lmV1-"+RandomDNSName(4))
		downloadDirV2      = filepath.Join(currDir, "lmV2-"+RandomDNSName(4))
		uploadedApps       []string
		testDir            string
	)

	// Create a list of apps to upload
	appVersion := "V1"
	appListV1 = BasicApps
	appFileList := GetAppFileList(appListV1)

	// Download V1 Apps
	downloadAppFiles(ctx, testDataS3Bucket, AzureDataContainer, appDirV1, downloadDirV1, appFileList, appVersion)

	// Upload V1 apps
	testDir = "lm-" + RandomDNSName(4)
	uploadedFiles := uploadAppFiles(ctx, testcaseEnvInst, testS3Bucket, testDir, downloadDirV1, appFileList, appVersion)
	uploadedApps = append(uploadedApps, uploadedFiles...)

	// Download License File
	downloadLicenseAndCreateConfigMap(ctx, testcaseEnvInst)

	// Create App framework Spec
	volumeName := "lm-test-volume-" + RandomDNSName(3)
	var volumeSpec []enterpriseApi.VolumeSpec
	switch ClusterProvider {
	case "eks":
		volumeSpec = []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpec(volumeName, GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "aws", "s3", GetDefaultS3Region())}
	case "azure":
		volumeSpec = []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpecAzure(volumeName, GetAzureEndpoint(ctx), testcaseEnvInst.GetIndexSecretName(), "azure", "blob")}
	case "gcp":
		volumeSpec = []enterpriseApi.VolumeSpec{GenerateIndexVolumeSpec(volumeName, GetS3Endpoint(), testcaseEnvInst.GetIndexSecretName(), "gcp", "blob", GetDefaultS3Region())}
	}

	// AppSourceDefaultSpec: Remote Storage volume name and Scope of App deployment
	appSourceDefaultSpec := enterpriseApi.AppSourceDefaultSpec{
		VolName: volumeName,
		Scope:   enterpriseApi.ScopeLocal,
	}

	// appSourceSpec: App source name, location and volume name and scope from appSourceDefaultSpec
	appSourceName := "lm-" + RandomDNSName(3)
	appSourceSpec := []enterpriseApi.AppSourceSpec{GenerateAppSourceSpec(appSourceName, testDir, appSourceDefaultSpec)}

	// appFrameworkSpec: AppSource settings, Poll Interval, volumes, appSources on volumes
	appFrameworkSpec := enterpriseApi.AppFrameworkSpec{
		Defaults:             appSourceDefaultSpec,
		AppsRepoPollInterval: 60,
		VolList:              volumeSpec,
		AppSources:           appSourceSpec,
	}

	var spec interface{}
	if config.LicenseManagerSpecType == "v3" {
		spec = enterpriseApiV3.LicenseMasterSpec{
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
					Image:           testcaseEnvInst.GetSplunkImage(),
				},
			},
			AppFrameworkConfig: appFrameworkSpec,
		}
	} else {
		spec = enterpriseApi.LicenseManagerSpec{
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
					Image:           testcaseEnvInst.GetSplunkImage(),
				},
			},
			AppFrameworkConfig: appFrameworkSpec,
		}
	}

	// Deploy the License Manager/Master with App Framework
	var err error
	_, err = config.DeployLicenseManagerWithGivenSpec(ctx, deployment, deployment.GetName(), spec)
	Expect(err).To(Succeed(), "Unable to deploy LM with App framework")

	// Wait for License Manager/Master to be in READY status
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Verify apps are copied at the correct location on License Manager/Master (/etc/apps/)
	podName := []string{fmt.Sprintf(config.LicenseManagerPodName, deployment.GetName(), 0)}
	VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testenvInstance.GetName(), podName, appListV1, true, enterpriseApi.ScopeLocal)

	// Verify apps are installed on License Manager/Master
	VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podName, appListV1, false, "enabled", false, false)

	// Delete files uploaded
	deleteUploadedFiles(ctx, testS3Bucket, uploadedApps)
	uploadedApps = nil

	// Create a list of apps to upload to S3 after poll period
	appListV2 = append(appListV1, NewAppsAddedBetweenPolls...)
	appFileList = GetAppFileList(appListV2)
	appVersion = "V2"

	// Download V2 Apps
	downloadAppFiles(ctx, testDataS3Bucket, AzureDataContainer, appDirV2, downloadDirV2, appFileList, appVersion)

	// Upload V2 apps
	uploadedFiles = uploadAppFiles(ctx, testcaseEnvInst, testS3Bucket, testDir, downloadDirV2, appFileList, appVersion)
	uploadedApps = append(uploadedApps, uploadedFiles...)

	time.Sleep(2 * time.Minute)

	// Verify License Manager/Master stays in ready state
	config.LicenseManagerReady(ctx, deployment, testcaseEnvInst)

	// Verify apps are copied at the correct location on License Manager/Master (/etc/apps/)
	VerifyAppsCopied(ctx, deployment, testcaseEnvInst, testenvInstance.GetName(), podName, appListV2, true, enterpriseApi.ScopeLocal)

	// Verify apps are installed on License Manager/Master
	VerifyAppInstalled(ctx, deployment, testcaseEnvInst, testcaseEnvInst.GetName(), podName, appListV2, true, "enabled", true, false)

	// Delete files uploaded
	deleteUploadedFiles(ctx, testS3Bucket, uploadedApps)

	// Delete locally downloaded app files
	os.RemoveAll(downloadDirV1)
	os.RemoveAll(downloadDirV2)
}

func RunLMM4Test(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, config *LicenseTestConfig) {
	// Download License File
	downloadLicenseAndCreateConfigMap(ctx, testcaseEnvInst)

	// Deploy Multisite Cluster with License Manager/Master and Search Head
	siteCount := 3
	mcRef := deployment.GetName()
	err := config.DeployMultisiteClusterWithSearchHead(ctx, deployment, deployment.GetName(), 1, siteCount, mcRef)
	Expect(err).To(Succeed(), "Unable to deploy cluster")

	// Wait for Cluster Manager to be in READY status
	config.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

	// Wait for Indexers to be in READY status
	IndexersReady(ctx, deployment, testcaseEnvInst, siteCount)

	// Verify Multisite Indexer Cluster status
	IndexerClusterMultisiteStatus(ctx, deployment, testcaseEnvInst, siteCount)

	// Wait for Search Head Cluster to be in READY status
	SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

	// Deploy Monitoring Console
	mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, deployment.GetName())
	Expect(err).To(Succeed(), "Unable to deploy Monitoring Console")

	// Verify Monitoring Console is Ready and stays in ready state
	VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc, testcaseEnvInst)

	// Verify RF SF is met
	VerifyRFSFMet(ctx, deployment, testcaseEnvInst)

	// Verify License Manager/Master is configured on indexers
	indexerPodName := fmt.Sprintf(MultiSiteIndexerPod, deployment.GetName(), 1, 0)
	VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)
	indexerPodName = fmt.Sprintf(MultiSiteIndexerPod, deployment.GetName(), 2, 0)
	VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)
	indexerPodName = fmt.Sprintf(MultiSiteIndexerPod, deployment.GetName(), 3, 0)
	VerifyLMConfiguredOnPod(ctx, deployment, indexerPodName)

	// Verify License Manager/Master is configured on SHs
	searchHeadPodName := fmt.Sprintf(SearchHeadPod, deployment.GetName(), 0)
	VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)
	searchHeadPodName = fmt.Sprintf(SearchHeadPod, deployment.GetName(), 1)
	VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)
	searchHeadPodName = fmt.Sprintf(SearchHeadPod, deployment.GetName(), 2)
	VerifyLMConfiguredOnPod(ctx, deployment, searchHeadPodName)

	// Verify License Manager/Master is configured on Monitoring Console
	monitoringConsolePodName := fmt.Sprintf(MonitoringConsolePod, deployment.GetName())
	VerifyLMConfiguredOnPod(ctx, deployment, monitoringConsolePodName)
}
