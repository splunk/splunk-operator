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
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
)

func TestIndexerConf_NilBuilder(t *testing.T) {
	entries := splunkconfig.IndexerConf(nil)
	assert.Empty(t, entries)
}

// An indexer needs inputs.conf, outputs.conf, and default-mode.conf — three entries.
func TestIndexerConf_HasInputsOutputsAndPipeline(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)

	entries := splunkconfig.IndexerConf(builder)
	require.Len(t, entries, 3)

	_, hasInputs := findEntry(entries, "inputs")
	_, hasOutputs := findEntry(entries, "outputs")
	_, hasPipeline := findEntry(entries, "default-mode")

	assert.True(t, hasInputs, "indexer must have inputs.conf")
	assert.True(t, hasOutputs, "indexer must have outputs.conf")
	assert.True(t, hasPipeline, "indexer must have default-mode.conf")
}

// Indexer pipeline stanzas enable remote queue processing and disable the classic typing pipeline.
func TestIndexerPipelineConf_Stanzas(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)
	pipeline, ok := findEntry(entries, "default-mode")
	require.True(t, ok, "default-mode entry missing")

	tests := []struct {
		stanza   string
		disabled string
	}{
		{"pipeline:remotequeueruleset", "false"},
		{"pipeline:ruleset", "true"},
		{"pipeline:remotequeuetyping", "false"},
		{"pipeline:remotequeueoutput", "false"},
		{"pipeline:typing", "true"},
	}
	for _, tc := range tests {
		t.Run(tc.stanza, func(t *testing.T) {
			assert.Equal(t, tc.disabled, pipeline.Value.Stanzas[tc.stanza]["disabled"])
		})
	}
}

// The indexer still runs the indexerPipe; only the ingestor disables it.
func TestIndexerPipelineConf_DoesNotDisableIndexerPipe(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)
	entries := splunkconfig.IndexerConf(builder)
	pipeline, ok := findEntry(entries, "default-mode")
	require.True(t, ok)

	_, hasIndexerPipe := pipeline.Value.Stanzas["pipeline:indexerPipe"]
	assert.False(t, hasIndexerPipe, "indexer must not disable indexerPipe")
}

func TestIndexerCredentialsConf_NilBuilder(t *testing.T) {
	entries := splunkconfig.IndexerCredentialsConf(nil, "AKIA", "shhh")
	assert.Empty(t, entries)
}

func TestIndexerCredentialsConf_EmptyCredentials(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)

	assert.Empty(t, splunkconfig.IndexerCredentialsConf(builder, "", ""), "no credentials → no entries")
	assert.Empty(t, splunkconfig.IndexerCredentialsConf(builder, "AKIA", ""), "missing secret key → no entries")
	assert.Empty(t, splunkconfig.IndexerCredentialsConf(builder, "", "shhh"), "missing access key → no entries")
}

// The credential entries target inputs.conf and outputs.conf, carry only access_key/secret_key,
// and live in the dedicated creds app directory (distinct from the structural 100-sok directory).
func TestIndexerCredentialsConf_InputsOutputsWithKeysInCredsDir(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("test-queue", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)

	entries := splunkconfig.IndexerCredentialsConf(builder, "AKIA", "shhh")
	require.Len(t, entries, 2)

	inputs, hasInputs := findEntry(entries, "inputs")
	outputs, hasOutputs := findEntry(entries, "outputs")
	require.True(t, hasInputs, "credentials must target inputs.conf")
	require.True(t, hasOutputs, "credentials must target outputs.conf")

	for _, e := range []common.ConfFileEntry{inputs, outputs} {
		assert.Equal(t, "/opt/splunk/etc/apps/101-sok-secrets/local", e.Value.Directory)
		stanza := e.Value.Stanzas["remote_queue:test-queue"]
		require.NotNil(t, stanza)
		assert.Equal(t, "AKIA", stanza["remote_queue.sqs_smartbus.access_key"])
		assert.Equal(t, "shhh", stanza["remote_queue.sqs_smartbus.secret_key"])
		// Structural fields must not leak into the credentials entry.
		assert.NotContains(t, stanza, "remote_queue.type")
		assert.NotContains(t, stanza, "remote_queue.sqs_smartbus.auth_region")
	}
}

// The sqs_cp provider uses the sqs_smartbus_cp field prefix for credential keys.
func TestIndexerCredentialsConf_SQSCPFieldPrefix(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		&enterpriseApi.QueueSpec{
			Provider: "sqs_cp",
			SQS:      enterpriseApi.SQSSpec{Name: "cpq", DLQ: "dlq", AuthRegion: "us-east-2"},
		},
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)

	entries := splunkconfig.IndexerCredentialsConf(builder, "AKIA", "shhh")
	require.Len(t, entries, 2)

	outputs, ok := findEntry(entries, "outputs")
	require.True(t, ok)
	stanza := outputs.Value.Stanzas["remote_queue:cpq"]
	require.NotNil(t, stanza)
	assert.Equal(t, "AKIA", stanza["remote_queue.sqs_smartbus_cp.access_key"])
	assert.Equal(t, "shhh", stanza["remote_queue.sqs_smartbus_cp.secret_key"])
}
