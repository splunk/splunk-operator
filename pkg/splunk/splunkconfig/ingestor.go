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

import "github.com/splunk/splunk-operator/pkg/splunk/common"

// IngestorConf returns the full set of default.yml ConfFileEntries for an IngestorCluster.
func IngestorConf(builder SmartBusConfBuilder) []common.ConfFileEntry {
	var entries []common.ConfFileEntry
	if builder != nil {
		entries = append(entries,
			builder.BuildOutputsConf(),
			builder.BuildPipelineConf(common.ConfFileStanzas{
				"pipeline:indexerPipe": {"disabled": "true"},
			}),
		)
	}
	return entries
}

// IngestorCredentialsConf returns the credential-only default.yml ConfFileEntries for an
// IngestorCluster: the access_key/secret_key stanza written into outputs.conf only
// (ingestors do not have an inputs.conf, unlike indexers). Returns an empty slice when the
// builder is nil or credentials are empty (e.g. IRSA / workload identity).
func IngestorCredentialsConf(builder SmartBusConfBuilder, accessKey, secretKey string) []common.ConfFileEntry {
	if builder == nil || accessKey == "" || secretKey == "" {
		return nil
	}
	return []common.ConfFileEntry{
		builder.BuildSecretConf("outputs", accessKey, secretKey),
	}
}
