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

	"github.com/stretchr/testify/require"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig/smartbus"
)

// confSet composes the SmartBus feature for one role exactly as a reconciler does.
func confSet(t *testing.T, queue *enterpriseApi.QueueSpec, objectStorage *enterpriseApi.ObjectStorageSpec, role common.InstanceType, accessKey, secretKey string) splunkconfig.ConfSet {
	t.Helper()
	set, err := splunkconfig.ForRole(role).
		With(smartbus.Conf(queue, objectStorage, accessKey, secretKey)).
		Build()
	require.NoError(t, err)
	return set
}

// indexerNonSensitive is the shape most provider tests need: an indexer emits both
// inputs.conf and outputs.conf, so one call exercises every non-sensitive stanza.
func indexerNonSensitive(t *testing.T, queue *enterpriseApi.QueueSpec, objectStorage *enterpriseApi.ObjectStorageSpec) []common.ConfFileEntry {
	t.Helper()
	return confSet(t, queue, objectStorage, common.SplunkIndexer, "", "").NonSensitive
}

func findEntry(entries []common.ConfFileEntry, confFileName string) (common.ConfFileEntry, bool) {
	for _, e := range entries {
		if e.ConfFileName == confFileName {
			return e, true
		}
	}
	return common.ConfFileEntry{}, false
}

func sqsQueue(name, dlq, region, endpoint string) *enterpriseApi.QueueSpec {
	return &enterpriseApi.QueueSpec{
		Provider: "sqs",
		SQS: enterpriseApi.SQSSpec{
			Name:       name,
			DLQ:        dlq,
			AuthRegion: region,
			Endpoint:   endpoint,
		},
	}
}

func s3Storage(path, endpoint string) *enterpriseApi.ObjectStorageSpec {
	return &enterpriseApi.ObjectStorageSpec{
		Provider: "s3",
		S3: enterpriseApi.S3Spec{
			Path:     path,
			Endpoint: endpoint,
		},
	}
}

func s3StorageWithEncryption(path, endpoint, scheme, kmsEndpoint, kmsKeyID string) *enterpriseApi.ObjectStorageSpec {
	s3 := enterpriseApi.S3Spec{
		Path:     path,
		Endpoint: endpoint,
	}
	if scheme != "" {
		s3.EncryptionScheme = &scheme
	}
	if kmsEndpoint != "" {
		s3.KMSEndpoint = &kmsEndpoint
	}
	if kmsKeyID != "" {
		s3.KMSKeyID = &kmsKeyID
	}
	return &enterpriseApi.ObjectStorageSpec{
		Provider: "s3",
		S3:       s3,
	}
}
