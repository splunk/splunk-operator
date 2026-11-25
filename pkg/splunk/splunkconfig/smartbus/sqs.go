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
	"strings"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

// sqsProvider builds SmartBus configuration for SQS queues and their S3-backed
// large-message store, including optional KMS encryption settings.
type sqsProvider struct {
	sqs      enterpriseApi.SQSSpec
	s3       enterpriseApi.S3Spec
	provider string // "sqs" or "sqs_cp"
}

var _ provider = (*sqsProvider)(nil)

// Only ingestors upload to CP. Indexer inputs, outputs and credentials use
// regular Smartbus even when they reference the same sqs_cp Queue.
func (builder *sqsProvider) providerKey(role common.InstanceType) string {
	if builder.provider == "sqs_cp" && role == common.SplunkIngestor {
		return "sqs_smartbus_cp"
	}
	return "sqs_smartbus"
}

func (builder *sqsProvider) s3Path() string {
	if strings.HasPrefix(builder.s3.Path, "s3://") {
		return builder.s3.Path
	}
	return "s3://" + builder.s3.Path
}

func (builder *sqsProvider) baseFields(role common.InstanceType) common.StanzaFields {
	pk := builder.providerKey(role)
	fields := common.StanzaFields{
		"remote_queue.type":                   pk,
		"remote_queue." + pk + ".auth_region": builder.sqs.AuthRegion,
	}
	if builder.sqs.Endpoint != "" {
		fields["remote_queue."+pk+".endpoint"] = builder.sqs.Endpoint
	}
	if builder.s3.Endpoint != "" {
		fields["remote_queue."+pk+".large_message_store.endpoint"] = builder.s3.Endpoint
	}
	fields["remote_queue."+pk+".large_message_store.path"] = builder.s3Path()
	if builder.s3.EncryptionScheme != nil {
		scheme := *builder.s3.EncryptionScheme
		fields["remote_queue."+pk+".large_message_store.encryption_scheme"] = scheme
		if builder.s3.KMSEndpoint != nil {
			fields["remote_queue."+pk+".large_message_store.kms_endpoint"] = *builder.s3.KMSEndpoint
		}
		if builder.s3.KMSKeyID != nil {
			fields["remote_queue."+pk+".large_message_store.key_id"] = *builder.s3.KMSKeyID
		}
	}
	return fields
}

func (builder *sqsProvider) outputsConf(role common.InstanceType) common.ConfFileEntry {
	return common.ConfFileEntry{
		ConfFileName: "outputs",
		Value: common.ConfFileValue{
			Directory: sokAppDirectory,
			Stanzas:   common.ConfFileStanzas{"remote_queue:" + builder.sqs.Name: builder.baseFields(role)},
		},
	}
}

func (builder *sqsProvider) inputsConf(role common.InstanceType) common.ConfFileEntry {
	fields := builder.baseFields(role)
	pk := builder.providerKey(role)
	fields["remote_queue."+pk+".dead_letter_queue.name"] = builder.sqs.DLQ

	return common.ConfFileEntry{
		ConfFileName: "inputs",
		Value: common.ConfFileValue{
			Directory: sokAppDirectory,
			Stanzas:   common.ConfFileStanzas{"remote_queue:" + builder.sqs.Name: fields},
		},
	}
}

// secretConf carries only access_key/secret_key, in a directory distinct from the
// non-sensitive entries. confFileName is "outputs" or "inputs".
func (builder *sqsProvider) secretConf(role common.InstanceType, confFileName, accessKey, secretKey string) common.ConfFileEntry {
	pk := builder.providerKey(role)
	return common.ConfFileEntry{
		ConfFileName: confFileName,
		Value: common.ConfFileValue{
			Directory: sokSecretsAppDirectory,
			Stanzas: common.ConfFileStanzas{
				"remote_queue:" + builder.sqs.Name: common.StanzaFields{
					"remote_queue." + pk + ".access_key": accessKey,
					"remote_queue." + pk + ".secret_key": secretKey,
				},
			},
		},
	}
}
