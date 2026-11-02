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

package smartbus_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig/smartbus"
)

// A nil queue means smartbus is not configured, so the feature must be nil and
// injecting it must contribute nothing.
func TestConf_NilQueue(t *testing.T) {
	assert.Nil(t, smartbus.Conf(nil, s3Storage("bucket", ""), "AKIA", "shhh"))

	set := confSet(t, nil, s3Storage("bucket", ""), common.SplunkIndexer, "AKIA", "shhh")
	assert.Empty(t, set.NonSensitive)
	assert.Empty(t, set.Secrets)
}

// An unsupported provider is deferred to Build so callers keep the fluent chain.
func TestConf_UnsupportedProvider(t *testing.T) {
	kafka := sqsQueue("q", "dlq", "us-east-2", "")
	kafka.Provider = "kafka"

	feature := smartbus.Conf(kafka, s3Storage("bucket", ""), "", "")
	require.NotNil(t, feature, "an unsupported provider must still yield a feature that reports the error")

	_, err := splunkconfig.ForRole(common.SplunkIndexer).With(feature).Build()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported queue provider")
	assert.Contains(t, err.Error(), "smartbus", "Build must attribute the error to the feature")
}

func TestConf_SupportedProviders(t *testing.T) {
	sqsCp := sqsQueue("q", "dlq", "us-east-2", "")
	sqsCp.Provider = "sqs_cp"

	for _, queue := range []*enterpriseApi.QueueSpec{sqsQueue("q", "dlq", "us-east-2", ""), sqsCp} {
		t.Run(queue.Provider, func(t *testing.T) {
			assert.NotEmpty(t, indexerNonSensitive(t, queue, s3Storage("bucket", "")))
		})
	}
}

// An indexer needs inputs.conf, outputs.conf, and default-mode.conf — three entries.
func TestIndexer_HasInputsOutputsAndPipeline(t *testing.T) {
	entries := indexerNonSensitive(t, sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""))
	require.Len(t, entries, 3)

	_, hasInputs := findEntry(entries, "inputs")
	_, hasOutputs := findEntry(entries, "outputs")
	_, hasPipeline := findEntry(entries, "default-mode")

	assert.True(t, hasInputs, "indexer must have inputs.conf")
	assert.True(t, hasOutputs, "indexer must have outputs.conf")
	assert.True(t, hasPipeline, "indexer must have default-mode.conf")
}

// Indexer pipeline stanzas enable remote queue processing and disable the classic typing pipeline.
func TestIndexerPipeline_Stanzas(t *testing.T) {
	entries := indexerNonSensitive(t, sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""))
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
func TestIndexerPipeline_DoesNotDisableIndexerPipe(t *testing.T) {
	entries := indexerNonSensitive(t, sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""))
	pipeline, ok := findEntry(entries, "default-mode")
	require.True(t, ok)

	_, hasIndexerPipe := pipeline.Value.Stanzas["pipeline:indexerPipe"]
	assert.False(t, hasIndexerPipe, "indexer must not disable indexerPipe")
}

func TestIndexerCredentials_EmptyCredentials(t *testing.T) {
	queue, storage := sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", "")

	tests := []struct {
		name      string
		accessKey string
		secretKey string
	}{
		{"no credentials", "", ""},
		{"missing secret key", "AKIA", ""},
		{"missing access key", "", "shhh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set := confSet(t, queue, storage, common.SplunkIndexer, tc.accessKey, tc.secretKey)
			assert.Empty(t, set.Secrets, "incomplete credentials → no secret entries (IRSA path)")
		})
	}
}

// The credential entries target inputs.conf and outputs.conf, carry only access_key/secret_key,
// and live in the dedicated creds app directory (distinct from the non-sensitive 100-sok directory).
func TestIndexerCredentials_InputsOutputsWithKeysInCredsDir(t *testing.T) {
	set := confSet(t, sqsQueue("test-queue", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""),
		common.SplunkIndexer, "AKIA", "shhh")
	require.Len(t, set.Secrets, 2)

	inputs, hasInputs := findEntry(set.Secrets, "inputs")
	outputs, hasOutputs := findEntry(set.Secrets, "outputs")
	require.True(t, hasInputs, "credentials must target inputs.conf")
	require.True(t, hasOutputs, "credentials must target outputs.conf")

	for _, e := range []common.ConfFileEntry{inputs, outputs} {
		assert.Equal(t, "/opt/splunk/etc/apps/101-sok-secrets/local", e.Value.Directory)
		stanza := e.Value.Stanzas["remote_queue:test-queue"]
		require.NotNil(t, stanza)
		assert.Equal(t, "AKIA", stanza["remote_queue.sqs_smartbus.access_key"])
		assert.Equal(t, "shhh", stanza["remote_queue.sqs_smartbus.secret_key"])
		// Non-sensitive fields must not leak into the credentials entry.
		assert.NotContains(t, stanza, "remote_queue.type")
		assert.NotContains(t, stanza, "remote_queue.sqs_smartbus.auth_region")
	}
}

// SmartBus credentials and queue settings target the same files. If both
// outputs are configured for the same directory, Build must reject the
// collision instead of allowing the credentials file to replace queue config.
func TestIndexerCredentials_RejectSharedConfDirectory(t *testing.T) {
	tests := []struct {
		name string
		dir  string
	}{
		{name: "Splunk default directory", dir: ""},
		{name: "shared app directory", dir: "/opt/splunk/etc/apps/100-sok/local"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := splunkconfig.ForRole(common.SplunkIndexer).
				NonSensitiveConfDir(tc.dir).
				SecretsConfDir(tc.dir).
				With(smartbus.Conf(
					sqsQueue("test-queue", "dlq", "us-east-2", ""),
					s3Storage("bucket/path", ""), "AKIA", "shhh",
				)).
				Build()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "smartbus")
			assert.Contains(t, err.Error(), "both write")
			assert.Contains(t, err.Error(), "inputs.conf")
			assert.Contains(t, err.Error(), tc.dir)
		})
	}
}

// The sqs_cp provider uses the sqs_smartbus_cp field prefix for credential keys.
func TestIndexerCredentials_SQSCPFieldPrefix(t *testing.T) {
	set := confSet(t,
		&enterpriseApi.QueueSpec{
			Provider: "sqs_cp",
			SQS:      enterpriseApi.SQSSpec{Name: "cpq", DLQ: "dlq", AuthRegion: "us-east-2"},
		},
		s3Storage("bucket/path", ""),
		common.SplunkIndexer, "AKIA", "shhh")
	require.Len(t, set.Secrets, 2)

	outputs, ok := findEntry(set.Secrets, "outputs")
	require.True(t, ok)
	stanza := outputs.Value.Stanzas["remote_queue:cpq"]
	require.NotNil(t, stanza)
	assert.Equal(t, "AKIA", stanza["remote_queue.sqs_smartbus_cp.access_key"])
	assert.Equal(t, "shhh", stanza["remote_queue.sqs_smartbus_cp.secret_key"])
}

// An ingestor (physical separation) only produces data — it needs outputs.conf
// and default-mode.conf but NOT inputs.conf.
func TestIngestor_HasOutputsAndPipelineButNotInputs(t *testing.T) {
	set := confSet(t, sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""),
		common.SplunkIngestor, "", "")
	require.Len(t, set.NonSensitive, 2)

	_, hasInputs := findEntry(set.NonSensitive, "inputs")
	_, hasOutputs := findEntry(set.NonSensitive, "outputs")
	_, hasPipeline := findEntry(set.NonSensitive, "default-mode")

	assert.False(t, hasInputs, "ingestor must not have inputs.conf")
	assert.True(t, hasOutputs, "ingestor must have outputs.conf")
	assert.True(t, hasPipeline, "ingestor must have default-mode.conf")
}

// The ingestor credentials entry is outputs-only (no inputs.conf, unlike indexers).
func TestIngestorCredentials_OutputsOnly(t *testing.T) {
	set := confSet(t, sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""),
		common.SplunkIngestor, "AKIAEXAMPLE", "shhh")
	require.Len(t, set.Secrets, 1, "ingestor credentials must produce exactly one entry (outputs only)")

	_, hasOutputs := findEntry(set.Secrets, "outputs")
	assert.True(t, hasOutputs, "ingestor credential entry must be for outputs.conf")

	_, hasInputs := findEntry(set.Secrets, "inputs")
	assert.False(t, hasInputs, "ingestor must not emit a credentials entry for inputs.conf")
}

func TestIngestorCredentials_EmptyKey(t *testing.T) {
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
			set := confSet(t, sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""),
				common.SplunkIngestor, tc.accessKey, tc.secretKey)
			assert.Empty(t, set.Secrets, "empty key → no credentials entry (IRSA path)")
		})
	}
}

// Ingestor pipeline stanzas enable remote queue processing, disable the classic typing
// pipeline, and additionally disable the indexing pipeline.
func TestIngestorPipeline_Stanzas(t *testing.T) {
	set := confSet(t, sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""),
		common.SplunkIngestor, "", "")
	pipeline, ok := findEntry(set.NonSensitive, "default-mode")
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

// A role SmartBus does not configure opts out by contributing nothing.
func TestUnconfiguredRole(t *testing.T) {
	for _, role := range []common.InstanceType{common.SplunkSearchHead, common.SplunkStandalone, common.SplunkDeployer} {
		t.Run(role.ToString(), func(t *testing.T) {
			set := confSet(t, sqsQueue("q", "dlq", "us-east-2", ""), s3Storage("bucket/path", ""),
				role, "AKIA", "shhh")
			assert.Empty(t, set.NonSensitive)
			assert.Empty(t, set.Secrets)
		})
	}
}
