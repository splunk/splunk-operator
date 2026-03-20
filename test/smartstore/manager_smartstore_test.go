package smartstore

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Smartstore test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	ctx := context.TODO()
	var deployment *testenv.Deployment

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "")
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Standalone Deployment (S1)", func() {
		It("managersmartstore, integration: Can configure multiple indexes through app", func() {
			RunS1MultipleIndexesTest(ctx, deployment, testcaseEnvInst, 5*time.Minute)
		})
	})

	Context("Standalone Deployment (S1)", func() {
		It("managersmartstore, integration: Can configure indexes which use default volumes through app", func() {
			RunS1DefaultVolumesTest(ctx, deployment, testcaseEnvInst)
		})
	})

	Context("Multisite Indexer Cluster with Search Head Cluster (M4)", func() {
		It("managersmartstore, smoke: Can configure indexes and volumes on Multisite Indexer Cluster through app", func() {
			config := NewSmartStoreTestConfigV4()
			RunM4MultisiteSmartStoreTest(ctx, deployment, testcaseEnvInst, config)
		})
	})
})
