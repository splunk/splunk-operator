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
package s1appfw

import (
	"os"

	"testing"
	"time"

	. "github.com/onsi/ginkgo"
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
	testSuiteName    string
	testDataS3Bucket = os.Getenv("TEST_BUCKET")
	testS3Bucket     = os.Getenv("TEST_INDEXES_S3_BUCKET")
	s3AppDirV1       = testenv.AppLocationV1
	s3AppDirV2       = testenv.AppLocationV2
	currDir, _       = os.Getwd()
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {

	RegisterFailHandler(Fail)

}

var _ = BeforeSuite(func() {
	testSuiteName    = "s1appfw-" + testenv.RandomDNSName(3)
})

var _ = AfterSuite(func() {

})
