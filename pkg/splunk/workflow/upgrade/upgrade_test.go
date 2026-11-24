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

func TestMonitoringConsoleWaitsWithoutError(t *testing.T) {
	for _, kind := range []string{"cluster manager", "search head", "indexer", "standalone"} {
		for _, phase := range []string{"", "Pending", "Updating", phaseReady} {
			t.Run(kind+"/"+phase, func(t *testing.T) {
				state := State{Kind: "MonitoringConsole", MonitoringConsoleDependencies: []ResourceState{
					{Type: "standalone", Name: "ready", Phase: phaseReady},
					{Type: kind, Name: "dependency", Phase: phase},
				}}
				result, err := Validate(state)
				if err != nil || result.Continue != (phase == phaseReady) {
					t.Fatalf("Validate() = %+v, %v; want Continue=%v without error", result, err, phase == phaseReady)
				}
			})
		}
	}
	if result, err := Validate(State{Kind: "MonitoringConsole"}); err != nil || !result.Continue {
		t.Fatalf("no dependencies: got %+v, %v", result, err)
	}
}

func TestMonitoringConsoleClusterManagerGate(t *testing.T) {
	tests := []struct {
		name, kind, phase, image string
		wantContinue, wantError  bool
	}{
		{"MC pending", "MonitoringConsole", "Updating", "new", false, false},
		{"MC ready", "MonitoringConsole", phaseReady, "new", true, false},
		{"MC incompatible image", "MonitoringConsole", phaseReady, "old", false, true},
		{"indexer pending unchanged", "IndexerCluster", "Updating", "new", false, true},
		{"SHC pending unchanged", "SearchHeadCluster", "Updating", "new", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Validate(State{
				Kind: tt.kind, Image: "new", HasClusterManagerRef: true,
				ClusterManager: &ResourceState{Name: "cm", Phase: tt.phase, Image: tt.image},
			})
			if result.Continue != tt.wantContinue || (err != nil) != tt.wantError {
				t.Fatalf("Validate() = %+v, %v; want Continue=%v, error=%v", result, err, tt.wantContinue, tt.wantError)
			}
		})
	}
}
