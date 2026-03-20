package smartstore

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var (
	testenvInstance *testenv.TestEnv
	testSuiteName   = "smartstore-" + testenv.RandomDNSName(3)
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {

	RegisterFailHandler(Fail)

	RunSpecs(t, "Running "+testSuiteName)
}

var _ = BeforeSuite(func() {
	var err error
	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
	}
})
