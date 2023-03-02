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
package example

import (
	"math/rand"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var (
	testenvInstance *testenv.TestEnv
	testSuiteName   = "example-" + testenv.RandomDNSName(3)
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestExampleSuite(t *testing.T) {

	RegisterFailHandler(Fail)

	RunSpecs(t, "Running "+testSuiteName)
}

var _ = BeforeSuite(func() {
	var err error
	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
})
