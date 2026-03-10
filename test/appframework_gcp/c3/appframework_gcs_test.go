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
// limitations under the License.s
package c3gcpappfw

import (
	"context"
	"fmt"
	"path/filepath"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	testenv "github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("c3appfw test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv

	var deployment *testenv.Deployment
	var gcsTestDirShc string
	var gcsTestDirIdxc string
	//var gcsTestDirShcLocal string
	//var gcsTestDirIdxcLocal string
	//var gcsTestDirShcCluster string
	//var gcsTestDirIdxcCluster string
	var appSourceNameIdxc string
	var appSourceNameShc string
	var uploadedApps []string
	var filePresentOnOperator bool
	var backend testenv.CloudStorageBackend

	ctx := context.TODO()

	BeforeEach(func() {

		var err error
		name := fmt.Sprintf("%s-%s", "master"+testenvInstance.GetName(), testenv.RandomDNSName(3))
		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")
		testenv.SpecifiedTestTimeout = 5000
		backend = testenv.NewCloudStorageBackend(testGcsBucket, testDataGcsBucket)
		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if types.SpecState(CurrentSpecReport().State) == types.SpecStateFailed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}

		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}

		// Delete files uploaded to cloud storage
		if !testcaseEnvInst.SkipTeardown {
			backend.DeleteFiles(ctx, uploadedApps)
		}

		if filePresentOnOperator {
			//Delete files from app-directory
			opPod := testenv.GetOperatorPodName(testcaseEnvInst)
			podDownloadPath := filepath.Join(testenv.AppDownloadVolume, "test_file.img")
			testenv.DeleteFilesOnOperatorPod(ctx, deployment, opPod, []string{podDownloadPath})
		}
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It(" c3gcp, masterappframeworkc3gcp,  c3_gcp_sanity: can deploy a C3 SVA with App Framework enabled, install apps then upgrade them", func() {
			testenv.C3AppFrameworkUpgradeTest(ctx, backend, testcaseEnvInst, deployment, appListV1, appListV2, downloadDirV1, downloadDirV2, &uploadedApps)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) with App Framework", func() {
		It(" c3gcp, masterappframeworkc3gcp,  c3_gcp_sanity: can deploy a C3 SVA with App Framework enabled, install apps then downgrade them", func() {
			testenv.C3AppFrameworkDowngradeTest(ctx, backend, testcaseEnvInst, deployment, appListV1, appListV2, downloadDirV1, downloadDirV2, &uploadedApps)
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It(" c3gcp, masterappframeworkc3gcp,  c3_gcp_sanity: can deploy a C3 SVA and have apps installed locally on Cluster Manager and Deployer", func() {
			testenv.C3AppFrameworkLocalScopeUpgradeTest(ctx, backend, testcaseEnvInst, deployment, appListV1, appListV2, downloadDirV1, downloadDirV2, &uploadedApps)
		})
	})

	Context("Single Site Indexer Cluster with Search Head Cluster (C3) and App Framework", func() {
		It(" c3gcp, masterappframeworkc3gcp,  c3_gcp_sanity: can deploy a C3 SVA with App Framework enabled and check isDeploymentInProgressFlag for CM and SHC CR's", func() {

			/*
			   Test Steps
			   ################## SETUP ##################
			   * Upload V1 apps to GCS for Indexer Cluster and Search Head Cluster
			   * Prepare and deploy C3 CRD with app framework
			   * Verify IsDeploymentInProgress is set
			   * Wait for the pods to be ready
			*/

			//################## SETUP ####################
			appVersion := "V1"
			appFileList := testenv.GetAppFileList(appListV1)

			// Upload V1 apps to GCS for Indexer Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Indexer Cluster", appVersion))
			gcsTestDirIdxc = "c3appfw-idxc-" + testenv.RandomDNSName(4)
			uploadedFiles, err := testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirIdxc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Indexer Cluster %s", appVersion, testGcsBucket))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Upload V1 apps to GCS for Search Head Cluster
			testcaseEnvInst.Log.Info(fmt.Sprintf("Upload %s apps to GCS for Search Head Cluster", appVersion))
			gcsTestDirShc = "c3appfw-shc-" + testenv.RandomDNSName(4)
			uploadedFiles, err = testenv.UploadFilesToGCP(testGcsBucket, gcsTestDirShc, appFileList, downloadDirV1)
			Expect(err).To(Succeed(), fmt.Sprintf("Unable to upload %s apps to GCS test directory for Search Head Cluster", appVersion))
			uploadedApps = append(uploadedApps, uploadedFiles...)

			// Create App framework Spec for C3
			appSourceNameIdxc = "appframework-idxc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceNameShc = "appframework-shc-" + enterpriseApi.ScopeCluster + testenv.RandomDNSName(3)
			appSourceVolumeNameIdxc := "appframework-test-volume-idxc-" + testenv.RandomDNSName(3)
			appSourceVolumeNameShc := "appframework-test-volume-shc-" + testenv.RandomDNSName(3)
			appFrameworkSpecIdxc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameIdxc, enterpriseApi.ScopeCluster, appSourceNameIdxc, gcsTestDirIdxc, 60)
			appFrameworkSpecShc := testenv.GenerateAppFrameworkSpec(ctx, testcaseEnvInst, appSourceVolumeNameShc, enterpriseApi.ScopeCluster, appSourceNameShc, gcsTestDirShc, 60)

			// Deploy C3 CRD
			testcaseEnvInst.Log.Info("Deploy Single Site Indexer Cluster with Search Head Cluster")
			indexerReplicas := 3
			cm, _, shc, err := deployment.DeploySingleSiteClusterMasterWithGivenAppFrameworkSpec(ctx, deployment.GetName(), indexerReplicas, true, appFrameworkSpecIdxc, appFrameworkSpecShc, "", "")
			Expect(err).To(Succeed(), "Unable to deploy Single Site Indexer Cluster with Search Head Cluster")

			// Verify IsDeploymentInProgress Flag is set to true for Cluster Master CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, cm.Name, cm.Kind)

			// Ensure Cluster Master goes to Ready phase
			testenv.ClusterMasterReady(ctx, deployment, testcaseEnvInst)

			// Verify IsDeploymentInProgress Flag is set to true for SHC CR
			testcaseEnvInst.Log.Info("Checking isDeploymentInProgress Flag")
			testenv.VerifyIsDeploymentInProgressFlagIsSet(ctx, deployment, testcaseEnvInst, shc.Name, shc.Kind)

			// Ensure Search Head Cluster go to Ready phase
			testenv.SearchHeadClusterReady(ctx, deployment, testcaseEnvInst)

			// Ensure Indexers go to Ready phase
			testenv.SingleSiteIndexersReady(ctx, deployment, testcaseEnvInst)

			// Verify RF SF is met
			testenv.VerifyRFSFMet(ctx, deployment, testcaseEnvInst)
		})
	})
})
