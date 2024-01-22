// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package deletecr

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	testenv "github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("DeleteCR test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if CurrentGinkgoTestDescription().Failed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Standalone deployment (S1 - Standalone Pod)", func() {
		It("integration, managerdeletecr: can deploy standalone and delete", func() {

			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
					},
					Volumes: []corev1.Volume{},
				},
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(ctx, deployment, deployment.GetName(), standalone, testcaseEnvInst)

			// Delete Standalone CR
			err = deployment.DeleteCR(ctx, standalone)
			Expect(err).To(Succeed(), "Unable to Delete Standalone")

		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3)", func() {
		It("integration, managerdeletecr: can deploy C3 and delete search head, clustermanager", func() {

			// Deploy C3
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), indexerReplicas, true, "")
			Expect(err).To(Succeed(), "Unable to deploy C3 instance")

			// Ensure Cluster Manager goes to Ready phase
			testenv.ClusterManagerReady(ctx, deployment, testcaseEnvInst)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			idxc := &enterpriseApi.IndexerCluster{}
			idxcName := deployment.GetName() + "-idxc"
			err = deployment.GetInstance(ctx, idxcName, idxc)
			Expect(err).To(Succeed(), "Unable to get Indexer instance")

			// Delete Indexer Cluster CR
			err = deployment.DeleteCR(ctx, idxc)
			Expect(err).To(Succeed(), "Unable to Delete Indexer Cluster")

			sh := &enterpriseApi.SearchHeadCluster{}
			shcName := deployment.GetName() + "-shc"
			err = deployment.GetInstance(ctx, shcName, sh)
			Expect(err).To(Succeed(), "Unable to get Search Head instance")

			// Delete Search Head Cluster CR
			err = deployment.DeleteCR(ctx, sh)
			Expect(err).To(Succeed(), "Unable to Delete Search Head Cluster")

			cm := &enterpriseApi.ClusterManager{}
			cmName := deployment.GetName()
			err = deployment.GetInstance(ctx, cmName, cm)
			Expect(err).To(Succeed(), "Unable to get Cluster Manager instance")

			// Delete Cluster Manager CR
			err = deployment.DeleteCR(ctx, cm)
			Expect(err).To(Succeed(), "Unable to Delete Cluster Manager")

		})
	})
})
