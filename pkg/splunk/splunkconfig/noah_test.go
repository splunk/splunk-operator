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
