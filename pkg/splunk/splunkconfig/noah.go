// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package splunkconfig

import "github.com/splunk/splunk-operator/pkg/splunk/common"

// NoahIndexerConf returns the non-sensitive server.conf settings required to
// select Noah before splunkd starts. Credentials and pod-specific identity are
// delivered separately and must not be included in this shared configuration.
func NoahIndexerConf(serviceURL, tenant string) []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "server",
			Value: common.ConfFileValue{
				// TODO: Restore the SOK app directory once splunk-ansible resolves the
				// configured server.conf entry in its Noah pre-auth and post-config paths.
				Stanzas: common.ConfFileStanzas{
					"noahService": {
						"disabled":        "false",
						"uri":             serviceURL,
						"tenant":          tenant,
						"heartbeatPeriod": "30",
						"usePeers":        "false",
					},
				},
			},
		},
	}
}
