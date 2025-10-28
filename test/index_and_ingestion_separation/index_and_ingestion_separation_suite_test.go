// Copyright (c) 2018-2025 Splunk Inc. All rights reserved.

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
package indingsep

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
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
	testenvInstance *testenv.TestEnv
	testSuiteName   = "indingsep-" + testenv.RandomDNSName(3)

	bus = enterpriseApi.BusConfigurationSpec{
		Type: "sqs_smartbus",
		SQS: enterpriseApi.SQSSpec{
			QueueName:                 "test-queue",
			AuthRegion:                "us-west-2",
			Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
			LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
			LargeMessageStorePath:     "s3://test-bucket/smartbus-test",
			DeadLetterQueueName:       "test-dead-letter-queue",
		},
	}
	serviceAccountName = "index-ingest-sa"

	inputs = []string{
		"[remote_queue:test-queue]",
		"remote_queue.type = sqs_smartbus",
		"remote_queue.sqs_smartbus.auth_region = us-west-2",
		"remote_queue.sqs_smartbus.dead_letter_queue.name = test-dead-letter-queue",
		"remote_queue.sqs_smartbus.endpoint = https://sqs.us-west-2.amazonaws.com",
		"remote_queue.sqs_smartbus.large_message_store.endpoint = https://s3.us-west-2.amazonaws.com",
		"remote_queue.sqs_smartbus.large_message_store.path = s3://test-bucket/smartbus-test",
		"remote_queue.sqs_smartbus.retry_policy = max_count",
		"remote_queue.sqs_smartbus.max_count.max_retries_per_part = 4"}
	outputs     = append(inputs, "remote_queue.sqs_smartbus.encoding_format = s2s", "remote_queue.sqs_smartbus.send_interval = 5s")
	defaultsAll = []string{
		"[pipeline:remotequeueruleset]\ndisabled = false",
		"[pipeline:ruleset]\ndisabled = true",
		"[pipeline:remotequeuetyping]\ndisabled = false",
		"[pipeline:remotequeueoutput]\ndisabled = false",
		"[pipeline:typing]\ndisabled = true",
	}
	defaultsIngest = append(defaultsAll, "[pipeline:indexerPipe]\ndisabled = true")

	awsEnvVars = []string{
		"AWS_REGION=us-west-2",
		"AWS_DEFAULT_REGION=us-west-2",
		"AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token",
		"AWS_ROLE_ARN=arn:aws:iam::",
		"AWS_STS_REGIONAL_ENDPOINTS=regional",
	}

	updateBus = enterpriseApi.BusConfigurationSpec{
		Type: "sqs_smartbus",
		SQS: enterpriseApi.SQSSpec{
			QueueName:                 "test-queue-updated",
			AuthRegion:                "us-west-2",
			Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
			LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
			LargeMessageStorePath:     "s3://test-bucket-updated/smartbus-test",
			DeadLetterQueueName:       "test-dead-letter-queue-updated",
		},
	}

	updatedInputs = []string{
		"[remote_queue:test-queue-updated]",
		"remote_queue.type = sqs_smartbus",
		"remote_queue.sqs_smartbus.auth_region = us-west-2",
		"remote_queue.sqs_smartbus.dead_letter_queue.name = test-dead-letter-queue-updated",
		"remote_queue.sqs_smartbus.endpoint = https://sqs.us-west-2.amazonaws.com",
		"remote_queue.sqs_smartbus.large_message_store.endpoint = https://s3.us-west-2.amazonaws.com",
		"remote_queue.sqs_smartbus.large_message_store.path = s3://test-bucket-updated/smartbus-test",
		"remote_queue.sqs_smartbus.retry_policy = max",
		"remote_queue.max.sqs_smartbus.max_retries_per_part = 5"}
	updatedOutputs     = append(updatedInputs, "remote_queue.sqs_smartbus.encoding_format = s2s", "remote_queue.sqs_smartbus.send_interval = 4s")
	updatedDefaultsAll = []string{
		"[pipeline:remotequeueruleset]\ndisabled = false",
		"[pipeline:ruleset]\ndisabled = false",
		"[pipeline:remotequeuetyping]\ndisabled = false",
		"[pipeline:remotequeueoutput]\ndisabled = false",
		"[pipeline:typing]\ndisabled = true",
	}
	updatedDefaultsIngest = append(updatedDefaultsAll, "[pipeline:indexerPipe]\ndisabled = true")

	inputsShouldNotContain = []string{
		"[remote_queue:test-queue]",
		"remote_queue.sqs_smartbus.dead_letter_queue.name = test-dead-letter-queue",
		"remote_queue.sqs_smartbus.large_message_store.path = s3://test-bucket/smartbus-test",
		"remote_queue.sqs_smartbus.retry_policy = max_count",
		"remote_queue.sqs_smartbus.max_count.max_retries_per_part = 4"}
	outputsShouldNotContain = append(inputs, "remote_queue.sqs_smartbus.send_interval = 5s")

	testDataS3Bucket    = os.Getenv("TEST_BUCKET")
	testS3Bucket        = os.Getenv("TEST_INDEXES_S3_BUCKET")
	currDir, _          = os.Getwd()
	downloadDirV1       = filepath.Join(currDir, "icappfwV1-"+testenv.RandomDNSName(4))
	appSourceVolumeName = "appframework-test-volume-" + testenv.RandomDNSName(3)
	s3TestDir           = "icappfw-" + testenv.RandomDNSName(4)
	appListV1           = testenv.BasicApps
	s3AppDirV1          = testenv.AppLocationV1
)

// TestBasic is the main entry point
func TestBasic(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Running "+testSuiteName)
}

var _ = BeforeSuite(func() {
	var err error
	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).ToNot(HaveOccurred())

	appListV1 = testenv.BasicApps
	appFileList := testenv.GetAppFileList(appListV1)

	// Download V1 Apps from S3
	err = testenv.DownloadFilesFromS3(testDataS3Bucket, s3AppDirV1, downloadDirV1, appFileList)
	Expect(err).To(Succeed(), "Unable to download V1 app files")
})

var _ = AfterSuite(func() {
	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).ToNot(HaveOccurred())
	}

	err := os.RemoveAll(downloadDirV1)
	Expect(err).To(Succeed(), "Unable to delete locally downloaded V1 app files")
})
