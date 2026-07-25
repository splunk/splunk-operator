// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
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

import "testing"

func TestClassifySHCDefaultsRestart(t *testing.T) {
	defaults := func(settings string) string {
		return `splunk:
  conf:
    server:
      content:
        shclustering:
` + settings
	}

	tests := []struct {
		name        string
		previous    string
		current     string
		wantSetting string
		wantError   bool
	}{
		{
			name: "unchanged",
			previous: defaults(
				"          replication_factor: 3\n",
			),
			current: defaults(
				"          replication_factor: 3\n",
			),
		},
		{
			name: "rolling compatible settings",
			previous: defaults(
				"          captain_is_adhoc_searchhead: false\n" +
					"          shcluster_label: old\n",
			),
			current: defaults(
				"          captain_is_adhoc_searchhead: true\n" +
					"          shcluster_label: new\n",
			),
		},
		{
			name: "simultaneous restart setting",
			previous: defaults(
				"          replication_factor: 3\n",
			),
			current: defaults(
				"          replication_factor: 5\n",
			),
			wantSetting: "replication_factor",
		},
		{
			name:     "deterministic first unsafe setting",
			previous: defaults("          shcluster_label: old\n"),
			current: defaults(
				"          replication_factor: 5\n" +
					"          captain_uri: https://captain:8089\n",
			),
			wantSetting: "captain_uri",
		},
		{
			name:      "malformed current document",
			previous:  defaults("          shcluster_label: old\n"),
			current:   "splunk: [",
			wantError: true,
		},
		{
			name: "non-scalar setting fails closed",
			current: defaults(
				"          replication_factor:\n" +
					"            - 3\n",
			),
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classification, err := ClassifySHCDefaultsRestart(
				test.current,
				test.previous,
			)
			if (err != nil) != test.wantError {
				t.Fatalf("classification error=%v wantError=%t", err, test.wantError)
			}
			if err != nil {
				return
			}
			if classification.Setting != test.wantSetting ||
				classification.RequiresSimultaneousRestart !=
					(test.wantSetting != "") {
				t.Fatalf(
					"classification=%#v wantSetting=%q",
					classification,
					test.wantSetting,
				)
			}
		})
	}
}
