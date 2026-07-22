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
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

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
