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
package m4appfw

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/joho/godotenv"
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
	testenvInstance       *testenv.TestEnv
	testSuiteName         = "m4appfw-" + testenv.RandomDNSName(3)
	appListV1             []string
	appListV2             []string
	testDataBucket        = os.Getenv("TEST_BUCKET")
	testBucket            = os.Getenv("TEST_INDEXES_S3_BUCKET")
	appDirV1              = testenv.AppLocationV1
	appDirV2              = testenv.AppLocationV2
	pvTestApps            = testenv.PVTestAppsLocation
	currDir, _            = os.Getwd()
	downloadDirV1         = filepath.Join(currDir, "m4appfwV1-"+testenv.RandomDNSName(4))
	downloadDirV2         = filepath.Join(currDir, "m4appfwV2-"+testenv.RandomDNSName(4))
	downloadDirPVTestApps = filepath.Join(currDir, "m4appfwPVTestApps-"+testenv.RandomDNSName(4))
	backend               testenv.CloudStorageBackend
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {

	if err := loadEnvFile(); err != nil {
		t.Fatalf("Error loading .env file: %s", err.Error())
	}
	RegisterFailHandler(Fail)

	sc, _ := GinkgoConfiguration()
	sc.Timeout = 240 * time.Minute

	RunSpecs(t, "Running "+testSuiteName, sc)
}

// loadEnvFile traverses up the directory tree to find a .env file
func loadEnvFile() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	for {
		envFile := filepath.Join(dir, ".env")
		if _, err := os.Stat(envFile); err == nil {
			return godotenv.Load(envFile)
		}

		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			return nil
		}
		dir = parentDir
	}
}

var _ = BeforeSuite(func() {
	ctx := context.TODO()
	var err error
	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).ToNot(HaveOccurred())

	backend = testenv.NewCloudStorageBackend(testBucket, testDataBucket)

	appListV1 = testenv.BasicApps
	appFileList := testenv.GetAppFileList(appListV1)

	err = backend.DownloadFiles(ctx, appDirV1, downloadDirV1, appFileList)
	Expect(err).To(Succeed(), "Unable to download V1 app files")

	appListV2 = append(appListV1, testenv.NewAppsAddedBetweenPolls...)
	appFileList = testenv.GetAppFileList(appListV2)

	err = backend.DownloadFiles(ctx, appDirV2, downloadDirV2, appFileList)
	Expect(err).To(Succeed(), "Unable to download V2 app files")
})

var _ = AfterSuite(func() {
	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
	}

	// Delete locally downloaded app files
	err := os.RemoveAll(downloadDirV1)
	Expect(err).To(Succeed(), "Unable to delete locally downloaded V1 app files")
	err = os.RemoveAll(downloadDirV2)
	Expect(err).To(Succeed(), "Unable to delete locally downloaded V2 app files")
})
