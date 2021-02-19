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
package smoke

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
)

func dumpGetPods(ns string) {
	output, _ := exec.Command("kubectl", "get", "pod", "-n", ns).Output()
	for _, line := range strings.Split(string(output), "\n") {
		testenvInstance.Log.Info(line)
	}
}

var _ = Describe("Smoke test", func() {

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

	Context("Standalone deployment (S1)", func() {
		It("smoke: can deploy a standalone instance", func() {

			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("smoke: can deploy indexers and search head cluster", func() {

			err := deployment.DeploySingleSiteCluster(deployment.GetName(), 3, true /*shc*/)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)
		})
	})

	Context("Multisite cluster deployment (M13 - Multisite indexer cluster, Search head cluster)", func() {
		It("smoke: can deploy indexers and search head cluster", func() {

			siteCount := 3
			err := deployment.DeployMultisiteClusterWithSearchHead(deployment.GetName(), 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)
		})
	})

	Context("Multisite cluster deployment (M1 - multisite indexer cluster)", func() {
		It("smoke: can deploy multisite indexers cluster", func() {

			siteCount := 3
			err := deployment.DeployMultisiteCluster(deployment.GetName(), 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure the indexers of all sites go to Ready phase
			testenv.IndexersReady(deployment, testenvInstance, siteCount)

			// Ensure cluster configured as multisite
			testenv.IndexerClusterMultisiteStatus(deployment, testenvInstance, siteCount)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)
		})
	})

	Context("Standalone deployment (S1) with LM", func() {
		It("smoke: can deploy a standalone instance and a License Master", func() {
			// Download License File
			licenseFilePath, err := testenv.DownloadFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to downlaod license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			// Create standalone Deployment with License Master
			standalone, err := deployment.DeployStandaloneWithLM(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for License Master to be in READY status
			testenv.LicenseMasterReady(deployment, testenvInstance)

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify LM is configured on standalone instance
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(deployment, standalonePodName)

			// Verify LM is configured on MC Pod
			mcPodName := fmt.Sprintf(testenv.MonitoringConsolePod, testenvInstance.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(deployment, mcPodName)
		})
	})

	Context("Standalone deployment (S1) with Service Account", func() {
		It("smoke: can deploy a standalone instance attached to a service account", func() {
			// Create Service Account
			serviceAccountName := "smoke-service-account"
			testenvInstance.CreateServiceAccount(serviceAccountName)

			standaloneSpec := enterprisev1.StandaloneSpec{
				CommonSplunkSpec: enterprisev1.CommonSplunkSpec{
					Spec: splcommon.Spec{
						ImagePullPolicy: "IfNotPresent",
					},
					Volumes:        []corev1.Volume{},
					ServiceAccount: serviceAccountName,
				},
			}

			// Create standalone Deployment with License Master
			standalone, err := deployment.DeployStandalonewithGivenSpec(deployment.GetName(), standaloneSpec)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with LM")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify serviceAccount is configured on Pod
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyServiceAccountConfiguredOnPod(deployment, testenvInstance.GetName(), standalonePodName, serviceAccountName)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer) with LM", func() {
		It("smoke: can deploy indexer cluster with LM", func() {
			// Download License File
			licenseFilePath, err := testenv.DownloadFromS3Bucket()
			Expect(err).To(Succeed(), "Unable to downlaod license file")

			// Create License Config Map
			testenvInstance.CreateLicenseConfigMap(licenseFilePath)

			// Create Cluster Master with LicenseMasterRef, IndexerCluster without LicenseMasterRef
			err = deployment.DeploySingleSiteCluster(deployment.GetName(), 3, true /*shc*/)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify LM is configured on indexers
			indexerPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
			testenv.VerifyLMConfiguredOnPod(deployment, indexerPodName)

		})
	})
})
