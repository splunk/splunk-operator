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

package upgrade

import "testing"

func TestIndexerClusterGateChecksEverySearchHeadCluster(t *testing.T) {
	const target = "splunk/splunk:new"
	ready := ResourceState{Name: "shc-a", Image: target, Phase: phaseReady}
	tests := []struct {
		name         string
		searchHeads  []ResourceState
		wantContinue bool
	}{
		{"no SHC", nil, true},
		{"all SHCs ready on target", []ResourceState{ready, {Name: "shc-b", Image: target, Phase: phaseReady}}, true},
		{"one SHC on old image", []ResourceState{ready, {Name: "shc-b", Image: "splunk/splunk:old", Phase: phaseReady}}, false},
		{"one SHC not ready", []ResourceState{ready, {Name: "shc-b", Image: target, Phase: "Updating"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Validate(State{
				Kind:                   "IndexerCluster",
				Name:                   "idx",
				Image:                  target,
				HasClusterManagerRef:   true,
				ValidateIndexerCluster: true,
				ClusterManager:         &ResourceState{Name: "cm", Image: target, Phase: phaseReady},
				SearchHeadClusters:     tt.searchHeads,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Continue != tt.wantContinue {
				t.Fatalf("Continue = %v, want %v", res.Continue, tt.wantContinue)
			}
		})
	}
}
