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
	ValidationWebhook  featuregate.Feature = "ValidationWebhook"
	PostgresController featuregate.Feature = "PostgresController"
	// CertManagement gates spec.certs[] mounting/rotation across all Splunk
	// Enterprise CR types (ReconcileCerts + the cert-secret watch mapper).
	// When disabled, spec.certs[] is ignored and no cert volumes are mounted.
	CertManagement featuregate.Feature = "CertManagement"
	// SplunkPodLifecycle gates the common Splunk workload lifecycle contract,
	// beginning with configurable Pod termination grace.
	SplunkPodLifecycle featuregate.Feature = "SplunkPodLifecycle"
	// SearchHeadClusterLifecycle gates durable Search Head Cluster lifecycle
	// orchestration and its customer-facing policy.
	SearchHeadClusterLifecycle featuregate.Feature = "SearchHeadClusterLifecycle"
)

// defaultFeatureGates is the authoritative registry of all feature gates and
// their default state / maturity. Each entry here automatically becomes
// available via --feature-gates on the operator binary.
var defaultFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	ValidationWebhook:  {Default: false, PreRelease: featuregate.Alpha},
	PostgresController: {Default: false, PreRelease: featuregate.Alpha},
	CertManagement:     {Default: true, PreRelease: featuregate.Beta},
	SplunkPodLifecycle: {Default: false, PreRelease: featuregate.Alpha},
	SearchHeadClusterLifecycle: {
		Default:    false,
		PreRelease: featuregate.Alpha,
	},
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

// EnableFeatureGate enables the given feature gates on the default mutable gate.
// Intended for use in test init() functions.
func EnableFeatureGate(gates ...featuregate.Feature) {
	enabled := make(map[string]bool, len(gates))
	for _, g := range gates {
		enabled[string(g)] = true
	}
	if err := DefaultMutableFeatureGate.SetFromMap(enabled); err != nil {
		panic(err)
	}
}

// ValidateFeatureGateDependencies verifies combinations that cannot operate
// safely. It is called after command-line feature-gate parsing.
func ValidateFeatureGateDependencies(fg featuregate.FeatureGate) error {
	if fg.Enabled(SearchHeadClusterLifecycle) && !fg.Enabled(SplunkPodLifecycle) {
		return fmt.Errorf("%s requires %s=true", SearchHeadClusterLifecycle, SplunkPodLifecycle)
	}
	return nil
}
