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
	"encoding/json"
	"fmt"
	"strings"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"

	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type licenserLocalPeerResponse struct {
	Entry []struct {
		Name    string `json:"name"`
		ID      string `json:"id"`
		Content struct {
			GUID                     []string `json:"guid"`
			LastTrackerdbServiceTime int      `json:"last_trackerdb_service_time"`
			LicenseKeys              []string `json:"license_keys"`
			ManagerGUID              string   `json:"master_guid"`
			ManagerURI               string   `json:"master_uri"`
		} `json:"content"`
	} `json:"entry"`
}

// CheckLicenseManagerConfigured checks if lm is configured on given pod
func CheckLicenseManagerConfigured(ctx context.Context, deployment *Deployment, podName string) bool {
	stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) " + splcommon.LocalURLLicensePeerJSONOutput
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(ctx, podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return false
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
	restResponse := licenserLocalPeerResponse{}
	err = json.Unmarshal([]byte(stdout), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse health status")
		return false
	}
	licenseManager := restResponse.Entry[0].Content.ManagerURI
	logf.Log.Info("License Manager configuration on POD", "pod", podName, "licenseManager", licenseManager)
	return strings.Contains(licenseManager, "license-manager-service:8089") || strings.Contains(licenseManager, "license-master-service:8089")
}

// LicenseTestConfig holds the version-specific (V3/V4) deployment and verification
// callbacks used by the license manager test functions.
type LicenseTestConfig struct {
	*ClusterReadinessConfig
	DeployLicenseManagerWithGivenSpec func(ctx context.Context, deployment *Deployment, name string, spec interface{}) (interface{}, error)
	BuildLMAppFrameworkSpec           func(testcaseEnvInst *TestCaseEnv, appFrameworkSpec enterpriseApi.AppFrameworkSpec) interface{}
	LicenseManagerPodName             string
}

// NewLicenseCommonSplunkSpec returns a CommonSplunkSpec pre-configured with the
// license config map volume and license URL.
func NewLicenseCommonSplunkSpec(testcaseEnvInst *TestCaseEnv) enterpriseApi.CommonSplunkSpec {
	return enterpriseApi.CommonSplunkSpec{
		Volumes: []corev1.Volume{{
			Name: "licenses",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: testcaseEnvInst.GetLMConfigMap(),
					},
				},
			},
		}},
		LicenseURL: "/mnt/licenses/enterprise.lic",
		Spec:       enterpriseApi.Spec{ImagePullPolicy: "Always", Image: testcaseEnvInst.GetSplunkImage()},
	}
}

// NewLicenseMasterConfig returns a LicenseTestConfig for V3 (LicenseMaster) tests.
func NewLicenseMasterConfig() *LicenseTestConfig {
	return &LicenseTestConfig{
		ClusterReadinessConfig: NewClusterReadinessConfigV3(),
		DeployLicenseManagerWithGivenSpec: func(ctx context.Context, deployment *Deployment, name string, spec interface{}) (interface{}, error) {
			return deployment.DeployLicenseMasterWithGivenSpec(ctx, name, spec.(enterpriseApiV3.LicenseMasterSpec))
		},
		BuildLMAppFrameworkSpec: func(testcaseEnvInst *TestCaseEnv, appFrameworkSpec enterpriseApi.AppFrameworkSpec) interface{} {
			return enterpriseApiV3.LicenseMasterSpec{
				CommonSplunkSpec:   NewLicenseCommonSplunkSpec(testcaseEnvInst),
				AppFrameworkConfig: appFrameworkSpec,
			}
		},
		LicenseManagerPodName: LicenseMasterPod,
	}
}

// NewLicenseManagerConfig returns a LicenseTestConfig for V4 (LicenseManager) tests.
func NewLicenseManagerConfig() *LicenseTestConfig {
	return &LicenseTestConfig{
		ClusterReadinessConfig: NewClusterReadinessConfigV4(),
		DeployLicenseManagerWithGivenSpec: func(ctx context.Context, deployment *Deployment, name string, spec interface{}) (interface{}, error) {
			return deployment.DeployLicenseManagerWithGivenSpec(ctx, name, spec.(enterpriseApi.LicenseManagerSpec))
		},
		BuildLMAppFrameworkSpec: func(testcaseEnvInst *TestCaseEnv, appFrameworkSpec enterpriseApi.AppFrameworkSpec) interface{} {
			return enterpriseApi.LicenseManagerSpec{
				CommonSplunkSpec:   NewLicenseCommonSplunkSpec(testcaseEnvInst),
				AppFrameworkConfig: appFrameworkSpec,
			}
		},
		LicenseManagerPodName: LicenseManagerPod,
	}
}

// DownloadAppFiles downloads app files from the appropriate cloud provider.
func DownloadAppFiles(ctx context.Context, testDataS3Bucket, azureDataContainer, appDir, downloadDir string, appFileList []string, version string) error {
	var err error

	switch ClusterProvider {
	case "eks":
		err = DownloadFilesFromS3(testDataS3Bucket, appDir, downloadDir, appFileList)
	case "azure":
		containerName := "/" + azureDataContainer + "/" + appDir
		err = DownloadFilesFromAzure(ctx, GetAzureEndpoint(ctx), StorageAccountKey, StorageAccount, downloadDir, containerName, appFileList)
	case "gcp":
		err = DownloadFilesFromGCP(testDataS3Bucket, appDir, downloadDir, appFileList)
	}

	if err != nil {
		return fmt.Errorf("unable to download %s app files: %w", version, err)
	}
	return nil
}

// UploadAppFiles uploads app files to the appropriate cloud provider and returns the uploaded file paths.
func UploadAppFiles(ctx context.Context, testcaseEnvInst *TestCaseEnv, testS3Bucket, testDir, downloadDir string, appFileList []string, version string) ([]string, error) {
	var uploadedFiles []string
	var err error

	switch ClusterProvider {
	case "eks":
		testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to S3", version))
		uploadedFiles, err = UploadFilesToS3(testS3Bucket, testDir, appFileList, downloadDir)
	case "azure":
		testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to Azure", version))
		uploadedFiles, err = UploadFilesToAzure(ctx, StorageAccount, StorageAccountKey, downloadDir, testDir, appFileList)
	case "gcp":
		testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCP", version))
		uploadedFiles, err = UploadFilesToGCP(testS3Bucket, testDir, appFileList, downloadDir)
	}

	if err != nil {
		return nil, fmt.Errorf("unable to upload %s apps: %w", version, err)
	}
	return uploadedFiles, nil
}

// DeleteUploadedFiles removes previously uploaded app files from the appropriate cloud provider.
func DeleteUploadedFiles(ctx context.Context, testS3Bucket string, uploadedApps []string) {
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
