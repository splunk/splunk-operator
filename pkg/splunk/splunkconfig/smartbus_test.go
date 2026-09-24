// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
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

package splunkconfig_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
)

func TestNewSmartBusConfBuilder(t *testing.T) {
	sqsCp := sqsQueue("q", "dlq", "us-east-2", "")
	sqsCp.Provider = "sqs_cp"
	kafka := sqsQueue("q", "dlq", "us-east-2", "")
	kafka.Provider = "kafka"

	tests := []struct {
		name          string
		queue         *enterpriseApi.QueueSpec
		expectBuilder bool
		expectErr     bool
	}{
		{"nil queue", nil, false, false},
		{"sqs", sqsQueue("q", "dlq", "us-east-2", ""), true, false},
		{"sqs_cp", sqsCp, true, false},
		{"unsupported provider", kafka, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder, err := splunkconfig.NewSmartBusConfBuilder(tc.queue, s3Storage("bucket", ""))
			if tc.expectErr {
				require.Error(t, err)
				assert.Nil(t, builder)
			} else {
				require.NoError(t, err)
				if tc.expectBuilder {
					assert.NotNil(t, builder)
				} else {
					assert.Nil(t, builder)
				}
			}
		})
	}
}

// Both outputs.conf and inputs.conf must use the same stanza name and app directory.
func TestSQSConf_StanzaName(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("splunk-test-queue", "splunk-dlq", "us-east-2", ""),
		s3Storage("bucket/smartbus", ""),
	)
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)

	for _, confFile := range []string{"outputs", "inputs"} {
		t.Run(confFile, func(t *testing.T) {
			entry, ok := findEntry(entries, confFile)
			require.True(t, ok)
			_, hasStanza := entry.Value.Stanzas["remote_queue:splunk-test-queue"]
			assert.True(t, hasStanza, "stanza must be remote_queue:<queue-name>")
		})
	}
}

func TestSQSConf_Directory(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)

	for _, confFile := range []string{"outputs", "inputs"} {
		t.Run(confFile, func(t *testing.T) {
			entry, ok := findEntry(entries, confFile)
			require.True(t, ok)
			assert.Equal(t, "/opt/splunk/etc/apps/100-sok/local", entry.Value.Directory)
		})
	}
}

func TestSQSOutputsConf_Fields(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("s3://bucket/path", ""),
	)
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)
	out, ok := findEntry(entries, "outputs")
	require.True(t, ok)
	fields := out.Value.Stanzas["remote_queue:q"]

	tests := []struct {
		field string
		want  string
	}{
		{"remote_queue.type", "sqs_smartbus"},
		{"remote_queue.sqs_smartbus.auth_region", "us-east-2"},
		{"remote_queue.sqs_smartbus.large_message_store.path", "s3://bucket/path"},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			assert.Equal(t, tc.want, fields[tc.field])
		})
	}
}

// dead_letter_queue.name is an inputs-only parameter — it has no meaning for outputs.
func TestSQSOutputsConf_NoDLQName(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "splunk-dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)
	out, ok := findEntry(entries, "outputs")
	require.True(t, ok)
	fields := out.Value.Stanzas["remote_queue:q"]

	_, has := fields["remote_queue.sqs_smartbus.dead_letter_queue.name"]
	assert.False(t, has, "dead_letter_queue.name must not appear in outputs.conf")
}

// Optional endpoint fields must appear only when configured, and be absent otherwise.
func TestSQSOutputsConf_Endpoints(t *testing.T) {
	tests := []struct {
		name        string
		sqsEndpoint string
		s3Endpoint  string
		wantSQSKey  bool
		wantS3Key   bool
	}{
		{"no endpoints", "", "", false, false},
		{"sqs endpoint only", "https://sqs.us-east-2.amazonaws.com", "", true, false},
		{"s3 endpoint only", "", "https://s3.us-east-2.amazonaws.com", false, true},
		{"both endpoints", "https://sqs.us-east-2.amazonaws.com", "https://s3.us-east-2.amazonaws.com", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder, err := splunkconfig.NewSmartBusConfBuilder(
				sqsQueue("q", "dlq", "us-east-2", tc.sqsEndpoint),
				s3Storage("bucket/path", tc.s3Endpoint),
			)
			require.NoError(t, err)
			entries := splunkconfig.IndexerConf(builder)
			out, ok := findEntry(entries, "outputs")
			require.True(t, ok)
			fields := out.Value.Stanzas["remote_queue:q"]

			_, hasSQS := fields["remote_queue.sqs_smartbus.endpoint"]
			_, hasS3 := fields["remote_queue.sqs_smartbus.large_message_store.endpoint"]
			assert.Equal(t, tc.wantSQSKey, hasSQS, "sqs endpoint presence mismatch")
			assert.Equal(t, tc.wantS3Key, hasS3, "s3 endpoint presence mismatch")
		})
	}
}

func TestSQSInputsConf_Fields(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "splunk-dlq", "us-east-2", ""),
		s3Storage("s3://bucket/path", ""),
	)
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)
	in, ok := findEntry(entries, "inputs")
	require.True(t, ok)
	fields := in.Value.Stanzas["remote_queue:q"]

	tests := []struct {
		field string
		want  string
	}{
		{"remote_queue.type", "sqs_smartbus"},
		{"remote_queue.sqs_smartbus.auth_region", "us-east-2"},
		{"remote_queue.sqs_smartbus.dead_letter_queue.name", "splunk-dlq"},
		{"remote_queue.sqs_smartbus.large_message_store.path", "s3://bucket/path"},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			assert.Equal(t, tc.want, fields[tc.field])
		})
	}
}

// The sqs_cp provider must use "sqs_smartbus_cp" as the field key prefix
// instead of "sqs_smartbus".
func TestSQSCPProvider_UsesCorrectFieldPrefix(t *testing.T) {
	q := sqsQueue("q", "dlq", "us-east-2", "")
	q.Provider = "sqs_cp"
	builder, err := splunkconfig.NewSmartBusConfBuilder(q, s3Storage("bucket/path", ""))
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)

	out, ok := findEntry(entries, "outputs")
	require.True(t, ok)
	fields := out.Value.Stanzas["remote_queue:q"]

	assert.Equal(t, "sqs_smartbus_cp", fields["remote_queue.type"])
	assert.Equal(t, "us-east-2", fields["remote_queue.sqs_smartbus_cp.auth_region"])
}

// encryption_scheme is written to both inputs.conf and outputs.conf when set.
func TestSQSConf_EncryptionScheme(t *testing.T) {
	for _, confFile := range []string{"outputs", "inputs"} {
		t.Run(confFile, func(t *testing.T) {
			builder, err := splunkconfig.NewSmartBusConfBuilder(
				sqsQueue("q", "dlq", "us-east-2", ""),
				s3StorageWithEncryption("bucket/path", "", "sse-s3", "", ""),
			)
			require.NoError(t, err)
			entries := splunkconfig.IndexerConf(builder)
			entry, ok := findEntry(entries, confFile)
			require.True(t, ok)
			fields := entry.Value.Stanzas["remote_queue:q"]
			assert.Equal(t, "sse-s3", fields["remote_queue.sqs_smartbus.large_message_store.encryption_scheme"])
		})
	}
}

// A pre-resolved kms_endpoint must be written to the conf exactly as provided.
func TestSQSConf_KMSEndpointWrittenWhenSet(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
	}{
		{"region-derived", "https://kms.us-west-2.amazonaws.com"},
		{"custom", "https://kms.custom.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			builder, err := splunkconfig.NewSmartBusConfBuilder(
				sqsQueue("q", "dlq", "us-west-2", ""),
				s3StorageWithEncryption("bucket/path", "", "sse-c", tc.endpoint, "alias/mykey"),
			)
			require.NoError(t, err)
			entries := splunkconfig.IndexerConf(builder)
			out, ok := findEntry(entries, "outputs")
			require.True(t, ok)
			fields := out.Value.Stanzas["remote_queue:q"]
			assert.Equal(t, tc.endpoint, fields["remote_queue.sqs_smartbus.large_message_store.kms_endpoint"])
		})
	}
}

// kms_endpoint must not appear when not set in the S3Spec (resolution is queue_os.go's job).
func TestSQSConf_KMSEndpointNotWrittenWhenNil(t *testing.T) {
	for _, scheme := range []string{"sse-s3", "sse-c"} {
		t.Run(scheme, func(t *testing.T) {
			builder, err := splunkconfig.NewSmartBusConfBuilder(
				sqsQueue("q", "dlq", "us-east-2", ""),
				s3StorageWithEncryption("bucket/path", "", scheme, "", ""),
			)
			require.NoError(t, err)
			entries := splunkconfig.IndexerConf(builder)
			out, ok := findEntry(entries, "outputs")
			require.True(t, ok)
			fields := out.Value.Stanzas["remote_queue:q"]
			_, has := fields["remote_queue.sqs_smartbus.large_message_store.kms_endpoint"]
			assert.False(t, has, "kms_endpoint must not appear when nil in S3Spec")
		})
	}
}

// key_id must be written when set.
func TestSQSConf_KMSKeyID(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3StorageWithEncryption("bucket/path", "", "sse-c", "", "alias/sqsssekeytrial"),
	)
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)
	out, ok := findEntry(entries, "outputs")
	require.True(t, ok)
	fields := out.Value.Stanzas["remote_queue:q"]
	assert.Equal(t, "alias/sqsssekeytrial", fields["remote_queue.sqs_smartbus.large_message_store.key_id"])
}

// When the path is given without the s3:// scheme, it must be added automatically.
func TestS3Path_Normalization(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"adds prefix when missing", "mybucket/smartbus", "s3://mybucket/smartbus"},
		{"does not double prefix", "s3://mybucket/smartbus", "s3://mybucket/smartbus"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder, err := splunkconfig.NewSmartBusConfBuilder(
				sqsQueue("q", "dlq", "us-east-2", ""),
				s3Storage(tc.input, ""),
			)
			require.NoError(t, err)
			entries := splunkconfig.IndexerConf(builder)
			out, ok := findEntry(entries, "outputs")
			require.True(t, ok)
			fields := out.Value.Stanzas["remote_queue:q"]
			assert.Equal(t, tc.expected, fields["remote_queue.sqs_smartbus.large_message_store.path"])
		})
	}
}
