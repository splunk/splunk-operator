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

package config

import (
	"fmt"
	"os"

	"k8s.io/component-base/featuregate"
)

// Feature gate constants. Add new feature gates here following the checklist
// in docs/FeatureGates.md.
//
// Lifecycle:
//
//	Alpha  – off by default, opt-in via --feature-gates=<Gate>=true
//	Beta   – on  by default, opt-out via --feature-gates=<Gate>=false
//	GA     – on, locked; remove the gate in a subsequent release
const (
	// ValidationWebhook gates the centralized validation webhook server.
	// When enabled, the operator runs a validating webhook that enforces
	// CR schema rules at admission time.
	// Replaces the legacy ENABLE_VALIDATION_WEBHOOK env var.
	ValidationWebhook featuregate.Feature = "ValidationWebhook"
)

// defaultFeatureGates is the authoritative registry of all feature gates and
// their default state / maturity. Each entry here automatically becomes
// available via --feature-gates on the operator binary.
var defaultFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	ValidationWebhook: {Default: false, PreRelease: featuregate.Alpha},
}

var DefaultMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

func init() {
	if err := DefaultMutableFeatureGate.Add(defaultFeatureGates); err != nil {
		panic(err)
	}
	applyLegacyValidationWebhookEnv(DefaultMutableFeatureGate)
}

// applyLegacyValidationWebhookEnv preserves backwards compatibility for
// deployments using the ENABLE_VALIDATION_WEBHOOK env var instead of
// --feature-gates=ValidationWebhook=true.
// Remove once the ENABLE_VALIDATION_WEBHOOK deprecation period ends.
func applyLegacyValidationWebhookEnv(fg featuregate.MutableFeatureGate) {
	if os.Getenv("ENABLE_VALIDATION_WEBHOOK") == "true" {
		if err := fg.SetFromMap(map[string]bool{string(ValidationWebhook): true}); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: failed to apply legacy env var ENABLE_VALIDATION_WEBHOOK: %v\n", err)
		}
	}
}
