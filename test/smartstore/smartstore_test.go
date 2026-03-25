package smartstore

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Smartstore test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	ctx := context.TODO()
	var deployment *testenv.Deployment

	smartstoreConfigs := []struct {
		namePrefix       string
		label            string
		s1IndexesTimeout time.Duration
		newConfig        func() *testenv.ClusterReadinessConfig
	}{
		{"master", "mastersmartstore", 2 * time.Minute, testenv.NewClusterReadinessConfigV3},
		{"", "managersmartstore", 5 * time.Minute, testenv.NewClusterReadinessConfigV4},
	}

	for _, tc := range smartstoreConfigs {
		tc := tc
		Context("Standalone Deployment (S1)", func() {
			BeforeEach(func() {
				testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, tc.namePrefix)
			})

			AfterEach(func() {
				testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
			})

			It(tc.label+", integration: Can configure multiple indexes through app", func() {
				RunS1MultipleIndexesTest(ctx, deployment, testcaseEnvInst, tc.s1IndexesTimeout)
			})

			It(tc.label+", integration: Can configure indexes which use default volumes through app", func() {
				RunS1DefaultVolumesTest(ctx, deployment, testcaseEnvInst)
			})
		})

		Context("Multisite Indexer Cluster with Search Head Cluster (M4)", func() {
			BeforeEach(func() {
				testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, tc.namePrefix)
			})

			AfterEach(func() {
				testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
			})

			It(tc.label+", m4, integration: Can configure indexes and volumes on Multisite Indexer Cluster through app", func() {
				config := tc.newConfig()
				RunM4MultisiteSmartStoreTest(ctx, deployment, testcaseEnvInst, config)
			})
		})
	}

	Context("Standalone deployment (S1) with App Framework", func() {
		BeforeEach(func() {
			testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")
		})

		AfterEach(func() {
			testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
		})

		It("integration, s1, smartstore: can deploy a Standalone instance with Epehemeral Etc storage", func() {
			storageConfig := enterpriseApi.StorageClassSpec{StorageClassName: "TestStorageEtcEph", StorageCapacity: "1Gi", EphemeralStorage: true}
			RunS1EphemeralStorageTest(ctx, deployment, testcaseEnvInst, storageConfig, true)
		})

		It("integration, s1, smartstore: can deploy a Standalone instance with Epehemeral Var storage", func() {
			storageConfig := enterpriseApi.StorageClassSpec{StorageClassName: "TestStorageVarEph", StorageCapacity: "1Gi", EphemeralStorage: true}
			RunS1EphemeralStorageTest(ctx, deployment, testcaseEnvInst, storageConfig, false)
		})
	})
})
