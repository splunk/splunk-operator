package basic

import (
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/test/testenv"
)

func dumpGetPods(ns string) {
	output, _ := exec.Command("kubectl", "get", "pod", "-n", ns).Output()
	for _, line := range strings.Split(string(output), "\n") {
		testenvInstance.Log.Info(line)
	}
}

var _ = Describe("Smoke test", func() {

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
		It("can deploy a standalone instance", func() {

			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			Eventually(func() enterprisev1.ResourcePhase {
				err = deployment.GetInstance(deployment.GetName(), standalone)
				if err != nil {
					return enterprisev1.PhaseError
				}
				testenvInstance.Log.Info("Waiting for standalone instance status to be ready", "instance", standalone.ObjectMeta.Name, "Phase", standalone.Status.Phase)
				dumpGetPods(testenvInstance.GetName())

				return standalone.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(enterprisev1.PhaseReady))

			// In a steady state, we should stay in Ready and not flip-flop around
			Consistently(func() enterprisev1.ResourcePhase {
				_ = deployment.GetInstance(deployment.GetName(), standalone)
				return standalone.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(enterprisev1.PhaseReady))
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("can deploy indexers and search head cluster", func() {

			err := deployment.DeployCluster(deployment.GetName(), 3)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure indexers go to Ready phase
			idc := &enterprisev1.IndexerCluster{}
			Eventually(func() enterprisev1.ResourcePhase {
				err := deployment.GetInstance(deployment.GetName(), idc)
				if err != nil {
					return enterprisev1.PhaseError
				}
				testenvInstance.Log.Info("Waiting for indexer cluster instance status to be ready", "instance", idc.ObjectMeta.Name, "Phase", idc.Status.Phase)

				dumpGetPods(testenvInstance.GetName())

				return idc.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(enterprisev1.PhaseReady))

			// In a steady state, we should stay in Ready and not flip-flop around
			Consistently(func() enterprisev1.ResourcePhase {
				_ = deployment.GetInstance(deployment.GetName(), idc)
				return idc.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(enterprisev1.PhaseReady))

			shc := &enterprisev1.SearchHeadCluster{}
			// Ensure search head cluster go to Ready phase
			Eventually(func() enterprisev1.ResourcePhase {
				err := deployment.GetInstance(deployment.GetName(), shc)
				if err != nil {
					return enterprisev1.PhaseError
				}
				testenvInstance.Log.Info("Waiting for search head cluster instance status to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
				return shc.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(enterprisev1.PhaseReady))

			// In a steady state, we should stay in Ready and not flip-flop around
			Consistently(func() enterprisev1.ResourcePhase {
				_ = deployment.GetInstance(deployment.GetName(), shc)
				return shc.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(enterprisev1.PhaseReady))
		})
	})
})
