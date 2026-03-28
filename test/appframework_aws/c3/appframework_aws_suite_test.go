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
package c3appfw

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/splunk/splunk-operator/test/testenv"
)

var (
	testenvInstance       *testenv.TestEnv
	testSuiteName         = "c3appfw-" + testenv.RandomDNSName(3)
	appListV1             []string
	appListV2             []string
	testDataS3Bucket      = os.Getenv("TEST_BUCKET")
	testS3Bucket          = os.Getenv("TEST_INDEXES_S3_BUCKET")
	currDir, _            = os.Getwd()
	downloadDirV1         = filepath.Join(currDir, "c3appfwV1-"+testenv.RandomDNSName(4))
	downloadDirV2         = filepath.Join(currDir, "c3appfwV2-"+testenv.RandomDNSName(4))
	downloadDirPVTestApps = filepath.Join(currDir, "c3appfwPVTestApps-"+testenv.RandomDNSName(4))
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {
	RegisterFailHandler(Fail)

	Expect(testenv.LoadEnvFile()).ToNot(HaveOccurred(), "Error loading .env file")

	sc, _ := GinkgoConfiguration()
	sc.Timeout = 240 * time.Minute

	RunSpecs(t, "Running "+testSuiteName, sc)
}

var _ = BeforeSuite(func() {
	testenvInstance, appListV1, appListV2 = testenv.SetupS3AppsSuite(testSuiteName, testDataS3Bucket, testenv.AppLocationV1, downloadDirV1, testenv.AppLocationV2, downloadDirV2)
})

var _ = AfterSuite(func() {
	testenv.CleanupLocalAppDownloads(testenvInstance, downloadDirV1, downloadDirV2)
})
