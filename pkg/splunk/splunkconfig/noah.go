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
// search-head member. No [decouple_search_indexing] and no usePeers override:
// decoupleSearchIndexing was an unverified addition of ours with no precedent
// in vivek-spike or in Splunk Cloud's production Noah config, and it forced
// usePeers=true and heartbeatPeriod=0 as hard splunkd-validation requirements
// that don't otherwise apply.
//
// heartbeatPeriod=0 (not unset, not 30) — live-verified 2026-09-08 against a
// real Splunk Cloud stack (mqiu-noah), which delivers this exact value to its
// search head's [noahService] (etc/apps/100-whisper-searchhead/local/server.conf,
// via `splunk btool server list noahService --debug`). Two prior assumptions
// both turned out wrong on splunkd build 10.5.2605.8:
//   - Leaving heartbeatPeriod entirely unset (this repo's server.conf.spec on
//     an older Enterprise build documents unset as valid — "will not send a
//     heartbeat") instead makes splunkd's NoahConfiguration::
//     loadNoahServiceFromConfFilesReloadable assert and SIGABRT during
//     startup on this build: `terminate called ... Cannot parse
//     'heartbeatPeriod' server.conf/[noahService]/heartbeatPeriod`.
//   - heartbeatPeriod=30 (vivek-spike's original value, copied unquestioned)
//     parses fine but makes the search head actually heartbeat and register
//     as a Noah peer — once real indexer peers went stale/down, Noah's
//     bucket-map strategy substituted the search heads/deployer as the map's
//     routing peers instead of correctly reporting no eligible peers,
//     leaving the SHC with zero real distributed-search peers.
//
// pass4SymmKey_minLength=10 also matches that same live production stanza
// (this repo's server.conf.spec instead documents a default of 12 — the
// production value overrides it explicitly rather than relying on the
// default). Credentials and pod-specific identity are delivered separately
// and must not be included here.
func NoahSearchHeadConf(serviceURL, tenant string) []common.ConfFileEntry {
	return []common.ConfFileEntry{
		{
			ConfFileName: "server",
			Value: common.ConfFileValue{
				Stanzas: common.ConfFileStanzas{
					"noahService": {
						"disabled":               "false",
						"uri":                    serviceURL,
						"tenant":                 tenant,
						"heartbeatPeriod":        "0",
						"pass4SymmKey_minLength": "10",
					},
				},
			},
		},
	}
}

// NoahCredentialsConf returns the credential-only server.conf entry carrying
// [noahService] pass4SymmKey. It is delivered through a Secret and combined
// with the non-sensitive Noah configuration by splunk-ansible.
func NoahCredentialsConf(pass4SymmKey string) []common.ConfFileEntry {
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
