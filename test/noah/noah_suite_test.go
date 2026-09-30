// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package noah

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var (
	operatorNamespace = testenv.GetEnvWithDefault("NOAH_TEST_NAMESPACE", "splunk-operator")
	operatorName      = testenv.GetEnvWithDefault("NOAH_TEST_OPERATOR_NAME", "splunk-operator-controller-manager")
	noahDeployment    = testenv.GetEnvWithDefault("NOAH_TEST_NOAH_DEPLOYMENT", "noah")
	clusterName       = testenv.GetEnvWithDefault("NOAH_TEST_C3_NAME", "c3")

	readyTimeout time.Duration

	testenvInstance     *testenv.TestEnv
	testcaseEnvInstance *testenv.TestCaseEnv
	deployment          *testenv.Deployment
	testSuiteName       = "noah-" + testenv.RandomDNSName(5)
)

func TestNoahIntegration(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, _ := GinkgoConfiguration()
	suiteConfig.Timeout = testenv.MediumSuiteTimeout
	RunSpecs(t, "Running "+testSuiteName, suiteConfig)
}

var _ = BeforeSuite(func() {
	var err error
	readyTimeout, err = time.ParseDuration(testenv.GetEnvWithDefault("NOAH_TEST_READY_TIMEOUT", "30m"))
	Expect(err).NotTo(HaveOccurred(), "NOAH_TEST_READY_TIMEOUT must be a Go duration")

	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).To(Succeed(), "failed to initialize the Kubernetes test client")

	testcaseEnvInstance, err = testenv.AttachToExistingEnv(
		testenvInstance.GetKubeClient(),
		operatorNamespace,
		operatorName,
	)
	Expect(err).To(Succeed(), "failed to attach to the existing Noah test environment")

	deployment, err = testcaseEnvInstance.NewDeployment(clusterName, nil)
	Expect(err).To(Succeed(), "failed to create the attached deployment handle")
})

var _ = AfterSuite(func() {
	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).To(Succeed(), "failed to stop the Noah test client")
	}
})

var _ = AfterEach(func() {
	if !CurrentSpecReport().Failed() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), testenv.BestEffortProbeTimeout)
	defer cancel()
	if testcaseEnvInstance != nil {
		dumpC3FailureState(ctx, testcaseEnvInstance.GetKubeClient(), operatorNamespace, clusterName, GinkgoWriter)
	}
})
