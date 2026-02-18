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
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// ── JSON output types ─────────────────────────────────────────────────

// Manifest is the top-level output structure.
type Manifest struct {
	Version         string                `json:"version"`
	GeneratedAt     string                `json:"generatedAt"`
	OperatorVersion string                `json:"operatorVersion"`
	APIVersion      string                `json:"apiVersion"`
	CRDs            map[string]CRDMapping `json:"crds"`
}

// CRDMapping holds the field mappings for one CRD kind.
type CRDMapping struct {
	SpecFields map[string]FieldMapping `json:"specFields"`
}

// FieldMapping is the per-field entry in the JSON output.
type FieldMapping struct {
	JSONPath string `json:"jsonPath"`
	GoType   string `json:"goType"`
	ConfFile string `json:"confFile"`
	Stanza   string `json:"stanza"`
	Key      string `json:"key"`
}

// ── CRD registry ──────────────────────────────────────────────────────

type crdEntry struct {
	kind     string
	specType reflect.Type
}

func crdRegistry() []crdEntry {
	return []crdEntry{
		{"Standalone", reflect.TypeOf(enterpriseApi.StandaloneSpec{})},
		{"ClusterManager", reflect.TypeOf(enterpriseApi.ClusterManagerSpec{})},
		{"IndexerCluster", reflect.TypeOf(enterpriseApi.IndexerClusterSpec{})},
		{"SearchHeadCluster", reflect.TypeOf(enterpriseApi.SearchHeadClusterSpec{})},
		{"LicenseManager", reflect.TypeOf(enterpriseApi.LicenseManagerSpec{})},
		{"MonitoringConsole", reflect.TypeOf(enterpriseApi.MonitoringConsoleSpec{})},
	}
}

// ── confContext tracks inherited confFile and stanza from parent tags ──

type confContext struct {
	confFile string
	stanza   string
}

// ── Reflection walker ─────────────────────────────────────────────────

// WalkConfTags recursively walks a struct type and collects fields that
// have a `splunkconf` struct tag. Parent struct/slice fields with a
// 2-part tag ("confFile,stanza") set the context for child leaf fields
// with a 1-part tag ("key").
func WalkConfTags(t reflect.Type, prefix string, ctx confContext) map[string]FieldMapping {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	result := make(map[string]FieldMapping)

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		// Determine JSON name
		jsonTag := sf.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		jsonName := strings.Split(jsonTag, ",")[0]

		// Handle embedded (anonymous) structs: flatten, keep parent prefix and context
		if sf.Anonymous {
			for k, v := range WalkConfTags(sf.Type, prefix, ctx) {
				result[k] = v
			}
			continue
		}

		if jsonName == "" {
			jsonName = sf.Name
		}

		fullPath := jsonName
		if prefix != "" {
			fullPath = prefix + "." + jsonName
		}

		// Check for splunkconf tag
		splunkTag := sf.Tag.Get("splunkconf")

		ft := sf.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		switch ft.Kind() {
		case reflect.Struct:
			if isLeafType(ft) {
				// Opaque k8s type — skip
				continue
			}
			// If this struct field has a 2-part splunkconf tag, it sets context for children
			childCtx := ctx
			if parts := strings.SplitN(splunkTag, ",", 2); len(parts) == 2 {
				childCtx = confContext{confFile: parts[0], stanza: parts[1]}
			}
			for k, v := range WalkConfTags(ft, fullPath, childCtx) {
				result[k] = v
			}

		case reflect.Slice:
			elemType := ft.Elem()
			if elemType.Kind() == reflect.Ptr {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct && !isLeafType(elemType) {
				childCtx := ctx
				if parts := strings.SplitN(splunkTag, ",", 2); len(parts) == 2 {
					childCtx = confContext{confFile: parts[0], stanza: parts[1]}
				}
				for k, v := range WalkConfTags(elemType, fullPath, childCtx) {
					result[k] = v
				}
			}

		default:
			// Leaf field — only include if it has a splunkconf key tag
			if splunkTag == "" {
				continue
			}
			if ctx.confFile == "" || ctx.stanza == "" {
				continue
			}
			result[fullPath] = FieldMapping{
				JSONPath: "spec." + fullPath,
				GoType:   ft.String(),
				ConfFile: ctx.confFile,
				Stanza:   ctx.stanza,
				Key:      splunkTag,
			}
		}
	}
	return result
}

// isLeafType returns true for complex types we don't want to recurse into.
func isLeafType(t reflect.Type) bool {
	return strings.HasPrefix(t.PkgPath(), "k8s.io/")
}

// ── Main ──────────────────────────────────────────────────────────────

func main() {
	outPath := flag.String("o", "spec-config-mapping.json", "Output JSON file path")
	flag.Parse()

	registry := crdRegistry()
	manifest := Manifest{
		Version:         "1.0.0",
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		OperatorVersion: "3.0.0",
		APIVersion:      enterpriseApi.APIVersion,
		CRDs:            make(map[string]CRDMapping),
	}

	for _, crd := range registry {
		fields := WalkConfTags(crd.specType, "", confContext{})
		manifest.CRDs[crd.kind] = CRDMapping{SpecFields: fields}
	}

	// Write output
	f, err := os.Create(*outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to create %s: %v\n", *outPath, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(manifest); err != nil {
		f.Close()
		fmt.Fprintf(os.Stderr, "error: failed to write JSON: %v\n", err)
		os.Exit(1)
	}
	f.Close()

	// Summary
	totalFields := 0
	for _, crd := range manifest.CRDs {
		totalFields += len(crd.SpecFields)
	}
	fmt.Fprintf(os.Stderr, "Generated %s: %d CRDs, %d total field mappings\n",
		*outPath, len(manifest.CRDs), totalFields)
}
