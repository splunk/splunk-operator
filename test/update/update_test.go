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
package update

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("update test", func() {

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
		It("update: can deploy a standalone instance with an older splunk version, then update it", func() {

			// Deploy old Splunk version
			baseline := "splunk/splunk:8.0.6"
			dockerBaseline := "docker.io/" + baseline
			standalone, err := deployment.DeployStandaloneBaseline(deployment.GetName(), dockerBaseline)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with older splunk version")

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify splunk version before update
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifySplunkVersion(deployment, testenvInstance.GetName(), standalonePodName, baseline)

			// Update splunk version to latest
			newImage := "splunk/splunk:latest"
			standalone.Spec.Image = newImage
			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with latest splunk version")

			// Verify standalone is updating
			testenv.VerifyStandaloneUpdating(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify splunk version has been updated to latest
			testenv.VerifySplunkVersion(deployment, testenvInstance.GetName(), standalonePodName, newImage)
		})
	})

	XContext("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("update: can deploy indexers and search head cluster", func() {

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

	XContext("Multisite cluster deployment (M13 - Multisite indexer cluster, Search head cluster)", func() {
		It("update: can deploy indexers and search head cluster", func() {

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

	XContext("Multisite cluster deployment (M1 - multisite indexer cluster)", func() {
		It("update: can deploy multisite indexers cluster", func() {

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

	XContext("Standalone deployment (S1) with Service Account", func() {
		It("update: can deploy a standalone instance attached to a service account", func() {
			// Create Service Account
			serviceAccountName := "update-service-account"
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
})
