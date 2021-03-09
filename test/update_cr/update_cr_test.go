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
package updatecr

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("update cr test", func() {

	var deployment *testenv.Deployment
	var storage string

	BeforeEach(func() {
		var err error
		deployment, err = testenvInstance.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		storage = "mnt-splunk-etc"
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
		It("update: can deploy a standalone instance, change its CR, update the instance ", func() {

			// Deploy Standalone
			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance")

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify no "mnt-splunk-etc" volume exists before update, as EphemeralStorage is not set by default
			standalonePodName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), standalonePodName, false, storage)

			// Change EphemeralStorage to "true" (this will create a volume named "mnt-splunk-etc") to trigger CR update
			standalone.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
			err = deployment.UpdateCR(standalone)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance with updated ImagePullPolicy")

			// Verify standalone is updating
			testenv.VerifyStandaloneUpdating(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Verify "mnt-splunk-etc" volume exists after update
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), standalonePodName, true, storage)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("update: can deploy indexers and search head cluster, change their CRs, update their instances", func() {

			// Deploy Cluster Master, Indexer Cluster, SH Cluster
			err := deployment.DeploySingleSiteCluster(deployment.GetName(), 3, true /*shc*/)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Ensure Search Head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify MC Pod is Ready
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(deployment, testenvInstance)

			// Verify no "mnt-splunk-etc" volume exists on Cluster Master before update, as EphemeralStorage is not set by default
			cmPodName := fmt.Sprintf(testenv.CMPod, deployment.GetName(), 0)
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), cmPodName, false, storage)

			// Change EphemeralStorage to "true" (this will create a volume named "mnt-splunk-etc") to trigger CR update
			cm := &enterprisev1.ClusterMaster{}
			err = deployment.GetInstance(deployment.GetName(), cm)
			if err != nil {
				testenvInstance.Log.Error(err, "Unable to fetch Cluster Master deployment")
			}
			cm.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
			err = deployment.UpdateCR(cm)
			Expect(err).To(Succeed(), "Unable to deploy Cluster Master instance with EphemeralStorage set to 'true'")

			// Verify Cluster Master is updating
			testenv.VerifyClusterMasterUpdating(deployment, deployment.GetName(), cm, testenvInstance)

			// Verify Cluster Master goes to ready state
			testenv.ClusterMasterReady(deployment, testenvInstance)

			// Verify "mnt-splunk-etc" volume exists on Cluster Master after update
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), cmPodName, true, storage)

			// Verify no "mnt-splunk-etc" volume exists on Indexer Cluster before update, as EphemeralStorage is not set by default
			indexerPodName := fmt.Sprintf(testenv.IndexerPod, deployment.GetName(), 0)
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), indexerPodName, false, storage)

			// Change EphemeralStorage to "true" (this will create a volume named "mnt-splunk-etc") to trigger CR update
			idxc := &enterprisev1.IndexerCluster{}
			instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
			err = deployment.GetInstance(instanceName, idxc)
			if err != nil {
				testenvInstance.Log.Error(err, "Unable to fetch Indexer Cluster deployment")
			}

			idxc.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
			err = deployment.UpdateCR(idxc)
			Expect(err).To(Succeed(), "Unable to deploy Indexer Cluster instance with EphemeralStorage set to 'true'")

			// Verify Indexer Cluster is updating
			testenv.VerifyIndexerClusterUpdating(deployment, deployment.GetName(), idxc, testenvInstance)

			// Verify Indexer Cluster goes to ready state
			testenv.SingleSiteIndexersReady(deployment, testenvInstance)

			// Verify "mnt-splunk-etc" volume exists on Indexer Cluster after update
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), indexerPodName, true, storage)

			// Verify no "mnt-splunk-etc" volume exists on Search Head Cluster before update, as EphemeralStorage is not set by default
			shPodName := fmt.Sprintf(testenv.SearchHeadSHCPod, deployment.GetName(), 0)
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), shPodName, false, storage)

			// Change EphemeralStorage to "true" (this will create a volume named "mnt-splunk-etc") to trigger CR update
			shc := &enterprisev1.SearchHeadCluster{}
			instanceName = fmt.Sprintf("%s-shc", deployment.GetName())
			fmt.Printf("instanceName SHC %v \n\n", instanceName)
			err = deployment.GetInstance(instanceName, shc)
			if err != nil {
				testenvInstance.Log.Error(err, "Unable to fetch Search Head Cluster deployment")
			}
			shc.Spec.EtcVolumeStorageConfig.EphemeralStorage = true
			err = deployment.UpdateCR(shc)
			Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster instance with EphemeralStorage set to 'true'")

			// Verify Search Head Cluster is updating
			testenv.VerifySHClusterUpdating(deployment, deployment.GetName(), shc, testenvInstance)

			// Verify Search Head Cluster goes to ready state
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Verify "mnt-splunk-etc" volume exists on  Indexer Cluster after update
			testenv.VerifyEphemeralStorage(deployment, testenvInstance.GetName(), shPodName, true, storage)

		})
	})
})
