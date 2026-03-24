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

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Standalone Deployment (S1)", func() {
		It("mastersmartstore, integration: Can configure multiple indexes through app", func() {
			RunS1MultipleIndexesTest(ctx, deployment, testcaseEnvInst, 2*time.Minute)
		})
	})

	Context("Standalone Deployment (S1)", func() {
		It("mastersmartstore, integration: Can configure indexes which use default volumes through app", func() {
			RunS1DefaultVolumesTest(ctx, deployment, testcaseEnvInst)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (M4)", func() {
		It("mastersmartstore, m4, integration: Can configure indexes and volumes on Multisite Indexer Cluster through app", func() {
			config := testenv.NewClusterReadinessConfigV3()
			RunM4MultisiteSmartStoreTest(ctx, deployment, testcaseEnvInst, config)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, smartstore: can deploy a Standalone instance with Epehemeral Etc storage", func() {
			storageConfig := enterpriseApi.StorageClassSpec{StorageClassName: "TestStorageEtcEph", StorageCapacity: "1Gi", EphemeralStorage: true}
			RunS1EphemeralStorageTest(ctx, deployment, testcaseEnvInst, storageConfig, true)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, smartstore: can deploy a Standalone instance with Epehemeral Var storage", func() {
			storageConfig := enterpriseApi.StorageClassSpec{StorageClassName: "TestStorageVarEph", StorageCapacity: "1Gi", EphemeralStorage: true}
			RunS1EphemeralStorageTest(ctx, deployment, testcaseEnvInst, storageConfig, false)
		})
	})
})
