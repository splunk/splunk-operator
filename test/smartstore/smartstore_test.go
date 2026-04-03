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
package smartstore

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

// smartstoreTestConfig extends MasterManagerTestConfig with a per-variant
// timeout used by the S1 multiple-indexes test.
type smartstoreTestConfig struct {
	testenv.MasterManagerTestConfig
	S1IndexesTimeout time.Duration
}

// masterManagerSmartstoreConfigs defines the V3 (master) and V4 (manager) variants
// shared by the S1 and M4 smartstore test tables.
var masterManagerSmartstoreConfigs = []smartstoreTestConfig{
	{testenv.MasterManagerTestConfig{NamePrefix: "master", Label: "mastersmartstore", NewConfig: testenv.NewClusterReadinessConfigV3}, 2 * time.Minute},
	{testenv.MasterManagerTestConfig{NamePrefix: "", Label: "managersmartstore", NewConfig: testenv.NewClusterReadinessConfigV4}, 5 * time.Minute},
}

var _ = Describe("Smartstore test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	ctx := context.TODO()

	for _, tc := range masterManagerSmartstoreConfigs {
		tc := tc
		Context("Standalone deployment (S1)", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", integration: Can configure multiple indexes through app", func() {
				RunS1MultipleIndexesTest(ctx, deployment, testcaseEnvInst, tc.S1IndexesTimeout)
			})

			It(tc.Label+", integration: Can configure indexes which use default volumes through app", func() {
				RunS1DefaultVolumesTest(ctx, deployment, testcaseEnvInst)
			})
		})

		Context("Multisite Indexer Cluster with Search Head Cluster (M4)", func() {
			BeforeEach(func() {
				var err error
				testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, tc.NamePrefix)
				Expect(err).To(Succeed(), "Failed to setup test case environment")
			})

			AfterEach(func() {
				Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
			})

			It(tc.Label+", m4, integration: Can configure indexes and volumes on Multisite Indexer Cluster through app", func() {
				config := tc.NewConfig()
				RunM4MultisiteSmartStoreTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}

	Context("Standalone deployment (S1) with App Framework", func() {
		BeforeEach(func() {
			var err error
			testcaseEnvInst, deployment, err = testenv.SetupTestCaseEnv(testenvInstance, "master")
			Expect(err).To(Succeed(), "Failed to setup test case environment")
		})

		AfterEach(func() {
			Expect(testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)).To(Succeed(), "Failed to teardown test case environment")
		})

		It("integration, s1, smartstore: can deploy a Standalone instance with Ephemeral Etc storage", func() {
			storageConfig := enterpriseApi.StorageClassSpec{StorageClassName: "TestStorageEtcEph", StorageCapacity: "1Gi", EphemeralStorage: true}
			RunS1EphemeralStorageTest(ctx, deployment, testcaseEnvInst, storageConfig, true)
		})

		It("integration, s1, smartstore: can deploy a Standalone instance with Ephemeral Var storage", func() {
			storageConfig := enterpriseApi.StorageClassSpec{StorageClassName: "TestStorageVarEph", StorageCapacity: "1Gi", EphemeralStorage: true}
			RunS1EphemeralStorageTest(ctx, deployment, testcaseEnvInst, storageConfig, false)
		})
	})
})
