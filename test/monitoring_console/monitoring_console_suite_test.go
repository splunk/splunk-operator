// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package monitoringconsoletest

import (
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
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
	testenvInstance *testenv.TestEnv
	testSuiteName   = "mc-" + testenv.RandomDNSName(3)
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {

	RegisterFailHandler(Fail)

	sc, _ := GinkgoConfiguration()
	sc.Timeout = 240 * time.Minute

	RunSpecs(t, "Running "+testSuiteName, sc)
}

var _ = BeforeSuite(func() {
	var err error

	// Override script locations to use absolute paths for integration tests
	// These are relative to test/monitoring_console/, so we go up 2 levels to project root
	enterprise.GetReadinessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../tools/k8_probes/readinessProbe.sh")
		return fileLocation
	}
	enterprise.GetLivenessScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../tools/k8_probes/livenessProbe.sh")
		return fileLocation
	}
	enterprise.GetStartupScriptLocation = func() string {
		fileLocation, _ := filepath.Abs("../../tools/k8_probes/startupProbe.sh")
		return fileLocation
	}

	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
	}
})
