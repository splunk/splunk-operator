// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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
package indexingestionsep

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/test/testenv"
)

var (
	testenvInstance *testenv.TestEnv
	testSuiteName   = "idxingsep-" + testenv.RandomDNSName(3)

	// Configurable AWS resource names (env var → default)
	sqsQueueName = testenv.GetEnvWithDefault("TEST_SQS_QUEUE", "index-ingest-separation-test-q")
	sqsDLQName   = testenv.GetEnvWithDefault("TEST_SQS_DLQ", "index-ingest-separation-test-dlq")
	s3BucketPath = testenv.GetEnvWithDefault("TEST_S3_BUCKET_PATH", "index-ingest-separation-test-bucket/smartbus-test")
	awsRegion    = testenv.GetEnvWithDefault("TEST_AWS_REGION", "us-west-2")
	sqsEndpoint  = testenv.GetEnvWithDefault("TEST_SQS_ENDPOINT", "")
	s3Endpoint   = testenv.GetEnvWithDefault("TEST_S3_ENDPOINT", "")

	queue              enterpriseApi.QueueSpec
	objectStorage      enterpriseApi.ObjectStorageSpec
	serviceAccountName = "index-ingest-sa"

	inputs                  []string
	outputs                 []string
	defaultsAll             []string
	defaultsIngest          []string
	awsEnvVars              []string
	inputsShouldNotContain  []string
	outputsShouldNotContain []string

	testDataS3Bucket    = os.Getenv("TEST_BUCKET")
	testS3Bucket        = os.Getenv("TEST_INDEXES_S3_BUCKET")
	currDir, _          = os.Getwd()
	downloadDirV1       = filepath.Join(currDir, "icappfwV1-"+testenv.RandomDNSName(4))
	appSourceVolumeName = "appframework-test-volume-" + testenv.RandomDNSName(3)
	s3TestDir           = "icappfw-" + testenv.RandomDNSName(4)
	appListV1           = testenv.BasicApps
)

func init() {
	// Derive endpoints from region if not explicitly set
	if sqsEndpoint == "" {
		sqsEndpoint = fmt.Sprintf("https://sqs.%s.amazonaws.com", awsRegion)
	}
	if s3Endpoint == "" {
		s3Endpoint = fmt.Sprintf("https://s3.%s.amazonaws.com", awsRegion)
	}

	queue = enterpriseApi.QueueSpec{
		Provider: "sqs",
		SQS: enterpriseApi.SQSSpec{
			Name:       sqsQueueName,
			AuthRegion: awsRegion,
			Endpoint:   sqsEndpoint,
			DLQ:        sqsDLQName,
		},
	}
	objectStorage = enterpriseApi.ObjectStorageSpec{
		Provider: "s3",
		S3: enterpriseApi.S3Spec{
			Endpoint: s3Endpoint,
			Path:     s3BucketPath,
		},
	}

	inputs = []string{
		fmt.Sprintf("[remote_queue:%s]", sqsQueueName),
		"remote_queue.type = sqs_smartbus",
		fmt.Sprintf("remote_queue.sqs_smartbus.auth_region = %s", awsRegion),
		fmt.Sprintf("remote_queue.sqs_smartbus.dead_letter_queue.name = %s", sqsDLQName),
		fmt.Sprintf("remote_queue.sqs_smartbus.endpoint = %s", sqsEndpoint),
		fmt.Sprintf("remote_queue.sqs_smartbus.large_message_store.endpoint = %s", s3Endpoint),
		fmt.Sprintf("remote_queue.sqs_smartbus.large_message_store.path = s3://%s", s3BucketPath),
		"remote_queue.sqs_smartbus.retry_policy = max_count",
		"remote_queue.sqs_smartbus.max_count.max_retries_per_part = 4",
	}
	outputs = append(inputs, "remote_queue.sqs_smartbus.encoding_format = s2s", "remote_queue.sqs_smartbus.send_interval = 5s")

	defaultsAll = []string{
		"[pipeline:remotequeueruleset]\ndisabled = false",
		"[pipeline:ruleset]\ndisabled = true",
		"[pipeline:remotequeuetyping]\ndisabled = false",
		"[pipeline:remotequeueoutput]\ndisabled = false",
		"[pipeline:typing]\ndisabled = true",
	}
	defaultsIngest = append(defaultsAll, "[pipeline:indexerPipe]\ndisabled = true")

	awsEnvVars = []string{
		fmt.Sprintf("AWS_REGION=%s", awsRegion),
		fmt.Sprintf("AWS_DEFAULT_REGION=%s", awsRegion),
		"AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token",
		"AWS_ROLE_ARN=arn:aws:iam::",
		"AWS_STS_REGIONAL_ENDPOINTS=regional",
	}

	inputsShouldNotContain = []string{
		fmt.Sprintf("[remote_queue:%s]", sqsQueueName),
		fmt.Sprintf("remote_queue.sqs_smartbus.dead_letter_queue.name = %s", sqsDLQName),
		fmt.Sprintf("remote_queue.sqs_smartbus.large_message_store.path = s3://%s", s3BucketPath),
		"remote_queue.sqs_smartbus.retry_policy = max_count",
		"remote_queue.sqs_smartbus.max_count.max_retries_per_part = 4",
	}
	outputsShouldNotContain = append(inputs, "remote_queue.sqs_smartbus.send_interval = 5s")
}

// TestIndexIngestionSeparation is the main entry point
func TestIndexIngestionSeparation(t *testing.T) {
	RegisterFailHandler(Fail)

	sc, _ := GinkgoConfiguration()
	sc.Timeout = testenv.ShortSuiteTimeout

	RunSpecs(t, "Running "+testSuiteName, sc)
}

var _ = BeforeSuite(func() {
	var err error
	testenvInstance, err = testenv.NewDefaultTestEnv(testSuiteName)
	Expect(err).To(Succeed(), "Failed to initialize test environment")

	appListV1 = testenv.BasicApps
	appFileList := testenv.GetAppFileList(appListV1)

	// Download V1 Apps from S3
	err = testenv.DownloadFilesFromS3(testDataS3Bucket, testenv.AppLocationV1, downloadDirV1, appFileList)
	Expect(err).To(Succeed(), "Unable to download V1 app files")
})

var _ = AfterSuite(func() {
	if testenvInstance != nil {
		Expect(testenvInstance.Teardown()).To(Succeed(), "Failed to teardown test environment")
	}

	err := os.RemoveAll(downloadDirV1)
	Expect(err).To(Succeed(), "Unable to delete locally downloaded V1 app files")
})
