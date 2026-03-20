// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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
package licensemaster

import (
	"context"

	. "github.com/onsi/ginkgo/v2"

	"github.com/splunk/splunk-operator/test/licensemanager"
	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Licensemaster test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var config *licensemanager.LicenseTestConfig
	ctx := context.TODO()

	BeforeEach(func() {
		testcaseEnvInst, deployment = testenv.SetupTestCaseEnv(testenvInstance, "master")

		config = licensemanager.NewLicenseMasterConfig()
	})

	AfterEach(func() {
		testenv.TeardownTestCaseEnv(testcaseEnvInst, deployment)
	})

	Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster) with License Master", func() {
		It("licensemaster, integration, m4: Splunk Operator can configure License Master with indexers and search head in M4 SVA", func() {
			licensemanager.RunLMM4Test(ctx, deployment, testcaseEnvInst, config)
		})
	})

})
