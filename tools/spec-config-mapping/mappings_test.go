// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestAllFieldsHaveMappings verifies that every CRD spec field discovered
// via reflection has a corresponding entry in fieldMappings.
func TestAllFieldsHaveMappings(t *testing.T) {
	registry := crdRegistry()
	allDiscovered := make(map[string]bool)

	for _, crd := range registry {
		fields := WalkSpecFields(crd.specType, "")
		for _, f := range fields {
			allDiscovered[f.jsonPath] = true
			if _, ok := fieldMappings[f.jsonPath]; !ok {
				t.Errorf("CRD %s: field %q has no entry in fieldMappings", crd.kind, f.jsonPath)
			}
		}
	}

	// Also check for stale entries: mapping keys that don't match any discovered field
	var stale []string
	for key := range fieldMappings {
		if !allDiscovered[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("Stale mapping entries (not found in any CRD spec):\n  %s", strings.Join(stale, "\n  "))
	}
}

// TestNoDuplicateFieldPaths ensures there are no duplicate field paths
// within a single CRD's discovered fields.
func TestNoDuplicateFieldPaths(t *testing.T) {
	registry := crdRegistry()
	for _, crd := range registry {
		fields := WalkSpecFields(crd.specType, "")
		seen := make(map[string]int)
		for _, f := range fields {
			seen[f.jsonPath]++
		}
		for path, count := range seen {
			if count > 1 {
				t.Errorf("CRD %s: field path %q discovered %d times (expected 1)", crd.kind, path, count)
			}
		}
	}
}

// TestFieldMappingDescriptions ensures every mapping has a non-empty description.
func TestFieldMappingDescriptions(t *testing.T) {
	for path, target := range fieldMappings {
		if target.Description == "" {
			t.Errorf("fieldMappings[%q] has an empty Description", path)
		}
	}
}

// TestConfFileMappingsHaveStanza checks that entries with a non-empty ConfFile
// (other than "env" or "") have a non-empty Stanza.
func TestConfFileMappingsHaveStanza(t *testing.T) {
	for path, target := range fieldMappings {
		if target.ConfFile != "" && target.ConfFile != "env" {
			if target.Stanza == "" {
				t.Errorf("fieldMappings[%q] has confFile=%q but no Stanza", path, target.ConfFile)
			}
		}
	}
}

// TestEnvMappingsHaveEnvVar checks that entries with ConfFile="env" have EnvVar set.
func TestEnvMappingsHaveEnvVar(t *testing.T) {
	for path, target := range fieldMappings {
		if target.ConfFile == "env" && target.EnvVar == "" {
			// extraEnv is a special case - it's a pass-through for arbitrary env vars
			if path == "extraEnv" {
				continue
			}
			t.Errorf("fieldMappings[%q] has confFile=env but no EnvVar", path)
		}
	}
}

// TestWalkSpecFieldsOutput is a sanity check that the walker produces expected paths.
func TestWalkSpecFieldsOutput(t *testing.T) {
	registry := crdRegistry()

	// Find the Standalone CRD and check some expected fields
	for _, crd := range registry {
		if crd.kind != "Standalone" {
			continue
		}
		fields := WalkSpecFields(crd.specType, "")
		paths := make(map[string]bool)
		for _, f := range fields {
			paths[f.jsonPath] = true
		}

		expected := []string{
			"replicas",
			"image",
			"licenseUrl",
			"smartstore.cacheManager.evictionPolicy",
			"smartstore.indexes.name",
			"smartstore.volumes.endpoint",
			"appRepo.appsRepoPollIntervalSeconds",
		}
		for _, e := range expected {
			if !paths[e] {
				t.Errorf("Standalone: expected field path %q not found. Got paths: %v", e, sortedKeys(paths))
			}
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestGenerateManifestIntegration runs the full generation logic and checks the output structure.
func TestGenerateManifestIntegration(t *testing.T) {
	registry := crdRegistry()
	fieldToCRDs := make(map[string][]string)

	for _, crd := range registry {
		fields := WalkSpecFields(crd.specType, "")
		for _, f := range fields {
			fieldToCRDs[f.jsonPath] = append(fieldToCRDs[f.jsonPath], crd.kind)
		}
	}

	// CommonSplunkSpec fields should appear in all 6 CRDs
	commonFields := []string{"image", "licenseUrl", "defaults", "clusterManagerRef"}
	for _, cf := range commonFields {
		crds, ok := fieldToCRDs[cf]
		if !ok {
			t.Errorf("expected common field %q to be discovered", cf)
			continue
		}
		if len(crds) != 6 {
			t.Errorf("common field %q found in %d CRDs, expected 6: %v", cf, len(crds), crds)
		}
	}

	// SmartStore fields should only appear in Standalone and ClusterManager
	ssFields := []string{"smartstore.cacheManager.evictionPolicy", "smartstore.indexes.name"}
	for _, sf := range ssFields {
		crds := fieldToCRDs[sf]
		if len(crds) != 2 {
			t.Errorf("smartstore field %q found in %d CRDs, expected 2: %v", sf, len(crds), crds)
		}
		for _, c := range crds {
			if c != "Standalone" && c != "ClusterManager" {
				t.Errorf("smartstore field %q unexpectedly in CRD %s", sf, c)
			}
		}
	}

	// appRepo fields: present in Standalone, ClusterManager, SearchHeadCluster, LicenseManager, MonitoringConsole (5)
	appFields := []string{"appRepo.appsRepoPollIntervalSeconds"}
	for _, af := range appFields {
		crds := fieldToCRDs[af]
		if len(crds) != 5 {
			t.Errorf("appRepo field %q found in %d CRDs, expected 5: %v", af, len(crds), crds)
		}
	}

	// replicas should appear in Standalone, IndexerCluster, SearchHeadCluster (3)
	crds := fieldToCRDs["replicas"]
	if len(crds) != 3 {
		t.Errorf("replicas found in %d CRDs, expected 3: %v", len(crds), crds)
	}

	// Deployer fields only in SearchHeadCluster
	for _, df := range []string{"deployerResourceSpec", "deployerNodeAffinity"} {
		crds := fieldToCRDs[df]
		if len(crds) != 1 || crds[0] != "SearchHeadCluster" {
			t.Errorf("field %q expected only in SearchHeadCluster, got: %v", df, crds)
		}
	}

	fmt.Printf("Integration check passed: %d unique field paths across %d CRDs\n", len(fieldToCRDs), len(registry))
}
