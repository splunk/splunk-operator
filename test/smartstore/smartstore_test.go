package smartstore

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

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

	Context("Confiugre indexes on standlaone deployment using CR Spec", func() {
		It("smartstore: Can configure indexes through app", func() {
			volumeName := "test-volume-" + testenv.RandomDNSName(3)
			indexName := "test-index-" + testenv.RandomDNSName(3)
			testenvInstance.Log.Info("index secret name ", "secret name ", testenvInstance.GetIndexSecretName())
			standalone, err := deployment.DeployStandaloneWithIndexes(deployment.GetName(), testenvInstance.GetIndexSecretName(), deployment.GetName(), volumeName, indexName)
			Expect(err).To(Succeed(), "Unable to deploy standalone instance ")

			// Verify standalone goes to ready state
			testenv.StandaloneReady(deployment, deployment.GetName(), standalone, testenvInstance)

			// Check index on pod
			podName := fmt.Sprintf(testenv.StandalonePod, deployment.GetName(), 0)
			testenv.VerifyIndexFoundOnPod(deployment, podName, indexName)

			// Ingest data to the index
			logFile := "/opt/splunk/var/log/splunk/splunkd.log"
			testenv.IngestFileViaMonitor(logFile, indexName, podName, deployment)

			// Roll Hot Buckets on the test index by restarting splunk
			testenv.RollHotToWarm(deployment, podName, indexName)

			// Check for index on S3
			testenv.VerifyIndexExistsOnS3(deployment, podName, indexName)
		})
	})
})
