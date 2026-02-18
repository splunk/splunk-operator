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
	"testing"
)

// TestStandaloneSmartStoreFields verifies SmartStore fields are discovered
// with correct conf coordinates for Standalone.
func TestStandaloneSmartStoreFields(t *testing.T) {
	registry := crdRegistry()

	for _, crd := range registry {
		if crd.kind != "Standalone" {
			continue
		}
		fields := WalkConfTags(crd.specType, "", confContext{})

		expected := map[string]FieldMapping{
			"smartstore.cacheManager.evictionPolicy": {
				ConfFile: "server.conf", Stanza: "cachemanager", Key: "eviction_policy",
			},
			"smartstore.cacheManager.maxCacheSize": {
				ConfFile: "server.conf", Stanza: "cachemanager", Key: "max_cache_size",
			},
			"smartstore.cacheManager.hotlistRecencySecs": {
				ConfFile: "server.conf", Stanza: "cachemanager", Key: "hotlist_recency_secs",
			},
			"smartstore.indexes.remotePath": {
				ConfFile: "indexes.conf", Stanza: "<index_name>", Key: "remotePath",
			},
			"smartstore.indexes.maxGlobalDataSizeMB": {
				ConfFile: "indexes.conf", Stanza: "<index_name>", Key: "maxGlobalDataSizeMB",
			},
			"smartstore.volumes.endpoint": {
				ConfFile: "indexes.conf", Stanza: "volume:<name>", Key: "remote.s3.endpoint",
			},
			"smartstore.defaults.volumeName": {
				ConfFile: "indexes.conf", Stanza: "default", Key: "remotePath",
			},
		}

		for path, want := range expected {
			got, ok := fields[path]
			if !ok {
				t.Errorf("expected field %q not found in Standalone", path)
				continue
			}
			if got.ConfFile != want.ConfFile {
				t.Errorf("%s: confFile = %q, want %q", path, got.ConfFile, want.ConfFile)
			}
			if got.Stanza != want.Stanza {
				t.Errorf("%s: stanza = %q, want %q", path, got.Stanza, want.Stanza)
			}
			if got.Key != want.Key {
				t.Errorf("%s: key = %q, want %q", path, got.Key, want.Key)
			}
		}
	}
}

// TestContextInheritance verifies that shared embedded types inherit the
// correct context from different parents (e.g., hotlistRecencySecs maps
// to server.conf under cacheManager but indexes.conf under indexes).
func TestContextInheritance(t *testing.T) {
	registry := crdRegistry()

	for _, crd := range registry {
		if crd.kind != "Standalone" {
			continue
		}
		fields := WalkConfTags(crd.specType, "", confContext{})

		cmField := fields["smartstore.cacheManager.hotlistRecencySecs"]
		idxField := fields["smartstore.indexes.hotlistRecencySecs"]

		if cmField.ConfFile != "server.conf" {
			t.Errorf("cacheManager.hotlistRecencySecs: confFile = %q, want server.conf", cmField.ConfFile)
		}
		if idxField.ConfFile != "indexes.conf" {
			t.Errorf("indexes.hotlistRecencySecs: confFile = %q, want indexes.conf", idxField.ConfFile)
		}
		if cmField.Stanza != "cachemanager" {
			t.Errorf("cacheManager.hotlistRecencySecs: stanza = %q, want cachemanager", cmField.Stanza)
		}
		if idxField.Stanza != "<index_name>" {
			t.Errorf("indexes.hotlistRecencySecs: stanza = %q, want <index_name>", idxField.Stanza)
		}
		if cmField.Key != idxField.Key {
			t.Errorf("hotlistRecencySecs key mismatch: %q vs %q", cmField.Key, idxField.Key)
		}
	}
}

// TestSmartStoreOnlyInCorrectCRDs verifies SmartStore fields only appear
// in Standalone and ClusterManager.
func TestSmartStoreOnlyInCorrectCRDs(t *testing.T) {
	registry := crdRegistry()
	ssField := "smartstore.cacheManager.evictionPolicy"

	for _, crd := range registry {
		fields := WalkConfTags(crd.specType, "", confContext{})
		_, found := fields[ssField]

		switch crd.kind {
		case "Standalone", "ClusterManager":
			if !found {
				t.Errorf("%s should have SmartStore fields but %q not found", crd.kind, ssField)
			}
		default:
			if found {
				t.Errorf("%s should NOT have SmartStore fields but %q was found", crd.kind, ssField)
			}
		}
	}
}

// TestNoUntaggedFieldsLeakThrough verifies that fields without a splunkconf
// tag do not appear in the output.
func TestNoUntaggedFieldsLeakThrough(t *testing.T) {
	registry := crdRegistry()

	untaggedPaths := []string{
		"image",
		"replicas",
		"licenseUrl",
		"affinity",
		"resources",
	}

	for _, crd := range registry {
		fields := WalkConfTags(crd.specType, "", confContext{})
		for _, path := range untaggedPaths {
			if _, found := fields[path]; found {
				t.Errorf("%s: untagged field %q should not appear in output", crd.kind, path)
			}
		}
	}
}

// TestEsDefaultsMapping verifies the web.conf mapping from the app framework.
func TestEsDefaultsMapping(t *testing.T) {
	registry := crdRegistry()

	for _, crd := range registry {
		if crd.kind != "Standalone" {
			continue
		}
		fields := WalkConfTags(crd.specType, "", confContext{})

		path := "appRepo.defaults.premiumAppsProps.esDefaults.sslEnablement"
		got, ok := fields[path]
		if !ok {
			t.Fatalf("expected field %q not found", path)
		}
		if got.ConfFile != "web.conf" || got.Stanza != "settings" || got.Key != "enableSplunkWebSSL" {
			t.Errorf("%s: got %s/%s/%s, want web.conf/settings/enableSplunkWebSSL",
				path, got.ConfFile, got.Stanza, got.Key)
		}
	}
}
