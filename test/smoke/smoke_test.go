package basic

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/test/testenv"
)

func dumpGetPods(ns string) {
	output, _ := exec.Command("kubectl", "get", "pod", "-n", ns).Output()
	for _, line := range strings.Split(string(output), "\n") {
		testenvInstance.Log.Info(line)
	}
}

// Smoke test
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

			Eventually(func() splcommon.Phase {
				err = deployment.GetInstance(deployment.GetName(), standalone)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for standalone instance status to be ready", "instance", standalone.ObjectMeta.Name, "Phase", standalone.Status.Phase)
				dumpGetPods(testenvInstance.GetName())

				return standalone.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

			// In a steady state, we should stay in Ready and not flip-flop around
			Consistently(func() splcommon.Phase {
				_ = deployment.GetInstance(deployment.GetName(), standalone)
				return standalone.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))
		})
	})

	Context("Clustered deployment (C3 - clustered indexer, search head cluster)", func() {
		It("can deploy indexers and search head cluster", func() {

			idxCount := 3
			err := deployment.DeploySingleSiteCluster(deployment.GetName(), idxCount)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			cm := &enterprisev1.ClusterMaster{}
			Eventually(func() splcommon.Phase {
				err := deployment.GetInstance(deployment.GetName(), cm)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for cluster-master instance status to be ready", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
				dumpGetPods(testenvInstance.GetName())
				// Test ClusterMaster Phase to see if its ready
				return cm.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

			// In a steady state, cluster-master should stay in Ready and not flip-flop around
			Consistently(func() splcommon.Phase {
				_ = deployment.GetInstance(deployment.GetName(), cm)
				return cm.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))

			// Ensure indexers go to Ready phase
			idc := &enterprisev1.IndexerCluster{}
			instanceName := fmt.Sprintf("%s-idxc", deployment.GetName())
			Eventually(func() splcommon.Phase {
				err := deployment.GetInstance(instanceName, idc)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for indexer instance's status to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
				dumpGetPods(testenvInstance.GetName())
				return idc.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

			// In a steady state, we should stay in Ready and not flip-flop around
			Consistently(func() splcommon.Phase {
				_ = deployment.GetInstance(instanceName, idc)
				return idc.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))

			// Ensure search head cluster go to Ready phase
			shc := &enterprisev1.SearchHeadCluster{}
			instanceName = fmt.Sprintf("%s-shc", deployment.GetName())
			Eventually(func() splcommon.Phase {
				err := deployment.GetInstance(instanceName, shc)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for search head cluster instance status to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
				dumpGetPods(testenvInstance.GetName())
				return shc.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

			// In a steady state, we should stay in Ready and not flip-flop around
			Consistently(func() splcommon.Phase {
				_ = deployment.GetInstance(deployment.GetName(), shc)
				return shc.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))
		})
	})

	Context("Multisite cluster deployment (M13 - Multisite indexer cluster, Search head cluster)", func() {
		It("can deploy indexers and search head cluster", func() {

			siteCount := 3
			err := deployment.DeployMultisiteClusterWithSearchHead(deployment.GetName(), 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			cm := &enterprisev1.ClusterMaster{}
			Eventually(func() splcommon.Phase {
				err := deployment.GetInstance(deployment.GetName(), cm)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for cluster-master instance status to be ready", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
				dumpGetPods(testenvInstance.GetName())
				// Test ClusterMaster Phase to see if its ready
				return cm.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

			// In a steady state, cluster-master should stay in Ready and not flip-flop around
			Consistently(func() splcommon.Phase {
				_ = deployment.GetInstance(deployment.GetName(), cm)
				return cm.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))

			// Ensure the indexers of all sites go to Ready phase
			siteIndexerMap := map[string][]string{}
			for site := 1; site <= siteCount; site++ {
				siteName := fmt.Sprintf("site%d", site)
				instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
				siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-indexer-0", instanceName)}
				// Ensure indexers go to Ready phase
				idc := &enterprisev1.IndexerCluster{}
				Eventually(func() splcommon.Phase {
					err := deployment.GetInstance(instanceName, idc)
					if err != nil {
						return splcommon.PhaseError
					}
					testenvInstance.Log.Info("Waiting for indexer site instance status to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
					dumpGetPods(testenvInstance.GetName())
					return idc.Status.Phase
				}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

				// In a steady state, we should stay in Ready and not flip-flop around
				Consistently(func() splcommon.Phase {
					_ = deployment.GetInstance(instanceName, idc)
					return idc.Status.Phase
				}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))
			}

			// Ensure cluster configured as multisite
			Eventually(func() map[string][]string {
				podName := fmt.Sprintf("splunk-%s-cluster-master-0", deployment.GetName())
				stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/master/sites?output_mode=json"
				command := []string{"/bin/sh"}
				stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
				if err != nil {
					testenvInstance.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
					return map[string][]string{}
				}
				testenvInstance.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
				siteIndexerResponse := ClusterMasterSitesResponse{}
				json.Unmarshal([]byte(stdout), &siteIndexerResponse)
				siteIndexerStatus := map[string][]string{}
				for _, site := range siteIndexerResponse.Entries {
					siteIndexerStatus[site.Name] = []string{}
					for _, peer := range site.Content.Peers {
						siteIndexerStatus[site.Name] = append(siteIndexerStatus[site.Name], peer.ServerName)
					}
				}
				return siteIndexerStatus
			}, deployment.GetTimeout(), PollInterval).Should(Equal(siteIndexerMap))

			shc := &enterprisev1.SearchHeadCluster{}
			instanceName := fmt.Sprintf("%s-shc", deployment.GetName())
			// Ensure search head cluster go to Ready phase
			Eventually(func() splcommon.Phase {
				err := deployment.GetInstance(instanceName, shc)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for search head cluster instance status to be ready", "instance", shc.ObjectMeta.Name, "Phase", shc.Status.Phase)
				return shc.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

			// In a steady state, we should stay in Ready and not flip-flop around
			Consistently(func() splcommon.Phase {
				_ = deployment.GetInstance(deployment.GetName(), shc)
				return shc.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))
		})
	})

	Context("Multisite cluster deployment (M1 - multisite indexer cluster)", func() {
		It("can deploy multisite indexers cluster", func() {

			siteCount := 3
			err := deployment.DeployMultisiteCluster(deployment.GetName(), 1, siteCount)
			Expect(err).To(Succeed(), "Unable to deploy cluster")

			// Ensure that the cluster-master goes to Ready phase
			cm := &enterprisev1.ClusterMaster{}
			Eventually(func() splcommon.Phase {
				err := deployment.GetInstance(deployment.GetName(), cm)
				if err != nil {
					return splcommon.PhaseError
				}
				testenvInstance.Log.Info("Waiting for cluster-master instance status to be ready", "instance", cm.ObjectMeta.Name, "Phase", cm.Status.Phase)
				dumpGetPods(testenvInstance.GetName())
				// Test ClusterMaster Phase to see if its ready
				return cm.Status.Phase
			}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

			// In a steady state, cluster-master should stay in Ready and not flip-flop around
			Consistently(func() splcommon.Phase {
				_ = deployment.GetInstance(deployment.GetName(), cm)
				return cm.Status.Phase
			}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))

			// Ensure the indexers of all sites go to Ready phase
			siteIndexerMap := map[string][]string{}
			for site := 1; site <= siteCount; site++ {
				siteName := fmt.Sprintf("site%d", site)
				instanceName := fmt.Sprintf("%s-%s", deployment.GetName(), siteName)
				siteIndexerMap[siteName] = []string{fmt.Sprintf("splunk-%s-indexer-0", instanceName)}
				// Ensure indexers go to Ready phase
				idc := &enterprisev1.IndexerCluster{}
				Eventually(func() splcommon.Phase {
					err := deployment.GetInstance(instanceName, idc)
					if err != nil {
						return splcommon.PhaseError
					}
					testenvInstance.Log.Info("Waiting for indexer site instance status to be ready", "instance", instanceName, "Phase", idc.Status.Phase)
					dumpGetPods(testenvInstance.GetName())
					return idc.Status.Phase
				}, deployment.GetTimeout(), PollInterval).Should(Equal(splcommon.PhaseReady))

				// In a steady state, we should stay in Ready and not flip-flop around
				Consistently(func() splcommon.Phase {
					_ = deployment.GetInstance(instanceName, idc)
					return idc.Status.Phase
				}, ConsistentDuration, ConsistentPollInterval).Should(Equal(splcommon.PhaseReady))
			}

			// Ensure cluster configured as multisite
			Eventually(func() map[string][]string {
				podName := fmt.Sprintf("splunk-%s-cluster-master-0", deployment.GetName())
				stdin := "curl -ks -u admin:$(cat /mnt/splunk-secrets/password) https://localhost:8089/services/cluster/master/sites?output_mode=json"
				command := []string{"/bin/sh"}
				stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
				if err != nil {
					testenvInstance.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
					return map[string][]string{}
				}
				testenvInstance.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)
				siteIndexerResponse := ClusterMasterSitesResponse{}
				json.Unmarshal([]byte(stdout), &siteIndexerResponse)
				siteIndexerStatus := map[string][]string{}
				for _, site := range siteIndexerResponse.Entries {
					siteIndexerStatus[site.Name] = []string{}
					for _, peer := range site.Content.Peers {
						siteIndexerStatus[site.Name] = append(siteIndexerStatus[site.Name], peer.ServerName)
					}
				}
				return siteIndexerStatus
			}, deployment.GetTimeout(), PollInterval).Should(Equal(siteIndexerMap))

		})
	})
})
