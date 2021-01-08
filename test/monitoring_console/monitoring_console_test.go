package monitoringconsoletest

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1beta1"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Monitoring Console test", func() {

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
		if !testenvInstance.SkipTeardown {
			testenv.DeleteMCPod(testenvInstance.GetName())
		}
		if deployment != nil {
			deployment.Teardown()
		}
	})

	Context("Standalone deployment (S1)", func() {
		It("monitoring_console: can deploy a MC with standalone instance and update MC with new standalone deployment", func() {

			standaloneOneName := deployment.GetName()
			standaloneOne, err := deployment.DeployStandalone(standaloneOneName)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for standalone to be in READY Status
			testenv.StandaloneReady(deployment, deployment.GetName(), standaloneOne, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all standalone instances in namespace
			peerList := testenv.GetConfiguredPeers(testenvInstance.GetName())
			testenvInstance.Log.Info("Peer List", "instance", peerList)

			// Only 1 peer expected in MC peer list
			Expect(len(peerList)).To(Equal(1))

			podName := fmt.Sprintf(testenv.StandalonePod, standaloneOneName, 0)
			testenvInstance.Log.Info("Check standalone instance in MC Peer list", "Standalone Pod", podName, "Peer in peer list", peerList[0])
			Expect(strings.Contains(peerList[0], podName)).To(Equal(true))

			// Add another standalone instance in namespace
			testenvInstance.Log.Info("Adding second standalone deployment to namespace")
			standaloneTwoName := deployment.GetName() + "-two"
			standaloneTwo, err := deployment.DeployStandalone(standaloneTwoName)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for standalone two to be in READY status
			testenv.StandaloneReady(deployment, standaloneTwoName, standaloneTwo, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all standalone instances in namespace
			peerList = testenv.GetConfiguredPeers(testenvInstance.GetName())
			configuredStandaloneOne := false
			confguredStandaloneTwo := false
			testenvInstance.Log.Info("Peer List", "instance", peerList)

			// Only 2 peers expected in MC peer list
			Expect(len(peerList)).To(Equal(2))

			podNameOne := fmt.Sprintf(testenv.StandalonePod, standaloneOneName, 0)
			podNameTwo := fmt.Sprintf(testenv.StandalonePod, standaloneTwoName, 0)
			for _, peer := range peerList {
				if strings.Contains(peer, podNameOne) {
					testenvInstance.Log.Info("Check standalone instance in MC Peer list", "Standalone Pod", podNameOne, "Peer in peer list", peer)
					configuredStandaloneOne = true
				}
				if strings.Contains(peer, podNameTwo) {
					confguredStandaloneTwo = true
					testenvInstance.Log.Info("Check standalone instance in MC Peer list", "Standalone Pod", podNameTwo, "Peer in peer list", peer)
				}
			}
			Expect(configuredStandaloneOne && confguredStandaloneTwo).To(Equal(true))
		})
	})

	Context("Standalone deployment with Scale up", func() {
		It("monitoring_console: can deploy a MC with standalone instance and update MC when standalone is scaled up", func() {

			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all standalone instances in namespace
			peerList := testenv.GetConfiguredPeers(testenvInstance.GetName())
			testenvInstance.Log.Info("Peer List", "instance", peerList)

			// Only 1 peer expected in MC peer list
			Expect(len(peerList)).To(Equal(1))

			// Check spluk standlone pods are configured in MC peer list
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenvInstance.Log.Info("Check standalone instance in MC Peer list", "Standalone Pod", podName, "Peer in peer list", peerList[0])
			Expect(testenv.CheckStandalonePodOnMC(testenvInstance.GetName(), podName)).To(Equal(true))

			// Scale Standalone instance
			testenvInstance.Log.Info("Scaling standalone cluster")
			scaledReplicaCount := 2
			replicas := fmt.Sprintf("--replicas=%d", scaledReplicaCount)
			_, err = exec.Command("kubectl", "scale", "standalone", "-n", testenvInstance.GetName(), deployment.GetName(), replicas).Output()
			Expect(err).To(Succeed(), "Failed to execute scale up command")

			// Ensure standalone is scaling up
			Eventually(func() splcommon.Phase {
				err := deployment.GetInstance(deployment.GetName(), standalone)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for standalone status to be Scaling Up", "instance", standalone.ObjectMeta.Name, "Phase", standalone.Status.Phase)
				testenv.DumpGetPods(testenvInstance.GetName())
				return standalone.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseScalingUp))

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Only 2 peer expected in MC peer list
			peerList = testenv.GetConfiguredPeers(testenvInstance.GetName())
			testenvInstance.Log.Info("Peers in configuredPeer List", "count", len(peerList))
			Expect(len(peerList)).To(Equal(2))

			// Check standalone pods are configured  in MC Peer List
			found := make(map[string]bool)
			for i := 0; i < 2; i++ {
				podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), i)
				found[podName] = testenv.CheckStandalonePodOnMC(testenvInstance.GetName(), podName)
			}
			allStandaloneConfigured := true
			for _, key := range found {
				if !key {
					allStandaloneConfigured = false
					break
				}
			}
			Expect(allStandaloneConfigured).To(Equal(true))
		})
	})

	Context("SearchHeadCluster deployment with Scale Up", func() {
		It("monitoring_console: MC can configure SHC instances after scale up in a namespace", func() {

			_, err := deployment.DeploySearchHeadCluster(deployment.GetName(), "", "", "")
			Expect(err).To(Succeed(), "Unable to deploy search head cluster")

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all search head instances in namespace
			defaultSHReplicas := 3
			found := testenv.GetSearchHeadPeersOnMC(testenvInstance.GetName(), deployment.GetName(), defaultSHReplicas)
			allSearchHeadsConfigured := true
			for _, key := range found {
				if !key {
					allSearchHeadsConfigured = false
					break
				}
			}
			Expect(allSearchHeadsConfigured).To(Equal(true))

			// Scale Search Head Cluster
			scaledSHReplicas := defaultSHReplicas + 1
			testenvInstance.Log.Info("Scaling search head cluster")
			replicas := fmt.Sprintf("--replicas=%d", scaledSHReplicas)
			_, err = exec.Command("kubectl", "scale", "shc", "-n", testenvInstance.GetName(), deployment.GetName(), replicas).Output()
			Expect(err).To(Succeed(), "Failed to scale search head cluster")

			// Ensure search head cluster go to ScalingUp phase
			shc := &enterprisev1.SearchHeadCluster{}

			Eventually(func() splcommon.Phase {
				err := deployment.GetInstance(deployment.GetName(), shc)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for search head cluster STATUS to be Scaling Up", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
				testenv.DumpGetPods(testenvInstance.GetName())
				return shc.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseScalingUp))

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all search head instances in namespace
			found = testenv.GetSearchHeadPeersOnMC(testenvInstance.GetName(), deployment.GetName(), scaledSHReplicas)

			allSearchHeadsConfigured = true
			for _, key := range found {
				if !key {
					allSearchHeadsConfigured = false
					break
				}
			}
			Expect(allSearchHeadsConfigured).To(Equal(true))
		})
	})

	Context("SearchHeadCluster and Standalone", func() {
		It("monitoring_console: MC can configure SHC and Standalone instances in a namespace", func() {

			_, err := deployment.DeploySearchHeadCluster(deployment.GetName(), "", "", "")
			Expect(err).To(Succeed(), "Unable to deploy search head cluster")

			// Ensure search head cluster go to Ready phase
			testenv.SearchHeadClusterReady(deployment, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Check Monitoring console is configured with all search head instances in namespace
			defaultSHReplicas := 3
			found := testenv.GetSearchHeadPeersOnMC(testenvInstance.GetName(), deployment.GetName(), defaultSHReplicas)
			allSearchHeadsConfigured := true
			for _, key := range found {
				if !key {
					allSearchHeadsConfigured = false
					break
				}
			}
			Expect(allSearchHeadsConfigured).To(Equal(true))

			// Deploy Standalone
			standalone, err := deployment.DeployStandalone(deployment.GetName())
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Wait for Standalone to be in READY status
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Wait for Monitoring Console Pod to be in READY status
			testenv.MCPodReady(testenvInstance.GetName(), deployment)

			// Get Search Head Pods configured on Monitoring Console
			found = testenv.GetSearchHeadPeersOnMC(testenvInstance.GetName(), deployment.GetName(), 3)

			// Check Standalone configured on Monitoring Console
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			found[podName] = testenv.CheckStandalonePodOnMC(testenvInstance.GetName(), podName)

			// Verify all instances are configured on Monitoring Console
			allInstancesConfigured := true
			for _, key := range found {
				if !key {
					allInstancesConfigured = false
					break
				}
			}
			Expect(allInstancesConfigured).To(Equal(true))
		})
	})
})
