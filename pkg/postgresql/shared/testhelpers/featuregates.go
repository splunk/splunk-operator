/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package testhelpers provides shared test utilities for postgresql packages.
// It must not be imported by production code.
package testhelpers

import (
	"k8s.io/component-base/featuregate"

	"github.com/splunk/splunk-operator/pkg/config"
)

// EnableFeatureGate enables the given feature gates on the default mutable gate.
// Intended for use in test init() functions.
func EnableFeatureGate(gates ...featuregate.Feature) {
	enabled := make(map[string]bool, len(gates))
	for _, g := range gates {
		enabled[string(g)] = true
	}
	if err := config.DefaultMutableFeatureGate.SetFromMap(enabled); err != nil {
		panic(err)
	}
}
