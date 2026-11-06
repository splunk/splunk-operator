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

package kvservice

import (
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	"github.com/splunk/splunk-operator/pkg/splunk/util"
)

type feature struct{ connectionString, tenant string }

var _ splunkconfig.ConfFeature = (*feature)(nil)

// Conf builds client settings from the resolved connection string and tenant.
// An empty connection string selects local KV Store.
// The caller selects placement; the KVService workflow uses system/local.
func Conf(connectionString, tenant string) splunkconfig.ConfFeature {
	return &feature{connectionString: connectionString, tenant: tenant}
}

func (*feature) Name() string { return "kvservice" }

func (f *feature) Contribute(role common.InstanceType) (splunkconfig.ConfSet, error) {
	switch role {
	case common.SplunkStandalone, common.SplunkIndexer, common.SplunkSearchHead,
		common.SplunkClusterManager, common.SplunkLicenseManager, common.SplunkMonitoringConsole,
		common.SplunkClusterMaster, common.SplunkLicenseMaster:
		settings := common.StanzaFields{"defaultKVStoreType": "local"}
		stanzas := common.ConfFileStanzas{"kvstore": settings}
		if f.connectionString != "" {
			endpoint, err := util.ParseKVServiceAddress(f.connectionString)
			if err != nil {
				return splunkconfig.ConfSet{}, err
			}
			settings["defaultKVStoreType"] = "external"
			settings["kvserviceConnectionString"] = endpoint.String()
			stanzas["scs"] = common.StanzaFields{"tenant": f.tenant, "mvl-enabled": "true"}
		} else {
			// Preserve local KV Store startup behavior when KVService is disabled.
			settings["postgresMigrateOnStartup"] = "false"
		}
		return splunkconfig.ConfSet{NonSensitive: []common.ConfFileEntry{{
			ConfFileName: "server",
			Value:        common.ConfFileValue{Stanzas: stanzas},
		}}}, nil
	}
	return splunkconfig.ConfSet{}, nil
}
