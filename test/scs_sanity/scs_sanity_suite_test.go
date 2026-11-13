// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package scssanity

import (
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

// The suite attaches to the already-running SOK install on an SCS cluster
// (see AttachToExistingEnv). Health checks read its state; the optional canary
// creates and cleans up its own Standalone in a pre-existing namespace.
var (
	operatorNamespace = testenv.GetEnvWithDefault("SCS_OPERATOR_NAMESPACE", "splunk-operator")
	operatorName      = testenv.GetEnvWithDefault("SCS_OPERATOR_NAME", "splunk-operator-controller-manager")
	targetOperatorImg = testenv.GetEnvWithDefault("TARGET_OPERATOR_IMAGE", "")

	testenvInstance     *testenv.TestEnv
	testcaseEnvInstance *testenv.TestCaseEnv
	testSuiteName       = "scs-sanity-" + testenv.RandomDNSName(3)
)

// TestSCSSanity is the main entry point. Only specs labeled tier:scs-sanity are ever
// expected to run here; the label filter is enforced by the caller (ginkgo
// --label-filter='tier:scs-sanity'), not by this file.
func TestSCSSanity(t *testing.T) {
	RegisterFailHandler(Fail)

	sc, _ := GinkgoConfiguration()
	// The gate sets SCS_SANITY_SUITE_TIMEOUT below its hard timeout so cleanup still runs.
	suiteTimeout, err := parseSuiteTimeout(os.Getenv("SCS_SANITY_SUITE_TIMEOUT"), testenv.MediumSuiteTimeout)
	if err != nil {
		t.Fatal(err)
	}
	sc.Timeout = suiteTimeout

	RunSpecs(t, "Running "+testSuiteName, sc)
}

var _ = BeforeSuite(func() {
	var err error
	// NewDefaultTestEnv is used only for its side effects: CRD scheme registration and a
	// running controller-runtime manager/cache, which yield a working kubeClient. It performs
	// no namespace/operator provisioning of its own (see test/testenv/testenv.go).
	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).To(Succeed(), "Failed to initialize kube client for scs-sanity suite")

	testcaseEnvInstance, err = testenv.AttachToExistingEnv(testenvInstance.GetKubeClient(), testenvInstance.GetAPIReader(), operatorNamespace, operatorName)
	Expect(err).To(Succeed(), "Failed to attach to existing SCS environment")
})

var _ = AfterSuite(func() {
	// AttachToExistingEnv sets SkipTeardown/registers no cleanup funcs, so Teardown is a no-op
	// for the attached TestCaseEnv; only the underlying TestEnv (manager/cache) needs stopping.
	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).To(Succeed(), "Failed to teardown scs-sanity suite")
	}
})
