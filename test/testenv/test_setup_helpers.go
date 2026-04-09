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

	"github.com/joho/godotenv"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
)

// SetupOption configures optional parameters for SetupTestCaseEnv.
type SetupOption func(*setupOptions)

type setupOptions struct {
	timeout *time.Duration
}

// WithTimeout overrides the default test timeout for the deployment.
func WithTimeout(seconds int) SetupOption {
	return func(o *setupOptions) {
		d := time.Duration(seconds) * time.Second
		o.timeout = &d
	}
}

// SetupTestCaseEnv creates a new test case environment and deployment for use in BeforeEach blocks.
// It also validates test prerequisites immediately to fail fast before any long operations.
func SetupTestCaseEnv(testenvInstance *TestEnv, namePrefix string, opts ...SetupOption) (*TestCaseEnv, *Deployment, error) {
	var o setupOptions
	for _, opt := range opts {
		opt(&o)
	}

	name := fmt.Sprintf("%s-%s", namePrefix+testenvInstance.GetName(), RandomDNSName(3))
	testcaseEnvInst, err := NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create testcaseenv: %w", err)
	}

	deployment, err := testcaseEnvInst.NewDeployment(RandomDNSName(3), o.timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create deployment: %w", err)
	}

	if err = testcaseEnvInst.ValidateTestPrerequisites(context.TODO(), deployment); err != nil {
		return nil, nil, fmt.Errorf("test prerequisites validation failed: %w", err)
	}

	return testcaseEnvInst, deployment, nil
}

// TeardownTestCaseEnv handles the common teardown logic for test case environments.
func TeardownTestCaseEnv(testcaseEnvInst *TestCaseEnv, deployment *Deployment) error {
	if types.SpecState(ginkgo.CurrentSpecReport().State) == types.SpecStateFailed {
		if testcaseEnvInst != nil {
			testcaseEnvInst.SkipTeardown = true
		}
	}

	if deployment != nil {
		deployment.Teardown()
	}

	if testcaseEnvInst != nil {
		if err := testcaseEnvInst.Teardown(); err != nil {
			return fmt.Errorf("teardown failed: %w", err)
		}
	}
	return nil
}

// CleanupOperatorFile deletes the test_file.img from the operator pod's app download directory
// if filePresentOnOperator is true.
func CleanupOperatorFile(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, filePresentOnOperator bool) {
	if filePresentOnOperator {
		opPod := testcaseEnvInst.GetOperatorPodName()
		podDownloadPath := filepath.Join(AppDownloadVolume, "test_file.img")
		DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
	}
}

// TeardownAppFrameworkTestCaseEnv handles teardown for app framework tests with provider-specific
// cloud storage cleanup. cloudCleanup is called only if SkipTeardown is false.
func TeardownAppFrameworkTestCaseEnv(ctx context.Context, testcaseEnvInst *TestCaseEnv, deployment *Deployment, cloudCleanup func(), filePresentOnOperator bool) error {
	if err := TeardownTestCaseEnv(testcaseEnvInst, deployment); err != nil {
		return err
	}

	if testcaseEnvInst != nil && !testcaseEnvInst.SkipTeardown && cloudCleanup != nil {
		cloudCleanup()
	}

	CleanupOperatorFile(ctx, deployment, testcaseEnvInst, filePresentOnOperator)
	return nil
}

// S3CloudCleanup returns a cleanup function that deletes the given files from an S3 bucket.
func S3CloudCleanup(bucket string, uploadedApps []string) func() {
	return func() {
		DeleteFilesOnS3(bucket, uploadedApps)
	}
}

// AzureCloudCleanup returns a cleanup function that deletes the given files from Azure Blob storage.
func AzureCloudCleanup(ctx context.Context, uploadedApps []string) func() {
	return func() {
		azureBlobClient := &AzureBlobClient{}
		azureBlobClient.DeleteFilesOnAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, uploadedApps)
	}
}

// GCPCloudCleanup returns a cleanup function that deletes the given files from a GCP bucket.
func GCPCloudCleanup(bucket string, uploadedApps []string) func() {
	return func() {
		DeleteFilesOnGCP(bucket, uploadedApps)
	}
}

// LoadEnvFile traverses up the directory tree from the current working directory
// to find and load a .env file using godotenv. Returns nil if no .env file is found.
func LoadEnvFile() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	for {
		envFile := filepath.Join(dir, ".env")
		if _, err := os.Stat(envFile); err == nil {
			return godotenv.Load(envFile)
		}

		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			return nil
		}
		dir = parentDir
	}
}

// SetupS3AppsSuite initialises the test environment and, when running on EKS,
// downloads the V1 and V2 app sets from S3.
func SetupS3AppsSuite(suiteName, testDataBucket, appDirV1, downloadDirV1, appDirV2, downloadDirV2 string) (*TestEnv, []string, []string, error) {
	testenvInst, err := NewDefaultTestEnv(suiteName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to create test env: %w", err)
	}

	if ClusterProvider == "eks" {
		appListV1 := BasicApps
		appFileList := GetAppFileList(appListV1)

		if err = DownloadFilesFromS3(testDataBucket, appDirV1, downloadDirV1, appFileList); err != nil {
			return nil, nil, nil, fmt.Errorf("unable to download V1 app files: %w", err)
		}

		appListV2 := append(appListV1, NewAppsAddedBetweenPolls...)
		appFileList = GetAppFileList(appListV2)

		if err = DownloadFilesFromS3(testDataBucket, appDirV2, downloadDirV2, appFileList); err != nil {
			return nil, nil, nil, fmt.Errorf("unable to download V2 app files: %w", err)
		}

		return testenvInst, appListV1, appListV2, nil
	}

	testenvInst.Log.Info("Skipping Before Suite Setup", "provider", ClusterProvider)
	return testenvInst, nil, nil, nil
}

// CleanupLocalAppDownloads tears down the test environment and removes locally
// downloaded app directories after a suite run.
func CleanupLocalAppDownloads(testenvInst *TestEnv, dirs ...string) error {
	if testenvInst != nil {
		if err := testenvInst.Teardown(); err != nil {
			return fmt.Errorf("teardown failed: %w", err)
		}
	}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("unable to delete locally downloaded app files from %s: %w", dir, err)
		}
	}
	return nil
}

// SetupAzureAppsSuite initialises the test environment and, when running on Azure,
// downloads the V1 and V2 app sets from Azure Blob.
func SetupAzureAppsSuite(suiteName, downloadDirV1, downloadDirV2 string) (*TestEnv, []string, []string, error) {
	testenvInst, err := NewDefaultTestEnv(suiteName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to create test env: %w", err)
	}

	if ClusterProvider == "azure" {
		ctx := context.TODO()

		appListV1 := BasicApps
		appFileList := GetAppFileList(appListV1)

		containerName := "/test-data/appframework/v1apps/"
		if err = DownloadFilesFromAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, downloadDirV1, containerName, appFileList); err != nil {
			return nil, nil, nil, fmt.Errorf("unable to download V1 app files: %w", err)
		}

		appListV2 := append(appListV1, NewAppsAddedBetweenPolls...)
		appFileList = GetAppFileList(appListV2)

		containerName = "/test-data/appframework/v2apps/"
		if err = DownloadFilesFromAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, downloadDirV2, containerName, appFileList); err != nil {
			return nil, nil, nil, fmt.Errorf("unable to download V2 app files: %w", err)
		}

		return testenvInst, appListV1, appListV2, nil
	}

	testenvInst.Log.Info("Skipping Before Suite Setup", "provider", ClusterProvider)
	return testenvInst, nil, nil, nil
}

// SetupGCPAppsSuite initialises the test environment and, when running on GCP,
// downloads the V1 and V2 app sets from GCS.
func SetupGCPAppsSuite(suiteName, testDataBucket, appDirV1, downloadDirV1, appDirV2, downloadDirV2 string) (*TestEnv, []string, []string, error) {
	testenvInst, err := NewDefaultTestEnv(suiteName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to create test env: %w", err)
	}

	if ClusterProvider == "gcp" {
		appListV1 := BasicApps
		appFileList := GetAppFileList(appListV1)

		testenvInst.Log.Info("logging download details", "bucket", testDataBucket, "appDirV1", appDirV1, "downloadDirV1", downloadDirV1, "appFileList", appFileList)
		if err = DownloadFilesFromGCP(testDataBucket, appDirV1, downloadDirV1, appFileList); err != nil {
			return nil, nil, nil, fmt.Errorf("unable to download V1 app files: %w", err)
		}

		appListV2 := append(appListV1, NewAppsAddedBetweenPolls...)
		appFileList = GetAppFileList(appListV2)

		if err = DownloadFilesFromGCP(testDataBucket, appDirV2, downloadDirV2, appFileList); err != nil {
			return nil, nil, nil, fmt.Errorf("unable to download V2 app files: %w", err)
		}

		return testenvInst, appListV1, appListV2, nil
	}

	testenvInst.Log.Info("Skipping Before Suite Setup", "provider", ClusterProvider)
	return testenvInst, nil, nil, nil
}

// SetupLicenseConfigMap downloads the license file from the appropriate provider
// and creates a license config map.
func SetupLicenseConfigMap(ctx context.Context, testcaseEnvInst *TestCaseEnv) error {
	downloadDir := "licenseFolder"
	var licenseFilePath string
	var err error

	switch ClusterProvider {
	case "eks":
		licenseFilePath, err = DownloadLicenseFromS3Bucket()
	case "azure":
		licenseFilePath, err = DownloadLicenseFromAzure(ctx, downloadDir)
	case "gcp":
		licenseFilePath, err = DownloadLicenseFromGCPBucket()
	default:
		testcaseEnvInst.Log.Info("Skipping license download", "provider", ClusterProvider)
		return nil
	}

	if err != nil {
		return fmt.Errorf("unable to download license file: %w", err)
	}

	if err := testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath); err != nil {
		return fmt.Errorf("unable to create license config map: %w", err)
	}
	return nil
}
