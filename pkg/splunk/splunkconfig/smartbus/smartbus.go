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

package smartbus

import (
	"fmt"
	"maps"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
)

// Defaults only: RoleConfBuilder may override them.
const (
	sokAppDirectory        = splunkconfig.SOKAppDir
	sokSecretsAppDirectory = splunkconfig.SOKSecretsAppDir
)

// feature carries the resolved queue, object storage and credentials.
type feature struct {
	provider  provider
	accessKey string
	secretKey string
	err       error
}

var _ splunkconfig.ConfFeature = (*feature)(nil)

// Conf returns the SmartBus feature, or nil when queue is nil (not configured).
// An unsupported provider is reported from Contribute so callers keep the fluent
// With(...) chain.
func Conf(queue *enterpriseApi.QueueSpec, objectStorage *enterpriseApi.ObjectStorageSpec, accessKey, secretKey string) splunkconfig.ConfFeature {
	if queue == nil {
		return nil
	}
	selected, err := newProvider(queue, objectStorage)
	return &feature{provider: selected, accessKey: accessKey, secretKey: secretKey, err: err}
}

// Name implements splunkconfig.ConfFeature.
func (*feature) Name() string { return "smartbus" }

// Contribute owns the role-to-conf-file mapping, identical for every provider: an
// indexer reads and writes the queue, an ingestor only writes and must not index.
func (smartbus *feature) Contribute(role common.InstanceType) (splunkconfig.ConfSet, error) {
	if smartbus.err != nil {
		return splunkconfig.ConfSet{}, smartbus.err
	}

	switch role {
	case common.SplunkIndexer:
		return splunkconfig.ConfSet{
			NonSensitive: []common.ConfFileEntry{
				smartbus.provider.inputsConf(),
				smartbus.provider.outputsConf(),
				pipelineConf(nil),
			},
			Secrets: smartbus.secretsConf("inputs", "outputs"),
		}, nil

	case common.SplunkIngestor:
		return splunkconfig.ConfSet{
			NonSensitive: []common.ConfFileEntry{
				smartbus.provider.outputsConf(),
				pipelineConf(common.ConfFileStanzas{
					"pipeline:indexerPipe": {"disabled": "true"},
				}),
			},
			Secrets: smartbus.secretsConf("outputs"),
		}, nil
	}

	return splunkconfig.ConfSet{}, nil
}

// Empty credentials mean IRSA / workload identity, where no static keys exist.
func (smartbus *feature) secretsConf(confFileNames ...string) []common.ConfFileEntry {
	if smartbus.accessKey == "" || smartbus.secretKey == "" {
		return nil
	}
	entries := make([]common.ConfFileEntry, 0, len(confFileNames))
	for _, confFileName := range confFileNames {
		entries = append(entries, smartbus.provider.secretConf(confFileName, smartbus.accessKey, smartbus.secretKey))
	}
	return entries
}

// provider is the queue-provider axis: it emits stanzas and knows nothing about
// Splunk roles. Only secretConf carries credentials.
type provider interface {
	outputsConf() common.ConfFileEntry
	inputsConf() common.ConfFileEntry
	secretConf(confFileName, accessKey, secretKey string) common.ConfFileEntry
}

// newProvider selects the provider implementation for a queue spec.
func newProvider(queue *enterpriseApi.QueueSpec, objectStorage *enterpriseApi.ObjectStorageSpec) (provider, error) {
	switch queue.Provider {
	case "sqs", "sqs_cp":
		return &sqsProvider{sqs: queue.SQS, s3: objectStorage.S3, provider: queue.Provider}, nil
	default:
		return nil, fmt.Errorf("unsupported queue provider: %q", queue.Provider)
	}
}

// pipelineConf enables the smartbus pipelines. extraStanzas allows role-specific
// overrides and is provider-independent.
func pipelineConf(extraStanzas common.ConfFileStanzas) common.ConfFileEntry {
	stanzas := common.ConfFileStanzas{
		"pipeline:remotequeueruleset": {"disabled": "false"},
		"pipeline:ruleset":            {"disabled": "true"},
		"pipeline:remotequeuetyping":  {"disabled": "false"},
		"pipeline:remotequeueoutput":  {"disabled": "false"},
		"pipeline:typing":             {"disabled": "true"},
	}
	maps.Copy(stanzas, extraStanzas)
	return common.ConfFileEntry{
		ConfFileName: "default-mode",
		Value: common.ConfFileValue{
			Directory: sokAppDirectory,
			Stanzas:   stanzas,
		},
	}
}
