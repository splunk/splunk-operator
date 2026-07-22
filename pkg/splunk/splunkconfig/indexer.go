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

// IndexerConf returns the full set of default.yml ConfFileEntries for an IndexerCluster.
func IndexerConf(builder SmartBusConfBuilder) []common.ConfFileEntry {
	var entries []common.ConfFileEntry
	if builder != nil {
		entries = append(entries,
			builder.BuildInputsConf(),
			builder.BuildOutputsConf(),
			builder.BuildPipelineConf(nil),
		)
	}
	return entries
}

// IndexerCredentialsConf returns the credential-only default.yml ConfFileEntries for an
// IndexerCluster: the access_key/secret_key stanza written into both inputs.conf and outputs.conf
// (matching the structural entries produced by IndexerConf). Returns an empty slice when the
// builder is nil or credentials are empty (e.g. IRSA / workload identity).
func IndexerCredentialsConf(builder SmartBusConfBuilder, accessKey, secretKey string) []common.ConfFileEntry {
	if builder == nil || accessKey == "" || secretKey == "" {
		return nil
	}
	return []common.ConfFileEntry{
		builder.BuildSecretConf("inputs", accessKey, secretKey),
		builder.BuildSecretConf("outputs", accessKey, secretKey),
	}
}
