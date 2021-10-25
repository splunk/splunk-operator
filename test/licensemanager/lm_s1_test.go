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
package licensemanager

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Licensemanager test", func() {

	var deployment *testenv.Deployment

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

	Context("Standalone deployment (S1) with LM", func() {
		It("licensemanager: Splunk Operator can configure License Manager with Standalone in S1 SVA", func() {

			// Download License File
			licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			// Create standalone Deployment with License Manager
			standalone, err := deployment.DeployStandaloneWithLM(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for License Manager to be in READY status
			testenv.LicenseManagerReady(deployment, testenvInstance)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			// testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify LM is configured on standalone instance
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(deployment, standalonePodName)
		})
	})
})
