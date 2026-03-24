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

	"github.com/joho/godotenv"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
)

// SetupTestCaseEnv creates a new test case environment and deployment for use in BeforeEach blocks.
// It also validates test prerequisites immediately to fail fast before any long operations.
func SetupTestCaseEnv(testenvInstance *TestEnv, namePrefix string) (*TestCaseEnv, *Deployment) {
	name := fmt.Sprintf("%s-%s", namePrefix+testenvInstance.GetName(), RandomDNSName(3))
	testcaseEnvInst, err := NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
	Expect(err).To(Succeed(), "Unable to create testcaseenv")

	deployment, err := testcaseEnvInst.NewDeployment(RandomDNSName(3))
	Expect(err).To(Succeed(), "Unable to create deployment")

	err = testcaseEnvInst.ValidateTestPrerequisites(context.TODO(), deployment)
	Expect(err).To(Succeed(), "Test prerequisites validation failed")

	return testcaseEnvInst, deployment
}

// TeardownTestCaseEnv handles the common teardown logic for test case environments.
func TeardownTestCaseEnv(testcaseEnvInst *TestCaseEnv, deployment *Deployment) {
	if types.SpecState(ginkgo.CurrentSpecReport().State) == types.SpecStateFailed {
		if testcaseEnvInst != nil {
			testcaseEnvInst.SkipTeardown = true
		}
	}

	if deployment != nil {
		deployment.Teardown()
	}

	if testcaseEnvInst != nil {
		Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
	}
}

// CleanupOperatorFile deletes the test_file.img from the operator pod's app download directory
// if filePresentOnOperator is true.
func CleanupOperatorFile(ctx context.Context, deployment *Deployment, testcaseEnvInst *TestCaseEnv, filePresentOnOperator bool) {
	if filePresentOnOperator {
		opPod := GetOperatorPodName(testcaseEnvInst)
		podDownloadPath := filepath.Join(AppDownloadVolume, "test_file.img")
		DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
	}
}

// TeardownAppFrameworkTestCaseEnv handles teardown for app framework tests with provider-specific
// cloud storage cleanup. cloudCleanup is called only if SkipTeardown is false.
func TeardownAppFrameworkTestCaseEnv(ctx context.Context, testcaseEnvInst *TestCaseEnv, deployment *Deployment, cloudCleanup func(), filePresentOnOperator bool) {
	TeardownTestCaseEnv(testcaseEnvInst, deployment)

	if testcaseEnvInst != nil && !testcaseEnvInst.SkipTeardown && cloudCleanup != nil {
		cloudCleanup()
	}

	CleanupOperatorFile(ctx, deployment, testcaseEnvInst, filePresentOnOperator)
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
func SetupS3AppsSuite(suiteName, testDataBucket, appDirV1, downloadDirV1, appDirV2, downloadDirV2 string) (*TestEnv, []string, []string) {
	testenvInst, err := NewDefaultTestEnv(suiteName)
	Expect(err).ToNot(HaveOccurred())

	if ClusterProvider == "eks" {
		appListV1 := BasicApps
		appFileList := GetAppFileList(appListV1)

		err = DownloadFilesFromS3(testDataBucket, appDirV1, downloadDirV1, appFileList)
		Expect(err).To(Succeed(), "Unable to download V1 app files")

		appListV2 := append(appListV1, NewAppsAddedBetweenPolls...)
		appFileList = GetAppFileList(appListV2)

		err = DownloadFilesFromS3(testDataBucket, appDirV2, downloadDirV2, appFileList)
		Expect(err).To(Succeed(), "Unable to download V2 app files")

		return testenvInst, appListV1, appListV2
	}

	testenvInst.Log.Info("Skipping Before Suite Setup", "provider", ClusterProvider)
	return testenvInst, nil, nil
}

// CleanupLocalAppDownloads tears down the test environment and removes locally
// downloaded app directories after a suite run.
func CleanupLocalAppDownloads(testenvInst *TestEnv, dirs ...string) {
	if testenvInst != nil {
		Expect(testenvInst.Teardown()).ToNot(HaveOccurred())
	}
	for _, dir := range dirs {
		Expect(os.RemoveAll(dir)).To(Succeed(), "Unable to delete locally downloaded app files from "+dir)
	}
}

// SetupAzureAppsSuite initialises the test environment and, when running on Azure,
// downloads the V1 and V2 app sets from Azure Blob.
func SetupAzureAppsSuite(suiteName, downloadDirV1, downloadDirV2 string) (*TestEnv, []string, []string) {
	testenvInst, err := NewDefaultTestEnv(suiteName)
	Expect(err).ToNot(HaveOccurred())

	if ClusterProvider == "azure" {
		ctx := context.TODO()

		appListV1 := BasicApps
		appFileList := GetAppFileList(appListV1)

		containerName := "/test-data/appframework/v1apps/"
		err = DownloadFilesFromAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, downloadDirV1, containerName, appFileList)
		Expect(err).To(Succeed(), "Unable to download V1 app files")

		appListV2 := append(appListV1, NewAppsAddedBetweenPolls...)
		appFileList = GetAppFileList(appListV2)

		containerName = "/test-data/appframework/v2apps/"
		err = DownloadFilesFromAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, downloadDirV2, containerName, appFileList)
		Expect(err).To(Succeed(), "Unable to download V2 app files")

		return testenvInst, appListV1, appListV2
	}

	testenvInst.Log.Info("Skipping Before Suite Setup", "provider", ClusterProvider)
	return testenvInst, nil, nil
}

// SetupGCPAppsSuite initialises the test environment and, when running on GCP,
// downloads the V1 and V2 app sets from GCS.
func SetupGCPAppsSuite(suiteName, testDataBucket, appDirV1, downloadDirV1, appDirV2, downloadDirV2 string) (*TestEnv, []string, []string) {
	testenvInst, err := NewDefaultTestEnv(suiteName)
	Expect(err).ToNot(HaveOccurred())

	if ClusterProvider == "gcp" {
		appListV1 := BasicApps
		appFileList := GetAppFileList(appListV1)

		testenvInst.Log.Info("logging download details", "bucket", testDataBucket, "appDirV1", appDirV1, "downloadDirV1", downloadDirV1, "appFileList", appFileList)
		err = DownloadFilesFromGCP(testDataBucket, appDirV1, downloadDirV1, appFileList)
		Expect(err).To(Succeed(), "Unable to download V1 app files")

		appListV2 := append(appListV1, NewAppsAddedBetweenPolls...)
		appFileList = GetAppFileList(appListV2)

		err = DownloadFilesFromGCP(testDataBucket, appDirV2, downloadDirV2, appFileList)
		Expect(err).To(Succeed(), "Unable to download V2 app files")

		return testenvInst, appListV1, appListV2
	}

	testenvInst.Log.Info("Skipping Before Suite Setup", "provider", ClusterProvider)
	return testenvInst, nil, nil
}

// SetupLicenseConfigMap downloads the license file from the appropriate cloud provider
// and creates a license config map.
func SetupLicenseConfigMap(ctx context.Context, testcaseEnvInst *TestCaseEnv) {
	downloadDir := "licenseFolder"
	var licenseFilePath string
	var err error

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
		testcaseEnvInst.Log.Info("Skipping license download", "ClusterProvider", ClusterProvider)
		return
	}

	testcaseEnvInst.CreateLicenseConfigMap(licenseFilePath)
}
