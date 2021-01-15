// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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
package example

import (
	"math/rand"
	"time"

	. "github.com/onsi/ginkgo"

	"github.com/splunk/splunk-operator/test/testenv"
)

var _ = XDescribe("Example2", func() {

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
})
