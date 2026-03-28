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
package azurem4appfw

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var (
	testenvInstance     *testenv.TestEnv
	testSuiteName       = "m4appfw-" + testenv.RandomDNSName(3)
	appListV1           []string
	appListV2           []string
	AzureDataContainer  = os.Getenv("TEST_CONTAINER")
	AzureContainer      = os.Getenv("INDEXES_CONTAINER")
	AzureStorageAccount = os.Getenv("AZURE_STORAGE_ACCOUNT")
	currDir, _          = os.Getwd()
	downloadDirV1       = filepath.Join(currDir, "m4appfwV1-"+testenv.RandomDNSName(4))
	downloadDirV2       = filepath.Join(currDir, "m4appfwV2-"+testenv.RandomDNSName(4))
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Running "+testSuiteName)
}

var _ = BeforeSuite(func() {
	testenvInstance, appListV1, appListV2 = testenv.SetupAzureAppsSuite(testSuiteName, downloadDirV1, downloadDirV2)
})

var _ = AfterSuite(func() {
	testenv.CleanupLocalAppDownloads(testenvInstance, downloadDirV1, downloadDirV2)
})
