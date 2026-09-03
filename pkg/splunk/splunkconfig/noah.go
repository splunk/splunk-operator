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

// NoahSearchHeadConf returns the non-sensitive server.conf settings for a Noah
// search-head member. This matches the proven-working reference
// (vivek-spike's noahDefaultsYAML) exactly: no [decouple_search_indexing] and
// no usePeers, which that reference never sets either — decoupleSearchIndexing
// was an unverified addition of ours with no precedent in that spike or in
// Splunk Cloud's production Noah config, and it forced usePeers=true and
// heartbeatPeriod=0 as hard splunkd-validation requirements that don't
// otherwise apply. Credentials and pod-specific identity are delivered
// separately and must not be included here.
func NoahSearchHeadConf(serviceURL, tenant string) []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "server",
			Value: common.ConfFileValue{
				Stanzas: common.ConfFileStanzas{
					"noahService": {
						"disabled":        "false",
						"uri":             serviceURL,
						"tenant":          tenant,
						"heartbeatPeriod": "30",
					},
					"teleport_supervisor": {
						"disabled": "true",
					},
				},
			},
		},
	}
}

// NoahDeployerConf returns the non-sensitive server.conf settings for a Noah
// SHC deployer. Content is intentionally identical to NoahSearchHeadConf,
// matching the proven-working reference (vivek-spike), which applies the same
// shared defaults to both deployer and search-head with no role-specific
// server.conf differences. Delivery still goes through separate ConfigMaps
// per role (deployer vs. search-head), so this stays a distinct function.
func NoahDeployerConf(serviceURL, tenant string) []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "server",
			Value: common.ConfFileValue{
				Stanzas: common.ConfFileStanzas{
					"noahService": {
						"disabled":        "false",
						"uri":             serviceURL,
						"tenant":          tenant,
						"heartbeatPeriod": "30",
					},
					"teleport_supervisor": {
						"disabled": "true",
					},
				},
			},
		},
	}
}

// NoahSHCCredentialsConf returns the credential-only server.conf ConfFileEntry
// carrying [noahService] pass4SymmKey. It is identical for the deployer and
// every search-head member, and is delivered via a Secret (WithDictionaryConf),
// never a ConfigMap. splunk-ansible's own defaults loader (environ.py's
// merge_dict) recursively deep-merges this into the same splunk.conf.server.
// content.noahService map produced by NoahSearchHeadConf/NoahDeployerConf, so
// the structural and credential entries union into one stanza before Ansible
// ever runs — no operator-owned volume or init container is involved.
func NoahSHCCredentialsConf(pass4SymmKey string) []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "server",
			Value: common.ConfFileValue{
				Stanzas: common.ConfFileStanzas{
					"noahService": {
						"pass4SymmKey": pass4SymmKey,
					},
				},
			},
		},
	}
}
