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

	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
)

func TestIngestorConf_NilBuilder(t *testing.T) {
	entries := splunkconfig.IngestorConf(nil)
	assert.Empty(t, entries)
}

// An ingestor (physical separation) only produces data — it needs outputs.conf
// and default-mode.conf but NOT inputs.conf.
func TestIngestorConf_HasOutputsAndPipelineButNotInputs(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)

	entries := splunkconfig.IngestorConf(builder)
	require.Len(t, entries, 2)

	_, hasInputs := findEntry(entries, "inputs")
	_, hasOutputs := findEntry(entries, "outputs")
	_, hasPipeline := findEntry(entries, "default-mode")

	assert.False(t, hasInputs, "ingestor must not have inputs.conf")
	assert.True(t, hasOutputs, "ingestor must have outputs.conf")
	assert.True(t, hasPipeline, "ingestor must have default-mode.conf")
}

// TestIngestorCredentialsConf verifies that the credentials entry is outputs-only
// (no inputs.conf, unlike indexers), and that empty keys / nil builder yield nil.
func TestIngestorCredentialsConf_OutputsOnly(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)

	entries := splunkconfig.IngestorCredentialsConf(builder, "AKIAEXAMPLE", "shhh")
	require.Len(t, entries, 1, "ingestor credentials must produce exactly one entry (outputs only)")

	_, hasOutputs := findEntry(entries, "outputs")
	assert.True(t, hasOutputs, "ingestor credential entry must be for outputs.conf")

	_, hasInputs := findEntry(entries, "inputs")
	assert.False(t, hasInputs, "ingestor must not emit a credentials entry for inputs.conf")
}

func TestIngestorCredentialsConf_NilBuilder(t *testing.T) {
	assert.Nil(t, splunkconfig.IngestorCredentialsConf(nil, "key", "secret"))
}

func TestIngestorCredentialsConf_EmptyKey(t *testing.T) {
	tests := []struct {
		name      string
		accessKey string
		secretKey string
	}{
		{"empty access key", "", "secret"},
		{"empty secret key", "AKIAEXAMPLE", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			builder, err := splunkconfig.NewSmartBusConfBuilder(
				sqsQueue("q", "dlq", "us-east-2", ""),
				s3Storage("bucket/path", ""),
			)
			require.NoError(t, err)
			assert.Nil(t, splunkconfig.IngestorCredentialsConf(builder, tc.accessKey, tc.secretKey),
				"empty key → no credentials entry (IRSA path)")
		})
	}
}

// Ingestor pipeline stanzas enable remote queue processing and disable the classic typing pipeline.
func TestIngestorPipelineConf_Stanzas(t *testing.T) {
	builder, err := splunkconfig.NewSmartBusConfBuilder(
		sqsQueue("q", "dlq", "us-east-2", ""),
		s3Storage("bucket/path", ""),
	)
	require.NoError(t, err)
	entries := splunkconfig.IngestorConf(builder)
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
		{"pipeline:indexerPipe", "true"},
	}
	for _, tc := range tests {
		t.Run(tc.stanza, func(t *testing.T) {
			assert.Equal(t, tc.disabled, pipeline.Value.Stanzas[tc.stanza]["disabled"])
		})
	}
}
