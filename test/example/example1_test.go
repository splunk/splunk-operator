package example

import (
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = XDescribe("Example1", func() {

	var deployment *testenv.Deployment

	// This is invoke for each "It" spec below
	BeforeEach(func() {
		// Create a deployment for this test
		deployment, _ = testenvInstance.NewDeployment(testenv.RandomDNSName(5))
	})

	AfterEach(func() {
		deployment.Teardown()
	})

	// "It" spec
	It("deploys successfully", func() {
		// Add your test spec!!
		// eg deployment.DeployStandalone()
		time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
		testenvInstance.Log.Info("Running test spec", "name", deployment.GetName())
	})

	// "It" spec
	It("can update volumes", func() {
		// Add your test spec!!
		time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
		testenvInstance.Log.Info("Running test spec", "name", deployment.GetName())
	})

	// "It" spec
	It("can update service ports", func() {
		// Add your test spec!!
		time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
		testenvInstance.Log.Info("Running test spec", "name", deployment.GetName())
	})
})
