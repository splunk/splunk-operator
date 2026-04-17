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

	// Default values for AWS resources (used when environment variables are not set)
	defaultSQSQueueName = "index-ingest-separation-test-q"
	defaultSQSDLQName   = "index-ingest-separation-test-dlq"
	defaultS3Bucket     = "index-ingest-separation-test-bucket"
	defaultS3Prefix     = "smartbus-test"
	defaultAWSRegion    = "us-west-2"
)

// AWS resource configuration - populated in init() from environment variables
var (
	// Configurable AWS resources via environment variables
	sqsQueueName string
	sqsDLQName   string
	s3Bucket     string
	s3Prefix     string
	awsRegion    string
	sqsEndpoint  string
	s3Endpoint   string
)

var (
	testenvInstance *testenv.TestEnv
	testSuiteName   = "indingsep-" + testenv.RandomDNSName(3)

	queue         enterpriseApi.QueueSpec
	objectStorage enterpriseApi.ObjectStorageSpec

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
	s3AppDirV1          = testenv.AppLocationV1
)

func init() {
	// Initialize AWS resource configuration from environment variables with defaults
	sqsQueueName = testenv.GetEnvWithDefault("TEST_SQS_QUEUE_NAME", defaultSQSQueueName)
	sqsDLQName = testenv.GetEnvWithDefault("TEST_SQS_DLQ_NAME", defaultSQSDLQName)
	s3Bucket = testenv.GetEnvWithDefault("TEST_INGEST_S3_BUCKET", defaultS3Bucket)
	s3Prefix = testenv.GetEnvWithDefault("TEST_INGEST_S3_PREFIX", defaultS3Prefix)
	awsRegion = testenv.GetEnvWithDefault("TEST_AWS_REGION", defaultAWSRegion)
	sqsEndpoint = testenv.GetEnvWithDefault("TEST_SQS_ENDPOINT", "https://sqs."+awsRegion+".amazonaws.com")
	s3Endpoint = testenv.GetEnvWithDefault("TEST_S3_ENDPOINT", "https://s3."+awsRegion+".amazonaws.com")

	// Build queue spec
	queue = enterpriseApi.QueueSpec{
		Provider: "sqs",
		SQS: enterpriseApi.SQSSpec{
			Name:       sqsQueueName,
			AuthRegion: awsRegion,
			Endpoint:   sqsEndpoint,
			DLQ:        sqsDLQName,
		},
	}

	// Build object storage spec
	objectStorage = enterpriseApi.ObjectStorageSpec{
		Provider: "s3",
		S3: enterpriseApi.S3Spec{
			Endpoint: s3Endpoint,
			Path:     s3Bucket + "/" + s3Prefix,
		},
	}

	// Build inputs configuration
	inputs = []string{
		"[remote_queue:" + sqsQueueName + "]",
		"remote_queue.type = sqs_smartbus",
		"remote_queue.sqs_smartbus.auth_region = " + awsRegion,
		"remote_queue.sqs_smartbus.dead_letter_queue.name = " + sqsDLQName,
		"remote_queue.sqs_smartbus.endpoint = " + sqsEndpoint,
		"remote_queue.sqs_smartbus.large_message_store.endpoint = " + s3Endpoint,
		"remote_queue.sqs_smartbus.large_message_store.path = s3://" + s3Bucket + "/" + s3Prefix,
		"remote_queue.sqs_smartbus.retry_policy = max_count",
		"remote_queue.sqs_smartbus.max_count.max_retries_per_part = 4",
	}
	outputs = append(inputs, "remote_queue.sqs_smartbus.encoding_format = s2s", "remote_queue.sqs_smartbus.send_interval = 5s")

	// Build defaults configuration
	defaultsAll = []string{
		"[pipeline:remotequeueruleset]\ndisabled = false",
		"[pipeline:ruleset]\ndisabled = true",
		"[pipeline:remotequeuetyping]\ndisabled = false",
		"[pipeline:remotequeueoutput]\ndisabled = false",
		"[pipeline:typing]\ndisabled = true",
	}
	defaultsIngest = append(defaultsAll, "[pipeline:indexerPipe]\ndisabled = true")

	// Build AWS environment variables for pods
	awsEnvVars = []string{
		"AWS_REGION=" + awsRegion,
		"AWS_DEFAULT_REGION=" + awsRegion,
		"AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/eks.amazonaws.com/serviceaccount/token",
		"AWS_ROLE_ARN=arn:aws:iam::",
		"AWS_STS_REGIONAL_ENDPOINTS=regional",
	}

	// Build negative test assertions
	inputsShouldNotContain = []string{
		"[remote_queue:" + sqsQueueName + "]",
		"remote_queue.sqs_smartbus.dead_letter_queue.name = " + sqsDLQName,
		"remote_queue.sqs_smartbus.large_message_store.path = s3://" + s3Bucket + "/" + s3Prefix,
		"remote_queue.sqs_smartbus.retry_policy = max_count",
		"remote_queue.sqs_smartbus.max_count.max_retries_per_part = 4",
	}
	outputsShouldNotContain = append(inputs, "remote_queue.sqs_smartbus.send_interval = 5s")
}

// TestBasic is the main entry point
func TestBasic(t *testing.T) {
	RegisterFailHandler(Fail)

	sc, _ := GinkgoConfiguration()
	sc.Timeout = testenv.ShortSuiteTimeout

	RunSpecs(t, "Running "+testSuiteName, sc)
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
