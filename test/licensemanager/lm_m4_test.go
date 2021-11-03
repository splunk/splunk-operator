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

	Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster)", func() {
		It("licensemanager, integration, m4: Splunk Operator can configure license manager with indexers and search head in M4 SVA", func() {

			// Download License File
			licenseFilePath, err := testenv.DownloadLicenseFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to download license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			siteCount := 3
			mcRef := deployment.GetName()
			err = deployment.DeployMultisiteClusterWithSearchHead(deployment.GetName(), 1, siteCount, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-manager goes to Ready phase
			testenv.ClusterManagerReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(mcRef, deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testenv.VerifyMonitoringConsoleReady(deployment, deployment.GetName(), mc, testenvInstance)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify LM is configured on indexers
			indexerPodName := fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 1, 0)
			testenv.VerifyLMConfiguredOnPod(deployment, indexerPodName)
			indexerPodName = fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 2, 0)
			testenv.VerifyLMConfiguredOnPod(deployment, indexerPodName)
			indexerPodName = fmt.Sprintf(testenv.MultiSiteIndexerPod, deployment.GetName(), 3, 0)
			testenv.VerifyLMConfiguredOnPod(deployment, indexerPodName)

			// Verify LM is configured on SHs
			searchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(deployment, searchHeadPodName)
			searchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 1)
			testenv.VerifyLMConfiguredOnPod(deployment, searchHeadPodName)
			searchHeadPodName = fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), 2)
			testenv.VerifyLMConfiguredOnPod(deployment, searchHeadPodName)

			// Verify LM Configured on Monitoring Console
			monitoringConsolePodName := fmt.Sprintf(testenv.MonitoringConsolePod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(deployment, monitoringConsolePodName)
		})
	})

})
