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

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
)

// SetupTestCaseEnv creates a new test case environment and deployment for use in BeforeEach blocks.
func SetupTestCaseEnv(testenvInstance *TestEnv, namePrefix string) (*TestCaseEnv, *Deployment) {
	name := fmt.Sprintf("%s-%s", namePrefix+testenvInstance.GetName(), RandomDNSName(3))
	testcaseEnvInst, err := NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
	Expect(err).To(Succeed(), "Unable to create testcaseenv")

	deployment, err := testcaseEnvInst.NewDeployment(RandomDNSName(3))
	Expect(err).To(Succeed(), "Unable to create deployment")

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
