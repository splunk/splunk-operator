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

package splunkconfig_test

import (
	"testing"

	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoahIndexerConf(t *testing.T) {
	entries := splunkconfig.NoahIndexerConf("https://noah.example.invalid:8080", "placeholder")
	require.Len(t, entries, 1)

	server := entries[0]
	assert.Equal(t, "server", server.ConfFileName)
	assert.Empty(t, server.Value.Directory)
	assert.Equal(t, map[string]string{
		"disabled":        "false",
		"uri":             "https://noah.example.invalid:8080",
		"tenant":          "placeholder",
		"heartbeatPeriod": "30",
		"usePeers":        "false",
	}, map[string]string(server.Value.Stanzas["noahService"]))
	assert.NotContains(t, server.Value.Stanzas, "teleport_supervisor")
	assert.NotContains(t, server.Value.Stanzas["noahService"], "pass4SymmKey")
	assert.NotContains(t, server.Value.Stanzas["noahService"], "advertisedAddr")
}

// heartbeatPeriod=0 (not unset, not 30) — live-verified 2026-09-08 against a
// real Splunk Cloud stack's search head, which delivers this exact value.
// Unset instead crashes splunkd's NoahConfiguration on build 10.5.2605.8;
// 30 (vivek-spike's original value) makes the search head actually
// heartbeat and register as a Noah peer, corrupting bucket-map generation
// once real indexer peers go stale. pass4SymmKey_minLength=10 also matches
// that live production stanza.
func TestNoahSearchHeadConf(t *testing.T) {
	entries := splunkconfig.NoahSearchHeadConf("https://noah.example.invalid:8080", "placeholder")
	require.Len(t, entries, 1)

	server := entries[0]
	assert.Equal(t, "server", server.ConfFileName)
	assert.Equal(t, map[string]string{
		"disabled":               "false",
		"uri":                    "https://noah.example.invalid:8080",
		"tenant":                 "placeholder",
		"heartbeatPeriod":        "0",
		"pass4SymmKey_minLength": "10",
	}, map[string]string(server.Value.Stanzas["noahService"]))
	assert.NotContains(t, server.Value.Stanzas["noahService"], "usePeers",
		"search head relies on usePeers' documented default (true), not an explicit override")
	assert.Equal(t, map[string]string{"disabled": "true"}, map[string]string(server.Value.Stanzas["teleport_supervisor"]))
	assert.NotContains(t, server.Value.Stanzas["noahService"], "pass4SymmKey")
}

// The deployer gets the same minimal [noahService] as the search head.
// Live-verified 2026-09-08: a deployer with no [noahService] at all crashes
// splunkd's NoahConfiguration on build 10.5.2605.8 exactly like an
// incomplete search-head stanza does.
func TestNoahDeployerConf(t *testing.T) {
	entries := splunkconfig.NoahDeployerConf("https://noah.example.invalid:8080", "placeholder")
	require.Len(t, entries, 1)

	server := entries[0]
	assert.Equal(t, "server", server.ConfFileName)
	assert.Equal(t, map[string]string{
		"disabled":               "false",
		"uri":                    "https://noah.example.invalid:8080",
		"tenant":                 "placeholder",
		"heartbeatPeriod":        "0",
		"pass4SymmKey_minLength": "10",
	}, map[string]string(server.Value.Stanzas["noahService"]))
	assert.Equal(t, map[string]string{"disabled": "true"}, map[string]string(server.Value.Stanzas["teleport_supervisor"]))
	assert.NotContains(t, server.Value.Stanzas["noahService"], "pass4SymmKey")
}
