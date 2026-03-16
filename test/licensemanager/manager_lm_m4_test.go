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
package licensemanager

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = Describe("Licensemanager test", func() {

	var testcaseEnvInst *testenv.TestCaseEnv
	var deployment *testenv.Deployment
	var config *LicenseTestConfig
	ctx := context.TODO()

	BeforeEach(func() {
		var err error
		name := fmt.Sprintf("%s-%s", testenvInstance.GetName(), testenv.RandomDNSName(3))

		testcaseEnvInst, err = testenv.NewDefaultTestCaseEnv(testenvInstance.GetKubeClient(), name)
		Expect(err).To(Succeed(), "Unable to create testcaseenv")

		deployment, err = testcaseEnvInst.NewDeployment(testenv.RandomDNSName(3))
		Expect(err).To(Succeed(), "Unable to create deployment")
		config = NewLicenseManagerConfig()
	})

	AfterEach(func() {
		// When a test spec failed, skip the teardown so we can troubleshoot.
		if types.SpecState(CurrentSpecReport().State) == types.SpecStateFailed {
			testcaseEnvInst.SkipTeardown = true
		}
		if deployment != nil {
			deployment.Teardown()
		}
		if testcaseEnvInst != nil {
			Expect(testcaseEnvInst.Teardown()).ToNot(HaveOccurred())
		}
	})

	Context("Multisite cluster deployment (M4 - Multisite indexer cluster, Search head cluster)  with License Manager", func() {
		It("licensemanager, integration, m4: Splunk Operator can configure License Manager with indexers and search head in M4 SVA", func() {
			RunLMM4Test(ctx, deployment, testcaseEnvInst, config)
		})
	})

})
