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

package splunkconfig

import (
	"fmt"
	"maps"
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

const sokAppDirectory = "/opt/splunk/etc/apps/100-sok/local"

// sokSecretsAppDirectory is a separate app directory for credential stanzas. It must differ
// from sokAppDirectory: splunk-ansible deletes the whole target .conf file before writing
// each defaults entry, so structural and credential entries targeting the same conf file
// would clobber one another if they shared a directory. Keeping them in distinct app dirs
// lets Splunk's btool layering union the disjoint keys at runtime.
const sokSecretsAppDirectory = "/opt/splunk/etc/apps/101-sok-secrets/local"

// smartBusPipelineConf returns the default-mode ConfFileEntry that enables smartbus pipelines.
// extraStanzas are merged in to allow role-specific overrides (e.g. disabling indexerPipe for ingestors).
func smartBusPipelineConf(extraStanzas common.ConfFileStanzas) common.ConfFileEntry {
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

// SmartBusConfBuilder builds default.yml ConfFileEntries for a specific SmartBus queue provider.
// Each provider implements its own stanza structure for outputs.conf, inputs.conf, and default-mode.conf.
//
// BuildOutputsConf/BuildInputsConf/BuildPipelineConf emit the structural config (endpoints, region,
// paths, pipeline stanzas) and never contain credentials. BuildSecretConf emits the sensitive
// access_key/secret_key stanza into a separate app directory so it can be delivered via a Secret.
type SmartBusConfBuilder interface {
	BuildOutputsConf() common.ConfFileEntry
	BuildInputsConf() common.ConfFileEntry
	BuildPipelineConf(extraStanzas common.ConfFileStanzas) common.ConfFileEntry
	BuildSecretConf(confFileName, accessKey, secretKey string) common.ConfFileEntry
}

// NewSmartBusConfBuilder returns a SmartBusConfBuilder for the given queue and object storage specs.
// Returns nil, nil if queue is nil (smartbus not configured).
// Returns an error if the provider is not supported.
func NewSmartBusConfBuilder(queue *enterpriseApi.QueueSpec, os *enterpriseApi.ObjectStorageSpec) (SmartBusConfBuilder, error) {
	if queue == nil {
		return nil, nil
	}
	switch queue.Provider {
	case "sqs", "sqs_cp":
		return &sqsConfBuilder{sqs: queue.SQS, s3: os.S3, provider: queue.Provider}, nil
	default:
		return nil, fmt.Errorf("unsupported queue provider: %q", queue.Provider)
	}
}

// sqsConfBuilder builds outputs.conf and inputs.conf ConfFileEntries for SQS-based queue providers.
type sqsConfBuilder struct {
	sqs      enterpriseApi.SQSSpec
	s3       enterpriseApi.S3Spec
	provider string // "sqs" or "sqs_cp"
}

// providerKey returns the field key prefix for this provider,
// e.g. "sqs_smartbus" or "sqs_smartbus_cp".
func (b *sqsConfBuilder) providerKey() string {
	if b.provider == "sqs_cp" {
		return "sqs_smartbus_cp"
	}
	return "sqs_smartbus"
}

func (b *sqsConfBuilder) s3Path() string {
	if strings.HasPrefix(b.s3.Path, "s3://") {
		return b.s3.Path
	}
	return "s3://" + b.s3.Path
}

func (b *sqsConfBuilder) baseFields() common.StanzaFields {
	pk := b.providerKey()
	fields := common.StanzaFields{
		"remote_queue.type":                                      pk,
		"remote_queue." + pk + ".auth_region":                    b.sqs.AuthRegion,
		"remote_queue." + pk + ".retry_policy":                   "max_count",
		"remote_queue." + pk + ".max_count.max_retries_per_part": "4",
	}
	if b.sqs.Endpoint != "" {
		fields["remote_queue."+pk+".endpoint"] = b.sqs.Endpoint
	}
	if b.s3.Endpoint != "" {
		fields["remote_queue."+pk+".large_message_store.endpoint"] = b.s3.Endpoint
	}
	fields["remote_queue."+pk+".large_message_store.path"] = b.s3Path()
	return fields
}

func (b *sqsConfBuilder) BuildOutputsConf() common.ConfFileEntry {
	fields := b.baseFields()
	pk := b.providerKey()
	fields["remote_queue."+pk+".encoding_format"] = "s2s"
	fields["remote_queue."+pk+".send_interval"] = "5s"

	return common.ConfFileEntry{
		ConfFileName: "outputs",
		Value: common.ConfFileValue{
			Directory: sokAppDirectory,
			Stanzas:   common.ConfFileStanzas{"remote_queue:" + b.sqs.Name: fields},
		},
	}
}

func (b *sqsConfBuilder) BuildPipelineConf(extraStanzas common.ConfFileStanzas) common.ConfFileEntry {
	return smartBusPipelineConf(extraStanzas)
}

func (b *sqsConfBuilder) BuildInputsConf() common.ConfFileEntry {
	fields := b.baseFields()
	pk := b.providerKey()
	fields["remote_queue."+pk+".dead_letter_queue.name"] = b.sqs.DLQ

	return common.ConfFileEntry{
		ConfFileName: "inputs",
		Value: common.ConfFileValue{
			Directory: sokAppDirectory,
			Stanzas:   common.ConfFileStanzas{"remote_queue:" + b.sqs.Name: fields},
		},
	}
}

// BuildSecretConf returns a ConfFileEntry carrying only the access_key/secret_key for the
// remote_queue stanza, rendered into sokSecretsAppDirectory (distinct from the structural entries)
// so it can be delivered via a Secret without clobbering the structural conf file. confFileName
// is the target conf file ("outputs" or "inputs").
func (b *sqsConfBuilder) BuildSecretConf(confFileName, accessKey, secretKey string) common.ConfFileEntry {
	pk := b.providerKey()
	return common.ConfFileEntry{
		ConfFileName: confFileName,
		Value: common.ConfFileValue{
			Directory: sokSecretsAppDirectory,
			Stanzas: common.ConfFileStanzas{
				"remote_queue:" + b.sqs.Name: common.StanzaFields{
					"remote_queue." + pk + ".access_key": accessKey,
					"remote_queue." + pk + ".secret_key": secretKey,
				},
			},
		},
	}
}
