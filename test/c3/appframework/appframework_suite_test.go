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
package c3appfw

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"

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
	testenvInstance  *testenv.TestEnv
	testSuiteName    = "c3appfw-" + testenv.RandomDNSName(3)
	appListV1        []string
	appListV2        []string
	testDataS3Bucket = os.Getenv("TEST_BUCKET")
	testS3Bucket     = os.Getenv("TEST_INDEXES_S3_BUCKET")
	s3AppDirV1       = testenv.AppLocationV1
	s3AppDirV2       = testenv.AppLocationV2
	currDir, _       = os.Getwd()
	downloadDirV1    = filepath.Join(currDir, "c3appfwV1-"+testenv.RandomDNSName(4))
	downloadDirV2    = filepath.Join(currDir, "c3appfwV2-"+testenv.RandomDNSName(4))
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {

	RegisterFailHandler(Fail)

	junitReporter := reporters.NewJUnitReporter(testSuiteName + "_junit.xml")
	RunSpecsWithDefaultAndCustomReporters(t, "Running "+testSuiteName, []Reporter{junitReporter})
}

var _ = BeforeSuite(func() {
	var err error
	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).ToNot(HaveOccurred())

	// Create a list of apps to upload to S3
	appListV1 = testenv.BasicApps
	appFileList := testenv.GetAppFileList(appListV1)

	// Download V1 Apps from S3
	err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
	Expect(err).To(Succeed(), "Unable to download V1 app files")

	// Create a list of apps to upload to S3 after poll period
	appListV2 = append(appListV1, testenv.NewAppsAddedBetweenPolls...)
	appFileList = testenv.GetAppFileList(appListV2)

	// Download V2 Apps from S3
	err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV2, downloadDirV2, appFileList)
	Expect(err).To(Succeed(), "Unable to download V2 app files")

})

var _ = AfterSuite(func() {
	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
	}

	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
	}

	// Delete locally downloaded app files
	err := os.RemoveAll(downloadDirV1)
	Expect(err).To(Succeed(), "Unable to delete locally downloaded V1 app files")
	err = os.RemoveAll(downloadDirV2)
	Expect(err).To(Succeed(), "Unable to delete locally downloaded V2 app files")
})
