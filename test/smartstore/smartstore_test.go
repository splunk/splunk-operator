package smartstore

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Smartstore test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	ctx := context.TODO()
	var deployment *testenv.Deployment

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")

		// Validate test prerequisites early to fail fast
		err := testcaseEnvInst.ValidateTestPrerequisites(ctx, deployment)
		Expect(err).To(Succeed(), "Test prerequisites validation failed")
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
			config := NewSmartStoreTestConfigV3()
			RunM4MultisiteSmartStoreTest(ctx, deployment, testcaseEnvInst, config)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, smartstore: can deploy a Standalone instance with Epehemeral Etc storage", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create spec for Standalone
			   * Prepare and deploy Standalone and wait for the pod to be ready
			   ############ VERIFICATION FOR STANDALONE ###########
			   * verify Standalone comes up with Ephemeral for Etc and pvc for Var volume
			*/

			// Create App framework spec for Standalone
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
					EtcVolumeStorageConfig: enterpriseApi.StorageClassSpec{
						StorageClassName: "TestStorageEtcEph",
						StorageCapacity:  "1Gi",
						EphemeralStorage: true,
					},
				},
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)
		})
	})

	Context("Standalone deployment (S1) with App Framework", func() {
		It("integration, s1, smartstore: can deploy a Standalone instance with Epehemeral Var storage", func() {

			/* Test Steps
			   ################## SETUP ####################
			   * Create spec for Standalone
			   * Prepare and deploy Standalone and wait for the pod to be ready
			   ############ VERIFICATION FOR STANDALONE ###########
			   * verify Standalone comes up with Ephemeral for Var and pvc for Etc volume
			*/

			// Create App framework spec for Standalone
			spec := enterpriseApi.StandaloneSpec{
				CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
					Spec: enterpriseApi.Spec{
						ImagePullPolicy: "Always",
						Image:           testcaseEnvInst.GetSplunkImage(),
					},
					Volumes: []corev1.Volume{},
					VarVolumeStorageConfig: enterpriseApi.StorageClassSpec{
						StorageClassName: "TestStorageVarEph",
						StorageCapacity:  "1Gi",
						EphemeralStorage: true,
					},
				},
			}

			// Deploy Standalone
			testcaseEnvInst.Log.Info("Deploy Standalone")
			standalone, err := deployment.DeployStandaloneWithGivenSpec(ctx, deployment.GetName(), spec)
			Expect(err).To(Succeed(), "Unable to deploy Standalone instance with App framework")

			// Wait for Standalone to be in READY status
			testcaseEnvInst.VerifyStandaloneReady(ctx, deployment, deployment.GetName(), standalone)

		})
	})
})
