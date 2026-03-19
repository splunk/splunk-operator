// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
package crcrud

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Crcrud test for SVA C3", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var defaultCPULimits string
	var newCPULimits string
	var verificationTimeout time.Duration

	ctx := context.TODO()

	BeforeEach(func() {
		defaultCPULimits = "4"
		newCPULimits = "2"
		verificationTimeout = 150 * time.Second

		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")

		// Validate test prerequisites early to fail fast
		err := testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("managercrcrud, integration, c3: can deploy indexer and search head cluster, change their CR, update the instances", func() {
			config := NewCRUDTestConfigV4()
			RunC3CPUUpdateTest(ctx, deployment, testcaseEnvInst, config, defaultCPULimits, newCPULimits)
		})
	})

	Context("Search Head Cluster", func() {
		// CSPL-3256 - Adding the SHC only test case under c3 as IDXC is irrelevant for this test case
		It("managercrcrud, integration, shc: can deploy Search Head Cluster with Deployer resource spec configured", func() {
			shcName := fmt.Sprintf("%s-shc", deployment.GetName())
			_, err := deployment.DeploySearchHeadCluster(ctx, shcName, "", "", "", "")
			if err != nil {
				Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster", "Shc", shcName)
			}

			// Verify CPU limits on Search Heads and deployer before updating CR
			searchHeadCount := 3
			for i := 0; i < searchHeadCount; i++ {
				SearchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				testcaseEnvInst.VerifyCPULimits(deployment, SearchHeadPodName, defaultCPULimits)
			}

			DeployerPodName := fmt.Sprintf(testenv.DeployerPod, deployment.GetName())
			testcaseEnvInst.VerifyCPULimits(deployment, DeployerPodName, defaultCPULimits)

			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(ctx, shcName, shc)
			Expect(err).To(Succeed(), "Unable to fetch Search Head Cluster deployment")

			// Assign new resources for deployer pod only
			newCPULimits = "4"
			newCPURequests := "2"
			newMemoryLimits := "14Gi"
			newMemoryRequests := "12Gi"

			depResSpec := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"cpu":    resource.MustParse(newCPURequests),
					"memory": resource.MustParse(newMemoryRequests),
				},
				Limits: corev1.ResourceList{
					"cpu":    resource.MustParse(newCPULimits),
					"memory": resource.MustParse(newMemoryLimits),
				},
			}
			shc.Spec.DeployerResourceSpec = depResSpec

			err = deployment.UpdateCR(ctx, shc)
			Expect(err).To(Succeed(), "Unable to deploy Search Head Cluster with updated CR")

			// Verify Search Head go to ready state
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Verify CPU limits on Search Heads - Should be same as before
			searchHeadCount = 3
			for i := 0; i < searchHeadCount; i++ {
				SearchHeadPodName := fmt.Sprintf(testenv.SearchHeadPod, deployment.GetName(), i)
				testcaseEnvInst.VerifyCPULimits(deployment, SearchHeadPodName, defaultCPULimits)
			}

			// Verify modified deployer spec
			testcaseEnvInst.VerifyResourceConstraints(deployment, DeployerPodName, depResSpec)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("managercrcrud, integration, c3: can verify IDXC, CM and SHC PVCs are correctly deleted after the CRs deletion", func() {

			// Deploy Single site Cluster and Search Head Clusters
			mcRef := deployment.GetName()
			err := deployment.DeploySingleSiteCluster(ctx, deployment.GetName(), 3, true /*shc*/, mcRef)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the Cluster Manager goes to Ready phase
			testcaseEnvInst.VerifyClusterManagerReady(ctx, deployment)

			// Ensure Indexers go to Ready phase
			testcaseEnvInst.VerifySingleSiteIndexersReady(ctx, deployment)

			// Verify Search Head go to ready state
			testcaseEnvInst.VerifySearchHeadClusterReady(ctx, deployment)

			// Deploy Monitoring Console CRD
			mc, err := deployment.DeployMonitoringConsole(ctx, mcRef, "")
			Expect(err).To(Succeed(), "Unable to deploy Monitoring Console One instance")

			// Verify Monitoring Console is Ready and stays in ready state
			testcaseEnvInst.VerifyMonitoringConsoleReady(ctx, deployment, deployment.GetName(), mc)

			// Verify RF SF is met
			testcaseEnvInst.VerifyRFSFMet(ctx, deployment)

			// Verify Search Heads PVCs (etc and var) exists
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-search-head", 3, true, verificationTimeout)

			// Verify Deployer PVCs (etc and var) exists
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-deployer", 1, true, verificationTimeout)

			// Verify Indexers PVCs (etc and var) exists
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "idxc-indexer", 3, true, verificationTimeout)

			// Verify Cluster Manager PVCs (etc and var) exists
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "cluster-manager", 1, true, verificationTimeout)

			// Delete the Search Head Cluster
			shc := &enterpriseApi.SearchHeadCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-shc", shc)
			Expect(err).To(Succeed(), "Unable to GET SHC instance", "SHC Name", shc)
			err = deployment.DeleteCR(ctx, shc)
			Expect(err).To(Succeed(), "Unable to delete SHC instance", "SHC Name", shc)

			// Delete the Indexer Cluster
			idxc := &enterpriseApi.IndexerCluster{}
			err = deployment.GetInstance(ctx, deployment.GetName()+"-idxc", idxc)
			Expect(err).To(Succeed(), "Unable to GET IDXC instance", "IDXC Name", idxc)
			err = deployment.DeleteCR(ctx, idxc)
			Expect(err).To(Succeed(), "Unable to delete IDXC instance", "IDXC Name", idxc)

			// Delete the Cluster Manager
			cm := &enterpriseApi.ClusterManager{}
			err = deployment.GetInstance(ctx, deployment.GetName(), cm)
			Expect(err).To(Succeed(), "Unable to GET Cluster Manager instance", "Cluster Manager Name", cm)
			err = deployment.DeleteCR(ctx, cm)
			Expect(err).To(Succeed(), "Unable to delete Cluster Manager instance", "Cluster Manger Name", cm)

			// Delete Monitoring Console
			err = deployment.GetInstance(ctx, mcRef, mc)
			Expect(err).To(Succeed(), "Unable to GET Monitoring Console instance", "Monitoring Console Name", mcRef)
			err = deployment.DeleteCR(ctx, mc)
			Expect(err).To(Succeed(), "Unable to delete Monitoring Console instance", "Monitoring Console Name", mcRef)

			// Verify Search Heads PVCs (etc and var) have been deleted
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-search-head", 3, false, verificationTimeout)

			// Verify Deployer PVCs (etc and var) have been deleted
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "shc-deployer", 1, false, verificationTimeout)

			// Verify Indexers PVCs (etc and var) have been deleted
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "idxc-indexer", 3, false, verificationTimeout)

			// Verify Cluster Manager PVCs (etc and var) have been deleted
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "cluster-manager", 1, false, verificationTimeout)

			// Verify Monitoring Console PVCs (etc and var) have been deleted
			testcaseEnvInst.VerifyPVCsPerDeployment(deployment, "monitoring-console", 1, false, verificationTimeout)
		})
	})
})
