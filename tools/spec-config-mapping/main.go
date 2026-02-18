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
	"sort"
	"strings"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// ── JSON output types ─────────────────────────────────────────────────

// Manifest is the top-level output structure.
type Manifest struct {
	Version          string                `json:"version"`
	GeneratedAt      string                `json:"generatedAt"`
	OperatorVersion  string                `json:"operatorVersion"`
	APIVersion       string                `json:"apiVersion"`
	CRDs map[string]CRDMapping `json:"crds"`
}

// CRDMapping holds the field mappings for one CRD kind.
type CRDMapping struct {
	SpecFields map[string]FieldMapping `json:"specFields"`
}

// FieldMapping is the per-field entry in the JSON output.
type FieldMapping struct {
	JSONPath    string `json:"jsonPath"`
	GoType      string `json:"goType"`
	ConfFile    string `json:"confFile"`
	Stanza      string `json:"stanza"`
	Key         string `json:"key"`
	Description string `json:"description"`
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

// ── Reflection walker ─────────────────────────────────────────────────

// fieldInfo holds metadata about a discovered struct field.
type fieldInfo struct {
	jsonPath string
	goType   string
}

// WalkSpecFields recursively walks a struct type and collects leaf field
// JSON paths. Embedded structs are flattened (their prefix is inherited).
// Slice and struct fields are recursed into; primitive fields are leaves.
func WalkSpecFields(t reflect.Type, prefix string) []fieldInfo {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	var fields []fieldInfo
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		// Determine JSON name
		jsonTag := sf.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		jsonName := strings.Split(jsonTag, ",")[0]

		// Handle embedded (anonymous) structs: flatten, keep parent prefix
		if sf.Anonymous {
			fields = append(fields, WalkSpecFields(sf.Type, prefix)...)
			continue
		}

		if jsonName == "" {
			jsonName = sf.Name
		}

		fullPath := jsonName
		if prefix != "" {
			fullPath = prefix + "." + jsonName
		}

		ft := sf.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}

		switch ft.Kind() {
		case reflect.Struct:
			// Check if this is a well-known Kubernetes type we treat as a leaf
			if isLeafType(ft) {
				fields = append(fields, fieldInfo{jsonPath: fullPath, goType: ft.String()})
			} else {
				fields = append(fields, WalkSpecFields(ft, fullPath)...)
			}
		case reflect.Slice:
			elemType := ft.Elem()
			if elemType.Kind() == reflect.Ptr {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct && !isLeafType(elemType) {
				// Recurse into the element type (represents array items)
				fields = append(fields, WalkSpecFields(elemType, fullPath)...)
			} else {
				fields = append(fields, fieldInfo{jsonPath: fullPath, goType: ft.String()})
			}
		default:
			fields = append(fields, fieldInfo{jsonPath: fullPath, goType: ft.String()})
		}
	}
	return fields
}

// isLeafType returns true for complex types we don't want to recurse into
// (Kubernetes API types, etc.).
func isLeafType(t reflect.Type) bool {
	pkg := t.PkgPath()
	// Treat all k8s.io types as opaque leaves
	if strings.HasPrefix(pkg, "k8s.io/") {
		return true
	}
	return false
}

// ── Main ──────────────────────────────────────────────────────────────

func main() {
	outPath := flag.String("o", "spec-config-mapping.json", "Output JSON file path")
	strict := flag.Bool("strict", true, "Exit non-zero if any spec fields are unmapped")
	flag.Parse()

	registry := crdRegistry()
	manifest := Manifest{
		Version:         "1.0.0",
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		OperatorVersion: "3.0.0",
		APIVersion:      enterpriseApi.APIVersion,
		CRDs:            make(map[string]CRDMapping),
	}

	var unmapped []string

	for _, crd := range registry {
		fields := WalkSpecFields(crd.specType, "")
		specFields := make(map[string]FieldMapping)

		for _, f := range fields {
			target, ok := fieldMappings[f.jsonPath]
			if !ok {
				unmapped = append(unmapped, fmt.Sprintf("%s.%s", crd.kind, f.jsonPath))
				continue
			}

			// Only include fields with a full conf coordinate (confFile + stanza + key)
			if target.ConfFile == "" || target.ConfFile == "env" || target.Stanza == "" || target.Key == "" {
				continue
			}

			specFields[f.jsonPath] = FieldMapping{
				JSONPath:    "spec." + f.jsonPath,
				GoType:      f.goType,
				ConfFile:    target.ConfFile,
				Stanza:      target.Stanza,
				Key:         target.Key,
				Description: target.Description,
			}

		}

		manifest.CRDs[crd.kind] = CRDMapping{SpecFields: specFields}
	}

	// Write output using Encoder so we can disable HTML escaping
	// (json.MarshalIndent escapes <> as \u003c/\u003e by default)
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

	if len(unmapped) > 0 {
		fmt.Fprintf(os.Stderr, "\nWARNING: %d unmapped field(s):\n", len(unmapped))
		sort.Strings(unmapped)
		for _, u := range unmapped {
			fmt.Fprintf(os.Stderr, "  - %s\n", u)
		}
		if *strict {
			fmt.Fprintf(os.Stderr, "\nAdd mappings to tools/spec-config-mapping/mappings.go to fix this.\n")
			os.Exit(1)
		}
	}
}
