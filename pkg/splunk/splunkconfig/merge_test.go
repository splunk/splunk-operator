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

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeConfEntries(t *testing.T) {
	noah := splunkconfig.NoahIndexerConf("https://noah.example", "tenant")[0]
	kv := common.ConfFileEntry{ConfFileName: "server", Value: common.ConfFileValue{
		Stanzas: common.ConfFileStanzas{"kvstore": {"defaultKVStoreType": "local"}},
	}}
	credentials := splunkconfig.NoahCredentialsConf("test-key")[0]
	app := common.ConfFileEntry{ConfFileName: "server", Value: common.ConfFileValue{
		Directory: splunkconfig.SOKAppDir,
		Stanzas:   common.ConfFileStanzas{"kvstore": {"defaultKVStoreType": "external"}},
	}}
	merged, err := splunkconfig.MergeConfEntries(noah, kv, credentials, app)
	require.NoError(t, err)
	require.Len(t, merged, 2)
	assert.Empty(t, merged[0].Value.Directory)
	assert.Equal(t, "https://noah.example", merged[0].Value.Stanzas["noahService"]["uri"])
	assert.Equal(t, "test-key", merged[0].Value.Stanzas["noahService"]["pass4SymmKey"])
	assert.Equal(t, "local", merged[0].Value.Stanzas["kvstore"]["defaultKVStoreType"])
	assert.Equal(t, app, merged[1])
	merged[0].Value.Stanzas["noahService"]["uri"] = "changed"
	merged[0].Value.Stanzas["kvstore"]["defaultKVStoreType"] = "changed"
	merged[1].Value.Stanzas["kvstore"]["defaultKVStoreType"] = "changed"
	assert.Equal(t, "https://noah.example", noah.Value.Stanzas["noahService"]["uri"])
	assert.NotContains(t, noah.Value.Stanzas["noahService"], "pass4SymmKey")
	assert.Equal(t, "local", kv.Value.Stanzas["kvstore"]["defaultKVStoreType"])
	assert.Equal(t, "external", app.Value.Stanzas["kvstore"]["defaultKVStoreType"])

	conflicting := splunkconfig.NoahIndexerConf("https://different.example", "tenant")[0]
	_, err = splunkconfig.MergeConfEntries(noah, conflicting)
	require.ErrorContains(t, err, "conflicting field uri in [noahService]")
	assert.NotContains(t, err.Error(), "https://different.example")
}
