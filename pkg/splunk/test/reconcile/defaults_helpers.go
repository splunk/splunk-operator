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

package reconciletest

import (
	"strings"
	"testing"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

// ExpectLocalKVStoreDefaultsCalls adds the local-defaults ConfigMap operations
// to classic reconciliation expectations. Configuration content is checked separately by
// the KVService defaults tests.
func ExpectLocalKVStoreDefaultsCalls(t *testing.T, cr common.MetaObject, createCalls, updateCalls map[string][]spltest.MockFuncCall) resources.DefaultsConfigMap {
	t.Helper()
	owner := common.AsOwner(cr, true)
	cm, err := resources.NewDefaultsConfigMap(cr, []common.ConfFileEntry{{
		ConfFileName: "server", Value: common.ConfFileValue{
			Stanzas: common.ConfFileStanzas{"kvstore": {"defaultKVStoreType": "local", "postgresMigrateOnStartup": "false"}},
		},
	}}, &owner)
	if err != nil {
		t.Fatal(err)
	}
	expected := spltest.MockFuncCall{MetaName: "*v1.ConfigMap-" + cr.GetNamespace() + "-" + cm.Name}
	insert := func(calls []spltest.MockFuncCall) []spltest.MockFuncCall {
		for _, call := range calls {
			if call.MetaName == expected.MetaName {
				return calls
			}
		}
		index := len(calls)
		for i, call := range calls {
			if cr.GetObjectKind().GroupVersionKind().Kind == "SearchHeadCluster" {
				if (strings.HasPrefix(call.MetaName, "*v1.StatefulSet-") && strings.HasSuffix(call.MetaName, "-search-head")) || strings.Contains(call.MetaName, "-search-head-secret-v") {
					index = i
					break
				}
				continue
			}
			if strings.HasPrefix(call.MetaName, "*v1.StatefulSet-") || strings.Contains(call.MetaName, "-probe-configmap") || strings.Contains(call.MetaName, "-secret-v") {
				index = i
				break
			}
		}
		out := make([]spltest.MockFuncCall, 0, len(calls)+1)
		out = append(out, calls[:index]...)
		out = append(out, expected)
		out = append(out, calls[index:]...)
		return out
	}
	createCalls["Get"] = insert(createCalls["Get"])
	createCalls["Create"] = insert(createCalls["Create"])
	updateCalls["Get"] = insert(updateCalls["Get"])
	return cm
}
