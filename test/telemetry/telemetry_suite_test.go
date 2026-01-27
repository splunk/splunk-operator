// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

package telemetrytest

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

const (
	PollInterval           = 5 * time.Second
	ConsistentPollInterval = 200 * time.Millisecond
	ConsistentDuration     = 2000 * time.Millisecond
)

var (
	testenvInstance *testenv.TestEnv
	testSuiteName   = "telemetry-" + testenv.RandomDNSName(3)
)

func TestTelemetry(t *testing.T) {
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
