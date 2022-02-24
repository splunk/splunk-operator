package smartstore

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

const (
	// PollInterval specifies the polling interval
	PollInterval = 5 * time.Second

	// ConsistentPollInterval is the interval to use to consistently check a state is stable
	ConsistentPollInterval = 200 * time.Millisecond
	ConsistentDuration     = 2000 * time.Millisecond
)

var (
	testSuiteName = "smartstore-" + testenv.RandomDNSName(3)
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {

	RegisterFailHandler(Fail)

	junitReporter := reporters.NewJUnitReporter(testSuiteName + "_junit.xml")
	RunSpecsWithDefaultAndCustomReporters(t, "Running "+testSuiteName, []Reporter{junitReporter})
}

var _ = BeforeSuite(func() {

})

var _ = AfterSuite(func() {

})
